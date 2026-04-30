package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIConfig configures the OpenAI-compatible provider.
type OpenAIConfig struct {
	// APIKey is the bearer token. Optional for self-hosted endpoints (e.g. Hermes).
	APIKey string
	// BaseURL is the API base (e.g. "http://192.168.59.118:8642/v1").
	// Defaults to "https://api.openai.com/v1".
	BaseURL string
	// Model name to use (e.g. "hermes-agent", "gpt-4o-mini").
	Model string
	// Timeout for HTTP requests. Defaults to 30s.
	Timeout time.Duration
}

// NewOpenAI returns an OpenAI-compatible provider.
func NewOpenAI(cfg OpenAIConfig) (Provider, error) {
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 90 * time.Second
	}
	return &openaiProvider{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}, nil
}

type openaiProvider struct {
	cfg    OpenAIConfig
	client *http.Client
}

func (p *openaiProvider) Name() string { return "openai" }

func (p *openaiProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	body := p.buildRequest(req)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai: marshal request: %w", err)
	}

	url := p.cfg.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("openai: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	return p.parseResponse(respBody)
}

// --- OpenAI wire format types ---

type oaiRequest struct {
	Model       string       `json:"model"`
	Messages    []oaiMessage `json:"messages"`
	Tools       []oaiTool    `json:"tools,omitempty"`
	Temperature *float64     `json:"temperature,omitempty"`
	MaxTokens   *int         `json:"max_tokens,omitempty"`
	Stream      bool         `json:"stream"`
}

type oaiMessage struct {
	Role       string        `json:"role"`
	Content    string        `json:"content,omitempty"`
	Name       string        `json:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty"`
	ToolCalls  []oaiToolCall `json:"tool_calls,omitempty"`
}

type oaiTool struct {
	Type     string      `json:"type"`
	Function oaiFunction `json:"function"`
}

type oaiFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type oaiToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function oaiToolCallFunc `json:"function"`
}

type oaiToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Error   *oaiError   `json:"error,omitempty"`
}

type oaiChoice struct {
	Message      oaiMessage `json:"message"`
	FinishReason string     `json:"finish_reason"`
}

type oaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// --- Conversion helpers ---

func (p *openaiProvider) buildRequest(req ChatRequest) oaiRequest {
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}

	msgs := make([]oaiMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		om := oaiMessage{
			Role:       string(m.Role),
			Content:    m.Content,
			Name:       m.Name,
			ToolCallID: m.ToolCallID,
		}
		for _, tc := range m.ToolCalls {
			argsJSON, _ := json.Marshal(tc.Arguments)
			om.ToolCalls = append(om.ToolCalls, oaiToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: oaiToolCallFunc{
					Name:      tc.Name,
					Arguments: string(argsJSON),
				},
			})
		}
		msgs = append(msgs, om)
	}

	var tools []oaiTool
	for _, t := range req.Tools {
		tools = append(tools, oaiTool{
			Type: "function",
			Function: oaiFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}

	r := oaiRequest{
		Model:    model,
		Messages: msgs,
		Tools:    tools,
		Stream:   false,
	}
	if req.Temperature > 0 {
		t := req.Temperature
		r.Temperature = &t
	}
	if req.MaxTokens > 0 {
		mt := req.MaxTokens
		r.MaxTokens = &mt
	}
	return r
}

func (p *openaiProvider) parseResponse(body []byte) (*ChatResponse, error) {
	var oaiResp oaiResponse
	if err := json.Unmarshal(body, &oaiResp); err != nil {
		return nil, fmt.Errorf("openai: unmarshal response: %w", err)
	}
	if oaiResp.Error != nil {
		return nil, fmt.Errorf("openai: API error: %s (%s)", oaiResp.Error.Message, oaiResp.Error.Type)
	}
	if len(oaiResp.Choices) == 0 {
		return nil, errors.New("openai: empty choices in response")
	}

	choice := oaiResp.Choices[0]
	resp := &ChatResponse{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
	}

	for _, tc := range choice.Message.ToolCalls {
		resp.ToolCalls = append(resp.ToolCalls, ToolCall{
			ID:   tc.ID,
			Name: tc.Function.Name,
			Arguments: func() map[string]any {
				var m map[string]any
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &m)
				return m
			}(),
		})
	}
	return resp, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

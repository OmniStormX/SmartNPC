package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// HermesConfig configures the Hermes Responses API provider.
// Uses /v1/responses endpoint for stateful, server-persisted sessions.
type HermesConfig struct {
	// APIKey is the bearer token. Optional for local instances.
	APIKey string
	// BaseURL is the gateway URL (e.g. "http://192.168.59.118:8642").
	// The /v1/responses path is appended automatically.
	BaseURL string
	// Model name (e.g. "hermes-agent").
	Model string
	// Conversation is the named session key. Hermes automatically chains
	// responses within the same conversation, maintaining full history
	// server-side. Typically set to "smartnpc-{npc_name}".
	Conversation string
	// Timeout for HTTP requests. Defaults to 90s.
	Timeout time.Duration
}

// NewHermes returns a Hermes Responses API provider. Unlike OpenAI chat
// completions, this provider maintains stateful sessions server-side — the
// caller only sends the latest user input, and Hermes reconstructs the full
// conversation from its persisted history.
//
// Conversations are date-scoped (e.g. "smartnpc-abigail-20260508") to prevent
// unbounded context growth. On context overflow, an epoch counter increments
// to start a fresh session while Hermes's long-term memory carries over.
func NewHermes(cfg HermesConfig) (Provider, error) {
	if cfg.Model == "" {
		cfg.Model = "hermes-agent"
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("hermes: BaseURL is required")
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 90 * time.Second
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &hermesProvider{
		cfg:         cfg,
		client:      &http.Client{Timeout: cfg.Timeout},
		initialized: make(map[string]bool),
		turnCount:   make(map[string]int),
	}, nil
}

type hermesProvider struct {
	cfg         HermesConfig
	client      *http.Client
	mu          sync.Mutex
	epoch       int // incremented on context overflow to start fresh conversation
	initialized map[string]bool
	lastTokens  int            // input_tokens from the most recent successful response
	turnCount   map[string]int // per-conversation-key turn counter
}

// maxInputTokens defines the soft ceiling before we proactively rotate the
// conversation epoch. This avoids waiting 80+ seconds for a 78K-token request
// to complete before detecting overflow. Set well below the model's hard limit
// (256K for gpt-5.5) so the last response in an epoch still has headroom.
const maxInputTokens = 30000

// maxTurnsPerEpoch limits turns within a single conversation epoch. When
// reached, we rotate to a new epoch with a compressed summary of the prior
// conversation, removing redundant system prompts and idle exchanges.
const maxTurnsPerEpoch = 10

func (p *hermesProvider) Name() string { return "hermes" }

// conversationKey returns the current conversation name with date + epoch.
func (p *hermesProvider) conversationKey() string {
	p.mu.Lock()
	e := p.epoch
	p.mu.Unlock()
	key := p.cfg.Conversation + "-" + time.Now().Format("20060102")
	if e > 0 {
		key += fmt.Sprintf("-e%d", e)
	}
	return key
}

// Chat implements Provider. Manages conversation lifecycle:
// 1. Tracks turns per epoch — at maxTurnsPerEpoch, compresses and rotates
// 2. On context_length_exceeded, auto-rotates with fresh persona prompt
// 3. On token budget exceeded (maxInputTokens), proactively rotates
func (p *hermesProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	userInput := extractLastUserMessage(req.Messages)
	if userInput == "" {
		return &ChatResponse{Content: "(no input)"}, nil
	}

	convKey := p.conversationKey()

	p.mu.Lock()
	first := !p.initialized[convKey]
	lastTok := p.lastTokens
	turns := p.turnCount[convKey]
	p.mu.Unlock()

	// ── Turn-based compression: rotate at maxTurnsPerEpoch ──
	if !first && turns >= maxTurnsPerEpoch {
		fmt.Fprintf(os.Stderr, "[hermes] turn limit reached (%d/%d), compressing context → new epoch\n", turns, maxTurnsPerEpoch)
		p.rotateEpoch(convKey)
		convKey = p.conversationKey()
		first = true
	}

	// ── Token budget check: rotate if last response was too large ──
	if !first && lastTok > maxInputTokens {
		fmt.Fprintf(os.Stderr, "[hermes] proactive rotation: last request used %d input tokens (limit %d)\n", lastTok, maxInputTokens)
		p.rotateEpoch(convKey)
		convKey = p.conversationKey()
		first = true
	}

	var instructions string
	if first {
		// First request in new epoch — send the full persona prompt so
		// Hermes has the complete system context before replying.
		instructions = extractSystemPrompt(req.Messages)
		if len(instructions) > 6000 {
			instructions = instructions[:6000]
		}
	} else {
		instructions = extractDynamicContext(req.Messages)
	}

	// If rotating due to turn limit, prepend a context-carry note so the
	// model knows prior conversation exists in its long-term memory.
	if first && turns > 0 {
		userInput = "[系统：之前的对话已归档到长期记忆中。你对这个玩家的了解仍然完整，请继续保持角色，不要重复自我介绍。]\n\n" + userInput
	}

	result, err := p.doRequest(ctx, req.Model, userInput, instructions, convKey)
	if err != nil && isContextOverflow(err) {
		// Context overflow — rotate to new epoch with fresh persona prompt.
		fmt.Fprintf(os.Stderr, "[hermes] context overflow, rotating: %s → new epoch\n", convKey)
		p.rotateEpoch(convKey)
		newConvKey := p.conversationKey()

		freshInstructions := extractSystemPrompt(req.Messages)
		if len(freshInstructions) > 4000 {
			freshInstructions = freshInstructions[:4000]
		}
		freshInput := "[系统：由于对话过长，之前的详细对话已归档到记忆中。你的长期记忆仍然完整，请继续保持角色。]\n\n" + userInput

		result, err = p.doRequest(ctx, req.Model, freshInput, freshInstructions, newConvKey)
		if err == nil {
			p.mu.Lock()
			p.initialized[newConvKey] = true
			p.turnCount[newConvKey] = 1
			p.mu.Unlock()
		}
		return result, err
	}

	if err == nil {
		p.mu.Lock()
		p.initialized[convKey] = true
		p.turnCount[convKey] = turns + 1
		p.mu.Unlock()
	}
	return result, err
}

// rotateEpoch increments the epoch counter and resets tracking for the old key.
func (p *hermesProvider) rotateEpoch(oldKey string) {
	p.mu.Lock()
	p.epoch++
	delete(p.initialized, oldKey)
	delete(p.turnCount, oldKey)
	p.mu.Unlock()
}

// doRequest executes a single /v1/responses call.
func (p *hermesProvider) doRequest(ctx context.Context, model, input, instructions, conversation string) (*ChatResponse, error) {
	reqStart := time.Now()
	body := hermesRequest{
		Model:        model,
		Input:        input,
		Instructions: instructions,
		Conversation: conversation,
		Store:        true,
	}
	if body.Model == "" {
		body.Model = p.cfg.Model
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("hermes: marshal request: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[hermes] POST conversation=%q input=%q instructions_len=%d\n",
		conversation, truncateForLog(input, 60), len(instructions))

	url := p.cfg.BaseURL + "/v1/responses"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("hermes: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	httpElapsed := time.Since(reqStart)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[hermes] request failed after %dms: %v\n", httpElapsed.Milliseconds(), err)
		return nil, fmt.Errorf("hermes: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hermes: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errMsg := truncate(string(respBody), 300)
		fmt.Fprintf(os.Stderr, "[hermes] ERROR HTTP %d after %dms: %s\n", resp.StatusCode, httpElapsed.Milliseconds(), truncate(string(respBody), 200))
		return nil, fmt.Errorf("hermes: HTTP %d: %s", resp.StatusCode, errMsg)
	}

	totalElapsed := time.Since(reqStart)
	inputTokens := extractInputTokens(respBody)
	fmt.Fprintf(os.Stderr, "[hermes] OK id=%s http=%dms total=%dms tokens_in=%s\n",
		extractResponseID(respBody), httpElapsed.Milliseconds(), totalElapsed.Milliseconds(),
		extractUsageInfo(respBody))

	// Track input token usage for proactive epoch rotation.
	p.mu.Lock()
	p.lastTokens = inputTokens
	p.mu.Unlock()

	return p.parseResponse(respBody)
}

// isContextOverflow checks if the error is a context length exceeded error.
func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "tokens exceed") ||
		strings.Contains(msg, "too many tokens")
}

// --- Hermes wire format types ---

type hermesRequest struct {
	Model        string `json:"model"`
	Input        string `json:"input"`
	Instructions string `json:"instructions,omitempty"`
	Conversation string `json:"conversation,omitempty"`
	Store        bool   `json:"store"`
}

type hermesResponse struct {
	ID     string             `json:"id"`
	Object string             `json:"object"`
	Status string             `json:"status"`
	Output []hermesOutputItem `json:"output"`
	Error  *hermesError       `json:"error,omitempty"`
}

type hermesOutputItem struct {
	Type    string              `json:"type"`
	Role    string              `json:"role,omitempty"`
	Content []hermesContentPart `json:"content,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type hermesContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type hermesError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

func (p *hermesProvider) parseResponse(body []byte) (*ChatResponse, error) {
	var resp hermesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("hermes: unmarshal response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("hermes: API error: %s (%s)", resp.Error.Message, resp.Error.Type)
	}

	var textParts []string
	for _, item := range resp.Output {
		if item.Type == "message" && item.Role == "assistant" {
			for _, part := range item.Content {
				if part.Type == "output_text" && part.Text != "" {
					textParts = append(textParts, part.Text)
				}
			}
		}
	}

	content := strings.Join(textParts, "\n")
	if content == "" {
		content = "(no response)"
	}

	return &ChatResponse{
		Content:      content,
		FinishReason: "stop",
	}, nil
}

// --- Helpers ---

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func extractResponseID(body []byte) string {
	var stub struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(body, &stub)
	return stub.ID
}

// extractUsageInfo pulls token counts from the response for logging.
func extractUsageInfo(body []byte) string {
	var stub struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &stub); err != nil || stub.Usage.TotalTokens == 0 {
		return "?"
	}
	return fmt.Sprintf("in=%d out=%d total=%d", stub.Usage.InputTokens, stub.Usage.OutputTokens, stub.Usage.TotalTokens)
}

// extractInputTokens returns the input_tokens count from a response body.
func extractInputTokens(body []byte) int {
	var stub struct {
		Usage struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(body, &stub)
	return stub.Usage.InputTokens
}

// extractDynamicContext pulls only per-turn dynamic context from system
// messages. Skips the static persona prompt (Hermes already has it).
// On first request (no dynamic lines found), sends capped full prompt.
func extractDynamicContext(msgs []Message) string {
	full := extractSystemPrompt(msgs)
	if full == "" {
		return ""
	}

	var dynamic []string
	for _, line := range strings.Split(full, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "Act with") ||
			strings.HasPrefix(trimmed, "Openness level") || strings.HasPrefix(trimmed, "Never quote") {
			dynamic = append(dynamic, line)
		}
	}

	if len(dynamic) == 0 {
		if len(full) > 4000 {
			return full[:4000]
		}
		return full
	}
	return strings.Join(dynamic, "\n")
}

func extractLastUserMessage(msgs []Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func extractSystemPrompt(msgs []Message) string {
	var parts []string
	for _, m := range msgs {
		if m.Role == RoleSystem && m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	return strings.Join(parts, "\n\n")
}

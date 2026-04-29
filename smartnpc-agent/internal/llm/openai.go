package llm

import (
	"context"
	"errors"
)

// OpenAIConfig configures the OpenAI provider.
//
// M1 keeps this as a stub: real wiring (HTTP client, tool-call translation,
// streaming, retries) lands in M4 along with the agent loop.
type OpenAIConfig struct {
	APIKey  string
	BaseURL string // optional; leave empty for the official endpoint
	Model   string // e.g. "gpt-4o-mini"
}

// NewOpenAI returns an OpenAI-backed provider.
func NewOpenAI(cfg OpenAIConfig) (Provider, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("openai: APIKey is required")
	}
	if cfg.Model == "" {
		cfg.Model = "gpt-4o-mini"
	}
	return &openaiProvider{cfg: cfg}, nil
}

type openaiProvider struct {
	cfg OpenAIConfig
}

func (p *openaiProvider) Name() string { return "openai" }

func (p *openaiProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// TODO(M4): implement using the official OpenAI Go SDK or net/http
	//          against /v1/chat/completions, translating ToolSpec/ToolCall.
	return nil, errors.New("openai provider not implemented yet (planned for M4)")
}

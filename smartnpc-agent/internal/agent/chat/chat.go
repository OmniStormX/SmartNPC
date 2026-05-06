// Package chat implements an LLM-backed NPC chat agent. It listens for
// chat_received MCP notifications, runs a multi-turn tool-calling loop
// against the configured LLM, and sends the final reply via chat_say.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

const (
	maxToolRounds  = 5
	defaultTimeout = 90 * time.Second
)

// Config configures the chat agent.
type Config struct {
	// Provider is the LLM backend (e.g. OpenAI-compatible Hermes).
	Provider llm.Provider
	// Speaker is the NPC display name (must match a game NPC for dialogue box).
	Speaker string
	// SystemPrompt seeds the LLM persona.
	SystemPrompt string
	// MaxHistory caps the number of user+assistant turns kept in memory.
	MaxHistory int
	// Timeout for a single LLM round-trip. Defaults to 90s.
	Timeout time.Duration
	// Logger for diagnostics.
	Logger *slog.Logger
}

// Agent holds conversation state and handles incoming chat notifications.
type Agent struct {
	cfg     Config
	mu      sync.Mutex
	history []llm.Message
	session *mcp.ClientSession
	tools   []llm.ToolSpec
}

// New creates a chat agent ready to handle notifications.
func New(cfg Config) *Agent {
	if cfg.Speaker == "" {
		cfg.Speaker = "Abigail"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are a friendly NPC in Stardew Valley. Respond briefly and in character. Use the player's language."
	}
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 20
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Agent{cfg: cfg}
}

// SetSession wires the MCP session used for tool calls.
// Must be called after mcpclient.Spawn completes.
func (a *Agent) SetSession(session *mcp.ClientSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = session
}

// LoadTools fetches available MCP tools and caches them for LLM requests.
func (a *Agent) LoadTools(ctx context.Context) error {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		return fmt.Errorf("session not ready")
	}

	res, err := s.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("list tools: %w", err)
	}

	specs := make([]llm.ToolSpec, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema := map[string]any{}
		if t.InputSchema != nil {
			b, _ := json.Marshal(t.InputSchema)
			_ = json.Unmarshal(b, &schema)
		}
		specs = append(specs, llm.ToolSpec{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}

	a.mu.Lock()
	a.tools = specs
	a.mu.Unlock()
	a.cfg.Logger.Info("tools loaded", "count", len(specs))
	return nil
}

// HandleNotification returns a function suitable for mcp.ClientOptions.LoggingMessageHandler.
func (a *Agent) HandleNotification() func(context.Context, *mcp.LoggingMessageRequest) {
	return func(_ context.Context, req *mcp.LoggingMessageRequest) {
		text, ok := extractChatText(req, a.cfg.Speaker)
		if !ok {
			return
		}
		a.cfg.Logger.Info("chat received", "text", text)

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
			defer cancel()

			reply, err := a.respond(ctx, text)
			if err != nil {
				a.cfg.Logger.Error("LLM call failed", "err", err)
				reply = "...抱歉，我刚才走神了。"
			}
			a.cfg.Logger.Info("LLM replied", "reply", reply)

			a.mu.Lock()
			s := a.session
			a.mu.Unlock()
			if s == nil {
				a.cfg.Logger.Warn("session not ready, dropping reply")
				return
			}

			_, err = s.CallTool(ctx, &mcp.CallToolParams{
				Name: "chat_say",
				Arguments: map[string]any{
					"speaker": a.cfg.Speaker,
					"text":    reply,
				},
			})
			if err != nil {
				a.cfg.Logger.Warn("chat_say failed", "err", err)
			}
		}()
	}
}

// respond runs the LLM loop: send messages → if tool_calls → execute → repeat.
func (a *Agent) respond(ctx context.Context, userText string) (string, error) {
	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: userText})
	a.trimHistory()
	msgs := a.buildMessages()
	tools := a.tools
	a.mu.Unlock()

	for round := 0; round < maxToolRounds; round++ {
		resp, err := a.cfg.Provider.Chat(ctx, llm.ChatRequest{
			Messages:    msgs,
			Tools:       tools,
			Temperature: 0.8,
			MaxTokens:   300,
		})
		if err != nil {
			return "", err
		}

		// If the model produced a text response, we're done.
		if len(resp.ToolCalls) == 0 {
			reply := resp.Content
			if reply == "" {
				reply = "(no response)"
			}
			a.mu.Lock()
			a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: reply})
			a.trimHistory()
			a.mu.Unlock()
			return reply, nil
		}

		// Model wants tool calls — execute them and feed results back.
		// First, append assistant message with tool calls to history.
		assistantMsg := llm.Message{Role: llm.RoleAssistant, ToolCalls: resp.ToolCalls}
		msgs = append(msgs, assistantMsg)

		for _, tc := range resp.ToolCalls {
			result, err := a.executeTool(ctx, tc)
			if err != nil {
				result = fmt.Sprintf("error: %v", err)
			}
			msgs = append(msgs, llm.Message{
				Role:       llm.RoleTool,
				Content:    result,
				Name:       tc.Name,
				ToolCallID: tc.ID,
			})
			a.cfg.Logger.Debug("tool executed", "name", tc.Name, "result_len", len(result))
		}
	}

	// Exhausted tool rounds — ask LLM for a final text reply without tools.
	resp, err := a.cfg.Provider.Chat(ctx, llm.ChatRequest{
		Messages:    msgs,
		Temperature: 0.8,
		MaxTokens:   300,
	})
	if err != nil {
		return "", err
	}
	reply := resp.Content
	if reply == "" {
		reply = "(no response)"
	}
	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: reply})
	a.trimHistory()
	a.mu.Unlock()
	return reply, nil
}

// executeTool calls an MCP tool via the session.
func (a *Agent) executeTool(ctx context.Context, tc llm.ToolCall) (string, error) {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		return "", fmt.Errorf("session not ready")
	}

	res, err := s.CallTool(ctx, &mcp.CallToolParams{
		Name:      tc.Name,
		Arguments: tc.Arguments,
	})
	if err != nil {
		return "", fmt.Errorf("call %s: %w", tc.Name, err)
	}

	if res.IsError {
		return fmt.Sprintf("tool error: %v", res.Content), nil
	}

	// Extract text content from result.
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		return string(b), nil
	}
	var out string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			out += tc.Text
		}
	}
	if out == "" {
		out = "{}"
	}
	return out, nil
}

// buildMessages constructs system + history for the LLM request.
func (a *Agent) buildMessages() []llm.Message {
	msgs := make([]llm.Message, 0, 1+len(a.history))
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: a.cfg.SystemPrompt})
	msgs = append(msgs, a.history...)
	return msgs
}

// trimHistory keeps only the most recent MaxHistory messages.
func (a *Agent) trimHistory() {
	if len(a.history) > a.cfg.MaxHistory*2 {
		a.history = a.history[len(a.history)-a.cfg.MaxHistory*2:]
	}
}

// extractChatText extracts player text from a chat_received notification.
func extractChatText(req *mcp.LoggingMessageRequest, selfSpeaker string) (string, bool) {
	if req == nil || req.Params == nil {
		return "", false
	}
	m, ok := req.Params.Data.(map[string]any)
	if !ok {
		return "", false
	}
	if m["kind"] != "stardew/event" || m["name"] != "chat_received" {
		return "", false
	}
	raw, ok := m["data"]
	if !ok {
		return "", false
	}
	var inner struct {
		Text   string `json:"text"`
		Source string `json:"source"`
	}
	switch v := raw.(type) {
	case json.RawMessage:
		_ = json.Unmarshal(v, &inner)
	case map[string]any:
		if t, ok := v["text"].(string); ok {
			inner.Text = t
		}
		if s, ok := v["source"].(string); ok {
			inner.Source = s
		}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		_ = json.Unmarshal(b, &inner)
	}
	if inner.Text == "" {
		return "", false
	}
	if inner.Source == selfSpeaker {
		return "", false
	}
	return inner.Text, true
}

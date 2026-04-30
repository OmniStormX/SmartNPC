// Package chat implements a simple LLM-backed chat agent that responds to
// player messages via the game's chat box. It maintains a short conversation
// history per session and forwards player input to a configured LLM provider.
package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-agent/internal/llm"
)

// Config configures the chat agent.
type Config struct {
	// Provider is the LLM backend (e.g. OpenAI-compatible Hermes).
	Provider llm.Provider
	// Speaker is the NPC display name shown in the chat box.
	Speaker string
	// SystemPrompt seeds the LLM persona.
	SystemPrompt string
	// MaxHistory caps the number of user+assistant turns kept in memory.
	// Older turns are dropped. 0 means unlimited (not recommended).
	MaxHistory int
	// Logger for diagnostics.
	Logger *slog.Logger
}

// Agent holds conversation state and handles incoming chat notifications.
type Agent struct {
	cfg     Config
	mu      sync.Mutex
	history []llm.Message
	session *mcp.ClientSession
}

// New creates a chat agent ready to handle notifications.
func New(cfg Config) *Agent {
	if cfg.Speaker == "" {
		cfg.Speaker = "SmartNPC"
	}
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = "You are a friendly NPC in Stardew Valley. Respond briefly and in character. Use the player's language."
	}
	if cfg.MaxHistory <= 0 {
		cfg.MaxHistory = 20
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Agent{cfg: cfg}
}

// SetSession wires the MCP session used for tool calls (chat_say).
// Must be called after mcpclient.Spawn completes.
func (a *Agent) SetSession(session *mcp.ClientSession) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = session
}

// HandleNotification returns a function suitable for mcp.ClientOptions.LoggingMessageHandler.
// The session is resolved lazily via SetSession, so it's safe to pass this
// handler to mcpclient.Spawn before the session exists.
func (a *Agent) HandleNotification() func(context.Context, *mcp.LoggingMessageRequest) {
	return func(_ context.Context, req *mcp.LoggingMessageRequest) {
		text, ok := extractChatText(req, a.cfg.Speaker)
		if !ok {
			return
		}
		a.cfg.Logger.Info("chat received", "text", text)

		go func() {
			// Use a fresh context — the notification handler ctx may be
			// cancelled as soon as the handler returns.
			ctx := context.Background()
			reply, err := a.respond(ctx, text)
			if err != nil {
				a.cfg.Logger.Error("LLM call failed", "err", err)
				return
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

// respond calls the LLM with the current conversation history + new user message.
func (a *Agent) respond(ctx context.Context, userText string) (string, error) {
	a.mu.Lock()
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: userText})
	a.trimHistory()
	// Build the full message list: system + history
	msgs := make([]llm.Message, 0, 1+len(a.history))
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: a.cfg.SystemPrompt})
	msgs = append(msgs, a.history...)
	a.mu.Unlock()

	resp, err := a.cfg.Provider.Chat(ctx, llm.ChatRequest{
		Messages:    msgs,
		Temperature: 0.8,
		MaxTokens:   200,
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

// trimHistory keeps only the most recent MaxHistory messages (user+assistant pairs).
func (a *Agent) trimHistory() {
	if len(a.history) > a.cfg.MaxHistory*2 {
		a.history = a.history[len(a.history)-a.cfg.MaxHistory*2:]
	}
}

// extractChatText mirrors the logic from the echo agent.
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

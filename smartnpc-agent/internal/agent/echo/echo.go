// Package echo implements a minimal "AI" that mirrors player chat back into
// the game. It exists to validate the round trip from in-game chat input ->
// MCP notification -> agent -> chat_say tool call -> in-game display, without
// requiring an LLM provider.
//
// Usage:
//
//	cs := /* mcp.ClientSession */
//	echo.Run(ctx, cs, echo.Options{Speaker: "SmartNPC"})
//
// The goroutine returned by Run terminates when ctx is cancelled or cs is
// closed. Errors during a single chat_say call are logged but do not stop
// the loop.
package echo

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Options configures the echo agent.
type Options struct {
	// Speaker is the chat-box display name used when echoing.
	Speaker string
	// Prefix is prepended to the echoed text. Default: "You said: ".
	Prefix string
	// Logger receives diagnostic output. Defaults to slog.Default().
	Logger *slog.Logger
}

// HandleNotification is the function to plug into mcp.ClientOptions
// LoggingMessageHandler. It picks out chat_received events and replies with
// chat_say. Other notifications are ignored.
//
// session is captured at construction time and used to issue tool calls.
func HandleNotification(session *mcp.ClientSession, opts Options) func(context.Context, *mcp.LoggingMessageRequest) {
	if opts.Speaker == "" {
		opts.Speaker = "SmartNPC"
	}
	if opts.Prefix == "" {
		opts.Prefix = "You said: "
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	return func(ctx context.Context, req *mcp.LoggingMessageRequest) {
		text, ok := extractChatText(req, opts.Speaker)
		if !ok {
			return
		}
		go func() {
			_, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name: "chat_say",
				Arguments: map[string]any{
					"speaker": opts.Speaker,
					"text":    opts.Prefix + text,
					"color":   "yellow",
				},
			})
			if err != nil {
				opts.Logger.Warn("echo chat_say failed", "err", err)
			}
		}()
	}
}

// extractChatText returns (text, true) when the notification is a
// chat_received event from a non-self source.
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
	// data is the inner event payload {text, source}.
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
		// Re-encode to bridge any format we don't know.
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

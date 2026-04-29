package echo

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// extractChatText is the only piece worth unit-testing in isolation; the
// full session integration test lives at the cmd level.
func TestExtractChatText(t *testing.T) {
	tests := []struct {
		name      string
		params    *mcp.LoggingMessageParams
		self      string
		wantText  string
		wantOK    bool
	}{
		{
			name: "valid chat_received from player",
			params: &mcp.LoggingMessageParams{
				Level: "info",
				Data: map[string]any{
					"kind": "stardew/event",
					"name": "chat_received",
					"data": map[string]any{"text": "hi", "source": "player"},
				},
			},
			self:     "SmartNPC",
			wantText: "hi",
			wantOK:   true,
		},
		{
			name: "ignored: wrong kind",
			params: &mcp.LoggingMessageParams{
				Data: map[string]any{
					"kind": "other",
					"name": "chat_received",
					"data": map[string]any{"text": "hi"},
				},
			},
			self:   "SmartNPC",
			wantOK: false,
		},
		{
			name: "ignored: own message",
			params: &mcp.LoggingMessageParams{
				Data: map[string]any{
					"kind": "stardew/event",
					"name": "chat_received",
					"data": map[string]any{"text": "hi", "source": "SmartNPC"},
				},
			},
			self:   "SmartNPC",
			wantOK: false,
		},
		{
			name: "ignored: empty text",
			params: &mcp.LoggingMessageParams{
				Data: map[string]any{
					"kind": "stardew/event",
					"name": "chat_received",
					"data": map[string]any{"text": ""},
				},
			},
			self:   "SmartNPC",
			wantOK: false,
		},
		{
			name: "data as RawMessage",
			params: &mcp.LoggingMessageParams{
				Data: map[string]any{
					"kind": "stardew/event",
					"name": "chat_received",
					"data": json.RawMessage(`{"text":"howdy","source":"player"}`),
				},
			},
			self:     "SmartNPC",
			wantText: "howdy",
			wantOK:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &mcp.LoggingMessageRequest{Params: tt.params}
			text, ok := extractChatText(req, tt.self)
			if ok != tt.wantOK || text != tt.wantText {
				t.Errorf("got (%q,%v) want (%q,%v)", text, ok, tt.wantText, tt.wantOK)
			}
		})
	}
}

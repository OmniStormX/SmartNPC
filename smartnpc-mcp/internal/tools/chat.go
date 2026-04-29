package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// ChatSayInput drives the `chat_say` tool.
type ChatSayInput struct {
	Speaker string `json:"speaker"         jsonschema:"display name shown in the chat box, e.g. \"SmartNPC\""`
	Text    string `json:"text"            jsonschema:"message body, plain text"`
	Color   string `json:"color,omitempty" jsonschema:"optional color: white|yellow|green|red|cyan|blue|purple|gray (default yellow)"`
}

// ChatSayOutput is the structured success response.
type ChatSayOutput struct {
	OK bool `json:"ok"`
}

func registerChat(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "chat_say",
		Description: "Show a line in the in-game chat box (bottom-left). " +
			"Requires the StardewMCPBridge SMAPI mod with a save loaded.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in ChatSayInput) (*mcp.CallToolResult, ChatSayOutput, error) {
		if in.Speaker == "" || in.Text == "" {
			return nil, ChatSayOutput{}, fmt.Errorf("speaker and text are required")
		}
		raw, err := br.Call(ctx, bridge.ActionChatSay, in)
		if err != nil {
			return nil, ChatSayOutput{}, fmt.Errorf("chat_say: %w", err)
		}
		var out ChatSayOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})
}

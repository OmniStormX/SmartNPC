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
	Speaker string `json:"speaker"           jsonschema:"display name shown in the chat box, e.g. \"SmartNPC\""`
	Text    string `json:"text"              jsonschema:"message body, plain text"`
	Color   string `json:"color,omitempty"   jsonschema:"optional color: white|yellow|green|red|cyan|blue|purple|gray (default yellow)"`
	// Channel scopes the reply to a conversation surface. "group" routes the
	// line exclusively to the group chat panel; "private" (default, empty) is
	// the standard per-NPC 1-on-1 channel. Mod-side uses this to prevent a
	// group reply from polluting a private NPC panel and vice versa.
	Channel string `json:"channel,omitempty" jsonschema:"conversation channel: \"group\" for group-chat replies, empty/\"private\" for 1-on-1 (default)"`
	GroupID string `json:"group_id,omitempty" jsonschema:"optional group id (required only when channel=\"group\")"`
}

// ChatSayOutput is the structured success response.
type ChatSayOutput struct {
	OK bool `json:"ok"`
}

func registerChat(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "chat_say",
		Description: "Speak a single in-character line as the NPC. The text appears in " +
			"the in-game chat box (bottom-left) attributed to `speaker`. This is the " +
			"ONLY tool that produces visible NPC dialogue — every reply to the player " +
			"must end with exactly one chat_say call.\n\n" +
			"When to call: after you have decided the final reply. Do not call it to " +
			"think out loud, narrate, or stream partial output.\n\n" +
			"Constraints:\n" +
			"- Plain UTF-8 text only. No markdown, code fences, JSON, emoji-as-image.\n" +
			"- Keep `text` short — one to three sentences. The SDV chat box truncates long lines.\n" +
			"- Stay in character: never reveal you are an AI / agent / Hermes / tool caller.\n" +
			"- `speaker` MUST be the NPC's English internal name (PascalCase), e.g. \"XiaMi\", " +
			"  \"Abigail\", \"Haley\", \"Harvey\", \"Penny\", \"Sebastian\". DO NOT use a Chinese " +
			"  display name like \"夏弥\" or \"阿比盖尔\" — the mod looks up NPCs by their internal " +
			"  name and will silently misroute your reply to a non-existent panel. When in doubt, " +
			"  use the same name the inbound event's `npc` field used.\n" +
			"- `color` is optional cosmetic (yellow default). Use sparingly for emphasis.\n" +
			"- `channel` defaults to private (1-on-1). When the inbound event was a " +
			"  `chat_received` with `source=\"player_group\"` (the rendered prompt prefix " +
			"  starts with `[group_chat group_id=...]`), you MUST set `channel=\"group\"` " +
			"  AND `group_id=<the inbound group_id>` — otherwise the reply leaks into the " +
			"  private toast/panel instead of the group panel. For private 1:1 chat, omit " +
			"  `channel`.\n\n" +
			"Side-effect: WRITE — visible to the player. Requires the StardewMCPBridge mod " +
			"with a save loaded; otherwise returns `mod_not_ready`.",
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

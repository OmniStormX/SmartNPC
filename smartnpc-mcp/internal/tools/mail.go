package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// MailSendInput is the request payload for the `mail_send` tool.
type MailSendInput struct {
	Text string `json:"text" jsonschema:"message body to display in-game (HUD message)"`
}

// MailSendOutput reports whether the mod accepted and displayed the message.
type MailSendOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if the mod accepted the message"`
	Message string `json:"message,omitempty" jsonschema:"optional human-readable status from the mod"`
}

func registerMail(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "mail_send",
		Description: "Show a system HUD bubble in the top-right of the screen. This is a " +
			"game-system notification channel, not NPC dialogue — use it for meta " +
			"messages like quest reminders, status pings, or debug hints. For NPC speech, " +
			"use `chat_say` instead.\n\n" +
			"When to call: when you need to surface something to the player outside of " +
			"in-character conversation (e.g. \"New mail from Abigail\", debug echoes).\n\n" +
			"Constraints: plain UTF-8 text, one line, short. No markdown.\n\n" +
			"Side-effect: WRITE — visible to the player. Requires the SMAPI mod with a " +
			"save loaded; otherwise returns `mod_not_ready`.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in MailSendInput) (*mcp.CallToolResult, MailSendOutput, error) {
		if in.Text == "" {
			return nil, MailSendOutput{}, fmt.Errorf("text is required")
		}
		raw, err := br.Call(ctx, bridge.ActionMailSend, in)
		if err != nil {
			return nil, MailSendOutput{}, fmt.Errorf("mail_send: %w", err)
		}
		var out MailSendOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})
}

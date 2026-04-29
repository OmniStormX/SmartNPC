package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// MailSendInput is the request payload for the experimental `mail_send` tool.
//
// M2 scope: pop a HUD message in the running game. Future milestones may
// extend this with a real mailbox letter (addMailForTomorrow + Data/Mail
// asset injection), but the schema is deliberately small for now.
type MailSendInput struct {
	Text string `json:"text" jsonschema:"message body to display in-game (HUD message)"`
}

// MailSendOutput reports whether the mod accepted and displayed the message.
type MailSendOutput struct {
	OK      bool   `json:"ok"             jsonschema:"true if the mod accepted the message"`
	Message string `json:"message,omitempty" jsonschema:"optional human-readable status from the mod"`
}

// registerMail wires the mail_send tool. The bridge.Client is the HTTP shim
// to the SMAPI mod (see internal/bridge/http_client.go).
func registerMail(s *mcp.Server, br *bridge.Client) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "mail_send",
		Description: "Display a hello-style HUD message in the running Stardew Valley game. " +
			"Requires the StardewMCPBridge SMAPI mod to be loaded with a save active.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in MailSendInput) (*mcp.CallToolResult, MailSendOutput, error) {
		if in.Text == "" {
			return nil, MailSendOutput{}, fmt.Errorf("text is required")
		}
		var out MailSendOutput
		if err := br.PostJSON(ctx, "/mail_send", in, &out); err != nil {
			return nil, MailSendOutput{}, fmt.Errorf("call mod: %w", err)
		}
		return nil, out, nil
	})
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// PlayerGetStatusInput takes no parameters; the mod returns whatever
// player state it can read from Game1.
type PlayerGetStatusInput struct{}

// PlayerGetStatusOutput mirrors the mod-side payload. The five flags
// together tell the proactive scheduler whether it is polite to interrupt
// the player right now.
type PlayerGetStatusOutput struct {
	OK       bool   `json:"ok"        jsonschema:"true on success"`
	Busy     bool   `json:"busy"      jsonschema:"true when the player is otherwise engaged (composite of in_menu/in_event/cutscene)"`
	InMenu   bool   `json:"in_menu"   jsonschema:"true when an active clickable menu is open"`
	InEvent  bool   `json:"in_event"  jsonschema:"true when an in-game event/cutscene is running"`
	IsMoving bool   `json:"is_moving" jsonschema:"true when the player is currently walking/running"`
	Location string `json:"location"  jsonschema:"name of the in-game map the player is on"`
}

// registerPlayerQuery wires the player_get_status MCP tool. Bridged through
// the ws server to the SMAPI mod which reads Game1 state synchronously.
func registerPlayerQuery(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "player_get_status",
		Description: "Read the player's current availability — whether a menu is open, " +
			"a cutscene is running, the player is moving, plus the current map name. " +
			"Use this BEFORE initiating a proactive NPC interaction so you do not " +
			"interrupt the player mid-cutscene or mid-menu. Takes no parameters.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in PlayerGetStatusInput) (*mcp.CallToolResult, PlayerGetStatusOutput, error) {
		raw, err := br.Call(ctx, bridge.ActionPlayerGetStatus, in)
		if err != nil {
			return nil, PlayerGetStatusOutput{}, fmt.Errorf("player_get_status: %w", err)
		}
		var out PlayerGetStatusOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/smartnpc/smartnpc-mcp/internal/bridge"
)

// ── npc_summon ─────────────────────────────────────────────────

// NpcSummonInput asks the mod to warp the NPC to the map edge and walk toward
// the player. Used when the player says "come here" / "过来" without naming a
// landmark — the mod decides the exact spawn tile.
type NpcSummonInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name, e.g. \"XiaMi\""`
}

// NpcSummonOutput is the ack.
type NpcSummonOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if the summon request was accepted"`
	NPC     string `json:"npc"               jsonschema:"echo of the NPC name"`
	Message string `json:"message,omitempty" jsonschema:"optional status hint, e.g. \"warped\" or \"approaching\""`
}

// ── npc_follow_start ───────────────────────────────────────────

// NpcFollowStartInput begins a follow behavior.
type NpcFollowStartInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

// NpcFollowStartOutput is the ack.
type NpcFollowStartOutput struct {
	OK  bool   `json:"ok"  jsonschema:"true on success"`
	NPC string `json:"npc" jsonschema:"echo of the NPC name"`
}

// ── npc_follow_stop ────────────────────────────────────────────

// NpcFollowStopInput cancels an active follow behavior.
type NpcFollowStopInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

// NpcFollowStopOutput is the ack.
type NpcFollowStopOutput struct {
	OK  bool   `json:"ok"  jsonschema:"true on success"`
	NPC string `json:"npc" jsonschema:"echo of the NPC name"`
}

// ── npc_lead_to ────────────────────────────────────────────────

// NpcLeadToInput asks the NPC to walk ahead of the player toward a tile.
type NpcLeadToInput struct {
	NPC string `json:"npc"           jsonschema:"NPC internal name"`
	X   int    `json:"x"             jsonschema:"target tile X"`
	Y   int    `json:"y"             jsonschema:"target tile Y"`
	Map string `json:"map,omitempty" jsonschema:"target map (default: NPC's current map)"`
}

// NpcLeadToOutput reports the accepted destination.
type NpcLeadToOutput struct {
	OK  bool   `json:"ok"  jsonschema:"true if the lead request was accepted"`
	NPC string `json:"npc" jsonschema:"echo of the NPC name"`
	X   int    `json:"x"   jsonschema:"destination tile X"`
	Y   int    `json:"y"   jsonschema:"destination tile Y"`
	Map string `json:"map" jsonschema:"destination map actually used"`
}

// ── npc_get_behavior ───────────────────────────────────────────

// NpcGetBehaviorInput queries the current behavior mode.
type NpcGetBehaviorInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

// NpcGetBehaviorOutput returns the high-level mode string.
type NpcGetBehaviorOutput struct {
	OK   bool   `json:"ok"   jsonschema:"true on success"`
	NPC  string `json:"npc"  jsonschema:"echo of the NPC name"`
	Mode string `json:"mode" jsonschema:"one of: idle / summoning / following / leading"`
}

// ── registration ───────────────────────────────────────────────

func registerNpcBehavior(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_summon",
		Description: "Summon an NPC to walk toward the player. The NPC warps to the map edge then pathfinds to the player. " +
			"Use this when the player says \"come here\" / \"过来\" without naming a specific landmark — the mod picks a " +
			"reasonable arrival tile. Returns once the summon request is queued; the NPC walks asynchronously.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcSummonInput) (*mcp.CallToolResult, NpcSummonOutput, error) {
		if in.NPC == "" {
			return nil, NpcSummonOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcSummon, in)
		if err != nil {
			return nil, NpcSummonOutput{}, fmt.Errorf("npc_summon: %w", err)
		}
		var out NpcSummonOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_follow_start",
		Description: "Make an NPC follow the player. The NPC stays ~2 tiles behind, following across map transitions. " +
			"Call `npc_follow_stop` to cancel. Only one follow behavior is active at a time per NPC; calling this again " +
			"refreshes the target. Safe to call while the NPC is idle, summoning, or leading — the mod cancels the prior mode.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcFollowStartInput) (*mcp.CallToolResult, NpcFollowStartOutput, error) {
		if in.NPC == "" {
			return nil, NpcFollowStartOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcFollowStart, in)
		if err != nil {
			return nil, NpcFollowStartOutput{}, fmt.Errorf("npc_follow_start: %w", err)
		}
		var out NpcFollowStartOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_follow_stop",
		Description: "Stop an NPC from following the player. Idempotent — calling it when the NPC is not following " +
			"simply returns ok=true. Use when the player says \"stop following\" / \"别跟了\" / \"停下\".",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcFollowStopInput) (*mcp.CallToolResult, NpcFollowStopOutput, error) {
		if in.NPC == "" {
			return nil, NpcFollowStopOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcFollowStop, in)
		if err != nil {
			return nil, NpcFollowStopOutput{}, fmt.Errorf("npc_follow_stop: %w", err)
		}
		var out NpcFollowStopOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_lead_to",
		Description: "Ask an NPC to lead the way to a destination. The NPC walks ahead of the player toward the target " +
			"tile, pausing when the player falls too far behind. Unlike `npc_move_to`, this mode actively coordinates with " +
			"the player's position. `map` is optional; defaults to the NPC's current map.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcLeadToInput) (*mcp.CallToolResult, NpcLeadToOutput, error) {
		if in.NPC == "" {
			return nil, NpcLeadToOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcLeadTo, in)
		if err != nil {
			return nil, NpcLeadToOutput{}, fmt.Errorf("npc_lead_to: %w", err)
		}
		var out NpcLeadToOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_get_behavior",
		Description: "Query an NPC's current behavior mode. Returns one of: `idle`, `summoning`, `following`, `leading`. " +
			"Use this to check whether a prior behavior command is still active before issuing a new one, or to describe " +
			"the NPC's current state in dialogue.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcGetBehaviorInput) (*mcp.CallToolResult, NpcGetBehaviorOutput, error) {
		if in.NPC == "" {
			return nil, NpcGetBehaviorOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcGetBehavior, in)
		if err != nil {
			return nil, NpcGetBehaviorOutput{}, fmt.Errorf("npc_get_behavior: %w", err)
		}
		var out NpcGetBehaviorOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})
}

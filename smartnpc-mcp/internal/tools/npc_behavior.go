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

// ── npc_emote ──────────────────────────────────────────────────

// NpcEmoteInput asks the mod to show a Stardew-native emote bubble above the
// NPC's head. Cosmetic only; the bubble fades on its own after ~1 second.
type NpcEmoteInput struct {
	NPC  string `json:"npc"  jsonschema:"NPC internal name, e.g. \"XiaMi\""`
	Kind string `json:"kind,omitempty" jsonschema:"emote kind — one of: exclamation, question, heart, sleep, happy, sad, angry, music, sparkle (default), pause"`
}

// NpcEmoteOutput is the ack.
type NpcEmoteOutput struct {
	OK   bool   `json:"ok"             jsonschema:"true if the emote was queued"`
	NPC  string `json:"npc"            jsonschema:"echo of the NPC name"`
	Kind string `json:"kind,omitempty" jsonschema:"echo of the emote kind that was actually used (server may substitute on unknown kinds)"`
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
		Description: "Make an NPC come to the player. The NPC warps to the nearest map " +
			"edge then pathfinds to the player's current tile. Cancels any active " +
			"follow/lead behavior.\n\n" +
			"When to call: the player says \"come here\" / \"过来\" / \"到我这来\" without " +
			"naming a specific landmark — the mod picks the arrival tile.\n\n" +
			"Side-effect: WRITE (high-impact — visibly teleports + moves a character). " +
			"Use only on explicit request. Errors: `unknown_npc`.",
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
		Name: "npc_emote",
		Description: "Show a Stardew-Valley emote bubble above the NPC's head. " +
			"Uses the game's native Character.doEmote API — the bubble appears for " +
			"about 1 second then fades on its own. Does not move the NPC, does not " +
			"send a chat line, does not block subsequent tool calls.\n\n" +
			"When to call: as a visual flourish accompanying a chat or action — " +
			"`sparkle` when an NPC drops in proactively (paired with `npc_summon` + " +
			"`chat_say`), `heart` after a positive interaction, `question` when " +
			"the NPC reacts to something surprising. Skip when there is nothing " +
			"to telegraph — the bubble is cheap but not free.\n\n" +
			"Valid `kind` values (SDV native): `exclamation`, `question`, `heart`, " +
			"`sleep`, `happy`, `sad`, `angry`, `music`, `sparkle`, `pause`. Unknown " +
			"values fall back to `sparkle` and the server logs a warning.\n\n" +
			"Side-effect: WRITE (visual only — cosmetic, no game-state mutation). " +
			"Errors: `unknown_npc`, `mod_not_ready` (no save loaded).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcEmoteInput) (*mcp.CallToolResult, NpcEmoteOutput, error) {
		if in.NPC == "" {
			return nil, NpcEmoteOutput{}, fmt.Errorf("npc is required")
		}
		if in.Kind == "" {
			in.Kind = "sparkle"
		}
		raw, err := br.Call(ctx, bridge.ActionNpcEmote, in)
		if err != nil {
			return nil, NpcEmoteOutput{}, fmt.Errorf("npc_emote: %w", err)
		}
		var out NpcEmoteOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_follow_start",
		Description: "Begin an NPC follow behavior. The NPC stays ~2 tiles behind the " +
			"player, crossing map transitions. Only one follow is active per NPC; calling " +
			"again refreshes the target. Calling during summon/lead cancels the prior mode.\n\n" +
			"When to call: player says \"follow me\" / \"跟我来\" / \"跟着我走\". Also for " +
			"tutorial escorts where the NPC tags along.\n\n" +
			"Side-effect: WRITE (ongoing — runs until `npc_follow_stop` or new behavior). " +
			"Errors: `unknown_npc`.",
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
		Description: "Stop an NPC from following the player. Idempotent — returns " +
			"`ok=true` even if the NPC was not following.\n\n" +
			"When to call: player says \"stop following\" / \"别跟了\" / \"停下\" / " +
			"\"我要一个人待会\". Also before triggering a scene that needs the NPC " +
			"stationary.\n\n" +
			"Side-effect: WRITE (cancels ongoing follow). Errors: `unknown_npc`.",
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
		Description: "Ask an NPC to lead the player to a destination tile. The NPC walks " +
			"ahead, pauses when the player falls behind, and resumes when they catch up. " +
			"`map` defaults to the NPC's current map.\n\n" +
			"When to call: player says \"带我去 X\" / \"take me to Y\" / \"show me the way\". " +
			"Choose this over `npc_move_to` when the player is expected to follow the NPC.\n\n" +
			"Side-effect: WRITE (ongoing — coordinates with player position). Errors: " +
			"`unknown_npc`, `unknown_map`, `pathfind_error`.",
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
		Description: "Query an NPC's current high-level behavior mode: `idle`, " +
			"`summoning`, `following`, or `leading`.\n\n" +
			"When to call: check if a prior behavior command is still in flight before " +
			"issuing a new one, or describe the NPC's state in dialogue (\"我已经在跟着你了\" / " +
			"\"I'm right behind you\").\n\n" +
			"Side-effect: READ. Requires a loaded save.",
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

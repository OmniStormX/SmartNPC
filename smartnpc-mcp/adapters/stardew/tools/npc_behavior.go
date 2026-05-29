package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
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

// ── npc_give_item ──────────────────────────────────────────────

// NpcGiveItemInput asks the mod to drop a Stardew item into the player's
// inventory, in-character as if the NPC handed it over. Used for the
// "signature gift" interaction (e.g. XiaMi handing the player a Cola,
// Abigail offering an amethyst). The set of items each NPC is willing to
// give is established in that NPC's SOUL.md; this tool does not gate.
type NpcGiveItemInput struct {
	NPC    string `json:"npc"               jsonschema:"NPC internal name, e.g. \"XiaMi\""`
	ItemID string `json:"item_id"           jsonschema:"SDV qualified item id, e.g. \"(O)167\" for Joja Cola, \"(O)66\" for Amethyst. See the NPC's SOUL.md \"Signature gift items\" list for valid choices."`
	Count  int    `json:"count,omitempty"   jsonschema:"how many to give (default 1, max 5)"`
}

// NpcGiveItemOutput is the ack with whatever the mod actually placed in
// the inventory.
type NpcGiveItemOutput struct {
	OK      bool   `json:"ok"                 jsonschema:"true if the item landed in the player's inventory"`
	NPC     string `json:"npc"                jsonschema:"echo of the NPC name"`
	ItemID  string `json:"item_id,omitempty"  jsonschema:"echo of the resolved qualified item id"`
	Count   int    `json:"count,omitempty"    jsonschema:"how many actually added (may be less than requested if inventory partially filled)"`
	Message string `json:"message,omitempty"  jsonschema:"optional human-readable status, e.g. \"inventory_full\""`
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
		logToolCall("npc_summon", in)
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
		logToolCall("npc_emote", in)
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
		Name: "npc_give_item",
		Description: "Place a Stardew item directly into the player's inventory, " +
			"in-character as if the NPC handed it over.\n\n" +
			"When to call: ONLY when the player has clearly asked to receive a " +
			"specific item from the NPC — \"给我一杯可乐\" / \"分我一颗紫水晶\" / " +
			"\"我能买一块面包吗\" / \"请给我点药\". Both \"索取\" (free) and \"购买\" " +
			"(payment-context) phrasings count; this tool does NOT charge gold today.\n\n" +
			"Constraints:\n" +
			"- The item MUST be on the NPC's \"Signature gift items\" list in their " +
			"SOUL.md. If the player asks for something not on that list, refuse in " +
			"character (\"我这没有那个，下次帮你留意\") and do NOT call this tool.\n" +
			"- Quote the SDV qualified item id (e.g. \"(O)167\" Joja Cola, \"(O)66\" " +
			"Amethyst) verbatim from your SOUL.md — do NOT invent ids.\n" +
			"- One call per request. If the player wants more, they ask again next turn.\n" +
			"- Default count is 1; max is 5. Don't be generous past that — the gift " +
			"should feel personal.\n" +
			"- Always pair with a short `chat_say` line acknowledging the handover " +
			"(\"给，慢慢喝。\" / \"喏。\" / \"小心，别让它碎了。\") so the player has " +
			"narrative confirmation, not just an inventory pop.\n\n" +
			"Side-effect: WRITE (adds to inventory, possibly fails if full). " +
			"Errors: `unknown_npc`, `unknown_item` (qualified id not recognized by " +
			"SDV's ItemRegistry), `inventory_full` (no slot available), " +
			"`mod_not_ready`.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcGiveItemInput) (*mcp.CallToolResult, NpcGiveItemOutput, error) {
		if in.NPC == "" {
			return nil, NpcGiveItemOutput{}, fmt.Errorf("npc is required")
		}
		if in.ItemID == "" {
			return nil, NpcGiveItemOutput{}, fmt.Errorf("item_id is required")
		}
		if in.Count <= 0 {
			in.Count = 1
		}
		if in.Count > 5 {
			in.Count = 5
		}
		logToolCall("npc_give_item", in)
		raw, err := br.Call(ctx, bridge.ActionNpcGiveItem, in)
		if err != nil {
			return nil, NpcGiveItemOutput{}, fmt.Errorf("npc_give_item: %w", err)
		}
		var out NpcGiveItemOutput
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
		logToolCall("npc_follow_start", in)
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
		logToolCall("npc_follow_stop", in)
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
		logToolCall("npc_lead_to", in)
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
		logToolCall("npc_get_behavior", in)
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

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/internal/bridge"
)

// ── npc_move_to ────────────────────────────────────────────────

// NpcMoveToInput asks the mod to pathfind an NPC to a tile destination.
type NpcMoveToInput struct {
	NPC string `json:"npc"           jsonschema:"NPC internal name, e.g. \"XiaMi\""`
	X   int    `json:"x"             jsonschema:"target tile X coordinate"`
	Y   int    `json:"y"             jsonschema:"target tile Y coordinate"`
	Map string `json:"map,omitempty" jsonschema:"target map name (default: NPC's current map)"`
}

// NpcMoveToOutput reports whether the path request was accepted.
type NpcMoveToOutput struct {
	OK      bool   `json:"ok"               jsonschema:"true if the path request was accepted"`
	NPC     string `json:"npc"              jsonschema:"echo of the NPC name"`
	Map     string `json:"map"              jsonschema:"destination map actually used"`
	X       int    `json:"x"                jsonschema:"destination tile X"`
	Y       int    `json:"y"                jsonschema:"destination tile Y"`
	Message string `json:"message,omitempty" jsonschema:"optional status hint, e.g. \"pathing\" or \"no_route\""`
}

// ── npc_face_direction ─────────────────────────────────────────

// NpcFaceDirectionInput sets an NPC's facing direction.
type NpcFaceDirectionInput struct {
	NPC       string `json:"npc"       jsonschema:"NPC internal name"`
	Direction string `json:"direction" jsonschema:"one of: up, down, left, right"`
}

// NpcFaceDirectionOutput is the ack.
type NpcFaceDirectionOutput struct {
	OK        bool   `json:"ok"        jsonschema:"true on success"`
	NPC       string `json:"npc"       jsonschema:"echo of NPC name"`
	Direction string `json:"direction" jsonschema:"applied direction (up/down/left/right)"`
	Facing    int    `json:"facing"    jsonschema:"SDV facing int 0=up 1=right 2=down 3=left"`
}

// ── npc_get_position ───────────────────────────────────────────

// NpcGetPositionInput queries an NPC's current tile position + facing.
type NpcGetPositionInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

// NpcGetPositionOutput reports where the NPC is right now.
type NpcGetPositionOutput struct {
	OK        bool    `json:"ok"        jsonschema:"true on success"`
	NPC       string  `json:"npc"       jsonschema:"echo of NPC name"`
	X         float64 `json:"x"         jsonschema:"tile X (may be fractional while walking)"`
	Y         float64 `json:"y"         jsonschema:"tile Y (may be fractional while walking)"`
	Map       string  `json:"map"       jsonschema:"current map name, e.g. \"Farm\""`
	Facing    int     `json:"facing"    jsonschema:"SDV facing int 0=up 1=right 2=down 3=left"`
	Direction string  `json:"direction" jsonschema:"facing as a word: up/down/left/right"`
	IsMoving  bool    `json:"is_moving" jsonschema:"true if NPC is currently following a path"`
}

// ── npc_get_named_locations ────────────────────────────────────

// NpcGetNamedLocationsInput currently takes no fields; exists so the MCP
// SDK generates a well-formed schema.
type NpcGetNamedLocationsInput struct{}

// NamedLocationEntry is one row in the location table returned to the LLM.
type NamedLocationEntry struct {
	Name    string   `json:"name"               jsonschema:"canonical display name, e.g. \"房子前面\""`
	Aliases []string `json:"aliases"            jsonschema:"alternative names the player might say"`
	Map     string   `json:"map"                jsonschema:"SDV map name, defaults to \"Farm\""`
	X       int      `json:"x"                  jsonschema:"tile X coordinate"`
	Y       int      `json:"y"                  jsonschema:"tile Y coordinate"`
}

// NpcGetNamedLocationsOutput is the static list of known landmarks.
type NpcGetNamedLocationsOutput struct {
	OK        bool                 `json:"ok"`
	Locations []NamedLocationEntry `json:"locations"`
}

// defaultNamedLocations is the Farm landmark set the move-intent parser uses.
var defaultNamedLocations = []NamedLocationEntry{
	{Name: "农场左边", Aliases: []string{"农场左边", "农场西边", "左边", "left", "west"}, Map: "Farm", X: 10, Y: 15},
	{Name: "房子前面", Aliases: []string{"房子前面", "房子", "家门口", "门口", "house", "door", "farmhouse"}, Map: "Farm", X: 64, Y: 16},
	{Name: "湖边", Aliases: []string{"湖边", "池塘", "小湖", "lake", "pond"}, Map: "Farm", X: 45, Y: 30},
	{Name: "大门", Aliases: []string{"大门", "入口", "农场入口", "门", "gate", "entrance"}, Map: "Farm", X: 79, Y: 15},
	{Name: "温室", Aliases: []string{"温室", "暖房", "greenhouse"}, Map: "Farm", X: 28, Y: 12},
	{Name: "畜棚", Aliases: []string{"畜棚", "牛棚", "barn"}, Map: "Farm", X: 77, Y: 10},
	{Name: "鸡舍", Aliases: []string{"鸡舍", "鸡窝", "coop"}, Map: "Farm", X: 72, Y: 10},
	{Name: "农场中心", Aliases: []string{"农场中心", "中心", "中间", "center", "middle"}, Map: "Farm", X: 64, Y: 20},
}

// validDirections lists the accepted direction strings.
var validDirections = map[string]bool{
	"up":    true,
	"down":  true,
	"left":  true,
	"right": true,
}

// ── registration ───────────────────────────────────────────────

func registerNpcMovement(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_move_to",
		Description: "Pathfind an NPC to a target tile using the game's PathFindController " +
			"(respects walls, doors, terrain). If `map` differs from the NPC's current map, " +
			"the NPC warps — cross-map pathing is deferred to a later milestone. Returns " +
			"immediately; the NPC walks asynchronously.\n\n" +
			"When to call: stage an arrival before a scene, reposition after a scheduled " +
			"event, or when the player says \"go to X\". For human-friendly destinations " +
			"like \"湖边\" / \"大门\", first call `npc_get_named_locations` to resolve the tile.\n\n" +
			"Do NOT call this during casual dialogue just to show off — it's a high-impact " +
			"write that visibly moves the character across the map.\n\n" +
			"Side-effect: WRITE — moves a character. Requires a loaded save. Errors: " +
			"`unknown_npc`, `unknown_map`, `pathfind_error`.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcMoveToInput) (*mcp.CallToolResult, NpcMoveToOutput, error) {
		if in.NPC == "" {
			return nil, NpcMoveToOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcMoveTo, in)
		if err != nil {
			return nil, NpcMoveToOutput{}, fmt.Errorf("npc_move_to: %w", err)
		}
		var out NpcMoveToOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_face_direction",
		Description: "Turn an NPC to face one of four cardinal directions " +
			"(up/down/left/right). Equivalent to `NPC.faceDirection(int)`; does not " +
			"cancel any active PathFindController.\n\n" +
			"When to call: reaction beats — face the player before speaking, glance at " +
			"an object mentioned in dialogue, turn away when sulking.\n\n" +
			"Side-effect: WRITE (low-impact visual). Safe during idle or movement. " +
			"Errors: `invalid_params` if direction is not one of up/down/left/right.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcFaceDirectionInput) (*mcp.CallToolResult, NpcFaceDirectionOutput, error) {
		if in.NPC == "" {
			return nil, NpcFaceDirectionOutput{}, fmt.Errorf("npc is required")
		}
		dir := strings.ToLower(in.Direction)
		if !validDirections[dir] {
			return nil, NpcFaceDirectionOutput{}, fmt.Errorf("direction must be one of up/down/left/right, got %q", in.Direction)
		}
		in.Direction = dir
		raw, err := br.Call(ctx, bridge.ActionNpcFaceDirection, in)
		if err != nil {
			return nil, NpcFaceDirectionOutput{}, fmt.Errorf("npc_face_direction: %w", err)
		}
		var out NpcFaceDirectionOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_get_position",
		Description: "Read an NPC's current tile coordinates, map, and facing. " +
			"Coordinates can be fractional while the NPC is mid-step; `is_moving` is " +
			"true when a PathFindController is active.\n\n" +
			"When to call: verify that a prior `npc_move_to` or `npc_summon` actually " +
			"arrived, or compute distance to the player before triggering a scene.\n\n" +
			"Side-effect: READ. Requires a loaded save.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcGetPositionInput) (*mcp.CallToolResult, NpcGetPositionOutput, error) {
		if in.NPC == "" {
			return nil, NpcGetPositionOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcGetPosition, in)
		if err != nil {
			return nil, NpcGetPositionOutput{}, fmt.Errorf("npc_get_position: %w", err)
		}
		var out NpcGetPositionOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_get_named_locations",
		Description: "Return the table of human-addressable landmarks on the Farm " +
			"(e.g. \"房子前面\", \"湖边\", \"大门\"). Each entry has a canonical name, " +
			"aliases, target map, and exact tile coordinates.\n\n" +
			"When to call: when the player asks \"where can you go\" / \"你能去哪\", or " +
			"when a player destination is fuzzy (\"过来\" / \"来这边\") and you need to " +
			"disambiguate before issuing `npc_move_to`.\n\n" +
			"Side-effect: READ. Static data — takes no parameters, always available " +
			"even without a loaded save.",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ NpcGetNamedLocationsInput) (*mcp.CallToolResult, NpcGetNamedLocationsOutput, error) {
		out := NpcGetNamedLocationsOutput{
			OK:        true,
			Locations: append([]NamedLocationEntry(nil), defaultNamedLocations...),
		}
		return nil, out, nil
	})
}

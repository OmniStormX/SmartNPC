package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/internal/bridge"
)

// ── npc_get_nearby ─────────────────────────────────────────────

// NpcGetNearbyInput describes the perception query for an NPC's surroundings.
type NpcGetNearbyInput struct {
	NPC    string  `json:"npc"               jsonschema:"NPC internal name, e.g. \"XiaMi\""`
	Radius float64 `json:"radius,omitempty"  jsonschema:"visibility radius in tiles (default 10)"`
}

// PerceptionEntity is one entry inside the nearby list.
type PerceptionEntity struct {
	Name     string  `json:"name"               jsonschema:"character name (internal for NPCs, farmer name for players)"`
	Type     string  `json:"type"               jsonschema:"\"npc\" or \"player\""`
	X        float64 `json:"x"                  jsonschema:"tile X coordinate"`
	Y        float64 `json:"y"                  jsonschema:"tile Y coordinate"`
	Distance float64 `json:"distance"           jsonschema:"Euclidean distance in tiles from the observer NPC"`
	Facing   int     `json:"facing"             jsonschema:"facing direction 0=up 1=right 2=down 3=left (-1 if unknown)"`
	Map      string  `json:"map,omitempty"      jsonschema:"map name the entity is on (usually same as observer)"`
	Action   string  `json:"action,omitempty"   jsonschema:"current action / animation hint, best-effort"`
}

// NpcGetNearbyOutput is the full response.
type NpcGetNearbyOutput struct {
	OK      bool               `json:"ok"                jsonschema:"true on success"`
	NPC     string             `json:"npc"               jsonschema:"observer NPC name"`
	Map     string             `json:"map"               jsonschema:"observer NPC's current map"`
	Radius  float64            `json:"radius"            jsonschema:"radius actually used for the scan"`
	Count   int                `json:"count"             jsonschema:"number of entities returned"`
	Nearby  []PerceptionEntity `json:"nearby"            jsonschema:"list of entities inside the visibility radius (sorted by distance)"`
}

// ── npc_get_environment ────────────────────────────────────────

// NpcGetEnvironmentInput describes the env query for an NPC.
type NpcGetEnvironmentInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

// EnvObject is a nearby object (furniture / crop / terrain feature).
type EnvObject struct {
	Name     string  `json:"name"            jsonschema:"object display or internal name"`
	Category string  `json:"category"        jsonschema:"object/terrain/furniture/crop/building"`
	X        float64 `json:"x"               jsonschema:"tile X"`
	Y        float64 `json:"y"               jsonschema:"tile Y"`
	Distance float64 `json:"distance"        jsonschema:"tiles from observer"`
}

// NpcGetEnvironmentOutput is the env summary.
type NpcGetEnvironmentOutput struct {
	OK            bool        `json:"ok"              jsonschema:"true on success"`
	NPC           string      `json:"npc"             jsonschema:"observer NPC name"`
	Map           string      `json:"map"             jsonschema:"current map name, e.g. \"Farm\""`
	X             float64     `json:"x"               jsonschema:"observer tile X"`
	Y             float64     `json:"y"               jsonschema:"observer tile Y"`
	Facing        int         `json:"facing"          jsonschema:"observer facing direction"`
	TimeOfDay     int         `json:"time_of_day"     jsonschema:"raw SDV time-of-day, e.g. 1430"`
	Hour          int         `json:"hour"            jsonschema:"0-23"`
	Minute        int         `json:"minute"          jsonschema:"0-59"`
	Season        string      `json:"season"          jsonschema:"spring/summer/fall/winter"`
	Weather       string      `json:"weather"         jsonschema:"sunny/rainy/snowy/stormy"`
	NearbyObjects []EnvObject `json:"nearby_objects"  jsonschema:"salient environment features near the NPC"`
}

// ── registration ───────────────────────────────────────────────

func registerNpcPerception(s *mcp.Server, br *bridge.WSClient) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_get_nearby",
		Description: "Scan the NPC's current map for other characters (NPCs and players) " +
			"inside the visibility radius (default 10 tiles). Returns entries sorted by " +
			"distance with name, type, tile coords, distance, and facing direction.\n\n" +
			"When to call: BEFORE reacting to the environment — e.g. greet a passing " +
			"villager by name, notice the player approaching, or avoid speaking when alone. " +
			"Also useful before `npc_send_message` to confirm another NPC is actually in " +
			"earshot before simulating inter-NPC dialogue.\n\n" +
			"Side-effect: READ. Cached ~1 Hz on the mod side; custom `radius` triggers " +
			"an on-demand scan. Requires a loaded save.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcGetNearbyInput) (*mcp.CallToolResult, NpcGetNearbyOutput, error) {
		if in.NPC == "" {
			return nil, NpcGetNearbyOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcGetNearby, in)
		if err != nil {
			return nil, NpcGetNearbyOutput{}, fmt.Errorf("npc_get_nearby: %w", err)
		}
		var out NpcGetNearbyOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_get_environment",
		Description: "Read a bundle of environmental context for the NPC in one call: " +
			"current map, tile position, facing, clock (time-of-day/hour/minute), season, " +
			"weather, and a short list of salient nearby objects (crops, furniture, terrain).\n\n" +
			"When to call: for situational dialogue (\"it's raining, maybe stay inside\", " +
			"\"the crops look ready\") when you want one round-trip instead of calling " +
			"`game_get_time` + `game_get_weather` + `npc_get_position` separately.\n\n" +
			"Side-effect: READ. Requires a loaded save.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcGetEnvironmentInput) (*mcp.CallToolResult, NpcGetEnvironmentOutput, error) {
		if in.NPC == "" {
			return nil, NpcGetEnvironmentOutput{}, fmt.Errorf("npc is required")
		}
		raw, err := br.Call(ctx, bridge.ActionNpcGetEnvironment, in)
		if err != nil {
			return nil, NpcGetEnvironmentOutput{}, fmt.Errorf("npc_get_environment: %w", err)
		}
		var out NpcGetEnvironmentOutput
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &out)
		}
		return nil, out, nil
	})
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/OmniStormX/SmartNPC/adapters/stardew/bridge"
)

// ── npc_wander ────────────────────────────────────────────────────

type NpcWanderInput struct {
	NPC           string `json:"npc"                    jsonschema:"NPC internal name, e.g. \"XiaMi\""`
	Location      string `json:"location,omitempty"     jsonschema:"map name to wander in (default: NPC's current map)"`
	DurationTicks int    `json:"duration_ticks,omitempty" jsonschema:"how many game ticks to wander (default: 300 ~5s)"`
}

type NpcWanderOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status hint"`
}

// ── npc_clear_debris ──────────────────────────────────────────────

type NpcClearDebrisInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	Radius   int    `json:"radius,omitempty"  jsonschema:"tile radius to scan (default 5, max 10)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max items to clear (default 3, max 10)"`
}

type NpcClearDebrisOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Cleared int    `json:"cleared,omitempty" jsonschema:"number of debris cleared"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_water_crops ───────────────────────────────────────────────

type NpcWaterCropsInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	Radius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 10)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max crops to water (default 5, max 20)"`
}

type NpcWaterCropsOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Watered int    `json:"watered,omitempty" jsonschema:"crops watered"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_harvest_crops ─────────────────────────────────────────────

type NpcHarvestCropsInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	Radius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 10)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max crops to harvest (default 5, max 10)"`
}

type NpcHarvestCropsOutput struct {
	OK        bool   `json:"ok"                  jsonschema:"true if accepted"`
	NPC       string `json:"npc"                 jsonschema:"echo"`
	Harvested int    `json:"harvested,omitempty" jsonschema:"crops harvested"`
	Message   string `json:"message,omitempty"   jsonschema:"status"`
}

// ── npc_deposit_items ─────────────────────────────────────────────

type NpcDepositItemsInput struct {
	NPC    string `json:"npc"              jsonschema:"NPC internal name"`
	ChestX int    `json:"chest_x"          jsonschema:"chest tile X"`
	ChestY int    `json:"chest_y"          jsonschema:"chest tile Y"`
	Map    string `json:"map,omitempty"    jsonschema:"map (default: NPC's current map)"`
}

type NpcDepositItemsOutput struct {
	OK        bool   `json:"ok"                  jsonschema:"true if accepted"`
	NPC       string `json:"npc"                 jsonschema:"echo"`
	Deposited int    `json:"deposited,omitempty" jsonschema:"items deposited"`
	Message   string `json:"message,omitempty"   jsonschema:"status"`
}

// ── npc_deliver_items ─────────────────────────────────────────────

type NpcDeliverItemsInput struct {
	NPC string `json:"npc" jsonschema:"NPC internal name"`
}

type NpcDeliverItemsOutput struct {
	OK        bool   `json:"ok"                  jsonschema:"true if accepted"`
	NPC       string `json:"npc"                 jsonschema:"echo"`
	Delivered int    `json:"delivered,omitempty" jsonschema:"items delivered to player"`
	Message   string `json:"message,omitempty"   jsonschema:"status"`
}

// ── npc_forage_collect ────────────────────────────────────────────

type NpcForageCollectInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	Radius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 8, max 15)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max items to collect (default 3, max 10)"`
}

type NpcForageCollectOutput struct {
	OK        bool   `json:"ok"                  jsonschema:"true if accepted"`
	NPC       string `json:"npc"                 jsonschema:"echo"`
	Collected int    `json:"collected,omitempty" jsonschema:"items collected"`
	Message   string `json:"message,omitempty"   jsonschema:"status"`
}

// ── npc_pet_animal ────────────────────────────────────────────────

type NpcPetAnimalInput struct {
	NPC        string `json:"npc"                   jsonschema:"NPC internal name"`
	AnimalName string `json:"animal_name,omitempty" jsonschema:"specific animal name (default: nearest un-petted)"`
}

type NpcPetAnimalOutput struct {
	OK         bool   `json:"ok"                    jsonschema:"true if accepted"`
	NPC        string `json:"npc"                   jsonschema:"echo"`
	AnimalName string `json:"animal_name,omitempty" jsonschema:"animal actually petted"`
	Message    string `json:"message,omitempty"     jsonschema:"status"`
}

// ── npc_plant_seeds ───────────────────────────────────────────────

type NpcPlantSeedsInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	SeedID   string `json:"seed_id"           jsonschema:"SDV qualified seed item id, e.g. \"(O)472\" for parsnip seeds"`
	Radius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 10)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max seeds to plant (default 5, max 10)"`
}

type NpcPlantSeedsOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Planted int    `json:"planted,omitempty" jsonschema:"seeds planted"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_till_soil ─────────────────────────────────────────────────

type NpcTillSoilInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	Radius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 3, max 8)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max tiles to till (default 5, max 15)"`
}

type NpcTillSoilOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Tilled  int    `json:"tilled,omitempty"  jsonschema:"tiles tilled"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── npc_inspect_object ────────────────────────────────────────────

type NpcInspectObjectInput struct {
	NPC string `json:"npc"            jsonschema:"NPC internal name"`
	X   int    `json:"x"              jsonschema:"target tile X"`
	Y   int    `json:"y"              jsonschema:"target tile Y"`
	Map string `json:"map,omitempty"  jsonschema:"map (default: NPC's current map)"`
}

type NpcInspectObjectOutput struct {
	OK          bool   `json:"ok"                     jsonschema:"true if accepted"`
	NPC         string `json:"npc"                    jsonschema:"echo"`
	ObjectName  string `json:"object_name,omitempty"  jsonschema:"name of the object/crop/terrain at target"`
	Description string `json:"description,omitempty"  jsonschema:"human-readable state description"`
	Message     string `json:"message,omitempty"      jsonschema:"status"`
}

// ── npc_place_object ──────────────────────────────────────────────

type NpcPlaceObjectInput struct {
	NPC    string `json:"npc"             jsonschema:"NPC internal name"`
	ItemID string `json:"item_id"         jsonschema:"SDV qualified item id to place"`
	X      int    `json:"x"              jsonschema:"target tile X"`
	Y      int    `json:"y"              jsonschema:"target tile Y"`
	Map    string `json:"map,omitempty"  jsonschema:"map (default: NPC's current map)"`
}

type NpcPlaceObjectOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Message string `json:"message,omitempty" jsonschema:"status"`
}

// ── registration ──────────────────────────────────────────────────

//nolint:unused
var _ = bridge.ActionNpcWander // ensure import is used

// callBridge forwards a stub-tool call to the Mod via WebSocket and
// unmarshals the response into out. Centralized so all 12 world-action
// stubs share the same forwarding plumbing.
func callBridge[Out any](ctx context.Context, br *bridge.WSClient, action string, in any, label string) (Out, error) {
	var out Out
	raw, err := br.Call(ctx, action, in)
	if err != nil {
		return out, fmt.Errorf("%s: %w", label, err)
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}

func registerNpcWorldAction(s *mcp.Server, br *bridge.WSClient) {

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_wander",
		Description: "Make an NPC wander randomly on their current map. The NPC picks " +
			"random passable tiles and pathfinds between them for the given duration.\n\n" +
			"When to call: NPC is idle and you want natural roaming behavior — e.g. " +
			"during downtime, after finishing a task, or to simulate casual exploration.\n\n" +
			"Side-effect: WRITE (moves character). Ongoing for duration_ticks.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcWanderInput) (*mcp.CallToolResult, NpcWanderOutput, error) {
		if in.NPC == "" {
			return nil, NpcWanderOutput{}, errNpcRequired
		}
		logToolCall("npc_wander", in)
		out, err := callBridge[NpcWanderOutput](ctx, br, bridge.ActionNpcWander, in, "npc_wander")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_clear_debris",
		Description: "NPC clears debris (weeds, twigs, stones) around their position.\n\n" +
			"When to call: NPC decides to help tidy the farm — typically morning routine " +
			"or when the player asks for help cleaning.\n\n" +
			"Constraints: radius max 10, max_count max 10. Only works on Farm-type maps.\n\n" +
			"Side-effect: WRITE (removes objects from the world).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcClearDebrisInput) (*mcp.CallToolResult, NpcClearDebrisOutput, error) {
		if in.NPC == "" {
			return nil, NpcClearDebrisOutput{}, errNpcRequired
		}
		logToolCall("npc_clear_debris", in)
		out, err := callBridge[NpcClearDebrisOutput](ctx, br, bridge.ActionNpcClearDebris, in, "npc_clear_debris")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_water_crops",
		Description: "NPC waters un-watered crops within radius.\n\n" +
			"When to call: morning farm routine, or player explicitly asks NPC to water.\n\n" +
			"Constraints: radius max 10, max_count max 20.\n\n" +
			"Side-effect: WRITE (modifies HoeDirt state).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcWaterCropsInput) (*mcp.CallToolResult, NpcWaterCropsOutput, error) {
		if in.NPC == "" {
			return nil, NpcWaterCropsOutput{}, errNpcRequired
		}
		logToolCall("npc_water_crops", in)
		out, err := callBridge[NpcWaterCropsOutput](ctx, br, bridge.ActionNpcWaterCrops, in, "npc_water_crops")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_harvest_crops",
		Description: "NPC harvests mature crops within radius and stores them in internal inventory.\n\n" +
			"When to call: crops are ready and NPC decides to help, or player asks.\n\n" +
			"Constraints: radius max 10, max_count max 10. Harvested items go to NPC backpack; " +
			"use npc_deposit_items or npc_deliver_items to transfer.\n\n" +
			"Side-effect: WRITE (removes crops, adds to NPC inventory).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcHarvestCropsInput) (*mcp.CallToolResult, NpcHarvestCropsOutput, error) {
		if in.NPC == "" {
			return nil, NpcHarvestCropsOutput{}, errNpcRequired
		}
		logToolCall("npc_harvest_crops", in)
		out, err := callBridge[NpcHarvestCropsOutput](ctx, br, bridge.ActionNpcHarvestCrops, in, "npc_harvest_crops")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_deposit_items",
		Description: "NPC walks to a chest and deposits all carried items.\n\n" +
			"When to call: after harvesting/foraging, NPC puts items away.\n\n" +
			"Side-effect: WRITE (modifies chest contents, clears NPC backpack).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcDepositItemsInput) (*mcp.CallToolResult, NpcDepositItemsOutput, error) {
		if in.NPC == "" {
			return nil, NpcDepositItemsOutput{}, errNpcRequired
		}
		logToolCall("npc_deposit_items", in)
		out, err := callBridge[NpcDepositItemsOutput](ctx, br, bridge.ActionNpcDepositItems, in, "npc_deposit_items")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_deliver_items",
		Description: "NPC walks to the player and hands over all carried items.\n\n" +
			"When to call: NPC finished collecting and wants to give items to player directly.\n\n" +
			"Side-effect: WRITE (adds items to player inventory, clears NPC backpack). " +
			"May partially fail if player inventory is full.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcDeliverItemsInput) (*mcp.CallToolResult, NpcDeliverItemsOutput, error) {
		if in.NPC == "" {
			return nil, NpcDeliverItemsOutput{}, errNpcRequired
		}
		logToolCall("npc_deliver_items", in)
		out, err := callBridge[NpcDeliverItemsOutput](ctx, br, bridge.ActionNpcDeliverItems, in, "npc_deliver_items")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_forage_collect",
		Description: "NPC picks up forageable items (spawned objects) in the area.\n\n" +
			"When to call: NPC decides to gather berries, shells, mushrooms during a walk.\n\n" +
			"Constraints: radius max 15, max_count max 10.\n\n" +
			"Side-effect: WRITE (removes spawn objects, adds to NPC backpack).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcForageCollectInput) (*mcp.CallToolResult, NpcForageCollectOutput, error) {
		if in.NPC == "" {
			return nil, NpcForageCollectOutput{}, errNpcRequired
		}
		logToolCall("npc_forage_collect", in)
		out, err := callBridge[NpcForageCollectOutput](ctx, br, bridge.ActionNpcForageCollect, in, "npc_forage_collect")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_pet_animal",
		Description: "NPC pets a farm animal (increases friendship, sets wasPet).\n\n" +
			"When to call: morning animal care routine, or NPC wants to interact with animals.\n\n" +
			"Side-effect: WRITE (modifies animal friendship state).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcPetAnimalInput) (*mcp.CallToolResult, NpcPetAnimalOutput, error) {
		if in.NPC == "" {
			return nil, NpcPetAnimalOutput{}, errNpcRequired
		}
		logToolCall("npc_pet_animal", in)
		out, err := callBridge[NpcPetAnimalOutput](ctx, br, bridge.ActionNpcPetAnimal, in, "npc_pet_animal")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_plant_seeds",
		Description: "NPC plants seeds on empty tilled soil within radius.\n\n" +
			"When to call: player asks NPC to plant, or NPC has seeds and sees empty HoeDirt.\n\n" +
			"Constraints: seed_id must be a valid SDV seed item; radius max 10, max_count max 10. " +
			"Only plants on Farm-type maps in the correct season.\n\n" +
			"Side-effect: WRITE (creates crops on HoeDirt).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcPlantSeedsInput) (*mcp.CallToolResult, NpcPlantSeedsOutput, error) {
		if in.NPC == "" {
			return nil, NpcPlantSeedsOutput{}, errNpcRequired
		}
		if in.SeedID == "" {
			return nil, NpcPlantSeedsOutput{}, errSeedIDRequired
		}
		logToolCall("npc_plant_seeds", in)
		out, err := callBridge[NpcPlantSeedsOutput](ctx, br, bridge.ActionNpcPlantSeeds, in, "npc_plant_seeds")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_till_soil",
		Description: "NPC tills empty ground to create HoeDirt for planting.\n\n" +
			"When to call: preparing farmland before planting.\n\n" +
			"Constraints: radius max 8, max_count max 15. Only on Farm-type maps.\n\n" +
			"Side-effect: WRITE (creates terrain features).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcTillSoilInput) (*mcp.CallToolResult, NpcTillSoilOutput, error) {
		if in.NPC == "" {
			return nil, NpcTillSoilOutput{}, errNpcRequired
		}
		logToolCall("npc_till_soil", in)
		out, err := callBridge[NpcTillSoilOutput](ctx, br, bridge.ActionNpcTillSoil, in, "npc_till_soil")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_inspect_object",
		Description: "NPC walks to a tile and examines the object/crop/terrain there, " +
			"returning a description to the LLM for decision-making.\n\n" +
			"When to call: NPC wants to observe surroundings before acting — e.g. " +
			"checking crop readiness, identifying an object, or investigating a noise.\n\n" +
			"Side-effect: READ (no world mutation, but NPC visibly walks to target).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcInspectObjectInput) (*mcp.CallToolResult, NpcInspectObjectOutput, error) {
		if in.NPC == "" {
			return nil, NpcInspectObjectOutput{}, errNpcRequired
		}
		logToolCall("npc_inspect_object", in)
		out, err := callBridge[NpcInspectObjectOutput](ctx, br, bridge.ActionNpcInspectObject, in, "npc_inspect_object")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_place_object",
		Description: "NPC places an item from internal inventory onto a tile.\n\n" +
			"When to call: NPC needs to set down a machine, decoration, or crafted item.\n\n" +
			"Constraints: target tile must be empty and on a valid map. item_id must be " +
			"a placeable SDV object. Use with extreme caution.\n\n" +
			"Side-effect: WRITE (adds object to world, removes from NPC backpack).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in NpcPlaceObjectInput) (*mcp.CallToolResult, NpcPlaceObjectOutput, error) {
		if in.NPC == "" {
			return nil, NpcPlaceObjectOutput{}, errNpcRequired
		}
		if in.ItemID == "" {
			return nil, NpcPlaceObjectOutput{}, errItemIDRequired
		}
		logToolCall("npc_place_object", in)
		out, err := callBridge[NpcPlaceObjectOutput](ctx, br, bridge.ActionNpcPlaceObject, in, "npc_place_object")
		return nil, out, err
	})
}

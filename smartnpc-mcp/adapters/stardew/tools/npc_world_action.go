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
	Radius        int    `json:"radius,omitempty"       jsonschema:"tile radius for random step (default 8, max 24)"`

	// ── Constraint A: center + tether ─────────────────────────────
	CenterX     int `json:"center_x,omitempty"     jsonschema:"center X for wander zone; defaults to NPC start position"`
	CenterY     int `json:"center_y,omitempty"     jsonschema:"center Y for wander zone; defaults to NPC start position"`
	MaxDistance int `json:"max_distance,omitempty" jsonschema:"max tiles from center; 0=unlimited"`

	// ── Constraint B: bounding box ────────────────────────────────
	X1 int `json:"x1,omitempty" jsonschema:"zone left edge (inclusive); set all 4 to enable"`
	Y1 int `json:"y1,omitempty" jsonschema:"zone top edge (inclusive)"`
	X2 int `json:"x2,omitempty" jsonschema:"zone right edge (inclusive)"`
	Y2 int `json:"y2,omitempty" jsonschema:"zone bottom edge (inclusive)"`
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
	NPC      string   `json:"npc"                jsonschema:"NPC internal name"`
	ChestX   int      `json:"chest_x,omitempty"  jsonschema:"chest tile X; ignored when auto_find=true"`
	ChestY   int      `json:"chest_y,omitempty"  jsonschema:"chest tile Y; ignored when auto_find=true"`
	AutoFind bool     `json:"auto_find,omitempty" jsonschema:"true = ignore coordinates and walk to nearest chest"`
	Map      string   `json:"map,omitempty"      jsonschema:"map name (default: NPC current map)"`
	ItemIds  []string `json:"item_ids,omitempty" jsonschema:"qualified item ids to deposit; omit to deposit everything"`
}

type NpcDepositItemsOutput struct {
	OK        bool   `json:"ok"                  jsonschema:"true if accepted"`
	NPC       string `json:"npc"                 jsonschema:"echo"`
	Deposited int    `json:"deposited,omitempty" jsonschema:"total items actually deposited"`
	ChestX    int    `json:"chest_x,omitempty"   jsonschema:"actual chest tile X used"`
	ChestY    int    `json:"chest_y,omitempty"   jsonschema:"actual chest tile Y used"`
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
	NPC    string `json:"npc"             jsonschema:"NPC internal name"`
	X      int    `json:"x,omitempty"     jsonschema:"center tile X (default: NPC's current tile)"`
	Y      int    `json:"y,omitempty"     jsonschema:"center tile Y (default: NPC's current tile)"`
	Radius int    `json:"radius,omitempty" jsonschema:"scan radius in tiles, 0 = single tile at (x,y) (default 0, max 10)"`
	What   string `json:"what,omitempty"  jsonschema:"filter: crops, objects, terrain, or all (default: all)"`
}

type JsonTile struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type JsonCrop struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Crop string `json:"crop"`
	ID   string `json:"id"`
}

type JsonTileObj struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Name string `json:"name"`
	ID   string `json:"id"`
}

type JsonTileType struct {
	X    int    `json:"x"`
	Y    int    `json:"y"`
	Type string `json:"type"`
}

type NpcInspectObjectOutput struct {
	OK             bool           `json:"ok"                       jsonschema:"true on success"`
	NPC            string         `json:"npc"                      jsonschema:"echo"`
	Center         JsonTile       `json:"center"                   jsonschema:"scan center tile"`
	Radius         int            `json:"radius"                   jsonschema:"scan radius used"`
	TilesScanned   int            `json:"tiles_scanned"            jsonschema:"total tiles examined"`
	Summary        string         `json:"summary"                  jsonschema:"one-line summary, e.g. '3 mature crops, 2 unwatered'"`
	Location       string         `json:"location"                 jsonschema:"map name"`
	Season         string         `json:"season"                   jsonschema:"current in-game season"`
	MatureCrops    []JsonCrop     `json:"mature_crops,omitempty"  jsonschema:"mature crops ready to harvest"`
	UnwateredCrops int            `json:"unwatered_crops,omitempty" jsonschema:"number of unwatered crop tiles"`
	EmptyHoedirt   int            `json:"empty_hoedirt,omitempty"   jsonschema:"number of empty HoeDirt tiles"`
	Objects        []JsonTileObj  `json:"objects,omitempty"      jsonschema:"objects on the ground"`
	Terrain        []JsonTileType `json:"terrain,omitempty"     jsonschema:"non-HoeDirt terrain features"`
}

// ── npc_withdraw_from_chest ─────────────────────────────────────────

type NpcWithdrawFromChestInput struct {
	NPC      string `json:"npc"                jsonschema:"NPC internal name"`
	ItemID   string `json:"item_id"            jsonschema:"SDV qualified item id to withdraw, e.g. \"(O)472\""`
	Count    int    `json:"count,omitempty"    jsonschema:"how many to withdraw (default 1, max 999)"`
	ChestX   int    `json:"chest_x,omitempty"  jsonschema:"chest tile X; ignored when auto_find=true"`
	ChestY   int    `json:"chest_y,omitempty"  jsonschema:"chest tile Y; ignored when auto_find=true"`
	AutoFind bool   `json:"auto_find,omitempty" jsonschema:"true = ignore coordinates and walk to nearest chest"`
	Map      string `json:"map,omitempty"      jsonschema:"map name (default: NPC current map)"`
}

type NpcWithdrawFromChestOutput struct {
	OK        bool   `json:"ok"                   jsonschema:"true if accepted"`
	NPC       string `json:"npc"                  jsonschema:"echo"`
	Withdrawn int    `json:"withdrawn,omitempty"  jsonschema:"items actually withdrawn"`
	ItemID    string `json:"item_id,omitempty"    jsonschema:"item id withdrawn"`
	ChestX    int    `json:"chest_x,omitempty"    jsonschema:"actual chest tile X used"`
	ChestY    int    `json:"chest_y,omitempty"    jsonschema:"actual chest tile Y used"`
	Message   string `json:"message,omitempty"    jsonschema:"status"`
}

// ── npc_transfer_item ──────────────────────────────────────────────

type NpcTransferItemInput struct {
	FromNPC string `json:"from_npc" jsonschema:"sender NPC internal name"`
	ToNPC   string `json:"to_npc"   jsonschema:"recipient NPC internal name"`
	ItemID  string `json:"item_id"  jsonschema:"SDV qualified item id, e.g. \"(O)472\""`
	Count   int    `json:"count"    jsonschema:"how many to transfer (max 999)"`
}

type NpcTransferItemOutput struct {
	OK          bool   `json:"ok"                   jsonschema:"true if accepted"`
	FromNPC     string `json:"from_npc"             jsonschema:"echo"`
	ToNPC       string `json:"to_npc"               jsonschema:"echo"`
	Transferred int    `json:"transferred,omitempty" jsonschema:"items actually transferred"`
	ItemID      string `json:"item_id,omitempty"    jsonschema:"item id transferred"`
	Message     string `json:"message,omitempty"    jsonschema:"status"`
}

// ── npc_fertilize ────────────────────────────────────────────────────

type NpcFertilizeInput struct {
	NPC          string `json:"npc"                jsonschema:"NPC internal name"`
	FertilizerID string `json:"fertilizer_id"      jsonschema:"SDV qualified fertilizer item id, e.g. \"(O)368\" for basic fertilizer"`
	Radius       int    `json:"radius,omitempty"   jsonschema:"tile radius (default 5, max 10)"`
	MaxCount     int    `json:"max_count,omitempty" jsonschema:"max tiles to fertilize (default 5, max 15)"`
}

type NpcFertilizeOutput struct {
	OK          bool   `json:"ok"                   jsonschema:"true if accepted"`
	NPC         string `json:"npc"                  jsonschema:"echo"`
	Fertilized  int    `json:"fertilized,omitempty"  jsonschema:"tiles fertilized"`
	FertilizerID string `json:"fertilizer_id,omitempty" jsonschema:"fertilizer used"`
	Message     string `json:"message,omitempty"    jsonschema:"status"`
}

// ── npc_break_resource ──────────────────────────────────────────────

type NpcBreakResourceInput struct {
	NPC      string `json:"npc"               jsonschema:"NPC internal name"`
	Radius   int    `json:"radius,omitempty"  jsonschema:"tile radius to scan (default 6, max 15)"`
	MaxCount int    `json:"max_count,omitempty" jsonschema:"max resources to break (default 3, max 10)"`
	What     string `json:"what,omitempty"    jsonschema:"filter: trees, stones, or all (default: all)"`
}

type NpcBreakResourceOutput struct {
	OK      bool   `json:"ok"                jsonschema:"true if accepted"`
	NPC     string `json:"npc"               jsonschema:"echo"`
	Broken  int    `json:"broken,omitempty"  jsonschema:"resources broken"`
	Message string `json:"message,omitempty" jsonschema:"status"`
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
//
// The MCP CallToolRequest is required so the underlying ws Request frame
// can be stamped with the originating NPC profile (see callCtx +
// WSClient.AgentForContext) — without it, NPC-agnostic debug routing on
// the mod side cannot attribute tool calls back to a specific Hermes
// profile.
func callBridge[Out any](ctx context.Context, req *mcp.CallToolRequest, br *bridge.WSClient, action string, in any, label string) (Out, error) {
	var out Out
	raw, err := br.Call(callCtx(ctx, req), action, in)
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
			"Constraints (all optional, can combine):\n" +
			"  A. center_x + center_y + max_distance — NPC stays within max_distance of center.\n" +
			"  B. x1/y1/x2/y2 — NPC only picks tiles inside this rectangular zone.\n" +
			"  If both are set, the effective zone is their intersection.\n" +
			"  If neither, wanders freely within radius of current position (legacy behavior).\n\n" +
			"Side-effect: WRITE (moves character). Ongoing for duration_ticks.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcWanderInput) (*mcp.CallToolResult, NpcWanderOutput, error) {
		if in.NPC == "" {
			return nil, NpcWanderOutput{}, errNpcRequired
		}
		logToolCall("npc_wander", in)
		out, err := callBridge[NpcWanderOutput](ctx, req, br, bridge.ActionNpcWander, in, "npc_wander")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_clear_debris",
		Description: "NPC clears debris (weeds, twigs, stones) around their position.\n\n" +
			"When to call: NPC decides to help tidy the farm — typically morning routine " +
			"or when the player asks for help cleaning.\n\n" +
			"Constraints: radius max 10, max_count max 10. Only works on Farm-type maps.\n\n" +
			"Side-effect: WRITE (removes objects from the world).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcClearDebrisInput) (*mcp.CallToolResult, NpcClearDebrisOutput, error) {
		if in.NPC == "" {
			return nil, NpcClearDebrisOutput{}, errNpcRequired
		}
		logToolCall("npc_clear_debris", in)
		out, err := callBridge[NpcClearDebrisOutput](ctx, req, br, bridge.ActionNpcClearDebris, in, "npc_clear_debris")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_water_crops",
		Description: "NPC waters un-watered crops within radius.\n\n" +
			"When to call: morning farm routine, or player explicitly asks NPC to water.\n\n" +
			"Constraints: radius max 10, max_count max 20.\n\n" +
			"Side-effect: WRITE (modifies HoeDirt state).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcWaterCropsInput) (*mcp.CallToolResult, NpcWaterCropsOutput, error) {
		if in.NPC == "" {
			return nil, NpcWaterCropsOutput{}, errNpcRequired
		}
		logToolCall("npc_water_crops", in)
		out, err := callBridge[NpcWaterCropsOutput](ctx, req, br, bridge.ActionNpcWaterCrops, in, "npc_water_crops")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_harvest_crops",
		Description: "NPC harvests mature crops within radius and stores them in internal inventory.\n\n" +
			"When to call: crops are ready and NPC decides to help, or player asks.\n\n" +
			"Constraints: radius max 10, max_count max 10. Harvested items go to NPC backpack; " +
			"use npc_deposit_items or npc_deliver_items to transfer.\n\n" +
			"Side-effect: WRITE (removes crops, adds to NPC inventory).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcHarvestCropsInput) (*mcp.CallToolResult, NpcHarvestCropsOutput, error) {
		if in.NPC == "" {
			return nil, NpcHarvestCropsOutput{}, errNpcRequired
		}
		logToolCall("npc_harvest_crops", in)
		out, err := callBridge[NpcHarvestCropsOutput](ctx, req, br, bridge.ActionNpcHarvestCrops, in, "npc_harvest_crops")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_deposit_items",
		Description: "NPC walks to a chest and deposits carried items from their backpack.\n\n" +
			"When to call: after npc_clear_debris / npc_forage_collect / npc_harvest_crops, " +
			"to transfer collected items into storage.\n\n" +
			"Chest selection: set auto_find=true to automatically walk to the nearest chest " +
			"(ignores chest_x/chest_y). Or specify chest_x+chest_y for a specific chest.\n\n" +
			"Item filter: set item_ids to deposit only specific items (e.g. [\"(O)390\"]); " +
			"omit to deposit everything in backpack.\n\n" +
			"Side-effect: WRITE (adds items to chest, removes from NPC backpack). " +
			"If chest is full, remaining items stay in NPC backpack.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcDepositItemsInput) (*mcp.CallToolResult, NpcDepositItemsOutput, error) {
		if in.NPC == "" {
			return nil, NpcDepositItemsOutput{}, errNpcRequired
		}
		logToolCall("npc_deposit_items", in)
		out, err := callBridge[NpcDepositItemsOutput](ctx, req, br, bridge.ActionNpcDepositItems, in, "npc_deposit_items")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_deliver_items",
		Description: "NPC walks to the player and hands over all carried items.\n\n" +
			"When to call: NPC finished collecting and wants to give items to player directly.\n\n" +
			"Side-effect: WRITE (adds items to player inventory, clears NPC backpack). " +
			"May partially fail if player inventory is full.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcDeliverItemsInput) (*mcp.CallToolResult, NpcDeliverItemsOutput, error) {
		if in.NPC == "" {
			return nil, NpcDeliverItemsOutput{}, errNpcRequired
		}
		logToolCall("npc_deliver_items", in)
		out, err := callBridge[NpcDeliverItemsOutput](ctx, req, br, bridge.ActionNpcDeliverItems, in, "npc_deliver_items")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_forage_collect",
		Description: "NPC picks up forageable items (spawned objects) in the area.\n\n" +
			"When to call: NPC decides to gather berries, shells, mushrooms during a walk.\n\n" +
			"Constraints: radius max 15, max_count max 10.\n\n" +
			"Side-effect: WRITE (removes spawn objects, adds to NPC backpack).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcForageCollectInput) (*mcp.CallToolResult, NpcForageCollectOutput, error) {
		if in.NPC == "" {
			return nil, NpcForageCollectOutput{}, errNpcRequired
		}
		logToolCall("npc_forage_collect", in)
		out, err := callBridge[NpcForageCollectOutput](ctx, req, br, bridge.ActionNpcForageCollect, in, "npc_forage_collect")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_pet_animal",
		Description: "NPC pets a farm animal (increases friendship, sets wasPet).\n\n" +
			"When to call: morning animal care routine, or NPC wants to interact with animals.\n\n" +
			"Side-effect: WRITE (modifies animal friendship state).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcPetAnimalInput) (*mcp.CallToolResult, NpcPetAnimalOutput, error) {
		if in.NPC == "" {
			return nil, NpcPetAnimalOutput{}, errNpcRequired
		}
		logToolCall("npc_pet_animal", in)
		out, err := callBridge[NpcPetAnimalOutput](ctx, req, br, bridge.ActionNpcPetAnimal, in, "npc_pet_animal")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_plant_seeds",
		Description: "NPC plants seeds on empty tilled soil within radius.\n\n" +
			"When to call: player asks NPC to plant, or NPC sees empty HoeDirt.\n\n" +
			"Seed consumption: if the NPC has seeds of the given seed_id in their backpack, " +
			"one seed is consumed per tile planted. If the backpack has no matching seeds, " +
			"planting still succeeds without consuming anything (free plant mode).\n\n" +
			"Constraints: seed_id must be a valid SDV seed item; radius max 10, max_count max 10. " +
			"Only plants on Farm-type maps.\n\n" +
			"Side-effect: WRITE (creates crops on HoeDirt, optionally consumes seeds from NPC backpack).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcPlantSeedsInput) (*mcp.CallToolResult, NpcPlantSeedsOutput, error) {
		if in.NPC == "" {
			return nil, NpcPlantSeedsOutput{}, errNpcRequired
		}
		if in.SeedID == "" {
			return nil, NpcPlantSeedsOutput{}, errSeedIDRequired
		}
		logToolCall("npc_plant_seeds", in)
		out, err := callBridge[NpcPlantSeedsOutput](ctx, req, br, bridge.ActionNpcPlantSeeds, in, "npc_plant_seeds")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_till_soil",
		Description: "NPC tills empty ground to create HoeDirt for planting.\n\n" +
			"When to call: preparing farmland before planting.\n\n" +
			"Constraints: radius max 8, max_count max 15. Only on Farm-type maps.\n\n" +
			"Side-effect: WRITE (creates terrain features).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcTillSoilInput) (*mcp.CallToolResult, NpcTillSoilOutput, error) {
		if in.NPC == "" {
			return nil, NpcTillSoilOutput{}, errNpcRequired
		}
		logToolCall("npc_till_soil", in)
		out, err := callBridge[NpcTillSoilOutput](ctx, req, br, bridge.ActionNpcTillSoil, in, "npc_till_soil")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_inspect_object",
		Description: "NPC walks to a tile and examines the object/crop/terrain there, " +
			"returning a description to the LLM for decision-making.\n\n" +
			"When to call: NPC wants to observe surroundings before acting — e.g. " +
			"checking crop readiness, identifying an object, or investigating a noise.\n\n" +
			"Side-effect: READ (no world mutation, but NPC visibly walks to target).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcInspectObjectInput) (*mcp.CallToolResult, NpcInspectObjectOutput, error) {
		if in.NPC == "" {
			return nil, NpcInspectObjectOutput{}, errNpcRequired
		}
		logToolCall("npc_inspect_object", in)
		out, err := callBridge[NpcInspectObjectOutput](ctx, req, br, bridge.ActionNpcInspectObject, in, "npc_inspect_object")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_place_object",
		Description: "NPC places an item from internal inventory onto a tile.\n\n" +
			"When to call: NPC needs to set down a machine, decoration, or crafted item.\n\n" +
			"Constraints: target tile must be empty and on a valid map. item_id must be " +
			"a placeable SDV object. Use with extreme caution.\n\n" +
			"Side-effect: WRITE (adds object to world, removes from NPC backpack).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcPlaceObjectInput) (*mcp.CallToolResult, NpcPlaceObjectOutput, error) {
		if in.NPC == "" {
			return nil, NpcPlaceObjectOutput{}, errNpcRequired
		}
		if in.ItemID == "" {
			return nil, NpcPlaceObjectOutput{}, errItemIDRequired
		}
		logToolCall("npc_place_object", in)
		out, err := callBridge[NpcPlaceObjectOutput](ctx, req, br, bridge.ActionNpcPlaceObject, in, "npc_place_object")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_withdraw_from_chest",
		Description: "NPC walks to a chest and withdraws items into their backpack.\n\n" +
			"When to call: NPC needs to get seeds, tools, or materials from storage " +
			"before doing farm work. E.g. manager NPC getting seeds to distribute to workers.\n\n" +
			"Chest selection: set auto_find=true to automatically walk to the nearest chest " +
			"(ignores chest_x/chest_y). Or specify chest_x+chest_y for a specific chest.\n\n" +
			"Side-effect: WRITE (removes items from chest, adds to NPC backpack). " +
			"If chest doesn't have enough, takes what's available.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcWithdrawFromChestInput) (*mcp.CallToolResult, NpcWithdrawFromChestOutput, error) {
		if in.NPC == "" {
			return nil, NpcWithdrawFromChestOutput{}, errNpcRequired
		}
		if in.ItemID == "" {
			return nil, NpcWithdrawFromChestOutput{}, errItemIDRequired
		}
		if in.Count <= 0 {
			in.Count = 1
		}
		logToolCall("npc_withdraw_from_chest", in)
		out, err := callBridge[NpcWithdrawFromChestOutput](ctx, req, br, bridge.ActionNpcWithdrawItems, in, "npc_withdraw_from_chest")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_transfer_item",
		Description: "Transfer items from one NPC's backpack to another NPC's backpack. " +
			"Direct inventory-to-inventory transfer — no movement involved.\n\n" +
			"When to call: farm manager distributing seeds/tools to workers, " +
			"or NPCs sharing resources among themselves.\n\n" +
			"Constraints: from_npc must have enough items; count is capped at what's available.\n\n" +
			"Side-effect: WRITE (modifies both NPCs' backpacks).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcTransferItemInput) (*mcp.CallToolResult, NpcTransferItemOutput, error) {
		if in.FromNPC == "" || in.ToNPC == "" {
			return nil, NpcTransferItemOutput{}, fmt.Errorf("from_npc and to_npc are required")
		}
		if in.ItemID == "" {
			return nil, NpcTransferItemOutput{}, errItemIDRequired
		}
		if in.Count <= 0 {
			return nil, NpcTransferItemOutput{}, fmt.Errorf("count must be positive")
		}
		if in.FromNPC == in.ToNPC {
			return nil, NpcTransferItemOutput{}, fmt.Errorf("from_npc and to_npc must differ")
		}
		logToolCall("npc_transfer_item", in)
		out, err := callBridge[NpcTransferItemOutput](ctx, req, br, bridge.ActionNpcTransferItem, in, "npc_transfer_item")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_fertilize",
		Description: "NPC applies fertilizer to empty tilled soil within radius.\n\n" +
			"When to call: before planting, to improve soil quality for better crops.\n\n" +
			"Fertilizer consumption: if the NPC has the fertilizer in their backpack, " +
			"one is consumed per tile. If the backpack has none, fertilizing still " +
			"succeeds (free mode). Valid fertilizer_ids: \"(O)368\" (Basic), \"(O)369\" " +
			"(Quality), \"(O)465\" (Speed-Gro), \"(O)466\" (Deluxe Speed-Gro), " +
			"\"(O)370\" (Basic Retaining), \"(O)371\" (Quality Retaining).\n\n" +
			"Constraints: radius max 10, max_count max 15. Only on Farm-type maps. " +
			"Skips tiles that already have fertilizer or a crop.\n\n" +
			"Side-effect: WRITE (sets HoeDirt fertilizer, optionally consumes from NPC backpack).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcFertilizeInput) (*mcp.CallToolResult, NpcFertilizeOutput, error) {
		if in.NPC == "" {
			return nil, NpcFertilizeOutput{}, errNpcRequired
		}
		if in.FertilizerID == "" {
			return nil, NpcFertilizeOutput{}, errItemIDRequired
		}
		logToolCall("npc_fertilize", in)
		out, err := callBridge[NpcFertilizeOutput](ctx, req, br, bridge.ActionNpcFertilize, in, "npc_fertilize")
		return nil, out, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "npc_break_resource",
		Description: "NPC breaks natural resources (trees, stumps, large stones) and collects " +
			"the drops into their backpack.\n\n" +
			"When to call: NPC is in a forest/mountain/quarry area and wants to gather wood, " +
			"hardwood, stone, or other natural materials.\n\n" +
			"Constraints: radius max 15, max_count max 10. Does NOT break player-placed objects " +
			"or machines. Trees with tappers are skipped. Use `what` to filter: \"trees\" (trees " +
			"+ stumps), \"stones\" (large stones only), or \"all\" (default).\n\n" +
			"Side-effect: WRITE (removes terrain features / world objects, adds drops to NPC backpack).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in NpcBreakResourceInput) (*mcp.CallToolResult, NpcBreakResourceOutput, error) {
		if in.NPC == "" {
			return nil, NpcBreakResourceOutput{}, errNpcRequired
		}
		logToolCall("npc_break_resource", in)
		out, err := callBridge[NpcBreakResourceOutput](ctx, req, br, bridge.ActionNpcBreakResource, in, "npc_break_resource")
		return nil, out, err
	})
}

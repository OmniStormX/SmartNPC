import sys

with open('smartnpc-mcp/adapters/stardew/tools/npc_world_action.go', 'r', encoding='utf-8') as f:
    content = f.read()

# --- Struct changes ---

# 1. NpcInspectObjectInput: expand radius max, add farm_actions
old = 'jsonschema:"scan radius in tiles, 0 = single tile at (x,y) (default 0, max 10)"'
new = 'jsonschema:"scan radius in tiles, 0 = single tile at (x,y) (default 0, max 30)"'
content = content.replace(old, new)

old = 'jsonschema:"filter: crops, objects, terrain, or all (default: all)"'
new = 'jsonschema:"filter: crops, objects, terrain, all, or farm_actions (default: all). farm_actions returns action-category groups with counts and bboxes"'
content = content.replace(old, new)

# 2. Add BBox, CropSummary, ActionGroup types before NpcInspectObjectOutput
old = 'type NpcInspectObjectOutput struct {'
new = '''// BBox is a rectangular bounding box in tile coordinates.
type BBox struct {
\tX1 int `json:"x1" jsonschema:"left edge (inclusive)"`
\tY1 int `json:"y1" jsonschema:"top edge (inclusive)"`
\tX2 int `json:"x2" jsonschema:"right edge (inclusive)"`
\tY2 int `json:"y2" jsonschema:"bottom edge (inclusive)"`
}

// CropSummary is a counted crop entry in a harvest action group.
type CropSummary struct {
\tID    string `json:"id"   jsonschema:"SDV qualified item id"`
\tName  string `json:"name" jsonschema:"crop display name"`
\tCount int    `json:"count" jsonschema:"how many tiles of this crop are ready"`
}

// ActionGroup describes one action category with count and bounding box.
type ActionGroup struct {
\tCount int            `json:"count"                    jsonschema:"number of target tiles"`
\tBBox  *BBox          `json:"bbox,omitempty"           jsonschema:"bounding box enclosing all targets"`
\tCrops []CropSummary  `json:"crops,omitempty"          jsonschema:"crop breakdown (harvest only)"`
}

type NpcInspectObjectOutput struct {'''
content = content.replace(old, new)

# 3. Add ActionsAvailable + fix up the end of NpcInspectObjectOutput
old = '\tEmptyHoedirt   int            `json:"empty_hoedirt,omitempty"   jsonschema:"number of empty HoeDirt tiles"`'
new = '''\tEmptyHoedirt   int                     `json:"empty_hoedirt,omitempty"   jsonschema:"number of empty HoeDirt tiles"`
\tObjects        []JsonTileObj           `json:"objects,omitempty"         jsonschema:"objects on the ground"`
\tTerrain        []JsonTileType          `json:"terrain,omitempty"         jsonschema:"non-HoeDirt terrain features"`
\t// farm_actions mode — action categories with counts and bounding boxes.
\tActionsAvailable map[string]ActionGroup `json:"actions_available,omitempty" jsonschema:"action categories with counts and bboxes (populated when what=farm_actions)"`'''
content = content.replace(old, new)

# 4. Update behavior tool inputs

# NpcClearDebrisInput
old = 'type NpcClearDebrisInput struct {\n\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`\n\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius to scan (default 5, max 10)"`\n\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max items to clear (default 3, max 10)"`\n}'
new = '''type NpcClearDebrisInput struct {
\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`
\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 30)"`
\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max items to clear (default 3, max 10)"`
\tX1       int    `json:"x1,omitempty" jsonschema:"bbox left edge; overrides radius when all 4 bbox fields are set"`
\tY1       int    `json:"y1,omitempty" jsonschema:"bbox top edge"`
\tX2       int    `json:"x2,omitempty" jsonschema:"bbox right edge"`
\tY2       int    `json:"y2,omitempty" jsonschema:"bbox bottom edge"`
}'''
content = content.replace(old, new)

# NpcWaterCropsInput
old = 'type NpcWaterCropsInput struct {\n\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`\n\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 10)"`\n\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max crops to water (default 5, max 20)"`\n}'
new = '''type NpcWaterCropsInput struct {
\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`
\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 30)"`
\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max crops to water (default 5, max 20)"`
\tX1       int    `json:"x1,omitempty" jsonschema:"bbox left edge; overrides radius when all 4 bbox fields are set"`
\tY1       int    `json:"y1,omitempty" jsonschema:"bbox top edge"`
\tX2       int    `json:"x2,omitempty" jsonschema:"bbox right edge"`
\tY2       int    `json:"y2,omitempty" jsonschema:"bbox bottom edge"`
}'''
content = content.replace(old, new)

# NpcHarvestCropsInput
old = 'type NpcHarvestCropsInput struct {\n\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`\n\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 10)"`\n\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max crops to harvest (default 5, max 10)"`\n}'
new = '''type NpcHarvestCropsInput struct {
\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`
\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 5, max 30)"`
\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max crops to harvest (default 5, max 10)"`
\tX1       int    `json:"x1,omitempty" jsonschema:"bbox left edge; overrides radius when all 4 bbox fields are set"`
\tY1       int    `json:"y1,omitempty" jsonschema:"bbox top edge"`
\tX2       int    `json:"x2,omitempty" jsonschema:"bbox right edge"`
\tY2       int    `json:"y2,omitempty" jsonschema:"bbox bottom edge"`
}'''
content = content.replace(old, new)

# NpcTillSoilInput
old = 'type NpcTillSoilInput struct {\n\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`\n\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 3, max 8)"`\n\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max tiles to till (default 5, max 15)"`\n}'
new = '''type NpcTillSoilInput struct {
\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`
\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 3, max 30)"`
\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max tiles to till (default 5, max 15)"`
\tX1       int    `json:"x1,omitempty" jsonschema:"bbox left edge; overrides radius when all 4 bbox fields are set"`
\tY1       int    `json:"y1,omitempty" jsonschema:"bbox top edge"`
\tX2       int    `json:"x2,omitempty" jsonschema:"bbox right edge"`
\tY2       int    `json:"y2,omitempty" jsonschema:"bbox bottom edge"`
}'''
content = content.replace(old, new)

# NpcForageCollectInput
old = 'type NpcForageCollectInput struct {\n\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`\n\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 8, max 15)"`\n\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max items to collect (default 3, max 10)"`\n}'
new = '''type NpcForageCollectInput struct {
\tNPC      string `json:"npc"               jsonschema:"NPC internal name"`
\tRadius   int    `json:"radius,omitempty"  jsonschema:"tile radius (default 8, max 30)"`
\tMaxCount int    `json:"max_count,omitempty" jsonschema:"max items to collect (default 3, max 10)"`
\tX1       int    `json:"x1,omitempty" jsonschema:"bbox left edge; overrides radius when all 4 bbox fields are set"`
\tY1       int    `json:"y1,omitempty" jsonschema:"bbox top edge"`
\tX2       int    `json:"x2,omitempty" jsonschema:"bbox right edge"`
\tY2       int    `json:"y2,omitempty" jsonschema:"bbox bottom edge"`
}'''
content = content.replace(old, new)

print('Struct changes applied')

# --- Description changes ---

# inspect_object
old_desc = ('\t\tDescription: "NPC walks to a tile and examines the object/crop/terrain there, " +\n'
            '\t\t\t"returning a description to the LLM for decision-making.\\n\\n" +\n'
            '\t\t\t"When to call: NPC wants to observe surroundings before acting — e.g. " +\n'
            '\t\t\t"checking crop readiness, identifying an object, or investigating a noise.\\n\\n" +\n'
            '\t\t\t"Side-effect: READ (no world mutation, but NPC visibly walks to target).",')

new_desc = ('\t\tDescription: "NPC scans a large area and returns categorized information for decision-making.\\n\\n" +\n'
            '\t\t\t"When to call: NPC wants to observe surroundings before acting — situational awareness, " +\n'
            '\t\t\t"checking crop readiness, finding work to do.\\n\\n" +\n'
            '\t\t\t"What modes:\\n" +\n'
            '\t\t\t"  crops/objects/terrain/all — per-tile detail (max radius 30).\\n" +\n'
            '\t\t\t"  farm_actions — action-category groups (harvest/water/clear/till/forage/plant) " +\n'
            '\t\t\t"each with count + bounding box. Use this as the single observation call before " +\n'
            '\t\t\t"issuing behavior commands; pass the returned bboxes directly to the corresponding " +\n'
            '\t\t\t"npc_* tools via their x1/y1/x2/y2 params.\\n\\n" +\n'
            '\t\t\t"Side-effect: READ (no world mutation, NPC does not move).",')

if old_desc in content:
    content = content.replace(old_desc, new_desc)
    print('inspect_object desc OK')
else:
    print('inspect_object desc NOT FOUND')

# clear_debris
old_desc = ('\t\tDescription: "NPC clears debris (weeds, twigs, stones) around their position.\\n\\n" +\n'
            '\t\t\t"When to call: NPC decides to help tidy the farm — typically morning routine " +\n'
            '\t\t\t"or when the player asks for help cleaning.\\n\\n" +\n'
            '\t\t\t"Constraints: radius max 10, max_count max 10. Only works on Farm-type maps.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (removes objects from the world).",')
new_desc = ('\t\tDescription: "NPC clears debris (weeds, twigs, stones) in an area.\\n\\n" +\n'
            '\t\t\t"When to call: NPC decides to help tidy the farm — typically morning routine " +\n'
            '\t\t\t"or when the player asks for help cleaning.\\n\\n" +\n'
            '\t\t\t"Area selection: pass x1/y1/x2/y2 from npc_inspect_object\'s clear bbox, " +\n'
            '\t\t\t"or use radius to scan around NPC. radius max 30, max_count max 10. " +\n'
            '\t\t\t"Only works on Farm-type maps.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (removes objects from the world).",')
if old_desc in content:
    content = content.replace(old_desc, new_desc)
    print('clear_debris desc OK')
else:
    print('clear_debris desc NOT FOUND')

# water_crops
old_desc = ('\t\tDescription: "NPC waters un-watered crops within radius.\\n\\n" +\n'
            '\t\t\t"When to call: morning farm routine, or player explicitly asks NPC to water.\\n\\n" +\n'
            '\t\t\t"Constraints: radius max 10, max_count max 20.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (modifies HoeDirt state).",')
new_desc = ('\t\tDescription: "NPC waters un-watered crops in an area.\\n\\n" +\n'
            '\t\t\t"When to call: morning farm routine, or player explicitly asks NPC to water.\\n\\n" +\n'
            '\t\t\t"Area selection: pass x1/y1/x2/y2 from npc_inspect_object\'s water bbox, " +\n'
            '\t\t\t"or use radius to scan around NPC. radius max 30, max_count max 20.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (modifies HoeDirt state).",')
if old_desc in content:
    content = content.replace(old_desc, new_desc)
    print('water_crops desc OK')
else:
    print('water_crops desc NOT FOUND')

# harvest_crops
old_desc = ('\t\tDescription: "NPC harvests mature crops within radius and stores them in internal inventory.\\n\\n" +\n'
            '\t\t\t"When to call: crops are ready and NPC decides to help, or player asks.\\n\\n" +\n'
            '\t\t\t"Constraints: radius max 10, max_count max 10. Harvested items go to NPC backpack; " +\n'
            '\t\t\t"use npc_deposit_items or npc_deliver_items to transfer.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (removes crops, adds to NPC inventory).",')
new_desc = ('\t\tDescription: "NPC harvests mature crops in an area and stores them in internal inventory.\\n\\n" +\n'
            '\t\t\t"When to call: crops are ready and NPC decides to help, or player asks.\\n\\n" +\n'
            '\t\t\t"Area selection: pass x1/y1/x2/y2 from npc_inspect_object\'s harvest bbox, " +\n'
            '\t\t\t"or use radius to scan around NPC. radius max 30, max_count max 10. " +\n'
            '\t\t\t"Harvested items go to NPC backpack; " +\n'
            '\t\t\t"use npc_deposit_items or npc_deliver_items to transfer.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (removes crops, adds to NPC inventory).",')
if old_desc in content:
    content = content.replace(old_desc, new_desc)
    print('harvest_crops desc OK')
else:
    print('harvest_crops desc NOT FOUND')

# till_soil
old_desc = ('\t\tDescription: "NPC tills empty ground to create HoeDirt for planting.\\n\\n" +\n'
            '\t\t\t"When to call: preparing farmland before planting.\\n\\n" +\n'
            '\t\t\t"Constraints: radius max 8, max_count max 15. Only on Farm-type maps.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (creates terrain features).",')
new_desc = ('\t\tDescription: "NPC tills empty ground to create HoeDirt for planting.\\n\\n" +\n'
            '\t\t\t"When to call: preparing farmland before planting.\\n\\n" +\n'
            '\t\t\t"Area selection: pass x1/y1/x2/y2 from npc_inspect_object\'s till bbox, " +\n'
            '\t\t\t"or use radius to scan around NPC. radius max 30, max_count max 15. " +\n'
            '\t\t\t"Only on Farm-type maps.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (creates terrain features).",')
if old_desc in content:
    content = content.replace(old_desc, new_desc)
    print('till_soil desc OK')
else:
    print('till_soil desc NOT FOUND')

# forage_collect
old_desc = ('\t\tDescription: "NPC picks up forageable items (spawned objects) in the area.\\n\\n" +\n'
            '\t\t\t"When to call: NPC decides to gather berries, shells, mushrooms during a walk.\\n\\n" +\n'
            '\t\t\t"Constraints: radius max 15, max_count max 10.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (removes spawn objects, adds to NPC backpack).",')
new_desc = ('\t\tDescription: "NPC picks up forageable items (spawned objects) in the area.\\n\\n" +\n'
            '\t\t\t"When to call: NPC decides to gather berries, shells, mushrooms during a walk.\\n\\n" +\n'
            '\t\t\t"Area selection: pass x1/y1/x2/y2 from npc_inspect_object\'s forage bbox, " +\n'
            '\t\t\t"or use radius to scan around NPC. radius max 30, max_count max 10.\\n\\n" +\n'
            '\t\t\t"Side-effect: WRITE (removes spawn objects, adds to NPC backpack).",')
if old_desc in content:
    content = content.replace(old_desc, new_desc)
    print('forage_collect desc OK')
else:
    print('forage_collect desc NOT FOUND')

with open('smartnpc-mcp/adapters/stardew/tools/npc_world_action.go', 'w', encoding='utf-8', newline='') as f:
    f.write(content)
print('Done')

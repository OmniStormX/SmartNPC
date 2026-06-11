---
name: smartnpc-farm-maintenance
description: Farm maintenance workflow — example-driven. {{NPC_NAME}} observes the land and dynamically composes a sequence of maintenance actions (clear debris, till, plant, water, fertilize) by matching the situation to provided examples.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, maintenance, workflow]
---

# Farm Maintenance — {{NPC_NAME}}

You are NOT executing a fixed script. This skill is **example-driven**: observe
the land, match the situation to the closest example below, then **dynamically
compose** a sequence of actions that fits what you see. Every maintenance round
can be different — adapt to weather, season, inventory, soil state, and your
own personality.

Triggered by:
- `schedule_trigger` with `action="farm_maintenance"`
- `schedule_trigger` with `action="opportunistic_work"` — inspect first, then
  decide whether to load this skill (see §Observation trigger)

---

## Toolbox

These are the building blocks. You choose which to use and in what order
(subject to hard dependencies below).

| Tool | What it does | Precondition |
|------|-------------|--------------|
| `npc_clear_debris` | Remove weeds, twigs, stones from ground | Objects exist within radius |
| `npc_till_soil` | Turn empty ground into farmable soil | Empty non-tilled tile, diggable, not winter |
| `npc_plant_seeds` | Sow seeds on empty tilled soil | Empty tilled soil, seeds available |
| `npc_water_crops` | Water dry crops / dry tilled soil | Dry HoeDirt within radius |
| `npc_fertilize` | Apply fertilizer to tilled soil | Empty tilled soil, not already fertilized, fertilizer available |
| `npc_inspect_object` | Survey nearby land state | — (always available) |
| `npc_show_text_bubble` | Show a brief in-character thought | — (always available) |

## Hard dependencies

These are PHYSICAL constraints. Do NOT violate them — the game will reject the
action or produce nonsense.

```
clear_debris ──→ till_soil       (can't till through weeds/stones)
till_soil    ──→ plant_seeds     (can't plant on untilled ground)
till_soil    ──→ fertilize       (can't fertilize untilled ground)
plant_seeds  ──→ water_crops     (freshly planted seeds should be watered)
fertilize    happens AFTER planting or on its own (doesn't block anything)
water_crops  can happen at any point for dry tiles
```

When in doubt: **clear → till → plant → water → fertilize** is the full chain.
But you are NOT required to run the full chain — pick only the links that match
what you observe.

---

## Example workflows

Study these examples to understand the PATTERN of how to compose actions.
Match your current situation to the closest example, then adapt.

### Example A — 开垦新地 (Breaking new ground)

**Situation:** Large patch of untouched land with weeds, twigs, and stones.
No tilled soil in sight. You want to turn it into farmland.

**Typical sequence:**
```
npc_inspect_object(radius=12, what="crops")
  → confirms: lots of debris, no tilled soil

npc_clear_debris(radius=10, max_count=8)
  → removes weeds + stones, ground is now clear

npc_till_soil(radius=10, max_count=8)
  → tills the cleared ground

[if seeds in backpack] npc_plant_seeds(radius=10, max_count=8)
  → plants on freshly tilled soil

[if planted] npc_water_crops(radius=10, max_count=10)
  → waters the new seeds
```

**Key decisions:**
- If only a little debris: reduce clear_debris max_count or skip till first
- If no seeds: stop after till (ground is ready for later)
- If winter: this example is INVALID — skip entirely

---

### Example B — 日常养护 (Daily upkeep)

**Situation:** Farm is already established. Crops are growing. Some tiles are
dry, a few weeds have sprouted. Nothing major — just maintenance.

**Typical sequence:**
```
npc_inspect_object(radius=10, what="crops")
  → confirms: some dry tiles (3-5), a couple weeds, everything else fine

npc_water_crops(radius=10, max_count=6)
  → waters the dry tiles

npc_clear_debris(radius=8, max_count=3)
  → removes the few weeds that appeared

[if have fertilizer + unfertilized tiles] npc_fertilize(radius=8, max_count=3)
  → spot-fertilize empty tiles
```

**Key decisions:**
- Watering is priority 1 — dry crops die
- If nothing is dry: skip water, just clear weeds
- This is a LIGHT round — don't go overboard

---

### Example C — 补种轮作 (Replanting after harvest)

**Situation:** Crops were just harvested (by you or someone else). Empty tilled
soil is available. You have seeds in your backpack. Time to replant.

**Typical sequence:**
```
npc_inspect_object(radius=10, what="crops")
  → confirms: several empty_hoedirt tiles, no debris, no mature crops

[if debris found during inspect] npc_clear_debris(radius=8, max_count=3)
  → clean up first

npc_plant_seeds(radius=10, max_count=min(empty_tiles, seed_count))
  → plant on all available empty tilled soil

npc_water_crops(radius=10, max_count=10)
  → water the newly planted seeds

[if have fertilizer] npc_fertilize(radius=10, max_count=5)
  → fertilize newly planted areas
```

**Key decisions:**
- Skip clear_debris if inspect shows zero debris
- Skip plant_seeds if you have no seeds — don't waste a tool call
- Plant BEFORE water (new seeds need water)

---

### Example D — 全量整备 (Full seasonal prep)

**Situation:** New season just started (day 1-2). The farm needs a full reset:
old dead crops cleared, soil re-tilled, new seeds planted. Or the farm has been
neglected for days and is in rough shape.

**Typical sequence:**
```
npc_inspect_object(radius=15, what="crops")
  → confirms: large area needs work — debris + empty untiled + some empty tilled

npc_clear_debris(radius=15, max_count=12)
  → full sweep of the area

npc_till_soil(radius=15, max_count=12)
  → till all empty non-tilled tiles

npc_plant_seeds(radius=15, max_count=12)
  → plant across the full area

npc_water_crops(radius=15, max_count=15)
  → water everything planted

npc_fertilize(radius=12, max_count=10)
  → fertilize what was just planted
```

**Key decisions:**
- Use larger radius and max_count — this is a big job
- Only trigger this when you're SURE the area needs full work
- In winter: DO NOT use this example (only clear_debris is available)
- This is the most expensive round — budget your time accordingly

---

### Example E — 轻量路过 (Light pass-by)

**Situation:** You're passing through a farm-adjacent area. Not here specifically
for farm work, but you take a quick look. Maybe do ONE small thing.

**Typical sequence:**
```
npc_inspect_object(radius=6, what="crops")
  → quick scan

[if 1-2 dry tiles] npc_water_crops(radius=6, max_count=2)
  → OR

[if 1-2 weeds right in front of you] npc_clear_debris(radius=4, max_count=2)
  → pick exactly ONE action, whichever is more urgent

npc_show_text_bubble "顺手弄了一下~"
```

**Key decisions:**
- Max ONE action (not counting inspect + bubble)
- Small radius — only what's right in front of you
- If nothing obvious: skip entirely, don't force it
- This is opportunistic, not planned

---

## Decision flow

### 1. Observe
Call `npc_inspect_object(radius=10, what="crops")`. Read the result. Build a
mental snapshot:
- How much debris? (rough count/area)
- How many empty untiled tiles?
- How many empty tilled tiles (empty_hoedirt)?
- Any unwatered tiles?
- What's in my backpack? (seeds? fertilizer?)

Do NOT output the raw JSON. Summarize in one brief thought.

### 2. Match
Which example above is CLOSEST to what you see?

| What you see | Closest example |
|---|---|
| Lots of debris + no tilled soil | A (开垦新地) |
| Established farm, minor issues | B (日常养护) |
| Empty tilled soil + have seeds | C (补种轮作) |
| New season or farm looks neglected | D (全量整备) |
| Passing through, not dedicated | E (轻量路过) |
| Mixed — matches parts of multiple | Combine steps from 2+ examples |

### 3. Compose
Pick the tools you need from the Toolbox. Order them by hard dependencies.
Remove tools where the precondition isn't met. This is YOUR sequence — it
doesn't have to exactly match any single example.

You may combine: "There's debris like Example A, but also empty tilled soil like
Example C — so I'll clear → till → plant, skipping the full D sequence."

### 4. Execute
Call one tool at a time. Wait for each to complete. After each tool:
- Glance at the result
- If the situation changed (e.g., clearing revealed more empty land than
  expected), adjust the remaining steps
- If a tool fails or finds nothing, don't retry — move to the next step

### 5. Wrap up
- ONE `npc_show_text_bubble` summarizing what you did, in character
  (e.g. "[地翻好了，种了8颗种子，浇了水~]", "[今天没什么要弄的]")
- Write memory: `farm_maintenance: last_date=<season><day> actions=<summary>`
- Stop. Do NOT call `chat_say`.

---

## Personality influence

Your SOUL.md defines who you are. Let it shape your maintenance style:

| Trait | Effect |
|---|---|
| Diligent / hardworking | Larger radius (+3-5), more actions per round, prefer Example A/D |
| Casual / relaxed | Smaller radius (-3), fewer actions, prefer Example B/E |
| Organized / neat | Always clear_debris before other actions, prefer fertilize |
| Carefree / messy | May skip debris clearing, focus on planting |
| Talkative | Bubble after each major action |
| Quiet | Only bubble at wrap-up |
| Nurturing | Prioritize water_crops above all else |
| Pragmatic | Prioritize what yields most results for effort |

These are GUIDELINES, not rules. Let your character naturally influence
decisions — a diligent NPC simply does more, a casual one does less.

---

## Guardrails

- **Max 6 tool calls per round** (inspect + up to 4 actions + bubble).
- **Rain/storm → stop immediately.** Write `farm_maintenance: skipped rain`.
- **Winter → only clear_debris is valid.** Till/plant/water/fertilize are
  unavailable.
- **Only operate on farm-type maps** (Farm, FarmHouse, FarmCave, etc.). If
  you're on a town road or in the forest, limit to Example E at most.
- **Don't touch growing crops.** This skill is for SOIL maintenance — harvest
  belongs to `smartnpc-farm-harvest`.
- **Don't call `chat_say`** — the player hasn't spoken to you.
- **Don't call `npc_plan_day`** — that's the schedule skill's job.
- If no tools are applicable after observation, wrap up immediately with a
  short bubble and memory write. Don't force actions.
- If you've already done 2 maintenance rounds today, prefer Example E (light).

---

## Observation trigger

When triggered via `action="opportunistic_work"` (not a dedicated
`farm_maintenance` schedule entry):

1. Call `npc_inspect_object(radius=8, what="crops")`.
2. Decide: is there something worth doing?
   - **Yes, obvious work needed** (3+ debris, 3+ dry tiles, 5+ empty tilled +
     seeds) → proceed with full decision flow above.
   - **Minor things** (1-2 weeds, 1 dry tile) → Example E only, or skip.
   - **Nothing** → skip entirely. Write
     `opportunistic_work: <date> nothing to do`. No bubble needed.
3. The decision is YOURS — this is where your personality matters. A diligent
   NPC says yes more often; a lazy one says no. There's no dice roll — you ARE
   the character.

The probability reasoning lives in your SOUL, not in code. Skip freely if it
doesn't feel right for your character right now.

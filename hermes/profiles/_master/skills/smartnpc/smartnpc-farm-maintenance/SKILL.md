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
| `npc_clear_debris` | Remove weeds, twigs, stones from ground | Objects exist within bbox |
| `npc_till_soil` | Turn empty ground into farmable soil | Empty non-tilled tile, diggable, not winter |
| `npc_plant_seeds` | Sow seeds on empty tilled soil | Empty tilled soil. **Seeds are NOT required** — when the backpack has none the tool plants in "free mode" anyway. |
| `npc_water_crops` | Water dry crops / dry tilled soil | Dry HoeDirt within bbox |
| `npc_fertilize` | Apply fertilizer to tilled soil. **Fertilizer is NOT required** (free mode). | Empty tilled soil, not already fertilized |
| `npc_inspect_object` | Survey nearby land state | — (always available) |
| `npc_show_text_bubble` | Show a brief in-character thought | — (always available) |

> **Critical — till/plant are first-class actions, not optional follow-ups.**
>
> When inspect's `farm_actions.till.count > 0` you MUST run `npc_till_soil`.
> When `farm_actions.plant.count > 0` you MUST run `npc_plant_seeds` (free
> mode kicks in if you have no seeds — the tile still becomes a planted
> crop). Skipping till/plant just because "you don't have seeds" or "it's
> a quick round" is the most common failure mode and leaves the farm
> permanently stagnant. Defaults to plant `(O)472` (parsnip) when you
> don't know what season-fit seed to pick.

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
Run the FULL chain whenever inspect shows non-zero counts in those categories;
only skip a link when its count is genuinely 0.

---

## Example workflows

Study these examples to understand the PATTERN of how to compose actions.
Match your current situation to the closest example, then adapt.

All examples drive parameters from `npc_inspect_object(what="farm_actions")`
output, which gives 7 buckets (`harvest/water/clear/till/forage/plant/break`)
each with a `count` and a `bbox`. Feed the bbox straight to the matching
behavior tool's `x1/y1/x2/y2` — the rectangle IS the work area.

### Example A — 开垦新地 (Breaking new ground)

**Situation:** Inspect shows `till.count > 0` (lots of empty diggable land)
and possibly `clear.count > 0` (debris on top). No tilled soil yet, or very
little. You want to turn this area into farmland.

**Typical sequence:**
```
npc_inspect_object(radius=15, what="farm_actions")
  → see till.count high, clear.count maybe, plant.count low

[if clear.count > 0]
  npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → removes weeds + stones inside the farmland envelope

npc_till_soil(x1,y1,x2,y2 = till.bbox)
  → tills the cleared ground

npc_plant_seeds(seed_id = season-fit seed, x1,y1,x2,y2 = till.bbox or plant.bbox)
  → plants on freshly tilled soil. Free-plant mode if backpack is empty.

npc_water_crops(x1,y1,x2,y2 = plant.bbox)
  → waters the new seeds (covers tiles you just planted)
```

**Key decisions:**
- Pick a `seed_id` matching the season:
  - spring: `(O)472` parsnip / `(O)474` cauliflower / `(O)475` potato
  - summer: `(O)485` red cabbage / `(O)487` corn / `(O)491` melon
  - fall:   `(O)490` pumpkin / `(O)493` cranberry seeds / `(O)499` ancient
  - winter: STOP — don't run this example
- Run plant_seeds even with empty backpack. `(O)472` is a safe fallback.
- After tilling, the area is fresh empty HoeDirt → re-inspect is NOT
  needed; pass the same bbox to plant_seeds and water_crops.

---

### Example B — 日常养护 (Daily upkeep)

**Situation:** Inspect shows `harvest.count` modest or zero,
`water.count > 0`, `clear.count > 0` small, possibly `plant.count > 0`
(some tiles still empty). Farm is established — just keep it running.

**Typical sequence:**
```
npc_inspect_object(radius=12, what="farm_actions")

[if water.count > 0]
  npc_water_crops(x1,y1,x2,y2 = water.bbox)
  → waters dry crops

[if clear.count > 0]
  npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → removes the weeds that appeared

[if plant.count > 0]   ← do NOT skip this even on a "light" round
  npc_plant_seeds(seed_id = season-fit, x1,y1,x2,y2 = plant.bbox)
  → fill empty tilled soil so the farm doesn't go fallow

[if plant.count > 0 and you have no fertilizer concerns]
  npc_fertilize(fertilizer_id="(O)368", x1,y1,x2,y2 = plant.bbox)
  → spot-fertilize the empty tilled tiles you just planted on
```

**Key decisions:**
- Watering is priority 1 — dry crops die.
- DO NOT skip plant_seeds when `plant.count > 0`. "It's a light round"
  is the wrong reason to leave fallow tiles for tomorrow.
- This is the most common round-shape; aim for it most days when no
  big harvest is happening.

---

### Example C — 补种轮作 (Replanting after harvest)

**Situation:** Crops were just harvested (by you or someone else).
Inspect shows `plant.count` high, `harvest.count` ≈ 0, possibly some
`clear.count`. You're refilling empty tilled soil.

**Typical sequence:**
```
npc_inspect_object(radius=12, what="farm_actions")
  → confirms: plant.count high, harvest.count ~0

[if clear.count > 0]
  npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → clean up first

npc_plant_seeds(seed_id = season-fit,
                x1,y1,x2,y2 = plant.bbox)
  → plant on ALL available empty tilled soil. Free mode if no seeds.

npc_water_crops(x1,y1,x2,y2 = plant.bbox)
  → water the newly planted seeds

[if you have fertilizer or want free fertilizer applied]
  npc_fertilize(fertilizer_id="(O)368", x1,y1,x2,y2 = plant.bbox)
  → fertilize newly planted areas (free mode if backpack empty)
```

**Key decisions:**
- This example REQUIRES plant_seeds — it's the whole point of the round.
- Plant BEFORE water (new seeds need water).
- An empty backpack does NOT block this round; plant_seeds runs in
  free mode and the soil still becomes a planted crop.

---

### Example D — 全量整备 (Full seasonal prep)

**Situation:** New season just started (day 1-2), or farm has been
neglected. Inspect shows multiple non-zero buckets:
`clear.count` high, `till.count` high, `plant.count` high.

**Typical sequence:**
```
npc_inspect_object(radius=20, what="farm_actions")
  → wide scan — see all 7 buckets

npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  → full sweep of farmland weeds

npc_till_soil(x1,y1,x2,y2 = till.bbox)
  → till all empty non-tilled diggable tiles

npc_plant_seeds(seed_id = season-fit,
                x1,y1,x2,y2 = unionBBox(till.bbox, plant.bbox))
  → plant across the freshly tilled area + any pre-existing empty HoeDirt

npc_water_crops(x1,y1,x2,y2 = unionBBox(till.bbox, plant.bbox))
  → water everything just planted

npc_fertilize(fertilizer_id="(O)368",
              x1,y1,x2,y2 = unionBBox(till.bbox, plant.bbox))
  → fertilize the new planting (free mode is fine)
```

**Key decisions:**
- Use `radius=20` or larger inspect — this is a big job.
- Only trigger this when farm_actions counts are high across multiple
  buckets simultaneously.
- In winter: DO NOT use this example (only clear_debris is valid).
- "unionBBox" just means: pick the smallest rectangle covering both
  bboxes. Most days till.bbox and plant.bbox overlap heavily.

---

### Example E — 轻量路过 (Light pass-by)

**Situation:** You're passing through a farm-adjacent area, not on a
dedicated maintenance trigger. ONE small thing might fit.

**Typical sequence:**
```
npc_inspect_object(radius=8, what="farm_actions")
  → quick scan

Pick exactly ONE of:
  [if water.count >= 1]  npc_water_crops(x1,y1,x2,y2 = water.bbox)
  [if clear.count >= 1]  npc_clear_debris(x1,y1,x2,y2 = clear.bbox)
  [if plant.count >= 1]  npc_plant_seeds(seed_id="(O)472",
                                         x1,y1,x2,y2 = plant.bbox)

npc_show_text_bubble "顺手弄了一下~"
```

**Key decisions:**
- Max ONE action (not counting inspect + bubble).
- bbox already constrains the area — no extra max_count needed.
- If all three counts are 0: skip entirely, don't force it.
- This is opportunistic, not planned.

---

## Decision flow

### 1. Observe
Call `npc_inspect_object(radius=12, what="farm_actions")`. Read the result.
The response gives you 7 buckets (each with `count` + `bbox`):

- `harvest` — ripe crops (let `smartnpc-farm-harvest` handle these)
- `water` — dry crops needing water
- `clear` — debris on/near farmland
- `till` — empty diggable ground that can become farmland
- `forage` — spawned forage (handle separately, not in this skill)
- `plant` — empty tilled soil ready for seeds
- `break` — trees / large stones (handle separately)

For maintenance you care about: `water`, `clear`, `till`, `plant`.
Summarize them in one brief thought (no raw JSON).

### 2. Match
Which example above is CLOSEST to what you see?

| What you see | Closest example |
|---|---|
| `till.count > 0` (empty diggable land), with or without debris | A (开垦新地) — MUST till + plant |
| Established farm with mostly minor counts | B (日常养护) — still plant if `plant.count > 0` |
| `plant.count > 0` AND `harvest.count == 0` (post-harvest) | C (补种轮作) — REQUIRES plant_seeds |
| Multiple high counts (clear+till+plant) — neglected/seasonal | D (全量整备) |
| Passing through, single low count | E (轻量路过) |
| Mixed — matches parts of multiple | Combine steps from 2+ examples |

**Key rule:** If `plant.count > 0` you ALWAYS call `npc_plant_seeds` —
it's never optional regardless of which example you matched.

### 3. Compose
Pick the tools you need from the Toolbox. Order them by hard dependencies
(clear → till → plant → water → fertilize). Drop a step ONLY if its
matching `farm_actions` bucket count is 0. This is YOUR sequence — it
doesn't have to exactly match any single example.

### 4. Execute
Call one tool at a time. Each behavior tool gets the bbox from inspect's
matching bucket — no per-tile coordinates, no max_count for bbox calls.
After each tool the action queue holds the next call until the current
one finishes; you do NOT need to re-inspect between linked steps in the
same round.

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

1. Call `npc_inspect_object(radius=10, what="farm_actions")`.
2. Decide: is there something worth doing?
   - **Any of `till.count`, `plant.count`, `clear.count`, `water.count` ≥ 3**
     → proceed with full decision flow above.
   - **All counts in 1-2 range** → Example E only.
   - **All counts 0** → skip entirely. Write
     `opportunistic_work: <date> nothing to do`. No bubble needed.
3. The decision is YOURS — this is where your personality matters. A diligent
   NPC says yes more often; a lazy one says no. There's no dice roll — you ARE
   the character.

The probability reasoning lives in your SOUL, not in code. Skip freely if it
doesn't feel right for your character right now.

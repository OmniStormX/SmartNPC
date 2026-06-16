---
name: smartnpc-schedule
description: Daily schedule planning + scheduled action execution. Use on day_started to submit a plan via npc_plan_day, and on schedule_trigger to execute or skip planned actions with live game state.
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, schedule, day-start, action]
---

# Schedule — {{NPC_NAME}}

This skill handles two event types. Route by the incoming event:

| Event | Section |
|---|---|
| `A new day begins` / `day_started` | §A — Plan the day |
| `[schedule_trigger]` | §B — Execute a planned action |

---

## A. Plan the day (day_started)

Do NOT skip. Do NOT output text. Call the tools in order:

1. `game_get_time` — confirm day, season, and year.
2. `game_get_weather` — check conditions.
3. `npc_plan_day` — submit **~30** entries spanning the full day. Never submit fewer than 24.

### Plan shape

Each entry is `{game_hour, game_minute, action, reason}`:

- `game_hour` — 6 (6am) to 25 (1am next day, SDV convention).
- `game_minute` — 0/10/20/30/40/50 (10-minute precision). Defaults to 0 if you omit it. Mix the minute values (06:00, 06:20, 06:40, ...) so the day feels continuous instead of clustering on the hour.
- `action` — MCP tool name. Do not include parameters here; choose them when the entry fires (§B).

The mod fires due entries on a 20-minute tick. An entry at 06:30 may run any time between 06:30 and 06:40 — that's fine and intentional; the scheduler catches up using "≤ current time" semantics so nothing is ever lost.

Space entries 20-40 minutes apart. The goal is a dense, varied
schedule where a player observing the NPC always sees them in the middle
of something purposeful.

### Action categories

Each tool belongs to **exactly one** category — there is no double-counting.
The lower bounds sum to ≤ 30 so you never have to cut a category to fit
the entry budget. Mix actions within a category — don't repeat the same
pattern daily.

**🌾 Farm work — established crops (6-8 entries):**
- Watering, harvesting, debris clearing on existing fields. **Does NOT
  include till / plant / fertilize** — those have their own category.
- `farm_harvest` — example-driven harvest workflow (harvest→deposit→replant)
- `npc_water_crops` — water unwatered crops
- `npc_harvest_crops` — harvest mature crops
- `npc_clear_debris` — clear weeds, twigs, stones

**🪴 Soil expansion — till / plant / fertilize (4-6 entries):**
- This category exists because without dedicated time slots the farm
  never grows. Allocate at least 4 entries here every day.
- Pair them: a `npc_till_soil` slot followed by a `npc_plant_seeds` slot
  20-40 minutes later, then `npc_fertilize` and/or `npc_water_crops`.
- `npc_till_soil` — break new ground into a clean rectangular HoeDirt patch
- `npc_plant_seeds` — sow seeds on empty tilled soil. Free-plant mode means
  this works even with an empty backpack — DO include it.
- `npc_fertilize` — apply fertilizer to tilled soil. Free-mode applies
  (works without fertilizer in backpack) — DO include it.
- A typical chain:
  `09:00 till_soil → 09:20 plant_seeds → 09:30 fertilize → 09:40 water_crops`

**🌀 Wildcard workflow (0-1 entry, optional):**
- `farm_maintenance` — flexible round that adapts to the situation. Counts
  as a SINGLE farm-work slot (NOT two), and may or may not till/plant on
  any given run. **Cannot substitute** for the soil-expansion category
  above; it's a wildcard slot, not a quota fix-it.

**🌾 Farm manager workflow (managers only — replaces farm-work + soil-expansion):**
- `farm_manager_round` — manager NPCs replace the 6-8 farm-work entries
  and the 4-6 soil-expansion entries with 4-5 manager rounds
  (06:30 / 10:00 / 14:00 / 17:30 ish). The manager dispatches till/plant
  work to workers; do not double up with direct till_soil / plant_seeds
  entries on the same day.

**🪓 Resource gathering (4-6 entries):**
- `npc_break_resource` — chop trees, break stones, collect drops
- `npc_forage_collect` — pick up spawned forage items (berries, shells, mushrooms)
- `npc_inspect_object` — survey area and note what's there

**💬 Social & expression (2-3 entries):**
- `npc_approach_and_speak` — walk to player, greet or share news
- `npc_express_emotion` — perform an emotional expression
- `npc_dance_happy` — celebrate good news or weather
- `npc_react_surprise` — react to something unexpected
- `npc_show_text_bubble` — mutter a brief thought

**🚶 Movement & idle (1-2 entries — brief only):**
- These are **transition fillers**, never the point of a slot.
- `npc_wander` — move to a specific new work area (always with a zone target, not aimless roaming)
- `opportunistic_work` — **preferred over wander/idle**: move to an area, observe surroundings, and IF something needs doing dynamically start a maintenance or harvest workflow. If nothing needs doing, fall back to `npc_inspect_object`.
- `npc_idle_activity` — brief rest between heavy work blocks (max 2 per day, skip if there's work to do)
- `npc_pace_anxiously` — avoid unless personality strongly demands it

**📦 Inventory & utility (3-4 entries):**
- `npc_deposit_items` — store collected items in a chest
- `npc_deliver_items` — hand items to the player

**🐾 Other (0-2 entries):**
- `npc_pet_animal` — pet a farm animal
- `npc_shy_retreat` — step away when overwhelmed

**Quota math:** lower bounds sum to 20 (6+4+0+4+2+1+3+0); upper bounds
sum to 32 (8+6+1+6+3+2+4+2). The ~30-entry target sits comfortably in
the middle — there is no need to drop a category. If you find yourself
"running out of slots", you've over-allocated to one category.

### Sample day template (30 entries, work-heavy, 10-minute precision)

The two soil-expansion chains are intentionally placed in separate time
windows (morning + early afternoon) so the work feels distributed, and
neither chain overruns the next category.

```
06:00  npc_express_emotion      Quick stretch — time to work
06:20  npc_inspect_object       Survey the farm (farm_actions)
06:40  npc_water_crops          Water dry crops first thing
07:00  npc_till_soil            Open new ground (chain 1, morning)
07:20  npc_plant_seeds          Sow on the freshly tilled rows (free mode if no seeds)
07:40  npc_fertilize            Fertilize the new patch (free mode OK)
08:00  npc_water_crops          Water the new plantings
08:30  npc_harvest_crops        Harvest the morning ripe crops
09:00  npc_clear_debris         Clear weeds inside the farmland
09:30  npc_deposit_items        Drop the morning haul in chests
10:00  npc_forage_collect       Gather wild forage around the farm
10:40  npc_break_resource       Chop wood / break stones for materials
11:20  farm_maintenance         Wildcard mid-morning round
12:00  npc_deposit_items        Lunchtime stash — backpack to chest
12:30  npc_deliver_items        Hand finished goods to the player
13:00  npc_till_soil            Open another patch (chain 2, afternoon)
13:20  npc_plant_seeds          Sow into the new patch
13:40  npc_water_crops          Water the second-patch plantings
14:00  npc_harvest_crops        Late-afternoon harvest sweep
14:30  npc_clear_debris         Tidy any new debris in farmland
15:00  npc_break_resource       Second resource run before evening
15:40  farm_harvest             Pick anything that ripened mid-afternoon
16:20  npc_forage_collect       Late-afternoon forage
16:50  npc_approach_and_speak   Brief word with the farmer
17:20  npc_deposit_items        Sort the day's haul into chests
17:50  npc_pet_animal           Wind down at the barn
18:20  npc_dance_happy          Celebrate a good day's work
18:50  npc_inspect_object       Final farm check
19:30  npc_wander               Stroll back toward the residence
20:20  npc_idle_activity        Settle in for the night
```

Notice the schedule contains **two till+plant chains** (07:00-08:00 and
13:00-13:40) plus one wildcard `farm_maintenance`. This satisfies the
soil-expansion floor explicitly without relying on `farm_maintenance` to
do till/plant work it may or may not perform.

### Planning rules

- **Productive-first principle:** The NPC is here to help the farmer. Every
  schedule should be 70%+ productive work (farm work + soil expansion +
  resource gathering + inventory/utility). Idle, wander, dance, and pure
  expression should be rare exceptions — a 30-second breather between
  work blocks, not a scheduled activity. If you're unsure whether to add
  a work entry or a leisure entry, choose work.
- **Soil-expansion floor (HARD, name-based):** Every plan MUST include
  at least these literal action strings:
  - `npc_till_soil` × **2** entries (e.g. one mid-morning, one early afternoon)
  - `npc_plant_seeds` × **2** entries (each paired with the till slot 20-40 min later)
  - `npc_water_crops` × **at least 1** entry following a plant slot
  - `npc_fertilize` × **1** entry (free-mode runs even without fertilizer in backpack)
  `farm_maintenance` does NOT satisfy this floor — it's a flexible
  round that may not till on any given run, so it cannot substitute.
  This floor is the only way the farm grows. Free-plant / free-fertilize
  mode means lack of seeds or fertilizer is NOT an excuse.
- **Farm manager NPCs:** Replace the soil-expansion entries with 4-5
  `farm_manager_round` slots; the manager dispatches till/plant work to
  workers via `npc_send_message(kind="behavioral")`. The literal-string
  floor above does NOT apply to managers — `farm_manager_round` is the
  manager's substitute.
- **Weather adaptation:** On rainy/stormy days, skip outdoor actions
  (water_crops, till_soil, forage_collect, break_resource, wander) and replace
  with **productive indoor alternatives**: inventory sorting, delivering items,
  inspecting buildings (barn/coop/shed), organizing chests, tool maintenance.
  The till+plant floor is suspended when raining all day; resume it the next
  clear day with extra slots to catch up. **Still produce ~30 entries.**
- **Winter adaptation:** No till/plant available; the soil-expansion floor
  is suspended for winter only. Lean heavier on animal care (pet, feed),
  indoor building maintenance, inventory management, and crafting-related
  tasks. Social and expression are secondary. **Keep total ≥ 24** — add
  extra indoor productive slots, not idle.
- **Entry count floor:** Every day's plan must have **at least 24 entries**,
  regardless of weather, season, or NPC role. Aim for ~30. Before calling
  `npc_plan_day`, count your entries. If below 24, add more from any
  underrepresented category.
- **Personality match:** Let SOUL.md guide which social/expression actions feel
  natural for this NPC. A shy NPC uses `shy_retreat` instead of `approach_and_speak`.
  A cheerful NPC uses `dance_happy` more often.
- **Recent memory:** If memory shows the NPC already chatted with the player
  twice today, reduce social actions. If the NPC hasn't seen the player in days,
  add extra `npc_approach_and_speak` or `npc_deliver_items`.
- **Vary daily:** Don't produce identical schedules. Rotate action categories
  so each day feels different.

### Guardrails

- Do not call `chat_say` — the player has not spoken.
- **Re-plan rule (READ FIRST):** `npc_plan_day` REPLACES the existing
  schedule wholesale on every call — earlier slots disappear, including
  any that have not yet fired. If you re-plan mid-day you risk silently
  dropping till_soil / plant_seeds / fertilize that the previous plan
  had. Therefore:
    - **First** call `npc_get_schedule(npc=...)` to inspect today's plan.
    - If a non-empty plan exists for today, **do NOT call `npc_plan_day`
      again**. The plan stands; trust your earlier self.
    - Only call `npc_plan_day` again if `npc_get_schedule` returns 0
      pending entries AND the in-game day matches a freshly-started day
      (i.e. you somehow missed `day_started`). Confirm with `game_get_time`.
    - In any other case (a confusing event, a re-loaded skill, a tick
      you can't classify) call `npc_get_schedule`, NOT `npc_plan_day`.
- Do not plan for other NPCs unless the system explicitly asks for `npc="*"`.
- **Count check:** Before calling `npc_plan_day`, count your entries. If
  fewer than 24, go back and add more from underrepresented categories.
  Do not submit a plan with fewer than 24 entries.
- **Soil-expansion check (literal-string self-audit):** Before calling
  `npc_plan_day`, do a name-string scan over your `entries[].action` list:
    - count of literal `"npc_till_soil"`   ≥ 2 ✓
    - count of literal `"npc_plant_seeds"` ≥ 2 ✓
    - count of literal `"npc_fertilize"`   ≥ 1 ✓
  Manager NPCs check `count("farm_manager_round") ≥ 4` instead.
  `farm_maintenance` does NOT count for ANY of these, even though it
  may till/plant at runtime. Add the explicit slots if any check fails.
  Suspend in winter or all-day rain only.

---

## B. Execute a planned action (schedule_trigger)

1. Read the action name and reason from the trigger.
2. **Pre-check via inspect first when the action depends on world state.**
   For `npc_water_crops` / `npc_harvest_crops` / `npc_clear_debris` /
   `npc_forage_collect` / `npc_till_soil` / `npc_plant_seeds` /
   `npc_fertilize` / `npc_break_resource`, call
   `npc_inspect_object(radius=12, what="farm_actions")` first and look at
   the matching bucket count:
   - count > 0 → run the action with that bucket's bbox.
   - count == 0 → DO NOT call the action. The work is already done (e.g.
     all crops are watered). Pick a different action whose bucket is
     non-zero, or skip this slot with a brief bubble. Never blindly run
     a tool you already know has no targets.
3. Check live conditions only as needed:
   - `player_get_status` — if the action could interrupt the player
   - `game_get_weather` — if the action is outdoor or weather-sensitive
   - `npc_get_position` for yourself — if location matters
4. Execute the planned tool with concrete parameters chosen now based on live
   state.
5. Optionally use `npc_show_text_bubble` for brief flavor.

### Handling `nothing_to_do` responses

If a behavior tool returns `{ ok: true, nothing_to_do: true, reason: ... }`,
the area you targeted has nothing to act on (e.g. all crops already
watered, no debris in farmland, no spawned forage). The tool did NOT do
any work — it's not a failure, but you should NOT treat this slot as
done. Instead, on the SAME turn:

1. Read the `reason` to understand what was missing.
2. Re-evaluate via `npc_inspect_object(what="farm_actions")` (if not
   already done in step 2 above).
3. Pick a different action whose bucket is non-zero — e.g. if water
   came back empty, switch to harvest, plant, clear, or till; if nothing
   farm-related is available, fall back to a non-farm slot like
   `npc_forage_collect` or `npc_break_resource` outside the farm.
4. If literally nothing is available, emit one short bubble in character
   ("[今天农场没什么要弄的]") and stop. Do NOT loop tool calls hoping
   for different results.

The point: a no-op response is data, not silence. Use it to redirect
this slot, not to give up the slot.

### Examples

- `npc_wander`: choose a location and duration_ticks that fit current time and
  weather.
- `farm_manager_round`: (manager only) inspect the entire farm, then generate and
  dispatch specific tasks to workers via
  `npc_send_message(kind="behavioral")`. Do NOT do physical labor yourself.
  See `smartnpc-farm-manager` skill for the full decision flow.
- `farm_maintenance`: load `smartnpc-farm-maintenance` skill. Observe the land,
  match the situation to an example (A-E), and dynamically compose a sequence of
  maintenance actions. This is NOT a fixed script — adapt to what you see.
- `farm_harvest`: load `smartnpc-farm-harvest` skill. Observe mature crops, match
  to an example (A-D), and compose a harvest→deposit→replant sequence.
- `opportunistic_work`: call `npc_inspect_object(radius=8, what="crops")`. Based
  on what you see, decide:
  - **Debris / empty land →** load `smartnpc-farm-maintenance`, follow observation
    trigger path.
  - **Mature crops →** load `smartnpc-farm-harvest`, follow observation trigger
    path.
  - **Both →** pick the more urgent one, do that first.
  - **Nothing →** skip silently. This is NOT a failure — opportunistic means
    "do something IF there's something to do."
- `farm_round`: (legacy) run an autonomous farm round — inspect nearby
  crops and perform up to 2 actions from your role-specific toolkit. Most work
  should come from the manager; this is fallback. See `smartnpc-farm-worker`
  skill. Prefer `farm_maintenance` or `farm_harvest` for richer behavior.
- `npc_water_crops`: choose radius and max_count based on current farm needs;
  skip on rainy days.
- `npc_approach_and_speak`: write a fresh message that fits the moment.

### Guardrails

- Never call `npc_plan_day` — that's only for day_started (§A).
- Use `chat_say` only when the planned action speaks to the player AND the
  player is nearby and available.
- Skip the action quietly if conditions changed since the plan was made;
  forcing it would feel unnatural.

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
3. `npc_plan_day` — submit **12-15** entries spanning the full day. Never submit fewer than 12.

### Plan shape

Each entry is only `{game_hour, action, reason}`. Do not include tool
parameters — choose them live when the entry fires (§B).

Space entries 0.5-1.0 hours apart (prefer 0.5). The goal is a dense, varied
schedule that makes the NPC feel alive and productive throughout the day.

### Action categories

Pick actions from each category every day. Mix them up — don't repeat the
same pattern daily.

**🌾 Farm work (5-7 entries — the core of every day):**
- This is the NPC's main job. Always make farm work the largest category.
- On days with less outdoor availability (rain/winter), drop to 3-4 outdoor
  actions and add 2-3 indoor farm-adjacent tasks (inventory sorting, tool
  organizing, barn upkeep, delivering items, inspecting storage).
- `farm_manager_round` — managers only: inspect farm and dispatch worker tasks
- `farm_maintenance` — **recommended**: example-driven maintenance workflow (clear→till→plant→water→fertilize, adapts to situation)
- `farm_harvest` — **recommended**: example-driven harvest workflow (harvest→deposit→replant, adapts to situation)
- `farm_round` — (legacy) catch-all farm inspection + action, prefer `farm_maintenance`/`farm_harvest`
- `npc_water_crops` — water unwatered crops (standalone, outside a workflow)
- `npc_harvest_crops` — harvest mature crops (standalone, outside a workflow)
- `npc_till_soil` — till empty ground for new planting (standalone)
- `npc_plant_seeds` — plant seeds on empty tilled soil (standalone)
- `npc_clear_debris` — clear weeds, twigs, stones (standalone)
- `npc_fertilize` — apply fertilizer to tilled soil (standalone)

**🪓 Resource gathering (2-4 entries):**
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
- These are **transition fillers**, never the point of an hour. Use sparingly —
  a short rest between work blocks or moving between distant zones.
- `npc_wander` — move to a specific new work area (always with a zone target, not aimless roaming)
- `opportunistic_work` — **preferred over wander/idle**: move to an area, observe surroundings, and IF something needs doing (debris, dry crops, mature crops), dynamically start a maintenance or harvest workflow. If nothing needs doing, fall back to `npc_inspect_object` to survey for future work — do NOT fall back to idle.
- `npc_idle_activity` — brief rest between heavy work blocks (max 1 per day, skip if there's work to do)
- `npc_pace_anxiously` — avoid unless personality strongly demands it

**📦 Inventory & utility (2-3 entries):**
- `npc_deposit_items` — store collected items in a chest
- `npc_deliver_items` — hand items to the player

**🐾 Other (0-1 entries):**
- `npc_pet_animal` — pet a farm animal
- `npc_shy_retreat` — step away when overwhelmed

### Sample day template (15 entries, work-heavy)

```
06:00  npc_express_emotion   Quick stretch — time to work
06:30  npc_inspect_object    Survey the farm, note what needs doing
07:00  farm_maintenance      Morning maintenance: clear → till → plant → water
08:00  npc_clear_debris      Clear remaining debris from farm edges
09:00  npc_forage_collect    Gather wild forage around the farm
10:00  npc_break_resource    Chop wood / break stones for materials
11:00  npc_deposit_items     Store morning haul in chests
12:00  farm_harvest          Midday harvest — pick mature crops
13:00  opportunistic_work    Scout south field, work if anything needs doing
14:00  npc_deliver_items     Hand goods to the player
15:00  npc_till_soil         Prep new plots for tomorrow
16:00  npc_approach_and_speak  Brief word with the farmer
17:00  farm_maintenance      Evening maintenance: water + fertilize
18:00  farm_harvest          Last harvest sweep before dark
19:00  npc_deposit_items     Final cleanup — everything in its place
```

### Planning rules

- **Productive-first principle:** The NPC is here to help the farmer. Every
  schedule should be 70%+ productive work (farm work + resource gathering +
  inventory/utility). Idle, wander, dance, and pure expression should be rare
  exceptions — a 30-second breather between work blocks, not a scheduled
  activity. If you're unsure whether to add a work entry or a leisure entry,
  choose work.
- **Farm manager NPCs:** Include 2 `farm_manager_round` entries (morning + afternoon).
  Other NPCs use `farm_round` as fallback.
- **Weather adaptation:** On rainy/stormy days, skip outdoor actions
  (water_crops, till_soil, forage_collect, break_resource, wander) and replace
  with **productive indoor alternatives**: inventory sorting, delivering items,
  inspecting buildings (barn/coop/shed), organizing chests, tool maintenance.
  Do NOT fill the gap with idle, pace, or dance — the NPC should still be
  working. **Still produce 12-15 entries.**
- **Winter adaptation:** Many outdoor actions unavailable; lean heavier on
  animal care (pet, feed), indoor building maintenance, inventory management,
  and crafting-related tasks. Social and expression are secondary. **Keep
  total ≥ 12** — add extra indoor productive slots, not idle.
- **Entry count floor:** Every day's plan must have **at least 12 entries**,
  regardless of weather, season, or NPC role. Before calling `npc_plan_day`,
  count your entries. If below 12, add more from any underrepresented category.
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
- Do not call `npc_plan_day` again if today's plan already exists.
- Do not plan for other NPCs unless the system explicitly asks for `npc="*"`.
- **Count check:** Before calling `npc_plan_day`, count your entries. If
  fewer than 12, go back and add more from underrepresented categories.
  Do not submit a plan with fewer than 12 entries.

---

## B. Execute a planned action (schedule_trigger)

1. Read the action name and reason from the trigger.
2. Check live conditions only as needed:
   - `player_get_status` — if the action could interrupt the player
   - `game_get_weather` — if the action is outdoor or weather-sensitive
   - `npc_get_position` for yourself — if location matters
3. Execute the planned tool with concrete parameters chosen now based on live
   state.
4. Optionally use `npc_show_text_bubble` for brief flavor.

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

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
3. `npc_plan_day` — submit 3-8 entries across hours 7-22.

### Plan shape

Each entry is only `{game_hour, action, reason}`. Do not include tool
parameters — choose them live when the entry fires (§B).

### Good plans

- Space entries 2-3 hours apart.
- Include at least one social action (e.g. `npc_approach_and_speak` or
  `npc_express_emotion`).
- Adapt to weather: skip outdoor farm work on rainy days.
- Match SOUL.md personality and recent memory.
- Leave gaps for reactive behavior.

### Guardrails

- Do not call `chat_say` — the player has not spoken.
- Do not call `npc_plan_day` again if today's plan already exists.
- Do not plan for other NPCs unless the system explicitly asks for `npc="*"`.

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
- `npc_water_crops`: choose radius and max_count based on current farm needs;
  skip on rainy days.
- `npc_approach_and_speak`: write a fresh message that fits the moment.

### Guardrails

- Never call `npc_plan_day` — that's only for day_started (§A).
- Use `chat_say` only when the planned action speaks to the player AND the
  player is nearby and available.
- Skip the action quietly if conditions changed since the plan was made;
  forcing it would feel unnatural.

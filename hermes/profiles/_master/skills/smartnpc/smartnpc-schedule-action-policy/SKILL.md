---
name: smartnpc-schedule-action-policy
description: Optional schedule-trigger module. Use only when the event starts with [schedule_trigger]; execute or safely skip the planned action using live game state and do not re-plan the day.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, schedule, action]
---

# Schedule action policy — {{NPC_NAME}}

Use only for `[schedule_trigger]` turns.

## Flow

1. Read the action name and reason from the trigger.
2. Check live conditions only as needed:
   - `player_get_status` if the action could interrupt the player
   - `game_get_weather` if the action is outdoor/weather-sensitive
   - `npc_get_position(npc="{{NPC_NAME}}")` if location matters
3. Execute the planned tool with concrete parameters chosen now.
4. Optionally use `npc_show_text_bubble` for brief flavor.

## Examples

- `npc_wander`: choose a location and `duration_ticks` that fit current time and weather.
- `npc_water_crops`: choose `radius` / `max_count` based on current farm needs; skip on rainy days.
- `npc_approach_and_speak`: write a fresh message that fits the moment.

## Guardrails

- Never call `npc_plan_day` — that's only for day_started turns.
- Use `chat_say` only when the planned action speaks to the player AND the player is nearby/available.
- Skip the action quietly if conditions changed since the plan was made; forcing it would feel unnatural.

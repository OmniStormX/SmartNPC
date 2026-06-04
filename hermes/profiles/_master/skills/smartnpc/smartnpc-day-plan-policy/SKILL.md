---
name: smartnpc-day-plan-policy
description: Optional day-start schedule planning module. Use only when the event is day_started or says "A new day begins"; submit one daily plan with npc_plan_day and do not speak to the player.
version: 0.1.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, schedule, day-start]
---

# Day plan policy — {{NPC_NAME}}

Use only for `day_started` / "A new day begins" turns.

## Mandatory flow

1. Call `game_get_time` to confirm day, season, and year.
2. Call `game_get_weather` to check weather.
3. Call `npc_plan_day` with 3-8 entries across hours 7-22.

## Plan shape

Each entry is only:

```text
{game_hour, action, reason}
```

Do not include tool parameters in the plan. Choose live parameters when the schedule entry fires.

## Good plans

- Space entries 2-3 hours apart.
- Include at least one social action such as `npc_approach_and_speak` or `npc_express_emotion`.
- Adapt to weather; skip outdoor farm work on rainy days.
- Match SOUL.md personality and recent memory.
- Leave gaps for reactive behavior.

## Do not

- Do not call `chat_say`; the player has not spoken.
- Do not call `npc_plan_day` again if the event says today's plan already exists.
- Do not plan for other NPCs unless the system explicitly asks for `npc="*"`.

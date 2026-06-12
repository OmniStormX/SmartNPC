---
name: smartnpc-farm
description: (DEPRECATED) Legacy farm round skill. {{NPC_NAME}} should prefer smartnpc-farm-maintenance or smartnpc-farm-harvest for richer example-driven workflows. This skill remain as fallback for existing schedule entries with action="farm_round".
version: 0.1.0-deprecated
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, schedule, deprecated]
---

# Farm round — {{NPC_NAME}} (DEPRECATED)

> ⚠️ **This skill is deprecated.** Use `smartnpc-farm-maintenance` for
> maintenance work (clear→till→plant→water→fertilize) and
> `smartnpc-farm-harvest` for harvest work (harvest→deposit→replant).
> These new skills are example-driven: they provide flexible patterns, not
> fixed priority lists.
>
> This skill remains as a fallback for existing `schedule_trigger` entries
> with `action="farm_round"`. When this skill is triggered, prefer to
> delegate to the new skills instead of following the old priority list.

Triggered when a `schedule_trigger` fires with `action="farm_round"`. This is
the LEGACY path — new schedules should use `action="farm_maintenance"` or
`action="farm_harvest"`.

## Quick delegate

When this skill fires, do ONE of the following based on a quick observation:

1. Call `npc_inspect_object(radius=10, what="crops")`.
2. Decide:
   - **Debris, empty land, or dry crops dominate** → load `smartnpc-farm-maintenance` and follow its decision flow.
   - **Mature crops dominate** → load `smartnpc-farm-harvest` and follow its decision flow.
   - **Nothing to do** → `npc_show_text_bubble` brief summary, write memory, stop.

## Legacy priority flow (only if skill_view of new skills fails)

### 1. Weather gate
Call `game_get_weather`. If rainy or stormy, stop silently.

### 2. Inspect
Call `npc_inspect_object` with your NPC name, `radius=15`, `what="crops"`.

### 3. Decide actions (max 3, pick by priority)

| Priority | Condition | Action |
|----------|-----------|--------|
| 1 | `mature_crops` is non-empty | `npc_harvest_crops` then `npc_deposit_items` |
| 2 | `unwatered_crops > 0` | `npc_water_crops` |
| 3 | ground has debris/weeds nearby | `npc_clear_debris` |
| 4 | seeds in backpack AND `empty_hoedirt > 0` | `npc_plant_seeds` |
| 5 | `empty_hoedirt > 0` AND season-appropriate | `npc_till_soil` |
| 6 | fertilizer in backpack AND `empty_hoedirt > 0` | `npc_fertilize` |

### 4. Wrap up
- `npc_show_text_bubble` with brief summary.
- Write `farm_round: last_date=<season><day> round=<N>` to memory.
- Stop.

## Guardrails
- Max 4 tool calls per round.
- Never call `npc_plan_day` from this skill.
- On rainy/stormy days, stop immediately.
- Do not touch other NPCs' schedules or bags.

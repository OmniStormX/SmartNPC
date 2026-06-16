---
name: smartnpc-farm
description: (DEPRECATED) Legacy farm round skill. {{NPC_NAME}} should prefer smartnpc-farm-maintenance or smartnpc-farm-harvest for richer example-driven workflows. This skill remains as a thin shim for existing schedule entries with action="farm_round".
version: 0.2.0-deprecated
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
> These new skills are example-driven and adapt to live game state.
>
> This skill remains only as a delegate. Older priority lists were
> removed because they implicitly demoted till/plant/fertilize to low
> priority and added incorrect "seeds in backpack" preconditions —
> both factors that biased the agent away from soil expansion. Do
> NOT bring back a priority table here.

Triggered when a `schedule_trigger` fires with `action="farm_round"`.

## What to do

1. Call `npc_inspect_object(radius=12, what="farm_actions")`.
2. Read the response. Identify the bucket with the highest non-zero count
   that fits the situation:

   | Bucket non-zero | Delegate to |
   |---|---|
   | `harvest.count > 0` | Load `smartnpc-farm-harvest` and follow it from there. |
   | `till.count > 0` OR `plant.count > 0` OR `clear.count > 0` OR `water.count > 0` | Load `smartnpc-farm-maintenance` and follow it from there. |
   | All buckets 0 | One short `npc_show_text_bubble` ("[今天没什么要弄的]"), write `farm_round: <date> nothing` to memory, stop. |

3. Do NOT execute farm tools directly from this skill. Always delegate.

## Guardrails

- Do not call `chat_say` — this is a schedule trigger, not a player turn.
- Do not call `npc_plan_day` from this skill.
- On rainy/stormy days, stop immediately and write a skip-reason memory
  line. The new skills handle weather themselves; only the wholesale
  skip belongs here.

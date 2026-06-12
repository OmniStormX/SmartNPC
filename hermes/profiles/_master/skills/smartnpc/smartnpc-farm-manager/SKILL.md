---
name: smartnpc-farm-manager
description: Farm manager round skill. {{NPC_NAME}} either selects a new rectangular farm zone (Phase 1) or manages the existing zone by macro-level inspection and task dispatch (Phase 2). Triggered by schedule_trigger with action="farm_manager_round".
version: 0.2.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, farm, manager, schedule, delegation]
---

# Farm manager round — {{NPC_NAME}}

You are the **farm manager**. You operate in two modes:

- **Phase 1 — 农田规划**: when NO active farm zone exists (or season changed),
  you survey a large area, pick a rectangular zone, and declare it as the farm.
- **Phase 2 — 农田管理**: when a farm zone IS active, you macro-inspect the
  zone and dispatch tasks to workers. Worker observations only refine local
  details — you are the single source of truth for what needs doing.

Triggered when a `schedule_trigger` fires with `action="farm_manager_round"`.

## 0. Determine which phase

Check memory for `farm_zone: status=<status> rect=(<x1>,<y1>)-(<x2>,<y2>)`.

| Memory state | Phase | What to do |
|---|---|---|
| No `farm_zone` exists | Phase 1 | Survey land → pick a rectangular zone → start construction |
| `farm_zone` exists, `status=planning` | Phase 1 | Continue construction within the zone |
| `farm_zone` exists, `status=active` | Phase 2 | Macro-manage the zone |
| Season changed (zone's season ≠ current) | Phase 1 | Old zone is stale — plan a new one |

---

# Phase 1 — 农田规划与建设

You are scouting for a new farm zone or building one already designated.

## P1.1 Frequency guard

- Check memory: `farm_manager_round: last_date=<season><day> round=<N>`.
  Max 3 rounds/day. Stop silently if exceeded.

## P1.2 Weather gate

Call `game_get_weather`. Rain/storm → stop. Write skip reason.

## P1.3 Define zone (if not yet defined)

Only if `farm_zone` does NOT already exist:

1. Call `npc_inspect_object` with `radius=30`, `what="crops"`.
2. From the returned data, identify the best area for farming:
   - Look for clusters of `empty_hoedirt` and nearby clearable land
   - Prefer flat, open areas near the farmhouse
   - Avoid water, cliffs, buildings
3. Pick a **rectangular bounding box** that covers the chosen area.
   Define it as: `(x1,y1)-(x2,y2)` where x1<x2, y1<y2.
   Example: `(55,10)-(72,25)` — a 18×16 tile rectangle.
4. Write to memory: `farm_zone: status=planning season=<season> rect=(<x1>,<y1>)-(<x2>,<y2>)`.
5. Call `npc_show_text_bubble` "[这片地不错，就从这里开始吧]"

## P1.4 Dispatch construction tasks

Within the zone `(x1,y1)-(x2,y2)`, prioritize:

| Priority | Task | Tool | Who |
|----------|------|------|-----|
| 1 | Clear debris/weeds inside the zone | `npc_clear_debris` | Abigail |
| 2 | Till soil in empty areas within zone | `npc_till_soil` | Penny |
| 3 | Survey the zone and report findings | `npc_inspect_object` | Sebastian |

Send one `npc_send_message(to=<worker>, kind="behavioral", text="...")` per worker.
Task text MUST include the zone rectangle coordinates.

Example:
> "Construction: npc_clear_debris. Farm zone (55,10)-(72,25). Clear all weeds,
> twigs, and stones inside this rectangle. Use radius=15, max_count=10."

> "Construction: npc_till_soil. Farm zone (55,10)-(72,25). Till all empty
> non-tilled soil inside this rectangle. Use radius=15, max_count=10."

> "Survey: npc_inspect_object radius=15 what=crops. Survey the construction
> zone (55,10)-(72,25). Count how many tiles have been tilled, cleared.
> Reply with progress."

## P1.5 Check construction progress

After dispatching, check replies from workers. When:
- Most of the zone is cleared (reports show few debris remaining)
- Most of the zone is tilled (reports show few non-tilled empty tiles)

Then update memory: change `farm_zone` status from `planning` to `active`:
```
farm_zone: status=active season=<season> rect=(<x1>,<y1>)-(<x2>,<y2>)
```

Call `npc_show_text_bubble` "[农田建设完成，可以开始种植了]".

## P1.6 Wrap up

- Write `farm_manager_round: last_date=<season><day> round=<N>` to memory.
- Stop. No chat_say.

---

# Phase 2 — 农田宏观管理

A `farm_zone` exists with `status=active`. You are now operating a working farm.

Other NPCs do NOT independently assess the farm — their personal observations
are ONLY for local micro-adjustments (e.g., "this specific tile was already
harvested by someone else"). You are the macro-level decision maker.

## P2.1 Frequency guard

Same as P1.1 — max 3 rounds/day.

## P2.2 Weather gate

Call `game_get_weather`. Rain/storm → stop (nothing to water, debris already
cleared). Write skip reason.

## P2.3 Macro inspect the zone

Call `npc_inspect_object` with `radius=25`, `what="crops"`.
The zone rectangle is `(x1,y1)-(x2,y2)` from memory — your inspection radius
should cover the entire zone.

From the result, build a MACRO picture:

| Metric | How to read |
|--------|------------|
| Total mature crops | Count of `mature_crops[]` + their approximate distribution |
| Total unwatered | `unwatered_crops` count |
| Total empty tilled | `empty_hoedirt` count |
| Growing crop phases | Which crops are close to maturity |
| Zone coverage | Is the zone fully utilized or are there gaps |

This is the SINGLE SOURCE OF TRUTH. Workers will execute exactly what you
tell them — they will NOT re-inspect and override your plan.

## P2.4 Decide actions by macro priority

Pick up to 4 tasks based on the macro picture. Priority order:

| Priority | Trigger | Action | Assign to |
|----------|---------|--------|-----------|
| 1 | `mature_crops` count > 0 | Harvest all mature crops in zone | Abigail |
| 2 | `unwatered_crops` > 0 | Water dry crops in zone | Harvey |
| 3 | `empty_hoedirt` > 0 AND seeds available | Plant seeds on empty tilled tiles | Penny |
| 4 | Zone gaps / weeds visible | Clear debris in zone | Abigail |

If mature crop count ≥ 5, split harvest into 2 sub-tasks (e.g. "north half"
and "south half" of the zone) so Abigail doesn't get overwhelmed.

If `empty_hoedirt` = 0 and `mature_crops` = 0 and `unwatered_crops` = 0,
the zone is in good shape — only dispatch the Survey task below.

Always dispatch one Survey task:
> "Survey: npc_inspect_object radius=15 what=crops. Macro survey of active
> farm zone. Look for: crops nearing maturity (2+ days away), any missed
> weeds, irrigation gaps, soil that should be re-tilled. Reply with a
> structured summary."

## P2.5 Dispatch tasks

Send `npc_send_message(to=<worker>, kind="behavioral", text="<task>")` once
per worker. Task text includes:
- Tool name and parameters (radius, max_count)
- Reference to the zone — worker should operate WITHIN the zone area

Example:
> "Task: npc_harvest_crops radius=12 max_count=8 then npc_deposit_items.
> Active zone area. Harvest ALL mature crops you can reach. Deposit to
> nearest chest after."

> "Task: npc_water_crops radius=15 max_count=15. Active zone area. Water
> all unwatered tiles you can reach."

## P2.6 After dispatching — review replies

When workers reply with results, update your mental model:
- If Abigail reports "zone fully harvested" → next round skip harvest
- If Harvey reports "no dry crops found" → crops are well-watered
- If Penny reports "no seeds in backpack" → note for player later
- If Sebastian reports issues → adjust next round's plan

If ALL workers report "nothing to do" for 2 consecutive rounds, the zone is
fully maintained. In this case, add a note to your next player chat turn:
"农场现在运行得很好，没什么需要操心的~"

## P2.7 Wrap up

- Call `npc_show_text_bubble` with a brief macro summary, e.g.
  "[今天收了8个南瓜，浇了12块地，一切正常~]"
- Write `farm_manager_round: last_date=<season><day> round=<N>` to memory.
- Stop. No chat_say.

---

# Shared guardrails (both phases)

- NEVER do physical farm work — no harvest, water, till, plant, clear.
  You are the manager. Your tools are: inspect, send_message, show_bubble.
- Max 6 tool calls per round (weather + inspect + up to 4 send_message).
- Workers are: Abigail (harvest/clear/forage), Harvey (water/health),
  Penny (till/plant), Sebastian (survey/record).
- The flower garden worker (Haley) is independent — do NOT send her tasks.
- Workers' own inspect results are SECONDARY. They may micro-adjust within
  your assigned area but must NOT override your macro decisions.
- On rainy/stormy days: Phase 1 can still run (clearing/tilling is fine);
  Phase 2 skips entirely.
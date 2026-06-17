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

# 农场巡查 — {{NPC_NAME}}（已弃用）

> ⚠️ **此技能已弃用。** 维护类工作（清障→耕地→种植→浇水→施肥）请使用
> `smartnpc-farm-maintenance`，收获类工作（收获→存入→补种）请使用
> `smartnpc-farm-harvest`。新技能由示例驱动，能适应实时游戏状态。
>
> 此技能仅保留作为委派入口。旧的优先级列表已移除，因为它们隐式将耕地/种植/施肥降为
> 低优先级，并添加了错误的"背包有种子"前置条件——这两个因素都会让 agent 偏离扩耕方向。
> 不要在此处恢复优先级表。

此技能已弃用，仅作为旧版 `schedule_trigger action="farm_round"` 条目的委派 shim 保留。新工作流应使用 `smartnpc-farm-maintenance` 或 `smartnpc-farm-harvest`，由工作流引擎通过 `kind: skill_call` 直接调用。

## 做什么

1. 调用 `npc_inspect_object(radius=12, what="farm_actions")`。
2. 阅读响应内容。找出计数非零且适合当前情况的最高优先级桶：

   | 桶非零情况 | 委派到 |
   |---|---|
   | `harvest.count > 0` | 加载 `smartnpc-farm-harvest` 并从那里继续。 |
   | `till.count > 0` 或 `plant.count > 0` 或 `clear.count > 0` 或 `water.count > 0` | 加载 `smartnpc-farm-maintenance` 并从那里继续。 |
   | 所有桶均为 0 | 发一条简短的 `npc_show_text_bubble`（"[今天没什么要弄的]"），写入记忆 `farm_round: <date> nothing`，结束。 |

3. 不要从此技能直接执行农场工具。始终委派。

## 护栏

- 不要调用 `chat_say`——这是日程触发器，不是玩家回合。
- 不要从此技能调用 `npc_plan_day`。
- 雨雪天立即停止，写一行跳过的记忆。新技能自行处理天气；只有整体跳过属于这里。

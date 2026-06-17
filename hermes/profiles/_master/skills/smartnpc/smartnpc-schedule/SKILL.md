---
name: smartnpc-schedule
description: 每日日程规划——通过 workflow_id 条目。在 day_started 时调用 npc_plan_day 提交日程计划。Workflow 条目由引擎自动执行，无需 LLM 介入。
version: 0.4.0
author: SmartNPC Project
license: MIT
metadata:
  npc: {{NPC_NAME}}
  hermes:
    tags: [SmartNPC, schedule, day-start, workflow]
---

# Schedule — {{NPC_NAME}}

## 按事件类型路由

| 事件 | 处理章节 |
|---|---|
| `day_started` | §A — 用 workflow 规划全天 |
| `schedule_trigger`（仅旧版 `action` 条目） | §B — 执行旧版单工具条目（已禁用） |

> **Workflow 条目（`workflow_id`）不会触发 §B。** 工作流引擎自动运行它们，
> 无需唤醒 LLM。你只会为旧版 `action` 条目收到 `schedule_trigger`
> （你应该停止使用 `action`）。

---

## A. 规划全天（day_started）

不要跳过。不要输出文本。按顺序调用工具：

1. `game_get_time` — 确认日期、季节、年份。
2. `game_get_weather` — 检查天气状况。
3. `workflow_list` — 发现可用的 workflow（每天调用，workflow 列表可能更新）。
4. `npc_plan_day` — 提交 **10-16 条 workflow 条目**覆盖全天。

### 条目格式（workflow 模型）

每条使用 `workflow_id` + 可选 `args`：

```
{ game_hour, game_minute, workflow_id, args?, reason }
```

- `game_hour` — 6（早上 6 点）到 25（次日凌晨 1 点）。
- `game_minute` — 0/10/20/30/40/50。条目间隔 40-90 分钟。
- `workflow_id` — 来自 `workflow_list`。如需查看 workflow 步骤细节，先调用 `workflow_get(id)`。
- `args` — 可选输入（如 `{"target_seed": "(O)490"}`）。省略则用默认值。
- `reason` — 简短的人类可读备注，用于日志。

**`action` 字段已被工具拒绝。** 使用 `action` 的条目将被跳过并返回错误。始终使用 `workflow_id`。先调用 `workflow_list`。

### 可用 workflow

始终先调用 `workflow_list`——列表可能变化。典型集合：

| workflow_id | 做什么 | 典型 args | 耗时 |
|---|---|---|---|
| `farm_care` | 巡视 → 收成熟作物 → 浇干作物 → 存箱 → 气泡 | `inspect_radius`（默认 12） | ~2-4 min |
| `farm_extension` | 巡视 → 清杂物 → 翻新地 → 重新巡视 → 播种 → 施肥 → 浇水 → 气泡 | `target_seed`, `fertilizer_id`, `inspect_radius` | ~3-5 min |
| `farm_cleanup` | 扫描农田 → 在农田+外围清杂物 → 存箱 → 气泡 | `inspect_radius`（默认 18）, `extend_bbox`（默认 3, 最大 10） | ~2-3 min |
| `resource_gather` | 巡视 → 采集 → 砍树碎石 → 存箱 → 气泡 | `inspect_radius`（默认 20） | ~3-5 min |
| `social_interact` | [可选 送货] → 走近和玩家说话 → 气泡 | `deliver_first`（默认 false） | ~1-2 min |
| `pet_routine` | 走向农场宠物 → 抚摸 → 气泡 | — | ~1 min |

### 每日配额

| Workflow | 每天调用次数 | 时间 |
|---|---|---|
| `farm_extension` | **1-2** | **最高优先级。** 第一个工作时段（6:30-7:00），第二轮在午后（13-14）。冬天跳过。 |
| `farm_care` | **3-4** | farm_extension 之后（8-9），午间（11-13），下午（15-17） |
| `farm_cleanup` | **2-3** | farm_care 轮次之后，分散在全天 |
| `resource_gather` | **2-3** | 农场轮次之间，移动到森林/山区 |
| `social_interact` | **1-2** | 午间、傍晚。如背包有物品用 `deliver_first: true`。 |
| `pet_routine` | **0-1** | 早晨或傍晚，如果玩家有宠物 |

**总计：每天 10-15 条。** 每条 workflow 捆绑了 3-6 个工具调用。

### 示例日程（12 条）

```
06:30  farm_extension        target_seed=(O)490  最高优先——开垦新地
08:00  farm_care             收菜+浇水（开垦之后的地也要浇）
09:30  resource_gather       砍树+采集
10:30  farm_cleanup          extend_bbox=3  全农场及周边清理
12:00  farm_care             午间收菜+浇水
13:00  social_interact       deliver_first=true  送货+聊天
14:00  farm_extension        target_seed=(O)490  第二轮开垦
15:30  resource_gather       第二轮采集+砍树
17:00  farm_cleanup          extend_bbox=3  傍晚清理
18:00  farm_care             只浇水不收了
19:00  social_interact       傍晚闲聊
19:40  pet_routine           摸摸宠物
```

### 优先级规则

- **farm_extension 始终排第一。** 在任何 farm_care 或 cleanup 之前，
  NPC 扩展农田。翻地→播种→施肥是关键瓶颈——
  作物需要好几天才能成熟，每少开垦一天就损失一天的产量。
  把 `farm_extension` 安排在 06:30-07:00 作为第一个工作时段。
- **第二轮开垦（午后）** 如果早上的完成了且还有可开垦土地。
  每天两次开垦 = 农场规模快速翻倍。
- 开垦之后才轮到 `farm_care`（已有作物的维护）
  和 `farm_cleanup`（整理已经存在的东西）。

### 适配规则

- **天气：** 雨天跳过 `farm_extension` 和 `resource_gather`
  （仅限室外）。增加额外的 `farm_care`（收获在雨中也能做）和
  `social_interact` 在室内。仍然保持 10+ 条目。
- **冬天：** 完全跳过 `farm_extension`。减少 `farm_care` 到
  只浇水不收获（没有作物）。通过旧版 `action` 添加室内活动
  （`npc_idle_activity`、库存整理、建筑巡视）。
- **早期游戏（春 Y1）：** 优先 `farm_extension` × 2 和
  `resource_gather` × 3 来启动农场。`farm_care` 可以轻一些。
- **季末（25-28 日）：** 不安排 `farm_extension`。最大化 `farm_care`
  （全部收获）。只在作物能成熟的情况下补种。
- **性格：** 勤劳的 NPC 多安排一次 `farm_extension` 或
  `farm_cleanup`。随性的 NPC 多安排一次 `social_interact`。遵循 SOUL.md。

### 护栏规则

- **workflow_list 优先。** 每次 `npc_plan_day` 调用前必须。
- **10-15 条，不是 30 条。** 每条 workflow 做了 3-6 个单工具的工作。
- **不要使用 `action` 字段——已被拒绝。** 工具会跳过任何带 `action` 的条目并返回错误。始终用 `workflow_id`。
- **重复规划规则：** 先调用 `npc_get_schedule`。如果今天已经规划过了，
  不要再调用 `npc_plan_day`。
- **条目计数：** 调用 `npc_plan_day` 之前，数一下条目。< 8
  说明你漏了一个类别；补上。> 18 说明你过度安排了
  （每条 workflow 运行 2-5 分钟）；减少。
- **Manager NPC：** 用 `farm_manager_round` × 4-5 替换
  `farm_extension` + 部分 `farm_care`。Manager 通过
  `npc_send_message(kind="behavioral")` 向工人分派任务。参见 smartnpc-farm-manager skill。

---

## B. 旧版 schedule_trigger（已禁用）

> **`action` 字段已被 `npc_plan_day` 拒绝。** 你不应该收到
> 工具动作的 `schedule_trigger` 事件。Workflow 条目由引擎自动处理，
> 无需唤醒 LLM。
>
> 如果你莫名其妙收到了 `schedule_trigger`，说明是非 workflow 的事件
> （如玩家互动）到达了。按照 SOUL.md 自然处理。

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
4. `npc_plan_day` — 提交 **10-15 条 workflow 条目**覆盖全天。

### 条目格式（workflow 模型）

每条使用 `workflow_id` + 可选 `args`：

```
{ game_hour, game_minute, workflow_id, args?, reason }
```

- `game_hour` — 6（早上 6 点）到 25（次日凌晨 1 点）。
- `game_minute` — 0/10/20/30/40/50。条目间隔 30-60 分钟。
- `workflow_id` — 来自 `workflow_list`。如需查看 workflow 步骤细节，先调用 `workflow_get(id)`。
- `args` — 可选输入（如 `{"target_seed": "(O)490"}`）。省略则用默认值。
- `reason` — 简短的人类可读备注，用于日志。

**`action` 字段已被工具拒绝。** 使用 `action` 的条目将被跳过并返回错误。始终使用 `workflow_id`。先调用 `workflow_list`。

### 可用 workflow

始终先调用 `workflow_list`——列表可能变化。典型集合：

| workflow_id | 做什么 | 典型 args | 耗时 |
|---|---|---|---|
| `farm_extension` | Agent 按 skill 决策（inspect→清→翻→施肥→种→浇→deposit→bubble→记忆） | `target_seed`, `fertilizer_id`, `inspect_radius` | ~3-5 min |
| `farm_care` | Agent 按 skill 决策（inspect→浇水+补种+清理→deposit→bubble→记忆） | `inspect_radius` | ~2-4 min |
| `farm_cleanup` | Agent 按 skill 决策（inspect→清杂物+砍树→deposit→bubble→记忆） | `inspect_radius` | ~2-4 min |
| `resource_gather` | Agent 按 skill 决策（inspect→采集+砍树→deposit→bubble→记忆） | `inspect_radius` | ~2-3 min |
| `farm_evening_close` | Agent 按 skill 决策（inspect→收割+补种→deposit→bubble/emotion→记忆） | `target_seed`, `inspect_radius` | ~2-3 min |
| `social_round_robin` | Agent 按 skill 决策（闲逛/跳舞→走近→聊天→bubble→记忆） | 无 | ~2-4 min |
| `social_interact` | Agent 按 skill 决策（情绪→摸宠→送货→走近→聊天→可选送礼→bubble→记忆） | `pet_first`, `deliver_first`, `give_gift` | ~2-5 min |

### 每日配额

| Workflow | 每天调用次数 | 时间 |
|---|---|---|
| `farm_extension` | **1-2** | 早上 6:30-7:00，春天/初夏可追加下午第二块。冬天跳过。 |
| `farm_care` | **2-3** | 上午 8-9、中午 12-13、下午 15-16。日常养护浇水补种。 |
| `farm_cleanup` | **1-2** | 上午或下午，清杂物砍树碎石。雨天跳过。 |
| `resource_gather` | **1-2** | 上午 9-10、下午 14-15。采集野物+砍树。雨天跳过。 |
| `farm_evening_close` | **1** | 收尾 17:30-18:30，收割→存箱→补种→收工。 |
| `social_round_robin` | **1-2** | 午后 13-14 或傍晚 19-20，闲逛/跳舞→走近玩家→聊天。 |
| `social_interact` | **1-2** | 灵活穿插——情绪→摸宠→送货→聊天→可选送礼。雨天/冬天多排。 |

**总计：每天 10-15 条。** 雨天和冬天用 `social_interact`、`social_round_robin` 补足缺少的室外条目。

### 示例日程（12 条，春 Y1 晴天）

```
06:30  farm_extension        target_seed=(O)472  开垦新地种防风草
07:30  farm_care             巡视已有作物，浇水补种
08:30  farm_cleanup          清理农场及周边杂物
09:30  resource_gather       采集野物+砍树攒资源
10:30  farm_care             第二轮养护——浇水清理
12:00  social_interact       午间互动——情绪→摸宠→送货→聊天
13:00  social_round_robin    午后闲逛→找玩家聊两句
14:00  resource_gather       第二轮采集+砍树
15:00  farm_care             下午第三轮养护
16:30  farm_cleanup          再清一轮杂物
17:30  farm_evening_close    收割→存箱→补种→收工
19:00  social_round_robin    傍晚散步→跟玩家聊今天干了什么
```

### 优先级规则

- **farm_extension 排第一。** 早上 6:30-7:00，冬天跳过。春/初夏可追加第二块（14:00）。
- **farm_care 穿插全天。** 上午、中午、下午各一轮。雨天跳过 outdoor 部分。
- **farm_cleanup 间歇执行。** 上午或下午 1-2 轮。雨天跳过。
- **resource_gather 上午+下午。** 各一轮采集+砍树。雨天跳过。
- **farm_evening_close 收尾。** 17:30-18:30 收割补种存箱。
- **社交穿插。** 午后+傍晚 1-2 条 social_round_robin，午间+晚间 1-2 条 social_interact。
- 雨天/冬天：用 `social_interact` 和 `social_round_robin` 填补所有跳过的工作流。

### 适配规则

- **天气：** 雨天跳过 `farm_extension`、`farm_cleanup`、`resource_gather`（室外），跳过 `farm_care` 的浇水部分。用 `social_interact` + `social_round_robin` 填补空位，仍保持 10-15 条。
- **冬天：** 跳过 `farm_extension` 和 `farm_care` 的浇水/耕种。保留 `farm_cleanup`（清杂物）、`resource_gather`（砍树碎石）。大量使用 `social_round_robin` 和 `social_interact`。
- **早期游戏（春 Y1）：** 可追加 1-2 条 `farm_extension`（共 2-3 条）加速扩田。`farm_care` 调 3 轮。
- **中期（夏秋 Y1）：** 标准配额，`farm_care` 侧重收获+浇水。
- **季末（25-28 日）：** 不安排 `farm_extension`。`farm_evening_close` 做最后一轮收割。多排 `social_round_robin`。
- **性格：** 勤劳 NPC 多排 `farm_care` + `resource_gather`（12-15 条）；随性 NPC 多排 `social_round_robin` + `social_interact`（10-12 条）。遵循 SOUL.md。

### 护栏规则

- **workflow_list 优先。** 每次 `npc_plan_day` 调用前必须。
- **10-15 条，不是 5 条。** 每条 workflow 含 3-6 个工具调用+LLM 决策。全天排满才有充实感。
- **不要使用 `action` 字段——已被拒绝。** 始终用 `workflow_id`。
- **重复规划规则：** 先调用 `npc_get_schedule`。如果今天已经规划过了，
  不要再调用 `npc_plan_day`。
- **条目计数：** 调用 `npc_plan_day` 之前，数一下条目。< 8
  太少了（NPC 会长时间发呆）；> 16 说明你过度安排了（间隔太密）；保持在 10-15。

---

## B. 旧版 schedule_trigger（已禁用）

> **`action` 字段已被 `npc_plan_day` 拒绝。** 你不应该收到
> 工具动作的 `schedule_trigger` 事件。Workflow 条目由引擎自动处理，
> 无需唤醒 LLM。
>
> 如果你莫名其妙收到了 `schedule_trigger`，说明是非 workflow 的事件
> （如玩家互动）到达了。按照 SOUL.md 自然处理。

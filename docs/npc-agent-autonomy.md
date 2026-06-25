# SmartNPC Agent 自运转技术方案

> NPC 在无人干预下感知、规划、执行、记忆的完整技术设计。

---

## 目录

1. [技术架构](#1-技术架构)
2. [自运转核心链路](#2-自运转核心链路)
3. [Schedule 时间驱动子系统](#3-schedule-时间驱动子系统)
4. [Workflow 引擎与预编译](#4-workflow-引擎与预编译)
5. [C# FollowSystem —— 游戏态执行层](#5-c-followsystem--游戏态执行层)
6. [事件驱动与多 NPC 扇出](#6-事件驱动与多-npc-扇出)
7. [记忆与状态持久化](#7-记忆与状态持久化)
8. [数据流小结](#8-数据流小结)

---

## 1. 技术架构

### 1.1 物理部署拓扑

```
┌─────────────────── Windows 主机 ───────────────────┐
│                                                      │
│  Stardew Valley ──ws── smartnpc-mcp ──HTTP──┐       │
│  + SMAPI + Mod       (Go, :3000/mcp)        │       │
│  ws server :18745     hermesrelay            │       │
│                                 │             │       │
│  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─WSL2 ─ ─ ─ ┼ ─ ─ ─ ─ ─ │       │
│                        ┌────────▼──────────┐ │       │
│                        │ Hermes Gateway     │ │       │
│                        │ xiami      :8642   │◄┘       │
│                        │ abigail    :8643   │         │
│                        │ ... 6 NPCs total   │         │
│                        │ each = SOUL + SKILL│         │
│                        │ + state.db + cron  │         │
│                        └───────────────────┘         │
└──────────────────────────────────────────────────────┘
```

### 1.2 四层职能划分

```
┌──────────────────────────────────────────────────┐
│  决策层  │ Hermes Agent (LLM)                    │
│          │ SOUL.md + SKILLs + state.db           │
│          │ "我是谁 → 现在该做什么 → 调哪个工具"    │
├──────────┼──────────────────────────────────────┤
│  编排层  │ smartnpc-mcp (Go)                     │
│          │ Scheduler + Workflow Engine + Relay   │
│          │ "到什么时间 → 触发哪个 workflow        │
│          │  → 怎么执行 steps → 怎样扇出事件"      │
├──────────┼──────────────────────────────────────┤
│  协议层  │ WebSocket JSON + MCP Streamable HTTP  │
│          │ request(id) ↔ response(id) + event    │
│          │ "工具 schema 长什么样 → 参数校验       │
│          │  → 结果怎么返回"                       │
├──────────┼──────────────────────────────────────┤
│  执行层  │ C# SMAPI Mod (StardewMCPBridge)       │
│          │ FollowSystem + NpcActionHandlerBase   │
│          │ "游戏 tick 里寻路 → 执行 → 反馈状态"    │
└──────────┴──────────────────────────────────────┘
```

### 1.3 关键组件

| 组件 | 语言 | 运行时 | 职责 |
|------|------|--------|------|
| `smapi-mod/` | C# (.NET 6) | Windows, SMAPI | 游戏线程集成：UI、ws server、FollowSystem、行为 handler |
| `smartnpc-mcp/` | Go | Windows 本机 | MCP 工具注册、ws 客户端、Hermes relay、Scheduler、Workflow Engine |
| `hermes/profiles/` | YAML + Markdown | WSL2 (Docker/Local) | NPC 人格、技能、配置、长期记忆 |
| `deploy/hermes/` | Dockerfile + compose | WSL2 Docker | 容器化部署编排 |

---

## 2. 自运转核心链路

NPC 自运转由两条互补链路驱动：**时间驱动**（Schedule）和**事件驱动**（Event Relay）。

### 2.1 一日生命周期

```
 ┌── 新的一天 (day_started event) ──┐
 │                                   │
 │ 1. LLM 感知                       │
 │    game_get_time / game_get_weather / player_get_status
 │                                   │
 │ 2. LLM 规划                       │
 │    npc_plan_day → 生成全天 Entry[]│
 │    每条 Entry = 触发时间 + workflow_id + args
 │                                   │
 │ 3. Scheduler 接管                 │
 │    全天 Entry 存入内存，Tick 驱动  │
 │                                   │
 │ 4. 随时间推进，逐个触发             │
 │    ┌─ 7:00 farm_care              │
 │    │   LLM 读 SKILL → 巡视→浇水    │
 │    ├─ 9:30 social_interact        │
 │    │   LLM 读 SKILL → 找玩家聊天   │
 │    ├─ 14:00 resource_gather       │
 │    │   LLM 读 SKILL → 采集+存放    │
 │    ├─ 17:00 farm_evening_close    │
 │    │   LLM 读 SKILL → 收尾工作    │
 │    └─ 20:00 (空闲/自由行为)        │
 │                                   │
 │ 5. 入夜 / 存档                    │
 │    day_started → Scheduler.ClearAll
 │    循环 ↑                         │
 └───────────────────────────────────┘
```

### 2.2 链路一：Schedule 时间驱动

```
game_time_tick event (每游戏小时)
  │
  ▼
scheduler.Tick(currentMinutes)
  │ 遍历所有 NPC 的 DaySchedule
  │ 匹配 Entry.MinutesOfDay() <= currentMinutes && !Fired
  ▼
FiredEntry → schedTriggerMsg channel → npcWorkflowWorker
  │
  ├─ 有 PrecompiledDef? ──→ engine.Run(def) 零 LLM 重放
  │
  ├─ 有 WorkflowID?
  │   └─ builtin YAML 解析 → 通常是 skill_call step
  │       └─ hermesrelay POST → LLM 读 SKILL → 实时决策 → 调 MCP tools
  │
  └─ 仅 Action (legacy)
      └─ schedule_trigger event → LLM 现场选 tool
```

**关键点：** `npc_plan_day` 每天只调一次 LLM 做日程规划，但每个 entry 触发时的**具体执行仍由 LLM 实时决策**（通过 `skill_call` → Hermes → LLM 读 SKILL → 决定工具调用顺序和参数）。`PrecompiledDef` 是已实现的可选优化路径。

### 2.3 链路二：事件驱动（响应式）

```
游戏事件            MCP 层处理             Hermes 注入
──────────         ──────────            ──────────
chat_message ──→   hermesrelay  ──→   POST /v1/responses
npc_interact ──→   按 NPC 名路由  ──→   NPC profile 收到事件
group_create ──→   附加 instructions ──→  LLM 决定是否回应
day_started  ──→   扇出到所有 NPC  ──→   触发 npc_plan_day
```

事件注入时 relay 自动附加 `instructions`（SOUL.md + critical-policy.md），确保 LLM 每次收到事件都拥有完整人格上下文。

---

## 3. Schedule 时间驱动子系统

### 3.1 数据模型

```go
// Scheduler 为每个 NPC 维护一个 DaySchedule
type DaySchedule struct {
    NPC     string
    Day     int      // 1-28
    Season  string
    Entries []Entry  // 按时间排序的日程表
}

type Entry struct {
    GameHour    int     // 6-25（SDV 时间）
    GameMinute  int     // 0/10/20/30/40/50（10 分钟精度）
    Reason      string  // LLM 记录的理由（调试用）

    // 三形态（优先级从高到低）：
    WorkflowID   string                    // 引用 builtin workflow
    Workflow     *workflow.Definition      // 内联定义（LLM 临时生成）
    Action       string                    // 单 tool（legacy，已弃用）

    Args            map[string]any         // workflow 输入参数
    PrecompiledDef  *workflow.Definition   // 预编译产物（P5）
}
```

### 3.2 npc_plan_day 调用

每天初 Hermes Agent 收到 `day_started` synthetic event → LLM 调用 `npc_plan_day`：

```json
{
  "npc": "XiaMi",
  "entries": [
    {
      "game_hour": 7, "game_minute": 0,
      "workflow_id": "farm_care",
      "args": { "inspect_radius": 30 },
      "reason": "早间农场巡视，检查作物状态"
    },
    {
      "game_hour": 9, "game_minute": 30,
      "workflow_id": "social_interact",
      "args": { "pet_first": true },
      "reason": "休息时段，找玩家聊聊并送小礼物"
    },
    {
      "game_hour": 17, "game_minute": 0,
      "workflow_id": "farm_evening_close",
      "args": {},
      "reason": "傍晚收尾——浇水、收获、施肥、补种、存放"
    }
  ]
}
```

LLM 从 `workflow_list` 知道可选 workflow 的 ID 和 input schema，从 SOUL.md 知道哪些行为符合该 NPC 的人设。

### 3.3 Tick 驱动

```go
func (s *Scheduler) Tick(gameMinutes int) []FiredEntry
```

每游戏小时由 `game_time_tick` 事件触发。Tick 遍历所有 NPC 的 schedule，找出 `MinutesOfDay() <= currentMinutes` 且未 fired 的 entry。

**迟触发容错：** 不要求精确时间匹配——如果某 entry 设在 7:10 而 Tick 在 7:40 才触发（如 cutscene 跳过了 7:20 的 tick），7:10 的 entry 仍会触发。每个 entry 每天最多触发一次。

### 3.4 npcWorkflowWorker —— 串行分发

```go
func npcWorkflowWorker(ch <-chan schedTriggerMsg, ...)
```

每个 NPC 独占一个 goroutine，从 channel 消费 schedule 消息。同一 NPC 的 workflow **严格串行**——新触发到达时取消旧 running workflow，确保不会一个 NPC 同时执行两个动作。

```
schedTriggerMsg channel (per-NPC)
  │
  ├─ worker-1 (xiami)
  ├─ worker-2 (abigail)
  └─ worker-N (每个 NPC 一个)
```

---

## 4. Workflow 引擎与预编译

### 4.1 两种执行路径

| | 实时 skill_call | 预编译 PrecompiledDef |
|---|---|---|
| LLM 参与 | 每次触发都调 LLM | 仅预编译阶段一次 |
| 延迟 | LLM API 往返（1-5s） | 本地引擎重放（~0） |
| 灵活性 | 高（根据实时环境调整） | 中（编译时环境快照） |
| 确定性 | 低 | 高 |
| 当前状态 | **默认路径** | 已实现，可选启用 |

### 4.2 实时执行路径（默认）

```
schedule entry 触发
  │
  ▼
resolveDefinition → builtin/*.yaml 解析
  │ 通常只含一个 step:
  │   kind: skill_call
  │   skill: smartnpc-farm-care
  ▼
hermesrelay POST → Hermes Agent 收到 workflow_skill_call event
  │
  ▼
LLM 读 smartnpc-farm-care/SKILL.md → 理解该 skill 的行为策略
  │
  ├─ 调 npc_inspect_object(radius=30)  → ws → C# 返回农场状态
  ├─ 根据返回数据决定:
  │   ├─ 作物缺水 → npc_water_crops(bbox=...)
  │   ├─ 有杂草 → npc_clear_debris(bbox=...)
  │   └─ 没事做 → 结束了
  └─ 每步 ws 往返，C# FollowSystem 排队执行
```

**Skill 是什么？** SKILL.md 是用 Markdown 写给 LLM 读的行为指导文档——不是配置文件。LLM 理解其中描述的策略、约束、示例后，在实时环境数据下自行决定调哪些工具、传什么参数。

### 4.3 预编译路径（可选优化）

```
schedule entry 中 PrecompiledDef 非空
  │
  ▼
engine.Run(ctx, runner, npc, precompiledDef, args)
  │ 本地执行！不调 LLM
  ├─ kind: tool → MCPRunner.CallTool → ws → C# 执行
  ├─ kind: branch → 本地表达式求值（$obs.water.count > 0）
  ├─ kind: wait → 轮询 FollowSystem 直到 Idle
  └─ kind: foreach → 遍历 $obs.harvest.crops 逐项处理
```

**PrecompiledDef 从哪来？** `PrecompileSkill` 流水线：

```
MCPRunner.PrecompileSkill(npc, skill, args)
  │
  ├─ 1. 注册 precompile channel (planID)
  ├─ 2. POST Hermes (precompile=true)
  │      LLM 在 recording 模式下跑 SKILL
  │      ├─ 读工具 (inspect/get) → RecordingRunner 代理真实 ws
  │      └─ 写工具 (water/harvest) → RecordingRunner 拦截记录！不真执行
  ├─ 3. LLM 提交 plan → workflow_precompile_result(planID, plan)
  └─ 4. 解析 plan → *workflow.Definition → Entry.PrecompiledDef
```

预编译后，触发时引擎直接按 concrete step 列表执行，每个 step 的参数都是预编译时 LLM 确定好的具体值（如 `bbox: {x1: 64, y1: 20, x2: 80, y2: 35}`），不再需要 LLM 重新决定。

### 4.4 8 种 Step 类型

| Step | 用途 | 预编译中出现 | 手写 YAML 中出现 |
|------|------|:----------:|:------------:|
| `tool` | 调 MCP 工具（含具体参数） | ✅ 主要 | — |
| `branch` | if/else（基于 `$obs` 变量） | ✅ LLM 生成 | — |
| `random` | 加权随机选分支 | ✅ | — |
| `foreach` | 遍历列表 | ✅ | — |
| `skill_call` | 委托给 SKILL（实时 LLM） | — | ✅ 唯一 |
| `llm_choice` | LLM 运行时选一（昂贵） | 少用 | — |
| `wait` | 等 FollowSystem Idle | ✅ | — |
| `stop` | 提前结束 | ✅ | — |

---

## 5. C# FollowSystem —— 游戏态执行层

### 5.1 设计原则

- **ws handler 只管设 mode，不碰 Game1**——FollowSystem 的 `PumpOnGameTick` 每 tick 执行实际游戏操作
- **串行独占**——同一 NPC 同一时刻只有一种长时 mode（如 `ClearDebris`），新任务排队或抢占
- **5 倍速**——Agent 驱动时 NPC 移速 5，回到 Idle 恢复 2

### 5.2 行为三模型

```
NpcActionHandlerBase
  │
  ├─ RefuseWhileBusy = false  →  轻量即时
  │   不进队列，当场执行。emote / chat_say / inspect / ...
  │
  ├─ RefuseWhileBusy = true, IsPreemptable = false  →  串行独占
  │   长时操作。water / harvest / clear / till / plant / ...
  │   当前 NPC 忙 → 入 NpcActionQueue，等 Idle 再跑
  │
  └─ RefuseWhileBusy = true, IsPreemptable = true  →  可抢占
      仅 npc_wander。高优任务到达 → 打断 wander 立即执行
```

### 5.3 NpcBehaviorMode 全集（19 种）

| Mode | 类型 | 说明 |
|------|------|------|
| `Idle` | — | 空闲，无事做 |
| `Summoning` | 即时 | 召唤到玩家身边 |
| `Following` | 即时 | 跟随玩家 |
| `Leading` | 即时 | 在前面带路 |
| `Wander` | 可抢占 | 连续随机漫步 |
| `ClearDebris` | 串行 | 清理杂物（草/石头/树枝/树桩） |
| `TillSoil` | 串行 | 耕地（含预清理阶段） |
| `PlantSeeds` | 串行 | 播种 |
| `WaterCrops` | 串行 | 浇水 |
| `HarvestCrops` | 串行 | 收获 |
| `Fertilize` | 串行 | 施肥 |
| `FillGaps` | 串行 | 补种空地 |
| `BreakResource` | 串行 | 砍树/碎石 |
| `ForageCollect` | 串行 | 采集掉落物 |
| `DepositItems` | 串行 | 走到箱子→存放 |
| `DeliverItems` | 串行 | 走到玩家→交货 |
| `WithdrawItems` | 串行 | 从箱子取物 |
| `ApproachAndSpeak` | 串行 | 走向玩家→表情/说话 |
| `PetAnimal` | 串行 | 摸宠物 |

### 5.4 串行动作队列

```
NpcActionQueue (static, per-NPC FIFO)
  │
  ├─ Enqueue(npc, thunk) → 入队，返回 position
  ├─ DrainReadyTasks() → 每个 game tick 调一次
  │   对每个 NPC: 若 FollowSystem mode == Idle → dequeue 并执行
  └─ Clear(npc) → 清空队列（action 被取消时）
```

**抢占规则：** Wander (IsPreemptable) 被新任务打断；Following/Leading 不抢占（玩家主动发起）。

---

## 6. 事件驱动与多 NPC 扇出

### 6.1 事件分类

| 事件 | 来源 | 扇出方式 |
|------|------|---------|
| `chat_message` | 玩家对附近 NPC 说话 | 路由给最近的可听见 NPC |
| `npc_interact` | 玩家点击 NPC | 路由给被点击的 NPC |
| `chat_received` | 玩家在面板对特定 NPC 说话 | 路由给该 NPC |
| `day_started` | 游戏进入新的一天 | **广播给所有 NPC** |
| `game_time_tick` | 每游戏小时 | → scheduler.Tick → 匹配 entry |
| `schedule_trigger` | scheduler Tick 触发 | 路由给对应 NPC |
| `npc_send_message` | NPC 间私信 | 路由给接收方 NPC |
| `npc_broadcast_event` | NPC 广播 | 所有其他 NPC |

### 6.2 Hermes Relay 转发

```
hermesrelay.POST(ctx, url, apiKey, instructions, conversation, event)
  │
  ├─ 拼装 instructions:
  │   critical-policy.md（硬规则，永不压缩）
  │   + SOUL.md（人格/人设/关系）
  │
  ├─ POST /v1/responses
  │   {"model":"xiami", "instructions":"...", "input":"[event] ..."}
  │
  └─ Hermes 内部:
      ├─ context compression（按阈值触发）
      ├─ 注入 state.db 长期记忆
      └─ LLM 决策 → MCP tool call → 回到 smartnpc-mcp
```

### 6.3 runtime-config 路由表

`hermes/runtime-config.yaml`（由 `render_profiles.sh` 从 `npcs.yaml` 生成）：

```yaml
profiles:
  - name: xiami
    npc_filter: XiaMi
    gateway_url: http://${SMARTNPC_HERMES_GATEWAY_HOST}:8642
    conversation: xiami
    model: hermes-agent
    api_key_env: SMARTNPC_HERMES_KEY
  # ... abigail, haley, ...
```

Relay 根据 `npc_filter` 匹配事件中的 NPC 名，路由到对应 gateway 端口。

---

## 7. 记忆与状态持久化

### 7.1 三层记忆

| 层 | 存储 | 生命周期 | 内容 |
|----|------|---------|------|
| 短期 | Hermes conversation | 单次 turn | 当前对话上下文、最近几条消息 |
| 中期 | state.db (SQLite) | 跨会话 | NPC 对玩家/其他 NPC 的印象、上次互动时间 |
| 长期 | `smartnpc-memory` SKILL | 跨天 | 重大事件摘要、关系变化趋势 |

### 7.2 Scheduler 状态

Scheduler 纯内存，不持久化——每天 `day_started` 时 `ClearAll()`，LLM 重新调用 `npc_plan_day`。

### 7.3 FollowSystem 状态

纯内存（`Dictionary<NpcName, NpcBehaviorState>`），每个 game tick 读取/更新。游戏退出即丢失——下次进游戏 NPC 从 Idle 开始。

### 7.4 Workflow Run History

Workflow 引擎每次执行生成 JSONL 日志：
- 每步 tool call 的结果（ok/nothing_to_do/error）
- branch 条件求值结果
- 变量绑定快照

可通过 `workflow_run_history` MCP 工具查询，用于调试和性能分析。

---

## 8. 数据流小结

```
                    ┌─────────────────┐
                    │   Hermes Agent  │
                    │   (LLM 决策)     │
                    └───────┬─────────┘
                            │ MCP tool call ↑↓ event inject
                    ┌───────┴─────────┐
                    │  smartnpc-mcp   │
                    │                 │
      schedule ──→  │  Scheduler ──→  workflow engine
      event    ──→  │  Relay ──────→  POST Hermes
      tool call ←── │  Bridge ─────→  ws request
                    └───────┬─────────┘
                            │ ws JSON ↑↓
                    ┌───────┴─────────┐
                    │  SMAPI Mod (C#) │
                    │                 │
                    │  MessageRouter  │
                    │  FollowSystem   │  ← 每 tick 执行+排队
                    │  Perception     │  ← 扫描环境
                    │  UI / Chat      │  ← 面板+气泡
                    └─────────────────┘
```

**一趟完整自运转周期：**

> `day_started` → ALL NPCs → LLM 各自 `npc_plan_day` → scheduler 存储全天 Entry
> → `game_time_tick` → scheduler.Tick → `npcWorkflowWorker`
> → `skill_call` → Hermes → LLM 读 SKILL → 决定调哪些工具
> → MCP 工具调用 → ws → C# FollowSystem → game tick 逐帧执行
> → 完成后 wait Idle → 下一个 entry 准备好再触发

NPC 始终在**感知-决策-执行-等待**的循环中运转，直到一天结束。

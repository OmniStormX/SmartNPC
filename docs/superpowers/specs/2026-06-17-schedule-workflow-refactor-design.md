# Schedule Workflow 重构设计

**Status:** Draft
**Branch:** `feat/schedule-workflows`
**Author:** synchen + Claude Opus 4.7
**Date:** 2026-06-17

---

## 1. 背景与动机

### 1.1 当前模型

Schedule entry 一个对应**单个 MCP 工具**，由 LLM 在 `npc_plan_day` 时选工具，在 `schedule_trigger` 时即时选参数：

```
schedule_trigger event → relay → Hermes Agent → LLM 在线选 tool 参数 → 调 tool
```

**缺点：**

| 问题 | 现状 |
|------|------|
| 表达力 | entry 只能是单工具；多步组合靠 LLM 即兴拼凑 |
| 决策时机 | 每个步骤参数都要走一次 LLM，方差大且贵 |
| 可重用性 | farm_maintenance / farm_harvest 是硬编码 SKILL，不可参数化 |
| 分支多样性 | LLM 每次重新决定，行为不稳定 |
| 调试 | 看 schedule.log + tool 调用，无步骤级状态 |

### 1.2 重构目标

| 维度 | 现状 | 目标 |
|------|------|------|
| Entry 表达力 | 单 `action` 字段 | 多步骤、可分支、有状态的工作流 |
| 决策时机 | 每步 LLM | LLM 选工作流（高层意图），引擎跑步骤 |
| 可重用性 | 硬编码 SKILL | YAML 命名工作流，参数化 |
| 分支多样性 | LLM 临场拼 | 工作流内置 if/branch/random（确定 + 可个性化） |
| 调试 | tool 级 | 步骤级（含分支决策、变量绑定） |

### 1.3 三处不变的事

1. **Mod handler 不变**——所有 `npc_*` 工具保持现有协议
2. **NpcActionQueue 串行模型不变**——工作流逐步调用，仍享 queue 保护
3. **bbox / TSP / nothing_to_do / 现有 farm_actions** 等先前重构不变

---

## 2. 概念模型

### 2.1 核心实体

```
Schedule entry
  ├─ time:        06:30
  ├─ workflow:    "farm_morning_round"     ← 工作流名（替代 action）
  └─ args:        { area_hint: "south", target_seed: "(O)490" }

Workflow definition (file)
  ├─ id:          farm_morning_round
  ├─ description: ...
  ├─ inputs:      { area_hint?, target_seed? }
  └─ steps:       [Step1, Step2, ...]

Step (DSL — 8 种 kind 的 tagged union)
  ├─ tool        : { kind: tool, name: ..., args: {...}, save_as: $obs, on_nothing_to_do: skip|stop|fail }
  ├─ branch      : { kind: branch, when: $obs.water.count > 0, then: [...], else: [...] }
  ├─ random      : { kind: random, weighted: [{weight: 3, do:[...]}, {weight: 1, do:[...]}] }
  ├─ foreach     : { kind: foreach, over: $crops, as: $c, do: [...], max_iter: 50 }
  ├─ skill_call  : { kind: skill_call, skill: smartnpc-greeting }
  ├─ llm_choice  : { kind: llm_choice, prompt: "...", options: [a,b,c], save_as: $pick }
  ├─ wait        : { kind: wait, condition: idle, timeout_seconds: 30 }
  └─ stop        : { kind: stop, reason: "..." }
```

### 2.2 变量与表达式

- 变量名以 `$` 引用：`$obs.water.count`，`$season == "fall"`
- 表达式只支持：path lookup、字面值（数字/字符串/bool/nil）、`==/!=/</<=/>/>=/&&/||/!`、括号
- **不引入完整脚本**——避免 Turing-complete 调试地狱
- Scope 写一次永不可变；foreach 推子 scope；missing var 解析为 nil

### 2.3 工作流双轨制

| 类型 | 来源 | 谁写 | 适用 |
|------|------|------|------|
| Built-in | YAML 嵌入仓库 | 开发者 | 通用流程（farm_morning_round 等） |
| Inline | LLM 在 `npc_plan_day` 直接传 steps | LLM | 个性化、临时变体 |

Schedule entry 三种合法形态：

```json
{ "time": "06:30", "workflow_id": "farm_morning_round", "args": {"area_hint": "south"} }
{ "time": "08:00", "workflow": { "steps": [...] } }                          // inline
{ "time": "10:00", "action": "npc_water_crops" }                             // legacy, auto-wrap
```

---

## 3. 架构

### 3.1 分层

```
Hermes Agent (LLM)
  └── npc_plan_day(workflow refs / inline steps)
       │
       ▼
smartnpc-mcp (Go)
  ├── pkg/workflow/                     ← 新模块
  │     ├── definition.go               DSL types + json/yaml tags
  │     ├── context.go                  Scope + expression evaluator
  │     ├── engine.go                   Run() entry + step interpreter
  │     ├── registry.go                 (P2) YAML loader / built-in registry
  │     ├── runner_mcp.go               (P4) 生产 Runner（ws / hermes）
  │     ├── metrics.go                  (P5) Prometheus / langfuse
  │     └── builtin/*.yaml              内置工作流
  ├── adapters/stardew/scheduler/       (P3) Entry 加 workflow 字段
  ├── adapters/stardew/tools/
  │     ├── npc_plan_day                (P3) 接受 workflow_id / inline / action
  │     ├── workflow_list/get/run       (P2) 发现 / 调试工具
  │     └── workflow_choice_reply       (P4) llm_choice 回路
  └── cmd/smartnpc-mcp/scheduler_pump   (P4) Tick → workflow runner
       │
       ▼ (一步一步发 ws action)
SMAPI Mod (C#)
  └── 现有 handler 不变（每步一个普通工具调用）
```

### 3.2 关键设计决策

| 决策 | 理由 |
|------|------|
| 工作流引擎放 mcp 端 | mod 已只做"游戏胶水"；mcp 已持有 scheduler；引擎可直接读 SDV state（通过现有 inspect 工具）做分支决策 |
| YAML 文件格式 | 人类可读；CI 可校验；运行时可外部覆盖（`SMARTNPC_WORKFLOW_DIR`） |
| 自写极简表达式 | 调试可控；避免依赖 cel-go 等重量级库 |
| 保留 `action` 字段（auto-wrap） | 渐进迁移；旧 schedule 不破坏 |
| 双通道触发？最终决定：单通道 | P4 切换后 schedule_trigger event 不再发；hermesrelay 不收 |
| Runner 接口抽象 | engine 完全可单测；MCPRunner 是 P4 的 swap-in |

### 3.3 触发流程对比

**之前：**
```
schedule_trigger event → relay → Hermes Agent → LLM 在线选 tool 参数 → 调 tool
```

**之后（P4）：**
```
schedule fires → mcp workflow engine 直接驱动：
  for each step in workflow:
    if tool step    → 调 mod handler，等 ack（已有 NpcActionQueue 串行）
    if branch       → 引擎本地求值
    if llm_choice   → 单独 POST Hermes Agent，把选择写回 context
    if skill_call   → 通过 hermesrelay 触发原 SKILL 路径
```

**好处：**
- 大多数工作流不再每步打扰 LLM（少 4-10 次 LLM 调用 / 触发）
- 必要时仍可 `llm_choice` 局部使用 LLM
- 步骤级日志、每步成功失败可追踪

---

## 4. 实施路线图

| Phase | 范围 | 是否破坏旧行为 | LLM SKILL 改动 | Mod 改动 |
|---|---|---|---|---|
| **P1** | DSL + 引擎骨架 + 单测 | ❌ | 无 | 无 |
| **P2** | YAML 加载 + 注册中心 + list/get/run | ❌ | 无 | 无 |
| **P3** | scheduler.Entry 升级 + npc_plan_day 兼容三形态 | ❌（兼容 wrap） | 介绍新字段 | 无 |
| **P4** | scheduler.Tick → workflow runner（默认行为切换） | ✅ | 重大（教 LLM 用 workflow_id） | 无 |
| **P5** | skill_call 完整 + 持久化 + 指标 + replay + SKILL 重写 | ❌ | SKILL 重写 | 无 |
| **P6** | 弃用 `action` + 删 schedule_trigger + 文档 / lint | ❌（删旧代码） | 删 deprecated 段 | 无 |

**整个 P1-P6 mod 不动一行代码。** 所有改动集中在 `smartnpc-mcp` + `hermes/profiles/_master/skills/` + `docs/`.

---

## 5. P1 — DSL + 引擎骨架（已完成）

### 5.1 目标

自包含 `pkg/workflow/`，无 scheduler/mod/hermes 耦合，引擎可在 Runner 接口下完全单测。

### 5.2 交付物

```
smartnpc-mcp/pkg/workflow/
├── definition.go    — Definition / Step / 8 种 kind / json+yaml tags
├── context.go       — Scope + expression evaluator
├── engine.go        — Run + Runner interface
└── engine_test.go   — 14 tests
```

### 5.3 关键 API

```go
// Run drives a workflow definition to completion against the given runner.
func Run(ctx context.Context, runner Runner, npc string,
    def *Definition, inputs map[string]any) (*RunResult, error)

type Runner interface {
    CallTool(ctx, npc, name, args) (map[string]any, error)
    CallSkill(ctx, npc, skill, args) error
    LLMChoice(ctx, npc, prompt, options) (string, error)
    WaitIdle(ctx, npc, timeout) (bool, error)
}

type RunResult struct {
    WorkflowID    string
    NPC           string
    StepCount     int
    ToolCalls     int
    Stopped       bool
    StopReason    string
    NothingToDoCt int
    FailedStep    int
}
```

### 5.4 测试覆盖（14/14 通过）

- Scope 链 / 路径解析
- 表达式所有形态（路径 / 字面值 / 比较 / 逻辑 / 括号）
- 线性 / branch / foreach / random / stop
- `on_nothing_to_do` 三策略（skip / stop / fail）
- Tool error 中止
- Arg 解析（含嵌套 map / list / `$ref`）
- Inputs 默认值与 caller 覆盖

### 5.5 状态：已完成

Commit: `08161a1 feat(workflow): P1 — DSL definitions + engine + tests`
14 tests pass. Full CI green.

---

## 6. P2 — Built-in 工作流加载 + 注册中心 + list/get/run 工具

### 6.1 目标

- YAML 文件落地仓库；smartnpc-mcp 启动时自动加载
- 提供 `Registry` 单例供 P3 引用
- 提供 3 个 MCP 工具让 LLM/operator 发现 + 调试工作流
- **不接入 scheduler；不替换任何现有行为**

### 6.2 新文件

```
smartnpc-mcp/pkg/workflow/
├── registry.go              # Registry struct, LoadDir, Get, List, Validate
├── registry_test.go
└── builtin/                 # 内置 YAML（go:embed）
    ├── farm_morning_round.yaml
    ├── farm_extension_chain.yaml
    ├── harvest_and_replant.yaml
    ├── forage_circuit.yaml
    ├── pet_routine.yaml
    └── social_visit.yaml

smartnpc-mcp/adapters/stardew/tools/
├── workflow.go              # workflow_list / workflow_get / workflow_run_inline
└── workflow_test.go
```

### 6.3 关键 API

```go
package workflow

type Registry struct {
    defs map[string]*Definition  // keyed by Definition.ID
}

// Init scans embedded `builtin/` plus optional override directory
// (env SMARTNPC_WORKFLOW_DIR). Errors on duplicate IDs / schema fails.
func (r *Registry) Init(extraDir string) error

func LoadDef(yamlBytes []byte) (*Definition, error)
func (r *Registry) Get(id string) *Definition
func (r *Registry) List() []*Definition

// Validate runs static checks: unknown step Kind / missing fields /
// empty bodies / duplicate save_as / circular skill_call.
func Validate(def *Definition) error
```

### 6.4 YAML 模板

```yaml
# pkg/workflow/builtin/farm_morning_round.yaml
id: farm_morning_round
description: 早间农场巡视：观测后串行执行所有非空类目
version: "1"
inputs:
  - name: inspect_radius
    description: 巡视半径，默认 25
    default: 25
steps:
  - kind: tool
    name: npc_inspect_object
    args:
      what: farm_actions
      radius: "$inspect_radius"
    save_as: obs

  - kind: branch
    when: "$obs.actions_available.harvest.count > 0"
    then:
      - kind: tool
        name: npc_harvest_crops
        args:
          x1: "$obs.actions_available.harvest.bbox.x1"
          y1: "$obs.actions_available.harvest.bbox.y1"
          x2: "$obs.actions_available.harvest.bbox.x2"
          y2: "$obs.actions_available.harvest.bbox.y2"
        on_nothing_to_do: skip
      - kind: tool
        name: npc_deposit_items
        args: { auto_find: true }
        on_nothing_to_do: skip

  - kind: branch
    when: "$obs.actions_available.water.count > 0"
    then:
      - kind: tool
        name: npc_water_crops
        args:
          x1: "$obs.actions_available.water.bbox.x1"
          y1: "$obs.actions_available.water.bbox.y1"
          x2: "$obs.actions_available.water.bbox.x2"
          y2: "$obs.actions_available.water.bbox.y2"
        on_nothing_to_do: skip

  # ... 其他类目（clear / till / plant / fertilize / forage）

  - kind: tool
    name: npc_show_text_bubble
    args: { text: "[早上忙完了]" }
```

### 6.5 嵌入 + 运行时覆盖

- `//go:embed builtin/*.yaml` 把 YAML 编进 binary
- 运行时可设 `SMARTNPC_WORKFLOW_DIR=/path` 让外部目录覆盖同名 ID（无需重编译）

### 6.6 三个 MCP 工具

```go
// adapters/stardew/tools/workflow.go

// workflow_list — 给 LLM 看可用工作流。
type WorkflowListInput  struct{}
type WorkflowListEntry  struct {
    ID          string                  `json:"id"`
    Description string                  `json:"description"`
    Inputs      []workflow.InputSpec    `json:"inputs,omitempty"`
}
type WorkflowListOutput struct {
    OK        bool                `json:"ok"`
    Workflows []WorkflowListEntry `json:"workflows"`
}

// workflow_get — 看单个工作流的完整步骤（debug 用）。
type WorkflowGetInput  struct { ID string `json:"id"` }
type WorkflowGetOutput struct {
    OK         bool                  `json:"ok"`
    Definition *workflow.Definition  `json:"definition,omitempty"`
    Message    string                `json:"message,omitempty"`
}

// workflow_run_inline — debug 工具，operator/LLM 可立即跑工作流验证。
// 默认仅在 --debug 启动 flag 下注册（防止 LLM 在 schedule 之外乱用）。
type WorkflowRunInlineInput struct {
    NPC        string                 `json:"npc"`
    WorkflowID string                 `json:"workflow_id,omitempty"`
    Inline     *workflow.Definition   `json:"inline,omitempty"`
    Args       map[string]any         `json:"args,omitempty"`
}
type WorkflowRunInlineOutput struct {
    OK            bool   `json:"ok"`
    StepCount     int    `json:"step_count"`
    ToolCalls     int    `json:"tool_calls"`
    NothingToDoCt int    `json:"nothing_to_do_ct"`
    Stopped       bool   `json:"stopped,omitempty"`
    StopReason    string `json:"stop_reason,omitempty"`
    Message       string `json:"message"`
}
```

### 6.7 测试

| 测试 | 内容 |
|------|------|
| `TestLoadAllBuiltin` | 加载 `builtin/` 全部 YAML 不出错 |
| `TestValidate_RejectsBadSteps` | 表驱动各种校验失败情况 |
| `TestRegistry_DuplicateIDError` | 同 ID 两个文件触发启动失败 |
| `TestRegistry_OverrideDir` | 外部目录加载 + 同 ID 覆盖语义 |
| `TestWorkflowGet_E2E` | MCP 工具端到端：list → get |

### 6.8 兼容性

完全无破坏。Scheduler / Mod 不动。

### 6.9 提交点

```
commit 1: pkg/workflow/registry.go + 6 yaml + tests
commit 2: adapters/stardew/tools/workflow.go + tests （depends on 1）
```

---

## 7. P3 — Scheduler 升级 + `npc_plan_day` 接受 workflow

### 7.1 目标

- `scheduler.Entry` 加 `WorkflowID` / `Workflow` / `Args` 字段
- `npc_plan_day` 入参兼容三形态：`action` / `workflow_id` / `workflow`（inline）
- 旧 `action` 自动 wrap 成单步骤工作流；**Tick 出来仍是 workflow**——为 P4 铺好

### 7.2 改动

```
smartnpc-mcp/adapters/stardew/scheduler/scheduler.go
  ├ Entry 加字段：WorkflowID, Workflow, Args
  ├ Tick 返回的 FiredEntry 同步带 workflow refs

smartnpc-mcp/adapters/stardew/tools/npc_schedule.go
  ├ NpcPlanDayInputEntry 新增三个字段
  ├ 入参规范化（normalizeEntry）

smartnpc-mcp/adapters/stardew/tools/action_logger.go
  ├ schedule.log 输出新增 workflow 列（兼容旧 action）
```

### 7.3 数据结构

```go
package scheduler

type Entry struct {
    GameHour    int
    GameMinute  int
    GameMinutes int

    // ── New (P3) ───────────────────────────────────────────────────
    // 入参规范化后，每个 Entry 一定有 Workflow 或 WorkflowID 之一。
    // 旧 Action 也包成 1-step Workflow，Tick 消费方只看 workflow refs。
    WorkflowID string
    Workflow   *workflow.Definition
    Args       map[string]any

    // ── Legacy ─────────────────────────────────────────────────────
    Action string  // deprecated; 保留用于 schedule.log 可读性
    Reason string

    Fired   bool
    FiredAt time.Time
}
```

### 7.4 `npc_plan_day` 入参变化

```go
type NpcPlanDayInputEntry struct {
    GameHour   int    `json:"game_hour"`
    GameMinute int    `json:"game_minute,omitempty"`
    Reason     string `json:"reason,omitempty"`

    // ── 三选一 ────────────────────────────────────────────────
    // 选 1：内置工作流引用（推荐）。
    WorkflowID string         `json:"workflow_id,omitempty"`
    Args       map[string]any `json:"args,omitempty"`

    // 选 2：临时 inline 工作流（LLM 自定义）。
    Workflow *workflow.Definition `json:"workflow,omitempty"`

    // 选 3：旧式单工具（向后兼容）。
    Action string `json:"action,omitempty"`
}
```

### 7.5 入参规范化

```go
func normalizeEntry(reg *workflow.Registry, in NpcPlanDayInputEntry) (scheduler.Entry, error) {
    e := scheduler.Entry{
        GameHour:   in.GameHour,
        GameMinute: in.GameMinute,
        Reason:     in.Reason,
    }
    switch {
    case in.WorkflowID != "":
        if reg.Get(in.WorkflowID) == nil {
            return e, fmt.Errorf("unknown workflow_id %q", in.WorkflowID)
        }
        e.WorkflowID = in.WorkflowID
        e.Args = in.Args

    case in.Workflow != nil:
        if err := workflow.Validate(in.Workflow); err != nil {
            return e, fmt.Errorf("inline workflow invalid: %w", err)
        }
        e.Workflow = in.Workflow
        e.Args = in.Args

    case in.Action != "":
        // Wrap legacy single-tool entries into 1-step workflow.
        e.Action = in.Action
        e.Workflow = &workflow.Definition{
            ID: "__legacy_action_wrapper__",
            Steps: []workflow.Step{{
                Kind: workflow.StepKindTool,
                Tool: &workflow.ToolStep{Name: in.Action},
            }},
        }

    default:
        return e, errors.New("entry missing workflow_id / workflow / action")
    }
    return e, nil
}
```

### 7.6 测试

| 测试 | 内容 |
|------|------|
| `TestPlanDay_WorkflowID_Accepted` | 引用内置 ID 正常入库 |
| `TestPlanDay_WorkflowID_Unknown` | 未知 ID 报错 |
| `TestPlanDay_Inline_Validates` | inline def 不合法被拒 |
| `TestPlanDay_LegacyAction_Wraps` | 旧 action 被包成 1-step workflow |
| `TestPlanDay_Mixed_Day` | 单一计划里三种形态混用 |
| `TestSchedule_TickReturnsWorkflowRef` | FiredEntry 带 workflow ref |

### 7.7 SKILL.md 微调

`npc_plan_day` 工具描述：让 LLM 优先用 `workflow_id`，介绍 `workflow_list` 可发现。SKILL.md 同步类目改为"列工作流" + 提示编排选择。

### 7.8 兼容性

- 旧 `action` 调用路径仍然成功
- Tick 仍然走 hermesrelay 旧通路（P3 不切换）
- LLM 完全可以不变

### 7.9 提交点

```
commit 1: scheduler.Entry + npc_plan_day intake + tests
commit 2: schedule_log + ack message text
```

---

## 8. P4 — 引擎接入触发：scheduler.Tick → workflow runner

### 8.1 目标

**这是默认行为切换的 phase。** scheduler 不再发 `schedule_trigger` event 到 hermesrelay；改为本地启动 workflow 引擎驱动 mod。LLM 仍参与，但只在 `llm_choice` / `skill_call` 时被调起。

### 8.2 三个新组件

```
smartnpc-mcp/pkg/workflow/runner_mcp.go        # MCPRunner: 真生产 Runner
smartnpc-mcp/pkg/workflow/runner_mcp_test.go
smartnpc-mcp/cmd/smartnpc-mcp/scheduler_pump.go # 接入点
```

### 8.3 `MCPRunner` 设计

```go
package workflow

// MCPRunner is the production Runner. Each Run() invocation gets a
// dedicated MCPRunner so per-run state (last NPC, last tool result) is
// isolated. The runner does NOT cache anything across runs.
type MCPRunner struct {
    bridge   *bridge.WSClient        // for CallTool → ws action
    follow   FollowSystemQuery        // for WaitIdle
    relay    *hermes.Relay            // for CallSkill, LLMChoice
    npc      string
    log      *slog.Logger
}

type FollowSystemQuery interface {
    GetMode(npc string) string  // "Idle" or other mode tag
}

func NewMCPRunner(opts MCPRunnerOptions) *MCPRunner

// CallTool: bridge.Call(ws_action, args). Translates returned bytes
// into map[string]any. Pre-pends "npc" arg if not already in args.
func (r *MCPRunner) CallTool(ctx, npc, name, args) (map[string]any, error)

// CallSkill: emits a synthetic event through hermesrelay.HandleEvent.
// Returns immediately — engine continues without waiting for the LLM
// turn (use Wait step explicitly if synchronisation needed).
func (r *MCPRunner) CallSkill(ctx, npc, skill, args) error

// LLMChoice: synchronous round-trip through hermesrelay. Wraps prompt +
// options as structured event the agent answers via workflow_choice_reply.
func (r *MCPRunner) LLMChoice(ctx, npc, prompt, options) (string, error)

// WaitIdle: polls FollowSystemQuery.GetMode at 250ms intervals until
// "Idle" or timeout.
func (r *MCPRunner) WaitIdle(ctx, npc, timeout) (bool, error)
```

### 8.4 Scheduler pump 改造

```go
// cmd/smartnpc-mcp/scheduler_pump.go

// 替代当前 main.go 里那段 schedule_trigger event 发布循环。
// 改为：每个 fired entry 启动 per-NPC workflow goroutine。
func handleFiredEntries(ctx context.Context, fired []scheduler.FiredEntry, deps *deps) {
    for _, e := range fired {
        def := resolveDefinition(deps.workflowReg, e)  // ID 查表 / inline
        runner := workflow.NewMCPRunner(...)
        go func(e scheduler.FiredEntry, def *workflow.Definition) {
            deps.workflowMu.Lock(e.NPC)  // serialize per NPC
            defer deps.workflowMu.Unlock(e.NPC)

            runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
            defer cancel()
            res, err := workflow.Run(runCtx, runner, e.NPC, def, e.Args)
            tools.LogWorkflowRun(e, res, err)
        }(e, def)
    }
}
```

### 8.5 关键设计决策

| 设计点 | 决策 |
|--------|------|
| schedule_trigger event 是否保留？ | **不再发**。hermesrelay 不收。 |
| 旧 SKILL（farm_maintenance / farm_harvest）怎么办？ | 通过 `skill_call` step 仍可被新 workflow 调用。SKILL.md 标"通过 workflow 调用，不直接 schedule" |
| 工作流并发性 | 每 NPC 一个 worker goroutine，per-NPC mutex；多 NPC 之间并行 |
| 全局 timeout | 单工作流默认 10 分钟（避免跨日穿越） |
| 失败重试 | 不重试。失败入 log，下个 schedule 自然推进 |
| LLMChoice 协议 | 新增 `workflow_choice_reply` 一次性 MCP 工具 |

### 8.6 `workflow_choice_reply` 协议

LLMChoice 步骤实现（mcp 端）：

1. mcp 发 synthetic event `workflow_llm_choice_request` 到 hermesrelay：
   ```json
   { "request_id": "uuid", "prompt": "...", "options": ["a", "b", "c"] }
   ```
2. hermesrelay 转给 agent（一个独立 turn）
3. format.go 渲染："⚠️ workflow needs your decision: pick one of [a, b, c]; respond by calling `workflow_choice_reply(request_id=..., choice=...)`"
4. agent 调 `workflow_choice_reply` MCP 工具，mcp 把答案 push 进对应 channel
5. mcp `LLMChoice()` 在 channel 上等待（带 timeout，超时用 `options[0]` fallback）

### 8.7 测试

| 测试 | 内容 |
|------|------|
| `TestMCPRunner_CallTool_E2E` | bridge testserver mock 验证 round-trip |
| `TestMCPRunner_WaitIdle` | follow mock 在第 N 次 poll 返回 Idle |
| `TestSchedulerPump_DispatchesWorkflow` | scheduler.Tick + handleFired 串测 |
| `TestSchedulerPump_PerNPCSerial` | 同一 NPC 两个 entries 不并行 |
| `TestLLMChoice_Roundtrip` | request → reply 全链路 mock |

### 8.8 风险 + 缓解

| 风险 | 缓解 |
|------|------|
| agent 不调 `workflow_choice_reply` | 超时后 fallback 到 `options[0]`（已在 P1 引擎里实现） |
| mod handler 改协议 | 不会，本 phase 无 mod 改动 |
| scheduler 状态恢复 | fired entries 不持久化，restart 后丢；可接受（当前行为已如此） |

### 8.9 提交点

```
commit 1: runner_mcp.go + tests
commit 2: workflow_choice_reply tool + relay format
commit 3: scheduler_pump.go 切换 + integration tests （默认行为切换在此）
```

---

## 9. P5 — 高级 step 完善 + 持久化与可观测

### 9.1 目标

把 P4 的最小可用版本打磨到生产级。

### 9.2 五个增强

#### 9.2.1 `skill_call` 完整化

让 workflow 能可靠地调起 `smartnpc-greeting` 等原 SKILL 并等它完成。

- 引入 `skill_call.wait_seconds`（默认 30）
- hermesrelay 触发 SKILL 时返回 `correlation_id`
- 引擎用 `correlation_id` 等 SKILL 写完 memory 标记后才继续

#### 9.2.2 持久化 workflow run 历史

```
logs/mcp/workflow_runs/
├── abigail-fall-d27-y1.jsonl     # 一天一文件
└── ...
```

每次 Run 完写一行：

```json
{
  "ts": "...", "npc": "Abigail", "workflow_id": "farm_morning_round",
  "step_count": 7, "tool_calls": 5, "nothing_to_do": 1,
  "duration_ms": 12450, "stopped": false, "args": {...}
}
```

附属：`workflow_run_history` MCP 工具供 LLM 查询昨天运行结果。

#### 9.2.3 步骤级指标

`pkg/workflow/metrics.go`：

- counter: `workflow_step_total{kind, npc, workflow_id}`
- histogram: `workflow_step_duration_ms{kind}`
- counter: `workflow_run_total{npc, workflow_id, outcome}`（outcome ∈ {ok, stopped, errored, timeout}）

通过现有 langfuse 或独立 prometheus endpoint 暴露。

#### 9.2.4 Replay 工具（debug）

```
workflow_replay --npc=Abigail --date=2026-06-17 --workflow=farm_morning_round
```

读 jsonl，把 `args` 重新跑一次（mock 当时 inspect 输出），用于回归排查。

#### 9.2.5 SKILL.md 全面重写

- 删除"列工具"段，改"列工作流 + 输入"
- 调用样例从 `action: "npc_water_crops"` 改为 `workflow_id: "farm_morning_round"`
- 解释 `workflow_list` 可发现，`workflow_get` 可查详情

### 9.3 测试

| 测试 | 内容 |
|------|------|
| `TestSkillCall_BlocksUntilCompletion` | mock relay 返回 correlation，skill_call 等成功 |
| `TestRunHistory_AppendsJSONL` | 每次 Run 后文件多 1 行 |
| `TestMetrics_Exposed` | step counter 增长正确 |
| `TestReplayTool` | 读历史重放生成相同步骤序列 |

### 9.4 提交点

```
commit 1: skill_call 完整 + tests
commit 2: run history jsonl + tests
commit 3: metrics + tests
commit 4: replay tool + tests
commit 5: SKILL.md 重写 + render
```

---

## 10. P6 — 收尾 + 弃用 + 文档

### 10.1 目标

清理过渡代码、废弃 legacy 路径、写实施手册。

### 10.2 六项工作

#### 10.2.1 弃用 `action` 字段

- `npc_plan_day` 入参 `action` 收到时仍接受，但响应里返回：
  ```
  "deprecated_actions": ["npc_water_crops", ...],
  "message": "Wrap these into a workflow next plan; action= will be rejected in P7."
  ```
- SKILL.md 说"不要再用 action"

#### 10.2.2 删除 hermesrelay 的 `schedule_trigger` 处理

确认 P4 切换稳定（一周以上日志正常）后，删除：

- hermesrelay 内对 `bridge.EventScheduleTrigger` 的处理
- `format.go` 里的 schedule_trigger case
- schedule_trigger event 完全不存在于代码库

#### 10.2.3 内置工作流扩展

根据 P4-P5 实际观察新增：

- `farm_evening_close` — 傍晚收尾（最后一轮 harvest + deposit + 锁箱子情绪）
- `mine_crawl` — 矿洞向下（experimental 标）
- `social_round_robin` — 走访 N 个 NPC 找个聊
- `weather_bookkeeping` — 雨天专用（室内活动循环）

#### 10.2.4 工作流 lint 工具

```
go run ./cmd/workflow-lint
```

CI 集成，校验：

- 所有内置 yaml 通过 `Validate`
- 每个工作流引用的 tool name 都是 mod 真实存在的 action
- 没有引用未定义的 input variable
- 没有 dangling skill_call

#### 10.2.5 文档

- `docs/architecture.md` 加 "Workflow engine" 章节
- `docs/workflow-authoring.md` — 给开发者写新工作流的指南
- `docs/adr/0005-schedule-as-workflow.md` — 决策记录

#### 10.2.6 监控 + 报警

按 SmartNPC 当前 langfuse 集成：

- 每个 workflow run 上报一次 trace
- step 是 child span
- 失败 trace 高亮（color=red）

如有 grafana：dashboard 模板放 `deploy/dashboards/workflow.json`。

### 10.3 测试

| 测试 | 内容 |
|------|------|
| `TestWorkflowLint_BadToolName` | lint 工具发现错引用 |
| `TestWorkflowLint_AllBuiltinPass` | 6 + 4 = 10 个内置都过 lint |
| `TestRelay_NoScheduleTrigger` | 旧 case 删除后 schedule_trigger event 被 ignore（不会 panic） |

### 10.4 提交点

```
commit 1: deprecate action= warning
commit 2: remove schedule_trigger relay path
commit 3: 4 new built-in workflows + tests
commit 4: workflow-lint cmd + CI integration
commit 5: docs + ADR
commit 6: langfuse / dashboard
```

---

## 11. 跨 phase 速查表

### 11.1 关注点定位

| 关心的事 | 在哪 |
|---------|------|
| 单元测试 / engine 行为 | P1 |
| YAML 文件 / `list/get` / `run_inline` | P2 |
| `npc_plan_day` 入参 + scheduler 数据 | P3 |
| **行为切换发生点** | **P4** |
| 高级特性（skill_call wait / 历史 / 指标 / replay） | P5 |
| Legacy 清理 / lint / 文档 | P6 |

### 11.2 兼容性矩阵

| Phase | 是否破坏旧行为 | LLM SKILL 改动 | mod 改动 |
|---|---|---|---|
| P1 | ❌ | 无 | 无 |
| P2 | ❌ | 无 | 无 |
| P3 | ❌（兼容 wrap） | 微调（介绍新字段） | 无 |
| P4 | ✅ 切换默认通路 | 重大（教 LLM 用 workflow_id） | 无 |
| P5 | ❌ | SKILL 重写 | 无 |
| P6 | ❌（删旧代码） | 删 deprecated 段 | 无 |

整个 P1-P6 **mod 不动一行代码**。所有改动集中在 mcp + hermes profiles + docs。

---

## 12. 风险与权衡

| 风险 | 缓解 |
|------|------|
| DSL 变得 Turing-complete 调试难 | 限步骤 8 种基本类型，禁止递归（除 foreach + max_iter） |
| 工作流"假死"（步骤间永远等 idle） | 每步 timeout + 全工作流 deadline（10 分钟游戏内） |
| Built-in 工作流变陈旧 | YAML 改即生效，CI 加 schema 校验 |
| 旧 schedule 被破坏 | P3 自动 wrap action 为单步工作流 |
| LLM 不知道 workflow 怎么写 | P6 SKILL 重写 + `list_workflows` 工具让它发现 |
| LLMChoice 模型答非所问 | 超时回退 `options[0]` |
| Skill_call 调用旧 SKILL 卡死 | `wait_seconds` 显式 timeout，逾时引擎继续 |

---

## 13. 已确定的设计决策（决议表）

| 编号 | 问题 | 决议 |
|------|------|------|
| Q1 | 内置工作流文件格式 | YAML（人类可读，已有 yaml.v3 依赖） |
| Q2 | DSL 表达式语法 | 自写极简（path lookup + 比较 + 逻辑） |
| Q3 | 旧 `action` 字段 | 保留 + 自动 wrap，永久兼容（P3 起 deprecated 提示，P5 起仍接受，P6 起 deprecated warning） |
| Q4 | `schedule_trigger` event 是否保留 | P4 起不再发，P6 删除处理代码（单通道） |
| Q5 | 实施顺序 | 逐 phase commit + push（每个独立可 review） |
| Q6 | YAML 加载源 | `//go:embed` + 可选 `SMARTNPC_WORKFLOW_DIR` 覆盖 |
| Q7 | `workflow_run_inline` 工具默认开启？ | 仅在 `--debug` 启动 flag 下注册 |

---

## 14. 状态跟踪

| Phase | 状态 | Commit |
|-------|------|--------|
| P1 — DSL + 引擎骨架 | ✅ 完成 | `08161a1` |
| P2 — 注册中心 + 内置 YAML + 工具 | ✅ 完成 | 本 commit |
| P3 — Scheduler 升级 | 待开始 | — |
| P4 — 引擎接入（默认行为切换） | 待开始 | — |
| P5 — 高级 step + 持久化 + 指标 | 待开始 | — |
| P6 — 弃用 + 文档 + lint | 待开始 | — |

---

## 15. 附录

### 15.1 内置工作流候选清单

P2 第一批：

| ID | 用途 | 关键步骤 |
|----|------|---------|
| `farm_morning_round` | 早间农场巡视（观测后串行执行非空类目） | inspect → branch×6 → bubble |
| `farm_extension_chain` | 开荒一片（till→plant→fertilize→water） | inspect → branch（无 till 跳） → till → plant → fertilize → water |
| `harvest_and_replant` | 收成 → 存箱 → 补种 | inspect → foreach harvest crops → deposit → branch plant |
| `forage_circuit` | 山林采集（forage + break_resource） | inspect → branch forage → branch break → deposit |
| `pet_routine` | 摸猫 | pet_animal（自动找当前 pet） |
| `social_visit` | 随机社交动作 | random（approach / dance / express） |

P6 第二批扩展：

| ID | 用途 |
|----|------|
| `farm_evening_close` | 傍晚收尾 |
| `mine_crawl` | 矿洞向下（experimental） |
| `social_round_robin` | 走访 N 个 NPC |
| `weather_bookkeeping` | 雨天室内活动 |

### 15.2 表达式语法 EBNF

```
expr        := orExpr
orExpr      := andExpr ( '||' andExpr )*
andExpr     := notExpr ( '&&' notExpr )*
notExpr     := '!' notExpr | atom
atom        := '(' expr ')' | comparison | path | literal
comparison  := path op operand        (path 仅在左侧 — 简化)
op          := '==' | '!=' | '<' | '<=' | '>' | '>='
operand     := literal | path
literal     := number | quoted-string | 'true' | 'false' | 'nil'
path        := ['$'] ident ('.' ident)*
ident       := [a-zA-Z_][a-zA-Z0-9_]*
```

### 15.3 truthy 语义

| 类型 | truthy 当 |
|------|-----------|
| nil | 永远 false |
| bool | 自身 |
| string | 非空 |
| number | 非零 |
| []any | len > 0 |
| map[string]any | len > 0 |
| 其他 | true（存在即真） |

---

## 16. 评审 / 推进路径

1. 此文档作为整个重构的设计单点真相
2. 每完成一个 phase 更新 §14 状态表 + 引用具体 commit
3. P2 启动前 confirm §15.1 工作流清单
4. P4 切换前在 dev 环境运行至少 3 个游戏日，验证 workflow runner 稳定
5. P6 删除 schedule_trigger 处理前，再确认 7 天日志无回归

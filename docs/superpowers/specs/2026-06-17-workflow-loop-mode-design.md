# workflow 循环运转模式 — loop mode

日期：2026-06-17 | 状态：已确认

## 问题

当前 workflow 单次执行后 NPC 闲置到下一个 schedule_trigger。中间时段 NPC 纯发呆，活人感差。

## 设计

### 1. 循环 workflow 选定

| Workflow | 循环 | 理由 |
|----------|------|------|
| `farm_care` | ✅ | 浇水/收菜循环——总有新活 |
| `farm_cleanup` | ✅ | 清完一片还有一片 |
| `resource_gather` | ✅ | 采集+砍树永无尽 |
| 其他（farm_extension, social_interact 等） | ❌ | 一次性任务 |

### 2. YAML 层 — loop 配置

```yaml
id: farm_care
loop:
  mode: skill_controlled
  stop_on: next_schedule
steps:
  - kind: tool
    name: npc_inspect_object
    args: { what: farm_actions, radius: 12 }
    save_as: obs
  - kind: skill_call
    skill: smartnpc-farm-maintenance
```

- `mode: skill_controlled` — LLM 每轮 inspect 后自己编排动作
- `stop_on: next_schedule` — 新 trigger 到达时 engine 发 cancel

### 3. Go engine 层

#### Definition 扩展

```go
type LoopConfig struct {
    Mode   LoopMode `json:"mode" yaml:"mode"`
    StopOn StopMode `json:"stop_on" yaml:"stop_on"`
}
type LoopMode string  // "" | "skill_controlled"
type StopMode string  // "" | "next_schedule"
```

#### npcWorkflowWorker 循环 + cancel

```
activeLoops: map[npc]context.CancelFunc

收到 trigger:
  如果 activeLoops[npc] 存在 → cancel() 旧 ctx（打断上一轮）
  创建新 ctx → 存入 activeLoops

  loop:
    Run(workflow, ctx)
    如果 ctx 被 cancel → 当前 tool 执行完后退出
    如果 result.Stopped（自动停止条件触发）→ 退出
    继续 loop

  清理 activeLoops[npc] → 处理下一个 trigger
```

#### 自动停止条件

每轮 `skill_call` 完成后，engine 检查 `save_as: obs` 的输出：
- `water=0 && harvest=0 && plant=0 && clear=0 && till=0` → 本轮零活 → 自动收工
- 否则继续循环

也允许 LLM 在 skill 内主动调用 `npc_workflow_stop` 工具收工（可选）。

#### cancel 信号传播

- `context.WithCancel` 传入 `Run()`
- `Run()` 在每个 step 前检查 `ctx.Err()`
- 如果 ctx 已 cancel → 当前 tool 调用（ws action）让它执行完（不可打断 NPC 物理动作），但下一个 step 前退出
- `skill_call` 步骤在 ctx cancel 后不再发起新一轮 LLM

### 4. 非目标

- 不改变非循环 workflow 的行为
- 不改变 FollowSystem 的 action 队列机制
- 不改变 schedule 生成逻辑（仍然产出固定时间点条目）

## 影响文件

| 文件 | 改动 |
|------|------|
| `smartnpc-mcp/pkg/workflow/definition.go` | 新增 `LoopConfig`、`LoopMode`、`StopMode` 类型 + YAML 解析 |
| `smartnpc-mcp/pkg/workflow/engine.go` | `Run()` 支持 ctx 中途 cancel 的优雅退出；新增自动停止检测 |
| `smartnpc-mcp/cmd/smartnpc-mcp/main.go` | `npcWorkflowWorker` 循环 + `activeLoops` cancel map |
| `smartnpc-mcp/pkg/workflow/builtin/farm_care.yaml` | 加 `loop` 配置 |
| `smartnpc-mcp/pkg/workflow/builtin/farm_cleanup.yaml` | 加 `loop` 配置 |
| `smartnpc-mcp/pkg/workflow/builtin/resource_gather.yaml` | 加 `loop` 配置 |

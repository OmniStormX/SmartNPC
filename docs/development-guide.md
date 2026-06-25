# SmartNPC 开发指南

> NPC 行为、工作流、Skill、Schedule 的增改流程与模式参考。

---

## 目录

1. [新增 NPC 行为（Action）](#1-新增-npc-行为action)
2. [新增工作流 / Skill](#2-新增工作流--skill)
3. [新增/修改 Skill](#3-新增修改-skill)
4. [新增 Schedule 条目](#4-新增-schedule-条目)
5. [调试与测试](#5-调试与测试)
6. [全层检查清单](#6-全层检查清单)

---

## 1. 新增 NPC 行为（Action）

NPC 行为分两种执行模型：

| 模型 | `RefuseWhileBusy` | `IsPreemptable` | 示例 | 适用 |
|------|-------------------|-----------------|------|------|
| 轻量即时 | `false` | — | `npc_emote`, `npc_inspect_object` | 不改变 FollowSystem 状态、不排队 |
| 串行独占 | `true` | `false` | `npc_water_crops`, `npc_harvest_crops` | 长时操作，同一 NPC 排队执行 |
| 可抢占 | `true` | `true` | `npc_wander` | 填充行为，高优任务可打断 |

新增一个行为需要修改 **4 层**：

### 1.1 C# 层：FollowSystem 模式 + Handler

**Step 1 — 在 `NpcBehaviorMode` 枚举添加新模式**

`smapi-mod/Movement/FollowSystem.cs`：

```csharp
internal enum NpcBehaviorMode
{
    Idle,
    // ... existing modes ...
    MyNewAction,  // ← 新增
}
```

**Step 2 — 在 `NpcBehaviorState` 添加运行时状态（如需）**

```csharp
internal sealed class NpcBehaviorState
{
    // 例：带队列的行为
    public Queue<Point>? MyNewQueue  { get; set; }
    public Point        MyNewTarget { get; set; }
    public bool         MyNewPathed { get; set; }
    public int          MyNewCount  { get; set; }
}
```

**Step 3 — 在 FollowSystem 添加 Start/Stop/Tick 方法**

```csharp
// StartMyNew: 扫描目标、初始化队列、设 Mode
public void StartMyNew(string npcName, NPC npc, int radius, ...)
{
    var state = GetOrCreate(npcName);
    // ... 扫描游戏世界，填充 state.MyNewQueue ...
    state.Mode = NpcBehaviorMode.MyNewAction;
    npc.Speed = 5;  // Agent 驱动时加速
}

// StopMyNew: 清队列，恢复 Idle
private void StopMyNew(string npcName, NPC npc)
{
    var state = GetOrCreate(npcName);
    state.MyNewQueue?.Clear();
    state.Mode = NpcBehaviorMode.Idle;
    npc.Speed = 2;
}

// TickMyNew: 每 tick 移动/执行一个 item
private void TickMyNew(string npcName, NPC npc)
{
    var state = GetOrCreate(npcName);
    // ... 寻路逻辑：到达目标 → 执行动作 → dequeue 下一个 ...
    if (state.MyNewQueue?.Count == 0)
        SetMode(npcName, NpcBehaviorMode.Idle);
}
```

参考：`WanderHandler.DoWander()` / `ClearDebrisHandler` 中的 `StartClearDebris` 模式。

**Step 4 — 创建 Handler 类（`WorldActionHandlers.cs` 或新文件）**

```csharp
internal sealed class MyNewActionHandler : NpcActionHandlerBase
{
    private readonly FollowSystem _follow;
    private readonly NpcInventory? _inventory;

    protected override string ActionName => "npc_my_new_action";
    protected override bool RefuseWhileBusy => true;   // 长时操作
    protected override bool IsPreemptable => false;     // 不可抢占

    public MyNewActionHandler(IMonitor log, Func<bool> showBubble,
        NpcInventory? inventory, FollowSystem follow) : base(log, showBubble)
    {
        _inventory = inventory;
        _follow = follow;
        SetBusyGate(follow);
    }

    protected override string ResolveBubble(JsonElement @params)
    {
        // 返回头顶气泡文本
        return "[my_action] 开始工作";
    }

    protected override void Execute(NPC npc, string npcName, JsonElement @params)
    {
        int radius = /* 解析 @params */;
        // 调用 FollowSystem 启动行为
        _follow.StartMyNew(npcName, npc, radius, ...);
    }
}
```

**Step 5 — 在 `ModEntry.cs` 注册**

```csharp
// actionHandlers 数组里加一行
new MyNewActionHandler(this.Monitor, showBubble, _npcInventory, _follow),

// foreach 自动注册 _router.Register(h.ActionNamePublic, h.Handle)
```

### 1.2 协议层：`protocol.go` 添加常量

`smartnpc-mcp/adapters/stardew/bridge/protocol.go`：

```go
ActionNpcMyNewAction = "npc_my_new_action"
```

### 1.3 Go MCP 工具层

**Step 1 — 在 `npc_world_action.go`（或新文件）添加 Input/Output struct**

```go
// ── npc_my_new_action ─────────────────────────────────────────────

type NpcMyNewActionInput struct {
    NPC    string `json:"npc"              jsonschema:"NPC internal name"`
    Radius int    `json:"radius,omitempty" jsonschema:"tile radius (default 5, max 30)"`
}

type NpcMyNewActionOutput struct {
    OK      bool   `json:"ok"`
    NPC     string `json:"npc"`
    Count   int    `json:"count,omitempty"`
    Message string `json:"message,omitempty"`
}
```

**Step 2 — 实现 Handler 函数**

```go
func handleNpcMyNewAction(br *bridge.WSClient) mcp.ToolHandlerFunc {
    return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        var in NpcMyNewActionInput
        if err := decode(req.Params.Arguments, &in); err != nil {
            return mcp.NewErrorResult(err.Error()), nil
        }
        // 转发 ws action 到 Mod
        resp, err := br.Call(ctx, bridge.ActionNpcMyNewAction, map[string]any{
            "npc":    in.NPC,
            "radius": in.Radius,
        })
        if err != nil {
            return mcp.NewErrorResult(err.Error()), nil
        }
        var out NpcMyNewActionOutput
        if err := json.Unmarshal(resp.Result, &out); err != nil {
            return mcp.NewErrorResult(err.Error()), nil
        }
        return mcp.NewToolResult(out), nil
    }
}
```

**Step 3 — 在 `registerNpcWorldAction()` 注册**

`npc_world_action.go` 底部：

```go
s.Register("npc_my_new_action", handleNpcMyNewAction(br))
```

### 1.4 Hermes 配置层：更新工具白名单

两个文件需要同步：

**`hermes/profiles/_master/config-overlay.yaml`** — 在 `tools.include` 列表添加：

```yaml
- npc_my_new_action
```

**`deploy/hermes/scripts/entrypoint.sh`** — 在 `tools: include:` 列表同步添加。

重跑 `bash scripts/render_profiles.sh` + `bash deploy/hermes/scripts/sync-profiles.sh`。

### 1.5 添加调试命令（可选）

`smapi-mod/Debug/DebugCommands.cs`：

```csharp
private const string CmdMyNew = "smartnpc_my_new";

// 在 Register() 里加
commands.Add(
    name: CmdMyNew,
    documentation: $"Usage: {CmdMyNew} <NpcName> [radius]",
    callback: (_, args) => HandleMyNew(args, log, follow));

// Handler 方法
private static void HandleMyNew(string[] args, IMonitor log, FollowSystem follow)
{
    if (args.Length < 2) { /* usage */ return; }
    var npc = Game1.getCharacterFromName(args[1]);
    if (npc == null) { log.Log("NPC not found", LogLevel.Warn); return; }
    int radius = args.Length > 2 && int.TryParse(args[2], out var r) ? r : 8;
    // 直接调用静态方法
    MyNewActionHandler.DoMyNew(npc, args[1], radius, follow, log);
    log.Log($"[{CmdMyNew}] {args[1]} started", LogLevel.Info);
}
```

---

## 2. 新增工作流 / Skill

### 2.1 核心设计：Skill 是真相源，YAML 是薄包装

SmartNPC 的工作流不是手写 YAML 配置文件——**Skill（Markdown）**才是真正的行为定义，由 AI 在预编译阶段读入、理解、生成具体执行计划。

```
开发者写 SKILL.md（Markdown，给 LLM 读）
        │
        ▼
  precompile 阶段：LLM 读 SKILL → 在 recording 模式下跑一次
  （RecordingRunner 拦截 mutating tool call，记录为 concrete step）
        │
        ▼
  预编译产物：workflow.Definition{Steps: [tool, tool, branch, ...]}
  （存入 Entry.PrecompiledDef，触发时零 LLM 重放）
```

**内置 Workflow YAML**（`pkg/workflow/builtin/*.yaml`）**只做一件事**：声明一个 ID + 委托给对应 SKILL。

```yaml
# pkg/workflow/builtin/farm_care.yaml
id: farm_care
description: 农场日常养护——Agent 根据 smartnpc-farm-care skill 动态决策
inputs:
  - name: inspect_radius
    description: 巡视半径
    default: 30
steps:
  - kind: skill_call       # ← 唯一的 step：委托给 SKILL
    skill: smartnpc-farm-care
    args:
      inspect_radius: "$inspect_radius"
```

这个 YAML 的作用：
1. `workflow_list` 工具能发现它（LLM 调用 `npc_plan_day` 时知道可选哪些 workflow）
2. 预编译管道知道触发哪个 SKILL
3. `workflow_get` 能返回其 input schema（LLM 知道传什么参数）

> **所有 9 个内置 workflow 都是这个模式**——不包含 `tool`/`branch`/`random` 等手工步骤。

### 2.2 两条执行路径

| | Precompile 路径（推荐） | 实时 Skill 路径（fallback） |
|---|---|---|
| 何时用 | Schedule trigger time | 没有预编译产物 / 场景太复杂 |
| LLM 参与 | 仅在 precompile 阶段一次 | 每次触发都调 LLM |
| 延迟 | ~0（本地引擎重放） | LLM API 往返（1-5s） |
| 确定性 | 高（concrete steps） | 低（LLM 每次可能不同） |
| 实现 | `PrecompileSkill` → `RecordingRunner` → `Entry.PrecompiledDef` | `skill_call` step → hermesrelay synthetic event |

**Precompile 流程详解：**

```
1. npcWorkflowWorker 扫描未预编译的 Entry
2. 调 MCPRunner.PrecompileSkill(npc, skill, args)
3. MCPRunner 通过 hermesrelay 发 workflow_skill_call 事件（precompile=true）
4. Hermes Agent 收到事件 → LLM 在 recording 模式下跑 skill
   - LLM 调 inspect / get 等读工具 → RecordingRunner 代理到真实 ws（获取真实环境数据）
   - LLM 调 water / harvest 等写工具 → RecordingRunner 拦截记录（不真执行）
   - LLM 调 workflow_precompile_result → 提交完整执行计划
5. MCPRunner 收到计划 → 解析为 Definition → 写入 Entry.PrecompiledDef
6. 触发时间到 → engine 直接重放 PrecompiledDef.Steps（零 LLM）
```

### 2.3 新增一个 Workflow（= 新增一个 SKILL）

**Step 1 — 写 SKILL.md**

在 `hermes/profiles/_master/skills/smartnpc/smartnpc-my-feature/SKILL.md`：

```markdown
---
name: smartnpc-my-feature
description: 我的功能——在什么情况下做什么
type: skill
---

# {{NPC_NAME}} 的我的功能

## 触发场景
- 每天早上作为 farm_care 的一部分检查
- 玩家靠近农场时

## 建议工作流示例

### 基础版
1. `npc_inspect_object` (radius=30) → 拿到 actions_available
2. 如果 actions_available.water.count > 0:
   - `npc_water_crops` (bbox=actions_available.water.bbox)
3. 如果没有任何可做：`npc_emote` 发个爱心然后结束

### 进阶版（收货+存放）
1. 先巡视 → 如果 crops_ready_to_harvest → 调 `npc_harvest_crops`
2. 然后 `npc_deposit_items` (auto_find=true)
3. 最后巡视检查有没有漏的

## 约束
- 只在农场地图操作
- 不要进别人家
- 一次不要超过 5 个工具调用
```

**Step 2 — 注册薄包装 YAML（可选但推荐）**

`smartnpc-mcp/pkg/workflow/builtin/my_feature.yaml`：

```yaml
id: my_feature
description: 我的功能——Agent 根据 smartnpc-my-feature skill 动态决策
version: "1"
inputs:
  - name: inspect_radius
    description: 巡视半径
    default: 30
steps:
  - kind: skill_call
    skill: smartnpc-my-feature
    args:
      inspect_radius: "$inspect_radius"
```

内嵌自动生效（`//go:embed builtin/*.yaml`），**无需改任何 Go 代码**。

**Step 3 — 渲染 + 同步**

```bash
bash scripts/render_profiles.sh    # _master/ → 所有 NPC
task profiles:verify               # 校验
bash deploy/hermes/scripts/sync-profiles.sh  # → Docker
```

**Step 4 — 可选：在 SOUL.md 或 cron-recipes.md 中引用**

让 LLM 知道这个 workflow 存在、何时该用。

### 2.4 不需要 YAML 包装器的情况

如果 workflow 只在 cron 或事件直接触发而不需要出现在 `workflow_list`，可以只写 SKILL.md，直接通过 `skill_call` 触发：

```yaml
# 在 cron-recipe 或事件中直接引用 skill 名
- kind: skill_call
  skill: smartnpc-my-feature
```

### 2.5 SKILL 写作要点

**LLM 在 precompile 模式下读 SKILL 来决定调什么工具**，所以 SKILL 必须：

1. **给出具体工具名和参数建议**——不是抽象描述，"用 npc_water_crops" 而不是 "给作物浇水"
2. **包含示例工作流**——帮助 LLM 理解典型步骤组合
3. **给出终止条件**——什么情况下不做事/提前结束
4. **注明约束边界**——地图范围、NPC 关系、时间限制
5. **使用 `{{NPC_NAME}}` 等占位符**——渲染后每个 NPC 有自己的版本

实际执行时 LLM 会：
- 先调读工具（`npc_inspect_object` / `game_get_time`）获取实时环境数据
- 根据数据决定调哪些写工具、传什么参数
- Precompile 模式下，写工具被 `RecordingRunner` 拦截记录，不真执行
- 最后调 `workflow_precompile_result` 提交完整计划

### 2.6 8 种 Step 类型的角色

虽然 builtin YAML 只用 `skill_call`，但**预编译产物**会包含多种 step：

| Kind | 来源 | 用途 |
|------|------|------|
| `tool` | RecordingRunner 记录 | 预编译后的具体工具调用（含具体参数） |
| `branch` | LLM 通过 `workflow_precompile_result` 提交 | 条件分支（基于预编译时已知的变量） |
| `skill_call` | 手写 YAML | 委托给 SKILL（触发时实时 LLM 决策） |
| `random` | LLM 提交 | 加权随机选一行为 |
| `foreach` | LLM 提交 | 遍历列表逐项处理 |
| `llm_choice` | LLM 提交 | 运行时 LLM 二选一（昂贵，少用） |
| `wait` | LLM 提交 | 等待 FollowSystem Idle |
| `stop` | LLM 提交 | 提前终止 |

手写 YAML 只用 `skill_call`；预编译后 `tool` 和 `branch` 成为主要 step 类型。

---

## 3. 新增/修改 Skill

### 3.1 Skill 文件位置

所有 Skill 的母本在 `hermes/profiles/_master/skills/smartnpc/<skill-name>/SKILL.md`。

```
hermes/profiles/_master/skills/smartnpc/
├── smartnpc-core/SKILL.md           # MCP 工具清单与使用指南
├── smartnpc-farm-care/SKILL.md      # 农场巡视+动态决策
├── smartnpc-farm-cleanup/SKILL.md   # 清理杂物
├── smartnpc-greeting/SKILL.md       # 主动打招呼
├── smartnpc-social-interact/SKILL.md # 社交互动
└── ...
```

### 3.2 新增 Skill

```bash
# 1. 在 _master/ 下创建目录结构和 SKILL.md
mkdir -p hermes/profiles/_master/skills/smartnpc/smartnpc-my-skill

# 2. 写 SKILL.md
# 3. 渲染到所有 NPC
bash scripts/render_profiles.sh

# 4. 验证
task profiles:verify

# 5. Docker 侧同步
bash deploy/hermes/scripts/sync-profiles.sh
```

### 3.3 SKILL.md 写作规范

```markdown
---
name: smartnpc-my-skill
description: 我的技能——做什么、何时触发
type: skill
---

# NPC_NAME 的我的技能

## 触发条件
- 当玩家靠近农场时
- 每天早上 8:00

## 可用工具
- `npc_inspect_object` — 巡视周围
- `npc_water_crops` — 浇水
- `npc_emote` — 表情

## 决策流程
1. 先调 `npc_inspect_object` 看农场状态
2. 如果作物缺水 → 调 `npc_water_crops`
3. 如果没事可做 → 调 `npc_emote` 发爱心

## 约束
- 只在自己的农场区域活动
- 不要进玩家房子
- 不和 vanilla NPC 互动
```

### 3.4 占位符

渲染时 `render_profiles.sh` 替换以下占位符：

| 占位符 | 含义 | 示例值 (xiami) |
|--------|------|---------------|
| `{{NPC_NAME}}` | PascalCase 内部名 | `XiaMi` |
| `{{NPC_DISPLAY}}` | 中文显示名 | `夏弥` |
| `{{NPC_DIR}}` | 小写目录名 | `xiami` |
| `{{NPC_PORT}}` | Gateway 端口 | `8642` |
| `{{PEER_A_NAME}}` | 第一个 peer | `Penny` |
| `{{PEER_A_DISPLAY}}` | 第一个 peer 中文名 | `潘妮` |
| `{{PEER_B_NAME}}` | 第二个 peer | `Abigail` |
| `{{PEER_B_DISPLAY}}` | 第二个 peer 中文名 | `阿比盖尔` |

### 3.5 Skill 名 vs Workflow 名

| | Skill | Workflow |
|---|---|---|
| 位置 | `hermes/profiles/_master/skills/` | `smartnpc-mcp/pkg/workflow/builtin/` |
| 语言 | Markdown（给 LLM 读） | YAML（给引擎执行） |
| 执行方 | Hermes Agent（LLM 决策+工具调用） | mcp workflow engine（确定性步骤） |
| 触发方式 | `skill_call` step / cron / 事件注入 | `npc_plan_day` 的 `workflow_id` 引用 |

简单工作流直接写 YAML，复杂/需要 LLM 判断的走 SKILL。

---

## 4. 新增 Schedule 条目

### 4.1 Schedule 数据模型

每个 NPC 的每日计划由 `npc_plan_day` MCP 工具设置，LLM 在每天开始时调用一次。

`smartnpc-mcp/adapters/stardew/scheduler/scheduler.go` 中 `Entry` 支持三种形式（优先级从高到低）：

```go
type Entry struct {
    GameHour    int    `json:"game_hour"`
    GameMinute  int    `json:"game_minute,omitempty"` // 0/10/20/30/40/50
    Reason      string `json:"reason,omitempty"`

    // 形态1（推荐）：引用内置 workflow
    WorkflowID string         `json:"workflow_id,omitempty"`
    Args       map[string]any `json:"args,omitempty"`

    // 形态2：内联 workflow 定义
    Workflow *workflow.Definition `json:"workflow,omitempty"`

    // 形态3（已弃用）：单步 tool
    Action string `json:"action,omitempty"`
}
```

### 4.2 LLM 视角的调度示例

LLM 调用 `npc_plan_day` 时的参数：

```json
{
  "npc": "XiaMi",
  "entries": [
    {
      "game_hour": 7,
      "game_minute": 0,
      "workflow_id": "farm_care",
      "args": { "inspect_radius": 30 },
      "reason": "早间农场巡视"
    },
    {
      "game_hour": 9,
      "game_minute": 30,
      "workflow_id": "social_round_robin",
      "args": {},
      "reason": "拜访朋友"
    },
    {
      "game_hour": 17,
      "game_minute": 0,
      "workflow_id": "farm_evening_close",
      "args": {},
      "reason": "傍晚收尾工作"
    }
  ]
}
```

### 4.3 调度执行流程

```
游戏时间推进 → game_time_tick event → scheduler.Tick(hour)
  → 匹配 Entry(MinutesOfDay == currentMinutes)
    → 标记 Fired=true
    → hermesrelay 发 schedule_trigger synthetic event
      → Hermes Agent 收到事件
        → 若 Entry.WorkflowID 非空：Agent 调 workflow_run_inline
        → mcp workflow engine 本地逐步执行（大部分步骤无需 LLM）
```

### 4.4 调度策略建议

| NPC 类型 | 推荐频率 | 示例 |
|---------|---------|------|
| 农场主 NPC | 3-6 条/天 | 早巡+午巡+社交+晚收 |
| 工人 NPC | 2-4 条/天 | 专项工作+休息 |
| 社交 NPC | 2-3 条/天 | 拜访+发呆 |

利用 workflow 的 `random` step 增加行为多样性，而不是让 LLM 每天选不同 workflow。

---

## 5. 调试与测试

### 5.1 Echo 模式（最快速的 ws 验证）

```powershell
task mcp:run-echo
```

不接 LLM，所有 NPC 回复原样回声。验证 ws 连接 + MCP 工具注册是否正常。

### 5.2 游戏内调试命令

在 SMAPI 控制台：

```
smartnpc_debug                 # 查看所有 Agent NPC 状态
smartnpc_status                # FollowSystem 当前模式
smartnpc_wander XiaMi          # 让夏弥开始闲逛
smartnpc_clear_debris XiaMi    # 清理杂物
smartnpc_goto XiaMi 64 20      # 移动到 (64,20)
smartnpc_friendship XiaMi      # 查看好感度
```

### 5.3 Workflow 调试

```powershell
# 列出所有内置 workflow
curl -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"workflow_list","arguments":{}},"id":1}' \
  http://127.0.0.1:3000/mcp

# 查看单个 workflow 定义
curl ... -d '{"name":"workflow_get","arguments":{"id":"farm_cleanup"}}' ...

# 干跑验证（不实际调 mod）
curl ... -d '{"name":"workflow_run_inline","arguments":{
  "npc":"XiaMi","workflow_id":"farm_cleanup","args":{"inspect_radius":30}
}}' ...
```

### 5.4 Go 测试

```powershell
# 工作流引擎单测
go test -run TestRun ./pkg/workflow/...
go test -run TestValidate ./pkg/workflow/...

# MCP 工具端到端测试（InMemoryTransport，不连真实 ws）
go test -run TestWander ./adapters/stardew/tools/...
go test -run TestClearDebris ./adapters/stardew/tools/...
```

### 5.5 查看 MCP 进程日志

`run.bat` 启动的 MCP 有独立 PowerShell 窗口；Docker 模式：

```bash
# 查看某个 gateway 日志
wsl docker compose -f /mnt/d/SmartNPC/deploy/hermes/docker-compose.yml logs --tail=100 hermes-xiami
```

---

## 6. 全层检查清单

新增行为/工具/工作流后，逐项确认：

| # | 检查项 | 命令/方法 |
|---|--------|----------|
| 1 | `task profiles:verify` 通过 | `task profiles:verify` |
| 2 | `task ci` 通过 | `task ci` |
| 3 | 工具白名单含新工具 | grep `hermes/profiles/_master/config-overlay.yaml` + `entrypoint.sh` |
| 4 | Echo 模式可调通 | `task mcp:run-echo` → 游戏内调 `smartnpc_xxx` 命令 |
| 5 | Docker entrypoint 工具列表对齐 | diff `config-overlay.yaml` 的 include vs `entrypoint.sh` 的 include |
| 6 | SOUL.md 引用新 skill/workflow（如需） | 对应 NPC 的 SOUL.md 中提及 |
| 7 | `docs/protocol.md` 记录新 ws action | 仅 ws 协议变更时需要 |
| 8 | 新增 Go package 带 `*_test.go` | 检查对应目录 |
| 9 | 新 MCP 工具配 InMemoryTransport 测试 | 参考 `*_test.go` |
| 10 | `render_profiles.sh` + `sync-profiles.sh` 跑通 | WSL 执行 |

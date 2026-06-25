---
name: smartnpc-action-development
description: Use when implementing a new NPC action — the full cross-layer workflow from MCP tool to C# handler to FollowSystem state to debug command. Also use when an action silently fails on vanilla NPCs.
---

# SmartNPC NPC Action Development

## 三层联动

```
Go MCP tool                      C# Mod                           FollowSystem
tools/npc_<action>.go            Behavior/WorldActionHandlers      Movement/FollowSystem.cs
  Input/Output + registry          <Action>Handler : Base           NpcBehaviorMode enum
                                   Debug/DebugCommands.cs           StartXxx() → TickXxx()
                                     smartnpc_<action> cmd           switch case 注册
```

**新增 action 必须四步全走**：Go tool → C# Handler → FollowSystem → Debug 命令。

---

## Step 1 — 瞬发 vs 持续

| 类型 | 特征 | 需要 FollowSystem? |
|------|------|--------------------|
| 瞬发 | Execute 里直接完成，不改 npc.controller | 否 |
| 持续 | 调用 PathFindController / 设 npc.controller | **是（必须）** |

> **黄金规则**：`npc.controller` 生命周期必须由 FollowSystem 拥有，否则原版 NPC（Abigail / Sebastian 等）的 Idle guard 下一 tick 会清掉 controller。

---

## Step 2 — Go MCP tool

- 文件：`smartnpc-mcp/adapters/stardew/tools/npc_<action>.go`
- Input/Output struct 必须有 `json` + `jsonschema` tag；Output 首字段 `OK bool`
- `registry.go` → `RegisterAll()` 注册
- 同步更新 `docs/protocol.md`

---

## Step 3 — C# Handler

- 文件：`smapi-mod/Behavior/WorldActionHandlers.cs`
- 继承 `NpcActionHandlerBase`，override `ActionName` 和 `Execute`
- **持续动作**：`Execute` 只做参数解析 → 调 `_follow.StartXxx()`，不直接碰 `npc.controller`
- 提取 `public static DoXxx()` 供 Debug 命令复用

---

## Step 4 — FollowSystem（持续动作）

文件：`smapi-mod/Movement/FollowSystem.cs`

1. `NpcBehaviorMode` 枚举加新值
2. `NpcBehaviorState` 加所需字段
3. 写 `StartXxx()` → 设 Mode + 目标 + `LastPathTick = 0`
4. 写 `TickXxx()` → 首 tick 发起寻路，后续 tick 检测路径耗尽回 Idle
5. `PumpOnGameTick` 的 switch 里加 `case` 分发

---

## Step 5 — Debug 命令

- 文件：`smapi-mod/Debug/DebugCommands.cs`
- 加 `const string CmdXxx = "smartnpc_<action>";`
- `Register` 里 `commands.Add(...)`
- Handler 复用 `Handler.DoXxx()` 静态方法

---

## Step 6 — ModEntry 接线

`ModEntry.cs` 构造 handler 并传入依赖（FollowSystem / inventory 等）。

---

## Step 7 — 验证

```
task mod:install          # 0 warning 0 error
```

游戏内：
1. `smartnpc_gather` — 聚 NPC
2. `smartnpc_<action> XiaMi` — 测自定义 NPC
3. `smartnpc_<action> Abigail` — **测原版 NPC（最容易暴露问题）**
4. 确认 SMAPI 日志无异常

---

## 常见陷阱

| 症状 | 原因 | 修法 |
|------|------|------|
| XiaMi 动但原版 NPC 不动 | `npc.controller` 直接赋值，被 Idle guard 清掉 | 走 FollowSystem.StartXxx |
| NPC 动一下就停 | Idle guard 仍在清 controller | 检查 NpcBehaviorMode 新值已加入 switch |
| pathLen=-1 | `currentLocation` 为 null | `gather` 先或加 null guard |
| Debug 命令结果和 MCP 不一致 | Handler 逻辑未提取为 static | 提取 `public static DoXxx` 两端共用 |

**Idle guard 原理**：`FollowSystem` 每 tick 在 Idle 模式下 `npc.Halt()` + 清 controller，这是为了对抗原版 NPC schedule 系统自动注入的 controller。任何持续动作必须通过非 Idle 的 FollowSystem 模式保护。

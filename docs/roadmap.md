# SmartNPC 路线图 (Roadmap)

本文档维护项目的 milestone 拆分、当前进度、以及 monorepo 工程化演进路线。
每个 milestone 完成后由用户验证，再进入下一个。

---

## 进度速览

| Milestone | 目标 | 状态 |
|-----------|------|------|
| **M1** | Go workspace + stdio MCP server 骨架 + agent 客户端能调 `ping` | ✅ 已完成 |
| **M1.5** | 工程化外壳：Taskfile + GitHub Actions CI + Release workflow | ✅ 已完成 |
| **M2** | SMAPI Mod + WebSocket 桥接 + 游戏内聊天 | ✅ 已完成 |
| **M3** | NPC 精灵系统 + 自定义 NPC 注册 | ✅ 已完成 |
| **M4** | Agent 对话系统 + 聊天 UI + 游戏状态工具 | ✅ 已完成 |
| **M5 (旧)** | Go agent 内置 SQLite 记忆 + 调度 + 多 NPC 编排 | ⛔ **冻结** — 重定向为 M5(Hermes-first) |
| **M5 (Hermes-first)** | smartnpc-agent 退出主链路；MCP 强化 + Hermes profile per NPC | ✅ 代码 + 6 NPC 配置就绪 + 实机端到端验证通过（2026-05-12，含 delegate-fix） |

---

## ⛔ smartnpc-agent 冻结声明

**自 2026-05-11 起**，`smartnpc-agent/` **不再接受新功能**：

- 不再向 `smartnpc-agent/internal/` 添加 memory / scheduler / multi-NPC pool / event router 等模块
- `internal/agent/`、`internal/llm/`、`personas/` 仅作为回归对照保留
- commit `b56d439` 落地的 M5 第一版（memory / delegation / proactive / group chat / QQ-UI）**不再继续扩展**，作为"旧路线对照实现"封存
- 仅允许：bug fix、依赖升级、为对照测试服务的最小改动

**原因**：详见 [REFACTOR.md](../REFACTOR.md)。当前项目同时存在两个 Agent 中心（Go agent + Hermes Agent），抽象重复且边界不清。Hermes-first 路线把决策/记忆/技能/反思全部交给 Hermes，Go 侧只保留游戏能力边界。

---

## M5 (Hermes-first) — Hermes-first NPC runtime 🔧

**目标**：把 NPC 决策/记忆/人格/技能/反思全部迁移到 Hermes profile；smartnpc-mcp 强化为正式 MCP Server；smartnpc-agent 退出主运行链路。

### 目标架构

```
Stardew Valley / SMAPI Mod
        |
        | WebSocket JSON envelope (port :18745)
        v
smartnpc-mcp
  - MCP Server（stdio + streamable HTTP）
  - 游戏协议适配
  - tool schema / event notification
  - 参数校验 / 限流 / 幂等
        |
        | MCP Streamable HTTP（正式）/ stdio（dev）
        v
Hermes Agent Profile (per NPC)
  - SOUL.md（人格）
  - memory（state.db / 可选 memory provider plugin）
  - skills（行为流程）
  - tool planning + tool calling
  - reflection / cron（主动行为）
```

### 任务拆分

| Phase | # | 任务 | 状态 | 关键产物 |
|---|---|---|---|---|
| 0 | 5.0 | 冻结 smartnpc-agent，重写路线图，更新 CLAUDE.md | ✅ | 本文档 + CLAUDE.md |
| 0 | 5.0b | 确认 Hermes 是否原生接 MCP notification 作 trigger（决定 5.4 走方案 A/B/C） | ✅ | `docs/hermes-event-trigger.md`（锁定方案 B） |
| 1 | 5.1 | smartnpc-mcp 加 streamable HTTP transport（`--http :3000`） | ✅ | `cmd/smartnpc-mcp/main.go::runHTTP` |
| 1 | 5.2 | 重写 tool description 为"操作手册式"，给 LLM 看 | ✅ | `smartnpc-mcp/internal/tools/*.go` description 字段 |
| 1 | 5.3 | 新增 `npc_send_message` / `npc_broadcast_event` / `npc_inbox_*` 正式 MCP tool | ✅ | `smartnpc-mcp/internal/tools/npc_message.go` |
| 1 | 5.4 | 事件 payload 规范化（chat_message / npc_interact 已实现；day_started / location_changed / friendship_changed 保留 schema） | ✅ | `smartnpc-mcp/internal/events/` + `docs/events.md` |
| 2 | 5.5 | 起 `hermes/profiles/xiami/`：SOUL.md + skills + mcp.yaml | ✅ | `hermes/profiles/xiami/` + `hermes/install.sh`；`hermes mcp test smartnpc_game` 可 discover 工具 |
| 2 | 5.6 | 跑通"玩家聊天 → Hermes profile → chat_say"端到端 | ✅ | `smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go` + `docs/manual-e2e-verification.md`；E2E 验证 2026-05-12（含 delegate-fix） |
| 2 | 5.7 | 跑通"问时间/天气/好感度 → Hermes 自动调 game_* 工具" | ✅ | 同上 |
| 3 | 5.8 | 事件触发链路实装（方案 B：smartnpc-mcp outbound HTTP → Hermes Gateway） | ✅ | `smartnpc-mcp/internal/hermesrelay/` + main.go `--hermes-*` flags |
| 3 | 5.9 | NPC 主动打招呼（npc_interact event → Hermes） | ✅ | `hermes/profiles/xiami/skills/smartnpc/proactive-greeting/SKILL.md` |
| 4 | 5.10 | 长期记忆迁移：Hermes 内置 state.db + FTS5 | ✅ | `hermes/profiles/xiami/skills/smartnpc/memory-policy/SKILL.md` (Hermes 内置 state.db 直接用) |
| 4 | 5.11 | 反思 / cron 主动行为 | ✅ | `hermes/profiles/xiami/cron-recipes.md` |
| 4 | 5.12 | 多 NPC profile 路由：SMAPI mod 里 AudibleNPCResolver + TurnQueue | ✅ | `smapi-mod/NPC/AudibleNPCResolver.cs` + `TurnQueue.cs` + `ModEntry.cs` 接入 |
| 5 | 5.13 | smartnpc-agent 降级为 dev harness | ✅ | `smartnpc-agent/README.md` + `CLAUDE.md` |
| 5 | 5.14 | 文档拆分 | ✅ | `docs/architecture.md` + `hermes-profiles.md` + `mcp-tools.md` + `migration-smartnpc-agent.md` |

### 5.0b 事件触发方案对比

| 方案 | smartnpc-mcp 改动 | 实时性 | 前提 |
|---|---|---|---|
| **A. MCP notification → Hermes trigger** | 仅发 notification | 最好 | Hermes 必须支持 notification 作为 agent 入口 |
| **B. smartnpc-mcp POST 到 Hermes Gateway** | 加 HTTP outbound client + Hermes 路由配置 | 好 | Hermes 暴露可被外部 POST 注入消息的端点（已知 `/v1/responses` + `conversation:` 满足） |
| **C. Hermes 轮询 `game_next_event`** | 加 2 个事件队列工具（`game_next_event` / `game_ack_event`） | 弱（秒级延迟） | 无外部依赖 |

5.0b 完成后写明结论并锁定方案，再启动 5.8。

### 应保留在 smartnpc-mcp / SMAPI mod 的"硬逻辑"

不要把所有规则都扔给 Hermes prompt。以下规则**必须**在 MCP server 或 SMAPI mod 里硬校验：

1. 工具参数合法性
2. NPC 是否存在
3. 地点是否可达
4. 当前游戏状态是否允许操作
5. 写操作权限（改好感度、给物品、传送）
6. tool timeout / retry / reconnect
7. 高频工具限流
8. 同一 NPC 同时多条消息的并发顺序
9. `chat_say` 文本长度、特殊字符、UI 限制
10. 游戏主线程调用安全

Hermes prompt / skill 是软约束；MCP handler 才是硬边界。

### 里程碑验收

- [ ] **M5.A**：smartnpc-mcp `--http :3000` 启动成功，Hermes profile 通过 `mcp_servers:` 接入并能调用 `chat_say`
- [ ] **M5.B**：玩家在 ChatWindow 输入 → Hermes XiaMi profile 回复 → `chat_say` 显示在游戏内
- [ ] **M5.C**：玩家问"现在几点"，XiaMi profile 自动调 `game_get_time` 后回复正确时间
- [ ] **M5.D**：NPC `npc_interact` 事件触发 Hermes，NPC 主动打招呼（依赖 5.0b 选定方案）
- [ ] **M5.E**：3 天未互动 cron 触发 → XiaMi 主动行为
- [ ] **M5.F**：第二个 NPC profile 上线，AudibleNPCResolver 路由正确
- [ ] **M5.G**：smartnpc-agent 从启动文档下线，README 仅展示 Hermes 路径

---

## M6 — Hermes-first 完工与归档

M5 实机验证通过后启动。

| # | 任务 | 备注 |
|---|------|------|
| 6.1 | Group chat orchestration（Hermes 侧） | 当前 mod 端 group UI 仍在；Hermes profile 需要补 group-chat skill / cron |
| 6.2 | smartnpc-agent 删除 | ✅ 已完成（2026-05-14）：`smartnpc-agent/` 整体从 `go.work` / `Taskfile.yml` / CI 中移除；历史代码见 git log |
| 6.3 | 移除 mcp 兼容旧链路的 legacy flags | 删除 `--hermes-url` / `--hermes-npc` / `--hermes-conversation` / `--hermes-model`；统一用 `--hermes-config` |

---

## M4 文件清单（保留作为历史）

### C# Mod 新增/修改

```
smapi-mod/
├── UI/
│   ├── ChatWindow.cs           ← 聊天窗口（TextBox + 消息历史）
│   ├── FriendListWindow.cs     ← 好友列表（F2 打开）
│   └── ChatMessageStore.cs     ← 消息存储
├── Query/
│   └── GameQueryHandler.cs     ← game_get_time / weather / friendship
├── Patches/
│   └── NpcDialoguePatch.cs     ← 点击 NPC → 打开聊天窗口
├── NPC/
│   ├── XiaMiData.cs            ← NPC 注册 + 精灵加载
│   └── AgentNpcRegistry.cs     ← Agent NPC 注册表
├── Chat/
│   └── ChatHandler.cs          ← 路由 chat_say 到 UI 或 DrawDialogue
└── ModEntry.cs                 ← F2 快捷键 + 全部注册
```

### Go MCP 新增/修改

```
smartnpc-mcp/internal/
├── tools/
│   ├── game_query.go           ← game_get_time / weather / friendship 工具
│   └── registry.go             ← 注册新工具
└── bridge/
    └── protocol.go             ← 新 action 常量
```

---

## 启动全栈（M4 当前路径，待 M5 完成后由 Hermes 路径替代）

```cmd
:: 1. 构建
task ci

:: 2. Hermes (WSL)
hermes -p xiami gateway run --accept-hooks

:: 3. 安装 mod + 启动游戏（游戏关闭状态）
task mod:install
"D:\Stardew Valley\StardewModdingAPI.exe"

:: 4. Agent（新 cmd 窗口）—— M5 完成后此步骤替换为 Hermes profile run
cd /d d:\SmartNPC\smartnpc-agent
bin\smartnpc-agent.exe ^
  -mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe ^
  -mcp-args="--ws-url=ws://127.0.0.1:18745/ws" ^
  -log-level debug ^
  run ^
  -llm-url http://localhost:8643/v1 ^
  -api-key xiami-npc-key ^
  -model xiami ^
  -speaker XiaMi ^
  -persona ..\smartnpc-agent\personas\xiami.json
```

**M5 完成后正式启动方式**（草案，最终以 5.13 文档为准）：

```cmd
:: 1. SMAPI 启动游戏
"D:\Stardew Valley\StardewModdingAPI.exe"

:: 2. smartnpc-mcp（新 cmd 窗口）
smartnpc-mcp.exe --http :3000 --ws-url ws://127.0.0.1:18745/ws

:: 3. Hermes profile per NPC（WSL）
hermes -p xiami gateway run --accept-hooks
```

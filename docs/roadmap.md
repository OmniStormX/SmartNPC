# SmartNPC 路线图 (Roadmap)

本文档维护项目的 milestone 拆分、当前进度、以及 monorepo 工程化演进路线。
每个 milestone 完成后由用户验证，再进入下一个。

---

## 进度速览

| Milestone | 目标 | 状态 |
|-----------|------|------|
| **M1** | Go workspace + stdio MCP server 骨架 + agent 客户端能调 `ping` | ✅ 已完成 |
| **M2** | SMAPI Mod 骨架 + WebSocket 桥接协议 + `bridge_ping` 端到端 | ⬜ 未开始 |
| **M3** | NPC 行为工具集（query / dialogue / movement / friendship） | ⬜ 未开始 |
| **M4** | OpenAI Provider 实现 + 单 NPC Agent loop（Abigail 可对话） | ⬜ 未开始 |
| **M5** | SQLite+FTS5 记忆 + Cron 调度 + 多 NPC 编排 | ⬜ 未开始 |

---

## M1 — 基础骨架 ✅

**产出**：

- `go.work` 联动 `smartnpc-mcp` + `smartnpc-agent` 两个 module
- `smartnpc-mcp` stdio server，注册 `ping` 工具，日志走 stderr
- `smartnpc-agent` CLI（`tools` / `ping` 子命令），通过 `CommandTransport` spawn mcp 子进程
- `internal/llm/` provider 抽象 + OpenAI stub（M4 实装）
- `tools/meta_test.go` 用 `InMemoryTransport` 端到端验证 PASS
- `.codebuddy/rules/` 4 条项目规则

**验证方式**：

```cmd
d: && cd d:\SmartNPC && go test ./smartnpc-mcp/... ./smartnpc-agent/...
```

---

## M2 — SMAPI Mod 骨架 + WS 桥接

**目标**：游戏内 Mod 与 `smartnpc-mcp` 双向通信跑通，新增一个 `bridge_ping` 工具透传 ws ping 到 Mod。

| # | 任务 | 关键产物 |
|---|------|---------|
| 2.1 | C# 项目骨架 | `smapi-mod/StardewMCPBridge.csproj`、`manifest.json`、`ModEntry.cs` |
| 2.2 | 内嵌 WebSocket Server | `smapi-mod/Bridge/WebSocketServer.cs`（`System.Net.WebSockets`，监听 `:8765`） |
| 2.3 | 协议 DTO（C# 侧） | `smapi-mod/Bridge/Protocol.cs`（Request/Response/Event） |
| 2.4 | 协议文档 | `docs/protocol.md`（JSON schema、action 命名规范、错误码） |
| 2.5 | Go ws 客户端 | `smartnpc-mcp/internal/bridge/client.go`（`coder/websocket`，含重连） |
| 2.6 | Go 协议类型 | `smartnpc-mcp/internal/bridge/protocol.go`（与 C# 端镜像） |
| 2.7 | `bridge_ping` 工具 | `smartnpc-mcp/internal/tools/bridge_meta.go`，端到端打通 |
| 2.8 | Mock bridge 用于测试 | `smartnpc-mcp/internal/bridge/mock.go` |
| 2.9 | 端到端测试 | `bridge_ping_test.go`：用 mock bridge 验证工具流 |

**里程碑**：在游戏里加载 mod → `smartnpc-agent ... bridge_ping` 收到 mod 真实响应。

---

## M3 — NPC 行为工具集

**目标**：MCP 暴露完整 NPC 操作面（约 20 个工具），任意 MCP client 能查询 / 控制 NPC。

| 工具组 | 工具 | 文件 |
|--------|------|------|
| Query | `npc_list`、`npc_get`、`npc_find_nearby`、`npc_get_schedule`、`npc_get_gift_taste`、`npc_get_dialogue_pool` | `tools/npc_query.go` |
| Dialogue | `npc_speak`、`npc_show_bubble`、`npc_ask_choice`、`npc_clear_dialogue` | `tools/npc_dialogue.go` |
| Movement | `npc_move_to`、`npc_warp`、`npc_face` | `tools/npc_movement.go` |
| Emote | `npc_emote`、`npc_play_animation` | `tools/npc_emote.go` |
| Schedule | `npc_override_schedule`、`npc_reset_schedule` | `tools/npc_schedule.go` |
| Friendship | `friendship_get`、`friendship_change` | `tools/friendship.go` |
| Events | `game_subscribe_event` + MCP notifications 推送 | `tools/events.go` |

**Mod 端配套**：

- `Npc/*Handler.cs`（每组对应一个 handler）
- `Patches/`：Harmony patch `DialogueBox.receiveLeftClick`、`NPC.checkAction`、`NPC.receiveGift`
- `Events/GameEventBroadcaster.cs`：把 `DayStarted` / `MenuChanged` / 礼物事件等推到 ws

**里程碑**：用 Claude Desktop 接 `smartnpc-mcp`，手动调工具能让 Abigail 在游戏里说话、走路、改好感度。

---

## M4 — OpenAI Agent Loop

**目标**：单个 NPC 通过 LLM 自主对话，闭环跑通"玩家点击 NPC → LLM 生成回复 → Mod 注入对话框"。

| # | 任务 | 关键产物 |
|---|------|---------|
| 4.1 | OpenAI provider 实装 | `smartnpc-agent/internal/llm/openai.go`（function calling 翻译） |
| 4.2 | MCP 工具 → OpenAI tool spec 转换 | `smartnpc-agent/internal/agent/toolbridge.go` |
| 4.3 | NPC 人格 YAML loader | `smartnpc-agent/internal/persona/{loader,prompt}.go` + `persona/templates/abigail.yaml` |
| 4.4 | Conversation context builder | `smartnpc-agent/internal/agent/context.go` |
| 4.5 | 单 NPC agent loop | `smartnpc-agent/internal/agent/npc_agent.go`（think → tool calls → repeat） |
| 4.6 | Agent 入口子命令 | `smartnpc-agent ... run --npc Abigail` |
| 4.7 | 配置文件 | `smartnpc-agent/configs/agent.yaml`（model / api_key / persona dir） |

**里程碑**：游戏内点 Abigail，弹出 AI 生成的、符合人格的对话。

---

## M5 — 记忆 / 调度 / 多 NPC 编排

**目标**：14+ NPC 并发自主行为，跨 session 记忆持久化，主动事件触发。

| # | 任务 | 关键产物 |
|---|------|---------|
| 5.1 | SQLite + FTS5 记忆存储 | `smartnpc-agent/internal/memory/{store,sqlite}.go` |
| 5.2 | 跨 NPC 消息队列 | `smartnpc-agent/internal/relay/message_queue.go` |
| 5.3 | 委托链 | `smartnpc-agent/internal/relay/delegate.go` |
| 5.4 | Cron 调度 | `smartnpc-agent/internal/scheduler/cron.go`（`robfig/cron/v3`） |
| 5.5 | Agent 池（自主 ≤3 + 按需 ≤2） | `smartnpc-agent/internal/orchestr/{pool,trigger,orchestrator}.go` |
| 5.6 | 事件路由 | 把 `smartnpc-mcp` 的 MCP notifications 翻译成 trigger |
| 5.7 | 全部 14 个可结婚 NPC persona 模板 | `persona/templates/*.yaml` |

**里程碑**：玩家 3 天没找 Abigail，她主动在酒吧搭话（cron + 好感度 + 主动行为）。

---

## Monorepo 工程化演进

仓库已是 monorepo（单仓库 + 多语言子项目 + go.work）。下面是按痛点分级的演进路径，**不必一次到位**。

### Level 0 — 现状（M1 末）✅

`go.work` 联动 Go module，C# 子项目独立 dotnet build。够用。

### Level 1 — 统一任务运行器（建议 M2 前完成）

引入 **Taskfile** (`go-task/task`)：

```
d:/SmartNPC/
├── Taskfile.yml                ← 根任务：build / test / lint / clean
├── smartnpc-mcp/Taskfile.yml
├── smartnpc-agent/Taskfile.yml
└── smapi-mod/Taskfile.yml
```

收益：

- 命令统一：`task build`、`task test`、`task mcp:run -- ping`
- 跨平台、零依赖（单 Go 二进制）
- Windows 友好，比 Make 顺手

### Level 2 — 依赖版本协调（M3 或出现漂移时）

Go 双 module 共用 MCP SDK，可能漂移。两条路：

- **保持双 module**：写 `scripts/upgrade-go-deps.cmd` 批量 `go get -u`
- **合并单 module**：`smartnpc-mcp` / `smartnpc-agent` 改为同 module 下两个 `cmd/`

建议 M3 前观察，**真痛了再动**。

### Level 3 — 抽 `pkg/` 共享 module（M3 高概率需要）

触发条件：第二次复制粘贴 ws 协议 DTO 时立即动手。

```
d:/SmartNPC/
├── pkg/                            ← 新增 Go module
│   ├── protocol/                   ← ws 协议 DTO
│   └── npctypes/                   ← NPC / 好感度共享类型
└── go.work                         ← use ./pkg ./smartnpc-mcp ./smartnpc-agent
```

C# 端 DTO 后续考虑用 codegen 从 Go 端生成（M4+ 评估）。

### Level 4 — CI（M2 上线后）

`.github/workflows/ci.yml`：

- matrix：`go-mcp` / `go-agent` / `csharp-mod`
- path filter：只跑改动的子项目
- Go：`actions/setup-go@v5` + `task test`
- C#：`actions/setup-dotnet@v4` + `dotnet test`

### Level 5 — 进阶（暂不需要）

Bazel / Pants / Nx / Turborepo / Lerna 都不引入。规模和复杂度不匹配。

---

## 立即行动建议

- [ ] M2 启动前先做 Level 1（Taskfile）+ Level 4（GitHub Actions）一次性搞定
- [ ] M2 进行中观察是否需要 Level 3（共享 `pkg/`）
- [ ] 其它升级按需

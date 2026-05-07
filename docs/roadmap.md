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
| **M4** | Agent 对话系统 + 聊天 UI + 游戏状态工具 | 🔧 进行中 |
| **M5** | SQLite 记忆 + 调度 + 多 NPC 编排 | ⬜ 未开始 |

---

## M4 — Agent 对话系统 + 聊天 UI + 游戏状态工具 🔧

**目标**：玩家能通过自定义聊天窗口与 AI NPC 自由对话；Agent 能感知游戏状态（时间/天气/好感度）。

### 已完成

| # | 任务 | 状态 | 关键产物 |
|---|------|------|---------|
| 4.1 | OpenAI 兼容 provider + Hermes 后端 | ✅ | `smartnpc-agent/internal/llm/openai.go` |
| 4.2 | Persona JSON loader + soul_notes | ✅ | `smartnpc-agent/internal/agent/chat/persona.go` |
| 4.3 | XiaMi NPC 注册 + 精灵管线 | ✅ | `smapi-mod/NPC/XiaMiData.cs`, `build_spritesheet.py` |
| 4.4 | Agent NPC Registry + 对话限制移除 | ✅ | `AgentNpcRegistry.cs`, `NpcDialoguePatch.cs` |
| 4.5 | `npc_interact` event 广播 | ✅ | Mod → ws → mcp → agent notification |
| 4.6 | `chat_message` event（ChatWindow → Agent） | ✅ | agent 侧 `extractChatMessage()` |
| 4.7 | ChatWindow 聊天窗口 UI | ✅ | `smapi-mod/UI/ChatWindow.cs` |
| 4.8 | FriendListWindow 好友列表 UI | ✅ | `smapi-mod/UI/FriendListWindow.cs` (F2 快捷键) |
| 4.9 | ChatMessageStore 消息存储 | ✅ | `smapi-mod/UI/ChatMessageStore.cs` |
| 4.10 | 游戏状态查询工具 (MCP) | ✅ | `game_get_time`, `game_get_weather`, `friendship_get` |
| 4.11 | ChatHandler 路由到 UI / DrawDialogue | ✅ | `chat_say` 回复自动追加到聊天窗口 |

### 待验证

| # | 任务 | 状态 | 说明 |
|---|------|------|------|
| 4.12 | 全栈端到端验证 | ⏳ 待测 | 点击 NPC → 聊天窗口 → AI 回复 |
| 4.13 | F2 好友列表远程聊天验证 | ⏳ 待测 | 不面对面也能聊天 |
| 4.14 | 游戏状态工具 LLM 调用验证 | ⏳ 待测 | 问"几点了"NPC 能回答正确时间 |

### 已知问题 / 待修复

| 问题 | 优先级 | 说明 |
|------|--------|------|
| `npc_interact` 直接点击可能不触发 agent | P1 | 需验证完整链路：mod → ws → mcp → agent → LLM → chat_say |
| ChatWindow 文本输入中文可能不work | P2 | SDV TextBox 对中文 IME 支持有限 |
| 好感度注入 system prompt | P3 | Agent 回复前先查 friendship，M4 后期或 M5 实现 |

---

## M4 文件清单

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

### Go Agent 修改

```
smartnpc-agent/internal/agent/chat/
└── chat.go                     ← 处理 chat_message / npc_interact event
```

---

## M5 — 记忆 / 调度 / 多 NPC 编排（计划）

**目标**：14+ NPC 并发自主行为，跨 session 记忆持久化，主动事件触发。

| # | 任务 | 关键产物 |
|---|------|---------|
| 5.1 | SQLite + FTS5 记忆存储 | `smartnpc-agent/internal/memory/{store,sqlite}.go` |
| 5.2 | 好感度注入 system prompt | Agent 回复前查 `friendship_get`，动态调整语气 |
| 5.3 | 跨 NPC 消息队列 | `smartnpc-agent/internal/relay/message_queue.go` |
| 5.4 | Cron 调度 + 主动行为 | `smartnpc-agent/internal/scheduler/cron.go` |
| 5.5 | Agent 池（多 NPC 并发） | `smartnpc-agent/internal/orchestr/pool.go` |
| 5.6 | 事件路由 | MCP notifications → trigger |
| 5.7 | 多 NPC persona 模板 | `persona/templates/*.json` |

**里程碑**：玩家 3 天没找 XiaMi，她主动在农场搭话。

---

## 启动全栈

```cmd
:: 1. 构建
task ci

:: 2. Hermes (WSL)
hermes -p xiami gateway run --accept-hooks

:: 3. 安装 mod + 启动游戏（游戏关闭状态）
task mod:install
"D:\Stardew Valley\StardewModdingAPI.exe"

:: 4. Agent（新 cmd 窗口）
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

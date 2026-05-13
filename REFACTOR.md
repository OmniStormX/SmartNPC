推荐的新架构

我建议目标架构改成：

Stardew Valley / SMAPI Mod
        |
        | WebSocket JSON envelope
        v
smartnpc-mcp
  - MCP Server
  - 游戏协议适配
  - tool schema
  - event notification
  - 权限/参数校验
        |
        | MCP Streamable HTTP 或 stdio
        v
Hermes Agent Profile
  - NPC persona
  - memory
  - skill
  - tool planning
  - tool calling
  - reflection

也就是：

SMAPI Mod -> smartnpc-mcp -> Hermes Agent

smartnpc-agent 不再参与正式运行链路。

4. 这次重构的核心原则
原则一：smartnpc-mcp 只做“游戏能力边界”，不做 AI 决策

smartnpc-mcp 应该保留并强化这些职责：

1. 连接 SMAPI WebSocket
2. 把游戏动作包装成 MCP tools
3. 把游戏事件转成 MCP notifications / resources / event queue
4. 做参数校验、错误返回、限流、幂等
5. 提供 health/debug 工具

它不应该做：

1. 选择什么时候调用工具
2. 维护 NPC 记忆
3. 决定 NPC 性格
4. 生成自然语言回复
5. 管理多轮 LLM tool loop

这些都应该交给 Hermes。

原则二：规则写入 tool schema / Hermes profile / skill，而不是写在 Go Agent 里

当前代码里已经有将 MCP tools 的 description/inputSchema 转给 LLM 的逻辑。 重构后这套逻辑不应该在 smartnpc-agent 中存在，而应该由 Hermes MCP Adapter 完成。

规则位置建议如下：

控制内容	放在哪里
工具名、参数、字段说明	smartnpc-mcp/internal/tools/*.go 的 MCP tool schema
工具调用时机	tool description + Hermes skill
NPC 性格	Hermes profile 的 SOUL.md
NPC 长期记忆	Hermes memory
NPC 行为流程	Hermes skill
强约束，例如不能瞬移到非法地点	smartnpc-mcp handler 内部校验
游戏协议字段	docs/protocol.md + smartnpc-mcp/internal/bridge
原则三：事件接入要做成“触发 Hermes”，而不是“Go Agent 决策”

这是最容易踩坑的地方。

现在 smartnpc-agent 负责监听 chat_message、chat_received、npc_interact 等 MCP notification，然后主动调用 LLM。 如果删掉它，Hermes 必须有等价的事件入口。

所以你需要确认 Hermes 是否支持这两种方式之一：

A. Hermes MCP Adapter 可以把 MCP notifications 当作 agent trigger
B. Hermes Gateway/API 可以被外部事件调用，外部把游戏事件转成一条 Hermes message

如果 A 已经支持，最理想：smartnpc-mcp 直接发 MCP notification，Hermes profile 接收后决策。

如果 A 不支持，但 B 支持，那么可以加一个非常薄的 smartnpc-gateway 或者直接由 smartnpc-mcp 调 Hermes HTTP API。这个组件只能做 event forwarding，不能做 LLM 决策。

如果 A/B 都不支持，那就退一步：smartnpc-mcp 暴露一个事件轮询工具，例如：

game_next_event
game_ack_event

Hermes 周期性或通过 skill 主动查询待处理事件。但这会比 notification 方案弱一些，不适合实时聊天。

5. 更合理的重构方案
Phase 0：先冻结当前 smartnpc-agent 新功能

不要继续往 smartnpc-agent 里加 memory、scheduler、multi-NPC pool。路线图里 M5 现在计划把 SQLite 记忆、调度、多 NPC 编排放到 smartnpc-agent/internal/...。 如果决定以 Hermes 为主，这部分应整体迁移为 Hermes profile / memory / skill / scheduler 方案，而不是继续实现 Go 版。

建议把 M5 改成：

M5 — Hermes-first NPC runtime
5.1 Hermes profile per NPC
5.2 smartnpc-mcp event notifications
5.3 conversation memory mapping
5.4 NPC behavior skills
5.5 game-day reflection
5.6 multi-NPC profile routing
Phase 1：把 smartnpc-mcp 做成正式稳定的 MCP Server

smartnpc-mcp 是保留核心，应该强化而不是简化。

需要保证这些能力：

smartnpc-mcp
├── transport
│   ├── stdio
│   └── streamable http / --http :3000
├── tools
│   ├── chat_say
│   ├── game_get_time
│   ├── game_get_weather
│   ├── friendship_get
│   ├── npc_move_to
│   ├── npc_emote
│   ├── inventory_*
│   └── debug_ping / health
├── events
│   ├── chat_message
│   ├── npc_interact
│   ├── day_started
│   ├── day_ended
│   ├── location_changed
│   └── friendship_changed
└── bridge
    ├── ws client/server
    ├── request correlation
    ├── timeout
    └── reconnect

工具的 description 要写得像给 Agent 看的操作手册。例如：

chat_say:
Send a visible in-game message from an NPC to the player.
Use this only after you have decided the final in-character reply.
Do not include markdown. Keep text short enough for Stardew Valley dialogue UI.
friendship_get:
Read current friendship state between player and NPC.
Call this before giving relationship-sensitive advice, gifts, apologies, romance,
or emotionally intense responses.

注意：这类“call before”是软规则。强规则仍然要在 tool handler 内做。

Phase 2：把 smartnpc-agent 降级为 dev harness

不要一上来直接删。建议先改定位：

smartnpc-agent/
  cmd/smartnpc-agent/       -> deprecated / dev-only
  internal/agent/chat/      -> 保留一段时间做回归测试
  internal/llm/             -> 标记 deprecated
  personas/                 -> 迁移到 hermes profiles

README 中把正式运行方式改成 Hermes：

正式链路：
SMAPI Mod -> smartnpc-mcp --http :3000 -> Hermes profile

调试链路：
SMAPI Mod -> smartnpc-mcp stdio -> smartnpc-agent dev harness

这样不会破坏现有端到端测试，也方便对比 Hermes 行为和旧 Agent 行为。

Phase 3：建立 Hermes profile per NPC

你仓库的 CODEBUDDY 片段已经提到每个 NPC 对应一个独立 Hermes profile，并隔离 SOUL、记忆、API 端口。 这个方向是对的。

建议目录约定：

hermes/
├── profiles/
│   ├── xiami/
│   │   ├── SOUL.md
│   │   ├── skills/
│   │   │   ├── stardew-dialogue.md
│   │   │   ├── friendship-behavior.md
│   │   │   └── game-tool-policy.md
│   │   └── mcp.yaml
│   ├── abigail/
│   │   ├── SOUL.md
│   │   ├── skills/
│   │   └── mcp.yaml
│   └── common/
│       ├── stardew-world.md
│       └── tool-policy.md

SOUL.md 负责角色身份：

# XiaMi

你是 Stardew Valley 中的 NPC XiaMi。
你说话自然、简短、有生活感。
不要暴露自己是 AI、Agent、Hermes 或工具调用者。
当玩家询问当前时间、天气、好感度、位置等游戏状态时，先使用可用工具查询。

game-tool-policy.md 负责工具规则：

# SmartNPC Tool Policy

- 回复玩家前，如果问题涉及游戏状态，先调用对应 game_* 工具。
- 最终给玩家说话必须使用 chat_say。
- 不要在普通对话里频繁调用移动、好感度修改、物品修改等高影响工具。
- 工具失败时，用角色内自然语言轻描淡写，不要暴露 JSON、MCP、HTTP、tool call。
Phase 4：实现事件触发链路

这里建议做成三档，按 Hermes 能力选择。

方案 A：Hermes 原生接 MCP notification，最优
SMAPI Mod emits event
  -> smartnpc-mcp converts to MCP notification
  -> Hermes receives notification as task input
  -> Hermes decides tools
  -> Hermes calls chat_say

这时 smartnpc-mcp 需要定义清晰的 event payload：

{
  "type": "chat_message",
  "npc": "XiaMi",
  "player": "Farmer",
  "text": "你今天过得怎么样？",
  "gameTime": "Spring 5, 9:40 AM",
  "location": "Farm"
}
方案 B：smartnpc-mcp 调 Hermes Gateway，只做转发
SMAPI Mod emits event
  -> smartnpc-mcp receives ws event
  -> smartnpc-mcp POST /hermes/profiles/xiami/messages
  -> Hermes handles it
  -> Hermes calls MCP tools

这个方案仍然符合“移除中间决策层”，因为 smartnpc-mcp 不生成回复、不选择工具，只转发事件。

方案 C：Hermes 主动轮询事件，保底方案

smartnpc-mcp 暴露：

game_next_event
game_ack_event

Hermes 定期执行：

Check pending game events. If a player message or NPC interaction is pending,
respond in character and call chat_say.

这个方案简单，但实时性和体验较弱。

Phase 5：把本来计划给 smartnpc-agent 的 M5 能力迁移到 Hermes

路线图中 M5 原计划包括 SQLite + FTS5 记忆、好感度注入、跨 NPC 消息队列、Cron 调度、Agent 池、事件路由、多 NPC persona 模板。

Hermes-first 后建议这样拆：

原 M5 项	新归属
SQLite + FTS5 记忆	Hermes memory
好感度注入 system prompt	Hermes skill + friendship_get tool
跨 NPC 消息队列	先做 MCP tool：npc_send_message / npc_broadcast_event，不要放 Go Agent
Cron 调度 + 主动行为	Hermes scheduler / reflexion loop
Agent 池	多 Hermes profile
事件路由	smartnpc-mcp event metadata + Hermes profile routing
persona 模板	Hermes SOUL.md / skills

尤其是 npc_send_message。现在它是 smartnpc-agent 里的 local-only tool，不经过 MCP。 重构后它应该变成 smartnpc-mcp 暴露的正式 MCP tool，否则 Hermes 看不到或无法统一管理。

Phase 6：更新文档和启动方式

README 当前 full stack 是先启动 Hermes LLM backend，再启动 smartnpc-agent，由 smartnpc-agent spawn smartnpc-mcp。 重构后建议改为：

# 1. 启动游戏 / SMAPI
"D:\Stardew Valley\StardewModdingAPI.exe"

# 2. 启动 MCP Server
smartnpc-mcp --http :3000 --ws-url ws://127.0.0.1:18745/ws

# 3. 启动 Hermes NPC profile
hermes -p xiami run

文档结构建议：

docs/
├── architecture.md          # Hermes-first 架构
├── hermes-profiles.md       # 每个 NPC profile 怎么配置
├── mcp-tools.md             # tool catalog
├── events.md                # game event -> Hermes trigger
├── migration-smartnpc-agent.md
└── protocol.md
6. 应该保留在 smartnpc-mcp 的“硬逻辑”

不要把所有规则都扔给 Hermes prompt。以下规则必须在 MCP server 或 SMAPI mod 里硬校验：

1. 工具参数合法性
2. NPC 是否存在
3. 地点是否可达
4. 当前游戏状态是否允许操作
5. 写操作权限，比如改好感度、给物品、传送
6. tool timeout / retry / reconnect
7. 高频工具限流
8. 同一 NPC 同时多条消息的并发顺序
9. chat_say 文本长度、特殊字符、UI 限制
10. 游戏主线程调用安全

原因很简单：Hermes 的 prompt / skill 是软约束，MCP handler 才是硬边界。

7. 推荐最终落地顺序

我建议按这个顺序做，风险最低：

1. 冻结 smartnpc-agent 新功能，只保留测试用途
2. 确认 Hermes 能否接收 MCP notification 或外部 HTTP event
3. 把 smartnpc-mcp 的 HTTP transport 作为正式入口
4. 完善 tool description / inputSchema / error output
5. 把 npc_send_message 从 smartnpc-agent local tool 移到 smartnpc-mcp
6. 为 XiaMi 建第一个 Hermes profile
7. 跑通：玩家聊天 -> Hermes -> chat_say
8. 跑通：问时间/天气/好感度 -> Hermes 自动调用工具
9. 跑通：npc_interact -> Hermes 主动打招呼
10. 再迁移 memory / reflection / multi-NPC
11. 最后删除或 archive smartnpc-agent
8. 我对方案的最终评价

适合重构，而且应该尽早重构。

原因是当前项目已经有两个 Agent 中心：

smartnpc-agent: persona / memory / scheduler / tool loop / multi-NPC
Hermes Agent: persona / memory / skill / tool router / MCP adapter / reflection

继续做下去会产生重复抽象。最合理的边界应该是：

SMAPI Mod：游戏内 API 和事件
smartnpc-mcp：MCP 工具与事件协议
Hermes Agent：决策、人格、记忆、技能、反思、多 NPC 行为
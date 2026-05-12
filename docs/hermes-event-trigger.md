# Hermes 事件触发链路研究（M5 任务 5.0b）

> **日期**：2026-05-11
> **决策**：锁定 **方案 B（smartnpc-mcp POST 到 Hermes Gateway）**
> **依据**：Hermes 官方文档 `developer-guide/agent-loop`、`gateway-internals`、`cron-internals`、`tools-runtime`

---

## 背景

[REFACTOR.md](../REFACTOR.md) 提出 3 种事件触发方案：

| 方案 | 说明 |
|---|---|
| A | MCP notification 直接被 Hermes 当作 agent trigger 消费 |
| B | smartnpc-mcp 主动 POST 到 Hermes Gateway，由 Hermes 处理 |
| C | Hermes 周期性轮询 `game_next_event` MCP 工具拉取事件 |

需要先确认 Hermes 的能力，再选定方案。

---

## 关键发现

### 1. Agent loop 是 user-message-driven

源：[`developer-guide/agent-loop`](https://hermesagent.org.cn/docs/developer-guide/agent-loop)

```
run_conversation(user_message=..., system_message=..., conversation_history=...)
```

所有 turn 入口都需要一条 `user_message`。**没有"被 MCP server 推送 notification 自动启动 turn"的代码路径**。

### 2. MCP 工具流是单向（LLM → MCP server）

源：[`developer-guide/tools-runtime`](https://hermesagent.org.cn/docs/developer-guide/tools-runtime)

```
Model response with tool_call
  ↓
agent loop
  ↓
registry.dispatch(name, args)
  ↓
ToolEntry.handler  ← MCP tool handler
  ↓
return result string
```

MCP server 的 notifications/messages 不会被 Hermes 当作"反向消息"注入 agent loop。`tools/mcp_tool.py` 仅做 outbound dispatch。

### 3. Cron 也不能复用 conversation 记忆

源：[`developer-guide/cron-internals`](https://hermesagent.org.cn/docs/developer-guide/cron-internals)

> 每个 cron 任务都在一个完全独立的 Agent 会话中运行
> - 无前次运行的对话历史
> - 无对之前 cron 执行的记忆（除非显式持久化到内存或文件）
> - 提示必须自包含

cron 也不是事件驱动入口，是时间驱动的"重新启 session"机制。

### 4. 但 Gateway 有 webhook + api_server 平台适配器

源：[`developer-guide/gateway-internals`](https://hermesagent.org.cn/docs/developer-guide/gateway-internals)

```
gateway/platforms/
├── webhook.py      # 入站/outbound webhook 适配器
├── api_server.py   # REST API 服务器适配器
├── telegram.py
├── discord.py
└── ...
```

外部系统可以通过：
- **REST API**：`POST /v1/responses` 带 `conversation:"xiami"` + `instructions:<persona>`（已验证可用）
- **Webhook 适配器**：进一步研究后可能更适合事件语义

会话密钥格式 `agent:main:{platform}:{chat_type}:{chat_id}` 天然支持 per-NPC 路由。

### 5. Hooks 是 outbound 通知，不是 inbound trigger

Gateway hooks（`agent:start`、`agent:end`、`session:start` 等）是 Hermes **推给外部**的生命周期事件，无法用作 Hermes **接收** 外部事件的入口。

---

## 决策

### 方案 A — 排除

**原因**：Hermes 没有"MCP notification → trigger agent turn"的代码路径，必须改 Hermes 核心才能实现。这违反了重构原则"smartnpc-mcp 强化、不动 Hermes 核心"。

### 方案 C — 备选

技术上可行，但实时性弱（轮询周期决定）。仅在方案 B 出现意外阻塞时回退到 C。

### **方案 B — 锁定** ✅

**链路**：

```
SMAPI Mod
  ├──ws──> smartnpc-mcp（MCP server，连游戏）
  └──ws──> smartnpc-mcp（同一进程的事件转发器）
                │
                │ HTTP POST
                v
       Hermes Gateway /v1/responses
       body:
         {
           "input": "玩家说：你今天好吗？",
           "instructions": "<XiaMi SOUL.md>",
           "conversation": "xiami",
           "store": true
         }
                │
                ↓
       Hermes AIAgent run_conversation
                │
                ├── tool_call: game_get_time / friendship_get / ...
                │     └──> smartnpc-mcp（同一 server 的 MCP tools）
                │             └──> SMAPI Mod ws
                │
                └── tool_call: chat_say
                      └──> smartnpc-mcp
                            └──> SMAPI Mod ws → 游戏内显示
```

**对合成事件的扩展（2026-05-12）**：`smartnpc-mcp` 内部工具
(`npc_send_message` / `npc_broadcast_event`) 产生的 synthetic event 走
**同一条 outbound 路径**——经由共享的 `bridge.EventHandler` 注入
`hermesrelay`，最终 POST 到 Hermes Gateway。这不改变 Plan B 的论断
(Hermes 仍不消费 inbound MCP notification)；只是 outbound 入口从"仅
ws 来源"扩展到"ws 来源 + tool-handler 来源"。Plan A 的排除依旧成立。
详见 [ADR-0001](./adr/0001-synthetic-events-go-through-hermesrelay.md)。

**关键属性**：

| 项 | 值 |
|---|---|
| 事件入口 | smartnpc-mcp 内嵌一个 outbound HTTP client，订阅 SMAPI ws 事件 → POST 到 Hermes |
| 路由维度 | `conversation` 字段（每个 NPC 一个 named conversation）|
| Persona 注入 | `instructions` 字段每次请求传入 SOUL.md 内容 |
| 工具调用 | 由 LLM 自主从 `mcp_servers` 注册中选择，**反向连回 smartnpc-mcp 的 MCP server 端口** |
| 实时性 | HTTP 同步调用，毫秒级 |

**优势**：

1. 用 Hermes 已有的标准能力，零核心改动
2. 已经在前面对话里 curl 验证过 `/v1/responses` + `conversation:` 可用
3. smartnpc-mcp 既是 MCP server（响应 LLM 工具调用）又是事件转发器（向 Hermes 推送游戏事件）—— 单一进程职责清晰
4. 升级到 webhook 平台适配器时只需替换 outbound 调用方式，不影响其他模块

**劣势**：

1. smartnpc-mcp 多了一项"事件转发"职责（但本来就是它的边界，没有越界）
2. 每次事件需要传 `instructions: <persona>`，浪费 token —— 可后续优化为通过 Hermes profile 配置 system prompt（profile 级隔离）

---

## 后续 Phase 影响

### Phase 1（MCP 强化）的具体内容据此细化

- **5.1 streamable HTTP transport**：仍需要做（让 Hermes 通过 `mcp_servers.url:` 反向连 smartnpc-mcp，避免 stdio 跨 WSL 边界）
- **5.4 事件 payload 规范化**：直接面向 `/v1/responses` 的 JSON 格式设计，例如：
  ```json
  {
    "input": "[player_chat] Farmer says: 你好",
    "instructions": "<persona>",
    "conversation": "xiami"
  }
  ```
- 新增 **5.4b**：smartnpc-mcp 内嵌 outbound HTTP client（Go `net/http` 即可），订阅 ws 事件 → 转 POST 到 Hermes

### Phase 4（事件触发实装）的内容

- 5.8 实装 outbound HTTP client + 配置项（Hermes URL、API_SERVER_KEY、conversation routing 表）
- 5.9 NPC 主动打招呼：`npc_interact` ws event → smartnpc-mcp 转 POST 到对应 NPC 的 conversation

---

## 待进一步研究（不阻塞 Phase 1）

1. **webhook 平台适配器细节**：是否比 `/v1/responses` 更适合事件语义？需要拉 `gateway/platforms/webhook.py` 源码或专门文档。
2. **per-conversation 持久化 system prompt**：`sessions` 表有 `system_prompt` 列；能否在创建 conversation 时一次性写入 persona，避免每次请求都传 `instructions`？需要查 `/v1/responses` 是否支持 `system_prompt` 持久化，或借助 `session-storage` API 直接写。
3. **Profile 级 persona vs conversation 级 persona**：14 个 NPC 是用 14 个 named conversation（同一 profile，省进程）还是 14 个 profile（重，但工具白名单可隔离）？取决于是否需要硬隔离工具集。

这些可在 Phase 2 起 `hermes/profiles/xiami/` 时再决定。

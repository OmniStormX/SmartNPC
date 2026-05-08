# SmartNPC 系统架构图

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                              Stardew Valley (SMAPI Mod)                                   │
│                                                                                           │
│  ┌──────────────┐  ┌───────────────┐  ┌──────────────┐  ┌──────────────┐                │
│  │  UI Layer    │  │  Handlers     │  │  Patches     │  │  Systems     │                │
│  ├──────────────┤  ├───────────────┤  ├──────────────┤  ├──────────────┤                │
│  │ NpcChatBar   │  │ ChatHandler   │  │ NpcDialogue  │  │ Perception   │                │
│  │ ChatPanel    │  │ MailHandler   │  │   Patch      │  │ Movement     │                │
│  │ ChatSideBtn  │  │ GameQuery     │  │              │  │ FollowSystem │                │
│  │ DebugPanel   │  │ Behavior      │  │              │  │ AgentNpcReg  │                │
│  └──────┬───────┘  └──────┬────────┘  └──────┬───────┘  └──────┬───────┘                │
│         │                  │                   │                  │                        │
│         └──────────────────┴───────────────────┴──────────────────┘                        │
│                                        │                                                   │
│                            ┌───────────┴───────────┐                                       │
│                            │  WebSocketServer      │                                       │
│                            │  ws://127.0.0.1:18745 │                                       │
│                            │  (single client)      │                                       │
│                            └───────────┬───────────┘                                       │
└────────────────────────────────────────┼───────────────────────────────────────────────────┘
                                         │ WebSocket (JSON frames)
                                         │ Events ↑ / Requests ↓
                                         │
┌────────────────────────────────────────┼───────────────────────────────────────────────────┐
│                            smartnpc-mcp (Go)                                               │
│                                        │                                                   │
│  ┌─────────────────────────────────────┴──────────────────────────────────────┐            │
│  │  WSClient (bridge)                                                         │            │
│  │  • Auto-reconnect with backoff                                             │            │
│  │  • Ping keepalive (30s)                                                    │            │
│  │  • Request/Response correlation by ID                                      │            │
│  │  • Event forwarding to MCP notifications                                   │            │
│  └─────────────────────────────────────┬──────────────────────────────────────┘            │
│                                        │                                                   │
│  ┌─────────────────────────────────────┴──────────────────────────────────────┐            │
│  │  MCP Server (stdio)                                                        │            │
│  │                                                                             │            │
│  │  Tools (15):                                                                │            │
│  │  ┌─────────────┐ ┌──────────────┐ ┌─────────────┐ ┌────────────────┐      │            │
│  │  │ ping        │ │ game_get_time│ │ npc_move_to │ │ npc_summon     │      │            │
│  │  │ chat_say    │ │ game_get_    │ │ npc_face_   │ │ npc_follow_    │      │            │
│  │  │ mail_send   │ │   weather    │ │   direction │ │   start/stop   │      │            │
│  │  │             │ │ friendship_  │ │ npc_get_    │ │ npc_lead_to    │      │            │
│  │  │             │ │   get        │ │   position  │ │ npc_get_       │      │            │
│  │  │             │ │              │ │ npc_get_    │ │   behavior     │      │            │
│  │  │             │ │              │ │   nearby    │ │                │      │            │
│  │  │             │ │              │ │ npc_get_    │ │                │      │            │
│  │  │             │ │              │ │  environment│ │                │      │            │
│  │  └─────────────┘ └──────────────┘ └─────────────┘ └────────────────┘      │            │
│  └────────────────────────────────────────────────────────────────────────────┘            │
└────────────────────────────────────────┬───────────────────────────────────────────────────┘
                                         │ stdio MCP (JSON-RPC 2.0)
                                         │ spawn as child process
                                         │
┌────────────────────────────────────────┼───────────────────────────────────────────────────┐
│                          smartnpc-agent (Go)                                               │
│                                        │                                                   │
│  ┌─────────────────────────────────────┴──────────────────────────────────────┐            │
│  │  MCP Client Session (shared by all agents)                                 │            │
│  └─────────────────────────────────────┬──────────────────────────────────────┘            │
│                                        │                                                   │
│  ┌─────────────────────────────────────┴──────────────────────────────────────┐            │
│  │                           Router                                            │            │
│  │  • Dispatch events by NPC name                                             │            │
│  │  • npc_send_message → DeliverNPCMessage (cross-agent)                      │            │
│  │  • StartProactive (ticker per agent, ~4min ± jitter)                        │            │
│  │                                                                             │            │
│  │  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐ │            │
│  │  │ XiaMi   │ │Abigail  │ │Sebastian│ │ Haley   │ │ Harvey  │ │ Penny   │ │            │
│  │  │ Agent   │ │ Agent   │ │ Agent   │ │ Agent   │ │ Agent   │ │ Agent   │ │            │
│  │  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘ │            │
│  └───────┼────────────┼──────────┼────────────┼──────────┼────────────┼──────┘            │
│          │            │          │            │          │            │                     │
│          └────────────┴──────────┴────────────┴──────────┴────────────┘                     │
│                                        │                                                   │
│                         Each Agent contains:                                               │
│                         • history []Message (bounded 40 msgs)                              │
│                         • Persona config (from JSON)                                       │
│                         • Tools (15 MCP + 1 local npc_send_message)                        │
│                         • respondAndSay() → respond() loop (5 rounds max)                  │
│                                        │                                                   │
│  ┌─────────────────────────────────────┴──────────────────────────────────────┐            │
│  │                    Dual-LLM Pipeline                                        │            │
│  │                                                                             │            │
│  │   ┌─────────────────────────┐        ┌─────────────────────────┐           │            │
│  │   │  Stage 1: Decision      │        │  Stage 2: Persona       │           │            │
│  │   │  (GPT-5.5)              │        │  (Hermes / local LLM)   │           │            │
│  │   │                         │        │                         │           │            │
│  │   │  • Sees all tools       │───────▶│  • NO tools             │           │            │
│  │   │  • Executes tool calls  │ action │  • Full persona prompt  │           │            │
│  │   │  • Structured reasoning │ results│  • Friendship context   │           │            │
│  │   │  • Output: tool results │        │  • Output: in-character │           │            │
│  │   │                         │        │    natural reply         │           │            │
│  │   └────────────┬────────────┘        └────────────┬────────────┘           │            │
│  │                │                                   │                        │            │
│  └────────────────┼───────────────────────────────────┼────────────────────────┘            │
└───────────────────┼───────────────────────────────────┼────────────────────────────────────┘
                    │                                   │
                    ▼                                   ▼
┌───────────────────────────────────┐  ┌───────────────────────────────────────┐
│  Decision LLM Backend             │  │  Persona LLM Backend (Hermes)          │
│  (OpenAI-compatible)              │  │                                         │
│                                   │  │  ┌───────────────────────────────────┐ │
│  POST /v1/chat/completions        │  │  │  POST /v1/responses               │ │
│  • Stateless                      │  │  │  • Stateful (conversation chain)  │ │
│  • Full tool calling              │  │  │  • SOUL.md auto-loaded            │ │
│  • Agent controls loop            │  │  │  • Memory system (cross-session)  │ │
│                                   │  │  │  • Context overflow → auto-rotate │ │
│  http://v2.open.venus.oa.com/     │  │  └───────────────────────────────────┘ │
│    llmproxy                       │  │                                         │
│                                   │  │  http://192.168.59.118:8643             │
│  Model: gpt-5.5                   │  │  Profile: xiami (shared by all NPCs)   │
└───────────────────────────────────┘  │  Model: gpt-5.5 (via custom provider)  │
                                       │                                         │
                                       │  ┌───────────────────────────────────┐ │
                                       │  │  Hermes Memory (per API key)      │ │
                                       │  │  • USER.md: player preferences    │ │
                                       │  │  • MEMORY.md: agent observations  │ │
                                       │  │  • Auto-summarized, cross-session │ │
                                       │  └───────────────────────────────────┘ │
                                       │                                         │
                                       │  Conversations (per NPC, daily):        │
                                       │  • smartnpc-xiami-20260508              │
                                       │  • smartnpc-abigail-20260508            │
                                       │  • smartnpc-sebastian-20260508          │
                                       │  • ...                                  │
                                       └─────────────────────────────────────────┘


═══════════════════════════════════════════════════════════════════════════════════
                            EVENT FLOW
═══════════════════════════════════════════════════════════════════════════════════

  Player Input                    System Events                 Proactive Ticker
       │                               │                              │
       ▼                               ▼                              ▼
┌──────────────┐              ┌──────────────┐              ┌──────────────────┐
│ chat_message │              │ npc_interact │              │ [系统提示：自主   │
│ {npc, text}  │              │ {npc}        │              │  行为时间]        │
└──────┬───────┘              └──────┬───────┘              │ 每4min ± 60s     │
       │                             │                      └────────┬─────────┘
       └─────────────┬───────────────┘                               │
                     ▼                                               │
              ┌──────────────┐                                       │
              │   Router     │◀──────────────────────────────────────┘
              │  dispatch()  │
              └──────┬───────┘
                     │ route by NPC name
                     ▼
              ┌──────────────┐
              │    Agent     │
              │respondAndSay │
              └──────┬───────┘
                     │
          ┌──────────┼──────────┐
          ▼                     ▼
   ┌─────────────┐      ┌─────────────┐
   │ Decision    │      │  Persona    │
   │ (tools)     │─────▶│  (reply)    │
   └─────────────┘      └──────┬──────┘
                                │
                                ▼
                         ┌─────────────┐
                         │  chat_say   │──▶ Game displays dialogue
                         └─────────────┘


═══════════════════════════════════════════════════════════════════════════════════
                         NPC-to-NPC MESSAGING
═══════════════════════════════════════════════════════════════════════════════════

  Agent A (Abigail)                              Agent B (Sebastian)
       │                                              │
       │  LLM decides to message Sebastian            │
       │  tool_call: npc_send_message                 │
       │    {to:"Sebastian", message:"今晚去矿洞?"}   │
       │                                              │
       ▼                                              │
  executeLocalTool()                                  │
       │                                              │
       ▼                                              │
  Router.DeliverNPCMessage()                          │
       │                                              │
       └─────────────────────────────────────────────▶│
                                                      ▼
                                              ReceiveNPCMessage()
                                                      │
                                                      ▼
                                              go respondAndSay(
                                                "[Abigail传话说：今晚去矿洞?]")
                                                      │
                                                      ▼
                                              LLM generates reply
                                                      │
                                                      ▼
                                              chat_say → Game displays
```

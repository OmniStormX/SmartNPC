# SmartNPC 架构图

```mermaid
graph TB
    %% ═══ Styles ═══
    classDef agent fill:#d1fae5,stroke:#059669,stroke-width:2px,color:#065f46
    classDef agentHL fill:#a7f3d0,stroke:#059669,stroke-width:3px,color:#065f46
    classDef mcp fill:#fef3c7,stroke:#d97706,stroke-width:2px,color:#92400e
    classDef mcpHL fill:#fde68a,stroke:#d97706,stroke-width:3px,color:#92400e
    classDef mod fill:#e0f2fe,stroke:#0284c7,stroke-width:2px,color:#075985
    classDef modHL fill:#bae6fd,stroke:#0284c7,stroke-width:3px,color:#075985
    classDef ext fill:#e0e7ff,stroke:#6366f1,stroke-width:2px,color:#4338ca
    classDef edgeLabel fill:none,stroke:none,color:#64748b,font-size:11px

    %% ═══ Agent 层 ═══
    subgraph AGENT["🧠 Agent 层 — Hermes Profile (每 NPC 独立)"]
        direction LR

        subgraph AGENT_SKILL["SKILLs 行为策略 (Markdown, AI 读入)"]
            skill_farm["🏡 农场: farm-care / cleanup<br/>harvest / extension / gather"]:::agent
            skill_social["💬 社交: social-interact<br/>social-round-robin"]:::agent
            skill_base["📋 基础: core / schedule<br/>memory / gift / greeting / visit"]:::agent
        end

        subgraph AGENT_CORE["NPC 人格与记忆"]
            soul["SOUL.md<br/>人格 · 口吻 · 关系"]:::agentHL
            policy["critical-policy.md<br/>硬规则，永不压缩"]:::agentHL
            memory["state.db<br/>长期记忆 (SQLite)"]:::agent
        end

        subgraph AGENT_PROFILE["6× NPC Gateway"]
            xm["xiami :8642 ⭐"]:::agentHL
            ab["abigail :8643"]:::agent
            hl["haley :8644"]:::agent
            hv["harvey :8645"]:::agent
            pn["penny :8646"]:::agent
            sb["sebastian :8647"]:::agent
        end
    end

    %% ═══ MCP 层 ═══
    subgraph MCP["⚙️ MCP 层 — smartnpc-mcp (Go)"]
        direction LR

        subgraph MCP_SCHED["Schedule 子系统"]
            sched["Scheduler<br/>per-NPC DaySchedule<br/>Tick → FiredEntry[]"]:::mcpHL
            worker["npcWorkflowWorker<br/>per-NPC goroutine<br/>串行消费 · 主动取消"]:::mcpHL
        end

        subgraph MCP_ENGINE["Workflow 引擎"]
            engine["Workflow Engine<br/>8 step types<br/>PrecompileSkill"]:::mcpHL
            runner["MCPRunner<br/>CallTool · CallSkill<br/>LLMChoice · WaitIdle"]:::mcp
        end

        subgraph MCP_RELAY["事件与通信"]
            relay["hermesrelay<br/>POST /v1/responses<br/>instructions 注入"]:::mcpHL
            mcp_http["MCP HTTP :3000/mcp<br/>tools/list · tools/call"]:::mcp
            ws_client["WS Client<br/>request(id) ↔ response(id)"]:::mcp
        end
    end

    %% ═══ Mod 层 ═══
    subgraph MOD["🎮 Mod 层 — C# SMAPI (游戏线程)"]
        direction LR

        subgraph MOD_EXEC["行为执行"]
            follow["FollowSystem<br/>19 NpcBehaviorMode<br/>PumpOnGameTick 逐帧驱动"]:::modHL
            queue["NpcActionQueue<br/>per-NPC FIFO<br/>三模型排队"]:::mod
        end

        subgraph MOD_ACTIONS["行为 Handler"]
            world["🌾 世界行为<br/>Wander · ClearDebris<br/>Water · Harvest · Till<br/>Plant · Fertilize · Fill<br/>Deposit · Deliver · Break"]:::mod
            social["💬 社交行为<br/>ApproachAndSpeak<br/>ExpressEmotion · Dance<br/>ReactSurprise · Pace"]:::mod
            instant["⚡ 即时行为<br/>ChatSay · Emote<br/>InspectObject · Place"]:::mod
        end

        subgraph MOD_IO["游戏 I/O"]
            router["MessageRouter<br/>action → handler 分发"]:::mod
            ws_server["WS Server :18745/ws<br/>JSON request/response/event"]:::mod
            ui["Chat UI<br/>Tab/F2/Esc<br/>面板 · 气泡 · 联系人"]:::modHL
        end
    end

    %% ═══ External ═══
    llm["☁ LLM API<br/>api.deepseek.com"]:::ext

    %% ═══ Layer-to-Layer ═══
    AGENT -->|"MCP tool call 回连 :3000/mcp"| MCP
    MCP -->|"hermesrelay POST /v1/responses"| AGENT
    AGENT -->|"HTTPS"| llm

    MCP -->|"ws request"| MOD
    MOD -->|"ws response / event"| MCP

    %% ═══ Internal flows ═══
    sched --> worker
    worker --> engine
    engine --> runner
    runner --> ws_client
    ws_client --> ws_server
    ws_server --> router
    router --> world
    router --> social
    router --> instant
    world --> queue
    social --> queue
    queue --> follow
    follow --> ui
    relay -->|"schedule trigger"| sched
    router -.->|"game events"| relay
```

**三层层间关系：**

```
Agent 层 (Hermes)  ←──→  MCP 层 (Go)  ←──→  Mod 层 (C#)
     决策                   编排                  执行
  "怎么想"              "何时做"             "怎么做"
```

**两条数据流：**

| 流向 | 路径 |
|------|------|
| ⏰ Schedule | `day_started → npc_plan_day → Scheduler → Tick → Worker → skill_call → Agent → MCP tool → ws → FollowSystem` |
| 📡 Event | `chat_message → ws event → hermesrelay → POST /v1/responses → Agent 决策 → MCP tool call → ws → 游戏执行` |

# SmartNPC Debug & Chat Architecture Analysis

**Date:** June 1, 2026
**Scope:** MCP server (Go), SMAPI mod (C#), WebSocket protocol, Hermes relay

---

## 1. DEBUG CONFIGURATION

**File:** `smartnpc-mcp/adapters/stardew/bridge/protocol.go`

EventDebugProactiveTrigger = "debug_proactive_trigger"

**File:** `smapi-mod/Debug/DebugCommands.cs`

Commands: smartnpc_friendship, smartnpc_debug, smartnpc_teleport, 
smartnpc_proactive (force trigger), smartnpc_status, smartnpc_tick, smartnpc_goto

---

## 2. CHAT MESSAGE SENDING: MCP → MOD

**Tool:** `chat_say` (smartnpc-mcp/adapters/stardew/tools/chat.go, lines 159-255)

Input:
- speaker: NPC internal name (PascalCase)
- text: message body
- color: optional (white|yellow|green|red|cyan|blue|purple|gray)
- channel: "group" or "" (private, default)
- group_id: required when channel="group"

Output:
- ok: bool (false if quota exhausted)
- hint: instruction to LLM (contains "TURN_END")

Quota Enforcement (ChatSayGuard):
- Private: ONE chat_say per NPC per wake-up (一问一答)
- Group: ONE chat_say per (group_id, speaker) per player turn
- Budget reset: on new inbound event (private) or player input (group)

---

## 3. WEBSOCKET EVENT PUSH: MCP → MOD

**Protocol:** smartnpc-mcp/adapters/stardew/bridge/protocol.go

Event struct:
- type: "event"
- name: event constant (chat_message, npc_interact, day_started, etc.)
- data: json.RawMessage
- timestamp: unix millis

Event Names (constants):
- chat_message, chat_received, npc_interact, group_create
- day_started, game_time_tick
- npc_message, npc_broadcast, schedule_trigger (synthetic)
- debug_proactive_trigger

---

## 4. NPC_PLAN_DAY TOOL

**File:** smartnpc-mcp/adapters/stardew/tools/npc_schedule.go (lines 78-174)

Input:
- npc: NPC internal name or "*" for all
- day: 1-28
- season: spring/summer/fall/winter
- year: default 1
- entries: list of (game_hour, action, reason) — max 20

Entry validation: game_hour 6-25, action must be non-empty

Output:
- ok: bool
- npc: echo
- accepted: number of entries stored
- message: status

Logging:
- npc_actions.log (JSON): tool_call records
- schedule.log (human-readable): per-NPC daily schedule

---

## 5. DEBUG PANEL: F3 INTERFACE

**File:** smapi-mod/UI/DebugPanel.cs

Opened with F3 key (line 162)

Display:
SmartNPC 调试面板 (F3)
Lists each Agent NPC with 4 buttons: 召唤 (summon), 跟随 (follow), 
带路 (lead), 停止 (stop)

Lead destinations: 农场左边, 房子前面, 湖边, 大门, 温室, 畜棚, 鸡舍

Mode label: [idle] or [active] (green when active)

---

## 6. HERMES RELAY LOGGING

**File:** smartnpc-mcp/pkg/relay/hermes/relay.go

Key log records:
1. "hermesrelay event received" (T0: event arrives)
2. "hermesrelay forwarded event" (T0 + elapsed_ms)
   - status, conversation, elapsed_ms
   - input_tokens, cached_tokens, cache_ratio
   - output_tokens

---

## 7. REQUEST/RESPONSE PROTOCOL

**File:** smartnpc-mcp/adapters/stardew/bridge/protocol.go

Request (client → server):
- type: "request"
- id: uuid
- action: tool name
- params: action-specific
- from_npc: NPC profile context

Response (server → client):
- type: "response"
- id: mirrors Request.ID
- ok: bool
- data: json.RawMessage
- error: optional ResponseError

Event (server → client push):
- type: "event"
- name: event constant
- data: json.RawMessage
- timestamp: unix millis

---

## 8. C# MOD CHAT HANDLER

**File:** smapi-mod/Chat/ChatHandler.cs

Handle() receives chat_say action, validates, queues to PumpOnGameTick()

PumpOnGameTick() processes queue:
- Checks if channel is group or private
- Fallback: if no channel set but group is active and speaker participates, promote to group
- Stores in ChatMessageStore (private only, not group)
- Increments unread if panel not active
- Invokes _onMessage callback to render

ChatSayParams:
- speaker, text, color, channel, group_id

---

## 9. KEY FILE PATHS

smartnpc-mcp:
- adapters/stardew/bridge/protocol.go (Event/Action constants, DTOs)
- adapters/stardew/bridge/ws_client.go (WebSocket dispatch)
- adapters/stardew/tools/chat.go (chat_say tool + ChatSayGuard)
- adapters/stardew/tools/npc_schedule.go (npc_plan_day, npc_get_schedule)
- adapters/stardew/tools/action_logger.go (npc_actions.log, schedule.log)
- adapters/stardew/events/events.go (Typed event payloads)
- adapters/stardew/events/format.go (FormatForHermes, RecipientNPC)
- adapters/stardew/scheduler/scheduler.go (Game-time tick dispatcher)
- pkg/relay/hermes/relay.go (Event → Hermes POST, logging)

smapi-mod:
- Bridge/Protocol.cs (Wire protocol DTOs)
- Bridge/WebSocketServer.cs (SMAPI mod's ws server)
- Chat/ChatHandler.cs (Receive chat_say, queue messages)
- UI/DebugPanel.cs (F3 panel with NPC buttons)
- Debug/DebugCommands.cs (smartnpc_* console commands)
- ModEntry.cs (SMAPI entry point, wiring)

---

**End of Analysis** — June 1, 2026

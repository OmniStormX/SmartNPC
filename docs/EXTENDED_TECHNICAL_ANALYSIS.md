# SmartNPC Extended Technical Analysis

## Complete Flow Diagrams & Code Examples

### 1. Chat Message Flow (Player → NPC)

Player Types in Game Chat Panel
  ↓
ChatInputCapture.OnInputSubmitted
  ↓ (ws Request: action=chat_message, params with npc, text, source)
  ↓
ws_client.go: conn.Write with websocket.MessageText body
  ↓ [WebSocket JSON-RPC envelope]

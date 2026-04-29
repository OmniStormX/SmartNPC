# smapi-mod (placeholder)

C# SMAPI mod that exposes Stardew Valley game APIs to `smartnpc-mcp` over
a WebSocket on `ws://localhost:8765/game`.

Will be filled in during **M2** with:

- `ModEntry.cs` — SMAPI entry, event subscriptions
- `Bridge/WebSocketServer.cs` — embedded `System.Net.WebSockets` server
- `Bridge/Protocol.cs` — Request / Response / Event DTOs
- `Npc/*Handler.cs` — query, dialogue, movement, friendship, schedule, emote
- `Patches/*.cs` — Harmony patches (DialogueBox, NPC.checkAction, NPC.receiveGift)

Project will target `.NET 6`, SMAPI `4.0+`, Stardew Valley `1.6+`.

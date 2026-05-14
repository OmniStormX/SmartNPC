# SmartNPC Event Catalog

> Canonical payload shapes for every server → client push frame.  
> Go ground truth: [`smartnpc-mcp/internal/events/events.go`](../smartnpc-mcp/internal/events/events.go).  
> Wire envelope: see [`protocol.md`](./protocol.md).

Two sources emit events into the MCP notification stream consumers see:

1. **Mod events** — originated by the SMAPI mod, arriving over the `:18745`
   WebSocket and forwarded into MCP as logging notifications by
   [`tools.MakeEventForwarder`](../smartnpc-mcp/internal/tools/events.go).
2. **Synthetic events** — emitted by `smartnpc-mcp` itself (e.g. inter-NPC
   messaging via `npc_send_message`). Uses the same envelope shape so
   consumers see one uniform stream, and (since 2026-05-12) flows
   through the same `bridge.EventHandler` chain mod events traverse —
   so audible-routing, hermesrelay POST, and group-dispatch fire
   identically regardless of source. See
   [ADR-0001](./adr/0001-synthetic-events-go-through-hermesrelay.md).

All MCP notifications wrap the event in this envelope:

```json
{
  "kind": "stardew/event",
  "name": "chat_message",
  "data": { "...": "..." },
  "timestamp": 1714000000000
}
```

Consumers (Hermes profile, Claude Desktop, custom MCP clients)
subscribe via `LoggingMessageHandler` and filter on `kind == "stardew/event"`.

**Forward-compat rule:** unrecognized fields MUST be ignored silently. New
fields will be added over time; consumers must not error on them.

---

## Status summary

| Event | Source | Status |
|---|---|---|
| `chat_message` | mod | ✅ implemented |
| `chat_received` | mod | ✅ implemented (legacy) |
| `npc_interact` | mod | ✅ implemented |
| `group_create` | mod | ✅ implemented (legacy group chat) |
| `day_started` | mod | 🔒 reserved — schema frozen, mod implementation pending |
| `location_changed` | mod | 🔒 reserved |
| `friendship_changed` | mod | 🔒 reserved |
| `npc_perception_update` | mod | 🔒 reserved |
| `npc_message` | synthetic | ✅ implemented (see `npc_send_message` tool) |
| `npc_broadcast` | synthetic | ✅ implemented (see `npc_broadcast_event` tool) |

---

## Mod events

### `chat_message`

Player sent a line targeted at a specific NPC via the in-game chat panel.
This is the primary "player talks to NPC" trigger for Hermes profiles.

Emitted by [`smapi-mod/ModEntry.cs::OnChatSend`](../smapi-mod/ModEntry.cs).

| field | type | notes |
|---|---|---|
| `npc` | string | recipient NPC internal name, e.g. `"XiaMi"` |
| `target` | string | redundant alias for `npc` today; reserved for multi-target |
| `text` | string | raw UTF-8 text typed by the player |
| `source` | string | always `"player"` today |

### `chat_received`

Generic chat channel — one-to-many. Emitted when the player types into the
in-game chat box without explicitly addressing an NPC (Ctrl+T path), and by
the legacy group chat UI. Carries the list of Agent-managed NPCs within
audible range so `smartnpc-mcp` can synthesize a targeted `chat_message`
for the nearest one (see Synthetic events below).

Emitted by [`smapi-mod/ModEntry.cs`](../smapi-mod/ModEntry.cs) and
[`UI/GroupChatManager.cs`](../smapi-mod/UI/GroupChatManager.cs) (with an empty
`audible_npcs` list — group chat does not use audible routing).

| field          | type             | notes                                                                 |
|----------------|------------------|-----------------------------------------------------------------------|
| `text`         | string           | the raw text                                                          |
| `source`       | string           | `"player"` (legacy private chat box) or `"player_group"` (group chat session) |
| `group_id`     | string           | group chat session id when `source="player_group"`; empty otherwise   |
| `audible_npcs` | array (optional) | Agent-managed NPCs in earshot, sorted by distance ascending; omitted or empty when none are in range |

Each entry of `audible_npcs`:

| field      | type   | notes                                  |
|------------|--------|----------------------------------------|
| `name`     | string | NPC internal name                      |
| `map`      | string | NPC's current map                      |
| `distance` | number | Euclidean tile distance from the player |
| `x`        | int    | NPC tile X                             |
| `y`        | int    | NPC tile Y                             |

**Group-source rendering**: when `source="player_group"` and `group_id`
is non-empty, [`events.FormatForHermes`](../smartnpc-mcp/internal/events/format.go)
prefixes the inbound `instructions` string with a structured
group-context tag so the receiving profile knows to mirror
`channel="group"` + `group_id=<id>` back through `chat_say`:

```
[group_chat group_id="<id>"] Player says in the group: <text> (reply via chat_say with channel="group" and group_id="<id>")
```

For `source="player"` / empty / missing `group_id`, the legacy private
rendering `Someone in the chat says: <text>` is retained. See
[ADR-0002](./adr/0002-group-chat-channel-end-to-end.md) for the
end-to-end group-chat channel contract.

### `npc_interact`

Player clicked an Agent-managed NPC sprite. Should wake the target NPC's
Hermes profile for a proactive greeting.

Emitted by [`smapi-mod/Patches/NpcDialoguePatch.cs::PumpInteractions`](../smapi-mod/Patches/NpcDialoguePatch.cs).

| field | type | notes |
|---|---|---|
| `npc` | string | clicked NPC internal name |
| `source` | string | always `"player"` today |

### `group_create`

Legacy group chat session was opened in the mod UI.

Emitted by [`smapi-mod/UI/GroupChatManager.cs`](../smapi-mod/UI/GroupChatManager.cs).

| field | type | notes |
|---|---|---|
| `participants` | array of string | NPC internal names in the group |

---

## Reserved mod events (not yet emitted)

These payloads are frozen here so downstream consumers (hermesrelay,
Hermes profile, tests) can code against them before the mod-side
implementation lands. The mod currently does **not** emit any of these.

### `day_started` 🔒

Fired once at the start of each in-game day.

| field | type | notes |
|---|---|---|
| `day` | int | 1–28 |
| `season` | string | `spring` / `summer` / `fall` / `winter` |
| `year` | int | in-game year |
| `day_of_week` | string | short name: `Mon` / `Tue` / … |

### `location_changed` 🔒

A watched NPC or the player moved between maps.

| field | type | notes |
|---|---|---|
| `who` | string | NPC name or `"player"` |
| `kind` | string | `"npc"` / `"player"` |
| `from_map` | string | SDV map name the character left |
| `to_map` | string | SDV map name the character entered |

### `friendship_changed` 🔒

Friendship points for an NPC changed by more than a small threshold.

| field | type | notes |
|---|---|---|
| `npc` | string | NPC internal name |
| `points` | int | new raw points value |
| `point_delta` | int | change since the last notification |
| `hearts` | int | new heart level |
| `trigger` | string | `"gift"` / `"quest"` / `"decay"` / `"other"` |

### `npc_perception_update` 🔒

Reserved. Intended for proactive perception diffs (an NPC notices another
character entered or left its visibility radius). No final schema yet.

---

## Synthetic events (from smartnpc-mcp itself)

### `npc_message`

Fanout of a `npc_send_message` tool call — one NPC talking privately to
another. Each message is also persisted in an in-memory mailbox accessible
via `npc_inbox_get` / `npc_inbox_ack`.

Like mod events, this is dispatched to MCP notification subscribers AND
POSTed to the Hermes Gateway via `hermesrelay` so the recipient NPC's
profile wakes for a turn. The recipient is matched against
`hermes/runtime-config.yaml` `match.npc` rules using the `to` field
(see [`events.RecipientNPC`](../smartnpc-mcp/internal/events/events.go)).

Emitted by [`smartnpc-mcp/internal/tools/npc_message.go`](../smartnpc-mcp/internal/tools/npc_message.go).

| field | type | notes |
|---|---|---|
| `id` | string | message uuid — echo in `npc_inbox_ack` to clear |
| `from` | string | sender NPC internal name |
| `to` | string | recipient NPC internal name |
| `text` | string | message body |
| `kind` | string | optional free-form tag, e.g. `"greeting"` / `"alert"` |
| `timestamp` | int | unix millis when queued |

### `npc_broadcast`

Fanout of a `npc_broadcast_event` tool call — world-wide signal with no
named recipient. Fire-and-forget (not queued in any inbox).

| field | type | notes |
|---|---|---|
| `from` | string | sender NPC internal name |
| `kind` | string | event category, e.g. `"alarm"` / `"party_invite"` |
| `data` | any | optional JSON payload forwarded verbatim |
| `timestamp` | int | unix millis when emitted |

---

## For hermesrelay consumers (M5 Phase 3)

The [Hermes event-trigger research](./hermes-event-trigger.md) locked in
Plan B: `smartnpc-mcp` POSTs mod events to the Hermes Gateway. For each
event, the relay renders a single-line `input` string using
[`events.FormatForHermes`](../smartnpc-mcp/internal/events/format.go), e.g.

| Event | Rendered `input` |
|---|---|
| `chat_message` | `Farmer says to you: 你好` |
| `npc_interact` | `The player walked up to you and opened a conversation.` |
| `day_started` | `A new day begins: Spring 5 (Mon), year 1.` |
| `npc_message` | `NPC Abigail says to you (privately): ...` |

The format function is deliberately forgiving — unknown event names fall
back to `Game event "<name>" occurred.` so new mod events don't break the
relay pipeline before a dedicated renderer is added.

# SmartNPC Bridge Protocol (v1)

Transport: **WebSocket** (ws://127.0.0.1:18745/ws), JSON text frames,
UTF-8. The SMAPI mod is the **server**, `smartnpc-mcp` is the **client**.

> M2 used a single HTTP POST endpoint. M3 migrates to WebSocket with three
> message kinds so that we get bidirectional pushes (e.g. `chat_received`) in
> addition to request/response.

## Envelope

Every frame is a JSON object with a `type` field. Three values:

| `type`     | Direction        | Purpose                                 |
|------------|------------------|-----------------------------------------|
| `request`  | client → server  | RPC call; expects a matching `response` |
| `response` | server → client  | Reply to a `request`, correlated by `id` |
| `event`    | server → client  | Server-initiated push (no reply needed) |

## `request`

```json
{
  "type": "request",
  "id": "01931c09-a40e-73f8-b3df-7b29b0e8c2e8",
  "action": "chat_say",
  "params": { "...": "..." }
}
```

- `id`: unique string (UUIDv4 recommended). Echoed back in the response.
- `action`: one of the actions listed below.
- `params`: action-specific object; may be omitted when empty.

## `response`

```json
{
  "type": "response",
  "id": "01931c09-a40e-73f8-b3df-7b29b0e8c2e8",
  "ok": true,
  "data": { "...": "..." }
}
```

Or on failure:

```json
{
  "type": "response",
  "id": "01931c09-a40e-73f8-b3df-7b29b0e8c2e8",
  "ok": false,
  "error": { "code": "mod_not_ready", "message": "no save loaded" }
}
```

- Exactly one of `data` or `error` is present.
- `error.code` is a stable machine-readable string; `error.message` is human text.

## `event`

```json
{
  "type": "event",
  "name": "chat_received",
  "data": { "...": "..." },
  "timestamp": 1714000000000
}
```

- `timestamp` is Unix milliseconds.
- Events are fire-and-forget; the server does not wait for an ack.

## Actions

### `chat_say`  (client → server)

Show a message in the in-game chat box (bottom-left). Requires a save to be
loaded; fails with `mod_not_ready` otherwise.

**params**

| field     | type   | required | notes                                      |
|-----------|--------|----------|--------------------------------------------|
| `speaker` | string | yes      | display name, e.g. `"SmartNPC"`            |
| `text`    | string | yes      | message body                               |
| `color`   | string | no       | one of: `white`, `yellow`, `green`, `red`, `cyan`, `blue`, `purple`, `gray`. Default `yellow`. |
| `channel` | string | no       | `"group"` routes the reply into the group chat panel; omit / `""` for private (default). When set to `"group"`, `group_id` is required. |
| `group_id`| string | conditional | Group chat session id; required when `channel="group"`, ignored otherwise. Must match an active `group_id` from a `group_create` event or an inbound `chat_received` event with `source=player_group`. See [ADR-0002](./adr/0002-group-chat-channel-end-to-end.md). |

**response.data**

| field | type | notes |
|-------|------|-------|
| `ok`  | bool | always `true` on success |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `speaker` or `text`

### `mail_send`  (client → server)

Display a HUD message (bottom-left). Carried over from M2; same semantics,
now delivered over ws.

**params**

| field  | type   | required | notes              |
|--------|--------|----------|--------------------|
| `text` | string | yes      | message body       |

**response.data**

| field     | type   | notes     |
|-----------|--------|-----------|
| `ok`      | bool   | `true`    |
| `message` | string | optional  |

### `game_get_time`  (client → server)

Read the current in-game time, date, season, and year. Read-only; safe to
call on every request. Fails with `mod_not_ready` if no save is loaded.

**params** — none.

**response.data**

| field         | type   | notes                                   |
|---------------|--------|-----------------------------------------|
| `ok`          | bool   | `true`                                  |
| `hour`        | int    | 0–23 (24-hour clock)                    |
| `minute`      | int    | 0 or 30 (SDV clock advances in 10 min)  |
| `timeOfDay`   | int    | raw value, e.g. `1430` for 14:30        |
| `day`         | int    | day of month, 1–28                      |
| `day_of_week` | string | short name: Mon/Tue/…                   |
| `season`      | string | `spring`/`summer`/`fall`/`winter`       |
| `year`        | int    | in-game year, starts at 1               |

**errors**

- `mod_not_ready` — no save loaded

### `game_get_weather`  (client → server)

Read today's weather and current season. Read-only.

**params** — none.

**response.data**

| field          | type   | notes                                         |
|----------------|--------|-----------------------------------------------|
| `ok`           | bool   | `true`                                        |
| `weather`      | string | `sunny`/`rainy`/`snowy`/`stormy` (summary)    |
| `is_raining`   | bool   | raw raining flag                              |
| `is_snowing`   | bool   | raw snowing flag                              |
| `is_lightning` | bool   | raw thunderstorm flag                         |
| `season`       | string | current season, same as `game_get_time`       |

**errors**

- `mod_not_ready` — no save loaded

### `friendship_get`  (client → server)

Read the player's friendship with a specific NPC.

**params**

| field | type   | required | notes                                |
|-------|--------|----------|--------------------------------------|
| `npc` | string | yes      | internal NPC name, e.g. `"Abigail"`  |

**response.data**

| field        | type   | notes                                                   |
|--------------|--------|---------------------------------------------------------|
| `ok`         | bool   | `true`                                                  |
| `npc`        | string | echo of the queried name                                |
| `points`     | int    | raw friendship points (250 per heart)                   |
| `hearts`     | int    | `points / 250`                                          |
| `max_hearts` | int    | usually `10`                                            |
| `status`     | string | `friendly`/`dating`/`engaged`/`married`/`none`          |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`

### `npc_get_nearby`  (client → server)

Scan the NPC's current map for other characters inside a visibility radius.
Read-only. Backed by a tick-driven cache (refreshed ~1 Hz); callers can pass a
custom `radius` to trigger an on-demand scan at that radius.

**params**

| field    | type   | required | notes                                         |
|----------|--------|----------|-----------------------------------------------|
| `npc`    | string | yes      | observer NPC internal name, e.g. `"XiaMi"`    |
| `radius` | number | no       | visibility radius in tiles (default `10`)     |

**response.data**

| field    | type   | notes                                                        |
|----------|--------|--------------------------------------------------------------|
| `ok`     | bool   | `true`                                                       |
| `npc`    | string | echo of observer                                             |
| `map`    | string | observer's current map name                                  |
| `radius` | number | radius actually used                                         |
| `count`  | int    | length of `nearby`                                           |
| `nearby` | array  | entities in range, sorted by distance (see row below)        |

Each element of `nearby`:

| field      | type   | notes                                                      |
|------------|--------|------------------------------------------------------------|
| `name`     | string | internal NPC name or farmer name                           |
| `type`     | string | `"npc"` or `"player"`                                      |
| `x`        | number | tile X                                                     |
| `y`        | number | tile Y                                                     |
| `distance` | number | Euclidean tile distance from the observer                  |
| `facing`   | int    | `0`=up `1`=right `2`=down `3`=left                         |
| `map`      | string | entity's map (currently always equal to observer's map)    |
| `action`   | string | (players only) `walking`/`using_tool`/`sitting`/`idle`     |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `npc_not_found` — no NPC with that internal name on the current save

### `npc_get_environment`  (client → server)

Read a bundle of environmental context around the NPC: map, tile position,
facing, clock, season, weather, and a short list of salient nearby objects
(placed objects, crops, terrain features). Read-only.

**params**

| field | type   | required | notes                        |
|-------|--------|----------|------------------------------|
| `npc` | string | yes      | observer NPC internal name   |

**response.data**

| field            | type   | notes                                               |
|------------------|--------|-----------------------------------------------------|
| `ok`             | bool   | `true`                                              |
| `npc`            | string | echo of observer                                    |
| `map`            | string | current map, e.g. `"Farm"`                          |
| `x`              | number | observer tile X                                     |
| `y`              | number | observer tile Y                                     |
| `facing`         | int    | `0`=up `1`=right `2`=down `3`=left                  |
| `time_of_day`    | int    | raw SDV time, e.g. `1430`                           |
| `hour`           | int    | 0–23                                                |
| `minute`         | int    | 0–59                                                |
| `season`         | string | `spring`/`summer`/`fall`/`winter`                   |
| `weather`        | string | `sunny`/`rainy`/`snowy`/`stormy`                    |
| `nearby_objects` | array  | up to 16 features near the observer                 |

Each element of `nearby_objects`:

| field      | type   | notes                                          |
|------------|--------|------------------------------------------------|
| `name`     | string | display/internal name                          |
| `category` | string | `object`/`terrain`/`crop`/`furniture`/`building` |
| `x`        | number | tile X                                         |
| `y`        | number | tile Y                                         |
| `distance` | number | tiles from observer                            |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `npc_not_found` — NPC not on the current save

### `npc_move_to`  (client → server)

Pathfind an NPC to a target tile on its current map, or warp cross-map.

Uses `PathFindController` for same-map moves (respects walls, doors, terrain).
If `map` differs from the NPC's current map, the NPC is warped instantly —
proper cross-map pathing is deferred to a later milestone. The call returns
once the path request is queued; the NPC walks asynchronously on subsequent
ticks.

**params**

| field | type   | required | notes                                             |
|-------|--------|----------|---------------------------------------------------|
| `npc` | string | yes      | NPC internal name                                 |
| `x`   | int    | yes      | target tile X                                     |
| `y`   | int    | yes      | target tile Y                                     |
| `map` | string | no       | target map name (default: NPC's current map)      |

**response.data**

| field     | type   | notes                                                    |
|-----------|--------|----------------------------------------------------------|
| `ok`      | bool   | `true` if a path was found (same-map) or warp succeeded  |
| `npc`     | string | echo                                                     |
| `map`     | string | destination map actually used                            |
| `x`       | int    | destination tile X                                       |
| `y`       | int    | destination tile Y                                       |
| `message` | string | `"pathing"` / `"warped"` / `"no_route"`                  |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save
- `unknown_map` — `map` does not resolve to a `GameLocation`
- `pathfind_error` — pathfinder threw (malformed tile, etc.)

### `npc_face_direction`  (client → server)

Turn an NPC to one of four cardinal directions. Equivalent to
`NPC.faceDirection(int)`. Does not cancel any active `PathFindController`.

**params**

| field       | type   | required | notes                                |
|-------------|--------|----------|--------------------------------------|
| `npc`       | string | yes      | NPC internal name                    |
| `direction` | string | yes      | `up` / `down` / `left` / `right`     |

**response.data**

| field       | type   | notes                                              |
|-------------|--------|----------------------------------------------------|
| `ok`        | bool   | `true`                                             |
| `npc`       | string | echo                                               |
| `direction` | string | applied direction (lowercase)                      |
| `facing`    | int    | `0`=up `1`=right `2`=down `3`=left                 |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc` or bad `direction`
- `unknown_npc` — NPC not on current save

### `npc_get_position`  (client → server)

Read an NPC's current tile position, map, and facing.

**params**

| field | type   | required | notes             |
|-------|--------|----------|-------------------|
| `npc` | string | yes      | NPC internal name |

**response.data**

| field       | type    | notes                                                  |
|-------------|---------|--------------------------------------------------------|
| `ok`        | bool    | `true`                                                 |
| `npc`       | string  | echo                                                   |
| `x`         | number  | tile X (fractional while walking)                      |
| `y`         | number  | tile Y (fractional while walking)                      |
| `map`       | string  | current map, e.g. `"Farm"`                             |
| `facing`    | int     | `0`=up `1`=right `2`=down `3`=left                     |
| `direction` | string  | facing as word: `up`/`down`/`left`/`right`             |
| `is_moving` | bool    | `true` iff `NPC.controller != null`                    |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save

### `npc_summon`  (client → server)

Warp the NPC to the map edge nearest the player, then pathfind it to the
player's current tile. Used when the player says "come here" / "过来" without
naming a landmark — the mod picks a reasonable arrival tile.

Cancels any active follow/lead behavior on the NPC.

**params**

| field | type   | required | notes             |
|-------|--------|----------|-------------------|
| `npc` | string | yes      | NPC internal name |

**response.data**

| field     | type   | notes                                                    |
|-----------|--------|----------------------------------------------------------|
| `ok`      | bool   | `true` once the summon request is queued                 |
| `npc`     | string | echo                                                     |
| `message` | string | `"warped"` / `"approaching"` / `"no_route"`              |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save

### `npc_emote`  (client → server)

Show a Stardew-native emote bubble above the NPC's head for ~1 second.
Cosmetic only — does not move the NPC, does not send a chat line.
Uses SDV's `Character.doEmote(int)` under the hood.

**params**

| field  | type   | required | notes                                                                                                   |
|--------|--------|----------|---------------------------------------------------------------------------------------------------------|
| `npc`  | string | yes      | NPC internal name                                                                                       |
| `kind` | string | no       | `exclamation` / `question` / `heart` / `sleep` / `happy` / `sad` / `angry` / `music` / `sparkle` / `pause`. Defaults to `sparkle`. Unknown values fall back to `exclamation` (classic `!` bubble). |

**response.data**

| field     | type   | notes                                                                           |
|-----------|--------|---------------------------------------------------------------------------------|
| `ok`      | bool   | `true` once the emote is queued on the game thread                              |
| `npc`     | string | echo                                                                            |
| `mode`    | string | the `kind` that was actually used (may differ from input on unknown-kind fallback) |
| `message` | string | `"emote <id>"` where `<id>` is the raw SDV emote integer                        |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save

### `npc_give_item`  (client → server)

Place a SDV item into the player's inventory, in-character as if the
NPC handed it over. Uses SDV's `ItemRegistry.Create(qualifiedItemId, count)`
to resolve the item. Each NPC's set of valid items is established in
their SOUL.md "Signature gift items" section.

**params**

| field     | type   | required | notes                                                                       |
|-----------|--------|----------|-----------------------------------------------------------------------------|
| `npc`     | string | yes      | NPC internal name                                                           |
| `item_id` | string | yes      | SDV qualified item id, e.g. `(O)167` (Joja Cola) or `(O)66` (Amethyst)      |
| `count`   | int    | no       | how many to give; defaults to 1, server-side clamped to max 5               |

**response.data**

| field     | type   | notes                                                                |
|-----------|--------|----------------------------------------------------------------------|
| `ok`      | bool   | `true` once placed in inventory                                      |
| `npc`     | string | echo                                                                 |
| `item_id` | string | echo of the resolved qualified item id                               |
| `count`   | int    | how many were actually added                                         |
| `message` | string | optional human-readable status, e.g. `"gave 1× (O)167"`              |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc` or `item_id`
- `unknown_npc` — NPC not on current save
- `unknown_item` — SDV's `ItemRegistry.Create` could not resolve the qualified id
- `inventory_full` — player inventory had no slot; SDV dropped the item at the player's feet as a pickup

### `npc_follow_start`  (client → server)

Begin a follow behavior — the NPC stays ~2 tiles behind the player, crossing
map boundaries as the player moves. Only one follow behavior is active per
NPC; calling this while already following refreshes the target. Calling it
while the NPC is summoning or leading cancels the prior mode.

**params**

| field | type   | required | notes             |
|-------|--------|----------|-------------------|
| `npc` | string | yes      | NPC internal name |

**response.data**

| field | type   | notes             |
|-------|--------|-------------------|
| `ok`  | bool   | `true` on success |
| `npc` | string | echo              |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save

### `npc_follow_stop`  (client → server)

End an active follow behavior. Idempotent — calling while the NPC is idle
returns `ok=true` with no effect.

**params**

| field | type   | required | notes             |
|-------|--------|----------|-------------------|
| `npc` | string | yes      | NPC internal name |

**response.data**

| field | type   | notes             |
|-------|--------|-------------------|
| `ok`  | bool   | `true`            |
| `npc` | string | echo              |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save

### `npc_lead_to`  (client → server)

Ask the NPC to walk ahead of the player toward a destination tile. Unlike
`npc_move_to`, this actively coordinates with the player's position —
the NPC pauses when the player falls too far behind and resumes when
they catch up.

**params**

| field | type   | required | notes                                         |
|-------|--------|----------|-----------------------------------------------|
| `npc` | string | yes      | NPC internal name                             |
| `x`   | int    | yes      | target tile X                                 |
| `y`   | int    | yes      | target tile Y                                 |
| `map` | string | no       | target map (default: NPC's current map)       |

**response.data**

| field | type   | notes                                |
|-------|--------|--------------------------------------|
| `ok`  | bool   | `true` if the lead request was queued |
| `npc` | string | echo                                 |
| `x`   | int    | destination tile X                   |
| `y`   | int    | destination tile Y                   |
| `map` | string | destination map actually used        |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save
- `unknown_map` — `map` does not resolve to a `GameLocation`
- `pathfind_error` — pathfinder threw

### `npc_get_behavior`  (client → server)

Query the NPC's current high-level behavior mode. Useful for deciding
whether a prior command is still in flight before issuing a new one.

**params**

| field | type   | required | notes             |
|-------|--------|----------|-------------------|
| `npc` | string | yes      | NPC internal name |

**response.data**

| field  | type   | notes                                                      |
|--------|--------|------------------------------------------------------------|
| `ok`   | bool   | `true`                                                     |
| `npc`  | string | echo                                                       |
| `mode` | string | one of: `idle` / `summoning` / `following` / `leading`     |

**errors**

- `mod_not_ready` — no save loaded
- `invalid_params` — missing `npc`
- `unknown_npc` — NPC not on current save

### `player_get_status`  (client → server)

Read whether the player is currently available to be interrupted. Used by
proactive scheduling to decide whether to defer a planned NPC action.

**params** — none.

**response.data**

| field       | type   | notes                                                       |
|-------------|--------|-------------------------------------------------------------|
| `ok`        | bool   | `true`                                                      |
| `busy`      | bool   | composite of `in_menu` / `in_event` / cutscene              |
| `in_menu`   | bool   | a clickable menu is open                                    |
| `in_event`  | bool   | a cutscene / in-game event is running                       |
| `is_moving` | bool   | player is walking/running                                   |
| `location`  | string | current map name                                            |

**errors**

- `mod_not_ready` — no save loaded

## MCP-only tools (no ws traffic)

These tools live entirely inside `smartnpc-mcp` and do not traverse the
WebSocket bridge. They are exposed to MCP clients only. See
[`docs/mcp-tools.md`](./mcp-tools.md) for the full input/output schema:

| tool                    | side-effect | description                                                              |
|-------------------------|-------------|--------------------------------------------------------------------------|
| `npc_send_message`      | WRITE       | NPC-to-NPC private message; buffered in an in-memory FIFO inbox AND triggers the recipient's Hermes profile via hermesrelay |
| `npc_broadcast_event`   | WRITE       | NPC-to-all fire-and-forget event; no inbox; fans out to every routed Hermes profile via hermesrelay                          |
| `npc_inbox_get`         | READ        | Peek pending messages queued for a recipient NPC                         |
| `npc_inbox_ack`         | WRITE       | Drop messages from an inbox by id                                        |
| `npc_get_named_locations` | READ      | Return the static table of human-addressable Farm landmarks              |

## Events

See [`events.md`](./events.md) for the full catalog (including reserved
schemas and synthetic events that smartnpc-mcp originates on its own).
The summaries below describe what the SMAPI mod emits today.

### `chat_message`  (server → client)

Emitted when the player sends a line targeted at a specific NPC via the
in-game chat panel. This is the primary "player talks to NPC" trigger.

**data**

| field    | type   | notes                                       |
|----------|--------|---------------------------------------------|
| `npc`    | string | recipient NPC internal name                 |
| `target` | string | redundant alias for `npc` today             |
| `text`   | string | raw UTF-8 text typed by the player          |
| `source` | string | `"player"`                                  |

### `chat_received`  (server → client)

Emitted when the player sends a line through the in-game chat box without
addressing a specific NPC (Ctrl+T). Carries the list of Agent-managed NPCs
within audible range so the consumer can decide whether to synthesize a
targeted `chat_message` for the nearest one or fan the line out as
ambient chatter. Also emitted by the legacy group chat UI (with an empty
`audible_npcs` list).

**data**

| field          | type             | notes                                                              |
|----------------|------------------|--------------------------------------------------------------------|
| `text`         | string           | the raw text the player typed                                      |
| `source`       | string           | one of `"player"` (legacy private chat box, default when empty) or `"player_group"` (player typed into a group chat session). Determines downstream rendering: `smartnpc-mcp` injects an explicit group-context prefix into the Hermes prompt when `source="player_group"`. See [ADR-0002](./adr/0002-group-chat-channel-end-to-end.md). |
| `group_id`     | string           | group chat session id when `source="player_group"`; empty / omitted otherwise. |
| `audible_npcs` | array (optional) | Agent-managed NPCs within `AudibleNPCResolver.DefaultRadius` tiles of the player, sorted by distance (closest first). Omitted / empty when none are in earshot or when the source is not the chat box. |

Each entry of `audible_npcs`:

| field      | type   | notes                                          |
|------------|--------|------------------------------------------------|
| `name`     | string | NPC internal name                              |
| `map`      | string | NPC's current map                              |
| `distance` | number | Euclidean tile distance from the player        |
| `x`        | int    | NPC tile X                                     |
| `y`        | int    | NPC tile Y                                     |

> `smartnpc-mcp` consumes `chat_received` and — when `audible_npcs` is non-empty —
> synthesizes a `chat_message` notification targeted at `audible_npcs[0]`.
> Downstream MCP clients receive both events on the same channel.


### `npc_interact`  (server → client)

Emitted when the player clicks on an Agent-managed NPC to initiate conversation.
The client should generate an AI response and send it back via `chat_say`.

**data**

| field  | type   | notes                                      |
|--------|--------|--------------------------------------------|
| `npc`  | string | internal NPC name, e.g. `"XiaMi"`          |
| `source` | string | `"player"` — the interaction initiator   |

### `group_create`  (server → client)

Emitted when the player opens a group chat session (legacy UI).

**data**

| field          | type           | notes                                |
|----------------|----------------|--------------------------------------|
| `participants` | array of string | NPC internal names in the group     |

## Lifecycle

- Client connects to `ws://127.0.0.1:18745/ws`; no handshake beyond the
  standard WebSocket upgrade.
- If the mod is not running, client retries with exponential backoff.
- On disconnect, the mod drops any queued events; no replay.
- Server sends a WebSocket ping every 30s; client must reply with pong.

## Hotkey

The in-game chat box is activated with **Ctrl+T** (single-player override of
the normal multiplayer `t`). The mod owns the hotkey; this is not part of the
wire protocol but documented here for operator awareness.

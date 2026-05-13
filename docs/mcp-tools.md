# SmartNPC MCP Tool Catalog

Tools exposed by [`smartnpc-mcp`](../smartnpc-mcp/). Definitions live in
[`internal/tools/`](../smartnpc-mcp/internal/tools/); descriptions below
mirror the registration-time strings the LLM sees.

> Format: each tool lists **side-effect**, **when to call**, **input
> fields**, **error codes**. For the canonical wire schema see
> [`protocol.md`](./protocol.md).

## Discovery

Tools are exposed via two transports:

- **stdio** — the legacy `smartnpc-agent` dev harness spawns mcp as a
  child process and listens on stdin/stdout.
- **streamable HTTP** — `smartnpc-mcp --http :3000` serves at `/mcp`.
  This is the Hermes-first path; Hermes profiles connect via
  `mcp_servers.smartnpc_game.url`.

A Hermes profile sees every tool listed below (minus any names in its
`mcp_servers.smartnpc_game.tools.exclude`).

## Always-available

| Tool | Side-effect | Requires save | Summary |
|---|---|---|---|
| [`ping`](#ping) | READ | no | liveness echo |
| [`npc_send_message`](#npc_send_message) | WRITE (in-process) | no | NPC→NPC private message |
| [`npc_broadcast_event`](#npc_broadcast_event) | WRITE (in-process) | no | NPC→all NPCs broadcast |
| [`npc_inbox_get`](#npc_inbox_get) | READ (in-process) | no | pull pending inter-NPC messages |
| [`npc_inbox_ack`](#npc_inbox_ack) | WRITE (in-process) | no | clear acked messages |

## Mod-backed (require ws bridge to SMAPI + loaded save)

### Speech & notifications

| Tool | Side-effect | Summary |
|---|---|---|
| [`chat_say`](#chat_say) | WRITE (visible) | speak one in-character line in the game chat box |
| [`mail_send`](#mail_send) | WRITE (visible) | system HUD notification — not NPC speech |

### Game state (read-only)

| Tool | Summary |
|---|---|
| [`game_get_time`](#game_get_time) | hour / minute / day / season / year |
| [`game_get_weather`](#game_get_weather) | weather + season |
| [`friendship_get`](#friendship_get) | hearts + relationship status with NPC |
| [`player_get_status`](#player_get_status) | busy / in_menu / in_event / is_moving / current location |
| [`npc_get_position`](#npc_get_position) | tile / map / facing / is_moving |
| [`npc_get_nearby`](#npc_get_nearby) | other characters within radius |
| [`npc_get_environment`](#npc_get_environment) | bundle: position + clock + weather + nearby objects |
| [`npc_get_named_locations`](#npc_get_named_locations) | farm landmarks (湖边, 大门, ...) |
| [`npc_get_behavior`](#npc_get_behavior) | current mode (idle/summoning/following/leading) |

### Physical action (high-impact)

| Tool | Summary |
|---|---|
| [`npc_move_to`](#npc_move_to) | pathfind to tile (same map) or warp (cross-map) |
| [`npc_face_direction`](#npc_face_direction) | turn to up/down/left/right |
| [`npc_summon`](#npc_summon) | warp to map edge + walk to player |
| [`npc_emote`](#npc_emote) | show sparkle / `!` / heart bubble above head (~1 s) |
| [`npc_give_item`](#npc_give_item) | hand a SDV item from the NPC's signature gift list to the player |
| [`npc_follow_start`](#npc_follow_start) | ~2 tiles behind, follows across maps |
| [`npc_follow_stop`](#npc_follow_stop) | cancel follow |
| [`npc_lead_to`](#npc_lead_to) | lead player to a tile, coordinating with player position |

---

## Detail

### `ping`

Liveness check. Echoes `message` and returns `serverNow`. Use to verify
the MCP connection is alive before more expensive tools.

### `chat_say`

Speak one in-character line. **Only sanctioned way** for an NPC
profile to emit visible dialogue. Plain UTF-8 text, no markdown, no
emoji-as-image, 1-3 short sentences. Attribute via `speaker` (NPC
display name).

Inputs: `speaker`, `text`, optional `color`, optional `channel`,
optional `group_id`. Errors: `mod_not_ready`, `invalid_params`.

**Group chat**: when replying to a `chat_received` event with
`source="player_group"`, you MUST set `channel="group"` and
`group_id=<the inbound group_id>`. Omitting either field causes the
mod to dispatch the reply to the private toast/panel instead of the
group panel — replies leak across channels. For private 1:1 chat,
omit `channel` (defaults to private). See
[ADR-0002](./adr/0002-group-chat-channel-end-to-end.md).

### `mail_send`

System HUD bubble — not NPC speech. Use for meta notifications (quest
hint, debug ping). For in-character speech use `chat_say`.

Inputs: `text`. Errors: `mod_not_ready`, `invalid_params`.

### `game_get_time`

Read clock + calendar. Call before time-of-day-sensitive greetings or
bedtime suggestions. No params.

Output: `hour`, `minute`, `timeOfDay`, `day`, `day_of_week`, `season`,
`year`. Errors: `mod_not_ready`.

### `game_get_weather`

Read weather + season. Call for weather-aware small talk. No params.

Output: `weather` (sunny/rainy/snowy/stormy), `is_raining`,
`is_snowing`, `is_lightning`, `season`. Errors: `mod_not_ready`.

### `friendship_get`

Read hearts + status with an NPC. Call BEFORE any
relationship-sensitive reply (gifts, apologies, romance).

Inputs: `npc`. Output: `npc`, `points`, `hearts`, `max_hearts`,
`status` (friendly/dating/engaged/married/none). Errors:
`mod_not_ready`, `invalid_params`, `npc_not_found`.

### `player_get_status`

Read whether the player is currently available to be interrupted —
call BEFORE proactive actions.

Output: `busy`, `in_menu`, `in_event`, `is_moving`, `location`.

### `npc_get_nearby`

Scan the NPC's map for other characters in radius.

Inputs: `npc`, optional `radius` (default 10). Output sorted by
distance.

### `npc_get_environment`

One-shot bundle: position + clock + weather + nearby objects. Use
instead of three separate calls.

Inputs: `npc`. Output includes `map`, `x`/`y`, `facing`, `time_of_day`,
`hour`, `minute`, `season`, `weather`, `nearby_objects` (up to 16).

### `npc_move_to`

Pathfind to a target tile. Same-map uses PathFindController; different
map warps (cross-map pathing is deferred). High-impact — call only
when the player explicitly asks.

Inputs: `npc`, `x`, `y`, optional `map`. Errors: `unknown_npc`,
`unknown_map`, `pathfind_error`.

### `npc_face_direction`

Turn an NPC to up/down/left/right. Low-impact.

### `npc_get_position`

Read tile/map/facing/is_moving. Use to verify a prior move arrived.

### `npc_summon`

Warp NPC to map edge then walk to player. Use when player says "过来"
without a landmark.

### `npc_emote`

Show a SDV-native emote bubble above the NPC's head (sparkle / `!` /
heart / etc) for ~1 second. Pure visual flourish — pairs naturally
with `npc_summon` when an NPC drops in proactively to telegraph "I
just arrived". Defaults to `sparkle` which maps to the exclamation
bubble. Does not move the NPC and does not send chat.

### `npc_give_item`

Hand the player a SDV item, in-character as if the NPC pulled it
out of their pocket. The item ID is a SDV qualified id (e.g.
`(O)167` Joja Cola, `(O)66` Amethyst). Each NPC has a fixed
"signature gift items" list in their SOUL.md — the LLM only passes
ids from that list. See `smartnpc-gift-policy` SKILL for the
intent-detection + refusal flow. No gold is charged today.

### `npc_follow_start` / `npc_follow_stop`

Begin/end a follow behavior. Idempotent.

### `npc_lead_to`

Walk ahead of the player toward a tile; pause when player falls
behind. Use for "带我去 X" requests.

### `npc_get_behavior`

Read current mode: `idle` / `summoning` / `following` / `leading`.

### `npc_get_named_locations`

Static table of human-addressable farm landmarks (湖边, 大门, ...).

### `npc_send_message`

Send a private message from one NPC to another. Recipient is reached
three ways from a single emit site:

1. **Hermes profile wake** (primary): the synthetic event is fed
   through the shared `bridge.EventHandler` so hermesrelay POSTs the
   recipient's Hermes Gateway, identical to a mod-sourced event. See
   [ADR-0001](./adr/0001-synthetic-events-go-through-hermesrelay.md).
2. MCP push notification (`npc_message`).
3. Inbox pull via `npc_inbox_get`.

Inputs: `from`, `to`, `text`, optional `kind`. Errors: `invalid_params`
when from/to/text are empty or from == to.

### `npc_broadcast_event`

Fire-and-forget broadcast to every subscribed NPC. Not queued in any
inbox. Use for world-wide signals. Like `npc_send_message`, fans out
through the shared `bridge.EventHandler` — every routed Hermes profile
receives a Gateway POST.

Inputs: `from`, `kind`, optional `data` (JSON).

### `npc_inbox_get` / `npc_inbox_ack`

Pull / clear pending inter-NPC messages for a recipient.

---

## Adding a new tool

1. Add a Go file in `smartnpc-mcp/internal/tools/<domain>.go` with
   `register<Domain>(s *mcp.Server, br *bridge.WSClient)`.
2. Wire it into `RegisterAll` in `registry.go`.
3. Write an Input/Output struct pair with `json` + `jsonschema` tags;
   Output's first field must be `OK bool`.
4. Add the SMAPI handler on the mod side and the bridge action constant.
5. Update `docs/protocol.md` with the new action.
6. Update this file with the new tool entry.
7. Test via `InMemoryTransport` in `<domain>_test.go`.

See [`smartnpc-mcp/internal/tools/chat.go`](../smartnpc-mcp/internal/tools/chat.go)
for a minimal template.

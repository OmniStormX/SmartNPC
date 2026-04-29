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

## Events

### `chat_received`  (server → client)

Emitted when the player submits a line in the in-game chat box.

**data**

| field    | type   | notes                                    |
|----------|--------|------------------------------------------|
| `text`   | string | the raw text the player typed            |
| `source` | string | `"player"` for now; reserved for future  |

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

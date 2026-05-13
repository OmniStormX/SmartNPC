# M5.6 / M5.7 — Manual End-to-End Verification

The automated pipeline test ([`pipeline_test.go`](../smartnpc-mcp/cmd/smartnpc-mcp/pipeline_test.go))
proves the wire shape of player chat → relay → Hermes is correct without
the game. This doc walks the **manual happy-path** with all five
processes live, for the cases the automated test cannot cover:

- the Hermes LLM actually replying in character
- a `chat_say` round-trip reaching the in-game chat bubble
- the LLM autonomously calling `game_get_time` / `game_get_weather`

## Prerequisites

| Component | How |
|---|---|
| Hermes installed in WSL | `hermes --version` returns 0.11.0+ |
| Hermes `mcp` Python package | `~/.hermes/hermes-agent/venv/bin/pip install mcp` |
| Stardew Valley + SMAPI | `D:\Stardew Valley\StardewModdingAPI.exe` runs |
| smartnpc-mcp binary | `task mcp:build` |
| Mod installed | `task mod:install` |
| xiami Hermes profile | `bash /mnt/d/SmartNPC/hermes/install.sh` (HOST_IP auto-detected) |
| LLM credentials in Hermes | profile's `config.yaml::custom_providers` configured |

## Process startup order

Open four terminal windows.

### 1. Stardew Valley + SMAPI

```cmd
"D:\Stardew Valley\StardewModdingAPI.exe"
```

Load a save. Wait until you can walk around. Locate XiaMi (spawns near
the farm — see [`smapi-mod/NPC/XiaMiData.cs`](../smapi-mod/NPC/XiaMiData.cs)).

### 2. smartnpc-mcp + Hermes relay (Windows cmd)

```cmd
cd /d D:\SmartNPC
bin\smartnpc-mcp\smartnpc-mcp.exe ^
  --http :3000 ^
  --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-url http://127.0.0.1:8642 ^
  --hermes-api-key smartnpc-test-key ^
  --hermes-conversation xiami ^
  --hermes-model xiami ^
  --hermes-npc XiaMi ^
  --hermes-persona-file hermes\profiles\xiami\SOUL.md ^
  --log-level debug
```

> Use `task mcp:build` if `bin/smartnpc-mcp/smartnpc-mcp.exe` is stale.
> Replace `127.0.0.1` with the WSL host IP if your setup is non-mirrored.

Expected log lines:

```
smartnpc-mcp starting ... http_addr=:3000
hermes relay enabled url=http://127.0.0.1:8642 conversation=xiami ...
ws connected url=ws://127.0.0.1:18745/ws
listening on streamable HTTP addr=:3000 endpoint=/mcp
```

### 3. Hermes xiami gateway (WSL window)

```cmd
wsl -d Ubuntu-22.04
```
```bash
hermes -p xiami gateway run --accept-hooks
```

Expected log:

```
Starting platform: api_server on 0.0.0.0:8642
...
Discovered N tools from smartnpc_game
```

If "Discovered N tools" doesn't appear, mcp-side HTTP isn't reachable
from WSL. Run `hermes -p xiami mcp test smartnpc_game` — see
[`hermes/README.md`](../hermes/README.md) troubleshooting.

### 4. (Optional) tail mcp logs

```cmd
type bin\smartnpc-mcp\... | findstr /i hermes
```

## Test M5.6 — Player chat reaches Hermes and `chat_say` displays

1. In-game, press **F2** (chat panel) and select **XiaMi**.
2. Type: `你好` and submit.
3. Watch the four windows:

   - **mod** logs: `[ChatUI] player → XiaMi: 你好`
   - **mcp** logs:
     ```
     ws frame received name=chat_message
     hermesrelay forwarded event event=chat_message status=200 conversation=xiami
     ```
   - **Hermes** logs: a new turn beginning, possibly tool calls like
     `friendship_get` / `game_get_time` (skill-driven).
   - **mcp** logs after Hermes calls `chat_say`:
     ```
     ws action chat_say speaker=XiaMi
     ```
   - **In-game**: a chat bubble appears in the bottom-left with
     XiaMi's reply.

**Pass criteria**:

- Reply appears in-game within ~5s.
- Text is in Chinese (per SOUL.md), 1-3 short sentences.
- Text does **not** mention AI, Hermes, MCP, JSON, tool names.

**Fail patterns**:

| Symptom | Likely cause | Fix |
|---|---|---|
| No POST from mcp | `--hermes-npc` filter mismatch | Confirm event payload `npc == "XiaMi"` (case-sensitive) |
| POST 404 / connection refused | Hermes gateway not running, or wrong port | Recheck step 3 |
| POST 401 | `--hermes-api-key` doesn't match `API_SERVER_KEY` | Sync overlay; restart Hermes |
| Hermes turn fires but no `chat_say` | LLM provider not configured / no credentials | Check `config.yaml::model`/`custom_providers` |
| chat_say returns `mod_not_ready` | No save loaded, or ws disconnected | Reload save; check mod logs |

## Test M5.7 — Hermes autonomously calls `game_get_time`

1. In the chat panel to XiaMi, ask: `现在几点了？`
2. Watch **mcp** logs for two correlated entries:

   ```
   hermesrelay forwarded event event=chat_message ...
   tools.call game_get_time ...        ← LLM autonomously called the tool
   ws action chat_say speaker=XiaMi    ← Final reply
   ```

3. In-game, the reply should reference the actual SDV time
   (paraphrased — "都下午了", not "14:30").

**Pass criteria**:

- LLM called `game_get_time` (visible in mcp logs).
- Reply phrasing matches the time of day, **does not** quote the raw
  number `14:30` or coordinates.

**Variations to spot-check**:

| Ask | LLM should call | Reply should mention |
|---|---|---|
| `今天天气？` | `game_get_weather` | sunny/rainy/...  in-character phrasing |
| `我们关系好吗？` | `friendship_get` with `npc=XiaMi` | warmth tuned to current hearts |

## Test M5.D — Proactive greeting on `npc_interact`

1. In-game, walk up to XiaMi and **right-click** her sprite.
2. Mod logs: `broadcast npc_interact for XiaMi`
3. mcp logs: `hermesrelay forwarded event event=npc_interact ...`
4. Hermes turn fires; the `proactive-greeting` skill should trigger.
5. In-game: XiaMi opens with a friendship-tier-appropriate line.

**Pass criteria**:

- Greeting appears within ~3-5s of the click.
- Line is appropriate for current heart tier (see SOUL.md table).
- LLM did **not** call movement tools — proactive-greeting forbids
  this on a clicked-greeting turn.

## Troubleshooting checklist

If anything goes wrong:

1. `hermes -p xiami mcp test smartnpc_game` — should ✓ Connected.
2. `curl http://127.0.0.1:8642/v1/models` (from Windows) — should
   return JSON listing `xiami`.
3. `curl http://127.0.0.1:3000/healthz` — should return `{"ok":true}`.
4. Tail mcp with `--log-level debug` and look for the relay-side
   POST after every event.
5. Tail Hermes with `tail -F ~/.hermes/profiles/xiami/logs/*.log`.

For known environment issues see [`hermes/README.md`](../hermes/README.md)
"前置条件" + WSL networking note.

## When tests pass

Mark M5.6 / M5.7 / M5.D ✅ in [`roadmap.md`](./roadmap.md). Open a PR
that:

- Updates the roadmap status table
- Adds any insights to this doc under a `## Lessons` section
- Captures a 30-second screen recording of the happy path if convenient

# SmartNPC Architecture (Hermes-first)

> **Status**: M5 production architecture (2026-05).  
> Predecessor: `smartnpc-agent` Go orchestrator — now frozen, see [`migration-smartnpc-agent.md`](./migration-smartnpc-agent.md).

## Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                  Stardew Valley + StardewModdingAPI             │
│                                                                 │
│  ┌────────────────────────────────────────────────────────┐    │
│  │ smapi-mod (C# .NET 6)                                  │    │
│  │   - Query / Perception / Movement / Behavior handlers  │    │
│  │   - NPC sprite + dialogue patches                      │    │
│  │   - Chat UI (panel + contact list + group)             │    │
│  │   - AudibleNPCResolver / TurnQueue (multi-NPC routing) │    │
│  │   - WebSocket server :18745                            │    │
│  └────────────────────────────────────────────────────────┘    │
└──────────────────────────────┬──────────────────────────────────┘
                               │  JSON envelope over ws
                               │  request / response / event
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│ smartnpc-mcp (Go)                                               │
│   internal/bridge      — ws client + protocol DTOs              │
│   internal/tools       — MCP tool implementations               │
│   internal/events      — typed event payloads + Hermes renderer │
│   internal/hermesrelay — outbound HTTP → Hermes /v1/responses   │
│   cmd/smartnpc-mcp     — stdio OR streamable HTTP transport     │
│                                                                 │
│   Transports:                                                   │
│     stdio  (default; legacy smartnpc-agent harness)             │
│     HTTP :3000 /mcp (Hermes-first; preferred)                   │
└──────┬─────────────────────────────────────────────────┬────────┘
       │ outbound HTTP POST                              │ MCP tool calls
       │ /v1/responses                                   │ (Hermes → mcp)
       ▼                                                 │
┌─────────────────────────────────────────────────────────┴───────┐
│ Hermes Agent — one profile per NPC                              │
│                                                                 │
│   ~/.hermes/profiles/<npc>/                                     │
│     SOUL.md            — persona / identity                     │
│     skills/smartnpc/   — game-tool-policy / proactive-greeting  │
│                          / memory-policy                        │
│     state.db           — conversation history + memory          │
│     config.yaml        — mcp_servers + API_SERVER_*             │
│                                                                 │
│   Gateway: api_server on :8642 (per-profile port)               │
│     - GET  /v1/models                                           │
│     - POST /v1/responses     ← events injected here             │
└─────────────────────────────────────────────────────────────────┘
```

## Component boundaries

### `smapi-mod/` — game-thread integration

- **Owns**: SMAPI hook surface, Harmony patches, NPC sprite + AnimatedSprite,
  chat UI, perception scanner, ws server, AudibleNPCResolver / TurnQueue.
- **Does not own**: AI decision-making, persona, memory, prompt construction.
- **Wire boundary**: emits typed JSON over `ws://127.0.0.1:18745/ws`.

### `smartnpc-mcp/` — capability boundary

- **Owns**: MCP tool definitions and schemas; ws-side validation
  (legal NPC names, valid maps, write permissions, timeouts, rate
  limits); event forwarding; outbound HTTP to Hermes.
- **Does not own**: NPC personality, decision-making, memory, choosing
  *when* to call a tool. Those are Hermes-side.
- **Hard checks** that MUST live here, not in Hermes prompts (because
  prompts are advisory):
  1. tool param validity, NPC exists, map resolvable
  2. write authorization (movement, friendship, inventory)
  3. timeout / retry / reconnect on the ws side
  4. rate limiting for high-frequency tools
  5. ordering of concurrent messages to the same NPC
  6. `chat_say` text length and UI safety
  7. game-thread call safety

### `hermes/profiles/<npc>/` — decision boundary

- **Owns**: persona (SOUL.md), behavior skills, long-term memory,
  conversation history per NPC, tool-call planning, reflection / cron.
- **Does not own**: game truth (always query via MCP), wire format,
  spatial routing.

## Event-trigger pipeline (Plan B from `hermes-event-trigger.md`)

The Hermes core is user-message-driven — it does not consume MCP
`notifications/message` as agent triggers. We bridge by having
`smartnpc-mcp` POST each relevant game event to Hermes Gateway's
`/v1/responses` endpoint. The conversation field keeps per-NPC memory
isolated; the `instructions` field re-asserts persona on every turn.

```
[player chat panel]            chat_message {npc=XiaMi, text="..."}
[player clicks XiaMi]          npc_interact {npc=XiaMi}
[day rolls over]               day_started {day, season, year}
                                       │
                                       ▼  (smartnpc-mcp ws receive loop)
                               bridge.EventHandler chain:
                                 1. MCP notification fan-out  (legacy MCP subscribers)
                                 2. hermesrelay.HandleEvent   ← Plan B

[NPC A tool call: npc_send_message]   npc_message {from=A, to=B, text}
[NPC A tool call: npc_broadcast]      npc_broadcast {from=A, kind, data}
                                       │
                                       ▼  (registerNpcMessage → emitSyntheticEvent
                                            feeds the SAME bridge.EventHandler;
                                            ctx is detached — see ADR-0001)
                                  → joins step 1 + step 2 above
                                       │
                                       │ POST http://gateway:8642/v1/responses
                                       │ {
                                       │   "model": "xiami",
                                       │   "input": "Farmer says to you: ...",
                                       │   "conversation": "xiami",
                                       │   "instructions": "<SOUL.md>",
                                       │   "store": true
                                       │ }
                                       ▼
                               Hermes turn:
                                 - tool calls back into mcp /mcp endpoint
                                 - decides chat_say
                                       │
                                       │ chat_say(speaker, text)
                                       │ (group chat: also channel="group" + group_id;
                                       │  see ADR-0002)
                                       ▼
                               smartnpc-mcp ws action → smapi-mod →
                                       chat bubble in-game
```

## Filtering & routing

| Layer | Filter |
|---|---|
| `smapi-mod` AudibleNPCResolver | Player-typed legacy chat with no addressee → nearest Agent-managed NPC within 8 tiles |
| `smartnpc-mcp` hermesrelay `--hermes-npc` | Events (mod-sourced **and** synthetic) whose `npc` / `to` / `target` matches this profile's NPC name; broadcast events (no NPC field) pass through |
| Hermes conversation | Per-NPC dialog memory isolation via the `conversation:` field |

## Profile cloning mechanism

The 6 NPC profiles under `hermes/profiles/` share most SKILL content
but each must self-narrate in its own NPC's voice and write to its
own memory namespace. Shared artifacts live in
[`hermes/profiles/_master/`](../hermes/profiles/_master/) (the master
template) and are rendered into each per-NPC profile dir by
[`scripts/render_profiles.sh`](../scripts/render_profiles.sh) via GNU
`sed` substitution of Mustache-style `{{NAME}}` tokens.

**Master scope**: `_master/` holds shared `config-overlay.yaml`,
`cron-recipes.md`, and `skills/smartnpc/{game-tool-policy,inter-npc-message,memory-policy,proactive-greeting}/SKILL.md`.
It deliberately does **not** hold `SOUL.md` — each NPC keeps its own
hand-written persona file, never templated.

Eight placeholders, all substituted at render time from a hardcoded
TABLE in `scripts/render_profiles.sh`:

| Placeholder | Example (abigail) | Use site |
|---|---|---|
| `{{NPC_NAME}}` | `Abigail` | PascalCase internal name; valid `speaker:` arg |
| `{{NPC_DISPLAY}}` | `阿比盖尔` | CN display name for prose |
| `{{NPC_DIR}}` | `abigail` | profile dir; `memories/<dir>/`, `conversation:` |
| `{{NPC_PORT}}` | `8643` | Hermes Gateway `API_SERVER_PORT` |
| `{{PEER_A_NAME}}` | `Penny` | first example peer in delegate flows |
| `{{PEER_A_DISPLAY}}` | `潘妮` | first peer CN display |
| `{{PEER_B_NAME}}` | `Sebastian` | second example peer |
| `{{PEER_B_DISPLAY}}` | `塞巴斯蒂安` | second peer CN display |

`scripts/render_profiles.sh` is idempotent: re-running produces the
same tree. xiami is rendered the same way the other 5 are (no
master-is-also-runnable asymmetry); the canonical sanity check is
`git diff hermes/profiles/xiami/` returns empty after a render.

`hermes/install.sh` is unchanged — it still owns the WSL-side copy
step; rendering happens earlier (and separately) in the author
workflow.

See [ADR-0003](./adr/0003-npc-name-placeholder-cloning.md) for the
rejected alternatives (runtime resolution, manual edit, xiami-as-master).

## Process layout (production)

| Process | Host | Port | Notes |
|---|---|---|---|
| Stardew Valley + SMAPI | Windows | n/a | Launch via `StardewModdingAPI.exe` |
| smartnpc-mcp (HTTP mode) | Windows | `:3000` | `--http`, `--ws-url`, `--hermes-*` |
| Hermes profile gateway | WSL | `:8642` | `hermes -p xiami gateway run` |

Each new NPC profile gets its own gateway port (`API_SERVER_PORT` in
the profile's `config.yaml`). A single `smartnpc-mcp` process fans
events out to all live profiles by reading
`hermes/runtime-config.yaml`:

```
SMAPI Mod (C#)
   │ ws :18745 (single client)
   ▼
smartnpc-mcp (Go, single process)
   ├── :3000/mcp  (Streamable HTTP) ◀── Hermes profile (MCP client)
   │                                      ├── xiami      (gateway :8642)
   │                                      └── abigail    (gateway :8643)
   │                                  (haley:8644, harvey:8645, penny:8646,
   │                                   sebastian:8647 — file-ready, not launched)
   │
   └── hermesrelay outbound  ── routed by event.npc/to/target → matching gateway
```

The fan-out routing is driven by `hermes/runtime-config.yaml` (consumed via
mcp's `--hermes-config` flag). Each entry maps an NPC's PascalCase internal
name (`npc_filter`) to a Hermes Gateway base URL plus conversation/model
identifiers and an env-var name (`api_key_env`) whose resolved value goes
into the outbound `Authorization: Bearer` header. Events whose `npc` field
doesn't match any entry are dropped with a Debug log. The mod's WebSocket
server only accepts one client, which is why mcp does the fan-out instead
of running multiple mcp instances.

## Why we picked this shape (one paragraph)

The previous architecture had two parallel "agent centers":
`smartnpc-agent` doing Go-side persona/memory/tool-loop AND Hermes
doing the same on the LLM side. The result was duplicate abstraction
and unclear ownership. The Hermes-first shape draws three sharp
boundaries — **smapi-mod** owns the game thread, **smartnpc-mcp** owns
the capability schema and the wire, **Hermes** owns decision and
identity — and gives each layer one job. Soft rules go into Hermes
prompts/skills; hard rules go into MCP handlers; spatial/thread-bound
rules go into smapi-mod.

## Doc map

| Doc | What's in it |
|---|---|
| `architecture.md` (this) | Component boundaries, data flow, why |
| `hermes-profiles.md` | How to author a profile (SOUL.md, skills, config-overlay) |
| `mcp-tools.md` | Tool catalog with description + side-effects |
| `events.md` | Event payload reference (mod-side + synthetic) |
| `protocol.md` | ws envelope spec, action / event schemas |
| `hermes-event-trigger.md` | The Plan A/B/C research that locked Plan B |
| `migration-smartnpc-agent.md` | Mapping from frozen Go agent → Hermes |
| `roadmap.md` | Milestone status + acceptance criteria |

# Migration: `smartnpc-agent` → Hermes-first

> **Effective**: 2026-05-11.  
> **Status**: M5 substantially complete; `smartnpc-agent` frozen (dev-only).

## TL;DR

The Go orchestrator `smartnpc-agent` is **out of the production
runtime**. Its responsibilities (persona, memory, scheduler, tool-loop,
multi-NPC) have moved to Hermes profiles. The capability boundary
(`smartnpc-mcp`) and the game-thread integration (`smapi-mod`) stay,
strengthened.

## Why

The pre-M5 stack had two parallel agent centers:

- `smartnpc-agent`: persona + memory + scheduler + tool loop + multi-NPC
- Hermes Agent: persona + memory + skill + tool router + MCP adapter
  + reflection

Continuing both meant duplicate abstraction and unclear ownership.
Hermes ships a more capable, more general version of every Go-side
feature we'd build, so the merge favors Hermes.

The new boundary:

| Layer | Owns |
|---|---|
| SMAPI mod | game APIs, game-thread safety, spatial routing |
| smartnpc-mcp | MCP tool schema, wire validation, event protocol, outbound HTTP relay |
| Hermes profile | decision-making, persona, memory, skills, reflection, multi-NPC behavior |

See [`architecture.md`](./architecture.md) for the full picture and
[`REFACTOR.md`](../REFACTOR.md) for the original rationale.

## What changed where

### Removed from the production path

| Frozen Go path | Why |
|---|---|
| `smartnpc-agent/cmd/smartnpc-agent/` | Hermes drives the agent loop now |
| `internal/agent/chat/` | Conversation loop is Hermes core + profile skills |
| `internal/agent/echo/` | Use `smartnpc-mcp --echo-mode` instead |
| `internal/llm/` | Hermes provider/model selection replaces it |
| `internal/memory/` (SQLite + FTS5) | Hermes `state.db` per profile |
| `internal/scheduler/` (cron) | Hermes `cron` CLI |
| `internal/group/` (multi-NPC orchestrator) | One profile per NPC + smapi-mod routing |
| `personas/*.json` | `hermes/profiles/<name>/SOUL.md` |
| `run.bat` legacy Agent path | Replaced by mcp `--hermes-*` flags + Hermes gateway |

### Added / strengthened

| Path | What |
|---|---|
| `smartnpc-mcp/internal/hermesrelay/` | outbound HTTP → Hermes `/v1/responses` |
| `smartnpc-mcp/internal/events/` | typed event payloads + Hermes input renderer |
| `smartnpc-mcp/internal/tools/npc_message.go` | inter-NPC messaging tools |
| `smartnpc-mcp/cmd/smartnpc-mcp/main.go` | `--http`, `--hermes-*` flags |
| `hermes/profiles/xiami/` | first Hermes profile |
| `hermes/install.sh` | sync script with HOST_IP auto-detection |
| `smapi-mod/NPC/AudibleNPCResolver.cs` | spatial NPC routing |
| `smapi-mod/NPC/TurnQueue.cs` | serialize concurrent NPC speech |

### Tool descriptions

The MCP tool descriptions were rewritten as "operations manuals"
(when-to-call / constraints / side-effect category) in M5.2. They are
the primary control surface the LLM sees — keep them rich. See
[`mcp-tools.md`](./mcp-tools.md).

## Feature mapping (Go → Hermes)

| Go feature | Hermes-first replacement |
|---|---|
| Dual-LLM router (dialogue vs. tool) | Hermes built-in tool routing |
| Persona JSON files | `SOUL.md` (markdown) per profile |
| SQLite memory + FTS5 | Hermes `state.db` per profile + memory toolset |
| Friendship injection into prompt | Skill instructions + `friendship_get` tool call |
| Cross-NPC channel (`local-only npc_send_message`) | MCP tools `npc_send_message` / `npc_broadcast_event` / `npc_inbox_*` |
| Scheduler / proactive | Hermes `cron` CLI ([recipes](../hermes/profiles/xiami/cron-recipes.md)) |
| Agent pool | Multiple Hermes profiles + per-profile gateway port |
| Event router | smartnpc-mcp event metadata + hermesrelay NPC filter + smapi-mod AudibleNPCResolver |
| Persona templates | `hermes/profiles/<name>/` template (SOUL + skills + overlay) |

Behavior-style delegation (the Go agent's `consult_npc`) is now handled
by the new `smartnpc-inter-npc-message` skill, which composes the
`npc_send_message` / `npc_inbox_*` MCP tools into the same "ask another
NPC and incorporate their answer" pattern.

## Behavior parity checklist

What works on the new stack vs the frozen Go stack:

All 6 NPCs (XiaMi + Abigail + Haley + Harvey + Penny + Sebastian) share
the same baseline skills (game-tool-policy, proactive-greeting,
memory-policy, inter-npc-message), so each row below applies uniformly
across the roster.

| Scenario | Frozen Go agent | Hermes-first |
|---|---|---|
| Player chat → NPC reply | ✅ | ✅ (via hermesrelay) |
| Time/weather-aware greeting | ✅ | ✅ (skill + tool calls) |
| Friendship-tier tone modulation | ✅ | ✅ (SOUL.md heart table + skill) |
| Movement on player request | ✅ | ✅ (game-tool-policy skill) |
| Memory across game days | ✅ (SQLite) | ✅ (state.db) |
| Proactive ("3 days no contact") | ✅ (Go scheduler) | ✅ (cron recipe) |
| Multi-NPC concurrent reply | ❌ (sequential) | ✅ (per-profile gateway + TurnQueue) |
| Group chat | ✅ (legacy UI) | ⚠️ Mod-side intact; orchestration TBD |

## If you have a request for `smartnpc-agent`

Before touching the Go agent, ask **"can this be a Hermes
profile/skill/MCP tool instead?"**:

1. **NPC behavior change** → edit `hermes/profiles/<npc>/SOUL.md` or
   add a SKILL.md.
2. **New game capability** → add an MCP tool in
   `smartnpc-mcp/internal/tools/` + bridge action + mod handler.
3. **New trigger / event** → add to `smapi-mod/`, declare in
   `internal/bridge/protocol.go` and `internal/events/`, update
   `events.md` and `protocol.md`.
4. **Proactive behavior** → add a cron recipe ([example](../hermes/profiles/xiami/cron-recipes.md)).
5. **Cross-NPC** → use the `npc_*_message` tools.

Only if **none** of the above fit, escalate to the Go agent — at
which point it's probably a real architecture decision and worth a
design doc.

## Decommission timeline

The Go agent stays on disk through M6 for regression-compare. Archival
target: once Hermes profiles ship two-NPC parity and ride one full
in-game season without regressions. Tracked as M5.G in
[`roadmap.md`](./roadmap.md).

Archive plan when triggered:

1. Move `smartnpc-agent/` to `archive/smartnpc-agent/`.
2. Remove it from `go.work` and the root `Taskfile.yml`.
3. Strip the legacy launch line from `run.bat`.
4. Keep the `personas/*.json` copy in archive as historical reference.

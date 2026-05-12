# smartnpc-agent — Frozen (dev-only)

> **Status (2026-05-12)**: **Frozen as of M5 refactor.** This package no longer
> participates in the production runtime. Its persona / memory / scheduler /
> tool-loop responsibilities have moved to Hermes profiles (see
> [`hermes/`](../hermes/) and [`docs/architecture.md`](../docs/architecture.md)).
>
> Kept on disk solely as:
>
> 1. A regression baseline for comparing Hermes behavior against the
>    original Go agent loop while M5 stabilizes.
> 2. A reference for the dual-LLM router design that M4 landed.
>
> **Do not add new features here.** New work goes into one of:
>
> - `smartnpc-mcp` — game capability boundary (tools, events, hard checks)
> - `hermes/profiles/<npc>/` — NPC persona, skills, memory, planning
> - `smapi-mod/` — game-thread integration and per-NPC routing

## Production runtime (M5)

The current production NPC pipeline does **not** use this binary:

```
Stardew Valley + SMAPI mod
  └─ws :18745─> smartnpc-mcp --http :3000 --hermes-* ─┐
                                                       │ MCP tool calls
                                                       │ + HTTP event POSTs
                                                       ▼
                                              Hermes profile gateway
```

See [`hermes/README.md`](../hermes/README.md) for the setup steps.

## Why this exists at all

When you want to **A/B compare** a new Hermes profile against the legacy
Go agent loop (same MCP tools, same game, two persona stacks), this
binary is still the cheapest dev harness. The launch line in
[`run.bat`](../run.bat) demonstrates the legacy path.

## Dev launch (kept working as regression harness)

```cmd
cd /d D:\SmartNPC\smartnpc-agent
bin\smartnpc-agent.exe ^
  --mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe ^
  --mcp-args "--ws-url ws://127.0.0.1:18745/ws" ^
  --log-level debug ^
  run ^
  --personas-dir personas ^
  --persona-url http://127.0.0.1:8642/v1 ^
  --api-key smartnpc-test-key ^
  --decision-url http://v2.open.venus.oa.com/llmproxy ^
  --decision-model gpt-5.5
```

Note that this still uses **stdio MCP** (mcp spawned as a child), not the
new HTTP transport. The Hermes-first path uses HTTP exclusively.

## What's allowed in this directory

| Change | Status |
|---|---|
| Bug fix in an existing feature | ✅ |
| Dependency upgrade | ✅ |
| Test that exercises the regression harness | ✅ |
| New feature (memory provider, new tool, ...) | ❌ — see [REFACTOR.md](../REFACTOR.md) |
| New persona | ❌ — write a Hermes profile instead |

## Migration map

Where each Go-agent concept lives in the Hermes-first architecture:

| Go agent (frozen) | Hermes-first replacement |
|---|---|
| `internal/agent/chat/` | Hermes agent loop + profile skills |
| `internal/agent/echo/` | `smartnpc-mcp --echo-mode` |
| `internal/llm/` | Hermes provider/model configuration |
| `internal/memory/` (SQLite + FTS5) | Hermes `state.db` (per profile) + memory toolset |
| `internal/scheduler/` (cron) | Hermes `cron` CLI ([example recipes](../hermes/profiles/xiami/cron-recipes.md)) |
| `internal/group/` (multi-NPC) | One Hermes profile per NPC + smapi-mod `AudibleNPCResolver` / `TurnQueue` |
| `personas/*.json` | `hermes/profiles/<name>/SOUL.md` + `skills/` |

## Removal plan

Targeted for archive (out of `go.work`) once Hermes profiles have
shipped two-NPC parity and ridden one full season without regressions.
No fixed date — track in `docs/roadmap.md` M5.G.

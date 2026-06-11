# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project overview

SmartNPC is a Hermes-first AI NPC runtime for Stardew Valley.

```
SMAPI Mod (C# .NET 6) ──ws :18745── smartnpc-mcp (Go) ──MCP HTTP── 6× Hermes Agent Profile
                                      └── hermesrelay ──POST──→ route by runtime-config.yaml
```

| Directory | Role |
|-----------|------|
| `smapi-mod/` | C# SMAPI mod: in-game UI, NPC sprites, WebSocket server, game-thread-safe queries |
| `smartnpc-mcp/` | Go MCP server: WebSocket↔MCP bridge, Hermes relay fan-out. No business persistence, no direct LLM calls. |
| `hermes/profiles/<npc>/` | NPC personality/decision: `SOUL.md`, skills, cron recipes, config overlays, local memory |
| `docs/` | Architecture, protocol, event docs |
| `scripts/` | Profile rendering, verification, launch |

## Commands

Use `task` as the canonical entry point. **Never use bare `go build` or `dotnet build`** when a `task` target exists.

```powershell
task --list
task ci              # profiles:verify + lint + test + build (final verification)
task ci-fast         # profiles:verify + lint + test (skip build)
task tidy            # go mod tidy + go work sync
task clean

task mcp:build       task mcp:test        task mcp:stop
task mcp:run         task mcp:run-echo    task mcp:health

task mod:build       task mod:install     # requires SMARTNPC_GAME_PATH

task profiles:render task profiles:verify
task net:check       task hooks:enable    task hooks:status
```

**Run a single Go test:**
```powershell
cd smartnpc-mcp; go test -run TestPing ./pkg/agentbridge/...
cd smartnpc-mcp; go test -run TestChatSay ./adapters/stardew/tools/...
```

## Environment

Copy `.env.example` to `.env` and edit. Taskfile auto-loads `.env`.

Key variables:
- `SMARTNPC_GAME_PATH` — Stardew Valley install dir
- `SMARTNPC_WS_URL` — ws://127.0.0.1:18745/ws
- `SMARTNPC_HTTP_PORT` — MCP HTTP port (default 3000)
- `SMARTNPC_MCP_URL` — full MCP URL for Hermes (e.g. `http://127.0.0.1:3000/mcp`)
- `SMARTNPC_HERMES_MODE` — `docker` or `local`
- `SMARTNPC_ACTIVE_PROFILES` — comma-separated NPC ids (e.g. `xiami,abigail`)

## Architecture boundaries

- **SMAPI glue** stays in `smapi-mod/`; reusable logic goes in Go.
- **`smartnpc-mcp`** is a protocol boundary only — no business persistence, no LLM calls.
- **Hermes profiles** carry all LLM personality/policy/decision. Do not bypass profiles with direct LLM calls in MCP or the mod.
- **`smartnpc-mcp/pkg/`** = game-agnostic framework. **`adapters/stardew/`** = Stardew-specific.
- **One MCP client per WebSocket bridge** — avoid changes that encourage multiple instances.
- **LLM access** is routed through Hermes Gateway; use `.env` values as truth source.

## Go conventions (`smartnpc-mcp/`)

- Module: `github.com/OmniStormX/SmartNPC`, Go 1.25+ (`go.mod` says `go 1.25.0`)
- **⚠️ CI workflow has `GO_VERSION: '1.22'`** — don't use Go 1.25-only features without fixing CI first
- stdio MCP mode: logs must go to **stderr only** — `fmt.Println` / default `log` corrupts MCP protocol
- Errors: `fmt.Errorf("...: %w", err)`
- Package comments and exported symbols in English
- Run `go test` through `task` where practical

### MCP tool rules

- Naming: `<domain>_<verb>` lowercase snake case. One domain per file.
- Input/Output structs: **both** `json` and `jsonschema` tags. Output first field: `OK bool`.
- Handler first return = `nil`; SDK fills Output struct.
- Framework tools (e.g. `ping`) go in `pkg/agentbridge/`, not the Stardew adapter.
- New tools → register in `adapters/stardew/tools/registry.go` `RegisterAll` → update `docs/protocol.md`.

## C# conventions (`smapi-mod/`)

- Target `net6.0`, nullable reference types enabled.
- Game-thread safety for all Stardew/SMAPI interactions.
- `.csproj` `<GamePath>` uses `SMARTNPC_GAME_PATH` env var; defaults to `ModBuildConfig` auto-detect.

## Hermes profiles

- **`hermes/npcs.yaml`** is the single source of truth for NPC metadata (id, game name, port, peers).
- **`hermes/profiles/_master/`** = shared templates. **`hermes/profiles/<npc>/`** = per-NPC output.
- `SOUL.md` is handwritten per NPC and tracked. Everything else is rendered from `_master/`.
- **Never edit non-`_master/` rendered output** — it will be overwritten by `task profiles:render`.
- After editing `_master/`: run `task profiles:verify`.
- 6 NPCs: `xiami`, `abigail`, `haley`, `harvey`, `penny`, `sebastian`. Ports: xiami=8642, abigail=8643, …, sebastian=8647.

## Testing

- New Go packages: at least one `*_test.go` with `Test*`.
- New MCP tools: end-to-end coverage with `InMemoryTransport` pattern (register → list → call → assert).
- Table-driven tests with `t.Run`. No sleeps >100ms. No real MCP subprocess or real WebSocket in unit tests.
- Standard cycle: `task ci-fast` during iteration → `task ci` before completion.
- If `task ci` fails, fix and rerun. **Don't claim completion without passing ci.**
- 3 failed attempts → stop and report the blocker.

## Git conventions

- **Do not commit unless explicitly asked.**
- Format: `<type>(<scope>): <subject>` — e.g. `feat(mcp): add npc_follow tool`
- Types: `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `ci`, `build`
- Scopes: `mcp`, `mod`, `hermes`, `docs`, `ci`, `tools`, `bridge`
- Commit only after `task ci` passes.
- No `--no-verify`, `[skip ci]`, or force-push to main.

## Windows / environment quirks

- Working directory: `D:\SmartNPC`. Shell: PowerShell (not cmd).
- All text files UTF-8 without BOM. Verify Chinese files: `python -c "open(r'PATH','rb').read().decode('utf-8')"`
- **`run.bat` must be CRLF** — `.gitattributes` enforces this, but direct writes may produce LF, breaking cmd parsing.
- C# mod builds need .NET 6 + local Stardew Valley/SMAPI assemblies. GitHub CI does **not** build the mod.
- `task mod:install` may stop a running game process to release locked DLLs. Close the game first.
- Game must launch via `StardewModdingAPI.exe`, not `Stardew Valley.exe`.
- WSL Hermes accessing Windows MCP `:3000/mcp` may need the Windows host IP, not `127.0.0.1`.

## Runtime launch order

1. Close game → `task mod:install`
2. Start MCP (`task mcp:run` or via `run.bat`)
3. Start Hermes gateways (WSL: `bash scripts/start_hermes_profiles.sh xiami,abigail`)
4. Launch game via `D:\Stardew Valley\StardewModdingAPI.exe`
5. Use `run.bat` for one-shot local flow

## Reply conventions

- Reply in Chinese. Keep technical terms, API names, file paths in English with backticks.
- After edits, 1-3 line summary with key file paths. Don't paste code unless asked.
- Prefer parallel tool calls for independent reads/searches.

# AGENTS.md

Guidance for AI coding agents working in this repository.

## Project overview

SmartNPC is a Hermes-first AI NPC runtime for Stardew Valley.

- `smapi-mod/`: C#/.NET 6 SMAPI mod. Owns in-game UI, NPC sprites, WebSocket server, game-thread-safe queries/actions, and thin SMAPI integration glue.
- `smartnpc-mcp/`: Go MCP server. Owns MCP tool schemas/handlers, WebSocket client bridge to the mod, validation, event formatting, and Hermes relay fan-out. It must not persist business state or call LLMs directly.
- `hermes/profiles/<npc>/`: NPC decision/personality layer. Each profile contains `SOUL.md`, skills, cron recipes, config overlays, and local memory/state.
- `docs/`: Architecture/protocol/tool/event documentation and manual verification reports.
- `scripts/`: Profile rendering, verification, launch, reset, and other helper scripts.

## Required workflow

Use `task` as the canonical local entry point for build/test/lint operations.

Common commands from repo root:

```powershell
task --list
task ci-fast        # lint + tests, skips build
task ci             # lint + tests + build
task tidy           # go mod tidy + go work sync
task clean

task mcp:build
task mcp:test
task mcp:run WS_URL=ws://127.0.0.1:18745/ws
task mcp:run-echo   # echo mode, no LLM
task mcp:health

task mod:build
task mod:install    # requires SMARTNPC_GAME_PATH

task profiles:verify
```

Notes:

- `task ci` is the expected final verification after code changes when local prerequisites are available.
- C# mod builds require .NET 6 plus local Stardew Valley/SMAPI assemblies. GitHub-hosted CI does not build the mod because it cannot install Stardew Valley.
- `task mod:install` may stop a running Stardew Valley/SMAPI process to release locked DLLs.
- Keep `.env` local. Use `.env.example` for documented configuration.

## Coding conventions

### Cross-cutting architecture

- Keep SMAPI-specific glue in `smapi-mod/`; put reusable protocol/business logic in Go when possible.
- `smartnpc-mcp` is a protocol/tool boundary: WebSocket <-> MCP plus Hermes outbound relay. Do not add long-lived business persistence there.
- Hermes profiles carry LLM personality, policy, and decision behavior. Do not bypass profiles by adding direct LLM calls in MCP or the mod.
- Maintain the one-MCP-client assumption for the SMAPI WebSocket bridge; avoid changes that encourage multiple `smartnpc-mcp` instances competing for the mod connection.

### Go (`smartnpc-mcp/`)

- Module path: `github.com/OmniStormX/SmartNPC`.
- Keep MCP tools under `internal/tools/`; register new tools in `internal/tools/registry.go`.
- For stdio MCP mode, logs must go to stderr. Do not use stdout logging that can corrupt the MCP protocol stream.
- Wrap errors with context using `fmt.Errorf("...: %w", err)`.
- Use English package comments, exported symbol comments, and TODO/FIXME milestone notes.
- Run `gofmt`/`go test` through `task` where practical.

### C# (`smapi-mod/`)

- Target framework is `net6.0`; nullable reference types are enabled.
- Prefer thin SMAPI event/API adapters and isolate pure logic where it can be tested or reasoned about without the game runtime.
- Respect game-thread safety for all Stardew/SMAPI interactions.

### Hermes profiles

- Shared profile templates live in `hermes/profiles/_master/`; rendered NPC profiles live under `hermes/profiles/<npc>/`.
- After editing `_master/` or regenerated profile content, run:

```bash
bash scripts/render_profiles.sh
bash scripts/test_profile_render.sh
```

or from repo root:

```powershell
task profiles:verify
```

- Avoid placeholder leaks and XiaMi-specific leaks in non-XiaMi profiles.

## Testing expectations

- New Go packages should include at least one `*_test.go` with a `Test*` function.
- New MCP tools need end-to-end coverage using the in-memory MCP transport pattern: register tools, list tools, call the tool, and assert structured content.
- Prefer table-driven Go tests for variants.
- Avoid sleeps over 100 ms in unit tests; use channels or eventually-style polling for async behavior.
- Do not rely on real MCP subprocesses or real WebSocket connections in unit tests; use in-memory transport and mocks.
- If a verification command fails, fix and rerun. If the same failure cannot be resolved after repeated attempts, stop and report the blocker clearly.

## Documentation and files

- Keep text files UTF-8. This repo contains Chinese documentation; avoid rewriting files with a legacy Windows encoding.
- Do not commit generated build outputs (`bin/`, `obj/`, `logs/`, `.gotmp/`, crop outputs) or local secrets (`.env`).
- Prefer updating existing docs over creating new markdown files unless the user asks or the new file is clearly necessary.
- When modifying files you have not read recently, reread the relevant section first.

## Git conventions

- Do not commit unless the user explicitly asks.
- Commit message format, when requested: `<type>(<scope>): <subject>` with types such as `feat`, `fix`, `refactor`, `test`, `docs`, `chore`, `ci`, or `build`.
- Do not use `--no-verify`, `[skip ci]`, or force-push main/master unless explicitly instructed.

## Windows/local environment notes

- The usual working directory is `d:\SmartNPC` on Windows.
- Prefer PowerShell-native commands in this environment, and avoid composing destructive filesystem operations across shells.
- Before recursive delete/move operations, verify resolved absolute paths are inside the intended workspace.
- If using WSL/Hermes, remember WSL may need the Windows host IP rather than `127.0.0.1` to reach `:3000/mcp`.
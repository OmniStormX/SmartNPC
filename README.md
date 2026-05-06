# SmartNPC

AI-driven NPC system for Stardew Valley, built on the Model Context Protocol (MCP).

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│  Stardew Valley.exe                                       │
│  └─ SMAPI Mod (C# .NET 6)  ── WebSocket :8765            │
└──────────────────────────────────────────────────────────┘
                       ↕ ws (JSON)
┌──────────────────────────────────────────────────────────┐
│  smartnpc-mcp (Go)        — MCP Server (stdio)           │
│  Exposes 20+ NPC behavior tools                           │
└──────────────────────────────────────────────────────────┘
                       ↕ stdio (MCP)
┌──────────────────────────────────────────────────────────┐
│  smartnpc-agent (Go)      — NPC Orchestrator (OpenAI)    │
│  14+ NPC agents with persona / memory / scheduler         │
└──────────────────────────────────────────────────────────┘
```

## Repository Layout

| Path               | Purpose                                                        |
|--------------------|----------------------------------------------------------------|
| `smapi-mod/`       | C# SMAPI mod that exposes game APIs via WebSocket              |
| `smartnpc-mcp/`    | Go MCP server, bridges WebSocket -> MCP tools                  |
| `smartnpc-agent/`  | Go agent orchestrator, drives NPC personas via OpenAI + MCP    |
| `docs/`            | Design docs, protocol spec, tool catalog (`roadmap.md`)        |
| `.codebuddy/`      | Project rules + skills consumed by CodeBuddy AI assistant      |
| `.github/`         | GitHub Actions: `ci.yml` (PR/push) + `release.yml` (tag)       |

## Quick Start

Install [Task](https://taskfile.dev) once:

```cmd
go install github.com/go-task/task/v3/cmd/task@latest
```

Build everything:

```cmd
task ci               :: lint + test + build (local equivalent of CI)
```

### Run the full stack (Windows)

**Prerequisites:**
- Go 1.22+, .NET SDK 6.0, Stardew Valley + SMAPI installed
- Hermes Agent (`v0.11+`) in WSL Ubuntu-22.04 with API Server enabled

**Step 1 — Start Hermes LLM backend (WSL terminal):**

```bash
hermes gateway run --accept-hooks
# verify: curl http://localhost:8642/health → {"status":"ok"}
```

**Step 2 — Start Stardew Valley via SMAPI:**

```cmd
"D:\Stardew Valley\StardewModdingAPI.exe"
```

**Step 3 — Start the NPC agent (Windows cmd):**

```cmd
cd /d d:\SmartNPC\smartnpc-agent
bin\smartnpc-agent.exe ^
  -mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe ^
  -mcp-args="--ws-url=ws://127.0.0.1:18745/ws" ^
  -log-level debug ^
  run ^
  -llm-url http://localhost:8642/v1 ^
  -api-key smartnpc-test-key ^
  -model hermes-agent ^
  -speaker XiaMi
```

The agent spawns `smartnpc-mcp` as a child process (stdio MCP) which connects
to the SMAPI mod via WebSocket. In-game press `Ctrl+T` to chat — the NPC will
respond through the Hermes LLM.

> **Important:** Do NOT run `task mcp:run` at the same time as the agent.
> The mod only accepts one WebSocket client; two mcp instances will fight
> over the connection.

### Other useful commands

```cmd
task --list           :: show all tasks
task ci-fast          :: lint + test (skip build)
task mcp:build        :: build only smartnpc-mcp
task agent:test       :: run only smartnpc-agent tests
task mod:install      :: build + deploy mod to game folder (game must be closed)
task tidy             :: go mod tidy across modules
```

> See `docs/roadmap.md` for the milestone breakdown and `.codebuddy/rules/`
> for the project conventions enforced during AI-assisted development.

## Releases

Push a semver tag to trigger `release.yml`:

```cmd
git tag v0.2.0
git push origin v0.2.0
```

GitHub Actions builds Windows + Linux Go binaries (`smartnpc-mcp.exe` and
`smartnpc-agent.exe`) and publishes a GitHub Release with auto-generated
changelog and `SHA256SUMS.txt`.

> **The SMAPI mod zip is not built in CI.** GitHub-hosted runners cannot
> install Stardew Valley, and `Pathoschild.Stardew.ModBuildConfig` requires
> the game's DLLs to be locally present. Build the mod locally with
> `task mod:build` and attach the resulting `StardewMCPBridge.dll` +
> `manifest.json` zip to the GitHub Release manually if you want to publish
> it. The same constraint is why CI has no `smapi-mod` job — local
> `task ci` is the source of truth for mod changes.

## Status

- [x] M1: Go workspace + stdio MCP server with `ping` tool + agent stdio client
- [x] M1.5: Taskfile + GitHub Actions CI/Release + project rules + ci-doctor skill
- [x] M2: SMAPI mod (HTTP + HUD), local auto-deploy hook
- [x] M3: WebSocket bridge, in-game chat box, `chat_say` tool, echo agent
- [ ] M4: OpenAI provider + persona loader + real Abigail dialogue
- [ ] M5: Memory (SQLite+FTS5), scheduler, multi-NPC orchestration

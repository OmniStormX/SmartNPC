Launch SmartNPC project components (Windows native, no WSL).

## Trigger
User says "启动项目" / "跑起来" / "启动 mcp" / "run server"

## Component Map
| Component | Launch | Port |
|-----------|--------|------|
| smartnpc-mcp | `task mcp:run` | `:3000` (HTTP), ws bridge to `:18745` |
| SMAPI Mod | User starts game via `StardewModdingAPI.exe` | `:18745` |
| smartnpc-agent | `bin\smartnpc-agent.exe ... run` | connects to mcp via stdio |

## SOP

### Step 1 — Prechecks
```cmd
go version
```
Require ≥1.22. If `task` is available: `C:\Users\synchen\go\bin\task.exe --version`

### Step 2 — Build
```cmd
C:\Users\synchen\go\bin\task.exe ci
```

### Step 3a — Start MCP (HTTP mode, for remote clients)
```cmd
C:\Users\synchen\go\bin\task.exe mcp:run
```
Or manually:
```cmd
smartnpc-mcp\bin\smartnpc-mcp.exe --http :3000 --ws-url="" --log-level=debug
```
Verify: `curl http://127.0.0.1:3000/healthz` → `{"ok":true}`

### Step 3b — Start Agent (stdio mode, for game chat)
```cmd
cd D:\SmartNPC\smartnpc-agent
bin\smartnpc-agent.exe --mcp-bin ..\smartnpc-mcp\bin\smartnpc-mcp.exe --mcp-args "--ws-url ws://127.0.0.1:18745/ws" --log-level debug run --api-key smartnpc-test-key
```

### Step 4 — (Optional) Start game
User launches Stardew Valley via `D:\Stardew Valley\StardewModdingAPI.exe`.
Agent auto-reconnects when ws becomes available.

## Key Flags
| Flag | Meaning |
|------|---------|
| `--http :PORT` | HTTP transport on `/mcp` + `/healthz` |
| `--ws-url URL` | SMAPI mod WebSocket URL |
| `--ws-url=""` | Disable ws bridge |
| `--echo-mode` | Built-in echo NPC (no LLM) |
| `--log-level` | debug/info/warn/error |
| `--api-key` | Hermes gateway API key |

## WDAC Troubleshooting
If exe blocked by Device Guard:
1. Rebuild: `task mcp:build` (hash changes)
2. Test: `bin\smartnpc-mcp.exe --version`
3. If still blocked: use `go test -run TestRunServer` entry point
4. Last resort: contact IT

## Anti-patterns
- Don't suggest WSL for running the project
- Don't use bare `smartnpc-mcp` (not in PATH)
- Don't start game with `Stardew Valley.exe` directly (must use `StardewModdingAPI.exe` for mods)

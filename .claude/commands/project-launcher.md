Launch SmartNPC project components (Windows native, no WSL).

## Trigger
User says "启动项目" / "跑起来" / "启动 mcp" / "run server"

## Component Map
| Component | Launch | Port |
|-----------|--------|------|
| smartnpc-mcp | `task mcp:run` | `:3000` (HTTP), ws bridge to `:18745` |
| SMAPI Mod | User starts game via `StardewModdingAPI.exe` | `:18745` |
| Hermes Gateway (per NPC) | `hermes -p <npc> gateway run --accept-hooks` (WSL) | `:8642+` |

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

### Step 3 — Start MCP (HTTP mode, for Hermes and other remote clients)
```cmd
C:\Users\synchen\go\bin\task.exe mcp:run
```
Or manually:
```cmd
smartnpc-mcp\bin\smartnpc-mcp.exe --http :3000 ^
  --ws-url ws://127.0.0.1:18745/ws ^
  --hermes-config D:\SmartNPC\hermes\runtime-config.yaml ^
  --log-level debug
```
Verify: `curl http://127.0.0.1:3000/healthz` → `{"ok":true}`

### Step 4 — Start Hermes Gateways (WSL, per active NPC)
```bash
bash scripts/start_hermes_profiles.sh xiami,abigail
```

### Step 5 — Launch game
User launches Stardew Valley via `D:\Stardew Valley\StardewModdingAPI.exe`.
MCP auto-reconnects when ws becomes available.

> One-shot: `run.bat` in the repo root chains all of the above.

## Key Flags
| Flag | Meaning |
|------|---------|
| `--http :PORT` | HTTP transport on `/mcp` + `/healthz` |
| `--ws-url URL` | SMAPI mod WebSocket URL |
| `--ws-url=""` | Disable ws bridge |
| `--hermes-config PATH` | Multi-profile fan-out config |
| `--hermes-api-key KEY` | Bearer token for outbound POSTs |
| `--log-level` | debug/info/warn/error |

## WDAC Troubleshooting
If exe blocked by Device Guard:
1. Rebuild: `task mcp:build` (hash changes)
2. Test: `bin\smartnpc-mcp.exe --version`
3. If still blocked: use `go test -run TestRunServer` entry point
4. Last resort: contact IT

## Anti-patterns
- Don't suggest WSL for running mcp itself (Hermes runs in WSL, mcp runs on Windows)
- Don't use bare `smartnpc-mcp` (not in PATH)
- Don't start game with `Stardew Valley.exe` directly (must use `StardewModdingAPI.exe` for mods)

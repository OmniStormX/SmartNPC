# smartnpc-agent

NPC orchestrator for Stardew Valley. Spawns `smartnpc-mcp` as a stdio
subprocess, drives NPC personas through OpenAI, persists memories.

## Build

```powershell
go build -o smartnpc-agent.exe ./cmd/smartnpc-agent
```

## M1 Smoke Test

```powershell
# 1. Build the MCP server first
cd ..\smartnpc-mcp
go build -o smartnpc-mcp.exe .\cmd\smartnpc-mcp

# 2. From the agent dir, list MCP tools
cd ..\smartnpc-agent
go run ./cmd/smartnpc-agent --mcp-bin ..\smartnpc-mcp\smartnpc-mcp.exe tools

# 3. Call ping
go run ./cmd/smartnpc-agent --mcp-bin ..\smartnpc-mcp\smartnpc-mcp.exe ping --message "hi"
```

Expected ping output:

```json
{
  "ok": true,
  "echo": "hi",
  "serverNow": "2026-04-29T..."
}
```

## Layout

| Path                    | Purpose                                                |
|-------------------------|--------------------------------------------------------|
| `cmd/smartnpc-agent/`   | CLI entry, subcommands                                 |
| `internal/mcpclient/`   | Thin wrapper over the MCP Go SDK client                |
| `internal/llm/`         | LLM provider interface + OpenAI implementation (M4)    |
| `internal/log/`         | slog setup                                             |

Future packages: `persona/`, `memory/`, `agent/`, `orchestr/`, `scheduler/`.

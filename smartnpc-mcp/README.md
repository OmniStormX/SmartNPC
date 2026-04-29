# smartnpc-mcp

MCP server that exposes Stardew Valley NPC behaviors as MCP tools.

## Build

```powershell
go build -o smartnpc-mcp.exe ./cmd/smartnpc-mcp
```

## Run (stdio)

```powershell
.\smartnpc-mcp.exe --log-level=debug
```

The process speaks MCP over stdin/stdout. Logs go to stderr.

## Configure in an MCP client

Example for Claude Desktop (`%APPDATA%\Claude\claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "smartnpc": {
      "command": "D:\\SmartNPC\\smartnpc-mcp\\smartnpc-mcp.exe",
      "args": ["--log-level=info"]
    }
  }
}
```

## Tools (M1)

| Name   | Description                                      |
|--------|--------------------------------------------------|
| `ping` | Liveness check; echoes input + server timestamp. |

Future milestones add `npc_*`, `friendship_*`, etc.

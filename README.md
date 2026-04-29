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
| `docs/`            | Design docs, protocol spec, tool catalog                       |

## Quick Start (M1 skeleton)

```powershell
# 1. Build both Go binaries
cd smartnpc-mcp;   go build ./...
cd ..\smartnpc-agent; go build ./...

# 2. Smoke test: agent spawns mcp via stdio and calls the `ping` tool
cd ..\smartnpc-agent
go run ./cmd/smartnpc-agent --mcp-bin ..\smartnpc-mcp\smartnpc-mcp.exe ping
```

## Status

- [x] M1: Go workspace + stdio MCP server with `ping` tool + agent stdio client
- [ ] M2: SMAPI mod skeleton + WebSocket bridge protocol
- [ ] M3: NPC query / dialogue / movement tools (end-to-end)
- [ ] M4: Persona loader + OpenAI integration + single NPC agent loop
- [ ] M5: Memory (SQLite+FTS5), scheduler, multi-NPC orchestration

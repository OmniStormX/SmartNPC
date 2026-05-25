# Hermes Agent — Remote Deployment

Deploy all 6 NPC Hermes Gateways on a remote Linux server via Docker Compose.

## Architecture

```
[Player PC - Windows]
  Stardew Valley ── ws ── smartnpc-mcp (:3000, public)
                                │
                          Internet (HTTP)
                                │
[Remote Server - Linux]         ▼
  Docker Compose: 6× Hermes Gateway (:8642-8647)
    ↕ tool calls via MCP HTTP back to player's :3000/mcp
```

## Prerequisites

- Remote server: Docker + Docker Compose v2
- Player PC: `smartnpc-mcp` exposed on a public IP or domain (port 3000)

## Quick Start

```bash
# 1. Clone repo (or just copy deploy/hermes/)
git clone https://github.com/OmniStormX/SmartNPC.git
cd SmartNPC/deploy/hermes

# 2. Configure
cp .env.example .env
# Edit .env — see comments for each variable

# 3. Sync profile data from repo
bash scripts/sync-profiles.sh

# 4. Build & start
docker compose up -d --build

# 5. Verify
bash scripts/healthcheck.sh
# or: docker compose ps
```

## Player-Side Setup

On the Windows PC running the game + `smartnpc-mcp`:

1. Ensure `:3000` is reachable from the remote server (port forward / firewall rule)

2. Update `.env` in repo root:
   ```env
   # Point hermesrelay to the remote server's public IP
   SMARTNPC_HERMES_GATEWAY_HOST=<remote-server-ip>

   # (Optional) Enable MCP API key auth
   SMARTNPC_MCP_API_KEY=your-strong-random-key
   ```

3. Start mcp with the API key:
   ```cmd
   smartnpc-mcp\bin\smartnpc-mcp.exe ^
     --http :3000 ^
     --ws-url ws://127.0.0.1:18745/ws ^
     --hermes-config hermes\runtime-config.yaml ^
     --hermes-api-key %SMARTNPC_HERMES_KEY% ^
     --mcp-api-key %SMARTNPC_MCP_API_KEY% ^
     --log-level debug
   ```

## Configuration Reference

### Remote `.env` variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MCP_PUBLIC_HOST` | Yes | Public IP/domain of the player's mcp server |
| `MCP_PUBLIC_PORT` | No | mcp HTTP port (default: 3000) |
| `MCP_API_KEY` | Recommended | Bearer token for MCP auth (must match player's `--mcp-api-key`) |
| `SMARTNPC_HERMES_KEY` | Yes | Bearer token for hermesrelay → gateway auth |
| `HERMES_AGENT_URL` | Yes | LLM provider base URL |
| `HERMES_AGENT_API_KEY` | Yes | LLM provider API key |
| `HERMES_AGENT_MODEL` | No | Model name (default: deepseek-v4-flash) |
| `HERMES_LANGFUSE_*` | No | Langfuse observability (leave empty to disable) |

### Port Mapping

| NPC | Container Port | Host Port |
|-----|---------------|-----------|
| xiami | 8642 | 8642 |
| abigail | 8643 | 8643 |
| haley | 8644 | 8644 |
| harvey | 8645 | 8645 |
| penny | 8646 | 8646 |
| sebastian | 8647 | 8647 |

## Operations

```bash
# View logs for a specific NPC
docker compose logs -f hermes-xiami

# Restart a single NPC gateway
docker compose restart hermes-xiami

# Stop all
docker compose down

# Rebuild after profile changes
bash scripts/sync-profiles.sh
docker compose up -d --build

# Check health of all gateways
bash scripts/healthcheck.sh
```

## Security

- **mcp → Hermes**: Already uses `SMARTNPC_HERMES_KEY` as Bearer token
- **Hermes → mcp**: Use `--mcp-api-key` flag (added in this deployment)
- **Recommended**: Also restrict `:3000` via firewall to only the remote server's IP

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Gateway unhealthy | Container crashed or still starting | `docker compose logs hermes-<npc>` |
| Tool calls timeout | mcp :3000 unreachable from remote | Check firewall, verify with `curl http://<mcp-host>:3000/healthz` |
| 401 on tool calls | API key mismatch | Ensure `MCP_API_KEY` in remote `.env` matches `--mcp-api-key` on mcp |
| NPC doesn't respond | hermesrelay can't reach gateway | Verify remote server ports 8642-8647 are open, `SMARTNPC_HERMES_GATEWAY_HOST` is correct |

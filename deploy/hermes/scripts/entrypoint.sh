#!/usr/bin/env bash
set -euo pipefail

: "${NPC_PROFILE:?NPC_PROFILE env var is required (e.g. xiami)}"
: "${NPC_PORT:?NPC_PORT env var is required (e.g. 8642)}"

# If SMARTNPC_MCP_URL is set, inject it into the profile's config.yaml
# so Hermes knows where to find the MCP tool server.
if [ -n "${SMARTNPC_MCP_URL:-}" ]; then
    CONFIG="/root/.hermes/profiles/${NPC_PROFILE}/config.yaml"
    if [ -f "$CONFIG" ]; then
        # Replace the mcp_servers URL placeholder with the actual value
        sed -i "s|__HOST_IP__:3000/mcp|${SMARTNPC_MCP_URL#http://}|g" "$CONFIG"
        sed -i "s|http://__HOST_IP__:3000/mcp|${SMARTNPC_MCP_URL}|g" "$CONFIG"
        # Also handle ${SMARTNPC_MCP_URL} literal in config
        sed -i "s|\${SMARTNPC_MCP_URL}|${SMARTNPC_MCP_URL}|g" "$CONFIG"
    fi
fi

echo "Starting Hermes gateway for profile=${NPC_PROFILE} on port=${NPC_PORT}"
exec hermes -p "${NPC_PROFILE}" gateway run --accept-hooks

#!/usr/bin/env bash
set -euo pipefail

# Required env vars
: "${NPC_PROFILE:?NPC_PROFILE is required (e.g. xiami)}"
: "${NPC_PORT:?NPC_PORT is required (e.g. 8642)}"
: "${SMARTNPC_MCP_URL:?SMARTNPC_MCP_URL is required (e.g. http://your-ip:3000/mcp)}"
: "${HERMES_AGENT_URL:?HERMES_AGENT_URL is required (LLM provider base URL)}"
: "${HERMES_AGENT_API_KEY:?HERMES_AGENT_API_KEY is required}"
: "${SMARTNPC_HERMES_KEY:?SMARTNPC_HERMES_KEY is required (gateway Bearer token)}"

HERMES_AGENT_MODEL="${HERMES_AGENT_MODEL:-deepseek-v4-flash}"

# ── Set up profile directory ──────────────────────────────────────────────
PROFILE_DIR="/root/.hermes/profiles/${NPC_PROFILE}"
mkdir -p "$PROFILE_DIR"

# Copy baked profile data (SOUL.md, skills, cron) into Hermes profile location
SRC="/hermes/profiles/${NPC_PROFILE}"
if [ -d "$SRC" ]; then
    cp -r "$SRC"/* "$PROFILE_DIR"/ 2>/dev/null || true
fi

# ── Generate config.yaml from environment variables ───────────────────────
cat > "$PROFILE_DIR/config.yaml" <<EOF
mcp_servers:
  smartnpc_game:
    url: ${SMARTNPC_MCP_URL}
    timeout: 60
    connect_timeout: 10
    tools:
      include:
        - chat_say
        - friendship_get
        - game_get_time
        - game_get_weather
        - player_get_status
        - npc_get_position
        - npc_send_message
        - npc_inbox_get
        - npc_inbox_ack
        - npc_summon
        - npc_emote
        - npc_give_item
      resources: false
      prompts: false

API_SERVER_ENABLED: true
API_SERVER_KEY: ${SMARTNPC_HERMES_KEY}
API_SERVER_HOST: 0.0.0.0
API_SERVER_PORT: ${NPC_PORT}
API_SERVER_MODEL_NAME: ${NPC_PROFILE}

model:
  provider: custom:smartnpc-agent
  default: ${HERMES_AGENT_MODEL}
  base_url: ${HERMES_AGENT_URL}
  api_mode: chat_completions
  context_length: 64000

providers:
  smartnpc-agent:
    name: smartnpc-agent
    base_url: ${HERMES_AGENT_URL}
    key_env: HERMES_AGENT_API_KEY
    default_model: ${HERMES_AGENT_MODEL}
    api_mode: chat_completions
    discover_models: false

compression:
  enabled: true
  threshold: 0.15
  target_ratio: 0.10
  protect_last_n: 8
  hygiene_hard_message_limit: 60

agent:
  tool_use_enforcement: false
EOF

# ── Optional: Langfuse observability plugin ───────────────────────────────
if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ] && [ -n "${HERMES_LANGFUSE_SECRET_KEY:-}" ]; then
    cat >> "$PROFILE_DIR/config.yaml" <<EOF

plugins:
  enabled:
    - observability/langfuse
EOF
fi

# ── Generate .env for Hermes profile (LLM credentials) ───────────────────
cat > "$PROFILE_DIR/.env" <<EOF
HERMES_AGENT_URL=${HERMES_AGENT_URL}
HERMES_AGENT_API_KEY=${HERMES_AGENT_API_KEY}
HERMES_AGENT_MODEL=${HERMES_AGENT_MODEL}
EOF

if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ]; then
    cat >> "$PROFILE_DIR/.env" <<EOF
HERMES_LANGFUSE_PUBLIC_KEY=${HERMES_LANGFUSE_PUBLIC_KEY}
HERMES_LANGFUSE_SECRET_KEY=${HERMES_LANGFUSE_SECRET_KEY:-}
HERMES_LANGFUSE_BASE_URL=${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com}
EOF
fi

echo "=== SmartNPC Hermes Gateway ==="
echo "  Profile:  ${NPC_PROFILE}"
echo "  Port:     ${NPC_PORT}"
echo "  MCP URL:  ${SMARTNPC_MCP_URL}"
echo "  LLM:      ${HERMES_AGENT_URL} (model: ${HERMES_AGENT_MODEL})"
echo "==============================="

exec hermes -p "${NPC_PROFILE}" gateway run --accept-hooks

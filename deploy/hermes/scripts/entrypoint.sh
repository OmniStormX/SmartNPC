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
echo "[diag] Checking Langfuse credentials..."
if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ]; then
    echo "[diag]   HERMES_LANGFUSE_PUBLIC_KEY: ${HERMES_LANGFUSE_PUBLIC_KEY:0:10}... (${#HERMES_LANGFUSE_PUBLIC_KEY} chars)"
else
    echo "[diag]   HERMES_LANGFUSE_PUBLIC_KEY: NOT SET"
fi
if [ -n "${HERMES_LANGFUSE_SECRET_KEY:-}" ]; then
    echo "[diag]   HERMES_LANGFUSE_SECRET_KEY: ${HERMES_LANGFUSE_SECRET_KEY:0:10}... (${#HERMES_LANGFUSE_SECRET_KEY} chars)"
else
    echo "[diag]   HERMES_LANGFUSE_SECRET_KEY: NOT SET"
fi
echo "[diag]   HERMES_LANGFUSE_BASE_URL: ${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com (default)}"

if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ] && [ -n "${HERMES_LANGFUSE_SECRET_KEY:-}" ]; then
    echo "[diag] Langfuse credentials found — enabling observability/langfuse plugin"
    cat >> "$PROFILE_DIR/config.yaml" <<EOF

plugins:
  enabled:
    - observability/langfuse
EOF
else
    echo "[diag] WARNING: Langfuse credentials incomplete — plugin will NOT be enabled"
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

# ── Global .env for Hermes plugins (Langfuse reads from ~/.hermes/.env) ──
# The observability/langfuse plugin reads credentials from the global
# ~/.hermes/.env, NOT from the profile-level .env. Write them there too.
mkdir -p /root/.hermes
cat > /root/.hermes/.env <<EOF
HERMES_AGENT_URL=${HERMES_AGENT_URL}
HERMES_AGENT_API_KEY=${HERMES_AGENT_API_KEY}
HERMES_AGENT_MODEL=${HERMES_AGENT_MODEL}
EOF

if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ]; then
    cat >> /root/.hermes/.env <<EOF
HERMES_LANGFUSE_PUBLIC_KEY=${HERMES_LANGFUSE_PUBLIC_KEY}
HERMES_LANGFUSE_SECRET_KEY=${HERMES_LANGFUSE_SECRET_KEY:-}
HERMES_LANGFUSE_BASE_URL=${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com}
LANGFUSE_PUBLIC_KEY=${HERMES_LANGFUSE_PUBLIC_KEY}
LANGFUSE_SECRET_KEY=${HERMES_LANGFUSE_SECRET_KEY:-}
LANGFUSE_HOST=${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com}
EOF
    # Export standard Langfuse SDK env vars so the plugin picks them up
    export LANGFUSE_PUBLIC_KEY="${HERMES_LANGFUSE_PUBLIC_KEY}"
    export LANGFUSE_SECRET_KEY="${HERMES_LANGFUSE_SECRET_KEY:-}"
    export LANGFUSE_HOST="${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com}"
    export HERMES_LANGFUSE_DEBUG=true
fi

# ── Diagnostic: dump generated files ───────────────────────────────────────
echo ""
echo "[diag] Generated config.yaml:"
echo "---"
cat "$PROFILE_DIR/config.yaml"
echo "---"
echo ""
echo "[diag] Global /root/.hermes/.env:"
echo "---"
cat /root/.hermes/.env
echo "---"

# ── Diagnostic: verify Langfuse keys landed in global .env ─────────────────
if grep -q "HERMES_LANGFUSE_PUBLIC_KEY" /root/.hermes/.env 2>/dev/null; then
    echo "[diag] HERMES_LANGFUSE_PUBLIC_KEY present in /root/.hermes/.env"
else
    echo "[diag] ERROR: HERMES_LANGFUSE_PUBLIC_KEY MISSING from /root/.hermes/.env"
fi
if grep -q "HERMES_LANGFUSE_SECRET_KEY" /root/.hermes/.env 2>/dev/null; then
    echo "[diag] HERMES_LANGFUSE_SECRET_KEY present in /root/.hermes/.env"
else
    echo "[diag] ERROR: HERMES_LANGFUSE_SECRET_KEY MISSING from /root/.hermes/.env"
fi

echo "[diag] Standard Langfuse SDK env vars exported:"
echo "[diag]   LANGFUSE_PUBLIC_KEY=${LANGFUSE_PUBLIC_KEY:-NOT SET}"
echo "[diag]   LANGFUSE_SECRET_KEY=${LANGFUSE_SECRET_KEY:+set (${#LANGFUSE_SECRET_KEY} chars)}"
echo "[diag]   LANGFUSE_HOST=${LANGFUSE_HOST:-NOT SET}"

# ── Diagnostic: validate credentials against Langfuse API ──────────────────
if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ] && [ -n "${HERMES_LANGFUSE_SECRET_KEY:-}" ]; then
    LANGFUSE_URL="${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com}"
    echo ""
    echo "[diag] Validating Langfuse credentials (curl ${LANGFUSE_URL}/api/public/health)..."
    HTTP_CODE=$(curl -s -o /tmp/langfuse_resp.txt -w "%{http_code}" \
        -u "${HERMES_LANGFUSE_PUBLIC_KEY}:${HERMES_LANGFUSE_SECRET_KEY}" \
        "${LANGFUSE_URL}/api/public/health" 2>/dev/null || echo "000")
    if [ "$HTTP_CODE" = "200" ]; then
        echo "[diag] Langfuse API credentials VALID (HTTP $HTTP_CODE)"
    else
        echo "[diag] ERROR: Langfuse API returned HTTP $HTTP_CODE"
        echo "[diag] Response body: $(cat /tmp/langfuse_resp.txt 2>/dev/null || echo '(empty)')"
    fi
fi

echo ""
echo "=== SmartNPC Hermes Gateway ==="
echo "  Profile:  ${NPC_PROFILE}"
echo "  Port:     ${NPC_PORT}"
echo "  MCP URL:  ${SMARTNPC_MCP_URL}"
echo "  LLM:      ${HERMES_AGENT_URL} (model: ${HERMES_AGENT_MODEL})"
if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ] && [ -n "${HERMES_LANGFUSE_SECRET_KEY:-}" ]; then
    echo "  Langfuse: ${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com} (plugin enabled)"
else
    echo "  Langfuse: DISABLED (missing credentials)"
fi
echo "==============================="

# ── Explicitly register langfuse plugin via Hermes CLI ─────────────────────
# config.yaml `plugins.enabled` may not be sufficient; the CLI command
# writes to Hermes's internal plugin registry.
if [ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ] && [ -n "${HERMES_LANGFUSE_SECRET_KEY:-}" ]; then
    echo "[diag] Registering langfuse plugin via CLI..."
    hermes -p "${NPC_PROFILE}" plugins enable observability/langfuse 2>&1 || echo "[diag] WARNING: 'hermes plugins enable' failed (may not be supported in this version)"
    echo "[diag] Plugin list:"
    hermes -p "${NPC_PROFILE}" plugins list 2>&1 || echo "[diag] WARNING: 'hermes plugins list' failed"
fi

exec hermes -p "${NPC_PROFILE}" gateway run --accept-hooks

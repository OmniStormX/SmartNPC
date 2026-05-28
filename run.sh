#!/usr/bin/env bash
# SmartNPC Launcher (Linux / WSL) — equivalent to run.bat on Windows.
#
# Usage:
#   bash run.sh
#   ./run.sh  (after chmod +x)
#
# Prerequisites:
#   - task (go-task), Go 1.25+, .NET 6 SDK
#   - Docker + Docker Compose v2 (docker mode) OR hermes CLI (local mode)
#   - Stardew Valley + SMAPI installed (for mod:build / mod:install)
#   - .env in repo root (cp .env.example .env)

set -euo pipefail

# ── Locate repo root ─────────────────────────────────────────────────────────
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

# ── Load .env ────────────────────────────────────────────────────────────────
if [ -f .env ]; then
    set -a
    source .env
    set +a
    echo "[env] loaded .env"
fi

# ── Defaults ─────────────────────────────────────────────────────────────────
: "${SMARTNPC_HTTP_PORT:=3000}"
: "${SMARTNPC_WS_URL:=ws://127.0.0.1:18745/ws}"
: "${SMARTNPC_HERMES_KEY:=smartnpc-test-key}"
: "${SMARTNPC_ACTIVE_PROFILES:=xiami,abigail,haley,harvey,penny,sebastian}"
: "${HERMES_AGENT_MODEL:=deepseek-v4-flash}"
: "${SMARTNPC_HERMES_MODE:=docker}"
: "${HERMES_EXE:=hermes}"

# Detect host IP for Docker containers to reach mcp.
# In local mode: everything is 127.0.0.1 (same host).
# On WSL2: use the Windows host IP (default gateway).
# On native Linux: use docker0 bridge (172.17.0.1).
if [[ "$SMARTNPC_HERMES_MODE" == "local" ]]; then
    WIN_HOST_IP="127.0.0.1"
elif grep -qi microsoft /proc/version 2>/dev/null; then
    # WSL2
    WIN_HOST_IP="${WIN_HOST_IP:-$(ip route | grep default | awk '{print $3}')}"
else
    # Native Linux — Docker containers use docker0 bridge
    WIN_HOST_IP="${WIN_HOST_IP:-172.17.0.1}"
fi

echo "[cfg] REPO_ROOT            = $REPO_ROOT"
echo "[cfg] HERMES_MODE          = $SMARTNPC_HERMES_MODE"
echo "[cfg] WIN_HOST_IP          = $WIN_HOST_IP"
echo "[cfg] SMARTNPC_HTTP_PORT   = $SMARTNPC_HTTP_PORT"
echo "[cfg] SMARTNPC_WS_URL      = $SMARTNPC_WS_URL"
echo "[cfg] ACTIVE_PROFILES      = $SMARTNPC_ACTIVE_PROFILES"
echo ""

# ── Per-run log file ─────────────────────────────────────────────────────────
mkdir -p "$REPO_ROOT/logs"
MCP_TS="$(date +%Y%m%d_%H%M%S)"
MCP_LOG="$REPO_ROOT/logs/mcp_${MCP_TS}.log"

echo "============================================"
echo "  SmartNPC - One-Click Launcher (Linux)"
echo "============================================"
echo ""

# ── Step 1: Build ────────────────────────────────────────────────────────────
echo "[1/6] Building mod + mcp ..."
task mod:build || true  # mod build may fail on Linux without SDV
task mcp:build
echo "[OK] Build complete."
echo ""

# ── Step 2: Kill old processes ───────────────────────────────────────────────
echo "[2/6] Killing existing mcp processes (if any)..."
pkill -f "smartnpc-mcp" 2>/dev/null || true
sleep 1
echo "[OK] Old processes cleared."
echo ""

# ── Step 3: Install mod ─────────────────────────────────────────────────────
echo "[3/6] Installing mod..."
task mod:install || echo "[WARN] mod:install failed (expected on Linux without SDV)"
echo ""

# ── Step 4: Start mcp in HTTP mode ──────────────────────────────────────────
echo "[4/6] Starting smartnpc-mcp (--http :${SMARTNPC_HTTP_PORT})..."
echo "      log -> $MCP_LOG"

MCP_API_KEY_FLAG=""
if [ -n "${SMARTNPC_MCP_API_KEY:-}" ]; then
    MCP_API_KEY_FLAG="--mcp-api-key ${SMARTNPC_MCP_API_KEY}"
fi

"$REPO_ROOT/smartnpc-mcp/bin/smartnpc-mcp" \
    --http ":${SMARTNPC_HTTP_PORT}" \
    --ws-url "${SMARTNPC_WS_URL}" \
    --hermes-config "$REPO_ROOT/hermes/runtime-config.yaml" \
    --hermes-api-key "${SMARTNPC_HERMES_KEY}" \
    ${MCP_API_KEY_FLAG} \
    --log-level debug \
    > "$MCP_LOG" 2>&1 &

MCP_PID=$!
echo "      mcp PID: $MCP_PID"

# Wait for mcp to be healthy
echo "      Waiting for mcp HTTP endpoint..."
for i in $(seq 1 30); do
    if curl -sf "http://127.0.0.1:${SMARTNPC_HTTP_PORT}/healthz" >/dev/null 2>&1; then
        break
    fi
    sleep 2
done

if ! curl -sf "http://127.0.0.1:${SMARTNPC_HTTP_PORT}/healthz" >/dev/null 2>&1; then
    echo "[ERROR] mcp failed to start. Check $MCP_LOG"
    exit 1
fi
echo "[OK] mcp HTTP up at :${SMARTNPC_HTTP_PORT}/mcp."
echo ""

# ── Step 4.5: Render Hermes profiles from _master/ ───────────────────────────
echo "[4.5/6] Rendering Hermes profiles from _master/ ..."
bash "$REPO_ROOT/scripts/render_profiles.sh"
echo "[OK] Profiles rendered."
echo ""

# ── Step 5: Start Hermes Gateways ────────────────────────────────────────────
declare -A NPC_PORTS=(
    [xiami]=8642 [abigail]=8643 [haley]=8644
    [harvey]=8645 [penny]=8646 [sebastian]=8647
)
IFS=',' read -ra PROFILES <<< "$SMARTNPC_ACTIVE_PROFILES"

if [[ "$SMARTNPC_HERMES_MODE" == "local" ]]; then
    echo "[5/6] Starting Hermes Gateways (local mode, $HERMES_EXE)..."
    HERMES_PIDS=()
    for profile in "${PROFILES[@]}"; do
        port="${NPC_PORTS[$profile]:-}"
        if [ -z "$port" ]; then
            echo "      [WARN] unknown profile '$profile' — skip"
            continue
        fi
        echo "      starting $profile on :$port"
        "$HERMES_EXE" -p "$profile" gateway run --accept-hooks \
            > "$REPO_ROOT/logs/hermes_${profile}_${MCP_TS}.log" 2>&1 &
        HERMES_PIDS+=($!)
    done

    # Wait for gateways to become healthy
    echo "      Waiting for gateways to become healthy..."
    for profile in "${PROFILES[@]}"; do
        port="${NPC_PORTS[$profile]:-}"
        [ -z "$port" ] && continue
        for i in $(seq 1 18); do
            if curl -sf "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
                echo "      [OK] $profile on :$port"
                break
            fi
            sleep 5
        done
        if ! curl -sf "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
            echo "      [WARN] $profile on :$port not healthy after 90s"
        fi
    done
    echo "[OK] Gateways started (local)."
    echo ""
else
    echo "[5/6] Starting Hermes Gateways via Docker Compose ..."

    # Generate deploy/hermes/.env from root config
    cat > "$REPO_ROOT/deploy/hermes/.env" <<EOF
SMARTNPC_MCP_URL=http://${WIN_HOST_IP}:${SMARTNPC_HTTP_PORT}/mcp
SMARTNPC_HERMES_KEY=${SMARTNPC_HERMES_KEY}
HERMES_AGENT_URL=${HERMES_AGENT_URL:-}
HERMES_AGENT_API_KEY=${HERMES_AGENT_API_KEY:-}
HERMES_AGENT_MODEL=${HERMES_AGENT_MODEL}
HERMES_LANGFUSE_PUBLIC_KEY=${HERMES_LANGFUSE_PUBLIC_KEY:-}
HERMES_LANGFUSE_SECRET_KEY=${HERMES_LANGFUSE_SECRET_KEY:-}
HERMES_LANGFUSE_BASE_URL=${HERMES_LANGFUSE_BASE_URL:-https://cloud.langfuse.com}
EOF
    echo "      Generated deploy/hermes/.env (MCP_URL=http://${WIN_HOST_IP}:${SMARTNPC_HTTP_PORT}/mcp)"

    # Pull latest and start
    cd "$REPO_ROOT/deploy/hermes"
    if ! docker compose pull 2>&1 | tail -5; then
        echo "[WARN] docker compose pull failed, trying local build..."
        docker compose up -d --build 2>&1 | tail -10
    else
        docker compose up -d 2>&1 | tail -10
    fi
    cd "$REPO_ROOT"

    # Wait for gateways to become healthy
    echo "      Waiting for gateways to become healthy..."
    for profile in "${PROFILES[@]}"; do
        port="${NPC_PORTS[$profile]:-}"
        if [ -z "$port" ]; then
            echo "      [WARN] unknown profile '$profile' — skip"
            continue
        fi
        for i in $(seq 1 18); do
            if curl -sf "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
                echo "      [OK] $profile on :$port"
                break
            fi
            sleep 5
        done
        if ! curl -sf "http://127.0.0.1:${port}/health" >/dev/null 2>&1; then
            echo "      [WARN] $profile on :$port not healthy after 90s"
        fi
    done
    echo "[OK] Gateways started (docker)."
    echo ""
fi

# ── Step 6: Launch the game ──────────────────────────────────────────────────
echo "[6/6] Launching Stardew Valley via SMAPI..."
GAME_PATH="${SMARTNPC_GAME_PATH:-}"
if [ -n "$GAME_PATH" ] && [ -f "$GAME_PATH/StardewModdingAPI" ]; then
    "$GAME_PATH/StardewModdingAPI" &
    echo "[OK] Game launching."
else
    echo "[INFO] SMARTNPC_GAME_PATH not set or SMAPI not found. Start the game manually."
fi
echo ""

echo "==========================="
echo "  Active NPCs: $SMARTNPC_ACTIVE_PROFILES"
echo "  Mode: $SMARTNPC_HERMES_MODE"
echo ""
echo "  mcp log:  $MCP_LOG"
echo "  mcp PID:  $MCP_PID"
if [[ "$SMARTNPC_HERMES_MODE" == "local" ]]; then
    echo "  hermes logs: logs/hermes_*_${MCP_TS}.log"
    echo "  stop all: kill $MCP_PID ${HERMES_PIDS[*]:-}"
else
    echo "  docker:   docker compose -f deploy/hermes/docker-compose.yml ps"
    echo "  stop all: kill $MCP_PID && docker compose -f deploy/hermes/docker-compose.yml down"
fi
echo "==========================="

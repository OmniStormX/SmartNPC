#!/usr/bin/env bash
# Start one or more Hermes Gateway processes in the background and wait for
# each /health endpoint to come up.
#
# Usage:   bash scripts/start_hermes_profiles.sh xiami,abigail
# Env:     HERMES_BOOT_TIMEOUT (seconds per profile, default 90)
#          HERMES_PIDFILE (default /tmp/smartnpc-hermes-pids.txt)
#
# Profile metadata is loaded from hermes/npcs.yaml.
#
# Exit codes:
#   0  every requested profile is healthy
#   1  one or more profiles failed to come up before HERMES_BOOT_TIMEOUT

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REGISTRY="$REPO_ROOT/hermes/npcs.yaml"

# shellcheck source=scripts/lib/npc_registry.sh
source "$REPO_ROOT/scripts/lib/npc_registry.sh"

if [[ $# -lt 1 ]]; then
    echo "usage: $0 profile1[,profile2,...]" >&2
    exit 2
fi

smartnpc_load_registry "$REGISTRY"

TIMEOUT="${HERMES_BOOT_TIMEOUT:-90}"
PIDFILE="${HERMES_PIDFILE:-/tmp/smartnpc-hermes-pids.txt}"
: > "$PIDFILE"

IFS=',' read -ra PROFILES <<<"$1"
failed=()

for profile in "${PROFILES[@]}"; do
    profile="${profile//[[:space:]]/}"
    [ -z "$profile" ] && continue

    port="${SMARTNPC_NPC_GATEWAY_PORT[$profile]:-}"
    if [[ -z "$port" || "${SMARTNPC_NPC_ENABLED[$profile]:-true}" != "true" ]]; then
        echo "[start_hermes_profiles] unknown or disabled profile: $profile" >&2
        failed+=("$profile")
        continue
    fi

    # Stop any old process bound to this port (best-effort).
    if pid=$(lsof -ti :"$port" 2>/dev/null); then
        echo "[start_hermes_profiles] killing stale pid $pid on :$port"
        kill -9 "$pid" 2>/dev/null || true
        sleep 0.5
    fi

    echo "[start_hermes_profiles] starting $profile on :$port"
    # Detach into background. Logs go to ~/.hermes/profiles/<name>/logs/
    # (hermes handles that itself); stderr is discarded here.
    nohup hermes -p "$profile" gateway run --accept-hooks \
        > "/tmp/hermes-${profile}.log" 2>&1 &
    pid=$!
    echo "$profile $pid $port" >> "$PIDFILE"

    # Wait for health.
    deadline=$(( $(date +%s) + TIMEOUT ))
    healthy=0
    while (( $(date +%s) < deadline )); do
        if curl -sS "http://127.0.0.1:$port/health" >/dev/null 2>&1; then
            healthy=1
            break
        fi
        sleep 1
    done

    if (( healthy == 0 )); then
        echo "[start_hermes_profiles] $profile failed to become healthy" >&2
        failed+=("$profile")
    else
        echo "[start_hermes_profiles] $profile healthy on :$port (pid $pid)"
    fi
done

if (( ${#failed[@]} > 0 )); then
    echo "[start_hermes_profiles] FAILED profiles: ${failed[*]}" >&2
    exit 1
fi

echo "[start_hermes_profiles] all healthy. PIDs recorded in $PIDFILE"

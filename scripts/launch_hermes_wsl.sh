#!/bin/bash
# Hermes gateway launcher for WSL-native mode.
# Called by run-wsl.bat. Starts all profiles in background with setsid.
# Args: space-separated profile names (e.g. "xiami abigail")
set -u

export HOME=/home/synchen
export PATH='/home/synchen/.local/bin:/usr/local/bin:/usr/bin:/bin'

# Langfuse Cloud credentials.
# Inherit from the parent shell. Do NOT hard-code keys in this script —
# put them in ~/.hermes/.env or export them before calling this launcher.
: "${HERMES_LANGFUSE_BASE_URL:=https://cloud.langfuse.com}"
export HERMES_LANGFUSE_BASE_URL
[ -n "${HERMES_LANGFUSE_PUBLIC_KEY:-}" ] && export HERMES_LANGFUSE_PUBLIC_KEY
[ -n "${HERMES_LANGFUSE_SECRET_KEY:-}" ] && export HERMES_LANGFUSE_SECRET_KEY

PROFILES="${*:-xiami abigail}"

# Kill any leftover gateways
pkill -f 'hermes.*gateway run' 2>/dev/null
sleep 1

for p in $PROFILES; do
    mkdir -p "/home/synchen/.hermes/profiles/$p/logs"
    setsid hermes -p "$p" gateway run --accept-hooks \
        > "/home/synchen/.hermes/profiles/$p/logs/gateway.log" 2>&1 &
    echo "      launched $p (pid=$!)"
done

# Give processes a moment to fork
sleep 2

#!/bin/bash
# Hermes gateway launcher for WSL-native mode.
# Called by run-wsl.bat. Starts each profile in background with setsid,
# injecting that profile's own .env (so per-NPC Langfuse keys work).
# Args: space-separated profile names (e.g. "xiami abigail")
set -u

export HOME=/home/synchen
export PATH='/home/synchen/.local/bin:/usr/local/bin:/usr/bin:/bin'

PROFILES="${*:-xiami abigail}"

# Kill any leftover gateways (best-effort; tolerate `pkill` returning 1
# when nothing matched).
pkill -f 'hermes.*gateway run' 2>/dev/null || true
sleep 1

for p in $PROFILES; do
    profile_dir="$HOME/.hermes/profiles/$p"
    profile_env="$profile_dir/.env"
    log_dir="$profile_dir/logs"
    mkdir -p "$log_dir"

    if [[ ! -f "$profile_env" ]]; then
        echo "      WARN: $profile_env missing — Langfuse trace will be silent for $p"
    fi

    # Spawn each gateway in a subshell so the env injection is scoped to
    # that one profile. set -a auto-exports every variable assigned during
    # `source`, which is exactly what the langfuse plugin needs (it reads
    # os.environ.get('HERMES_LANGFUSE_*'), not the profile config file).
    # setsid detaches the child from this shell's session so it survives
    # the bash -c invocation that run-wsl.bat uses.
    (
        set -a
        # shellcheck disable=SC1090
        [[ -f "$profile_env" ]] && source "$profile_env"
        set +a
        setsid hermes -p "$p" gateway run --accept-hooks \
            > "$log_dir/gateway.log" 2>&1 < /dev/null &
        echo "      launched $p (pid=$!)"
    )
done

# Give processes a moment to fork before run-wsl.bat starts polling /health.
sleep 2

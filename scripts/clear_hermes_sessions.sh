#!/usr/bin/env bash
# clear_hermes_sessions.sh — Clear all Hermes NPC sessions and response chains.
# Usage: wsl -d Ubuntu-22.04 bash -l /mnt/d/SmartNPC/scripts/clear_hermes_sessions.sh
#
# This script:
#   1. Removes response_store.db (conversation chains — main source of bloat)
#   2. Removes state.db (session history + FTS indexes)
#   3. Clears the main (default) profile as well
#   4. Hermes will auto-recreate empty databases on next startup
#
# Safe to run while the gateway is stopped. If the gateway is running, restart
# it after clearing so it picks up the empty state.

set -euo pipefail

HERMES_DIR="${HERMES_DIR:-$HOME/.hermes}"
PROFILES=("abigail" "haley" "harvey" "penny" "sebastian" "xiami")

clear_profile() {
    local profile="$1"
    local dir="$HERMES_DIR/profiles/$profile"

    if [[ ! -d "$dir" ]]; then
        echo "  SKIP $profile (no profile directory)"
        return
    fi

    # Remove response store (conversation chains)
    rm -f "$dir/response_store.db" "$dir/response_store.db-wal" "$dir/response_store.db-shm"

    # Remove session store (message history + FTS indexes — can be huge)
    rm -f "$dir/state.db" "$dir/state.db-wal" "$dir/state.db-shm"

    echo "  OK   $profile"
}

echo "Clearing Hermes NPC sessions..."
echo ""

for p in "${PROFILES[@]}"; do
    clear_profile "$p"
done

# Also clear the main/default profile (used by the shared gateway on port 8642)
echo ""
echo "Clearing main profile..."
rm -f "$HERMES_DIR/response_store.db" "$HERMES_DIR/response_store.db-wal" "$HERMES_DIR/response_store.db-shm"
rm -f "$HERMES_DIR/state.db" "$HERMES_DIR/state.db-wal" "$HERMES_DIR/state.db-shm"
echo "  OK   main"

echo ""
echo "All sessions cleared. Restart the gateway:"
echo "  hermes gateway run --replace"

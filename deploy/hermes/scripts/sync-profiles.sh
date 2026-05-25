#!/usr/bin/env bash
# sync-profiles.sh — Render and sync Hermes profile data from the repo
# into deploy/hermes/profiles/ for Docker image building.
#
# Run this from the repo root or from deploy/hermes/:
#   bash scripts/sync-profiles.sh
#   bash deploy/hermes/scripts/sync-profiles.sh  (from repo root)

set -euo pipefail

# Resolve paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/../.." && pwd)"

HERMES_DIR="$REPO_ROOT/hermes"
PROFILES_SRC="$HERMES_DIR/profiles"
PROFILES_DST="$DEPLOY_DIR/profiles"

NPCS_YAML="$HERMES_DIR/npcs.yaml"

if [ ! -f "$NPCS_YAML" ]; then
    echo "ERROR: $NPCS_YAML not found. Run from repo root." >&2
    exit 1
fi

# Parse NPC IDs from npcs.yaml (simple grep, no yq dependency)
NPC_IDS=$(grep '^\s*- id:' "$NPCS_YAML" | sed 's/.*id:\s*//' | tr -d ' ')

echo "Syncing profiles to $PROFILES_DST"
echo "NPCs: $NPC_IDS"

# First, run the profile render script if it exists
RENDER_SCRIPT="$REPO_ROOT/scripts/render_profiles.sh"
if [ -f "$RENDER_SCRIPT" ]; then
    echo "Running render_profiles.sh..."
    bash "$RENDER_SCRIPT"
fi

# Clean and recreate destination
rm -rf "$PROFILES_DST"
mkdir -p "$PROFILES_DST"

for npc in $NPC_IDS; do
    SRC="$PROFILES_SRC/$npc"
    DST="$PROFILES_DST/$npc"

    if [ ! -d "$SRC" ]; then
        echo "WARN: profile source $SRC does not exist, skipping $npc" >&2
        continue
    fi

    echo "  Syncing $npc..."
    mkdir -p "$DST"

    # Copy SOUL.md (hand-written, per-NPC identity)
    [ -f "$SRC/SOUL.md" ] && cp "$SRC/SOUL.md" "$DST/"

    # Copy .env (LLM credentials — will be overridden by docker env vars)
    [ -f "$SRC/.env" ] && cp "$SRC/.env" "$DST/"

    # Copy config-overlay.yaml → config.yaml (the overlay IS the config for Docker)
    if [ -f "$SRC/config-overlay.yaml" ]; then
        cp "$SRC/config-overlay.yaml" "$DST/config.yaml"
    fi

    # Copy skills directory
    if [ -d "$SRC/skills" ]; then
        cp -r "$SRC/skills" "$DST/"
    fi

    # Copy cron recipes
    [ -f "$SRC/cron-recipes.md" ] && cp "$SRC/cron-recipes.md" "$DST/"
done

echo "Done. Profile count: $(echo "$NPC_IDS" | wc -w)"
echo "Ready for: docker compose build"

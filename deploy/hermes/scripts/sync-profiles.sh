#!/usr/bin/env bash
# sync-profiles.sh — Prepare profile data for Docker image build.
#
# Copies SOUL.md, skills, cron-recipes from the repo's hermes/profiles/ into
# deploy/hermes/profiles/ so the Dockerfile can COPY them into the image.
#
# Run this ONCE before the first `docker compose build`, or after editing
# SOUL.md / skills. You do NOT need to run it for config changes — those
# are generated at runtime from .env by entrypoint.sh.
#
# Usage (from repo root):
#   bash deploy/hermes/scripts/sync-profiles.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$DEPLOY_DIR/../.." && pwd)"

PROFILES_SRC="$REPO_ROOT/hermes/profiles"
PROFILES_DST="$DEPLOY_DIR/profiles"
NPCS_YAML="$REPO_ROOT/hermes/npcs.yaml"

if [ ! -f "$NPCS_YAML" ]; then
    echo "ERROR: $NPCS_YAML not found. Run from repo root." >&2
    exit 1
fi

# Parse NPC IDs
NPC_IDS=$(grep '^\s*- id:' "$NPCS_YAML" | sed 's/.*id:\s*//' | tr -d ' ')

echo "Syncing profile data → $PROFILES_DST"

# Run render script if available (ensures profiles are up to date)
RENDER_SCRIPT="$REPO_ROOT/scripts/render_profiles.sh"
if [ -f "$RENDER_SCRIPT" ]; then
    echo "Running render_profiles.sh..."
    bash "$RENDER_SCRIPT"
fi

rm -rf "$PROFILES_DST"
mkdir -p "$PROFILES_DST"

for npc in $NPC_IDS; do
    SRC="$PROFILES_SRC/$npc"
    DST="$PROFILES_DST/$npc"

    if [ ! -d "$SRC" ]; then
        echo "  SKIP: $npc (no source directory)"
        continue
    fi

    mkdir -p "$DST"

    # Only copy content files (not config — that's generated at runtime)
    [ -f "$SRC/SOUL.md" ] && cp "$SRC/SOUL.md" "$DST/"
    [ -d "$SRC/skills" ] && cp -r "$SRC/skills" "$DST/"
    [ -f "$SRC/cron-recipes.md" ] && cp "$SRC/cron-recipes.md" "$DST/"

    echo "  OK: $npc"
done

echo ""
echo "Done. Now run: docker compose build"

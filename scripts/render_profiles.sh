#!/usr/bin/env bash
# Render per-NPC Hermes profile files from hermes/profiles/_master/ templates.
#
# Scope: renders everything EXCEPT SOUL.md. The six SOUL.md files are hand-
# written per-NPC personas and must never be touched by this script.
#
# Placeholders (replaced by GNU sed):
#   {{NPC_NAME}}       PascalCase internal name (XiaMi, Abigail, ...)
#   {{NPC_DISPLAY}}    Chinese display name (夏弥, 阿比盖尔, ...)
#   {{NPC_DIR}}        lower-case profile directory name (xiami, abigail, ...)
#   {{NPC_PORT}}       Hermes Gateway API_SERVER_PORT (per runtime-config.yaml)
#   {{PEER_A_NAME}}    First peer, PascalCase
#   {{PEER_A_DISPLAY}} First peer, Chinese display name
#   {{PEER_B_NAME}}    Second peer, PascalCase
#   {{PEER_B_DISPLAY}} Second peer, Chinese display name
#
# Usage (from repo root):
#   bash scripts/render_profiles.sh
#
# Windows note: run inside WSL, e.g.
#   wsl -d Ubuntu-22.04 bash -c "cd /mnt/d/SmartNPC && bash scripts/render_profiles.sh"
#
# This script is idempotent: re-running produces the same output tree. The
# canonical sanity check is that `git diff hermes/profiles/xiami/` is empty
# after a render, because xiami is itself rendered from _master/.
#
# Do NOT edit rendered per-NPC files (skills/, config-overlay.yaml,
# cron-recipes.md) directly — they are regenerated on every render. Edit
# hermes/profiles/_master/ instead, then re-run this script.
#
# See ADR-0003 (docs/adr/0003-npc-name-placeholder-cloning.md) and
# docs/architecture.md § Profile cloning mechanism for the design rationale.

set -euo pipefail

# Resolve repo root relative to this script so it works regardless of cwd.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MASTER="$REPO_ROOT/hermes/profiles/_master"

if [ ! -d "$MASTER" ]; then
  echo "error: master template directory not found: $MASTER" >&2
  exit 1
fi

# NPC table: DIR NAME DISPLAY PORT PEER_A_NAME PEER_A_DISPLAY PEER_B_NAME PEER_B_DISPLAY
# Ports mirror hermes/runtime-config.yaml. Keep in sync when editing either.
TABLE='xiami XiaMi 夏弥 8642 Penny 潘妮 Abigail 阿比盖尔
abigail Abigail 阿比盖尔 8643 Penny 潘妮 Sebastian 塞巴斯蒂安
haley Haley 黑利 8644 Penny 潘妮 Abigail 阿比盖尔
harvey Harvey 哈维 8645 Penny 潘妮 Abigail 阿比盖尔
penny Penny 潘妮 8646 Abigail 阿比盖尔 Haley 黑利
sebastian Sebastian 塞巴斯蒂安 8647 Abigail 阿比盖尔 Penny 潘妮'

while read -r DIR NAME DISP PORT PAN PAD PBN PBD; do
  [ -z "$DIR" ] && continue
  TARGET="$REPO_ROOT/hermes/profiles/$DIR"
  mkdir -p "$TARGET/skills"

  # Copy skills tree (mirror _master exactly — excludes SOUL.md which never
  # lives in _master). We wipe the target first so stale skills removed from
  # _master don't linger. Using cp -r (works in Git Bash, WSL, and Linux;
  # rsync may be unavailable on minimal Windows bash installs).
  rm -rf "$TARGET/skills"
  cp -r "$MASTER/skills" "$TARGET/skills"

  # Copy scalar templates. Do NOT touch SOUL.md.
  cp "$MASTER/config-overlay.yaml" "$TARGET/config-overlay.yaml"
  cp "$MASTER/cron-recipes.md" "$TARGET/cron-recipes.md"

  # Gather every rendered file for this NPC. Using \( ... \) so -o binds
  # correctly; -print terminates so xargs sees a single list.
  mapfile -t FILES < <(find "$TARGET/skills" "$TARGET/config-overlay.yaml" "$TARGET/cron-recipes.md" \
    -type f \( -name '*.md' -o -name '*.yaml' \) -print)

  # Use | as sed delimiter (paths and values can contain /).
  for f in "${FILES[@]}"; do
    sed -i \
      -e "s|{{NPC_NAME}}|$NAME|g" \
      -e "s|{{NPC_DISPLAY}}|$DISP|g" \
      -e "s|{{NPC_DIR}}|$DIR|g" \
      -e "s|{{NPC_PORT}}|$PORT|g" \
      -e "s|{{PEER_A_NAME}}|$PAN|g" \
      -e "s|{{PEER_A_DISPLAY}}|$PAD|g" \
      -e "s|{{PEER_B_NAME}}|$PBN|g" \
      -e "s|{{PEER_B_DISPLAY}}|$PBD|g" \
      "$f"
  done

  echo "rendered: $DIR ($NAME / $DISP, port $PORT, peers $PAN + $PBN)"
done <<< "$TABLE"

echo ""
echo "done. Sanity check: 'git diff hermes/profiles/xiami/' should be empty."

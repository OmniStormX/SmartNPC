#!/usr/bin/env bash
# Sync the xiami (and any other) Hermes profile from this repo into the
# user's ~/.hermes/profiles/<name>/ directory inside WSL.
#
# What gets copied:
#   - SOUL.md
#   - skills/           (merged, not replaced — Hermes's built-in skills are preserved)
#
# What does NOT get copied automatically:
#   - mcp-servers.yaml block → you must paste it into ~/.hermes/profiles/<name>/config.yaml
#     (the script prints the block + the target file at the end)
#
# Usage (from inside WSL, with this repo mounted at /mnt/d/SmartNPC):
#
#   bash /mnt/d/SmartNPC/hermes/install.sh
#
# Or from Windows:
#
#   wsl -d Ubuntu-22.04 bash /mnt/d/SmartNPC/hermes/install.sh
#
# Set HERMES_HOME (default: $HOME/.hermes) to target a non-standard install.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_PROFILES="$SCRIPT_DIR/profiles"
HERMES_HOME="${HERMES_HOME:-$HOME/.hermes}"
TARGET_BASE="$HERMES_HOME/profiles"

if [[ ! -d "$REPO_PROFILES" ]]; then
    echo "error: $REPO_PROFILES does not exist" >&2
    exit 1
fi

if [[ ! -d "$HERMES_HOME" ]]; then
    echo "warning: $HERMES_HOME does not exist. Hermes creates it on first run."
    echo "Try: hermes -p xiami run  # once, to bootstrap the profile, then re-run this script."
    exit 1
fi

mkdir -p "$TARGET_BASE"

declare -a synced=()
declare -a pending_merge=()

have_yq=0
if command -v yq >/dev/null 2>&1; then
    have_yq=1
fi

# Detect the WSL default-gateway IP, which resolves back to the Windows host
# from inside WSL. Works regardless of mirrored-mode networking as long as
# smartnpc-mcp is bound to 0.0.0.0:3000 on Windows.
HOST_IP="$(ip route 2>/dev/null | awk '/default/ { print $3; exit }')"
if [[ -z "$HOST_IP" ]]; then
    HOST_IP="127.0.0.1"
    echo "warning: could not detect WSL default gateway — falling back to $HOST_IP"
else
    echo "[detect] WSL→Windows host IP = $HOST_IP"
fi

for profile_dir in "$REPO_PROFILES"/*/; do
    profile="$(basename "$profile_dir")"
    target="$TARGET_BASE/$profile"
    mkdir -p "$target"

    # Copy SOUL.md
    if [[ -f "$profile_dir/SOUL.md" ]]; then
        cp "$profile_dir/SOUL.md" "$target/SOUL.md"
        echo "[sync] $profile/SOUL.md"
    fi

    # Merge skills/ (recursive copy; preserves existing Hermes skills)
    if [[ -d "$profile_dir/skills" ]]; then
        mkdir -p "$target/skills"
        cp -r "$profile_dir/skills/"* "$target/skills/"
        echo "[sync] $profile/skills/ (merged)"
    fi

    # Merge config-overlay.yaml into config.yaml
    overlay="$profile_dir/config-overlay.yaml"
    # Back-compat: accept the older mcp-servers.yaml filename too.
    if [[ ! -f "$overlay" && -f "$profile_dir/mcp-servers.yaml" ]]; then
        overlay="$profile_dir/mcp-servers.yaml"
    fi
    if [[ -f "$overlay" ]]; then
        target_cfg="$target/config.yaml"
        # Render the block with HOST_IP substituted.
        rendered="$(sed "s|__HOST_IP__|$HOST_IP|g" "$overlay")"

        if [[ ! -f "$target_cfg" ]]; then
            pending_merge+=("$profile:needs_bootstrap")
        elif grep -q "^mcp_servers:" "$target_cfg" || grep -q "^API_SERVER_ENABLED:" "$target_cfg"; then
            # Existing overlay-ish content — don't clobber; let the operator decide.
            pending_merge+=("$profile:already_present")
        else
            # Append the rendered block.
            {
                echo
                echo "# ── injected by hermes/install.sh (smartnpc profile) ──"
                echo "$rendered"
            } >> "$target_cfg"
            echo "[merge] $profile/config.yaml overlay appended (HOST_IP=$HOST_IP)"
        fi
    fi

    synced+=("$profile")
done

echo
echo "synced profiles: ${synced[*]:-none}"

if [[ ${#pending_merge[@]} -gt 0 ]]; then
    echo
    echo "===== pending manual steps ====="
    for entry in "${pending_merge[@]}"; do
        profile="${entry%%:*}"
        reason="${entry#*:}"
        case "$reason" in
            needs_bootstrap)
                echo "  - $profile: config.yaml does not exist yet."
                echo "      Run once in WSL: hermes -p $profile run"
                echo "      Then re-run this script."
                ;;
            already_present)
                target_cfg="$TARGET_BASE/$profile/config.yaml"
                echo "  - $profile: config.yaml already has mcp_servers: or API_SERVER_*."
                echo "      Review and manually merge the blocks from"
                echo "      $REPO_PROFILES/$profile/config-overlay.yaml"
                echo "      into $target_cfg (substituting __HOST_IP__ → $HOST_IP)."
                ;;
        esac
    done
fi

echo
echo "done."

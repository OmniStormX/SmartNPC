#!/usr/bin/env bash
# Sync the xiami (and any other) Hermes profile from this repo into the
# user's ~/.hermes/profiles/<name>/ directory inside WSL.
#
# What gets synced:
#   - SOUL.md
#   - .env
#   - skills/           (merged, not replaced — Hermes's built-in skills are preserved)
#   - critical-policy.md
#   - config-overlay.yaml merged into ~/.hermes/profiles/<name>/config.yaml
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
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
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

# Materialize generated profile files from _master before syncing. This keeps
# ignored rendered skill copies current in clean clones.
bash "$REPO_ROOT/scripts/render_profiles.sh"

declare -a synced=()
declare -a pending_merge=()

PY=""
for candidate in "$HERMES_HOME/hermes-agent/venv/bin/python" "$HERMES_HOME/bin/python" python3 python; do
    if command -v "$candidate" >/dev/null 2>&1 && "$candidate" -c 'import yaml' >/dev/null 2>&1; then
        PY="$candidate"
        break
    fi
done
if [[ -z "$PY" ]]; then
    echo "error: no Python with PyYAML found (tried Hermes venv + system python)" >&2
    echo "       install PyYAML or run Hermes setup first." >&2
    exit 1
fi

# Legacy: previously detected WSL default-gateway IP to substitute __HOST_IP__
# in config-overlay.yaml. Now the overlay uses ${SMARTNPC_MCP_URL} which Hermes
# expands at runtime from the profile's .env. The sed below is kept as a no-op
# safety net for any leftover __HOST_IP__ references.
HOST_IP="$(ip route 2>/dev/null | awk '/default/ && host == "" { host = $3 } END { print host }')"
if [[ -z "$HOST_IP" ]]; then
    HOST_IP="127.0.0.1"
fi
echo "[detect] host IP = $HOST_IP (used only if __HOST_IP__ placeholder remains in overlay)"

# ── Repo-root .env override for HERMES_AGENT_* ─────────────────────────────
# The shared LLM endpoint / key / model live in the repo-root .env (the
# single source of truth). Per-NPC .env files keep their own copies for
# convenience but those have repeatedly drifted to placeholder values
# after various render / write operations. We therefore re-inject the
# repo-root values over the per-NPC files at sync time so a
# `task net:check` failure can't be caused by a stale `sk-REPLACE_ME`
# anymore.
ROOT_ENV="$(cd "$SCRIPT_DIR/.." && pwd)/.env"
declare -a ROOT_ENV_OVERRIDES=()
if [[ -f "$ROOT_ENV" ]]; then
    for key in HERMES_AGENT_URL HERMES_AGENT_API_KEY HERMES_AGENT_MODEL; do
        # Read raw line; strip trailing CR (Windows-edited .env files have
        # CRLF endings, which would otherwise corrupt URLs and key values
        # written to per-NPC .env files inside WSL).
        line="$(grep -E "^[[:space:]]*(export[[:space:]]+)?${key}=" "$ROOT_ENV" | tail -n1 | tr -d '\r' || true)"
        if [[ -n "$line" ]]; then
            value="$(printf '%s' "$line" | sed -E "s/^[[:space:]]*(export[[:space:]]+)?${key}=//; s/^['\"]//; s/['\"]\$//")"
            ROOT_ENV_OVERRIDES+=("$key=$value")
        fi
    done
    if [[ ${#ROOT_ENV_OVERRIDES[@]} -gt 0 ]]; then
        echo "[detect] repo-root .env will override ${#ROOT_ENV_OVERRIDES[@]} per-NPC HERMES_AGENT_* line(s)"
    fi
else
    echo "[detect] no repo-root .env; per-NPC values will be used as-is"
fi

# Apply ROOT_ENV_OVERRIDES to a target .env in place. Each override is
# a `KEY=VALUE` string. If KEY exists, replace the entire line; if not,
# append. Uses awk so values may safely contain sed metacharacters.
apply_root_env_overrides() {
    local file="$1"
    [[ ${#ROOT_ENV_OVERRIDES[@]} -eq 0 ]] && return 0
    [[ -f "$file" ]] || return 0
    local tmp; tmp="$(mktemp)"
    awk -v overrides="$(printf '%s\n' "${ROOT_ENV_OVERRIDES[@]}")" '
        BEGIN {
            n = split(overrides, lines, "\n")
            for (i = 1; i <= n; i++) {
                if (length(lines[i]) == 0) continue
                eq = index(lines[i], "=")
                k = substr(lines[i], 1, eq - 1)
                v = substr(lines[i], eq + 1)
                kv[k] = v
                seen[k] = 0
            }
        }
        {
            line = $0
            stripped = line
            sub(/^[[:space:]]*(export[[:space:]]+)?/, "", stripped)
            eq = index(stripped, "=")
            if (eq > 0) {
                k = substr(stripped, 1, eq - 1)
                if (k in kv) {
                    print k "=" kv[k]
                    seen[k] = 1
                    next
                }
            }
            print line
        }
        END {
            for (k in kv) {
                if (!seen[k]) print k "=" kv[k]
            }
        }
    ' "$file" > "$tmp"
    mv "$tmp" "$file"
}

for profile_dir in "$REPO_PROFILES"/*/; do
    profile="$(basename "$profile_dir")"
    if [[ "$profile" == "_master" ]]; then
        continue
    fi
    target="$TARGET_BASE/$profile"
    mkdir -p "$target"

    # Copy SOUL.md
    if [[ -f "$profile_dir/SOUL.md" ]]; then
        cp "$profile_dir/SOUL.md" "$target/SOUL.md"
        echo "[sync] $profile/SOUL.md"
    fi

    # Copy .env (LLM + Langfuse credentials), then apply repo-root .env
    # overrides for HERMES_AGENT_* (LLM endpoint / key / model). Per-NPC
    # values for everything else (Langfuse keys, MCP URL, etc.) are kept
    # untouched.
    if [[ -f "$profile_dir/.env" ]]; then
        cp "$profile_dir/.env" "$target/.env"
        apply_root_env_overrides "$target/.env"
        echo "[sync] $profile/.env (with repo-root HERMES_AGENT_* overrides)"
    fi

    # Merge skills/ (recursive copy; preserves existing Hermes skills)
    if [[ -d "$profile_dir/skills" ]]; then
        mkdir -p "$target/skills"
        cp -r "$profile_dir/skills/"* "$target/skills/"
        echo "[sync] $profile/skills/ (merged)"
    fi

    # Copy always-loaded critical policy used by the relay instructions field.
    if [[ -f "$profile_dir/critical-policy.md" ]]; then
        cp "$profile_dir/critical-policy.md" "$target/critical-policy.md"
        echo "[sync] $profile/critical-policy.md"
    fi

    # Merge config-overlay.yaml into config.yaml
    overlay="$profile_dir/config-overlay.yaml"
    # Back-compat: accept the older mcp-servers.yaml filename too.
    if [[ ! -f "$overlay" && -f "$profile_dir/mcp-servers.yaml" ]]; then
        overlay="$profile_dir/mcp-servers.yaml"
    fi
    if [[ -f "$overlay" ]]; then
        target_cfg="$target/config.yaml"
        rendered_overlay="$(mktemp)"
        sed "s|__HOST_IP__|$HOST_IP|g" "$overlay" > "$rendered_overlay"

        if [[ ! -f "$target_cfg" ]]; then
            rm -f "$rendered_overlay"
            pending_merge+=("$profile:needs_bootstrap")
        else
            "$PY" - "$target_cfg" "$rendered_overlay" <<'PY'
import os
import pathlib
import re
import sys
import yaml

cfg_path = pathlib.Path(sys.argv[1])
overlay_path = pathlib.Path(sys.argv[2])


def load_env(path):
    env = dict(os.environ)
    if path.exists():
        for raw in path.read_text(encoding="utf-8").splitlines():
            line = raw.strip()
            if not line or line.startswith("#") or "=" not in line:
                continue
            key, value = line.split("=", 1)
            env[key.strip()] = value.strip().strip('"').strip("'")
    return env


def expand_env_refs(text, env):
    return re.sub(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}", lambda m: env.get(m.group(1), m.group(0)), text)


base = yaml.safe_load(cfg_path.read_text(encoding="utf-8")) or {}
overlay_text = expand_env_refs(overlay_path.read_text(encoding="utf-8"), load_env(cfg_path.parent / ".env"))
overlay = yaml.safe_load(overlay_text) or {}


def merge(dst, src, path=()):
    if isinstance(dst, dict) and isinstance(src, dict):
        for key, value in src.items():
            dst[key] = merge(dst.get(key), value, path + (str(key),))
        return dst
    if isinstance(dst, list) and isinstance(src, list):
        if path[-1:] == ("enabled",):
            out = list(dst)
            for item in src:
                if item not in out:
                    out.append(item)
            return out
        return src
    return src

merged = merge(base, overlay)
cfg_path.write_text(yaml.safe_dump(merged, sort_keys=False, allow_unicode=True, width=4096), encoding="utf-8")
PY
            rm -f "$rendered_overlay"
            echo "[merge] $profile/config.yaml overlay merged"
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
        esac
    done
fi

echo
echo "done."

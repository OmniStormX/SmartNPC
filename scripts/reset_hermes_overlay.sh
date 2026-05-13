#!/usr/bin/env bash
# Strip the smartnpc-injected overlay block from each Hermes profile's
# config.yaml so the next run of hermes/install.sh re-appends a fresh
# copy (picking up any changes to hermes/profiles/_master/config-overlay.yaml:
# tool include/exclude lists, model.context_length, compression.* knobs, etc).
#
# WHAT THIS TOUCHES
#   - Truncates ~/.hermes/profiles/<npc>/config.yaml at the installer marker
#       # ── injected by hermes/install.sh (smartnpc profile) ──
#     (keeps everything above it — i.e. Hermes' own bootstrapped config).
#
# WHAT THIS DOES NOT TOUCH
#   - SOUL.md       — install.sh already overwrites this every run.
#   - skills/       — install.sh already merges this every run. You can
#                     freely edit _master/ + re-render; next run.bat picks up.
#   - memories/, plans/ — long-term per-NPC memory artifacts. Preserved
#                     even with --wipe-state. Nuke by hand if you really
#                     want a clean slate.
#   - session / state / response_store / logs — preserved by default.
#     Pass --wipe-state to wipe the full transient + conversation layer
#     (see WIPE TARGETS below).
#
# WIPE TARGETS (only with --wipe-state)
#   Per profile, these are removed if present:
#     sessions/                       — per-gateway-start transcripts
#     state.db state.db-wal state.db-shm
#     response_store.db  + -wal + -shm — the big one (can be 500 MB+)
#     compression.db + -wal + -shm    — hermes compression checkpoints
#     conversations/                  — legacy conversation dump
#     logs/                           — gateway stderr logs
#     gateway_state.json .clean_shutdown gateway.lock auth.lock
#     .skills_prompt_snapshot.json channel_directory.json
#   These all regenerate on the next gateway start.
#
# USAGE
#   bash scripts/reset_hermes_overlay.sh                # all 6 profiles
#   bash scripts/reset_hermes_overlay.sh xiami abigail  # specific profiles
#   bash scripts/reset_hermes_overlay.sh --wipe-state   # also clear conversations
#   HERMES_HOME=/custom/path bash scripts/reset_hermes_overlay.sh
#
# AFTER RUNNING: re-run D:\SmartNPC\run.bat (or `bash hermes/install.sh`)
# to re-inject the overlay with the current _master/ content.

set -euo pipefail

HERMES_HOME="${HERMES_HOME:-$HOME/.hermes}"
PROFILES_DIR="$HERMES_HOME/profiles"
PIDFILE="${HERMES_PIDFILE:-/tmp/smartnpc-hermes-pids.txt}"
MARKER='# ── injected by hermes/install.sh (smartnpc profile) ──'

ALL_PROFILES=(xiami abigail haley harvey penny sebastian)

wipe_state=0
targets=()
for arg in "$@"; do
  case "$arg" in
    --wipe-state) wipe_state=1 ;;
    --help|-h)
      sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    -*)
      echo "error: unknown flag: $arg" >&2
      exit 2
      ;;
    *)
      targets+=("$arg")
      ;;
  esac
done

if [[ ${#targets[@]} -eq 0 ]]; then
  targets=("${ALL_PROFILES[@]}")
fi

if [[ ! -d "$PROFILES_DIR" ]]; then
  echo "error: $PROFILES_DIR does not exist (Hermes never bootstrapped?)" >&2
  exit 1
fi

# Kill any running gateway that smartnpc's launcher tracked, so the next
# run.bat starts fresh. Best-effort: we don't fail if the pidfile is missing.
if [[ -f "$PIDFILE" ]]; then
  echo "[reset] stopping tracked gateways from $PIDFILE"
  while read -r name pid port _; do
    [[ -z "${name:-}" ]] && continue
    if kill -0 "$pid" 2>/dev/null; then
      echo "  - $name (pid $pid, port $port)"
      kill "$pid" 2>/dev/null || true
    fi
  done < "$PIDFILE"
  : > "$PIDFILE"
fi

stripped=0
wiped=0
missing=()

for profile in "${targets[@]}"; do
  target="$PROFILES_DIR/$profile"
  cfg="$target/config.yaml"

  if [[ ! -d "$target" ]]; then
    missing+=("$profile")
    continue
  fi

  # Truncate config.yaml at the installer marker. If the marker isn't
  # present, the file is already clean and we leave it alone.
  if [[ -f "$cfg" ]] && grep -Fq "$MARKER" "$cfg"; then
    cp "$cfg" "$cfg.bak"
    # Keep every line strictly before the marker. One blank line above
    # the marker (added by install.sh) also goes — awk prints lines
    # until it hits the marker, then stops.
    awk -v marker="$MARKER" '
      $0 == marker { exit }
      { print }
    ' "$cfg.bak" > "$cfg"
    # Trim trailing blank lines so the file ends cleanly.
    # (shellcheck-friendly: use sed, not a python one-liner.)
    sed -i -e :a -e '/^$/{$d;N;ba' -e '}' "$cfg"
    echo "[reset] $profile: overlay stripped (backup: $cfg.bak)"
    stripped=$((stripped + 1))
  else
    echo "[reset] $profile: no overlay marker — config.yaml already clean"
  fi

  if (( wipe_state )); then
    # Transient runtime flags — regenerated on next gateway start.
    for f in gateway_state.json .clean_shutdown gateway.lock auth.lock \
             .skills_prompt_snapshot.json channel_directory.json; do
      full="$target/$f"
      if [[ -f "$full" ]]; then
        rm -f "$full"
        echo "  - wiped $profile/$f"
        wiped=$((wiped + 1))
      fi
    done
    # Directory trees.
    for d in sessions conversations logs; do
      full="$target/$d"
      if [[ -d "$full" ]]; then
        rm -rf "$full"
        echo "  - wiped $profile/$d/"
        wiped=$((wiped + 1))
      fi
    done
    # SQLite families: always sweep the main file *and* its -wal / -shm
    # siblings together. Orphaned wal/shm can confuse SQLite on next
    # open (it'll try to replay stale journal pages).
    for base in state.db response_store.db compression.db; do
      for suffix in "" "-wal" "-shm"; do
        full="$target/$base$suffix"
        if [[ -f "$full" ]]; then
          rm -f "$full"
          echo "  - wiped $profile/$base$suffix"
          wiped=$((wiped + 1))
        fi
      done
    done
  fi
done

echo
echo "summary: $stripped profile(s) stripped"
if (( wipe_state )); then
  echo "         $wiped state artifact(s) wiped"
fi
if (( ${#missing[@]} > 0 )); then
  echo "         missing profile dirs (skipped): ${missing[*]}"
fi

echo
echo "next step: re-run run.bat (or 'bash hermes/install.sh') to re-inject"
echo "          the overlay with the current _master/ content."

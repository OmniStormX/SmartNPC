#!/usr/bin/env bash
# Read-only sanity checks on the rendered Hermes profile tree.
#
# The source of truth is hermes/profiles/_master/ plus hermes/npcs.yaml.
# This script asserts the working tree is consistent with those sources — it
# does NOT re-render, does NOT modify files, does NOT require a clean git state.
#
# Checks performed (each fatal on failure):
#   1. hermes/npcs.yaml is present, complete, and internally consistent.
#   2. Rendered profile directories and SOUL.md exist for every enabled NPC.
#   3. runtime-config.yaml and per-profile config overlays match registry ports
#      and NPC filters.
#   4. No `{{...}}` placeholder leaks in any rendered profile.
#   5. No XiaMi / xiami / 夏弥 string leaks in non-xiami profiles outside SOUL.md.
#   6. SmartNPC SKILL.md frontmatter names match their global Hermes skill IDs:
#      name: <directory-name>.
#   7. SKILL files classified as "shared, no per-NPC fields" are byte-identical
#      across all enabled NPCs. Today this is proactive-greeting/SKILL.md.
#
# This script does NOT re-run render_profiles.sh — that requires GNU sed + bash
# and may mutate the working tree. To check render idempotency, run:
#   bash scripts/render_profiles.sh && git diff --exit-code hermes/profiles/
#
# Usage (from repo root):
#   bash scripts/test_profile_render.sh
#
# Windows: run inside Git Bash or WSL.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROFILES="$REPO_ROOT/hermes/profiles"
REGISTRY="$REPO_ROOT/hermes/npcs.yaml"
RUNTIME_CONFIG="$REPO_ROOT/hermes/runtime-config.yaml"

NPCS=()
NON_XIAMI=()
declare -A NPC_NAME=()
declare -A NPC_DISPLAY=()
declare -A NPC_PORT=()
declare -A NPC_KIND=()
declare -A NPC_ENABLED=()
declare -A NPC_PEER_A_NAME=()
declare -A NPC_PEER_A_DISPLAY=()
declare -A NPC_PEER_B_NAME=()
declare -A NPC_PEER_B_DISPLAY=()

red()   { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
yellow(){ printf '\033[33m%s\033[0m\n' "$*"; }

fail() { red "FAIL: $*"; exit 1; }
trim_cr() { printf '%s' "${1%$'\r'}"; }
strip_quotes() {
  local v
  v="$(trim_cr "$1")"
  v="${v%\"}"; v="${v#\"}"
  v="${v%\'}"; v="${v#\'}"
  printf '%s' "$v"
}

load_registry() {
  [ -f "$REGISTRY" ] || fail "missing NPC registry: $REGISTRY"

  local current="" key="" val="" line=""
  while IFS= read -r line || [ -n "$line" ]; do
    line="$(trim_cr "$line")"
    line="${line%%#*}"
    [[ -z "${line//[[:space:]]/}" ]] && continue

    if [[ "$line" =~ ^[[:space:]]*-[[:space:]]id:[[:space:]]*([^[:space:]]+)[[:space:]]*$ ]]; then
      current="$(strip_quotes "${BASH_REMATCH[1]}")"
      NPC_ENABLED["$current"]="true"
      continue
    fi

    if [[ -n "$current" && "$line" =~ ^[[:space:]]*([A-Za-z_]+):[[:space:]]*(.*)$ ]]; then
      key="${BASH_REMATCH[1]}"
      val="$(strip_quotes "${BASH_REMATCH[2]}")"
      case "$key" in
        game_name) NPC_NAME["$current"]="$val" ;;
        display_name) NPC_DISPLAY["$current"]="$val" ;;
        gateway_port) NPC_PORT["$current"]="$val" ;;
        enabled) NPC_ENABLED["$current"]="$val" ;;
        kind) NPC_KIND["$current"]="$val" ;;
        peer_a_name) NPC_PEER_A_NAME["$current"]="$val" ;;
        peer_a_display) NPC_PEER_A_DISPLAY["$current"]="$val" ;;
        peer_b_name) NPC_PEER_B_NAME["$current"]="$val" ;;
        peer_b_display) NPC_PEER_B_DISPLAY["$current"]="$val" ;;
      esac
    fi
  done < "$REGISTRY"

  local n="" seen_ports=""
  while IFS= read -r n; do
    [ -n "$n" ] || continue
    if [ "${NPC_ENABLED[$n]:-true}" = "true" ]; then
      NPCS+=("$n")
      [ "$n" = "xiami" ] || NON_XIAMI+=("$n")
    fi
  done < <(sed -n 's/^[[:space:]]*-[[:space:]]id:[[:space:]]*//p' "$REGISTRY" | tr -d '\r' | awk '{print $1}')

  [ "${#NPCS[@]}" -gt 0 ] || fail "registry has no enabled NPCs"

  for n in "${NPCS[@]}"; do
    [ -n "${NPC_NAME[$n]:-}" ] || fail "registry missing game_name for $n"
    [ -n "${NPC_DISPLAY[$n]:-}" ] || fail "registry missing display_name for $n"
    [ -n "${NPC_PORT[$n]:-}" ] || fail "registry missing gateway_port for $n"
    [[ "${NPC_PORT[$n]}" =~ ^[0-9]+$ ]] || fail "registry gateway_port for $n is not numeric: ${NPC_PORT[$n]}"
    [ -n "${NPC_KIND[$n]:-}" ] || fail "registry missing kind for $n"
    case "${NPC_KIND[$n]}" in custom|vanilla) ;; *) fail "registry kind for $n must be custom or vanilla, got ${NPC_KIND[$n]}" ;; esac

    if printf '%s\n' "$seen_ports" | grep -qx "${NPC_PORT[$n]}"; then
      fail "duplicate gateway_port in registry: ${NPC_PORT[$n]}"
    fi
    seen_ports="$seen_ports${NPC_PORT[$n]}"$'\n'
  done
}

runtime_field() {
  local profile="$1" key="$2"
  awk -v profile="$profile" -v key="$key" '
    $0 ~ "^[[:space:]]*-[[:space:]]name:[[:space:]]*" profile "[[:space:]]*$" { inside=1; next }
    inside && $0 ~ "^[[:space:]]*-[[:space:]]name:" { inside=0 }
    inside && $0 ~ "^[[:space:]]*" key ":" {
      sub("^[[:space:]]*" key ":[[:space:]]*", "")
      sub("[[:space:]]*$", "")
      print
      exit
    }
  ' "$RUNTIME_CONFIG" | tr -d '\r'
}

extract_port_from_url() {
  printf '%s' "$1" | sed -n 's|.*:\([0-9][0-9]*\)\(/.*\)\{0,1\}$|\1|p'
}

# 1. registry is valid
load_registry
green "[1/7] NPC registry valid (${#NPCS[@]} enabled profiles)"

# 2. directories exist
for n in "${NPCS[@]}"; do
  [ -d "$PROFILES/$n" ] || fail "missing profile dir: $PROFILES/$n"
  [ -f "$PROFILES/$n/SOUL.md" ] || fail "missing SOUL.md for $n"
done
green "[2/7] enabled NPC profile directories present with SOUL.md"

# 3. runtime-config.yaml and rendered config overlays match registry
[ -f "$RUNTIME_CONFIG" ] || fail "missing runtime config: $RUNTIME_CONFIG"
for n in "${NPCS[@]}"; do
  npc_filter="$(runtime_field "$n" npc_filter)"
  conversation="$(runtime_field "$n" conversation)"
  gateway_url="$(runtime_field "$n" gateway_url)"
  gateway_port="$(extract_port_from_url "$gateway_url")"

  [ "$npc_filter" = "${NPC_NAME[$n]}" ] || fail "runtime-config $n npc_filter=$npc_filter, want ${NPC_NAME[$n]}"
  [ "$conversation" = "$n" ] || fail "runtime-config $n conversation=$conversation, want $n"
  [ "$gateway_port" = "${NPC_PORT[$n]}" ] || fail "runtime-config $n gateway_url port=$gateway_port, want ${NPC_PORT[$n]}"

  overlay="$PROFILES/$n/config-overlay.yaml"
  [ -f "$overlay" ] || fail "missing config overlay for $n: $overlay"
  overlay_port="$(sed -n 's/^API_SERVER_PORT:[[:space:]]*//p' "$overlay" | head -n 1 | tr -d '\r')"
  overlay_model="$(sed -n 's/^API_SERVER_MODEL_NAME:[[:space:]]*//p' "$overlay" | head -n 1 | tr -d '\r')"
  [ "$overlay_port" = "${NPC_PORT[$n]}" ] || fail "$n config-overlay API_SERVER_PORT=$overlay_port, want ${NPC_PORT[$n]}"
  [ "$overlay_model" = "$n" ] || fail "$n config-overlay API_SERVER_MODEL_NAME=$overlay_model, want $n"
done
green "[3/7] runtime-config and config overlays match NPC registry"

# 4. no placeholder leaks anywhere
LEAKS="$(grep -rln '{{' "${NPCS[@]/#/$PROFILES/}" 2>/dev/null || true)"
if [ -n "$LEAKS" ]; then
  red "placeholder leak — files still contain '{{':"
  printf '  %s\n' $LEAKS >&2
  fail "fix by re-running scripts/render_profiles.sh"
fi
green "[4/7] no '{{...}}' placeholder leaks in any rendered profile"

# 5. no xiami-name leaks in non-xiami profiles, excluding SOUL.md
LEAK_FILES=""
for n in "${NON_XIAMI[@]}"; do
  while IFS= read -r f; do
    base="$(basename "$f")"
    [ "$base" = "SOUL.md" ] && continue
    LEAK_FILES="$LEAK_FILES$f"$'\n'
  done < <(grep -rIln -E 'XiaMi|xiami|夏弥' "$PROFILES/$n" 2>/dev/null || true)
done
if [ -n "${LEAK_FILES%$'\n'}" ]; then
  red "XiaMi-name leak in non-xiami profile rendered files:"
  printf '%s' "$LEAK_FILES" >&2
  fail "rendered template referencing XiaMi outside SOUL.md — fix _master/ template or re-run render"
fi
green "[5/7] no XiaMi/xiami/夏弥 leaks in non-xiami profiles (SOUL.md excluded)"

# 6. SmartNPC skill directories and frontmatter names must use the same globally namespaced ID.
check_skill_frontmatter_names() {
  local root="$1"
  local label="$2"
  local base="$root/skills/smartnpc"
  local bad=0

  [ -d "$base" ] || fail "missing smartnpc skill dir: $base"

  while IFS= read -r d; do
    local dirname
    dirname="$(basename "$d")"
    if [[ "$dirname" != smartnpc-* ]]; then
      red "  $label/$dirname: smartnpc skill directory must be prefixed with smartnpc-"
      bad=1
    fi
  done < <(find "$base" -mindepth 1 -maxdepth 1 -type d | sort)

  while IFS= read -r f; do
    local slug expected actual
    slug="$(basename "$(dirname "$f")")"
    expected="$slug"
    actual="$(sed -n 's/^name:[[:space:]]*//p' "$f" | head -n 1 | tr -d '\r')"
    if [ "$actual" != "$expected" ]; then
      red "  $label/$slug: expected frontmatter name '$expected', got '${actual:-<missing>}'"
      bad=1
    fi
  done < <(find "$base" -type f -name SKILL.md | sort)

  [ "$bad" -eq 0 ]
}

echo "[6/7] smartnpc skill frontmatter names"
ok=1
check_skill_frontmatter_names "$PROFILES/_master" "_master" || ok=0
for n in "${NPCS[@]}"; do
  check_skill_frontmatter_names "$PROFILES/$n" "$n" || ok=0
done
[ "$ok" -eq 1 ] || fail "smartnpc skill frontmatter name check failed; see above"
green "  ok: all smartnpc SKILL.md names match their smartnpc-* directory"

# 7. byte-identity for skills that should be fully shared
check_identical() {
  local rel="$1"; shift
  local mode="$1"; shift   # "fail" or "warn"
  local missing=0
  local hashes=""
  for n in "${NPCS[@]}"; do
    local f="$PROFILES/$n/$rel"
    if [ ! -f "$f" ]; then
      missing=$((missing+1))
      continue
    fi
    local h
    h="$(md5sum "$f" | awk '{print $1}')"
    hashes="$hashes$h $n"$'\n'
  done
  if [ "$missing" -eq "${#NPCS[@]}" ]; then
    yellow "  skip: $rel — not present in any profile (run render_profiles.sh to materialize)"
    return 0
  fi
  if [ "$missing" -ne 0 ]; then
    if [ "$mode" = "warn" ]; then
      yellow "  warn: $rel — missing from $missing/${#NPCS[@]} profiles (likely render-lag)"
      return 0
    fi
    red "  $rel — missing from $missing/${#NPCS[@]} profiles"
    return 1
  fi
  local uniq
  uniq="$(printf '%s' "$hashes" | awk '{print $1}' | sort -u | wc -l | tr -d ' ')"
  if [ "$uniq" != "1" ]; then
    red "  $rel — md5 differs across NPCs:"
    printf '%s' "$hashes" >&2
    return 1
  fi
  green "  ok:   $rel — byte-identical across all enabled NPCs"
}

echo "[7/7] byte-identity for skills with no per-NPC placeholders"
ok=1
check_identical "skills/smartnpc/smartnpc-proactive-greeting/SKILL.md" "fail" || ok=0
[ "$ok" -eq 1 ] || fail "byte-identity check failed; see above"

echo ""
green "all profile render sanity checks passed."

#!/usr/bin/env bash
# Read-only sanity checks on the rendered Hermes profile tree.
#
# Phase 2 follow-up PR (F1/F2): hermes-profile introduced
# hermes/profiles/_master/ + scripts/render_profiles.sh so that 6 NPCs share a
# single source of truth. This script asserts the working tree is consistent
# with that source — it does NOT re-render, does NOT modify files, does NOT
# require a clean git state.
#
# Checks performed (each fatal on failure):
#   1. No `{{...}}` placeholder leaks in any rendered profile (xiami included).
#   2. No XiaMi / xiami / 夏弥 string leaks in non-xiami profiles outside SOUL.md
#      (SOUL.md is hand-written per NPC and may legitimately reference peers,
#      including XiaMi).
#   3. No XiaMi / xiami / display-name string leaks in non-xiami rendered files.
#   4. SmartNPC SKILL.md frontmatter names match their global Hermes skill IDs:
#      name: <directory-name> (directories are globally namespaced as smartnpc-*).
#   5. SKILL files that hermes-profile classifies as "shared, no per-NPC fields"
#      are byte-identical across all 6 NPCs (md5sum). Today the only such file
#      is proactive-greeting/SKILL.md. group-chat-reply DOES carry per-NPC
#      placeholders ({{NPC_NAME}} appears in its body and tool examples) and
#      is intentionally NOT byte-identical.
#
# This script does NOT re-run render_profiles.sh — that requires GNU sed +
# bash and may mutate the working tree. To check render idempotency, run
# `bash scripts/render_profiles.sh && git diff --exit-code hermes/profiles/`
# manually.
#
# Usage (from repo root):
#   bash scripts/test_profile_render.sh
#
# Windows: run inside Git Bash or WSL. Pure POSIX, no GNU-only features.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROFILES="$REPO_ROOT/hermes/profiles"
NPCS=(xiami abigail haley harvey penny sebastian)
NON_XIAMI=(abigail haley harvey penny sebastian)

red()   { printf '\033[31m%s\033[0m\n' "$*" >&2; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
yellow(){ printf '\033[33m%s\033[0m\n' "$*"; }

fail() { red "FAIL: $*"; exit 1; }

# 1. directories exist
for n in "${NPCS[@]}"; do
  [ -d "$PROFILES/$n" ]      || fail "missing profile dir: $PROFILES/$n"
  [ -f "$PROFILES/$n/SOUL.md" ] || fail "missing SOUL.md for $n"
done
green "[1/5] all 6 NPC profile directories present with SOUL.md"

# 2. no placeholder leaks anywhere
LEAKS="$(grep -rln '{{' "${NPCS[@]/#/$PROFILES/}" 2>/dev/null || true)"
if [ -n "$LEAKS" ]; then
  red "placeholder leak — files still contain '{{':"
  printf '  %s\n' $LEAKS >&2
  fail "fix by re-running scripts/render_profiles.sh"
fi
green "[2/5] no '{{...}}' placeholder leaks in any rendered profile"

# 3. no xiami-name leaks in non-xiami profiles, excluding SOUL.md
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
green "[3/5] no XiaMi/xiami/夏弥 leaks in non-xiami profiles (SOUL.md excluded)"

# 4. SmartNPC skill directories and frontmatter names must use the same globally namespaced ID.
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

echo "[4/5] smartnpc skill frontmatter names"
ok=1
check_skill_frontmatter_names "$PROFILES/_master" "_master" || ok=0
for n in "${NPCS[@]}"; do
  check_skill_frontmatter_names "$PROFILES/$n" "$n" || ok=0
done
[ "$ok" -eq 1 ] || fail "smartnpc skill frontmatter name check failed; see above"
green "  ok: all smartnpc SKILL.md names match their smartnpc-* directory"

# 5. byte-identity for skills that should be fully shared
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
  green "  ok:   $rel — byte-identical across all 6 NPCs"
}

echo "[5/5] byte-identity for skills with no per-NPC placeholders"
ok=1
check_identical "skills/smartnpc/smartnpc-proactive-greeting/SKILL.md" "fail" || ok=0
[ "$ok" -eq 1 ] || fail "byte-identity check failed; see above"

echo ""
green "all profile render sanity checks passed."

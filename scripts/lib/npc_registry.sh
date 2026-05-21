#!/usr/bin/env bash
# Shared loader for hermes/npcs.yaml.
# shellcheck shell=bash

smartnpc_strip_quotes() {
  local v
  v="${1%$'\r'}"
  v="${v%\"}"; v="${v#\"}"
  v="${v%\'}"; v="${v#\'}"
  printf '%s' "$v"
}

smartnpc_registry_reset() {
  SMARTNPC_NPCS=()
  declare -gA SMARTNPC_NPC_GAME_NAME=()
  declare -gA SMARTNPC_NPC_DISPLAY_NAME=()
  declare -gA SMARTNPC_NPC_GATEWAY_PORT=()
  declare -gA SMARTNPC_NPC_ENABLED=()
  declare -gA SMARTNPC_NPC_KIND=()
  declare -gA SMARTNPC_NPC_PEER_A_NAME=()
  declare -gA SMARTNPC_NPC_PEER_A_DISPLAY=()
  declare -gA SMARTNPC_NPC_PEER_B_NAME=()
  declare -gA SMARTNPC_NPC_PEER_B_DISPLAY=()
}

smartnpc_load_registry() {
  local registry="$1"
  [ -f "$registry" ] || { echo "missing NPC registry: $registry" >&2; return 1; }

  smartnpc_registry_reset

  local current="" key="" val="" line=""
  while IFS= read -r line || [ -n "$line" ]; do
    line="${line%$'\r'}"
    line="${line%%#*}"
    [[ -z "${line//[[:space:]]/}" ]] && continue

    if [[ "$line" =~ ^[[:space:]]*-[[:space:]]id:[[:space:]]*([^[:space:]]+)[[:space:]]*$ ]]; then
      current="$(smartnpc_strip_quotes "${BASH_REMATCH[1]}")"
      SMARTNPC_NPC_ENABLED["$current"]="true"
      continue
    fi

    if [[ -n "$current" && "$line" =~ ^[[:space:]]*([A-Za-z_]+):[[:space:]]*(.*)$ ]]; then
      key="${BASH_REMATCH[1]}"
      val="$(smartnpc_strip_quotes "${BASH_REMATCH[2]}")"
      case "$key" in
        game_name) SMARTNPC_NPC_GAME_NAME["$current"]="$val" ;;
        display_name) SMARTNPC_NPC_DISPLAY_NAME["$current"]="$val" ;;
        gateway_port) SMARTNPC_NPC_GATEWAY_PORT["$current"]="$val" ;;
        enabled) SMARTNPC_NPC_ENABLED["$current"]="$val" ;;
        kind) SMARTNPC_NPC_KIND["$current"]="$val" ;;
        peer_a_name) SMARTNPC_NPC_PEER_A_NAME["$current"]="$val" ;;
        peer_a_display) SMARTNPC_NPC_PEER_A_DISPLAY["$current"]="$val" ;;
        peer_b_name) SMARTNPC_NPC_PEER_B_NAME["$current"]="$val" ;;
        peer_b_display) SMARTNPC_NPC_PEER_B_DISPLAY["$current"]="$val" ;;
      esac
    fi
  done < "$registry"

  local n="" seen_ports=""
  while IFS= read -r n; do
    n="${n%$'\r'}"
    [ -n "$n" ] || continue
    if [ "${SMARTNPC_NPC_ENABLED[$n]:-true}" = "true" ]; then
      SMARTNPC_NPCS+=("$n")
    fi
  done < <(sed -n 's/^[[:space:]]*-[[:space:]]id:[[:space:]]*//p' "$registry" | tr -d '\r' | awk '{print $1}')

  [ "${#SMARTNPC_NPCS[@]}" -gt 0 ] || { echo "registry has no enabled NPCs" >&2; return 1; }

  for n in "${SMARTNPC_NPCS[@]}"; do
    [ -n "${SMARTNPC_NPC_GAME_NAME[$n]:-}" ] || { echo "registry missing game_name for $n" >&2; return 1; }
    [ -n "${SMARTNPC_NPC_DISPLAY_NAME[$n]:-}" ] || { echo "registry missing display_name for $n" >&2; return 1; }
    [ -n "${SMARTNPC_NPC_GATEWAY_PORT[$n]:-}" ] || { echo "registry missing gateway_port for $n" >&2; return 1; }
    [[ "${SMARTNPC_NPC_GATEWAY_PORT[$n]}" =~ ^[0-9]+$ ]] || { echo "gateway_port for $n is not numeric: ${SMARTNPC_NPC_GATEWAY_PORT[$n]}" >&2; return 1; }
    [ -n "${SMARTNPC_NPC_KIND[$n]:-}" ] || { echo "registry missing kind for $n" >&2; return 1; }

    if printf '%s\n' "$seen_ports" | grep -qx "${SMARTNPC_NPC_GATEWAY_PORT[$n]}"; then
      echo "duplicate gateway_port: ${SMARTNPC_NPC_GATEWAY_PORT[$n]}" >&2
      return 1
    fi
    seen_ports="$seen_ports${SMARTNPC_NPC_GATEWAY_PORT[$n]}"$'\n'
  done
}

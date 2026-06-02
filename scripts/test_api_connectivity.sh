#!/usr/bin/env bash
# scripts/test_api_connectivity.sh
#
# Connectivity smoke-test for every external dependency the SmartNPC stack
# touches. Designed to answer the operator question "is X actually
# reachable right now?" without spinning up the whole game / hermes /
# mcp triangle.
#
# Checks performed, per profile (or once for shared cfg):
#   1. DNS / TCP reach to HERMES_AGENT_URL host
#   2. GET <HERMES_AGENT_URL>/models  — OpenAI protocol catalogue (cheap)
#   3. POST <HERMES_AGENT_URL>/chat/completions — minimal 1-token ping
#      that exercises the actual inference path
#   4. (optional) GET https://cloud.langfuse.com/api/public/health
#      with HTTP basic auth from HERMES_LANGFUSE_PUBLIC/SECRET_KEY
#
# Reads config from each enabled NPC profile under
# ~/.hermes/profiles/<npc>/.env (set by hermes/install.sh) so the
# results match what the running gateways are actually using.
#
# Usage:
#   bash scripts/test_api_connectivity.sh                  # all profiles
#   bash scripts/test_api_connectivity.sh xiami abigail    # subset
#
# Exit code: 0 if every chat/completions probe returned 200,
# non-zero otherwise (suitable for CI / pre-flight scripts).

set -uo pipefail

# ── Color helpers (TTY only, plain otherwise) ───────────────────────────────
if [[ -t 1 ]]; then
    C_RED=$'\e[31m'; C_YEL=$'\e[33m'; C_GRN=$'\e[32m'; C_DIM=$'\e[2m'; C_RST=$'\e[0m'
else
    C_RED=''; C_YEL=''; C_GRN=''; C_DIM=''; C_RST=''
fi

PROFILES_DIR="${HERMES_PROFILES_DIR:-$HOME/.hermes/profiles}"

# Default to all directory-style profiles when no args given. We deliberately
# enumerate the live WSL profile tree (not the repo-side YAML) because
# whatever those .env files hold is what the gateways are actually loading.
if [[ $# -gt 0 ]]; then
    PROFILES=("$@")
else
    if [[ ! -d "$PROFILES_DIR" ]]; then
        echo "${C_RED}error:${C_RST} profile dir not found: $PROFILES_DIR" >&2
        echo "  hint: run 'wsl bash hermes/install.sh' first, or pass profile names explicitly" >&2
        exit 2
    fi
    # Enumerate live profile dirs but skip non-NPC ones:
    #   _master   — repo template, no real keys
    #   smartnpc  — Hermes built-in skill bundle, not a profile
    mapfile -t PROFILES < <(find "$PROFILES_DIR" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' \
        | grep -Ev '^(_master|smartnpc)$' \
        | sort)
fi

if [[ ${#PROFILES[@]} -eq 0 ]]; then
    echo "${C_YEL}warn:${C_RST} no profiles found"
    exit 0
fi

# ── Per-probe runners ───────────────────────────────────────────────────────

# Strip trailing /v1 etc., we only need scheme+host[:port] for tcp probe.
host_of() {
    # Input: http://api.example.com/v1
    # Output: api.example.com
    local u="$1"
    u="${u#http://}"; u="${u#https://}"
    u="${u%%/*}"; u="${u%%:*}"
    printf '%s' "$u"
}

port_of() {
    # Input: http://api.example.com:8080/v1  → 8080
    #        http://api.example.com/v1        → 80 (or 443 for https)
    local u="$1"
    local default=80
    [[ "$u" == https://* ]] && default=443
    u="${u#http://}"; u="${u#https://}"
    u="${u%%/*}"
    if [[ "$u" == *:* ]]; then
        printf '%s' "${u##*:}"
    else
        printf '%d' "$default"
    fi
}

# Run curl, swallow body unless we want it; print "HTTP <code> in <s>s".
# $1=label  $2=method  $3=url  [$4=auth_header]  [$5=body_json]
probe_http() {
    local label="$1" method="$2" url="$3" auth="${4:-}" body="${5:-}"
    local args=(-sS -o /dev/null -w '%{http_code}|%{time_total}'
                --connect-timeout 5 --max-time 15
                -X "$method" "$url")
    [[ -n "$auth" ]] && args=(-H "Authorization: Bearer $auth" "${args[@]}")
    if [[ -n "$body" ]]; then
        args+=(-H "Content-Type: application/json" --data-raw "$body")
    fi

    local out; out="$(curl "${args[@]}" 2>&1)" || true
    if [[ "$out" =~ ^([0-9]+)\|([0-9.]+)$ ]]; then
        local code="${BASH_REMATCH[1]}" t="${BASH_REMATCH[2]}"
        local color="$C_GRN"
        case "$code" in
            2*)        color="$C_GRN" ;;
            401|403)   color="$C_YEL" ;;  # reachable but auth issue
            000)       color="$C_RED" ;;  # connect refused / timeout
            *)         color="$C_RED" ;;
        esac
        printf "  %-30s %sHTTP %s%s in %ss\n" "$label" "$color" "$code" "$C_RST" "$t"
        # Return code via global so caller can check critical probes
        LAST_CODE="$code"
    else
        printf "  %-30s ${C_RED}FAIL${C_RST} (%s)\n" "$label" "$(printf '%s' "$out" | tr '\n' ' ' | cut -c1-80)"
        LAST_CODE="000"
    fi
}

# Read a single var from a profile .env. Sourcing the whole file would
# pollute our shell with HERMES_AGENT_* etc. across iterations.
read_env_var() {
    local file="$1" key="$2"
    [[ -f "$file" ]] || return 1
    grep -E "^[[:space:]]*(export[[:space:]]+)?${key}=" "$file" \
        | tail -n1 \
        | sed -E "s/^[[:space:]]*(export[[:space:]]+)?${key}=//; s/^['\"]//; s/['\"]\$//"
}

# ── Header ──────────────────────────────────────────────────────────────────

echo "${C_DIM}SmartNPC API connectivity probe${C_RST}"
echo "${C_DIM}profiles dir: $PROFILES_DIR${C_RST}"
echo "${C_DIM}target profiles: ${PROFILES[*]}${C_RST}"
echo

# ── Per-profile probes ──────────────────────────────────────────────────────

ANY_FAIL=0

for profile in "${PROFILES[@]}"; do
    env_file="$PROFILES_DIR/$profile/.env"
    if [[ ! -f "$env_file" ]]; then
        echo "${C_YEL}[$profile]${C_RST} no .env at $env_file — skipped"
        echo
        continue
    fi

    url="$(read_env_var "$env_file" HERMES_AGENT_URL || true)"
    key="$(read_env_var "$env_file" HERMES_AGENT_API_KEY || true)"
    model="$(read_env_var "$env_file" HERMES_AGENT_MODEL || true)"
    lf_pk="$(read_env_var "$env_file" HERMES_LANGFUSE_PUBLIC_KEY || true)"
    lf_sk="$(read_env_var "$env_file" HERMES_LANGFUSE_SECRET_KEY || true)"
    lf_host="$(read_env_var "$env_file" HERMES_LANGFUSE_HOST || true)"

    echo "${C_DIM}── [$profile] ──${C_RST}"
    if [[ -z "$url" || -z "$key" ]]; then
        echo "  ${C_RED}missing HERMES_AGENT_URL or HERMES_AGENT_API_KEY${C_RST}"
        echo
        ANY_FAIL=1
        continue
    fi

    h="$(host_of "$url")"
    p="$(port_of "$url")"
    printf "  endpoint: %s  model: %s\n" "$url" "${model:-<unset>}"
    printf "  api key:  %.10s****  (len=%d)\n" "$key" "${#key}"

    # 1. TCP reach (5s connect timeout)
    if timeout 5 bash -c "exec 3<>/dev/tcp/$h/$p" 2>/dev/null; then
        printf "  %-30s ${C_GRN}OK${C_RST}  (%s:%s)\n" "tcp connect" "$h" "$p"
        exec 3<&- 2>/dev/null || true
        exec 3>&- 2>/dev/null || true
    else
        printf "  %-30s ${C_RED}REFUSED${C_RST} (%s:%s)\n" "tcp connect" "$h" "$p"
        ANY_FAIL=1
        echo
        continue
    fi

    # 2. GET /models  (OpenAI catalogue endpoint, cheap)
    probe_http "GET /models"          GET  "${url%/}/models" "$key"
    code_models="$LAST_CODE"

    # 3. POST /chat/completions  (real inference, 4-token reply max)
    body="{\"model\":\"${model:-gpt-3.5-turbo}\",\"messages\":[{\"role\":\"user\",\"content\":\"ping\"}],\"max_tokens\":4}"
    probe_http "POST /chat/completions" POST "${url%/}/chat/completions" "$key" "$body"
    code_chat="$LAST_CODE"
    [[ "$code_chat" != 2* ]] && ANY_FAIL=1

    # 4. (optional) Langfuse cloud health
    if [[ -n "$lf_pk" && -n "$lf_sk" ]]; then
        lf_host="${lf_host:-https://cloud.langfuse.com}"
        out="$(curl -sS -o /dev/null -w '%{http_code}|%{time_total}' \
                    --connect-timeout 5 --max-time 10 \
                    -u "$lf_pk:$lf_sk" \
                    "${lf_host%/}/api/public/health" 2>&1)" || true
        if [[ "$out" =~ ^([0-9]+)\|([0-9.]+)$ ]]; then
            code="${BASH_REMATCH[1]}" t="${BASH_REMATCH[2]}"
            color="$C_GRN"; [[ "$code" != 2* ]] && color="$C_YEL"
            printf "  %-30s %sHTTP %s%s in %ss  (%s)\n" "Langfuse health" "$color" "$code" "$C_RST" "$t" "${lf_host}"
        else
            printf "  %-30s ${C_YEL}skip${C_RST}  (%s)\n" "Langfuse health" "${out:0:60}"
        fi
    else
        printf "  %-30s ${C_DIM}skip (no LANGFUSE keys)${C_RST}\n" "Langfuse health"
    fi
    echo
done

# ── Summary ────────────────────────────────────────────────────────────────

if [[ "$ANY_FAIL" -eq 0 ]]; then
    echo "${C_GRN}OK — all chat/completions probes returned 2xx.${C_RST}"
    exit 0
else
    echo "${C_RED}FAIL — at least one probe failed (see above).${C_RST}"
    echo "Most common causes:"
    echo "  - curl 52 / 000           upstream is down or returning empty replies"
    echo "  - HTTP 401 / 403          auth: rotate or re-issue HERMES_AGENT_API_KEY"
    echo "  - HTTP 404                wrong path: HERMES_AGENT_URL must end at /v1"
    echo "  - tcp REFUSED             DNS or proxy issue from this host"
    exit 1
fi

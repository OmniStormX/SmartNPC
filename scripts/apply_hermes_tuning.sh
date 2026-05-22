#!/usr/bin/env bash
# Apply context-window + compression tuning to each Hermes profile's
# bootstrapped config.yaml using a real per-key YAML merge (PyYAML).
#
# WHY THIS EXISTS
#   hermes/install.sh dumb-appends config-overlay.yaml to config.yaml.
#   That works for blocks that don't exist in Hermes' bootstrap config
#   (mcp_servers, API_SERVER_*) but creates duplicate top-level keys
#   for blocks that DO exist (model, compression). PyYAML silently
#   discards the first definition on duplicate keys, so dumb-appending
#   `model: { context_length: 64000 }` would overwrite the bootstrap's
#   `model: { provider: ..., base_url: ... }` and break LLM auth.
#
#   This script reads the live config.yaml, sets only the tuning keys,
#   and writes back — preserving every other field.
#
# IDEMPOTENT: re-runs are safe; the values converge.
#
# WHAT IT SETS (per profile)
#   model.provider = custom:smartnpc-agent
#   model.default = HERMES_AGENT_MODEL from profile .env
#   model.base_url = HERMES_AGENT_URL from profile .env
#   model.context_length = 64000
#   providers.smartnpc-agent.key_env = HERMES_AGENT_API_KEY
#   compression.enabled = true
#   compression.threshold = 0.15
#   compression.target_ratio = 0.10
#   compression.protect_last_n = 8
#   compression.hygiene_hard_message_limit = 60
#   agent.tool_use_enforcement = false
#
# WHY agent.tool_use_enforcement = false
#   Hermes' bootstrap injects a "Tool-use enforcement" + <tool_persistence>
#   block into the system prompt for any model whose name matches gpt /
#   codex / gemini / gemma / grok (default `auto`). That block tells the
#   model "Keep calling tools until task complete & verified" and "Do not
#   stop early when another tool call would materially improve the
#   result" — which DIRECTLY conflicts with our SmartNPC "exactly one
#   chat_say per wake-up, then end the turn" rule. The model defers to
#   the developer-level system prompt over our skill content, so even
#   with the chat_say TURN_END signal it keeps emitting more tool calls
#   in a loop. Disabling the injection lets SOUL.md + skills own the
#   tool-loop policy.
#
# Usage:
#   bash scripts/apply_hermes_tuning.sh                    # all 6 profiles
#   bash scripts/apply_hermes_tuning.sh xiami abigail      # specific profiles
#   HERMES_HOME=/custom/path bash scripts/apply_hermes_tuning.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
REGISTRY="$REPO_ROOT/hermes/npcs.yaml"

# shellcheck source=scripts/lib/npc_registry.sh
source "$SCRIPT_DIR/lib/npc_registry.sh"
smartnpc_load_registry "$REGISTRY"

HERMES_HOME="${HERMES_HOME:-$HOME/.hermes}"
PROFILES_DIR="$HERMES_HOME/profiles"

targets=("$@")
if [[ ${#targets[@]} -eq 0 ]]; then
  targets=("${SMARTNPC_NPCS[@]}")
fi

if [[ ! -d "$PROFILES_DIR" ]]; then
  echo "error: $PROFILES_DIR does not exist" >&2
  exit 1
fi

# Pick a Python with PyYAML. Hermes ships its own venv under
# ~/.hermes/bin/python; fall back to system python3 if missing.
PY=""
for candidate in "$HERMES_HOME/bin/python" python3 python; do
  if command -v "$candidate" >/dev/null 2>&1; then
    if "$candidate" -c 'import yaml' >/dev/null 2>&1; then
      PY="$candidate"
      break
    fi
  fi
done
if [[ -z "$PY" ]]; then
  echo "error: no Python with PyYAML found (tried hermes venv + system)" >&2
  echo "       try: pip install pyyaml" >&2
  exit 1
fi

applied=0
skipped=0

for profile in "${targets[@]}"; do
  cfg="$PROFILES_DIR/$profile/config.yaml"
  if [[ ! -f "$cfg" ]]; then
    echo "[tune] $profile: no config.yaml — skipping"
    skipped=$((skipped + 1))
    continue
  fi

  "$PY" - "$cfg" <<'PY'
import os, sys, yaml, pathlib

path = pathlib.Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
data = yaml.safe_load(text) or {}


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


env = load_env(path.parent / ".env")
agent_url = env.get("HERMES_AGENT_URL", "${HERMES_AGENT_URL}")
agent_model = env.get("HERMES_AGENT_MODEL", "${HERMES_AGENT_MODEL}")

# SmartNPC profiles are model-provider driven by each profile's .env.
# Write non-secret url/model values into config.yaml so Hermes gateway paths
# that do not expand ${...} still call the intended OpenAI-compatible API.
model = data.get("model")
if not isinstance(model, dict):
    model = {} if model is None else {"_legacy": model}
model.update({
    "provider": "custom:smartnpc-agent",
    "default": agent_model,
    "base_url": agent_url,
    "api_mode": "chat_completions",
    "context_length": 64000,
})
data["model"] = model

providers = data.get("providers")
if not isinstance(providers, dict):
    providers = {}
providers["smartnpc-agent"] = {
    "name": "smartnpc-agent",
    "base_url": agent_url,
    "key_env": "HERMES_AGENT_API_KEY",
    "default_model": agent_model,
    "api_mode": "chat_completions",
    "discover_models": False,
}
data["providers"] = providers

# compression: union with existing.
comp = data.get("compression")
if not isinstance(comp, dict):
    comp = {}
comp.update({
    "enabled": True,
    "threshold": 0.15,
    "target_ratio": 0.10,
    "protect_last_n": 8,
    "hygiene_hard_message_limit": 60,
})
data["compression"] = comp

# agent: turn off tool_use_enforcement so Hermes does NOT inject the
# "keep calling tools until task complete" / <tool_persistence> block
# into the system prompt. That injection competes with our SmartNPC
# "one chat_say per wake-up then stop" rule and produces the chat_say
# loop. Keeping the rest of `agent.*` (max_turns, max_iterations, ...)
# untouched.
agent = data.get("agent")
if not isinstance(agent, dict):
    agent = {}
agent["tool_use_enforcement"] = False
data["agent"] = agent

# Write back. sort_keys=False preserves authoring order on top-level
# keys we did not touch; PyYAML will reorder dict insertion order on
# nested mutated dicts but Hermes does not care about ordering.
new_text = yaml.safe_dump(data, sort_keys=False, allow_unicode=True, width=4096)
path.write_text(new_text, encoding="utf-8")
print(f"  - wrote SmartNPC model/provider env refs + compression.* + agent.tool_use_enforcement=false → {path}")
PY
  applied=$((applied + 1))
done

echo
echo "summary: $applied profile(s) tuned, $skipped skipped"

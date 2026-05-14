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
#   model.context_length = 64000
#   compression.enabled = true
#   compression.threshold = 0.15
#   compression.target_ratio = 0.10
#   compression.protect_last_n = 8
#   compression.hygiene_hard_message_limit = 60
#
# Usage:
#   bash scripts/apply_hermes_tuning.sh                    # all 6 profiles
#   bash scripts/apply_hermes_tuning.sh xiami abigail      # specific profiles
#   HERMES_HOME=/custom/path bash scripts/apply_hermes_tuning.sh

set -euo pipefail

HERMES_HOME="${HERMES_HOME:-$HOME/.hermes}"
PROFILES_DIR="$HERMES_HOME/profiles"

ALL_PROFILES=(xiami abigail haley harvey penny sebastian)
targets=("$@")
if [[ ${#targets[@]} -eq 0 ]]; then
  targets=("${ALL_PROFILES[@]}")
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
import sys, yaml, pathlib

path = pathlib.Path(sys.argv[1])
text = path.read_text(encoding="utf-8")
data = yaml.safe_load(text) or {}

# model: keep every existing field; just set context_length.
model = data.get("model")
if not isinstance(model, dict):
    model = {} if model is None else {"_legacy": model}
model["context_length"] = 64000
data["model"] = model

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

# Write back. sort_keys=False preserves authoring order on top-level
# keys we did not touch; PyYAML will reorder dict insertion order on
# nested mutated dicts but Hermes does not care about ordering.
new_text = yaml.safe_dump(data, sort_keys=False, allow_unicode=True, width=4096)
path.write_text(new_text, encoding="utf-8")
print(f"  - wrote model.context_length=64000 + compression.* → {path}")
PY
  applied=$((applied + 1))
done

echo
echo "summary: $applied profile(s) tuned, $skipped skipped"

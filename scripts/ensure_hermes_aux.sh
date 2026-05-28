#!/usr/bin/env bash
# Idempotently ensure ~/.hermes/config.yaml routes the session_search task
# to gpt-4o-mini via the Venus llmproxy. Hermes' built-in session
# summarizer hardcodes temperature=0.1, which the default gpt-5.5 model
# rejects ("Only the default (1) value is supported"); using gpt-4o-mini
# avoids that error without disabling the feature.
#
# Idempotent: re-running has no effect when the config is already in the
# desired state. Comments / formatting elsewhere in the YAML are preserved
# because we patch only the three lines inside the session_search block.

set -euo pipefail

CONFIG="${HERMES_CONFIG:-$HOME/.hermes/config.yaml}"

if [[ ! -f "$CONFIG" ]]; then
    echo "[ensure-hermes-aux] $CONFIG not found; skipping" >&2
    exit 0
fi

PERL_SCRIPT='
my $in = 0;
while (<>) {
    if (/^  session_search:/)      { $in = 1 }
    elsif (/^  [A-Za-z]/ && $in)   { $in = 0 }
    if ($in) {
        s|^(    provider: ).*|${1}custom|;
        s|^(    model: ).*|${1}gpt-4o-mini|;
        s|^(    base_url: ).*|;
    }
    print;
}
'

TMP=$(mktemp)
perl -e "$PERL_SCRIPT" "$CONFIG" > "$TMP"

if cmp -s "$CONFIG" "$TMP"; then
    rm -f "$TMP"
    echo "[ensure-hermes-aux] session_search already gpt-4o-mini, no change"
    exit 0
fi

cp "$CONFIG" "$CONFIG.bak.$(date +%s)"
mv "$TMP" "$CONFIG"

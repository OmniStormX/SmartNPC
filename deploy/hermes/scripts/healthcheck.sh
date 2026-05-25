#!/usr/bin/env bash
# healthcheck.sh — Check all Hermes gateways are healthy.
# Usage: bash scripts/healthcheck.sh [host]
#   host defaults to localhost

set -euo pipefail

HOST="${1:-localhost}"
PORTS=(8642 8643 8644 8645 8646 8647)
NAMES=(xiami abigail haley harvey penny sebastian)
FAILED=0

for i in "${!PORTS[@]}"; do
    port="${PORTS[$i]}"
    name="${NAMES[$i]}"
    if curl -sf "http://${HOST}:${port}/health" >/dev/null 2>&1; then
        echo "  ✓ ${name} (:${port}) healthy"
    else
        echo "  ✗ ${name} (:${port}) UNHEALTHY"
        FAILED=$((FAILED + 1))
    fi
done

if [ "$FAILED" -gt 0 ]; then
    echo "FAIL: $FAILED gateway(s) unhealthy"
    exit 1
fi

echo "All gateways healthy."

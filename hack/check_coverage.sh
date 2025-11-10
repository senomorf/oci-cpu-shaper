#!/usr/bin/env bash
set -euo pipefail

THRESHOLD="${1:-30}"

if [ ! -f coverage.out ]; then
    echo "coverage.out not found. Run 'make coverage' first." >&2
    exit 1
fi

TOTAL=$(go tool cover -func=coverage.out | awk '/^total:/ {print $NF}')
if [ -z "$TOTAL" ]; then
    echo "Unable to determine total coverage from coverage.out" >&2
    exit 1
fi

COVERAGE="${TOTAL%\%}"

COMPARE=$(awk -v cov="$COVERAGE" -v thr="$THRESHOLD" 'BEGIN { if (cov+0 < thr+0) { print "lt"; } else { print "ge"; } }')

if [ "$COMPARE" = "lt" ]; then
    echo "Coverage ${COVERAGE}% is below the required threshold of ${THRESHOLD}%." >&2
    exit 1
fi

echo "Coverage ${COVERAGE}% meets the ${THRESHOLD}% threshold."

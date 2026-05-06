#!/usr/bin/env bash
# Sweep read and write rates against the local stack to find each path's ceiling.
# Each run is a fresh k6 invocation (own setup, own propagation wait).
set -euo pipefail

cd "$(dirname "$0")/.."

GATEWAY="${GATEWAY:-http://localhost:8080}"
DURATION="${DURATION:-10s}"
SEED_COUNT="${SEED_COUNT:-500}"
READ_RATES=(${READ_RATES:-2000 5000 10000 20000})
WRITE_RATES=(${WRITE_RATES:-500 1000 2000 5000})

command -v k6 >/dev/null 2>&1 || { echo "k6 not found; install with: brew install k6"; exit 1; }

if ! curl -sf "$GATEWAY/healthz" >/dev/null; then
  echo "gateway not responding at $GATEWAY/healthz — bring the stack up with 'make dev-up'"
  exit 1
fi

run() {
  local scenario=$1 ratevar=$2 rate=$3
  printf '\n\n=== %s @ %s RPS for %s ===\n' "$scenario" "$rate" "$DURATION"
  if ! k6 run \
      --env GATEWAY="$GATEWAY" \
      --env SCENARIO="$scenario" \
      --env "$ratevar=$rate" \
      --env SEED_COUNT="$SEED_COUNT" \
      --env DURATION="$DURATION" \
      tests/load/k6.js; then
    echo "  (thresholds crossed or run failed — continuing)"
  fi
}

echo "load-sweep against $GATEWAY (duration=$DURATION, seed=$SEED_COUNT)"
echo "read rates:  ${READ_RATES[*]}"
echo "write rates: ${WRITE_RATES[*]}"

for rate in "${READ_RATES[@]}";  do run read  READ_RATE  "$rate"; done
for rate in "${WRITE_RATES[@]}"; do run write WRITE_RATE "$rate"; done

echo
echo "done. inspect each scenario's final summary above."

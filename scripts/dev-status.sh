#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PIDS="$ROOT/.run/pids"
LOGS="$ROOT/.run/logs"

printf "%-22s %-8s %s\n" SERVICE PID STATUS
printf "%-22s %-8s %s\n" "------" "---" "------"

if docker ps --filter name=^kafka-dev$ --format '{{.Names}}' 2>/dev/null | grep -qx kafka-dev; then
  printf "%-22s %-8s %s\n" kafka-dev "(docker)" "running"
else
  printf "%-22s %-8s %s\n" kafka-dev "(docker)" "not running"
fi

if [[ -d "$PIDS" ]]; then
  for pidfile in "$PIDS"/*.pid; do
    [[ -f "$pidfile" ]] || continue
    name=$(basename "$pidfile" .pid)
    pid=$(cat "$pidfile")
    if kill -0 "$pid" 2>/dev/null; then
      printf "%-22s %-8s %s\n" "$name" "$pid" "alive"
    else
      printf "%-22s %-8s %s\n" "$name" "$pid" "dead (stale pidfile)"
    fi
  done
fi

echo
echo "Logs in $LOGS/"
echo "Tail:  tail -f $LOGS/<service>.log"

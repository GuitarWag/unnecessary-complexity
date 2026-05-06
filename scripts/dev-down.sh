#!/usr/bin/env bash
# Stop everything dev-up.sh started. Set KEEP_KAFKA=1 to leave the Kafka container running.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PIDS="$ROOT/.run/pids"

log() { printf "\033[36m==> %s\033[0m\n" "$*"; }

if [[ -d "$PIDS" ]]; then
  for pidfile in "$PIDS"/*.pid; do
    [[ -f "$pidfile" ]] || continue
    name=$(basename "$pidfile" .pid)
    pid=$(cat "$pidfile")
    if kill -0 "$pid" 2>/dev/null; then
      log "stopping $name (pid $pid)"
      kill "$pid" 2>/dev/null || true
      for _ in $(seq 1 20); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.25
      done
      kill -9 "$pid" 2>/dev/null || true
    fi
    rm -f "$pidfile"
  done
fi

if [[ "${KEEP_KAFKA:-}" != "1" ]] && \
   docker ps --filter name=^kafka-dev$ --format '{{.Names}}' 2>/dev/null | grep -qx kafka-dev; then
  log "stopping kafka-dev (set KEEP_KAFKA=1 to skip)"
  docker stop kafka-dev >/dev/null
fi

log "done"

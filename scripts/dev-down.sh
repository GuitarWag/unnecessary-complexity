#!/usr/bin/env bash
# Stop everything dev-up.sh started. Set KEEP_KAFKA=1 to leave the Kafka container running.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
PIDS="$ROOT/.run/pids"

log() { printf "\033[36m==> %s\033[0m\n" "$*"; }

# kill_tree signal pid — sends signal to pid and its descendants. The frontend
# remotes are launched as `pnpm --filter <app> run dev`, where pnpm spawns
# rsbuild-node as a child. Killing only pnpm orphans rsbuild-node, leaving the
# port held until the next reboot. Walking children fixes that.
kill_tree() {
  local sig=$1 pid=$2
  local child
  for child in $(pgrep -P "$pid" 2>/dev/null); do
    kill_tree "$sig" "$child"
  done
  kill "$sig" "$pid" 2>/dev/null || true
}

if [[ -d "$PIDS" ]]; then
  for pidfile in "$PIDS"/*.pid; do
    [[ -f "$pidfile" ]] || continue
    name=$(basename "$pidfile" .pid)
    pid=$(cat "$pidfile")
    if kill -0 "$pid" 2>/dev/null; then
      log "stopping $name (pid $pid)"
      kill_tree -TERM "$pid"
      for _ in $(seq 1 20); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.25
      done
      kill_tree -KILL "$pid"
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

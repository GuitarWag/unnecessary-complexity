#!/usr/bin/env bash
# Bring the full local dev stack up: Kafka in Docker, all backends as Go processes, all
# frontend dev servers via pnpm. State (PIDs, logs, sqlite) lives in .run/.
#
#   scripts/dev-up.sh                      # everything
#   MINIMAL_FRONTEND=1 scripts/dev-up.sh   # only shell + mf-url-input
#   SKIP_FRONTEND=1   scripts/dev-up.sh    # backends only
#   SKIP_KAFKA=1      scripts/dev-up.sh    # assume Kafka is already on localhost:9092
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUN="$ROOT/.run"
LOGS="$RUN/logs"
PIDS="$RUN/pids"
BIN="$RUN/bin"
DATA="$RUN/data"
mkdir -p "$LOGS" "$PIDS" "$BIN" "$DATA"

log()  { printf "\033[36m==> %s\033[0m\n" "$*"; }
warn() { printf "\033[33m!! %s\033[0m\n" "$*"; }
die()  { printf "\033[31m!! %s\033[0m\n" "$*" >&2; exit 1; }

require() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }
require docker
require go
require pnpm
require curl

is_alive() {
  local pidfile="$PIDS/$1.pid"
  [[ -f "$pidfile" ]] && kill -0 "$(cat "$pidfile")" 2>/dev/null
}

start() {
  local name=$1; shift
  if is_alive "$name"; then
    log "$name already running (pid $(cat "$PIDS/$name.pid"))"
    return
  fi
  log "starting $name"
  ( "$@" ) >"$LOGS/$name.log" 2>&1 &
  echo $! > "$PIDS/$name.pid"
}

wait_tcp() {
  local host=$1 port=$2 name=${3:-$host:$port} timeout=${4:-30}
  for _ in $(seq 1 "$timeout"); do
    if (exec 3<>"/dev/tcp/$host/$port") 2>/dev/null; then
      exec 3<&-
      return 0
    fi
    sleep 1
  done
  die "$name not reachable at $host:$port after ${timeout}s"
}

wait_http_ok() {
  local url=$1 name=${2:-$url} timeout=${3:-30}
  for _ in $(seq 1 "$timeout"); do
    if curl -fsS --max-time 2 "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  die "$name healthcheck failed at $url after ${timeout}s"
}

# 1) Kafka
if [[ -z "${SKIP_KAFKA:-}" ]]; then
  if docker ps --filter name=^kafka-dev$ --format '{{.Names}}' | grep -qx kafka-dev; then
    log "kafka-dev already running"
  elif docker ps -a --filter name=^kafka-dev$ --format '{{.Names}}' | grep -qx kafka-dev; then
    log "starting existing kafka-dev container"
    docker start kafka-dev >/dev/null
  else
    log "creating kafka-dev container"
    docker run -d --name kafka-dev -p 9092:9092 \
      -e KAFKA_NODE_ID=1 \
      -e KAFKA_PROCESS_ROLES=broker,controller \
      -e KAFKA_LISTENERS=PLAINTEXT://:9092,CONTROLLER://:9093 \
      -e KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:9092 \
      -e KAFKA_CONTROLLER_LISTENER_NAMES=CONTROLLER \
      -e KAFKA_LISTENER_SECURITY_PROTOCOL_MAP=CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT \
      -e KAFKA_CONTROLLER_QUORUM_VOTERS=1@localhost:9093 \
      -e KAFKA_INTER_BROKER_LISTENER_NAME=PLAINTEXT \
      -e KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR=1 \
      -e KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR=1 \
      -e KAFKA_TRANSACTION_STATE_LOG_MIN_ISR=1 \
      -e KAFKA_AUTO_CREATE_TOPICS_ENABLE=true \
      apache/kafka:3.7.0 >/dev/null
  fi
  wait_tcp localhost 9092 "kafka" 60

  log "ensuring topics exist"
  for topic in short_urls.created clicks.recorded; do
    docker exec kafka-dev /opt/kafka/bin/kafka-topics.sh \
      --bootstrap-server localhost:9092 \
      --create --if-not-exists \
      --topic "$topic" \
      --partitions 1 --replication-factor 1 >/dev/null 2>&1 || \
      warn "could not create topic $topic (may already exist)"
  done
fi

# 2) Build Go binaries
log "building Go services"
for svc in shortener resolver analytics gateway; do
  (cd "$ROOT/services/$svc" && go build -o "$BIN/$svc" ./cmd/server)
done

# 3) Backends
start shortener env \
  SHORTENER_ADDR=:50051 \
  SHORTENER_DB="$DATA/shortener.db" \
  SHORTENER_KAFKA_BROKERS=localhost:9092 \
  "$BIN/shortener"

start resolver env \
  RESOLVER_ADDR=:50052 \
  RESOLVER_HTTP_ADDR=:50080 \
  RESOLVER_DB="$DATA/resolver.db" \
  RESOLVER_KAFKA_BROKERS=localhost:9092 \
  "$BIN/resolver"

start analytics env \
  ANALYTICS_ADDR=:50053 \
  ANALYTICS_DB="$DATA/analytics.db" \
  ANALYTICS_KAFKA_BROKERS=localhost:9092 \
  "$BIN/analytics"

wait_tcp localhost 50051 "shortener gRPC" 30
wait_tcp localhost 50052 "resolver gRPC" 30
wait_tcp localhost 50053 "analytics gRPC" 30

start gateway env \
  GATEWAY_ADDR=:8080 \
  GATEWAY_SHORTENER_ADDR=localhost:50051 \
  GATEWAY_RESOLVER_ADDR=localhost:50052 \
  GATEWAY_ANALYTICS_ADDR=localhost:50053 \
  GATEWAY_ALLOWED_ORIGINS="http://localhost:5173,http://localhost:5174,http://localhost:5175,http://localhost:5176,http://localhost:5177,http://localhost:5178" \
  "$BIN/gateway"

wait_http_ok "http://localhost:8080/healthz" "gateway" 30

# 4) Frontend
if [[ -z "${SKIP_FRONTEND:-}" ]]; then
  log "ensuring frontend deps"
  (cd "$ROOT/frontend" && pnpm install --silent --prefer-offline >>"$LOGS/pnpm-install.log" 2>&1)

  if [[ -n "${MINIMAL_FRONTEND:-}" ]]; then
    apps=(shell mf-url-input)
  else
    apps=(shell mf-url-input mf-create-button mf-copy-button mf-url-list mf-analytics-chart)
  fi

  for app in "${apps[@]}"; do
    start "fe-$app" bash -lc "cd '$ROOT/frontend' && pnpm --filter @url-shortener/$app run dev"
  done
fi

cat <<EOF

URLs:
  Shell UI         http://localhost:5173
  Gateway (Connect) http://localhost:8080
  Healthz          http://localhost:8080/healthz

Quick smoke test:
  curl -s -X POST http://localhost:8080/shortener.v1.ShortenerService/Shorten \\
       -H 'Content-Type: application/json' \\
       -d '{"longUrl":"https://example.com/hello"}'

Logs:    $LOGS/
PIDs:    $PIDS/
Status:  scripts/dev-status.sh
Stop:    scripts/dev-down.sh
EOF

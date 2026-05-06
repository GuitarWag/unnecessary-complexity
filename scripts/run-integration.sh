#!/usr/bin/env bash
# Run integration tests against a freshly-deployed kind cluster.
#
# Usage:
#   scripts/run-integration.sh           # full lifecycle: build, deploy, test, teardown port-forward
#   SKIP_BUILD=1 scripts/run-integration.sh
#   SKIP_DEPLOY=1 scripts/run-integration.sh
#   KEEP_CLUSTER=1 scripts/run-integration.sh   # do not delete kind cluster on exit
#
# Env knobs:
#   CLUSTER_NAME              kind cluster name (default: url-shortener)
#   GATEWAY_LOCAL_PORT        local port for port-forward (default: 18080)
#   READY_TIMEOUT             how long to wait for pods (default: 300s)
#   PROPAGATION_TIMEOUT       passed to tests as INTEGRATION_PROPAGATION_TIMEOUT (default: 60s)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CLUSTER_NAME="${CLUSTER_NAME:-url-shortener}"
GATEWAY_LOCAL_PORT="${GATEWAY_LOCAL_PORT:-18080}"
READY_TIMEOUT="${READY_TIMEOUT:-300s}"
PROPAGATION_TIMEOUT="${PROPAGATION_TIMEOUT:-60s}"
NS="url-shortener"

require() {
  command -v "$1" >/dev/null 2>&1 || { echo "missing required tool: $1" >&2; exit 1; }
}

require kind
require kubectl
require docker
require go

echo "==> ensuring kind cluster '$CLUSTER_NAME' is up"
OWN_CLUSTER=0
if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  CLUSTER_NAME="$CLUSTER_NAME" "$ROOT/scripts/kind-up.sh"
  OWN_CLUSTER=1
fi

if [[ -z "${SKIP_BUILD:-}" ]]; then
  echo "==> building images"
  make docker-build
  echo "==> loading images into kind"
  make kind-load KIND_CLUSTER="$CLUSTER_NAME"
fi

if [[ -z "${SKIP_DEPLOY:-}" ]]; then
  echo "==> applying manifests"
  kubectl apply -k k8s/base
fi

echo "==> waiting for Kafka and core deployments"
kubectl -n "$NS" rollout status statefulset/kafka --timeout="$READY_TIMEOUT"
for d in shortener resolver analytics gateway; do
  kubectl -n "$NS" rollout status deployment/"$d" --timeout="$READY_TIMEOUT"
done

PF_PID=""
cleanup() {
  if [[ -n "$PF_PID" ]] && kill -0 "$PF_PID" 2>/dev/null; then
    echo "==> stopping port-forward (pid $PF_PID)"
    kill "$PF_PID" || true
    wait "$PF_PID" 2>/dev/null || true
  fi
  if [[ -z "${KEEP_CLUSTER:-}" && "${OWN_CLUSTER:-0}" == "1" ]]; then
    echo "==> deleting kind cluster '$CLUSTER_NAME'"
    kind delete cluster --name "$CLUSTER_NAME" || true
  fi
}
trap cleanup EXIT

echo "==> port-forwarding gateway → localhost:$GATEWAY_LOCAL_PORT"
kubectl -n "$NS" port-forward svc/gateway "$GATEWAY_LOCAL_PORT:8080" >/tmp/integration-pf.log 2>&1 &
PF_PID=$!

# Wait for the port-forward to actually accept connections.
for _ in $(seq 1 30); do
  if curl -fsS "http://localhost:$GATEWAY_LOCAL_PORT/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo "==> running integration tests"
INTEGRATION_GATEWAY_URL="http://localhost:$GATEWAY_LOCAL_PORT" \
  INTEGRATION_PROPAGATION_TIMEOUT="$PROPAGATION_TIMEOUT" \
  go test -v -tags=integration -count=1 -timeout=10m \
  ./tests/integration/...

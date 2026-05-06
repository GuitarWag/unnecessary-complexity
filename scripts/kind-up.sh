#!/usr/bin/env bash
# Bring up a local kind cluster with nginx-ingress installed and ingress mapped to host ports 80/443.
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-url-shortener}"
KIND_CONFIG="$(dirname "$0")/kind-config.yaml"

if ! command -v kind >/dev/null 2>&1; then
  echo "kind is required. Install: https://kind.sigs.k8s.io/" >&2
  exit 1
fi
if ! command -v kubectl >/dev/null 2>&1; then
  echo "kubectl is required." >&2
  exit 1
fi

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  echo "kind cluster '$CLUSTER_NAME' already exists; using it."
else
  echo "Creating kind cluster '$CLUSTER_NAME' ..."
  kind create cluster --name "$CLUSTER_NAME" --config "$KIND_CONFIG"
fi

echo "Installing nginx-ingress controller ..."
kubectl apply -f https://raw.githubusercontent.com/kubernetes/ingress-nginx/controller-v1.11.3/deploy/static/provider/kind/deploy.yaml

echo "Waiting for nginx-ingress to be ready ..."
kubectl wait --namespace ingress-nginx \
  --for=condition=ready pod \
  --selector=app.kubernetes.io/component=controller \
  --timeout=180s

cat <<EOF

Cluster ready. Add this to /etc/hosts to use the host-based ingress:
  127.0.0.1  url-shortener.local

Then deploy the app:
  make docker-build
  make kind-load
  make k8s-apply

EOF

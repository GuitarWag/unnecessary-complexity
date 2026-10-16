#!/usr/bin/env bash
# Configure Vault for the url-shortener namespace. Safe to run again, and run it
# again after the Vault pod restarts, because dev mode keeps data in memory.
#
# Vault is the source of truth for the Postgres password:
#   - keep the password Vault already holds;
#   - else reuse the one ESO last synced into the k8s Secret;
#   - else generate one.
# If postgres-0 already runs, set its password to match. The official image
# trusts local socket connections, so this needs no password.
set -euo pipefail

NS=url-shortener
SECRET_PATH=secret/url-shortener/postgres

v() { kubectl -n vault exec -i deploy/vault -- env VAULT_ADDR=http://127.0.0.1:8200 VAULT_TOKEN=root vault "$@"; }

kubectl -n vault rollout status deployment/vault --timeout=180s

if ! v auth list -format=json | grep -q '"kubernetes/"'; then
  v auth enable kubernetes
fi
# In a pod, Vault reviews tokens with its own service-account token and CA.
v write auth/kubernetes/config kubernetes_host=https://kubernetes.default.svc >/dev/null

v policy write url-shortener-read - >/dev/null <<'POLICY'
path "secret/data/url-shortener/*" {
  capabilities = ["read"]
}
POLICY

v write auth/kubernetes/role/url-shortener \
  bound_service_account_names=vault-reader \
  bound_service_account_namespaces="$NS" \
  policies=url-shortener-read \
  ttl=1h >/dev/null

if v kv get -field=password "$SECRET_PATH" >/dev/null 2>&1; then
  password=$(v kv get -field=password "$SECRET_PATH")
  echo "vault: keeping the existing Postgres password"
else
  password=$(kubectl -n "$NS" get secret postgres-credentials -o jsonpath='{.data.password}' 2>/dev/null | base64 -d || true)
  if [[ -n "$password" ]]; then
    echo "vault: restoring the Postgres password from the synced Secret"
  else
    password=$(openssl rand -hex 24)
    echo "vault: generated a new Postgres password"
  fi
  # Through stdin, so the password never appears in a process listing.
  printf '%s' "$password" | v kv put "$SECRET_PATH" username=postgres password=- >/dev/null
fi

if kubectl -n "$NS" get pod postgres-0 >/dev/null 2>&1; then
  printf "ALTER USER postgres PASSWORD '%s';\n" "$password" |
    kubectl -n "$NS" exec -i postgres-0 -- psql -U postgres -v ON_ERROR_STOP=1 -q
  echo "postgres: password set to match Vault"
fi

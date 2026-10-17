SHELL := /usr/bin/env bash
GOBIN := $(shell go env GOPATH)/bin
PATH  := $(GOBIN):$(PATH)
export PATH

# Tool versions (pinned).
BUF_VERSION             := v1.47.2
PROTOC_GEN_GO_VERSION   := v1.35.2
PROTOC_GEN_GRPC_VERSION := v1.5.1
GOLANGCI_LINT_VERSION   := v1.64.8

GO_SERVICES   := shortener resolver analytics gateway loadgen
GO_LIBS       := platform-events
GO_MODULES    := $(GO_SERVICES) $(GO_LIBS)

.PHONY: tools
tools: tool-buf tool-protoc-gen-go tool-protoc-gen-go-grpc tool-golangci-lint

.PHONY: tool-buf
tool-buf:
	@command -v buf >/dev/null 2>&1 || \
		go install github.com/bufbuild/buf/cmd/buf@$(BUF_VERSION)

.PHONY: tool-protoc-gen-go
tool-protoc-gen-go:
	@command -v protoc-gen-go >/dev/null 2>&1 || \
		go install google.golang.org/protobuf/cmd/protoc-gen-go@$(PROTOC_GEN_GO_VERSION)

.PHONY: tool-protoc-gen-go-grpc
tool-protoc-gen-go-grpc:
	@command -v protoc-gen-go-grpc >/dev/null 2>&1 || \
		go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@$(PROTOC_GEN_GRPC_VERSION)

.PHONY: tool-golangci-lint
tool-golangci-lint:
	@command -v golangci-lint >/dev/null 2>&1 || \
		go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: proto
proto: tool-buf tool-protoc-gen-go tool-protoc-gen-go-grpc
	cd proto && buf generate

.PHONY: proto-lint
proto-lint: tool-buf
	cd proto && buf lint

# Throwaway Postgres for the shortener tests. Each test creates its own database
# on it. wal_level=logical lets the outbox test read the WAL.
TEST_PG_PORT ?= 55432
export SHORTENER_TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:$(TEST_PG_PORT)/postgres?sslmode=disable

.PHONY: test-pg
test-pg:
	@docker ps --filter name=^url-shortener-test-pg$$ --format '{{.Names}}' | grep -qx url-shortener-test-pg || \
		docker run -d --rm --name url-shortener-test-pg -p $(TEST_PG_PORT):5432 \
			-e POSTGRES_PASSWORD=postgres postgres:16-alpine -c wal_level=logical >/dev/null
	@until docker exec url-shortener-test-pg pg_isready -U postgres -h localhost >/dev/null 2>&1; do sleep 0.5; done

.PHONY: test
test: test-pg
	@for s in $(GO_MODULES); do \
		echo "==> test services/$$s"; \
		(cd services/$$s && go test ./... -race -count=1) || exit $$?; \
	done

.PHONY: lint
lint: tool-golangci-lint
	@for s in $(GO_MODULES); do \
		echo "==> lint services/$$s"; \
		(cd services/$$s && golangci-lint run ./...) || exit $$?; \
	done

.PHONY: tidy
tidy:
	@for s in $(GO_MODULES); do \
		(cd services/$$s && go mod tidy); \
	done

.PHONY: build
build:
	@for s in $(GO_MODULES); do \
		echo "==> build services/$$s"; \
		(cd services/$$s && go build ./...) || exit $$?; \
	done

.PHONY: ci
ci: proto-lint lint test build

# ----- Docker -----

DOCKER_TAG       ?= dev
DOCKER_REGISTRY  ?= url-shortener
GO_IMAGES        := shortener resolver analytics gateway loadgen
FE_IMAGES        := shell mf-url-input mf-create-button mf-copy-button mf-url-list mf-analytics-chart mf-load-test

.PHONY: docker-build
docker-build: docker-build-go docker-build-fe

.PHONY: docker-build-go
docker-build-go:
	@for s in $(GO_IMAGES); do \
		echo "==> docker build $$s"; \
		docker build -f services/Dockerfile.go --build-arg SERVICE=$$s \
			-t $(DOCKER_REGISTRY)/$$s:$(DOCKER_TAG) services || exit $$?; \
	done

.PHONY: docker-build-fe
docker-build-fe:
	@for a in $(FE_IMAGES); do \
		echo "==> docker build $$a"; \
		docker build -f frontend/Dockerfile --build-arg APP=$$a \
			-t $(DOCKER_REGISTRY)/$$a:$(DOCKER_TAG) frontend || exit $$?; \
	done

# ----- kind / k8s -----

KIND_CLUSTER ?= url-shortener

.PHONY: kind-up
kind-up:
	./scripts/kind-up.sh

.PHONY: kind-down
kind-down:
	kind delete cluster --name $(KIND_CLUSTER)

.PHONY: kind-load
kind-load:
	@for s in $(GO_IMAGES) $(FE_IMAGES); do \
		echo "==> kind load $$s"; \
		kind load docker-image $(DOCKER_REGISTRY)/$$s:$(DOCKER_TAG) --name $(KIND_CLUSTER) || exit $$?; \
	done

# ----- Secrets (Vault + External Secrets Operator) -----
# k8s/base reads the Postgres password from a Secret that ESO syncs from Vault,
# so this must run before k8s-apply. Run it again after the Vault pod restarts.

ESO_CHART_VERSION ?= 2.12.0

.PHONY: secrets-up
secrets-up:
	helm repo add external-secrets https://charts.external-secrets.io >/dev/null 2>&1 || true
	helm upgrade --install external-secrets external-secrets/external-secrets \
		--version $(ESO_CHART_VERSION) -n external-secrets --create-namespace --wait --timeout 5m
	kubectl apply -k k8s/secrets
	./scripts/vault-bootstrap.sh

.PHONY: k8s-apply
k8s-apply:
	kubectl apply -k k8s/base

.PHONY: k8s-delete
k8s-delete:
	kubectl delete -k k8s/base --ignore-not-found

.PHONY: k8s-up
k8s-up: docker-build kind-load secrets-up k8s-apply obs-up

# ----- Observability (Prometheus, Loki, Grafana) -----

.PHONY: obs-up obs-down grafana
obs-up:
	kubectl apply -k k8s/observability
	@for d in prometheus kube-state-metrics loki alloy grafana; do \
		kubectl -n observability rollout status deployment/$$d --timeout=300s || exit $$?; \
	done
	@echo "Grafana: run 'make grafana', then open http://localhost:3000"

obs-down:
	kubectl delete -k k8s/observability --ignore-not-found

grafana:
	kubectl -n observability port-forward svc/grafana 3000:3000

# ----- Usage simulator -----
# Runs on this machine and enters the cluster through the kind ingress on
# localhost:80. Pass flags with SIM_ARGS, for example:
#   make simulate SIM_ARGS="-users 100 -duration 5m"
#   make simulate SIM_ARGS="-target http://localhost:8080 -host ''"   # against Tilt

SIM_ARGS ?=

.PHONY: simulate simulate-chaos
simulate:
	cd services/loadgen && go run ./cmd/simulate $(SIM_ARGS)

# Random drift, flash-crowd spikes (3-8x users, one viral link) and lulls.
simulate-chaos:
	cd services/loadgen && go run ./cmd/simulate -chaos $(SIM_ARGS)

# ----- Integration tests -----

# Local dev runs on Tilt (Tiltfile + compose.yaml). UI at http://localhost:10350.
.PHONY: dev-up dev-down
dev-up:
	tilt up

dev-down:
	tilt down

.PHONY: integration-test
integration-test:
	./scripts/run-integration.sh

.PHONY: integration-test-only
integration-test-only:
	@if [ -z "$$INTEGRATION_GATEWAY_URL" ]; then \
		echo "INTEGRATION_GATEWAY_URL not set; pointing at http://localhost:18080"; \
	fi
	cd tests/integration && go test -v -tags=integration -count=1 -timeout=10m ./...

# ----- Load tests (k6) -----

LOAD_GATEWAY    ?= http://localhost:8080
LOAD_SEED_COUNT ?= 500
LOAD_DURATION   ?= 30s

.PHONY: load-test load-test-read load-test-write load-test-mixed

load-test: load-test-mixed

load-test-read:
	@command -v k6 >/dev/null 2>&1 || { echo "k6 not found; install with: brew install k6"; exit 1; }
	k6 run \
		--env GATEWAY=$(LOAD_GATEWAY) \
		--env SEED_COUNT=$(LOAD_SEED_COUNT) \
		--env DURATION=$(LOAD_DURATION) \
		--env SCENARIO=read \
		tests/load/k6.js

load-test-write:
	@command -v k6 >/dev/null 2>&1 || { echo "k6 not found; install with: brew install k6"; exit 1; }
	k6 run \
		--env GATEWAY=$(LOAD_GATEWAY) \
		--env DURATION=$(LOAD_DURATION) \
		--env SCENARIO=write \
		tests/load/k6.js

load-test-mixed:
	@command -v k6 >/dev/null 2>&1 || { echo "k6 not found; install with: brew install k6"; exit 1; }
	k6 run \
		--env GATEWAY=$(LOAD_GATEWAY) \
		--env SEED_COUNT=$(LOAD_SEED_COUNT) \
		--env DURATION=$(LOAD_DURATION) \
		--env SCENARIO=mixed \
		tests/load/k6.js

.PHONY: load-sweep
load-sweep:
	GATEWAY=$(LOAD_GATEWAY) DURATION=$(LOAD_DURATION) SEED_COUNT=$(LOAD_SEED_COUNT) ./scripts/load-sweep.sh

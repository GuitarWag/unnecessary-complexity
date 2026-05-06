SHELL := /usr/bin/env bash
GOBIN := $(shell go env GOPATH)/bin
PATH  := $(GOBIN):$(PATH)
export PATH

# Tool versions (pinned).
BUF_VERSION             := v1.47.2
PROTOC_GEN_GO_VERSION   := v1.35.2
PROTOC_GEN_GRPC_VERSION := v1.5.1
GOLANGCI_LINT_VERSION   := v1.64.8

GO_SERVICES   := shortener resolver analytics gateway
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

.PHONY: test
test:
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
GO_IMAGES        := shortener resolver analytics gateway
FE_IMAGES        := shell mf-url-input mf-create-button mf-copy-button mf-url-list mf-analytics-chart

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

.PHONY: k8s-apply
k8s-apply:
	kubectl apply -k k8s/base

.PHONY: k8s-delete
k8s-delete:
	kubectl delete -k k8s/base --ignore-not-found

.PHONY: k8s-up
k8s-up: docker-build kind-load k8s-apply

# ----- Integration tests -----

.PHONY: dev-up dev-down dev-status
dev-up:
	./scripts/dev-up.sh

dev-down:
	./scripts/dev-down.sh

dev-status:
	./scripts/dev-status.sh

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

LOAD_GATEWAY    ?= http://localhost:18080
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

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A deliberately over-engineered URL shortener used as a teaching/demo project. It is event-sourced, multi-language, multi-process, and built so that pieces can be developed and reasoned about in isolation. The goal is to exercise serious patterns (DDD-flavoured services, Connect/gRPC, Kafka, Module Federation, k8s) on a problem small enough to fit in one repo.

## Commands

All from the repo root unless noted.

```
make tools             # one-time: installs buf, protoc plugins, golangci-lint into $(go env GOPATH)/bin
make proto             # regenerate Go + Connect stubs from proto/. After this, also run:
                       #   (cd frontend && pnpm --filter @url-shortener/proto-web run generate)

make ci                # buf lint + golangci-lint + go test -race + go build across every Go module
make test              # tests only (all Go modules)
make lint              # lint only (all Go modules)
make build             # build only

make dev-up            # local dev: Kafka in Docker + 4 backends + 6 frontend dev servers
make dev-down          # stop everything dev-up started
make dev-status        # who's alive
make integration-test  # spin up kind, deploy, port-forward, run tests/integration, tear down
```

Single-package and single-test runs (Go modules are independent — `cd` first):

```
cd services/shortener && go test ./internal/service -race -run TestShorten_SameKeyReturnsSameCode
cd services/gateway   && go test ./internal/handler -race -count=1
```

Frontend (always run from `frontend/`):

```
pnpm install
pnpm run lint                                     # Biome over the workspace
pnpm run test                                     # Vitest in every app/package
pnpm --filter @url-shortener/mf-url-input run test
pnpm --filter @url-shortener/shell run dev        # one app's dev server
```

`make dev-up` knobs: `MINIMAL_FRONTEND=1` (only shell + mf-url-input), `SKIP_FRONTEND=1`, `SKIP_KAFKA=1`. Process state lives in `.run/` (gitignored): `.run/logs/<service>.log`, `.run/pids/<service>.pid`, `.run/data/*.db`. `make integration-test-only` runs the test suite against an already-deployed gateway exposed at `INTEGRATION_GATEWAY_URL` (defaults to `http://localhost:18080`).

## Architecture

### Modules and how they're wired

Multi-module Go workspace via `go.work`. The proto stubs are their own module so every service depends on a single source of truth:

- `services/proto/` — generated `*.pb.go`, `*_grpc.pb.go`, and Connect handlers (`*v1connect/`). Never edit by hand; rerun `make proto`.
- `services/platform-events/` — Kafka publisher (`segmentio/kafka-go`), generic `Consumer[T proto.Message]`, in-memory `FakePublisher`/`KafkaWriter`/`KafkaReader` for tests, topic constants (`short_urls.created`, `clicks.recorded`).
- `services/shortener/` — write side. Owns the canonical `short_urls` SQLite. Publishes `ShortURLCreated` after every successful insert.
- `services/resolver/` — read side. Has its own SQLite, hydrated by consuming `short_urls.created`. On every successful resolve, asynchronously publishes `ClickRecorded`.
- `services/analytics/` — consumes `clicks.recorded` into a clicks fact table; exposes a `Stats` RPC with daily aggregation and date filters.
- `services/gateway/` — HTTP/Connect/gRPC-Web front door. Holds gRPC clients to all three backends, exposes them via Connect handlers (browser-compatible). Also owns the **`GET /<code>` 302 redirect** that turns short URLs into navigable links — this is the only HTTP handler outside the Connect surface.
- `tests/integration/` — end-to-end tests with `//go:build integration`; never compiled by `make ci`. Run via `make integration-test`.

### Data flow (event-sourced, deliberately decoupled)

1. Browser → gateway (Connect-Web POST) → shortener (gRPC).
2. Shortener writes its row, publishes `ShortURLCreated` (publish failure is logged, doesn't fail the RPC — write is the source of truth).
3. Resolver's consumer receives it, upserts its local cache.
4. Browser → `http://gateway/<code>` → gateway redirect handler → resolver `Resolve` → 302 to long URL. Resolver fires `ClickRecorded` async (5s timeout, separate context — caller's ctx is already done by then).
5. Analytics consumer receives `ClickRecorded`, inserts into clicks table.

This means: resolver and analytics are eventually consistent with shortener. Integration tests use `require.Eventually` with `INTEGRATION_PROPAGATION_TIMEOUT` (default 30s) to handle the lag.

### Shorten idempotency contract (non-obvious)

`ShortenRequest` carries optional `idempotency_key` and `owner_id`. The semantic is **not** value-based dedup:

- **No key** → every call creates a fresh row with a fresh code, even for the same URL.
- **Same `(owner_id, idempotency_key)`** → returns the previously-created row, `created: false`.
- **Different owners with the same key** → distinct rows. Key uniqueness is scoped to owner via a partial unique index (`WHERE idempotency_key IS NOT NULL`).
- **Race** (two concurrent calls, same `(owner, key)`) → the loser catches `ErrDuplicateIdempotencyKey` from the repo and re-reads to return the winner's code.

If you change this contract, update both unit tests in `services/shortener/internal/{repo,service}/` and the integration tests in `tests/integration/e2e_test.go` (`TestShorten_NoKeyAlwaysCreatesFreshRow`, `TestShorten_IdempotentWithKey`, `TestShorten_KeyScopedByOwner`).

### Frontend (Module Federation)

Six pnpm-workspace apps in `frontend/apps/`. `shell` is the host; the five `mf-*` are remotes. Wiring:

- Shell's `rsbuild.config.ts` declares the remotes and shares React/ReactDOM/React Router as singletons.
- Each remote runs on a fixed dev port (5174-5178 in this repo) with `assetPrefix` matching that origin so chunks resolve correctly when loaded by the shell at 5173.
- **Async bootstrap split is required** in the shell: `apps/shell/src/index.tsx` is one line (`import('./bootstrap.js')`), and the actual `createRoot(...).render(<App />)` lives in `bootstrap.tsx`. Without this you get Module Federation `RUNTIME-006` ("Invalid loadShareSync function call") and a blank page, because shared singletons can't satisfy a synchronous import before the federation runtime initializes.
- Remote contracts are declared in `apps/shell/src/remotes.d.ts` (TypeScript ambient module declarations). Update this file when remote prop signatures change.
- `frontend/packages/proto-web` exports `createClients({baseUrl})` returning `{shortener, resolver, analytics, transport}` Connect-Web clients. Every component that talks to the gateway accepts `clients?: Pick<Clients, '...'>` so tests can inject fakes.

### Kafka local dev gotcha

The `apache/kafka:3.7.0` image we use in `scripts/dev-up.sh` does **not** auto-create topics on first publish (the `KAFKA_AUTO_CREATE_TOPICS_ENABLE=true` env doesn't behave like Bitnami's). The script pre-creates `short_urls.created` and `clicks.recorded` after Kafka is up. If you add a new topic, register it in `services/platform-events/events/topics.go` *and* in `scripts/dev-up.sh`'s topic-creation loop.

### Tests, build tags, and isolation

- Each service module is its own `go.mod` with `replace` directives to the local `proto/` and `platform-events/` modules.
- `tests/integration/` uses build tag `integration` so `go test ./...` from any service module never tries to compile it (which is correct — those tests need a live gateway).
- Frontend tests use Vitest + jsdom + Testing Library. The `mf-copy-button` and `mf-url-input` clipboard tests must call `userEvent.setup()` **before** stubbing `navigator.clipboard` — user-event v14 installs its own clipboard during setup and would clobber the stub otherwise.

### Repository conventions worth knowing

- Generated stubs (`services/proto/gen/**`, `frontend/packages/proto-web/src/gen/**`, `frontend/apps/*/@mf-types/**`) are checked in but never edited. Lint config excludes them.
- Default to no comments. Keep doc comments on exported Go identifiers (revive's `exported` rule needs them) and keep "why" comments where the rationale isn't obvious from the code; remove anything that just describes the next line.
- Source layout: `/services` for Go services, `/proto` for protobuf, `/frontend` for the JS workspace, `/k8s/base` for the Kustomize manifests, `/scripts` for orchestration shell scripts, `/tests/integration` for the build-tagged e2e tests, `/docs` for any handwritten docs (currently empty).

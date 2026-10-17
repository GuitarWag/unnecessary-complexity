# Multi-stage Dockerfile for any of the Go services. Build with:
#   docker build -f services/Dockerfile.go --build-arg SERVICE=shortener -t url-shortener/shortener:dev services
ARG SERVICE
FROM golang:1.26-bookworm AS builder
ARG SERVICE
WORKDIR /src

# go.work lives at the repo root, outside this build context. Each module's
# replace directives point at ../proto and ../platform-events, so build without it.
ENV GOWORK=off
COPY proto ./proto
COPY platform-events ./platform-events
COPY shortener ./shortener
COPY resolver ./resolver
COPY analytics ./analytics
COPY gateway ./gateway
COPY loadgen ./loadgen

# Build the requested service. CGO_ENABLED=1 is required for sqlite (resolver, analytics). The other services do not need it.
WORKDIR /src/${SERVICE}
ENV CGO_ENABLED=1
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*
RUN go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=builder /out/server /server
USER nonroot:nonroot
ENTRYPOINT ["/server"]

// Command server runs the loadgen gRPC service that streams synthetic load
// against the gateway and resolver.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/yld/url-shortener/services/loadgen/internal/runner"
	"github.com/yld/url-shortener/services/loadgen/internal/server"
	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type config struct {
	addr           string
	gatewayURL     string
	resolverURL    string
	seedCount      int
	requestTimeout time.Duration
	shutdownDur    time.Duration
}

func loadConfig() config {
	return config{
		addr:           envDefault("LOADGEN_ADDR", ":50054"),
		gatewayURL:     envDefault("LOADGEN_TARGET", "http://localhost:8080"),
		resolverURL:    envDefault("LOADGEN_RESOLVER_TARGET", "http://localhost:50080"),
		seedCount:      envInt("LOADGEN_SEED_COUNT", 200),
		requestTimeout: envDuration("LOADGEN_REQUEST_TIMEOUT", 5*time.Second),
		shutdownDur:    10 * time.Second,
	}
}

func envDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func run() error {
	cfg := loadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	httpClient := &http.Client{
		Timeout: cfg.requestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        2048,
			MaxIdleConnsPerHost: 1024,
			IdleConnTimeout:     30 * time.Second,
		},
		// Don't follow redirects: the read scenario expects a 302 status.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	srv := server.New(runner.Config{
		GatewayURL:  cfg.gatewayURL,
		ResolverURL: cfg.resolverURL,
		HTTPClient:  httpClient,
		SeedCount:   cfg.seedCount,
		SeedTimeout: cfg.requestTimeout,
	})

	gsrv := grpc.NewServer(
		grpc.MaxSendMsgSize(8 * 1024 * 1024),
	)
	loadgenv1.RegisterLoadGenServiceServer(gsrv, srv)

	hs := health.NewServer()
	hs.SetServingStatus("loadgen.v1.LoadGenService", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gsrv, hs)
	reflection.Register(gsrv)

	lis, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.addr, err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("loadgen gRPC listening",
			"addr", cfg.addr,
			"gateway", cfg.gatewayURL,
			"resolver", cfg.resolverURL,
		)
		if err := gsrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("grpc serve: %w", err)
		}
		return nil
	}

	stopped := make(chan struct{})
	go func() {
		gsrv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(cfg.shutdownDur):
		gsrv.Stop()
	}
	return nil
}

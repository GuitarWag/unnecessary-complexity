// Command server runs the gateway HTTP service that proxies Connect/gRPC-Web to upstream gRPC services.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yld/url-shortener/services/gateway/internal/handler"
	analyticsv1 "github.com/yld/url-shortener/services/proto/gen/analytics/v1"
	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type config struct {
	addr           string
	shortenerAddr  string
	resolverAddr   string
	analyticsAddr  string
	loadgenAddr    string
	allowedOrigins []string
	shutdownDur    time.Duration
}

func loadConfig() config {
	return config{
		addr:           envDefault("GATEWAY_ADDR", ":8080"),
		shortenerAddr:  envDefault("GATEWAY_SHORTENER_ADDR", "localhost:50051"),
		resolverAddr:   envDefault("GATEWAY_RESOLVER_ADDR", "localhost:50052"),
		analyticsAddr:  envDefault("GATEWAY_ANALYTICS_ADDR", "localhost:50053"),
		loadgenAddr:    envDefault("GATEWAY_LOADGEN_ADDR", "localhost:50054"),
		allowedOrigins: envList("GATEWAY_ALLOWED_ORIGINS"),
		shutdownDur:    10 * time.Second,
	}
}

func envDefault(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envList(key string) []string {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func run() error {
	cfg := loadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	}

	shortenerConn, err := grpc.NewClient(cfg.shortenerAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial shortener: %w", err)
	}
	defer func() { _ = shortenerConn.Close() }()

	resolverConn, err := grpc.NewClient(cfg.resolverAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial resolver: %w", err)
	}
	defer func() { _ = resolverConn.Close() }()

	analyticsConn, err := grpc.NewClient(cfg.analyticsAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial analytics: %w", err)
	}
	defer func() { _ = analyticsConn.Close() }()

	loadgenConn, err := grpc.NewClient(cfg.loadgenAddr, dialOpts...)
	if err != nil {
		return fmt.Errorf("dial loadgen: %w", err)
	}
	defer func() { _ = loadgenConn.Close() }()

	resolverClient := resolverv1.NewResolverServiceClient(resolverConn)
	router := handler.Router{
		Shortener:      handler.NewShortenerHandler(shortenerv1.NewShortenerServiceClient(shortenerConn)),
		Resolver:       handler.NewResolverHandler(resolverClient),
		Analytics:      handler.NewAnalyticsHandler(analyticsv1.NewAnalyticsServiceClient(analyticsConn)),
		LoadGen:        handler.NewLoadGenHandler(loadgenv1.NewLoadGenServiceClient(loadgenConn)),
		Redirect:       handler.NewRedirectHandler(resolverClient),
		AllowedOrigins: cfg.allowedOrigins,
	}

	srv := &http.Server{
		Addr:              cfg.addr,
		Handler:           router.Build(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("gateway listening",
			"addr", cfg.addr,
			"shortener", cfg.shortenerAddr,
			"resolver", cfg.resolverAddr,
			"analytics", cfg.analyticsAddr,
			"loadgen", cfg.loadgenAddr,
			"allowed_origins", cfg.allowedOrigins,
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("http serve: %w", err)
		}
		return nil
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.shutdownDur)
	defer shutCancel()
	return srv.Shutdown(shutCtx)
}

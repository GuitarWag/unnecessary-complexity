// Command server runs the shortener gRPC service.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/yld/url-shortener/services/platform-events/metrics"
	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
	"github.com/yld/url-shortener/services/shortener/internal/batcher"
	"github.com/yld/url-shortener/services/shortener/internal/codegen"
	"github.com/yld/url-shortener/services/shortener/internal/repo"
	"github.com/yld/url-shortener/services/shortener/internal/service"
)

// batchingRepo decorates *repo.Repository so Insert is routed through a
// WriteBatcher while the read methods stay direct.
type batchingRepo struct {
	*repo.Repository
	wb *batcher.WriteBatcher
}

func (b *batchingRepo) Insert(ctx context.Context, row repo.ShortURL) error {
	return b.wb.Insert(ctx, row)
}

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type config struct {
	addr         string
	databaseURL  string
	codeLength   int
	maxRetries   int
	batchSize    int
	maxConns     int
	batchTimeout time.Duration
	shutdownDur  time.Duration
}

func loadConfig() config {
	return config{
		addr:         envDefault("SHORTENER_ADDR", ":50051"),
		databaseURL:  os.Getenv("SHORTENER_DATABASE_URL"),
		codeLength:   envInt("SHORTENER_CODE_LENGTH", 8),
		maxRetries:   envInt("SHORTENER_MAX_RETRIES", 5),
		batchSize:    envInt("SHORTENER_BATCH_SIZE", 0),
		maxConns:     envInt("SHORTENER_DB_MAX_CONNS", 20),
		batchTimeout: envDuration("SHORTENER_BATCH_TIMEOUT", 10*time.Millisecond),
		shutdownDur:  10 * time.Second,
	}
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
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

func run() error {
	cfg := loadConfig()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	metrics.Serve(os.Getenv("METRICS_ADDR"), logger)

	if cfg.databaseURL == "" {
		return errors.New("SHORTENER_DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", cfg.databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	// Postgres allows 100 connections by default. Without a cap, a burst of
	// concurrent Shorten calls (no batcher) opens one connection each.
	db.SetMaxOpenConns(cfg.maxConns)
	defer func() { _ = db.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := migrateWithRetry(ctx, db, logger); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	gen, err := codegen.NewGenerator(cfg.codeLength, nil)
	if err != nil {
		return fmt.Errorf("codegen: %w", err)
	}

	r := repo.New(db)
	var insertRepo service.Repo = r
	var wb *batcher.WriteBatcher
	if cfg.batchSize > 1 {
		wb = batcher.New(r, cfg.batchSize, cfg.batchTimeout)
		insertRepo = &batchingRepo{Repository: r, wb: wb}
		logger.Info("write batcher enabled", "size", cfg.batchSize, "timeout", cfg.batchTimeout)
	}
	srv := service.New(insertRepo, gen, cfg.maxRetries)

	gsrv := grpc.NewServer(grpc.ChainUnaryInterceptor(metrics.UnaryServerInterceptor()))
	shortenerv1.RegisterShortenerServiceServer(gsrv, srv)

	hs := health.NewServer()
	hs.SetServingStatus("shortener.v1.ShortenerService", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gsrv, hs)
	reflection.Register(gsrv)

	lis, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.addr, err)
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("shortener gRPC listening", "addr", cfg.addr)
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
		if wb != nil {
			wb.Close()
		}
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(cfg.shutdownDur):
		gsrv.Stop()
	}
	return nil
}

// migrateWithRetry waits for Postgres, which can still be starting when the
// shortener starts (compose under Tilt, a fresh k8s deploy).
func migrateWithRetry(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	const attempts = 30
	var err error
	for i := 1; i <= attempts; i++ {
		if err = repo.Migrate(ctx, db); err == nil {
			return nil
		}
		// A SQL error from the server (bad password, missing database) will not
		// fix itself. Only "starting up" is worth waiting for.
		if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code != "57P03" {
			return err
		}
		logger.Warn("migrate failed, retrying", "attempt", i, "err", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return err
}

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
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/yld/url-shortener/services/platform-events/events"
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
	dbPath       string
	codeLength   int
	maxRetries   int
	batchSize    int
	batchTimeout time.Duration
	kafkaBrokers []string
	shutdownDur  time.Duration
}

func loadConfig() config {
	return config{
		addr:         envDefault("SHORTENER_ADDR", ":50051"),
		dbPath:       envDefault("SHORTENER_DB", "shortener.db"),
		codeLength:   envInt("SHORTENER_CODE_LENGTH", 8),
		maxRetries:   envInt("SHORTENER_MAX_RETRIES", 5),
		batchSize:    envInt("SHORTENER_BATCH_SIZE", 0),
		batchTimeout: envDuration("SHORTENER_BATCH_TIMEOUT", 10*time.Millisecond),
		kafkaBrokers: envList("SHORTENER_KAFKA_BROKERS"),
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

	dsn := fmt.Sprintf("%s?_journal=WAL&_busy_timeout=5000&_foreign_keys=on", cfg.dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = db.Close() }()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if err := repo.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	gen, err := codegen.NewGenerator(cfg.codeLength, nil)
	if err != nil {
		return fmt.Errorf("codegen: %w", err)
	}

	var publisher service.Publisher
	if len(cfg.kafkaBrokers) > 0 {
		kp := events.NewKafkaPublisher(cfg.kafkaBrokers)
		defer func() { _ = kp.Close() }()
		publisher = kp
		logger.Info("kafka publisher enabled", "brokers", cfg.kafkaBrokers)
	} else {
		logger.Warn("SHORTENER_KAFKA_BROKERS unset; ShortURLCreated events will not be published")
	}

	r := repo.New(db)
	var insertRepo service.Repo = r
	var wb *batcher.WriteBatcher
	if cfg.batchSize > 1 {
		wb = batcher.New(r, cfg.batchSize, cfg.batchTimeout)
		insertRepo = &batchingRepo{Repository: r, wb: wb}
		logger.Info("write batcher enabled", "size", cfg.batchSize, "timeout", cfg.batchTimeout)
	}
	srv := service.New(insertRepo, gen, publisher, cfg.maxRetries, service.WithLogger(logger))

	gsrv := grpc.NewServer()
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
		logger.Info("shortener gRPC listening", "addr", cfg.addr, "db", cfg.dbPath)
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

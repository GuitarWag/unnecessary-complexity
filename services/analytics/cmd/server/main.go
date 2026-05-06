// Command server runs the analytics gRPC service and click consumer.
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
	"strings"
	"syscall"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/yld/url-shortener/services/analytics/internal/consumer"
	"github.com/yld/url-shortener/services/analytics/internal/repo"
	"github.com/yld/url-shortener/services/analytics/internal/service"
	"github.com/yld/url-shortener/services/platform-events/events"
	analyticsv1 "github.com/yld/url-shortener/services/proto/gen/analytics/v1"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type config struct {
	addr          string
	dbPath        string
	kafkaBrokers  []string
	kafkaGroupID  string
	consumerStart string
	shutdownDur   time.Duration
}

func loadConfig() config {
	return config{
		addr:          envDefault("ANALYTICS_ADDR", ":50053"),
		dbPath:        envDefault("ANALYTICS_DB", "analytics.db"),
		kafkaBrokers:  envList("ANALYTICS_KAFKA_BROKERS"),
		kafkaGroupID:  envDefault("ANALYTICS_KAFKA_GROUP_ID", "analytics-svc"),
		consumerStart: envDefault("ANALYTICS_KAFKA_START", "first"),
		shutdownDur:   10 * time.Second,
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

	r := repo.New(db)
	srv := service.New(r)

	gsrv := grpc.NewServer()
	analyticsv1.RegisterAnalyticsServiceServer(gsrv, srv)
	hs := health.NewServer()
	hs.SetServingStatus("analytics.v1.AnalyticsService", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(gsrv, hs)
	reflection.Register(gsrv)

	lis, err := net.Listen("tcp", cfg.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.addr, err)
	}

	consumerErr := make(chan error, 1)
	if len(cfg.kafkaBrokers) > 0 {
		startOffset := kafka.FirstOffset
		if cfg.consumerStart == "last" {
			startOffset = kafka.LastOffset
		}
		reader := kafka.NewReader(kafka.ReaderConfig{
			Brokers:     cfg.kafkaBrokers,
			GroupID:     cfg.kafkaGroupID,
			Topic:       events.TopicClickRecorded,
			StartOffset: startOffset,
		})
		c := events.NewConsumer(reader,
			func() *eventsv1.ClickRecorded { return &eventsv1.ClickRecorded{} },
			consumer.NewClickRecordedHandler(r),
		)
		go func() {
			logger.Info("kafka consumer started", "topic", events.TopicClickRecorded, "group", cfg.kafkaGroupID)
			err := c.Run(ctx, func(decErr error) { logger.Warn("decode error", "err", decErr) })
			_ = c.Close()
			consumerErr <- err
		}()
	} else {
		logger.Warn("ANALYTICS_KAFKA_BROKERS unset; click events will not be consumed")
		close(consumerErr)
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("analytics gRPC listening", "addr", cfg.addr, "db", cfg.dbPath)
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
	case err := <-consumerErr:
		if err != nil {
			return fmt.Errorf("kafka consumer: %w", err)
		}
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

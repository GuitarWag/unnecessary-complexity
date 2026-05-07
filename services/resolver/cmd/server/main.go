// Command server runs the resolver gRPC service and its Kafka consumer.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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

	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
	"github.com/yld/url-shortener/services/resolver/internal/consumer"
	"github.com/yld/url-shortener/services/resolver/internal/httpserver"
	"github.com/yld/url-shortener/services/resolver/internal/repo"
	"github.com/yld/url-shortener/services/resolver/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

type config struct {
	addr          string
	httpAddr      string
	dbPath        string
	kafkaBrokers  []string
	kafkaGroupID  string
	consumerStart string
	shutdownDur   time.Duration
}

func loadConfig() config {
	return config{
		addr:          envDefault("RESOLVER_ADDR", ":50052"),
		httpAddr:      envDefault("RESOLVER_HTTP_ADDR", ""),
		dbPath:        envDefault("RESOLVER_DB", "resolver.db"),
		kafkaBrokers:  envList("RESOLVER_KAFKA_BROKERS"),
		kafkaGroupID:  envDefault("RESOLVER_KAFKA_GROUP_ID", "resolver-svc"),
		consumerStart: envDefault("RESOLVER_KAFKA_START", "first"),
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

	var publisher service.Publisher
	if len(cfg.kafkaBrokers) > 0 {
		kp := events.NewKafkaPublisher(cfg.kafkaBrokers)
		defer func() { _ = kp.Close() }()
		publisher = kp
	} else {
		logger.Warn("RESOLVER_KAFKA_BROKERS unset; ClickRecorded events will not be published")
	}

	srv := service.New(r, publisher, service.WithLogger(logger))

	gsrv := grpc.NewServer()
	resolverv1.RegisterResolverServiceServer(gsrv, srv)

	hs := health.NewServer()
	hs.SetServingStatus("resolver.v1.ResolverService", healthpb.HealthCheckResponse_SERVING)
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
			Topic:       events.TopicShortURLCreated,
			StartOffset: startOffset,
		})
		c := events.NewConsumer(reader,
			func() *eventsv1.ShortURLCreated { return &eventsv1.ShortURLCreated{} },
			consumer.NewShortURLCreatedHandler(r),
		)
		go func() {
			logger.Info("kafka consumer started", "topic", events.TopicShortURLCreated, "group", cfg.kafkaGroupID)
			err := c.Run(ctx, func(decErr error) {
				logger.Warn("decode error", "err", decErr)
			})
			_ = c.Close()
			consumerErr <- err
		}()
	} else {
		close(consumerErr)
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("resolver gRPC listening", "addr", cfg.addr, "db", cfg.dbPath)
		if err := gsrv.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			serveErr <- err
		}
		close(serveErr)
	}()

	var httpSrv *http.Server
	httpErr := make(chan error, 1)
	if cfg.httpAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/", httpserver.NewRedirectHandler(srv))
		httpSrv = &http.Server{Addr: cfg.httpAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			logger.Info("resolver HTTP redirect listening", "addr", cfg.httpAddr)
			if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				httpErr <- err
			}
			close(httpErr)
		}()
	} else {
		close(httpErr)
	}

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serveErr:
		if err != nil {
			return fmt.Errorf("grpc serve: %w", err)
		}
		return nil
	case err := <-httpErr:
		if err != nil {
			return fmt.Errorf("http serve: %w", err)
		}
	case err := <-consumerErr:
		if err != nil {
			return fmt.Errorf("kafka consumer: %w", err)
		}
	}

	stopped := make(chan struct{})
	go func() {
		if httpSrv != nil {
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.shutdownDur)
			_ = httpSrv.Shutdown(shutdownCtx)
			cancelShutdown()
		}
		gsrv.GracefulStop()
		srv.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(cfg.shutdownDur):
		gsrv.Stop()
	}
	return nil
}

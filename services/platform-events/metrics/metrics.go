// Package metrics holds the Prometheus instruments shared by the Go services: gRPC
// and HTTP latency, Kafka consume results, and the /metrics endpoint itself.
package metrics

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// Buckets span sub-millisecond cache hits up to multi-second stalls.
var buckets = []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}

var (
	grpcDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "grpc_server_handling_seconds",
		Help:    "Latency of unary gRPC calls by method and status code.",
		Buckets: buckets,
	}, []string{"method", "code"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_server_request_duration_seconds",
		Help:    "Latency of HTTP requests by mux route and status code.",
		Buckets: buckets,
	}, []string{"route", "code"})

	eventsConsumed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "events_consumed_total",
		Help: "Kafka messages consumed by topic and result (ok, handler_error, decode_error).",
	}, []string{"topic", "result"})

	eventHandle = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "event_handle_seconds",
		Help:    "Time the consumer handler spent on one message, by topic.",
		Buckets: buckets,
	}, []string{"topic"})
)

// UnaryServerInterceptor records grpc_server_handling_seconds for every unary call.
func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		grpcDuration.WithLabelValues(info.FullMethod, status.Code(err).String()).Observe(time.Since(start).Seconds())
		return resp, err
	}
}

// HTTP records http_server_request_duration_seconds. The route label is the
// ServeMux pattern that matched, which ServeMux writes into r.Pattern, so a
// redirect for any code is one series ("/"), not one per code. next must
// contain the ServeMux; requests that never reach it are labelled "unmatched".
func HTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		httpDuration.WithLabelValues(route, strconv.Itoa(rec.code)).Observe(time.Since(start).Seconds())
	})
}

// ObserveEvent records one consumed Kafka message.
func ObserveEvent(topic, result string, handle time.Duration) {
	eventsConsumed.WithLabelValues(topic, result).Inc()
	if handle > 0 {
		eventHandle.WithLabelValues(topic).Observe(handle.Seconds())
	}
}

// Serve exposes /metrics on addr in the background. An empty addr disables it,
// which keeps local Tilt runs free of port clashes.
func Serve(addr string, logger *slog.Logger) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("metrics listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics server stopped", "err", err)
		}
	}()
}

// statusRecorder keeps Flush and Unwrap working, because Connect server streams
// (the load-test RPC) flush through the wrapped writer.
type statusRecorder struct {
	http.ResponseWriter
	code        int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.code, s.wroteHeader = code, true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Package service implements the gRPC ResolverService.
package service

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
	"github.com/yld/url-shortener/services/resolver/internal/repo"
)

// Repo is the read contract used by Server.
type Repo interface {
	Lookup(ctx context.Context, code string) (repo.ShortURL, error)
}

// Publisher is the event-bus contract used by Server.
type Publisher interface {
	Publish(ctx context.Context, topic, key string, msg proto.Message) error
}

// Server implements resolverv1.ResolverServiceServer.
type Server struct {
	resolverv1.UnimplementedResolverServiceServer

	repo      Repo
	publisher Publisher
	now       func() time.Time
	logger    *slog.Logger

	wg sync.WaitGroup
}

// Option configures the Server.
type Option func(*Server)

// WithNow overrides the time source.
func WithNow(now func() time.Time) Option { return func(s *Server) { s.now = now } }

// WithLogger sets a custom logger.
func WithLogger(l *slog.Logger) Option { return func(s *Server) { s.logger = l } }

// New constructs a Server. publisher may be nil to disable click events.
func New(r Repo, publisher Publisher, opts ...Option) *Server {
	s := &Server{
		repo:      r,
		publisher: publisher,
		now:       func() time.Time { return time.Now().UTC() },
		logger:    slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Resolve implements ResolverService/Resolve.
func (s *Server) Resolve(ctx context.Context, req *resolverv1.ResolveRequest) (*resolverv1.ResolveResponse, error) {
	if req.GetCode() == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}

	row, err := s.repo.Lookup(ctx, req.GetCode())
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return nil, status.Errorf(codes.NotFound, "code %q not found", req.GetCode())
	case err != nil:
		return nil, status.Errorf(codes.Internal, "lookup: %v", err)
	}

	s.publishClick(ctx, req)

	return &resolverv1.ResolveResponse{LongUrl: row.LongURL}, nil
}

// Wait blocks until all in-flight async publishes have settled.
// Useful at shutdown; tests use Eventually so they don't need this.
func (s *Server) Wait() { s.wg.Wait() }

// publishClick emits ClickRecorded asynchronously. Failures are logged.
// Async because the response should not wait on Kafka latency.
func (s *Server) publishClick(_ context.Context, req *resolverv1.ResolveRequest) {
	if s.publisher == nil {
		return
	}
	evt := &eventsv1.ClickRecorded{
		Code:      req.GetCode(),
		ClickedAt: timestamppb.New(s.now()),
		UserAgent: req.GetUserAgent(),
		Referer:   req.GetReferer(),
		Ip:        req.GetIp(),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// New context: caller's ctx is already done by the time the response returns.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.publisher.Publish(ctx, events.TopicClickRecorded, req.GetCode(), evt); err != nil {
			s.logger.Warn("publish ClickRecorded failed", "code", req.GetCode(), "err", err)
		}
	}()
}

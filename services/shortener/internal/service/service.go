// Package service implements the gRPC ShortenerService.
package service

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/yld/url-shortener/services/platform-events/events"
	eventsv1 "github.com/yld/url-shortener/services/proto/gen/events/v1"
	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
	"github.com/yld/url-shortener/services/shortener/internal/repo"
)

const maxLongURLLen = 2048

// Repo is the persistence contract used by Server.
type Repo interface {
	Insert(ctx context.Context, row repo.ShortURL) error
	GetByCode(ctx context.Context, code string) (repo.ShortURL, error)
	GetByIdempotencyKey(ctx context.Context, ownerID, key string) (repo.ShortURL, error)
}

// CodeGenerator returns fresh short codes.
type CodeGenerator interface {
	Generate() (string, error)
}

// Publisher is the event-bus contract used by Server. Defined as a local interface to avoid
// importing kafka dependencies into pure unit tests.
type Publisher interface {
	Publish(ctx context.Context, topic, key string, msg proto.Message) error
}

// Server implements shortenerv1.ShortenerServiceServer.
type Server struct {
	shortenerv1.UnimplementedShortenerServiceServer

	repo       Repo
	gen        CodeGenerator
	publisher  Publisher
	maxRetries int
	now        func() time.Time
	logger     *slog.Logger
	wg         sync.WaitGroup
}

// Wait blocks until all in-flight async publishes have settled.
// Useful at shutdown; tests use Eventually so they don't need this.
func (s *Server) Wait() { s.wg.Wait() }

// Option configures a Server.
type Option func(*Server)

// WithLogger sets a custom logger; defaults to slog.Default().
func WithLogger(l *slog.Logger) Option { return func(s *Server) { s.logger = l } }

// WithNow overrides the time source (used in tests).
func WithNow(now func() time.Time) Option { return func(s *Server) { s.now = now } }

// New constructs a Server. maxRetries bounds collision retries; publisher may be nil for tests
// that don't care about events.
func New(r Repo, gen CodeGenerator, publisher Publisher, maxRetries int, opts ...Option) *Server {
	if maxRetries <= 0 {
		maxRetries = 5
	}
	s := &Server{
		repo:       r,
		gen:        gen,
		publisher:  publisher,
		maxRetries: maxRetries,
		now:        func() time.Time { return time.Now().UTC() },
		logger:     slog.Default(),
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Shorten implements ShortenerService/Shorten.
//
// Dedup behaviour: if idempotency_key is non-empty, two requests from the same owner_id
// with that key are guaranteed to return the same code. An empty key disables dedup —
// the same owner may shorten the same long_url N times and get N distinct rows.
func (s *Server) Shorten(ctx context.Context, req *shortenerv1.ShortenRequest) (*shortenerv1.ShortenResponse, error) {
	if err := validateLongURL(req.GetLongUrl()); err != nil {
		return nil, err
	}
	owner := req.GetOwnerId()
	key := req.GetIdempotencyKey()

	if key != "" {
		existing, err := s.repo.GetByIdempotencyKey(ctx, owner, key)
		switch {
		case err == nil:
			return &shortenerv1.ShortenResponse{Url: toProto(existing), Created: false}, nil
		case errors.Is(err, repo.ErrNotFound):
		default:
			return nil, status.Errorf(codes.Internal, "lookup: %v", err)
		}
	}

	for attempt := 0; attempt < s.maxRetries; attempt++ {
		code, err := s.gen.Generate()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "generate code: %v", err)
		}
		row := repo.ShortURL{
			Code:           code,
			LongURL:        req.GetLongUrl(),
			OwnerID:        owner,
			IdempotencyKey: key,
			CreatedAt:      s.now(),
		}
		switch err := s.repo.Insert(ctx, row); {
		case err == nil:
			s.publishCreated(ctx, row)
			return &shortenerv1.ShortenResponse{Url: toProto(row), Created: true}, nil
		case errors.Is(err, repo.ErrDuplicateCode):
			continue
		case errors.Is(err, repo.ErrDuplicateIdempotencyKey):
			// A concurrent request with the same (owner, key) won the unique constraint.
			// Re-read and return the winner's code.
			existing, lookupErr := s.repo.GetByIdempotencyKey(ctx, owner, key)
			if lookupErr != nil {
				return nil, status.Errorf(codes.Internal, "race lookup: %v", lookupErr)
			}
			return &shortenerv1.ShortenResponse{Url: toProto(existing), Created: false}, nil
		default:
			return nil, status.Errorf(codes.Internal, "insert: %v", err)
		}
	}
	return nil, status.Error(codes.Internal, "exhausted code-generation retries")
}

// Get implements ShortenerService/Get.
func (s *Server) Get(ctx context.Context, req *shortenerv1.GetRequest) (*shortenerv1.GetResponse, error) {
	if req.GetCode() == "" {
		return nil, status.Error(codes.InvalidArgument, "code is required")
	}
	row, err := s.repo.GetByCode(ctx, req.GetCode())
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return nil, status.Errorf(codes.NotFound, "code %q not found", req.GetCode())
	case err != nil:
		return nil, status.Errorf(codes.Internal, "lookup: %v", err)
	}
	return &shortenerv1.GetResponse{Url: toProto(row)}, nil
}

// publishCreated emits ShortURLCreated asynchronously. A publish failure is logged but does
// not fail the RPC, since the source-of-truth write has already succeeded. Async because the
// response should not wait on Kafka latency.
func (s *Server) publishCreated(_ context.Context, row repo.ShortURL) {
	if s.publisher == nil {
		return
	}
	evt := &eventsv1.ShortURLCreated{
		Code:      row.Code,
		LongUrl:   row.LongURL,
		CreatedAt: timestamppb.New(row.CreatedAt),
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.publisher.Publish(ctx, events.TopicShortURLCreated, row.Code, evt); err != nil {
			s.logger.Warn("publish ShortURLCreated failed", "code", row.Code, "err", err)
		}
	}()
}

func validateLongURL(raw string) error {
	if raw == "" {
		return status.Error(codes.InvalidArgument, "long_url is required")
	}
	if len(raw) > maxLongURLLen {
		return status.Errorf(codes.InvalidArgument, "long_url exceeds %d bytes", maxLongURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "long_url invalid: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return status.Error(codes.InvalidArgument, "long_url must use http or https")
	}
	if u.Host == "" {
		return status.Error(codes.InvalidArgument, "long_url must include a host")
	}
	return nil
}

func toProto(r repo.ShortURL) *shortenerv1.ShortURL {
	return &shortenerv1.ShortURL{
		Code:      r.Code,
		LongUrl:   r.LongURL,
		CreatedAt: timestamppb.New(r.CreatedAt),
	}
}

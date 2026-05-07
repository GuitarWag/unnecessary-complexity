// Package server adapts the runner to the gRPC LoadGenService surface.
package server

import (
	"net/http"
	"time"

	"google.golang.org/grpc"

	"github.com/yld/url-shortener/services/loadgen/internal/runner"
	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
)

// Server implements loadgenv1.LoadGenServiceServer by delegating each
// streaming call to a fresh Runner.
type Server struct {
	loadgenv1.UnimplementedLoadGenServiceServer
	cfg runner.Config
}

// New constructs a Server bound to the given runner config. The HTTPClient
// inside cfg is shared across runs.
func New(cfg runner.Config) *Server {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &Server{cfg: cfg}
}

// RunLoadTest validates the request, builds a Runner and streams samples back.
func (s *Server) RunLoadTest(req *loadgenv1.LoadTestRequest, stream grpc.ServerStreamingServer[loadgenv1.LoadTestSample]) error {
	plan, err := runner.PlanFromRequest(req)
	if err != nil {
		return err
	}
	r, err := runner.New(s.cfg)
	if err != nil {
		return err
	}
	return r.Run(stream.Context(), plan, sinkFunc(stream.Send))
}

type sinkFunc func(*loadgenv1.LoadTestSample) error

func (f sinkFunc) Send(s *loadgenv1.LoadTestSample) error { return f(s) }

// Compile-time check that Server matches the streaming interface.
var _ loadgenv1.LoadGenServiceServer = (*Server)(nil)

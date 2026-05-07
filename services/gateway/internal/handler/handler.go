// Package handler implements Connect-RPC handlers that proxy to upstream gRPC services.
package handler

import (
	"context"
	"errors"
	"fmt"
	"io"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	analyticsv1 "github.com/yld/url-shortener/services/proto/gen/analytics/v1"
	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
)

// ShortenerClient is the slice of shortenerv1.ShortenerServiceClient used by the handler.
type ShortenerClient interface {
	Shorten(ctx context.Context, in *shortenerv1.ShortenRequest, opts ...grpc.CallOption) (*shortenerv1.ShortenResponse, error)
	Get(ctx context.Context, in *shortenerv1.GetRequest, opts ...grpc.CallOption) (*shortenerv1.GetResponse, error)
}

// ShortenerHandler implements the Connect ShortenerServiceHandler.
type ShortenerHandler struct {
	client ShortenerClient
}

// NewShortenerHandler constructs a ShortenerHandler.
func NewShortenerHandler(c ShortenerClient) *ShortenerHandler { return &ShortenerHandler{client: c} }

// Shorten forwards to the upstream gRPC service.
func (h *ShortenerHandler) Shorten(ctx context.Context, req *connect.Request[shortenerv1.ShortenRequest]) (*connect.Response[shortenerv1.ShortenResponse], error) {
	resp, err := h.client.Shorten(ctx, req.Msg)
	if err != nil {
		return nil, toConnectErr(err)
	}
	return connect.NewResponse(resp), nil
}

// Get forwards to the upstream gRPC service.
func (h *ShortenerHandler) Get(ctx context.Context, req *connect.Request[shortenerv1.GetRequest]) (*connect.Response[shortenerv1.GetResponse], error) {
	resp, err := h.client.Get(ctx, req.Msg)
	if err != nil {
		return nil, toConnectErr(err)
	}
	return connect.NewResponse(resp), nil
}

// ResolverClient is the slice of resolverv1.ResolverServiceClient used by the handler.
type ResolverClient interface {
	Resolve(ctx context.Context, in *resolverv1.ResolveRequest, opts ...grpc.CallOption) (*resolverv1.ResolveResponse, error)
}

// ResolverHandler implements the Connect ResolverServiceHandler.
type ResolverHandler struct {
	client ResolverClient
}

// NewResolverHandler constructs a ResolverHandler.
func NewResolverHandler(c ResolverClient) *ResolverHandler { return &ResolverHandler{client: c} }

// Resolve forwards to the upstream gRPC service.
func (h *ResolverHandler) Resolve(ctx context.Context, req *connect.Request[resolverv1.ResolveRequest]) (*connect.Response[resolverv1.ResolveResponse], error) {
	resp, err := h.client.Resolve(ctx, req.Msg)
	if err != nil {
		return nil, toConnectErr(err)
	}
	return connect.NewResponse(resp), nil
}

// AnalyticsClient is the slice of analyticsv1.AnalyticsServiceClient used by the handler.
type AnalyticsClient interface {
	Stats(ctx context.Context, in *analyticsv1.StatsRequest, opts ...grpc.CallOption) (*analyticsv1.StatsResponse, error)
}

// AnalyticsHandler implements the Connect AnalyticsServiceHandler.
type AnalyticsHandler struct {
	client AnalyticsClient
}

// NewAnalyticsHandler constructs an AnalyticsHandler.
func NewAnalyticsHandler(c AnalyticsClient) *AnalyticsHandler { return &AnalyticsHandler{client: c} }

// Stats forwards to the upstream gRPC service.
func (h *AnalyticsHandler) Stats(ctx context.Context, req *connect.Request[analyticsv1.StatsRequest]) (*connect.Response[analyticsv1.StatsResponse], error) {
	resp, err := h.client.Stats(ctx, req.Msg)
	if err != nil {
		return nil, toConnectErr(err)
	}
	return connect.NewResponse(resp), nil
}

// LoadGenClient is the slice of loadgenv1.LoadGenServiceClient used by the handler.
type LoadGenClient interface {
	RunLoadTest(ctx context.Context, in *loadgenv1.LoadTestRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[loadgenv1.LoadTestSample], error)
}

// LoadGenHandler implements the Connect LoadGenServiceHandler. It bridges the
// gRPC server-streaming upstream to a Connect server-streaming response.
type LoadGenHandler struct {
	client LoadGenClient
}

// NewLoadGenHandler constructs a LoadGenHandler.
func NewLoadGenHandler(c LoadGenClient) *LoadGenHandler { return &LoadGenHandler{client: c} }

// RunLoadTest opens a stream against the upstream loadgen service and forwards
// samples to the Connect client. Returns when the upstream stream ends, the
// caller's context is cancelled, or sending to the Connect stream fails.
func (h *LoadGenHandler) RunLoadTest(ctx context.Context, req *connect.Request[loadgenv1.LoadTestRequest], stream *connect.ServerStream[loadgenv1.LoadTestSample]) error {
	upstream, err := h.client.RunLoadTest(ctx, req.Msg)
	if err != nil {
		return toConnectErr(err)
	}
	for {
		sample, err := upstream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return toConnectErr(err)
		}
		if err := stream.Send(sample); err != nil {
			return err
		}
	}
}

// toConnectErr converts a gRPC error to a Connect error preserving status code where possible.
func toConnectErr(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return connect.NewError(connect.CodeUnavailable, fmt.Errorf("upstream: %w", err))
	}
	return connect.NewError(grpcCodeToConnect(st.Code()), errors.New(st.Message()))
}

func grpcCodeToConnect(c grpcCodes.Code) connect.Code {
	switch c {
	case grpcCodes.OK:
		return connect.Code(0)
	case grpcCodes.Canceled:
		return connect.CodeCanceled
	case grpcCodes.InvalidArgument:
		return connect.CodeInvalidArgument
	case grpcCodes.DeadlineExceeded:
		return connect.CodeDeadlineExceeded
	case grpcCodes.NotFound:
		return connect.CodeNotFound
	case grpcCodes.AlreadyExists:
		return connect.CodeAlreadyExists
	case grpcCodes.PermissionDenied:
		return connect.CodePermissionDenied
	case grpcCodes.ResourceExhausted:
		return connect.CodeResourceExhausted
	case grpcCodes.FailedPrecondition:
		return connect.CodeFailedPrecondition
	case grpcCodes.Aborted:
		return connect.CodeAborted
	case grpcCodes.OutOfRange:
		return connect.CodeOutOfRange
	case grpcCodes.Unimplemented:
		return connect.CodeUnimplemented
	case grpcCodes.Internal:
		return connect.CodeInternal
	case grpcCodes.Unavailable:
		return connect.CodeUnavailable
	case grpcCodes.DataLoss:
		return connect.CodeDataLoss
	case grpcCodes.Unauthenticated:
		return connect.CodeUnauthenticated
	default:
		return connect.CodeUnknown
	}
}

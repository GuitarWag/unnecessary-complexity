package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	analyticsv1 "github.com/yld/url-shortener/services/proto/gen/analytics/v1"
	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
)

// --- Shortener tests ---

type fakeShortener struct {
	shortenResp *shortenerv1.ShortenResponse
	shortenErr  error
	getResp     *shortenerv1.GetResponse
	getErr      error
}

func (f *fakeShortener) Shorten(_ context.Context, _ *shortenerv1.ShortenRequest, _ ...grpc.CallOption) (*shortenerv1.ShortenResponse, error) {
	return f.shortenResp, f.shortenErr
}

func (f *fakeShortener) Get(_ context.Context, _ *shortenerv1.GetRequest, _ ...grpc.CallOption) (*shortenerv1.GetResponse, error) {
	return f.getResp, f.getErr
}

func TestShortenerHandler_DelegatesShorten(t *testing.T) {
	t.Parallel()

	upstream := &fakeShortener{
		shortenResp: &shortenerv1.ShortenResponse{
			Url: &shortenerv1.ShortURL{
				Code:      "abc",
				LongUrl:   "https://example.com",
				CreatedAt: timestamppb.New(time.Unix(1700000000, 0).UTC()),
			},
			Created: true,
		},
	}
	h := NewShortenerHandler(upstream)
	resp, err := h.Shorten(context.Background(), connect.NewRequest(&shortenerv1.ShortenRequest{
		LongUrl: "https://example.com",
	}))
	require.NoError(t, err)
	assert.Equal(t, "abc", resp.Msg.GetUrl().GetCode())
	assert.True(t, resp.Msg.GetCreated())
}

func TestShortenerHandler_PropagatesUpstreamError(t *testing.T) {
	t.Parallel()
	h := NewShortenerHandler(&fakeShortener{shortenErr: errors.New("upstream down")})
	_, err := h.Shorten(context.Background(), connect.NewRequest(&shortenerv1.ShortenRequest{LongUrl: "https://x"}))
	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeUnavailable, ce.Code())
}

func TestShortenerHandler_DelegatesGet(t *testing.T) {
	t.Parallel()
	upstream := &fakeShortener{
		getResp: &shortenerv1.GetResponse{Url: &shortenerv1.ShortURL{Code: "abc", LongUrl: "https://x"}},
	}
	h := NewShortenerHandler(upstream)
	resp, err := h.Get(context.Background(), connect.NewRequest(&shortenerv1.GetRequest{Code: "abc"}))
	require.NoError(t, err)
	assert.Equal(t, "https://x", resp.Msg.GetUrl().GetLongUrl())
}

// --- Resolver tests ---

type fakeResolver struct {
	resp *resolverv1.ResolveResponse
	err  error
}

func (f *fakeResolver) Resolve(_ context.Context, _ *resolverv1.ResolveRequest, _ ...grpc.CallOption) (*resolverv1.ResolveResponse, error) {
	return f.resp, f.err
}

func TestResolverHandler_Delegates(t *testing.T) {
	t.Parallel()
	h := NewResolverHandler(&fakeResolver{
		resp: &resolverv1.ResolveResponse{LongUrl: "https://example.com/x"},
	})
	resp, err := h.Resolve(context.Background(), connect.NewRequest(&resolverv1.ResolveRequest{Code: "abc"}))
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/x", resp.Msg.GetLongUrl())
}

// --- Analytics tests ---

type fakeAnalytics struct {
	resp *analyticsv1.StatsResponse
	err  error
}

func (f *fakeAnalytics) Stats(_ context.Context, _ *analyticsv1.StatsRequest, _ ...grpc.CallOption) (*analyticsv1.StatsResponse, error) {
	return f.resp, f.err
}

func TestAnalyticsHandler_Delegates(t *testing.T) {
	t.Parallel()
	h := NewAnalyticsHandler(&fakeAnalytics{
		resp: &analyticsv1.StatsResponse{Code: "abc", Total: 7},
	})
	resp, err := h.Stats(context.Background(), connect.NewRequest(&analyticsv1.StatsRequest{Code: "abc"}))
	require.NoError(t, err)
	assert.Equal(t, int64(7), resp.Msg.GetTotal())
}

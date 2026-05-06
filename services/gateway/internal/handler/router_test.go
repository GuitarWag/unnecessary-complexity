package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	shortenerv1 "github.com/yld/url-shortener/services/proto/gen/shortener/v1"
	"github.com/yld/url-shortener/services/proto/gen/shortener/v1/shortenerv1connect"
)

func newTestRouter(t *testing.T, shortener ShortenerClient, resolver ResolverClient, analytics AnalyticsClient) http.Handler {
	t.Helper()
	r := Router{
		Shortener:      NewShortenerHandler(shortener),
		Resolver:       NewResolverHandler(resolver),
		Analytics:      NewAnalyticsHandler(analytics),
		AllowedOrigins: []string{"http://localhost:5173"},
	}
	return r.Build()
}

func TestRouter_HealthzReturns200(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestRouter(t, &fakeShortener{}, &fakeResolver{}, &fakeAnalytics{}))
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "ok", string(body))
}

func TestRouter_ShortenViaConnect(t *testing.T) {
	t.Parallel()

	upstream := &fakeShortener{
		shortenResp: &shortenerv1.ShortenResponse{
			Url:     &shortenerv1.ShortURL{Code: "abc", LongUrl: "https://example.com"},
			Created: true,
		},
	}
	srv := httptest.NewServer(newTestRouter(t, upstream, &fakeResolver{}, &fakeAnalytics{}))
	defer srv.Close()

	client := shortenerv1connect.NewShortenerServiceClient(srv.Client(), srv.URL)
	resp, err := client.Shorten(context.Background(), connect.NewRequest(&shortenerv1.ShortenRequest{
		LongUrl: "https://example.com",
	}))
	require.NoError(t, err)
	assert.Equal(t, "abc", resp.Msg.GetUrl().GetCode())
}

func TestRouter_CORSPreflight(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestRouter(t, &fakeShortener{}, &fakeResolver{}, &fakeAnalytics{}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/shortener.v1.ShortenerService/Shorten", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Connect-Protocol-Version")

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "http://localhost:5173", resp.Header.Get("Access-Control-Allow-Origin"))
}

func TestRouter_CORSDisallowedOriginNoHeader(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newTestRouter(t, &fakeShortener{}, &fakeResolver{}, &fakeAnalytics{}))
	defer srv.Close()

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/healthz", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "http://evil.example")
	req.Header.Set("Access-Control-Request-Method", "GET")

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
}

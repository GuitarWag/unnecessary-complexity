package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
)

type stubResolver struct {
	resp    *resolverv1.ResolveResponse
	err     error
	gotCode string
	gotUA   string
	gotRef  string
	gotIP   string
	called  int
}

func (s *stubResolver) Resolve(_ context.Context, in *resolverv1.ResolveRequest, _ ...grpc.CallOption) (*resolverv1.ResolveResponse, error) {
	s.called++
	s.gotCode = in.GetCode()
	s.gotUA = in.GetUserAgent()
	s.gotRef = in.GetReferer()
	s.gotIP = in.GetIp()
	return s.resp, s.err
}

func TestRedirect_Found(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{resp: &resolverv1.ResolveResponse{LongUrl: "https://example.com/x"}}
	h := NewRedirectHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	req.Header.Set("User-Agent", "ua-x")
	req.Header.Set("Referer", "https://ref.example")
	req.Header.Set("X-Forwarded-For", "203.0.113.1, 10.0.0.1")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://example.com/x", w.Header().Get("Location"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	assert.Equal(t, "abc123", stub.gotCode)
	assert.Equal(t, "ua-x", stub.gotUA)
	assert.Equal(t, "https://ref.example", stub.gotRef)
	assert.Equal(t, "203.0.113.1", stub.gotIP)
}

func TestRedirect_NotFoundForUnknownCode(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{err: status.Error(codes.NotFound, "nope")}
	h := NewRedirectHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRedirect_BadGatewayOnUpstreamError(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{err: status.Error(codes.Unavailable, "down")}
	h := NewRedirectHandler(stub)

	req := httptest.NewRequest(http.MethodGet, "/abc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

func TestRedirect_RejectsNonGetMethods(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{}
	h := NewRedirectHandler(stub)

	req := httptest.NewRequest(http.MethodPost, "/abc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "GET, HEAD", w.Header().Get("Allow"))
	assert.Equal(t, 0, stub.called)
}

func TestRedirect_RejectsInvalidPaths(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{}
	h := NewRedirectHandler(stub)

	for _, p := range []string{"/", "/path/with/slash", "/with-dash", "/with.dot"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code, "path %q", p)
	}
	assert.Equal(t, 0, stub.called, "resolver must not be called for invalid paths")
}

func TestRedirect_HeadIsAllowed(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{resp: &resolverv1.ResolveResponse{LongUrl: "https://example.com/x"}}
	h := NewRedirectHandler(stub)

	req := httptest.NewRequest(http.MethodHead, "/abc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://example.com/x", w.Header().Get("Location"))
}

func TestRouter_RedirectMountedAtRoot(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{resp: &resolverv1.ResolveResponse{LongUrl: "https://example.com/r"}}
	router := Router{
		Shortener: NewShortenerHandler(&fakeShortener{}),
		Resolver:  NewResolverHandler(&fakeResolver{}),
		Analytics: NewAnalyticsHandler(&fakeAnalytics{}),
		Redirect:  NewRedirectHandler(stub),
	}
	srv := httptest.NewServer(router.Build())
	defer srv.Close()

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(srv.URL + "/abcXYZ09")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusFound, resp.StatusCode)
	assert.Equal(t, "https://example.com/r", resp.Header.Get("Location"))
	assert.Equal(t, "abcXYZ09", stub.gotCode)
}

func TestRouter_HealthzStillWinsOverRedirect(t *testing.T) {
	t.Parallel()
	stub := &stubResolver{}
	router := Router{
		Shortener: NewShortenerHandler(&fakeShortener{}),
		Resolver:  NewResolverHandler(&fakeResolver{}),
		Analytics: NewAnalyticsHandler(&fakeAnalytics{}),
		Redirect:  NewRedirectHandler(stub),
	}
	srv := httptest.NewServer(router.Build())
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/healthz")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 0, stub.called)
}

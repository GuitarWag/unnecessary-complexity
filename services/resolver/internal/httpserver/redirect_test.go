package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
)

type fakeResolver struct {
	resp *resolverv1.ResolveResponse
	err  error
	got  *resolverv1.ResolveRequest
}

func (f *fakeResolver) Resolve(_ context.Context, req *resolverv1.ResolveRequest) (*resolverv1.ResolveResponse, error) {
	f.got = req
	return f.resp, f.err
}

func TestRedirect_HappyPath(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{resp: &resolverv1.ResolveResponse{LongUrl: "https://example.com/long"}}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	r.Header.Set("User-Agent", "ua")
	r.Header.Set("Referer", "https://ref")
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")

	NewRedirectHandler(fr).ServeHTTP(w, r)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://example.com/long", w.Header().Get("Location"))
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	assert.Equal(t, "abc123", fr.got.GetCode())
	assert.Equal(t, "ua", fr.got.GetUserAgent())
	assert.Equal(t, "https://ref", fr.got.GetReferer())
	assert.Equal(t, "1.2.3.4", fr.got.GetIp())
}

func TestRedirect_NotFound(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{err: status.Error(grpcCodes.NotFound, "no such code")}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/missing", nil)

	NewRedirectHandler(fr).ServeHTTP(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRedirect_RejectsNonGet(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/abc", nil)

	NewRedirectHandler(fr).ServeHTTP(w, r)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "GET, HEAD", w.Header().Get("Allow"))
}

func TestRedirect_RejectsBadCode(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/has-dashes", nil)

	NewRedirectHandler(fr).ServeHTTP(w, r)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Nil(t, fr.got, "resolver must not be invoked for an invalid code")
}

func TestRedirect_UpstreamErrorBecomes502(t *testing.T) {
	t.Parallel()
	fr := &fakeResolver{err: errors.New("boom")}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/abc", nil)

	NewRedirectHandler(fr).ServeHTTP(w, r)

	assert.Equal(t, http.StatusBadGateway, w.Code)
}

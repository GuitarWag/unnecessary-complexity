// Package httpserver serves the GET /<code> 302 redirect directly from resolver,
// skipping the gateway hop on the read path. The service.Server is invoked in-process,
// so there's no gRPC marshalling between the HTTP handler and the SQLite SELECT.
package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
)

// Resolver is the subset of *service.Server used by the redirect handler.
type Resolver interface {
	Resolve(ctx context.Context, req *resolverv1.ResolveRequest) (*resolverv1.ResolveResponse, error)
}

// RedirectHandler turns GET /<code> into a 302 to the long URL.
type RedirectHandler struct {
	resolver Resolver
	timeout  time.Duration
}

// NewRedirectHandler constructs a RedirectHandler with a 5s upstream timeout.
func NewRedirectHandler(r Resolver) *RedirectHandler {
	return &RedirectHandler{resolver: r, timeout: 5 * time.Second}
}

func (h *RedirectHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	code := strings.TrimPrefix(r.URL.Path, "/")
	if !isLikelyCode(code) {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	resp, err := h.resolver.Resolve(ctx, &resolverv1.ResolveRequest{
		Code:      code,
		UserAgent: r.UserAgent(),
		Referer:   r.Referer(),
		Ip:        clientIP(r),
	})
	if err != nil {
		var st *status.Status
		if s, ok := status.FromError(err); ok {
			st = s
		}
		if st != nil && st.Code() == grpcCodes.NotFound {
			http.NotFound(w, r)
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			http.Error(w, "resolver timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "resolver error", http.StatusBadGateway)
		return
	}

	long := resp.GetLongUrl()
	if long == "" {
		http.Error(w, "resolver returned no URL", http.StatusBadGateway)
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, long, http.StatusFound)
}

func isLikelyCode(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')) {
			return false
		}
	}
	return true
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	return host
}

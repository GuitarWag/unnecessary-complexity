package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	grpcCodes "google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	resolverv1 "github.com/yld/url-shortener/services/proto/gen/resolver/v1"
)

// RedirectHandler turns GET /<code> into a 302 to the long URL via the resolver gRPC client.
// It returns 404 for unknown codes or paths that don't look like a code, and 405 for non-GET methods.
type RedirectHandler struct {
	client  ResolverClient
	timeout time.Duration
}

// NewRedirectHandler constructs a RedirectHandler.
func NewRedirectHandler(c ResolverClient) *RedirectHandler {
	return &RedirectHandler{client: c, timeout: 5 * time.Second}
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

	resp, err := h.client.Resolve(ctx, &resolverv1.ResolveRequest{
		Code:      code,
		UserAgent: r.UserAgent(),
		Referer:   r.Referer(),
		Ip:        clientIP(r),
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && st.Code() == grpcCodes.NotFound {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "upstream resolver unavailable", http.StatusBadGateway)
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

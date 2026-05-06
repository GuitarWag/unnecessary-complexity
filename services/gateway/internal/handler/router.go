package handler

import (
	"net/http"

	"github.com/rs/cors"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	"github.com/yld/url-shortener/services/proto/gen/analytics/v1/analyticsv1connect"
	"github.com/yld/url-shortener/services/proto/gen/resolver/v1/resolverv1connect"
	"github.com/yld/url-shortener/services/proto/gen/shortener/v1/shortenerv1connect"
)

// Router builds the gateway HTTP handler that serves Connect, gRPC, and gRPC-Web on the same port.
type Router struct {
	Shortener *ShortenerHandler
	Resolver  *ResolverHandler
	Analytics *AnalyticsHandler
	// Redirect serves GET /<code> as a 302 to the long URL. May be nil to disable.
	Redirect *RedirectHandler
	// AllowedOrigins controls CORS. Empty list disables CORS entirely.
	AllowedOrigins []string
}

// Build returns an http.Handler with all Connect endpoints mounted, CORS applied,
// and h2c support so plain HTTP/2 (gRPC) works without TLS.
func (r Router) Build() http.Handler {
	mux := http.NewServeMux()

	mux.Handle(shortenerv1connect.NewShortenerServiceHandler(r.Shortener))
	mux.Handle(resolverv1connect.NewResolverServiceHandler(r.Resolver))
	mux.Handle(analyticsv1connect.NewAnalyticsServiceHandler(r.Analytics))

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	if r.Redirect != nil {
		mux.Handle("/", r.Redirect)
	}

	var handler http.Handler = mux
	if len(r.AllowedOrigins) > 0 {
		handler = cors.New(cors.Options{
			AllowedOrigins:   r.AllowedOrigins,
			AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
			AllowedHeaders:   []string{"*"},
			ExposedHeaders:   []string{"Grpc-Status", "Grpc-Message"},
			AllowCredentials: false,
		}).Handler(mux)
	}

	return h2c.NewHandler(handler, &http2.Server{})
}

//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/yld/url-shortener/services/proto/gen/analytics/v1/analyticsv1connect"
	"github.com/yld/url-shortener/services/proto/gen/resolver/v1/resolverv1connect"
	"github.com/yld/url-shortener/services/proto/gen/shortener/v1/shortenerv1connect"
)

const (
	envGatewayURL  = "INTEGRATION_GATEWAY_URL"
	envReadyWait   = "INTEGRATION_READY_TIMEOUT"
	envPropagation = "INTEGRATION_PROPAGATION_TIMEOUT"
	defaultBaseURL = "http://localhost:8080"
)

type clients struct {
	shortener shortenerv1connect.ShortenerServiceClient
	resolver  resolverv1connect.ResolverServiceClient
	analytics analyticsv1connect.AnalyticsServiceClient
	baseURL   string
}

func newClients(t *testing.T) clients {
	t.Helper()
	base := envOr(envGatewayURL, defaultBaseURL)
	httpc := &http.Client{Timeout: 30 * time.Second}
	return clients{
		shortener: shortenerv1connect.NewShortenerServiceClient(httpc, base),
		resolver:  resolverv1connect.NewResolverServiceClient(httpc, base),
		analytics: analyticsv1connect.NewAnalyticsServiceClient(httpc, base),
		baseURL:   base,
	}
}

func waitForGatewayReady(ctx context.Context, base string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	httpc := &http.Client{Timeout: 5 * time.Second}
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
		if err != nil {
			return fmt.Errorf("build healthz request: %w", err)
		}
		resp, err := httpc.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("gateway %s not ready after %s (last err: %v)", base, timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envDurationOr(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func readyTimeout() time.Duration   { return envDurationOr(envReadyWait, 60*time.Second) }
func propagationTimeout() time.Duration {
	return envDurationOr(envPropagation, 30*time.Second)
}

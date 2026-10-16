package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func sampleCount(t *testing.T, vec *prometheus.HistogramVec, labels ...string) uint64 {
	t.Helper()
	var m dto.Metric
	require.NoError(t, vec.WithLabelValues(labels...).(prometheus.Histogram).Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func TestHTTP_LabelsByMuxPatternNotPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusFound) })
	mux.HandleFunc("/healthz", func(http.ResponseWriter, *http.Request) {})
	h := HTTP(mux)

	for _, path := range []string{"/abc123", "/zzz999", "/healthz"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	assert.Equal(t, uint64(2), sampleCount(t, httpDuration, "/", "302"),
		"redirects for different codes must share the \"/\" series")
	assert.Equal(t, uint64(1), sampleCount(t, httpDuration, "/healthz", "200"))
}

func TestUnaryServerInterceptor_RecordsMethodAndCode(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: "/test.v1.Svc/Missing"}
	_, _ = UnaryServerInterceptor()(context.Background(), nil, info, func(context.Context, any) (any, error) {
		return nil, status.Error(codes.NotFound, "nope")
	})

	assert.Equal(t, uint64(1), sampleCount(t, grpcDuration, "/test.v1.Svc/Missing", "NotFound"))
}

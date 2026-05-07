package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/yld/url-shortener/services/loadgen/internal/runner"
	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
)

func newFakeStack(t *testing.T) (string, func()) {
	t.Helper()
	var counter atomic.Uint64
	var codes sync.Map
	mux := http.NewServeMux()
	mux.HandleFunc("/shortener.v1.ShortenerService/Shorten", func(w http.ResponseWriter, _ *http.Request) {
		n := counter.Add(1)
		code := fmt.Sprintf("c%d", n)
		codes.Store(code, true)
		_, _ = fmt.Fprintf(w, `{"url":{"code":%q,"longUrl":"https://example.com/x"},"created":true}`, code)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/")
		if _, ok := codes.Load(code); !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Location", "https://example.com/long/"+code)
		w.WriteHeader(http.StatusFound)
	})
	srv := httptest.NewServer(mux)
	return srv.URL, srv.Close
}

func TestServer_RunLoadTest_StreamsRunningAndTerminal(t *testing.T) {
	t.Parallel()

	url, closeStack := newFakeStack(t)
	defer closeStack()

	httpClient := &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}

	srv := New(runner.Config{
		GatewayURL:  url,
		ResolverURL: url,
		HTTPClient:  httpClient,
		SeedCount:   2,
		SeedTimeout: 1 * time.Second,
		Workers:     8,
	})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gsrv := grpc.NewServer()
	loadgenv1.RegisterLoadGenServiceServer(gsrv, srv)
	go func() { _ = gsrv.Serve(lis) }()
	defer gsrv.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := loadgenv1.NewLoadGenServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stream, err := client.RunLoadTest(ctx, &loadgenv1.LoadTestRequest{
		Preset:          loadgenv1.Preset_CUSTOM,
		Scenario:        "write",
		TargetRps:       30,
		DurationSeconds: 2,
	})
	require.NoError(t, err)

	var samples []*loadgenv1.LoadTestSample
	for {
		s, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		samples = append(samples, s)
	}

	require.NotEmpty(t, samples)
	final := samples[len(samples)-1]
	assert.Equal(t, loadgenv1.Status_COMPLETED, final.GetStatus())

	hasRunning := false
	for _, s := range samples[:len(samples)-1] {
		if s.GetStatus() == loadgenv1.Status_RUNNING {
			hasRunning = true
			break
		}
	}
	assert.True(t, hasRunning, "stream should include at least one RUNNING sample")
}

func TestServer_RunLoadTest_RejectsBadRequest(t *testing.T) {
	t.Parallel()

	url, closeStack := newFakeStack(t)
	defer closeStack()

	srv := New(runner.Config{
		GatewayURL:  url,
		ResolverURL: url,
		HTTPClient:  &http.Client{Timeout: time.Second},
		SeedCount:   1,
	})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	gsrv := grpc.NewServer()
	loadgenv1.RegisterLoadGenServiceServer(gsrv, srv)
	go func() { _ = gsrv.Serve(lis) }()
	defer gsrv.Stop()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := loadgenv1.NewLoadGenServiceClient(conn)
	stream, err := client.RunLoadTest(context.Background(), &loadgenv1.LoadTestRequest{
		Preset:   loadgenv1.Preset_CUSTOM,
		Scenario: "",
	})
	require.NoError(t, err)
	_, err = stream.Recv()
	require.Error(t, err)
}

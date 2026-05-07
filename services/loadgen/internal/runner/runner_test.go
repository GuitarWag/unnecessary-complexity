package runner

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
)

type bufferSink struct {
	mu      sync.Mutex
	samples []*loadgenv1.LoadTestSample
}

func (b *bufferSink) Send(s *loadgenv1.LoadTestSample) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.samples = append(b.samples, s)
	return nil
}

func (b *bufferSink) snapshot() []*loadgenv1.LoadTestSample {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]*loadgenv1.LoadTestSample, len(b.samples))
	copy(out, b.samples)
	return out
}

// fakeStack stands in for the gateway+resolver pair.
// /shortener.v1.ShortenerService/Shorten returns a numbered code, /<code> 302s.
type fakeStack struct {
	srv         *httptest.Server
	client      *http.Client
	shortenHits atomic.Uint64
	resolveHits atomic.Uint64
	codes       sync.Map
	counter     atomic.Uint64
}

func newFakeStack() *fakeStack {
	fs := &fakeStack{}
	mux := http.NewServeMux()
	mux.HandleFunc("/shortener.v1.ShortenerService/Shorten", func(w http.ResponseWriter, r *http.Request) {
		fs.shortenHits.Add(1)
		n := fs.counter.Add(1)
		code := fmt.Sprintf("c%d", n)
		fs.codes.Store(code, true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"url":{"code":%q,"longUrl":"https://example.com/x"},"created":true}`, code)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fs.resolveHits.Add(1)
		code := strings.TrimPrefix(r.URL.Path, "/")
		if _, ok := fs.codes.Load(code); !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Location", "https://example.com/long/"+code)
		w.WriteHeader(http.StatusFound)
	})
	fs.srv = httptest.NewServer(mux)
	fs.client = &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	return fs
}

func (fs *fakeStack) close() { fs.srv.Close() }

func TestRunner_WriteScenarioCompletes(t *testing.T) {
	t.Parallel()
	fs := newFakeStack()
	defer fs.close()

	r, err := New(Config{
		GatewayURL:  fs.srv.URL,
		ResolverURL: fs.srv.URL,
		HTTPClient:  fs.client,
		SeedCount:   3,
		SeedTimeout: 1 * time.Second,
		Workers:     8,
	})
	require.NoError(t, err)

	sink := &bufferSink{}
	plan := Plan{Scenario: "write", TargetRPS: 50, Duration: 2 * time.Second}
	require.NoError(t, r.Run(context.Background(), plan, sink))

	samples := sink.snapshot()
	require.NotEmpty(t, samples)
	last := samples[len(samples)-1]
	assert.Equal(t, loadgenv1.Status_COMPLETED, last.GetStatus())
	assert.Greater(t, last.GetTotalRequests(), uint64(0))

	hasRunning := false
	for _, s := range samples[:len(samples)-1] {
		if s.GetStatus() == loadgenv1.Status_RUNNING {
			hasRunning = true
		}
	}
	assert.True(t, hasRunning, "expected at least one RUNNING sample")
}

func TestRunner_ReadScenarioSeedsAndUsesCodes(t *testing.T) {
	t.Parallel()
	fs := newFakeStack()
	defer fs.close()

	r, err := New(Config{
		GatewayURL:  fs.srv.URL,
		ResolverURL: fs.srv.URL,
		HTTPClient:  fs.client,
		SeedCount:   5,
		SeedTimeout: 1 * time.Second,
		Workers:     8,
	})
	require.NoError(t, err)

	sink := &bufferSink{}
	plan := Plan{Scenario: "read", TargetRPS: 50, Duration: 1500 * time.Millisecond}
	require.NoError(t, r.Run(context.Background(), plan, sink))

	assert.Equal(t, uint64(5), fs.shortenHits.Load(), "expected 5 seed writes (no run-time writes)")
	assert.Greater(t, fs.resolveHits.Load(), uint64(5), "expected resolve hits beyond the seed-propagation probe")

	samples := sink.snapshot()
	require.NotEmpty(t, samples)
	assert.Equal(t, loadgenv1.Status_COMPLETED, samples[len(samples)-1].GetStatus())
}

func TestRunner_MixedScenarioHitsBoth(t *testing.T) {
	t.Parallel()
	fs := newFakeStack()
	defer fs.close()

	r, err := New(Config{
		GatewayURL:  fs.srv.URL,
		ResolverURL: fs.srv.URL,
		HTTPClient:  fs.client,
		SeedCount:   3,
		SeedTimeout: 1 * time.Second,
		Workers:     16,
	})
	require.NoError(t, err)

	sink := &bufferSink{}
	plan := Plan{Scenario: "mixed", TargetRPS: 200, Duration: 1500 * time.Millisecond}
	require.NoError(t, r.Run(context.Background(), plan, sink))

	// Seed = 3 writes; 90/10 read/write at 200rps over ~1.5s = ~30 writes.
	assert.Greater(t, fs.shortenHits.Load(), uint64(3), "mixed scenario should issue write traffic past seeding")
	assert.Greater(t, fs.resolveHits.Load(), uint64(3), "mixed scenario should issue read traffic")
}

func TestRunner_RateLimitsWithinTolerance(t *testing.T) {
	t.Parallel()
	fs := newFakeStack()
	defer fs.close()

	r, err := New(Config{
		GatewayURL:  fs.srv.URL,
		ResolverURL: fs.srv.URL,
		HTTPClient:  fs.client,
		SeedCount:   2,
		SeedTimeout: 1 * time.Second,
		Workers:     16,
	})
	require.NoError(t, err)

	sink := &bufferSink{}
	plan := Plan{Scenario: "write", TargetRPS: 100, Duration: 2 * time.Second}
	require.NoError(t, r.Run(context.Background(), plan, sink))

	final := sink.snapshot()[len(sink.snapshot())-1]
	// Expected ~200 over 2 seconds. Allow a generous +/-50% to absorb scheduling jitter on CI.
	total := final.GetTotalRequests()
	assert.GreaterOrEqual(t, total, uint64(100), "rate too low: %d", total)
	assert.LessOrEqual(t, total, uint64(400), "rate too high: %d", total)
}

func TestRunner_ContextCancelStops(t *testing.T) {
	t.Parallel()
	fs := newFakeStack()
	defer fs.close()

	r, err := New(Config{
		GatewayURL:  fs.srv.URL,
		ResolverURL: fs.srv.URL,
		HTTPClient:  fs.client,
		SeedCount:   2,
		SeedTimeout: 1 * time.Second,
		Workers:     8,
	})
	require.NoError(t, err)

	sink := &bufferSink{}
	plan := Plan{Scenario: "write", TargetRPS: 100, Duration: 30 * time.Second}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err = r.Run(ctx, plan, sink)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Less(t, elapsed, 5*time.Second, "ctx cancel should stop the run promptly")

	samples := sink.snapshot()
	require.NotEmpty(t, samples)
	assert.Equal(t, loadgenv1.Status_FAILED, samples[len(samples)-1].GetStatus())
}

func TestRunner_RejectsUnknownScenarioWithFailedSample(t *testing.T) {
	t.Parallel()
	fs := newFakeStack()
	defer fs.close()

	r, err := New(Config{
		GatewayURL:  fs.srv.URL,
		ResolverURL: fs.srv.URL,
		HTTPClient:  fs.client,
		SeedCount:   1,
		Workers:     2,
	})
	require.NoError(t, err)

	sink := &bufferSink{}
	err = r.Run(context.Background(), Plan{Scenario: "explode", TargetRPS: 10, Duration: time.Second}, sink)
	require.Error(t, err)

	samples := sink.snapshot()
	require.Len(t, samples, 1)
	assert.Equal(t, loadgenv1.Status_FAILED, samples[0].GetStatus())
}

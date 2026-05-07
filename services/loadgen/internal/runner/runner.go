package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	loadgenv1 "github.com/yld/url-shortener/services/proto/gen/loadgen/v1"
)

// SampleSink is the consumer side of the sample stream. Implementations are
// the gRPC server stream, an in-memory buffer for tests, or anything else.
type SampleSink interface {
	Send(*loadgenv1.LoadTestSample) error
}

// Doer is the subset of *http.Client used by the runner. Tests inject a fake.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Config wires the runner to its surroundings: targets and clock/randomness.
type Config struct {
	// GatewayURL is the base for write operations (no trailing slash).
	GatewayURL string
	// ResolverURL is the base for read operations (no trailing slash).
	ResolverURL string
	// HTTPClient is the HTTP client used for both reads and writes. Must be set.
	HTTPClient Doer
	// SeedCount is the number of codes to seed before read/mixed runs.
	SeedCount int
	// SeedTimeout is the per-seed deadline.
	SeedTimeout time.Duration
	// PropagationTimeout caps how long we poll the resolver direct path for
	// the last seeded code to land. Mirrors the k6.js setup pattern.
	PropagationTimeout time.Duration
	// Workers is the size of the worker pool used to drive requests. When
	// zero, a default is chosen from the plan.
	Workers int
	// Now is the clock; defaults to time.Now.
	Now func() time.Time
	// Rand is the rng used to pick seeded codes; defaults to a per-run rng.
	Rand *rand.Rand
}

// Runner generates load and emits per-second samples.
type Runner struct {
	cfg    Config
	randMu sync.Mutex
}

func (r *Runner) randIntn(n int) int {
	r.randMu.Lock()
	defer r.randMu.Unlock()
	return r.cfg.Rand.Intn(n)
}

func (r *Runner) randFloat() float64 {
	r.randMu.Lock()
	defer r.randMu.Unlock()
	return r.cfg.Rand.Float64()
}

func (r *Runner) randInt63() int64 {
	r.randMu.Lock()
	defer r.randMu.Unlock()
	return r.cfg.Rand.Int63()
}

// New constructs a Runner. The HTTPClient must be non-nil; the caller is
// expected to size timeouts appropriately for the target (GW response times
// dominate at high RPS).
func New(cfg Config) (*Runner, error) {
	if cfg.HTTPClient == nil {
		return nil, fmt.Errorf("HTTPClient required")
	}
	if cfg.GatewayURL == "" || cfg.ResolverURL == "" {
		return nil, fmt.Errorf("GatewayURL and ResolverURL required")
	}
	if cfg.SeedCount <= 0 {
		cfg.SeedCount = 200
	}
	if cfg.SeedTimeout == 0 {
		cfg.SeedTimeout = 2 * time.Second
	}
	if cfg.PropagationTimeout == 0 {
		cfg.PropagationTimeout = 5 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		// math/rand is intentional here: weak random is fine for synthetic load.
		cfg.Rand = rand.New(rand.NewSource(cfg.Now().UnixNano())) //nolint:gosec
	}
	return &Runner{cfg: cfg}, nil
}

// Run executes the plan and streams samples. It returns when the run finishes
// (either naturally, via ctx cancellation, or after fatal seed failure). The
// terminal sample (COMPLETED or FAILED) is always sent before Run returns,
// unless the sink itself errors out.
func (r *Runner) Run(ctx context.Context, plan Plan, sink SampleSink) error {
	if !validScenario(plan.Scenario) {
		return r.fail(sink, fmt.Errorf("unknown scenario %q", plan.Scenario))
	}
	if plan.TargetRPS == 0 || plan.Duration <= 0 {
		return r.fail(sink, fmt.Errorf("invalid plan: rps=%d duration=%s", plan.TargetRPS, plan.Duration))
	}

	var codes []string
	if plan.Scenario == "read" || plan.Scenario == "mixed" {
		seeded, err := r.seed(ctx)
		if err != nil {
			return r.fail(sink, fmt.Errorf("seed: %w", err))
		}
		codes = seeded
	}

	return r.drive(ctx, plan, codes, sink)
}

func (r *Runner) seed(ctx context.Context) ([]string, error) {
	codes := make([]string, 0, r.cfg.SeedCount)
	stamp := r.cfg.Now().UnixNano()
	for i := 0; i < r.cfg.SeedCount; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		code, err := r.shorten(ctx, fmt.Sprintf("https://example.com/load/%d-%d", stamp, i))
		if err != nil {
			return nil, fmt.Errorf("seed[%d]: %w", i, err)
		}
		codes = append(codes, code)
	}
	if len(codes) == 0 {
		return nil, fmt.Errorf("seed produced zero codes")
	}
	last := codes[len(codes)-1]
	deadline := r.cfg.Now().Add(r.cfg.PropagationTimeout)
	for r.cfg.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if r.probeRedirect(ctx, last) {
			return codes, nil
		}
	}
	return nil, fmt.Errorf("resolver did not catch up within %s", r.cfg.PropagationTimeout)
}

func (r *Runner) shorten(ctx context.Context, longURL string) (string, error) {
	body, err := json.Marshal(map[string]string{"longUrl": longURL})
	if err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, r.cfg.SeedTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, r.cfg.GatewayURL+"/shortener.v1.ShortenerService/Shorten", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var out struct {
		URL struct {
			Code string `json:"code"`
		} `json:"url"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.URL.Code == "" {
		return "", fmt.Errorf("missing code in response")
	}
	return out.URL.Code, nil
}

func (r *Runner) probeRedirect(ctx context.Context, code string) bool {
	cctx, cancel := context.WithTimeout(ctx, r.cfg.SeedTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, r.cfg.ResolverURL+"/"+code, nil)
	if err != nil {
		return false
	}
	resp, err := r.cfg.HTTPClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently
}

type result struct {
	latencyMS float64
	err       bool
}

// drive runs the worker pool and per-second sample loop. It blocks until the
// run terminates and ALWAYS sends a terminal sample (COMPLETED or FAILED) on
// the way out, except when the sink itself errors mid-stream.
func (r *Runner) drive(ctx context.Context, plan Plan, codes []string, sink SampleSink) error {
	workers := r.cfg.Workers
	if workers <= 0 {
		workers = defaultWorkerCount(plan.TargetRPS)
	}

	// Cancellable run context; we either tear down on duration end or on caller cancel.
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	jobs := make(chan struct{}, workers*2)
	results := make(chan result, workers*4)

	var workerWG sync.WaitGroup
	workerWG.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer workerWG.Done()
			r.workerLoop(runCtx, plan, codes, jobs, results)
		}()
	}

	// Producer ticks at 1/target_rps cadence. We send a token per tick and
	// drop tokens (count an error) when the channel is saturated, so a backed-up
	// system shows realistic error counts rather than skewing the rate downwards.
	var producerWG sync.WaitGroup
	producerWG.Add(1)
	go func() {
		defer producerWG.Done()
		defer close(jobs)
		interval := time.Second / time.Duration(plan.TargetRPS)
		if interval <= 0 {
			interval = time.Microsecond
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		end := r.cfg.Now().Add(plan.Duration)
		for {
			select {
			case <-runCtx.Done():
				return
			case t := <-ticker.C:
				if !t.Before(end) {
					return
				}
				select {
				case jobs <- struct{}{}:
				default:
				}
			}
		}
	}()

	// Sample emitter: 1Hz aggregation of results into samples.
	var totalReq, totalErr atomic.Uint64
	start := r.cfg.Now()
	windowMu := sync.Mutex{}
	window := make([]float64, 0, 8192)
	windowReq := uint64(0)
	windowErr := uint64(0)

	collectorDone := make(chan struct{})
	go func() {
		defer close(collectorDone)
		for res := range results {
			totalReq.Add(1)
			if res.err {
				totalErr.Add(1)
			}
			windowMu.Lock()
			window = append(window, res.latencyMS)
			windowReq++
			if res.err {
				windowErr++
			}
			windowMu.Unlock()
		}
	}()

	tickWindow := time.NewTicker(time.Second)
	defer tickWindow.Stop()

	emit := func(status loadgenv1.Status, message string) error {
		windowMu.Lock()
		latencies := window
		window = make([]float64, 0, len(latencies))
		wReq := windowReq
		windowReq = 0
		windowErr = 0
		windowMu.Unlock()
		p50, p95, p99 := Percentiles(latencies)
		now := r.cfg.Now()
		elapsed := uint32(now.Sub(start).Seconds())
		sample := &loadgenv1.LoadTestSample{
			Ts:             timestamppb.New(now),
			ElapsedSeconds: elapsed,
			CurrentRps:     float64(wReq),
			TotalRequests:  totalReq.Load(),
			Errors:         totalErr.Load(),
			P50Ms:          p50,
			P95Ms:          p95,
			P99Ms:          p99,
			Status:         status,
			Message:        message,
		}
		return sink.Send(sample)
	}

	// Wait for either deadline or ctx cancel; emit per-second samples meanwhile.
	end := r.cfg.Now().Add(plan.Duration)
	var sendErr error
	for {
		stop := false
		select {
		case <-ctx.Done():
			stop = true
		case <-tickWindow.C:
			if r.cfg.Now().After(end) {
				stop = true
				break
			}
			if err := emit(loadgenv1.Status_RUNNING, ""); err != nil {
				sendErr = err
				stop = true
			}
		}
		if stop {
			break
		}
	}

	cancelRun()
	producerWG.Wait()
	workerWG.Wait()
	close(results)
	<-collectorDone

	if sendErr != nil {
		return sendErr
	}

	if err := ctx.Err(); err != nil {
		return emit(loadgenv1.Status_FAILED, err.Error())
	}
	return emit(loadgenv1.Status_COMPLETED, "")
}

func (r *Runner) workerLoop(ctx context.Context, plan Plan, codes []string, jobs <-chan struct{}, results chan<- result) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-jobs:
			if !ok {
				return
			}
			results <- r.execOne(ctx, plan, codes)
		}
	}
}

func (r *Runner) execOne(ctx context.Context, plan Plan, codes []string) result {
	switch plan.Scenario {
	case "read":
		return r.doRead(ctx, codes)
	case "write":
		return r.doWrite(ctx)
	case "mixed":
		// 90/10 read/write, matches k6.js READ_RATIO default.
		if r.randFloat() < 0.9 && len(codes) > 0 {
			return r.doRead(ctx, codes)
		}
		return r.doWrite(ctx)
	}
	return result{err: true}
}

func (r *Runner) doRead(ctx context.Context, codes []string) result {
	if len(codes) == 0 {
		return result{err: true}
	}
	code := codes[r.randIntn(len(codes))]
	t0 := r.cfg.Now()
	cctx, cancel := context.WithTimeout(ctx, r.cfg.SeedTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, r.cfg.ResolverURL+"/"+code, nil)
	if err != nil {
		return result{latencyMS: latencyMS(r.cfg.Now().Sub(t0)), err: true}
	}
	resp, err := r.cfg.HTTPClient.Do(req)
	dur := r.cfg.Now().Sub(t0)
	if err != nil {
		return result{latencyMS: latencyMS(dur), err: true}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		return result{latencyMS: latencyMS(dur), err: true}
	}
	return result{latencyMS: latencyMS(dur)}
}

func (r *Runner) doWrite(ctx context.Context) result {
	body, err := json.Marshal(map[string]string{
		"longUrl": "https://example.com/load/" + strconv.FormatInt(r.randInt63(), 36),
	})
	if err != nil {
		return result{err: true}
	}
	t0 := r.cfg.Now()
	cctx, cancel := context.WithTimeout(ctx, r.cfg.SeedTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodPost, r.cfg.GatewayURL+"/shortener.v1.ShortenerService/Shorten", bytes.NewReader(body))
	if err != nil {
		return result{latencyMS: latencyMS(r.cfg.Now().Sub(t0)), err: true}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.cfg.HTTPClient.Do(req)
	dur := r.cfg.Now().Sub(t0)
	if err != nil {
		return result{latencyMS: latencyMS(dur), err: true}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return result{latencyMS: latencyMS(dur), err: true}
	}
	return result{latencyMS: latencyMS(dur)}
}

func (r *Runner) fail(sink SampleSink, err error) error {
	if sendErr := sink.Send(&loadgenv1.LoadTestSample{
		Ts:      timestamppb.New(r.cfg.Now()),
		Status:  loadgenv1.Status_FAILED,
		Message: err.Error(),
	}); sendErr != nil {
		return errors.Join(err, sendErr)
	}
	return err
}

func latencyMS(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func defaultWorkerCount(rps uint32) int {
	switch {
	case rps <= 200:
		return 16
	case rps <= 2000:
		return 64
	case rps <= 10000:
		return 256
	case rps <= 25000:
		return 512
	default:
		return 1024
	}
}

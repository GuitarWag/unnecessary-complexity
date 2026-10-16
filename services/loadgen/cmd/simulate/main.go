// Command simulate plays a crowd of users against a running url-shortener from
// outside the cluster. Each user shortens links, clicks the popular ones (newest
// links are hottest, Zipf-distributed), reads stats and looks codes up, with a
// random think time between actions. It runs until Ctrl-C or -duration.
//
// By default it goes through kind's nginx ingress on localhost:80 with the
// url-shortener.local Host header, so it needs no /etc/hosts entry or
// port-forward. Against Tilt: -target http://localhost:8080 -host "".
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"
)

type action string

const (
	actShorten action = "shorten"
	actReplay  action = "shorten-replay"
	actClick   action = "click"
	actStats   action = "stats"
	actGet     action = "get"
	// actBad is an invalid request; ok means the system rejected it with a 4xx.
	actBad action = "bad-request"
)

// outcome is how one action ended. "miss" is a click on a code the resolver has
// not received yet: expected for a few hundred ms after a shorten.
type outcome string

const (
	ok   outcome = "ok"
	miss outcome = "miss"
	fail outcome = "error"
)

type client struct {
	target string
	host   string
	http   *http.Client
}

func newClient(target, host string, timeout time.Duration) *client {
	return &client{target: target, host: host, http: &http.Client{
		Timeout: timeout,
		// A click must see the 302 itself, not follow it to the long URL.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// do returns the status code and body; the response itself is already closed.
func (c *client) do(ctx context.Context, method, path string, body any) (int, []byte, error) {
	if body == nil {
		return c.send(ctx, method, path, "", nil)
	}
	b, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	return c.send(ctx, method, path, "application/json", b)
}

// send is do with a raw body and content type, so probes can send broken input.
func (c *client) send(ctx context.Context, method, path, contentType string, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.target+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if c.host != "" {
		req.Host = c.host
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	return resp.StatusCode, data, err
}

// shorten returns the code. Connect JSON uses camelCase field names.
func (c *client) shorten(ctx context.Context, longURL, owner, key string) (string, error) {
	code, data, err := c.do(ctx, http.MethodPost, "/shortener.v1.ShortenerService/Shorten",
		map[string]string{"longUrl": longURL, "ownerId": owner, "idempotencyKey": key})
	if err != nil {
		return "", err
	}
	if code != http.StatusOK {
		return "", fmt.Errorf("shorten: HTTP %d: %s", code, data)
	}
	var out struct {
		URL struct {
			Code string `json:"code"`
		} `json:"url"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.URL.Code == "" {
		return "", fmt.Errorf("shorten: bad response %q", data)
	}
	return out.URL.Code, nil
}

func (c *client) click(ctx context.Context, code string) outcome {
	status, _, err := c.do(ctx, http.MethodGet, "/"+code, nil)
	switch {
	case err != nil:
		return fail
	case status == http.StatusFound:
		return ok
	case status == http.StatusNotFound:
		return miss
	}
	return fail
}

func (c *client) rpc(ctx context.Context, path, code string) outcome {
	status, _, err := c.do(ctx, http.MethodPost, path, map[string]string{"code": code})
	switch {
	case err != nil:
		return fail
	case status == http.StatusOK:
		return ok
	case status == http.StatusNotFound:
		return miss
	}
	return fail
}

// pool holds every code the crowd has created, oldest first. pick favours the
// newest codes with a Zipf distribution, so a few fresh links take most of the
// clicks. A code becomes pickable only settle after creation: people click a
// link some time after it is shared, and by then it has reached the resolver.
type pool struct {
	mu     sync.Mutex
	codes  []string
	born   []time.Time
	settle time.Duration
	now    func() time.Time
}

func (p *pool) add(code string) {
	p.mu.Lock()
	p.codes = append(p.codes, code)
	p.born = append(p.born, p.now())
	p.mu.Unlock()
}

func (p *pool) pick(r *rand.Rand) (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := p.now().Add(-p.settle)
	n := sort.Search(len(p.born), func(i int) bool { return p.born[i].After(cutoff) })
	if n == 0 {
		return "", false
	}
	z := rand.NewZipf(r, 1.2, 1, uint64(n-1)) //nolint:gosec // n >= 1 here
	return p.codes[n-1-int(z.Uint64())], true //nolint:gosec // Zipf output is <= n-1
}

// newest returns the youngest code that is old enough to click.
func (p *pool) newest() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := p.now().Add(-p.settle)
	n := sort.Search(len(p.born), func(i int) bool { return p.born[i].After(cutoff) })
	if n == 0 {
		return "", false
	}
	return p.codes[n-1], true
}

type sample struct {
	act action
	out outcome
	dur time.Duration
}

type stats struct {
	mu      sync.Mutex
	window  []sample
	totals  map[action]map[outcome]int
	started time.Time
}

func (s *stats) record(a action, o outcome, d time.Duration) {
	s.mu.Lock()
	s.window = append(s.window, sample{a, o, d})
	if s.totals[a] == nil {
		s.totals[a] = map[outcome]int{}
	}
	s.totals[a][o]++
	s.mu.Unlock()
}

func (s *stats) flush(w io.Writer, every time.Duration) {
	s.mu.Lock()
	win := s.window
	s.window = nil
	s.mu.Unlock()

	by := map[action][]sample{}
	for _, x := range win {
		by[x.act] = append(by[x.act], x)
	}
	_, _ = fmt.Fprintf(w, "[%s] %d actions, %.1f/s\n",
		time.Since(s.started).Round(time.Second), len(win), float64(len(win))/every.Seconds())
	for _, a := range []action{actShorten, actReplay, actClick, actStats, actGet, actBad} {
		xs := by[a]
		if len(xs) == 0 {
			continue
		}
		durs := make([]time.Duration, 0, len(xs))
		counts := map[outcome]int{}
		for _, x := range xs {
			durs = append(durs, x.dur)
			counts[x.out]++
		}
		_, _ = fmt.Fprintf(w, "  %-15s n=%-5d ok=%-5d miss=%-4d err=%-4d p50=%-8s p95=%s\n", a, len(xs),
			counts[ok], counts[miss], counts[fail], percentile(durs, 0.50), percentile(durs, 0.95))
	}
}

func percentile(d []time.Duration, q float64) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	i := int(math.Ceil(q*float64(len(d)))) - 1
	return d[max(i, 0)].Round(100 * time.Microsecond)
}

var sites = []string{
	"https://en.wikipedia.org/wiki/", "https://github.com/", "https://news.ycombinator.com/item?id=",
	"https://www.youtube.com/watch?v=", "https://go.dev/blog/", "https://example.com/docs/",
}

// user runs one simulated person until ctx ends. The user idles while its id is
// at or above the crowd's active count, which is how the conductor adds and
// removes people. During a spike most clicks go to the viral link.
func user(ctx context.Context, id int, c *client, p *pool, st *stats, cr *crowd, think time.Duration, seed uint64, chaos bool) {
	r := rand.New(rand.NewPCG(seed, uint64(id))) //nolint:gosec // simulated traffic, not security; id >= 0
	who := pickPersona(r, chaos)
	owner := "sim-" + who.name + "-" + strconv.Itoa(id)
	var lastKey, lastURL string
	for ctx.Err() == nil {
		if int64(id) >= cr.active.Load() {
			if !sleep(ctx, 250*time.Millisecond) {
				return
			}
			continue
		}
		start := time.Now()
		badRate := who.bad
		if cr.attack.Load() {
			badRate = max(badRate, attackBadRate)
		}
		if r.Float64() < badRate {
			o := c.sendBad(ctx, r, cr.warn)
			if ctx.Err() != nil {
				return
			}
			st.record(actBad, o, time.Since(start))
			if !sleep(ctx, time.Duration(r.ExpFloat64()*who.pace*float64(think))) {
				return
			}
			continue
		}
		roll := r.Float64()
		var a action
		var o outcome
		code, have := p.pick(r)
		switch {
		case !have || roll < who.shorten:
			if lastKey != "" && r.Float64() < 0.2 {
				// A double-submit: same owner and key must return the same code.
				a, o = actReplay, ok
				if _, err := c.shorten(ctx, lastURL, owner, lastKey); err != nil {
					o = fail
				}
				break
			}
			a, o = actShorten, ok
			lastURL = sites[r.IntN(len(sites))] + strconv.FormatUint(r.Uint64()%1e6, 36)
			lastKey = ""
			if r.Float64() < 0.5 {
				lastKey = owner + "-" + strconv.FormatUint(r.Uint64(), 36)
			}
			code, err := c.shorten(ctx, lastURL, owner, lastKey)
			if err != nil {
				o = fail
			} else {
				p.add(code)
			}
		case roll < who.click:
			if v := cr.viralCode(); v != "" && r.Float64() < 0.7 {
				code = v
			}
			a, o = actClick, c.click(ctx, code)
		case roll < who.stats:
			a, o = actStats, c.rpc(ctx, "/analytics.v1.AnalyticsService/Stats", code)
		default:
			a, o = actGet, c.rpc(ctx, "/shortener.v1.ShortenerService/Get", code)
		}
		if ctx.Err() != nil {
			return
		}
		st.record(a, o, time.Since(start))
		if !sleep(ctx, time.Duration(r.ExpFloat64()*who.pace*float64(think))) {
			return
		}
	}
}

// sleep waits d, or returns false as soon as ctx ends.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func main() {
	target := flag.String("target", "http://localhost/api", "gateway base URL, as seen from this machine")
	host := flag.String("host", "url-shortener.local", `Host header for the ingress; "" sends none`)
	users := flag.Int("users", 25, "concurrent simulated users (the baseline, with -chaos)")
	chaos := flag.Bool("chaos", false, "vary the crowd: random drift, flash-crowd spikes with a viral link, and lulls")
	maxUsers := flag.Int("max-users", 0, "cap on users during a spike; 0 means 8x -users")
	think := flag.Duration("think", 400*time.Millisecond, "mean pause between one user's actions")
	duration := flag.Duration("duration", 0, "stop after this long; 0 runs until Ctrl-C")
	settle := flag.Duration("settle", 2*time.Second, "how old a link must be before users click it")
	report := flag.Duration("report", 10*time.Second, "print a summary this often")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *duration > 0 {
		ctx, cancel = context.WithTimeout(ctx, *duration)
		defer cancel()
	}

	c := newClient(*target, *host, *timeout)
	if err := preflight(ctx, c); err != nil {
		fmt.Fprintf(os.Stderr, "simulate: %v\n", err)
		os.Exit(1)
	}
	p := &pool{settle: *settle, now: time.Now}
	st := &stats{totals: map[action]map[outcome]int{}, started: time.Now()}
	seed := rand.Uint64() //nolint:gosec // simulated traffic, not security
	cr := &crowd{warn: warnOnce(func(f string, a ...any) {
		fmt.Printf("[%s] "+f, append([]any{time.Since(st.started).Round(time.Second)}, a...)...)
	})}
	cr.active.Store(int64(*users))
	spawn := *users
	if *chaos {
		if *maxUsers <= 0 {
			*maxUsers = 8 * *users
		}
		spawn = *maxUsers
		cond := newConductor(*users, *maxUsers)
		go conduct(ctx.Done(), cond, cr, p, seed, func(f string, a ...any) {
			fmt.Printf("[%s] "+f, append([]any{time.Since(st.started).Round(time.Second)}, a...)...)
		})
		fmt.Printf("simulating ~%d users (chaos, up to %d) against %s (Host: %q). Ctrl-C to stop.\n", *users, *maxUsers, *target, *host)
	} else {
		fmt.Printf("simulating %d users against %s (Host: %q). Ctrl-C to stop.\n", *users, *target, *host)
	}
	var wg sync.WaitGroup
	for i := range spawn {
		wg.Add(1)
		go func() {
			defer wg.Done()
			user(ctx, i, c, p, st, cr, *think, seed, *chaos)
		}()
	}

	tick := time.NewTicker(*report)
	defer tick.Stop()
	for done := false; !done; {
		select {
		case <-tick.C:
			fmt.Printf("users now %d", cr.active.Load())
			if v := cr.viralCode(); v != "" {
				fmt.Printf(", viral %s", v)
			}
			fmt.Println()
			st.flush(os.Stdout, *report)
		case <-ctx.Done():
			done = true
		}
	}
	wg.Wait()
	fmt.Printf("\nstopped after %s, %d links created\n", time.Since(st.started).Round(time.Second), len(p.codes))
	for _, a := range []action{actShorten, actReplay, actClick, actStats, actGet, actBad} {
		if t := st.totals[a]; t != nil {
			fmt.Printf("  %-15s ok=%d miss=%d err=%d\n", a, t[ok], t[miss], t[fail])
		}
	}
}

// preflight fails fast with a useful message when nothing answers at target.
func preflight(ctx context.Context, c *client) error {
	status, _, err := c.do(ctx, http.MethodGet, "/healthz", nil)
	if err != nil {
		return fmt.Errorf("gateway not reachable at %s/healthz: %w", c.target, err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("gateway /healthz returned HTTP %d; is the stack up and the -host right?", status)
	}
	return nil
}

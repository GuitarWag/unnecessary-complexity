package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGateway speaks just enough Connect JSON, and only answers when the Host
// header matches, like the nginx ingress rule.
func fakeGateway(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	codes := map[string]bool{}
	n := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "url-shortener.local" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/healthz":
		case r.URL.Path == "/shortener.v1.ShortenerService/Shorten":
			var req map[string]string
			if json.NewDecoder(r.Body).Decode(&req) != nil || req["longUrl"] == "" || req["ownerId"] == "" {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			n++
			code := fmt.Sprintf("c%d", n)
			codes[code] = true
			_ = json.NewEncoder(w).Encode(map[string]any{"url": map[string]string{"code": code}})
		case strings.Contains(r.URL.Path, "Service/"):
			_, _ = w.Write([]byte(`{}`))
		case codes[strings.TrimPrefix(r.URL.Path, "/")]:
			w.Header().Set("Location", "https://example.com")
			w.WriteHeader(http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestUsers_DriveEveryActionWithoutErrors(t *testing.T) {
	srv := fakeGateway(t)
	defer srv.Close()
	c := newClient(srv.URL, "url-shortener.local", time.Second)
	require.NoError(t, preflight(context.Background(), c))

	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	p := &pool{now: time.Now}
	st := &stats{totals: map[action]map[outcome]int{}, started: time.Now()}
	cr := &crowd{}
	cr.active.Store(8)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); user(ctx, i, c, p, st, cr, time.Millisecond, 42, false) }()
	}
	wg.Wait()

	for _, a := range []action{actShorten, actClick, actStats, actGet} {
		assert.Positive(t, st.totals[a][ok], "action %s never succeeded", a)
	}
	for a, t2 := range st.totals {
		assert.Zero(t, t2[fail], "action %s had errors", a)
	}
}

func TestPreflight_WrongHostFails(t *testing.T) {
	srv := fakeGateway(t)
	defer srv.Close()
	assert.Error(t, preflight(context.Background(), newClient(srv.URL, "", time.Second)))
}

func TestPool_PickFavoursNewest(t *testing.T) {
	p := &pool{now: time.Now}
	for i := range 100 {
		p.add(fmt.Sprint(i))
	}
	r := rand.New(rand.NewPCG(1, 2))
	hits := map[string]int{}
	for range 10000 {
		c, _ := p.pick(r)
		hits[c]++
	}
	assert.Greater(t, hits["99"], hits["50"]*10, "the newest link should get far more clicks than a middle one")
}

func TestPool_HidesLinksYoungerThanSettle(t *testing.T) {
	now := time.Unix(1000, 0)
	p := &pool{settle: 2 * time.Second, now: func() time.Time { return now }}
	p.add("old")
	now = now.Add(5 * time.Second)
	p.add("fresh")

	r := rand.New(rand.NewPCG(1, 2))
	for range 100 {
		c, ok := p.pick(r)
		require.True(t, ok)
		assert.Equal(t, "old", c)
	}
}

func TestConductor_SpikesLullsAndBounds(t *testing.T) {
	c := newConductor(20, 160)
	r := rand.New(rand.NewPCG(7, 7))
	seen := map[phase]int{}
	peak, low := 0, 1<<30
	for range 3600 { // one simulated hour
		c.step(r)
		seen[c.phase]++
		peak, low = max(peak, c.users()), min(low, c.users())
		require.LessOrEqual(t, c.users(), 160)
		require.GreaterOrEqual(t, c.users(), 0)
	}
	assert.Positive(t, seen[spike])
	assert.Positive(t, seen[lull])
	assert.Positive(t, seen[attack])
	assert.Greater(t, seen[steady], seen[spike]+seen[lull], "most of the time should be steady")
	assert.GreaterOrEqual(t, peak, 60, "a spike should reach at least 3x the baseline")
	assert.LessOrEqual(t, low, 4, "a lull should drop to almost nobody")
}

func TestUsers_IdleAboveActiveCount(t *testing.T) {
	srv := fakeGateway(t)
	defer srv.Close()
	c := newClient(srv.URL, "url-shortener.local", time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	st := &stats{totals: map[action]map[outcome]int{}, started: time.Now()}
	cr := &crowd{} // active = 0: nobody may act
	user(ctx, 0, c, &pool{now: time.Now}, st, cr, time.Millisecond, 1, false)
	assert.Empty(t, st.totals)
}

// Every probe must reach the server as a well-formed HTTP request, and the
// scoring must count a 4xx as ok and anything else as a finding.
func TestSendBad_ScoresRejectionsAndWarnsOnHoles(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   outcome
	}{{http.StatusBadRequest, ok}, {http.StatusNotFound, ok}, {http.StatusOK, fail}, {http.StatusInternalServerError, fail}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) }))
		c := newClient(srv.URL, "", time.Second)
		var warnings []string
		warn := warnOnce(func(f string, a ...any) { warnings = append(warnings, fmt.Sprintf(f, a...)) })
		r := rand.New(rand.NewPCG(3, 4))
		for range len(probes) * 20 {
			assert.Equal(t, tc.want, c.sendBad(context.Background(), r, warn), "status %d", tc.status)
		}
		srv.Close()
		if tc.want == ok {
			assert.Empty(t, warnings)
		} else {
			assert.NotEmpty(t, warnings)
			assert.LessOrEqual(t, len(warnings), len(probes), "each finding is printed once")
		}
	}
}

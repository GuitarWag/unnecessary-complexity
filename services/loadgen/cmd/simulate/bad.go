package main

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
)

// probe is one kind of invalid request. Every probe must get a 4xx: a 2xx means
// a validation hole, a 5xx means a crash path.
type probe struct {
	name        string
	method      string
	path        string
	contentType string
	body        func(r *rand.Rand) []byte
}

const (
	shortenPath = "/shortener.v1.ShortenerService/Shorten"
	getPath     = "/shortener.v1.ShortenerService/Get"
	statsPath   = "/analytics.v1.AnalyticsService/Stats"
)

func lit(s string) func(*rand.Rand) []byte { return func(*rand.Rand) []byte { return []byte(s) } }

// Unknown JSON fields are left out on purpose: Connect ignores them for forward
// compatibility, so they are not invalid input.
var probes = []probe{
	{"empty-url", http.MethodPost, shortenPath, "application/json", lit(`{"longUrl":""}`)},
	{"javascript-url", http.MethodPost, shortenPath, "application/json", lit(`{"longUrl":"javascript:alert(1)"}`)},
	{"ftp-url", http.MethodPost, shortenPath, "application/json", lit(`{"longUrl":"ftp://files.example/a"}`)},
	{"url-without-host", http.MethodPost, shortenPath, "application/json", lit(`{"longUrl":"https://"}`)},
	{"url-too-long", http.MethodPost, shortenPath, "application/json", func(*rand.Rand) []byte {
		return []byte(`{"longUrl":"https://example.com/` + strings.Repeat("a", 3000) + `"}`)
	}},
	{"nul-in-owner", http.MethodPost, shortenPath, "application/json", lit(`{"longUrl":"https://example.com","ownerId":"a\u0000b"}`)},
	{"key-too-long", http.MethodPost, shortenPath, "application/json", func(*rand.Rand) []byte {
		return []byte(`{"longUrl":"https://example.com","idempotencyKey":"` + strings.Repeat("k", 300) + `"}`)
	}},
	{"malformed-json", http.MethodPost, shortenPath, "application/json", lit(`{"longUrl":`)},
	{"wrong-field-type", http.MethodPost, shortenPath, "application/json", lit(`{"longUrl":123}`)},
	{"wrong-content-type", http.MethodPost, shortenPath, "text/plain", lit(`https://example.com`)},
	{"get-on-post-only", http.MethodGet, shortenPath, "", nil},
	{"unknown-procedure", http.MethodPost, "/shortener.v1.ShortenerService/DropAll", "application/json", lit(`{}`)},
	{"get-empty-code", http.MethodPost, getPath, "application/json", lit(`{"code":""}`)},
	{"stats-empty-code", http.MethodPost, statsPath, "application/json", lit(`{"code":""}`)},
	{"stats-bad-date", http.MethodPost, statsPath, "application/json", lit(`{"code":"abc","fromDate":"yesterday"}`)},
	{"click-unknown-code", http.MethodGet, "", "", nil},
	{"click-traversal", http.MethodGet, "/..%2f..%2fetc%2fpasswd", "", nil},
	{"big-body-256k", http.MethodPost, shortenPath, "application/json", func(*rand.Rand) []byte {
		return []byte(`{"longUrl":"https://example.com/` + strings.Repeat("b", 256<<10) + `"}`)
	}},
}

// sendBad sends one random probe. ok means the system rejected it with a 4xx.
// Anything else is a finding and goes to warn, once per probe and status.
func (c *client) sendBad(ctx context.Context, r *rand.Rand, warn func(key, msg string)) outcome {
	pr := probes[r.IntN(len(probes))]
	path := pr.path
	if pr.name == "click-unknown-code" {
		// Codes are 8 base62 chars; 'zz-' cannot be one.
		path = fmt.Sprintf("/zz-%x", r.Uint64())
	}
	var body []byte
	if pr.body != nil {
		body = pr.body(r)
	}
	status, _, err := c.send(ctx, pr.method, path, pr.contentType, body)
	switch {
	case err != nil && ctx.Err() != nil:
		return ok
	case err != nil:
		warn(pr.name+"/transport", fmt.Sprintf("%s: transport error: %v", pr.name, err))
		return fail
	case status >= 400 && status < 500:
		return ok
	}
	warn(fmt.Sprintf("%s/%d", pr.name, status), fmt.Sprintf("%s: got HTTP %d, want 4xx", pr.name, status))
	return fail
}

// warnOnce prints the first message for each key and drops the rest. Keys are
// a probe plus an outcome class, never the message itself: messages carry
// per-request data such as random paths, so they would never repeat.
func warnOnce(print func(string, ...any)) func(key, msg string) {
	var mu sync.Mutex
	seen := map[string]bool{}
	return func(key, msg string) {
		mu.Lock()
		defer mu.Unlock()
		if !seen[key] {
			seen[key] = true
			print("[bad] %s\n", msg)
		}
	}
}

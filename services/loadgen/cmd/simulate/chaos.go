package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// crowd is the shared state the conductor writes and the users read: how many
// users are active right now, and which link (if any) is going viral.
type crowd struct {
	active atomic.Int64
	viral  atomic.Pointer[string]
}

func (c *crowd) viralCode() string {
	if v := c.viral.Load(); v != nil {
		return *v
	}
	return ""
}

type phase int

const (
	steady phase = iota
	spike
	lull
)

func (p phase) String() string {
	return [...]string{"steady", "spike", "lull"}[p]
}

// Odds per second of leaving the steady phase: a spike about every 45s, a lull
// about every 90s, on average.
const (
	spikeOdds = 1.0 / 45
	lullOdds  = 1.0 / 90
)

// conductor decides the crowd size once per second. In steady it drifts around
// base; a spike ramps fast to 3-8x base for 10-40s; a lull drops to almost
// nobody for 10-30s. step is pure apart from r, so tests can drive it.
type conductor struct {
	base, max int
	phase     phase
	left      int // seconds left in a spike or lull
	target    int
	cur       float64
}

func newConductor(base, maxUsers int) *conductor {
	return &conductor{base: base, max: maxUsers, target: base, cur: float64(base)}
}

// step advances one second. It returns a description when a phase starts.
func (c *conductor) step(r *rand.Rand) string {
	event := ""
	if c.phase != steady {
		c.left--
		if c.left <= 0 {
			c.phase, c.target = steady, c.base
			event = "back to steady"
		}
	}
	if c.phase == steady && event == "" {
		switch x := r.Float64(); {
		case x < spikeOdds:
			c.phase = spike
			c.target = min(c.base*(3+r.IntN(6)), c.max)
			c.left = 10 + r.IntN(31)
			event = fmt.Sprintf("SPIKE to %d users for %ds", c.target, c.left)
		case x < spikeOdds+lullOdds:
			c.phase = lull
			c.target = r.IntN(c.base/5 + 1)
			c.left = 10 + r.IntN(21)
			event = fmt.Sprintf("lull down to %d users for %ds", c.target, c.left)
		default:
			c.target = clamp(c.target+r.IntN(5)-2, c.base/2, c.base*3/2)
		}
	}
	// A flash crowd arrives fast; everything else eases in.
	rate := 0.2
	if c.phase == spike {
		rate = 0.5
	}
	c.cur += (float64(c.target) - c.cur) * rate
	return event
}

func (c *conductor) users() int { return clamp(int(math.Round(c.cur)), 0, c.max) }

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

// conduct runs the conductor every second until done closes. On a spike it
// makes the newest clickable link viral; on the way back it clears it.
func conduct(done <-chan struct{}, cond *conductor, cr *crowd, p *pool, seed uint64, logf func(string, ...any)) {
	r := rand.New(rand.NewPCG(seed, 0xc4a05)) //nolint:gosec // simulated traffic, not security
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-done:
			return
		case <-tick.C:
		}
		if event := cond.step(r); event != "" {
			if cond.phase == spike {
				if code, ok := p.newest(); ok {
					cr.viral.Store(&code)
					event += ", viral link " + code
				}
			} else {
				cr.viral.Store(nil)
			}
			logf("[chaos] %s\n", event)
		}
		cr.active.Store(int64(cond.users()))
	}
}

// persona is one kind of user: the action mix and how fast they act.
type persona struct {
	name                  string
	shorten, click, stats float64 // cumulative thresholds; the rest is Get
	pace                  float64 // multiplies -think
}

var personas = []struct {
	share float64
	p     persona
}{
	{0.60, persona{name: "clicker", shorten: 0.04, click: 0.90, stats: 0.96, pace: 0.7}},
	{0.25, persona{name: "creator", shorten: 0.35, click: 0.80, stats: 0.95, pace: 1.5}},
	{0.15, persona{name: "analyst", shorten: 0.05, click: 0.40, stats: 0.85, pace: 1.2}},
}

func pickPersona(r *rand.Rand) persona {
	x := r.Float64()
	for _, p := range personas {
		if x < p.share {
			return p.p
		}
		x -= p.share
	}
	return personas[0].p
}

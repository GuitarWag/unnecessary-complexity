package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"
)

// crowd is the shared state the conductor writes and the users read: how many
// users are active right now, which link (if any) is going viral, and whether an
// attack is on. warn reports a bad request the system did not reject.
type crowd struct {
	active atomic.Int64
	viral  atomic.Pointer[string]
	attack atomic.Bool
	warn   func(key, msg string)
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
	attack
)

func (p phase) String() string {
	return [...]string{"steady", "spike", "lull", "attack"}[p]
}

// Odds per second of leaving the steady phase: a spike about every 45s, a lull
// about every 90s, an attack about every 120s, on average.
const (
	spikeOdds  = 1.0 / 45
	lullOdds   = 1.0 / 90
	attackOdds = 1.0 / 120
	// attackBadRate is the share of every user's actions that are bad requests
	// while an attack runs.
	attackBadRate = 0.3
)

// conductor decides the crowd size once per second. In steady it drifts around
// base; a spike ramps fast to 3-8x base for 10-40s; a lull drops to almost
// nobody for 10-30s; an attack keeps the size but turns 30% of all actions into
// bad requests for 10-25s. step is pure apart from r, so tests can drive it.
type conductor struct {
	base, max int
	phase     phase
	left      int // seconds left in a spike, lull or attack
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
		case x < spikeOdds+lullOdds+attackOdds:
			c.phase = attack
			c.left = 10 + r.IntN(16)
			event = fmt.Sprintf("ATTACK: %.0f%% bad requests for %ds", attackBadRate*100, c.left)
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
			cr.attack.Store(cond.phase == attack)
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

// persona is one kind of user: the action mix and how fast they act. bad is the
// share of actions that are invalid requests.
type persona struct {
	name                  string
	shorten, click, stats float64 // cumulative thresholds; the rest is Get
	pace                  float64 // multiplies -think
	bad                   float64
}

var personas = []struct {
	share float64
	p     persona
}{
	{0.60, persona{name: "clicker", shorten: 0.04, click: 0.90, stats: 0.96, pace: 0.7}},
	{0.25, persona{name: "creator", shorten: 0.35, click: 0.80, stats: 0.95, pace: 1.5}},
	{0.15, persona{name: "analyst", shorten: 0.05, click: 0.40, stats: 0.85, pace: 1.2}},
}

// abuser joins the crowd only with -chaos. It mostly sends invalid requests.
var abuser = persona{name: "abuser", shorten: 0.30, click: 0.80, stats: 0.90, pace: 0.5, bad: 0.8}

const abuserShare = 0.06

func pickPersona(r *rand.Rand, chaos bool) persona {
	if chaos && r.Float64() < abuserShare {
		return abuser
	}
	x := r.Float64()
	for _, p := range personas {
		if x < p.share {
			return p.p
		}
		x -= p.share
	}
	return personas[0].p
}

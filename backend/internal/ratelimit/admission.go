package ratelimit

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Policy is scoped to a global category, broker category/operation, or admin
// realtime category/operation. '*' is the aggregate policy within that scope.
type Policy struct {
	Scope      string `json:"scope"`
	Subject    string `json:"subject"`
	Operation  string `json:"operation"`
	Rate       int    `json:"rate"`
	Burst      int    `json:"burst"`
	Concurrent int    `json:"concurrent"`
	Enabled    bool   `json:"enabled"`
}

func (p Policy) Key() string { return p.Scope + ":" + p.Subject + ":" + p.Operation }
func (p Policy) Validate() error {
	if p.Scope != "global" && p.Scope != "broker" && p.Scope != "admin" {
		return fmt.Errorf("invalid scope")
	}
	if p.Subject == "" || len(p.Subject) > 64 || len(p.Operation) > 64 || p.Operation == "" {
		return fmt.Errorf("invalid subject or operation")
	}
	if p.Scope == "global" && p.Subject != "*" {
		return fmt.Errorf("global subject must be *")
	}
	if p.Scope == "admin" && p.Subject != "*" {
		return fmt.Errorf("admin policies apply to each authenticated user; subject must be *")
	}
	if p.Rate < 1 || p.Rate > 1000000 || p.Burst < 1 || p.Burst > 1000000 || p.Concurrent < 1 || p.Concurrent > 1000000 {
		return fmt.Errorf("rate, burst and concurrent must be in 1..1000000")
	}
	return nil
}

type policySet struct{ values map[string]Policy }
type gate struct {
	// GCRA virtual arrival time implements a token bucket without a global lock.
	next               atomic.Int64
	active             atomic.Int64
	requests           atomic.Uint64
	allowed            atomic.Uint64
	rejected           atomic.Uint64
	rateRejected       atomic.Uint64
	concurrentRejected atomic.Uint64
	lastLog            atomic.Int64
	lastSeen           atomic.Int64
	policy             atomic.Pointer[Policy]
	window             atomic.Uint64 // high bits unix second, low bits attempted requests
	peak               atomic.Uint64
}
type Admission struct {
	policies   atomic.Pointer[policySet]
	gates      sync.Map
	now        func() time.Time
	stateCount atomic.Int64
	maxStates  int64
	Latency    func(time.Duration)
}

func NewAdmission() *Admission {
	return NewAdmissionWithCapacity(100000)
}
func NewAdmissionWithCapacity(capacity int64) *Admission {
	if capacity < 1 {
		capacity = 100000
	}
	start := time.Now()
	a := &Admission{now: func() time.Time { return start.Add(time.Since(start)) }, maxStates: capacity}
	a.Replace(nil)
	return a
}
func (a *Admission) Replace(policies []Policy) {
	m := make(map[string]Policy, len(policies))
	for _, p := range policies {
		m[p.Key()] = p
	}
	a.policies.Store(&policySet{m})
}

type Rejection struct {
	Policy      Policy        `json:"policy"`
	Reason      string        `json:"reason"`
	RetryAfter  time.Duration `json:"-"`
	Sample      bool          `json:"-"`
	CurrentRate uint64        `json:"current_rps"`
}

func (r *Rejection) Error() string { return r.Reason + " limit exceeded: " + r.Policy.Key() }
func (a *Admission) resolve(scope, subject, operation string) (Policy, bool) {
	return resolvePolicy(a.policies.Load().values, scope, subject, operation)
}
func resolvePolicy(m map[string]Policy, scope, subject, operation string) (Policy, bool) {
	p, ok := m[scope+":"+subject+":"+operation]
	if !ok {
		p, ok = m[scope+":*:"+operation]
	}
	return p, ok
}
func (a *Admission) get(p Policy, subject string) *gate {
	// Wildcard templates still get independent runtime buckets per identity.
	if p.Scope != "global" {
		p.Subject = subject
	}
	value, ok := a.gates.Load(p.Key())
	if !ok {
		if a.stateCount.Add(1) > a.maxStates {
			a.stateCount.Add(-1)
			return nil
		}
		var loaded bool
		value, loaded = a.gates.LoadOrStore(p.Key(), &gate{})
		if loaded {
			a.stateCount.Add(-1)
		}
	}
	g := value.(*gate)
	previous := g.policy.Load()
	if previous == nil || *previous != p {
		cp := p
		g.policy.Store(&cp)
	}
	return g
}
func (g *gate) attempt(now time.Time) {
	sec := uint64(now.Unix())
	g.lastSeen.Store(now.UnixNano())
	g.requests.Add(1)
	for {
		old := g.window.Load()
		count := uint64(1)
		if old>>32 == sec {
			count = (old & 0xffffffff) + 1
		}
		if count > 0xffffffff {
			count = 0xffffffff
		}
		if g.window.CompareAndSwap(old, sec<<32|count) {
			for {
				peak := g.peak.Load()
				if count <= peak || g.peak.CompareAndSwap(peak, count) {
					break
				}
			}
			return
		}
	}
}
func (g *gate) take(p Policy, now time.Time) time.Duration {
	interval := int64(math.Ceil(float64(time.Second) / float64(p.Rate)))
	tolerance := int64(p.Burst-1) * interval
	t := now.UnixNano()
	for {
		old := g.next.Load()
		start := old
		if start < t {
			start = t
		}
		if start-t > tolerance {
			return time.Duration(start - t - tolerance)
		}
		if g.next.CompareAndSwap(old, start+interval) {
			return 0
		}
	}
}
func (g *gate) slot(max int) bool {
	for {
		old := g.active.Load()
		if old >= int64(max) {
			return false
		}
		if g.active.CompareAndSwap(old, old+1) {
			return true
		}
	}
}

// Acquire rejects immediately. Call release exactly once after the operation;
// all connections/tokens of an authenticated identity share the same gates.
func (a *Admission) Acquire(scope, subject, category, operation string) (func(), *Rejection) {
	if a.Latency != nil {
		start := time.Now()
		defer func() { a.Latency(time.Since(start)) }()
	}
	now := a.now()
	configured := a.policies.Load().values
	var selected []*gate
	var policies []Policy
	keys := [][3]string{{"global", "*", "*"}, {"global", "*", category}, {scope, subject, "*"}, {scope, subject, category}, {scope, subject, operation}}
	seen := map[string]bool{}
	for _, k := range keys {
		p, ok := resolvePolicy(configured, k[0], k[1], k[2])
		if !ok || seen[p.Key()] {
			continue
		}
		seen[p.Key()] = true
		g := a.get(p, subject)
		if g == nil {
			return nil, &Rejection{Policy: p, Reason: "limiter capacity", RetryAfter: time.Second}
		}
		selected = append(selected, g)
		policies = append(policies, p)
	}
	acquired := 0
	release := func() {
		for i := 0; i < acquired; i++ {
			selected[i].active.Add(-1)
		}
	}
	for i, g := range selected {
		p := policies[i]
		g.attempt(now)
		if !p.Enabled {
			g.active.Add(1)
			g.allowed.Add(1)
			acquired++
			continue
		}
		retry := g.take(p, now)
		reason := "rate"
		if retry == 0 {
			if g.slot(p.Concurrent) {
				g.allowed.Add(1)
				acquired++
				continue
			}
			reason = "concurrency"
			retry = time.Second
			g.concurrentRejected.Add(1)
		} else {
			g.rateRejected.Add(1)
		}
		g.rejected.Add(1)
		release()
		last := g.lastLog.Load()
		sample := now.UnixNano()-last >= int64(time.Second) && g.lastLog.CompareAndSwap(last, now.UnixNano())
		return nil, &Rejection{Policy: p, Reason: reason, RetryAfter: retry, Sample: sample, CurrentRate: g.window.Load() & 0xffffffff}
	}
	var released atomic.Bool
	return func() {
		if released.CompareAndSwap(false, true) {
			release()
		}
	}, nil
}

type Usage struct {
	Policy
	Requests            uint64 `json:"requests"`
	Allowed             uint64 `json:"allowed"`
	Rejected            uint64 `json:"rejected"`
	RateRejected        uint64 `json:"rate_rejected"`
	ConcurrencyRejected uint64 `json:"concurrency_rejected"`
	CurrentRPS          uint64 `json:"current_rps"`
	PeakRPS             uint64 `json:"peak_rps"`
	Active              int64  `json:"active"`
}

func (a *Admission) Snapshot() []Usage {
	out := []Usage{}
	seen := map[string]bool{}
	sec := uint64(a.now().Unix())
	a.gates.Range(func(_, v any) bool {
		g := v.(*gate)
		p := g.policy.Load()
		if p == nil {
			return true
		}
		u := Usage{Policy: *p, Requests: g.requests.Load(), Allowed: g.allowed.Load(), Rejected: g.rejected.Load(), RateRejected: g.rateRejected.Load(), ConcurrencyRejected: g.concurrentRejected.Load(), PeakRPS: g.peak.Load(), Active: g.active.Load()}
		w := g.window.Load()
		if w>>32 == sec {
			u.CurrentRPS = w & 0xffffffff
		}
		// Display latest configured policy even when no new traffic arrived.
		if latest, ok := a.resolve(p.Scope, p.Subject, p.Operation); ok {
			latest.Subject = p.Subject
			u.Policy = latest
		}
		out = append(out, u)
		seen[u.Key()] = true
		return true
	})
	for _, p := range a.policies.Load().values {
		if !seen[p.Key()] {
			out = append(out, Usage{Policy: p})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

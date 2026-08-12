package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a simple in-memory token-bucket limiter keyed by string.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
}

type bucket struct {
	tokens float64
	last   time.Time
}

func New(ratePerSec, burst int) *Limiter {
	if ratePerSec <= 0 {
		ratePerSec = 100
	}
	if burst <= 0 {
		burst = ratePerSec
	}
	return &Limiter{
		buckets: make(map[string]*bucket),
		rate:    float64(ratePerSec),
		burst:   float64(burst),
	}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.buckets[key]
	if !ok {
		l.buckets[key] = &bucket{tokens: l.burst - 1, last: now}
		return true
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// SlidingWindow counts events in a time window (for auth failure throttling).
type SlidingWindow struct {
	mu      sync.Mutex
	events  map[string][]time.Time
	window  time.Duration
	maxHits int
}

func NewSlidingWindow(window time.Duration, maxHits int) *SlidingWindow {
	if window <= 0 {
		window = time.Minute
	}
	if maxHits <= 0 {
		maxHits = 20
	}
	return &SlidingWindow{
		events:  make(map[string][]time.Time),
		window:  window,
		maxHits: maxHits,
	}
}

func (s *SlidingWindow) Blocked(key string) bool {
	return s.Count(key) >= s.maxHits
}

func (s *SlidingWindow) Allow(key string) bool {
	if s.Blocked(key) {
		return false
	}
	return true
}

func (s *SlidingWindow) Record(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-s.window)
	list := s.events[key]
	n := 0
	for _, t := range list {
		if t.After(cutoff) {
			list[n] = t
			n++
		}
	}
	s.events[key] = append(list[:n], now)
}

func (s *SlidingWindow) Count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-s.window)
	list := s.events[key]
	n := 0
	for _, t := range list {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

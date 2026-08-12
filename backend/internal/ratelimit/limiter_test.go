package ratelimit

import "testing"

func TestLimiterAllowsBurst(t *testing.T) {
	l := New(2, 2)
	if !l.Allow("a") || !l.Allow("a") {
		t.Fatal("burst should allow 2")
	}
	if l.Allow("a") {
		t.Fatal("third should be denied")
	}
}

func TestSlidingWindowFailures(t *testing.T) {
	w := NewSlidingWindow(0, 3)
	if w.Blocked("ip") {
		t.Fatal("should not block initially")
	}
	w.Record("ip")
	w.Record("ip")
	w.Record("ip")
	if !w.Blocked("ip") {
		t.Fatal("should block after max")
	}
}

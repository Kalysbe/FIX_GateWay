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

func TestPolicyUpdateDoesNotRefillExhaustedBucket(t *testing.T) {
	l := New(1, 2)
	if !l.Allow("a") || !l.Allow("a") {
		t.Fatal("expected burst")
	}
	l.Update(2, 3)
	if l.Allow("a") {
		t.Fatal("policy change refilled exhausted bucket")
	}
	l.Update(2, 3)
	if l.Allow("a") {
		t.Fatal("refresh refilled exhausted bucket")
	}
	if l.rate != 2 || l.burst != 3 {
		t.Fatal("policy not updated")
	}
}

func TestPolicyDecreaseClampsExistingTokens(t *testing.T) {
	l := New(100, 100)
	if !l.Allow("a") {
		t.Fatal("expected burst")
	}
	l.Update(1, 1)
	if !l.Allow("a") || l.Allow("a") {
		t.Fatal("lower burst not applied")
	}
}

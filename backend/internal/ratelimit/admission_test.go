package ratelimit

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in paced load driver: does not submit orders or access an exchange.
func TestAdmissionLoadProfiles(t *testing.T) {
	if testing.Short() {
		t.Skip("paced load profiles")
	}
	for _, input := range []int{1000, 5000, 10000, 20000, 50000} {
		t.Run(fmt.Sprint(input), func(t *testing.T) {
			a := NewAdmission()
			limit := input / 2
			a.Replace([]Policy{policy("broker", "A", "Trading", limit, input/10, 500), policy("broker", "B", "Trading", 1000, 200, 100)})
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			timer := time.NewTimer(time.Second)
			defer timer.Stop()
			attempts, allowed, rejected := 0, 0, 0
		loop:
			for {
				select {
				case <-timer.C:
					break loop
				case <-ticker.C:
					batch := int(time.Since(start).Seconds()*float64(input)) - attempts
					if batch > input/10 {
						batch = input / 10
					}
					for i := 0; i < batch; i++ {
						attempts++
						release, r := a.Acquire("broker", "A", "Trading", "SubmitOrder")
						if r != nil {
							rejected++
						} else {
							allowed++
							release()
						}
					}
					take(t, a, "B", "SubmitOrder")()
				}
			}
			elapsed := time.Since(start)
			runtime.GC()
			runtime.ReadMemStats(&after)
			if rejected == 0 || allowed > int(elapsed.Seconds()*float64(limit))+input/10+2 {
				t.Fatalf("unbounded admission: %d/%d", allowed, attempts)
			}
			if a.stateCount.Load() != 2 {
				t.Fatal("unbounded state")
			}
			t.Logf("target=%d/s actual=%.0f/s allowed=%d rejected=%d retained_heap_delta=%d bytes states=%d", input, float64(attempts)/elapsed.Seconds(), allowed, rejected, int64(after.HeapAlloc)-int64(before.HeapAlloc), a.stateCount.Load())
		})
	}
}

func BenchmarkAdmissionAllowed(b *testing.B) {
	a := NewAdmission()
	a.Replace([]Policy{policy("broker", "*", "Trading", 1000000, 1000000, 1000), policy("broker", "*", "SubmitOrder", 1000000, 1000000, 1000)})
	var workers atomic.Int64
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		subject := fmt.Sprint(workers.Add(1))
		for pb.Next() {
			release, r := a.Acquire("broker", subject, "Trading", "SubmitOrder")
			if r != nil {
				b.Fatal(r)
			}
			release()
		}
	})
}

func policy(scope, subject, op string, rate, burst, concurrent int) Policy {
	return Policy{scope, subject, op, rate, burst, concurrent, true}
}
func fakeAdmission(p ...Policy) (*Admission, *time.Time) {
	a := NewAdmission()
	now := time.Unix(1800000000, 0)
	a.now = func() time.Time { return now }
	a.Replace(p)
	return a, &now
}
func take(t *testing.T, a *Admission, broker, op string) func() {
	t.Helper()
	release, denied := a.Acquire("broker", broker, "Trading", op)
	if denied != nil {
		t.Fatal(denied)
	}
	return release
}
func TestAdmissionBurstRefillIsolation(t *testing.T) {
	a, now := fakeAdmission(policy("broker", "*", "Trading", 10, 2, 10))
	take(t, a, "A", "SubmitOrder")()
	take(t, a, "A", "SubmitOrder")()
	if _, r := a.Acquire("broker", "A", "Trading", "SubmitOrder"); r == nil || r.RetryAfter != 100*time.Millisecond {
		t.Fatalf("burst rejection: %v", r)
	}
	take(t, a, "B", "SubmitOrder")()
	*now = now.Add(100 * time.Millisecond)
	take(t, a, "A", "SubmitOrder")()
	if _, r := a.Acquire("broker", "A", "Trading", "SubmitOrder"); r == nil {
		t.Fatal("refill allowed too many")
	}
}
func TestAdmissionConcurrencyDynamicAndMethods(t *testing.T) {
	p := policy("broker", "A", "SubmitOrder", 100, 100, 1)
	a, _ := fakeAdmission(p, policy("broker", "A", "CancelOrder", 100, 100, 2))
	release := take(t, a, "A", "SubmitOrder")
	if _, r := a.Acquire("broker", "A", "Trading", "SubmitOrder"); r == nil || r.Reason != "concurrency" {
		t.Fatal(r)
	}
	take(t, a, "A", "CancelOrder")()
	p.Concurrent = 2
	a.Replace([]Policy{p})
	take(t, a, "A", "SubmitOrder")()
	release()
	release()
	if a.Snapshot()[0].Active < 0 {
		t.Fatal("double release")
	}
	p.Enabled = false
	a.Replace([]Policy{p})
	for i := 0; i < 200; i++ {
		take(t, a, "A", "SubmitOrder")()
	}
}
func TestAdmissionGlobalAndRollback(t *testing.T) {
	a, _ := fakeAdmission(policy("global", "*", "Trading", 100, 100, 1), policy("broker", "A", "Trading", 100, 100, 1))
	release := take(t, a, "A", "SubmitOrder")
	if _, r := a.Acquire("broker", "B", "Trading", "SubmitOrder"); r == nil || r.Policy.Scope != "global" {
		t.Fatal(r)
	}
	release()
	take(t, a, "B", "SubmitOrder")()
	a, _ = fakeAdmission(policy("global", "*", "Trading", 100, 100, 10), policy("broker", "A", "Trading", 1, 1, 10))
	take(t, a, "A", "SubmitOrder")()
	if _, r := a.Acquire("broker", "A", "Trading", "SubmitOrder"); r == nil {
		t.Fatal("expected denial")
	}
	for _, u := range a.Snapshot() {
		if u.Active != 0 {
			t.Fatal("slot leaked", u)
		}
	}
}
func TestAdmissionConcurrentIdentityAndPolicyChanges(t *testing.T) {
	a := NewAdmission()
	p := policy("broker", "A", "Trading", 100000, 100000, 4)
	a.Replace([]Policy{p})
	var running, peak atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				release, r := a.Acquire("broker", "A", "Trading", "SubmitOrder")
				if r != nil {
					continue
				}
				n := running.Add(1)
				for {
					old := peak.Load()
					if old >= n || peak.CompareAndSwap(old, n) {
						break
					}
				}
				running.Add(-1)
				release()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			a.Replace([]Policy{p})
		}
	}()
	wg.Wait()
	if peak.Load() > 4 {
		t.Fatal("concurrency exceeded", peak.Load())
	}
	for _, u := range a.Snapshot() {
		if u.Active != 0 {
			t.Fatal("slot leaked")
		}
	}
}
func TestAdmissionDynamicDoesNotRefill(t *testing.T) {
	p := policy("broker", "A", "Trading", 10, 2, 5)
	a, _ := fakeAdmission(p)
	take(t, a, "A", "SubmitOrder")()
	take(t, a, "A", "SubmitOrder")()
	for i := 0; i < 100; i++ {
		a.Replace([]Policy{p})
		if _, r := a.Acquire("broker", "A", "Trading", "SubmitOrder"); r == nil {
			t.Fatal("config bypass")
		}
	}
}
func TestAdmissionSustainedOverload(t *testing.T) {
	a, now := fakeAdmission(policy("broker", "A", "Trading", 5000, 1000, 500), policy("broker", "B", "Trading", 1000, 200, 100))
	allowed := 0
	for i := 0; i < 100000; i++ {
		if release, r := a.Acquire("broker", "A", "Trading", "SubmitOrder"); r == nil {
			allowed++
			release()
		}
		if i%10 == 0 {
			take(t, a, "B", "SubmitOrder")()
		}
		*now = now.Add(100 * time.Microsecond)
	}
	// 10 sec sustained 5k/sec, with an initially full burst of 1000.
	if allowed < 50990 || allowed > 51000 {
		t.Fatalf("allowed %d", allowed)
	}
	if a.stateCount.Load() != 2 {
		t.Fatal("state growth", a.stateCount.Load())
	}
}
func TestAdmissionCapacityBound(t *testing.T) {
	a := NewAdmissionWithCapacity(2)
	a.Replace([]Policy{policy("broker", "*", "Trading", 100, 100, 10)})
	for i := 0; i < 100; i++ {
		release, r := a.Acquire("broker", fmt.Sprint(i), "Trading", "SubmitOrder")
		if r == nil {
			release()
		}
	}
	if a.stateCount.Load() != 2 {
		t.Fatal(a.stateCount.Load())
	}
}
func BenchmarkAdmission(b *testing.B) {
	for _, rate := range []int{1000, 5000, 10000, 20000, 50000} {
		b.Run(fmt.Sprint(rate), func(b *testing.B) {
			a := NewAdmission()
			a.Replace([]Policy{policy("global", "*", "Trading", rate, 1000, 500), policy("broker", "A", "Trading", rate, 1000, 500)})
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					release, r := a.Acquire("broker", "A", "Trading", "SubmitOrder")
					if r == nil {
						release()
					}
				}
			})
		})
	}
}

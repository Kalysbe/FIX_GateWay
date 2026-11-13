package main

import (
	"sync"
	"testing"
	"time"
)

// A session back within the grace period raises nothing; one that stays down
// raises a single alarm for the outage.
func TestSessionDownAlarmWaitsForGrace(t *testing.T) {
	var mu sync.Mutex
	raised := map[string]int{}
	a := newSessionDownAlarm(50*time.Millisecond, func(id, _ string) {
		mu.Lock()
		raised[id]++
		mu.Unlock()
	})
	a.down("quik-fix-1", "s1")
	a.up("quik-fix-1")
	a.down("quik-fix-2", "s2")
	a.down("quik-fix-2", "s2") // a second disconnect of the same outage
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if raised["quik-fix-1"] != 0 || raised["quik-fix-2"] != 1 {
		t.Fatalf("raised %v", raised)
	}
}

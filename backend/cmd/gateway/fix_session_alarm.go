package main

import (
	"sync"
	"time"
)

// fixSessionDownGrace is how long a FIX session may stay down before
// FIX_SESSION_DOWN is raised. A gateway restart or a dropped socket is back
// within one reconnect interval (5s); the QUIK adapter also discards the first
// logon while it is still closing the previous session.
const fixSessionDownGrace = 30 * time.Second

// sessionDownAlarm raises one alarm per outage of an adapter's session that
// lasts longer than the grace period; a logon in time cancels it.
type sessionDownAlarm struct {
	mu     sync.Mutex
	grace  time.Duration
	timers map[string]*time.Timer
	raise  func(adapterID, sessionID string)
}

func newSessionDownAlarm(grace time.Duration, raise func(adapterID, sessionID string)) *sessionDownAlarm {
	return &sessionDownAlarm{grace: grace, timers: map[string]*time.Timer{}, raise: raise}
}

func (a *sessionDownAlarm) down(adapterID, sessionID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.timers[adapterID] != nil {
		return
	}
	var t *time.Timer
	t = time.AfterFunc(a.grace, func() {
		a.mu.Lock()
		current := a.timers[adapterID] == t
		if current {
			delete(a.timers, adapterID)
		}
		a.mu.Unlock()
		if current {
			a.raise(adapterID, sessionID)
		}
	})
	a.timers[adapterID] = t
}

func (a *sessionDownAlarm) up(adapterID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if t := a.timers[adapterID]; t != nil {
		t.Stop()
		delete(a.timers, adapterID)
	}
}

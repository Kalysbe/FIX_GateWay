package quikexport

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestPositionFeedReportsOnlyRealChanges(t *testing.T) {
	row := func(balance string) Positions {
		return Positions{Cash: []Row{{Raw: json.RawMessage(`{"FirmId":"2040","CurrentBal":` + balance + `}`)}}, Instruments: []Row{}}
	}
	type step struct {
		positions Positions
		err       error
	}
	steps := []step{
		{positions: row("1")},            // baseline, not a change
		{positions: row("1")},            // same
		{err: errors.New("unavailable")}, // not an empty table
		{positions: row("1")},            // same as before the failure
		{positions: row("2")},            // change
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n, changes, failures := 0, 0, 0
	read := func(_ context.Context, firms []string) (Positions, error) {
		if firms != nil {
			t.Errorf("the feed must read every firm: %v", firms)
		}
		if n == len(steps)-1 {
			cancel()
		}
		s := steps[n]
		n++
		return s.positions, s.err
	}
	runPositionFeed(ctx, read, time.Millisecond, func() { changes++ }, func(error) { failures++ })
	if changes != 1 || failures != 1 {
		t.Fatalf("changes=%d failures=%d", changes, failures)
	}
}

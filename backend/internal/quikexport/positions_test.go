package quikexport

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPositionFeedReportsOnlyRealChangesPerTable(t *testing.T) {
	row := func(cash, depo string) Positions {
		return Positions{
			Cash:        []Row{{Raw: json.RawMessage(`{"FirmId":"2040","CurrentBal":` + cash + `}`)}},
			Instruments: []Row{{Raw: json.RawMessage(`{"FirmId":"2040","CurrentBal":` + depo + `}`)}},
		}
	}
	type step struct {
		positions Positions
		err       error
	}
	steps := []step{
		{positions: row("1", "5")},       // baseline, not a change
		{positions: row("1", "5")},       // same
		{err: errors.New("unavailable")}, // not an empty table
		{positions: row("1", "5")},       // same as before the failure
		{positions: row("2", "5")},       // money only
		{positions: row("2", "6")},       // securities only
		{positions: row("3", "7")},       // both
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n, failures := 0, 0
	var changes []string
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
	runPositionFeed(ctx, read, time.Millisecond, func(table string) { changes = append(changes, table) }, func(error) { failures++ })
	want := []string{MoneyLimits, DepoLimits, MoneyLimits, DepoLimits}
	if !reflect.DeepEqual(changes, want) || failures != 1 {
		t.Fatalf("changes=%v failures=%d", changes, failures)
	}
}

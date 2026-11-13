package executioninbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
)

// cases is an in-memory stand-in for the operator case table.
type cases struct {
	mu  sync.Mutex
	ids map[uuid.UUID]bool
}

func (c *cases) held(_ context.Context, id uuid.UUID) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ids[id], nil
}

func (c *cases) open(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ids[id] = true
}

func runInbox(t *testing.T, in *Inbox, apply func(context.Context, Entry) error) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		in.Run(ctx, apply, nil)
	}()
	return func() { cancel(); <-done }
}

func TestQuarantineUnblocksTradingAndKeepsEvidence(t *testing.T) {
	dir := t.TempDir()
	in := openTest(t, dir)
	defer in.Close()
	c := &cases{ids: map[uuid.UUID]bool{}}
	in.SetQuarantineCheck(c.held)
	for _, id := range []string{"bust", "fill"} {
		if err := in.Append(domain.ExecutionReport{ClOrdID: id}); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var applied []string
	stop := runInbox(t, in, func(_ context.Context, e Entry) error {
		if e.Report.ClOrdID == "bust" {
			return errors.New("execution correction requires venue reconciliation")
		}
		mu.Lock()
		applied = append(applied, e.Report.ClOrdID)
		mu.Unlock()
		return nil
	})
	defer stop()
	waitFor(t, func() bool { _, ok := in.Blocked(); return ok })
	b, _ := in.Blocked()
	if b.Entry.Report.ClOrdID != "bust" || b.Attempts < 1 || b.Error == "" || b.ReadError {
		t.Fatalf("blocked = %+v", b)
	}
	if in.Status().Healthy() {
		t.Fatal("failed head must pause trading")
	}
	if err := in.WithBlocked(uuid.New(), func(Blocked) error { return nil }); !errors.Is(err, ErrNotBlocked) {
		t.Fatalf("other entry: %v", err)
	}
	if err := in.WithBlocked(b.Entry.ID, func(Blocked) error { return errors.New("db down") }); err == nil {
		t.Fatal("case failure must be returned")
	}
	if in.Status().Pending != 2 {
		t.Fatal("failed case must not move the entry")
	}
	if err := in.WithBlocked(b.Entry.ID, func(Blocked) error { c.open(b.Entry.ID); return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return in.Status().Pending == 0 })
	if !in.Status().Healthy() {
		t.Fatal("trading not resumed after quarantine")
	}
	mu.Lock()
	if len(applied) != 1 || applied[0] != "fill" {
		t.Fatalf("applied = %v", applied)
	}
	mu.Unlock()
	if _, err := os.Stat(filepath.Join(dir, QuarantineDir, b.Name)); err != nil {
		t.Fatalf("quarantined evidence missing: %v", err)
	}
}

func TestQuarantineCaseSurvivesCrashBeforeMove(t *testing.T) {
	dir := t.TempDir()
	in := openTest(t, dir)
	if err := in.Append(domain.ExecutionReport{ClOrdID: "held"}); err != nil {
		t.Fatal(err)
	}
	id := in.queue[0].name
	in.Close()
	entryID, err := EntryID(id)
	if err != nil {
		t.Fatal(err)
	}
	// The case was committed, then the process stopped before the move.
	c := &cases{ids: map[uuid.UUID]bool{entryID: true}}
	in = openTest(t, dir)
	defer in.Close()
	in.SetQuarantineCheck(c.held)
	stop := runInbox(t, in, func(context.Context, Entry) error {
		t.Error("an entry with an operator case must never be applied")
		return nil
	})
	defer stop()
	waitFor(t, func() bool { return in.Status().Pending == 0 })
	if _, err := os.Stat(filepath.Join(dir, QuarantineDir, id)); err != nil {
		t.Fatal(err)
	}
	stop()
	in.Close()
	// Quarantined files are not replayed on the next start.
	in = openTest(t, dir)
	if in.Status().Pending != 0 {
		t.Fatal("quarantined entry replayed")
	}
}

func TestUnreadableEntryCanBeQuarantined(t *testing.T) {
	dir := t.TempDir()
	in := openTest(t, dir)
	defer in.Close()
	c := &cases{ids: map[uuid.UUID]bool{}}
	in.SetQuarantineCheck(c.held)
	if err := in.Append(domain.ExecutionReport{ClOrdID: "x"}); err != nil {
		t.Fatal(err)
	}
	name := in.queue[0].name
	if err := os.WriteFile(filepath.Join(dir, name), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	stop := runInbox(t, in, func(context.Context, Entry) error { return nil })
	defer stop()
	waitFor(t, func() bool { _, ok := in.Blocked(); return ok })
	b, _ := in.Blocked()
	if !b.ReadError {
		t.Fatalf("blocked = %+v", b)
	}
	id, _ := EntryID(b.Name)
	if err := in.WithBlocked(id, func(Blocked) error { c.open(id); return nil }); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return in.Status().Pending == 0 && in.Status().Healthy() })
}

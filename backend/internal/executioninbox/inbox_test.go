package executioninbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

func openTest(t *testing.T, dir string) *Inbox {
	t.Helper()
	in, err := Open(dir, 1<<20, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for inbox condition")
}

func TestDurableReplayAcrossRestartAndDBFailure(t *testing.T) {
	dir := t.TempDir()
	in := openTest(t, dir)
	for _, id := range []string{"ack", "partial", "fill", "cancel", "cancel-reject"} {
		if err := in.Append(domain.ExecutionReport{ClOrdID: id, CancelReject: id == "cancel-reject"}); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		in.Run(ctx, func(context.Context, Entry) error { return errors.New("database unavailable") }, nil)
	}()
	waitFor(t, func() bool { return in.Status().Failed })
	if in.Status().Pending != 5 {
		t.Fatal("report lost on DB failure")
	}
	cancel()
	<-done
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	in = openTest(t, dir)
	defer in.Close()
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	done = make(chan struct{})
	var received []string
	go func() {
		defer close(done)
		in.Run(ctx, func(_ context.Context, e Entry) error { received = append(received, e.Report.ClOrdID); return nil }, nil)
	}()
	waitFor(t, func() bool { return in.Status().Pending == 0 })
	cancel()
	<-done
	if len(received) != 5 {
		t.Fatalf("recovered %d reports", len(received))
	}
	for i, id := range []string{"ack", "partial", "fill", "cancel", "cancel-reject"} {
		if received[i] != id {
			t.Fatalf("reordered reports: %v", received)
		}
	}
	if !in.Status().Healthy() {
		t.Fatal("inbox did not recover readiness")
	}
}

func TestInboxRejectsSecondOwnerAndCorruptJournal(t *testing.T) {
	dir := t.TempDir()
	in := openTest(t, dir)
	if second, err := Open(dir, 1<<20, 100, nil); err == nil {
		second.Close()
		t.Fatal("second active gateway accepted")
	}
	if err := in.Append(domain.ExecutionReport{ClOrdID: "persisted"}); err != nil {
		t.Fatal(err)
	}
	in.Close()
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if filepath.Ext(f.Name()) == ".json" {
			if err := os.WriteFile(filepath.Join(dir, f.Name()), []byte("partial/corrupt"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if corrupt, err := Open(dir, 1<<20, 100, nil); err == nil {
		corrupt.Close()
		t.Fatal("corrupt ledger journal accepted")
	}
}

func TestInboxCapacityNeverDiscardsAcceptedReports(t *testing.T) {
	in, err := Open(t.TempDir(), 1<<20, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if err = in.Append(domain.ExecutionReport{ClOrdID: "first"}); err != nil {
		t.Fatal(err)
	}
	if err = in.Append(domain.ExecutionReport{ClOrdID: "second"}); err == nil {
		t.Fatal("capacity bound ignored")
	}
	if in.Status().Pending != 1 {
		t.Fatal("accepted report discarded")
	}
}

func TestCrashAfterApplyBeforeRemovalReplaysSameReceipt(t *testing.T) {
	dir := t.TempDir()
	in := openTest(t, dir)
	if err := in.Append(domain.ExecutionReport{ClOrdID: "order"}); err != nil {
		t.Fatal(err)
	}
	head, _ := in.head()
	first, _, err := in.read(head.name)
	if err != nil {
		t.Fatal(err)
	}
	in.Close()
	in = openTest(t, dir)
	defer in.Close()
	head, _ = in.head()
	replay, _, err := in.read(head.name)
	if err != nil {
		t.Fatal(err)
	}
	if replay.ID != first.ID {
		t.Fatal("receipt changed across crash")
	}
}

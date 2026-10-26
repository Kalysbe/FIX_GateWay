package executioninbox

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
)

// "ГКВ" in Windows-1251, as QUIK sends instrument names in tag 55.
const cp1251Name = "\xc3\xca\xc2-12"

func cp1251Report() domain.ExecutionReport {
	return domain.ExecutionReport{AdapterID: "quik-fix-1", ClOrdID: "G1", ExecID: "E1", ExecType: "2", LastQty: "10",
		Text: "SENTI//", RawFIX: "8=FIX.4.2|35=8|55=" + cp1251Name + "|58=SENTI//|10=080|"}
}

func runOnce(t *testing.T, in *Inbox) []Entry {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var got []Entry
	go func() {
		defer close(done)
		in.Run(ctx, func(_ context.Context, e Entry) error { got = append(got, e); return nil }, nil)
	}()
	waitFor(t, func() bool { s := in.Status(); return s.Pending == 0 || s.Failed })
	cancel()
	<-done
	return got
}

func TestReportWithInvalidUTF8IsAppliedAfterRestart(t *testing.T) {
	dir := t.TempDir()
	in := openTest(t, dir)
	if err := in.Append(cp1251Report()); err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	in = openTest(t, dir)
	defer in.Close()
	got := runOnce(t, in)
	if !in.Status().Healthy() || len(got) != 1 || got[0].Report.ExecID != "E1" || !utf8.ValidString(got[0].Report.RawFIX) {
		t.Fatalf("report with Windows-1251 text not applied: %+v %+v", in.Status(), got)
	}
}

// Entries written before the fix hashed the escaped form of invalid UTF-8.
func writeLegacyEntry(t *testing.T, dir string) (string, Entry) {
	t.Helper()
	e := Entry{ID: uuid.New(), ReceivedAt: time.Date(2026, 10, 26, 10, 20, 20, 851665248, time.UTC), Report: cp1251Report()}
	raw, _ := json.Marshal(e)
	e.Checksum = checksumOf(raw)
	raw, _ = json.Marshal(e)
	escaped := string(jsonRuneErrorEscape)
	if !strings.Contains(string(raw), "55="+escaped+escaped+escaped+"-12") {
		t.Fatalf("unexpected legacy encoding: %s", raw)
	}
	name := "01793010020851665248_" + e.ID.String() + ".json"
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return name, e
}

func TestLegacyEntryWithInvalidUTF8IsAccepted(t *testing.T) {
	dir := t.TempDir()
	_, e := writeLegacyEntry(t, dir)
	in := openTest(t, dir)
	defer in.Close()
	got := runOnce(t, in)
	if !in.Status().Healthy() || len(got) != 1 || got[0].ID != e.ID || got[0].Report.LastQty != "10" {
		t.Fatalf("legacy entry not applied: %+v %+v", in.Status(), got)
	}
}

func TestChangedEntryStillFailsChecksum(t *testing.T) {
	dir := t.TempDir()
	name, _ := writeLegacyEntry(t, dir)
	path := filepath.Join(dir, name)
	raw, _ := os.ReadFile(path)
	changed := strings.Replace(string(raw), `"LastQty":"10"`, `"LastQty":"11"`, 1)
	if changed == string(raw) {
		t.Fatal("fixture has no LastQty")
	}
	if err := os.WriteFile(path, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	in, err := Open(dir, 1<<20, 100, nil)
	if err == nil {
		defer in.Close()
		runOnce(t, in)
		if in.Status().Healthy() {
			t.Fatal("changed entry accepted")
		}
		return
	}
	if !strings.Contains(err.Error(), "checksum mismatch") && !errors.Is(err, os.ErrInvalid) {
		t.Fatal(err)
	}
}

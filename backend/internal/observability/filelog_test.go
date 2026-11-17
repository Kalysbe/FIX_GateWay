package observability

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFileLogNamesFileByBishkekDate(t *testing.T) {
	dir := t.TempDir()
	logs, err := OpenFileLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	// 20:30 UTC on 5 November is 02:30 on 6 November in Bishkek.
	ts := time.Date(2026, 11, 5, 20, 30, 0, 0, time.UTC)
	if err := logs.Append(Event{Type: TypeSystem, Subtype: "X", Timestamp: ts}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "2026-11-06.log")); err != nil {
		t.Fatal("file not named by the Bishkek date:", err)
	}
}

func TestFileLogKeepsSevenDays(t *testing.T) {
	dir := t.TempDir()
	logs, err := OpenFileLog(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, LogZone)
	for i := 0; i < 10; i++ {
		if err := logs.Append(Event{Type: TypeSystem, Timestamp: now.AddDate(0, 0, -i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := logs.Cleanup(now); err != nil {
		t.Fatal(err)
	}
	days, err := logs.Days()
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 7 || days[0] != "2026-11-06" || days[6] != "2026-10-31" {
		t.Fatalf("kept days = %v", days)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.txt")); err != nil {
		t.Fatal("cleanup removed a file that is not a log")
	}
}

func TestStoreListsAndCountsFromFiles(t *testing.T) {
	logs, err := OpenFileLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	s := NewStore(nil, logs)
	ctx := context.Background()
	broker := int64(7)
	base := time.Now().Add(-time.Minute)
	for i := 0; i < 5; i++ {
		e := Event{Type: TypeAPI, Subtype: SubOrderCreate, BrokerID: &broker, Timestamp: base.Add(time.Duration(i) * time.Second)}
		if i == 4 {
			e.Type, e.Subtype = TypeError, ErrFIX
		}
		if err := s.InsertBatch(ctx, []Event{e}); err != nil {
			t.Fatal(err)
		}
		// Reads between writes parse only the appended lines.
		if n, err := s.Count(ctx, Filter{BrokerID: &broker}); err != nil || n != int64(i+1) {
			t.Fatalf("count after %d writes = %d, %v", i+1, n, err)
		}
	}
	page, more, err := s.List(ctx, Filter{Type: TypeAPI, Limit: 3})
	if err != nil || !more || len(page) != 3 || !page[0].Timestamp.After(page[1].Timestamp) {
		t.Fatalf("first page = %d, more=%v, %v", len(page), more, err)
	}
	last := page[len(page)-1]
	rest, more, err := s.List(ctx, Filter{Type: TypeAPI, Limit: 3, CursorTS: last.Timestamp, CursorID: last.ID})
	if err != nil || more || len(rest) != 1 {
		t.Fatalf("second page = %d, more=%v, %v", len(rest), more, err)
	}
	got, err := s.Get(ctx, rest[0].ID)
	if err != nil || got.ID != rest[0].ID {
		t.Fatalf("get = %v, %v", got.ID, err)
	}
	o, err := s.Overview(ctx)
	if err != nil || o.OrdersToday != 4 || o.ErrorsToday != 1 || o.ActiveBrokers != 1 {
		t.Fatalf("overview = %+v, %v", o, err)
	}
}

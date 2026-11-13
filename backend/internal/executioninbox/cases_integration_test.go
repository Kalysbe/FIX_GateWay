//go:build integration

package executioninbox

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQuarantineCaseLifecycle(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	if err := migrate.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := NewCaseStore(pool)
	id := uuid.New()
	defer pool.Exec(ctx, `DELETE FROM execution_inbox_quarantine WHERE entry_id=$1`, id)
	b := Blocked{
		Name:  "00000000000000000001_" + id.String() + ".json",
		Entry: Entry{ID: id, ReceivedAt: time.Now().UTC(), Report: domain.ExecutionReport{AdapterID: "quik-fix-1", ClOrdID: "C1", ExecID: "E1", ExecType: "H", RawFIX: "35=8|150=H"}},
		Error: "execution correction requires venue reconciliation", FailedAt: time.Now().UTC(), Attempts: 3,
	}
	if held, err := store.Exists(ctx, id); err != nil || held {
		t.Fatalf("exists before case: %v %v", held, err)
	}
	if _, err := store.Open(ctx, id, b, nil, "admin-1", "  "); !errors.Is(err, ErrCaseNote) {
		t.Fatalf("empty reason: %v", err)
	}
	c, err := store.Open(ctx, id, b, nil, "admin-1", "ARQA bust; reconcile manually")
	if err != nil {
		t.Fatal(err)
	}
	if c.ExecType != "H" || c.Attempts != 3 || c.ResolvedAt != nil || len(c.Report) == 0 {
		t.Fatalf("case = %+v", c)
	}
	// A repeated case after a crash keeps the original decision.
	again, err := store.Open(ctx, id, b, nil, "admin-2", "other")
	if err != nil || again.QuarantinedBy != "admin-1" {
		t.Fatalf("repeat = %+v %v", again, err)
	}
	if held, err := store.Exists(ctx, id); err != nil || !held {
		t.Fatalf("exists: %v %v", held, err)
	}
	open, err := store.List(ctx, true, 500)
	if err != nil || !containsCase(open, id) {
		t.Fatalf("open list: %v", err)
	}
	if _, err := store.Resolve(ctx, id, "admin-1", ""); !errors.Is(err, ErrCaseNote) {
		t.Fatalf("empty note: %v", err)
	}
	done, err := store.Resolve(ctx, id, "admin-1", "Trade cancelled in QUIK; ledger corrected")
	if err != nil || done.ResolvedAt == nil || *done.ResolutionNote == "" {
		t.Fatalf("resolve = %+v %v", done, err)
	}
	if _, err := store.Resolve(ctx, id, "admin-1", "again"); !errors.Is(err, ErrCaseResolved) {
		t.Fatalf("double resolve: %v", err)
	}
	if _, err := store.Resolve(ctx, uuid.New(), "admin-1", "x"); !errors.Is(err, ErrCaseNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if held, _ := store.Exists(ctx, id); !held {
		t.Fatal("a resolved case must still keep the entry from being replayed")
	}
	open, _ = store.List(ctx, true, 500)
	if containsCase(open, id) {
		t.Fatal("resolved case listed as open")
	}
	// Unreadable file: no report or received time is stored.
	unreadable := uuid.New()
	defer pool.Exec(ctx, `DELETE FROM execution_inbox_quarantine WHERE entry_id=$1`, unreadable)
	u, err := store.Open(ctx, unreadable, Blocked{Name: "2_" + unreadable.String() + ".json", Error: "checksum mismatch", ReadError: true}, nil, "admin-1", "corrupt file")
	if err != nil || u.Report != nil || u.ReceivedAt != nil {
		t.Fatalf("unreadable = %+v %v", u, err)
	}
}

func containsCase(list []Case, id uuid.UUID) bool {
	for _, c := range list {
		if c.EntryID == id {
			return true
		}
	}
	return false
}

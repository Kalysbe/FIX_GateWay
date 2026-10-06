//go:build integration

package outbox

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDurableQueueCrashRecoveryIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := "queue_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	migration, err := os.ReadFile("../../migrations/000001_init.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	ddl := strings.Split(strings.Split(string(migration), "CREATE TABLE outbox_events (")[1], ");")[0]
	if _, err = pool.Exec(ctx, "CREATE TABLE outbox_events ("+ddl+"); CREATE TABLE orders (id UUID PRIMARY KEY, fix_send_attempted_at TIMESTAMPTZ)"); err != nil {
		t.Fatal(err)
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for i, id := range ids {
		var fence any
		if i == 1 {
			fence = time.Now()
		}
		if _, err = pool.Exec(ctx, "INSERT INTO orders VALUES ($1,$2)", id, fence); err != nil {
			t.Fatal(err)
		}
		event := "ORDER_SEND"
		if i == 2 {
			event = "ORDER_CANCEL"
		}
		if _, err = pool.Exec(ctx, `INSERT INTO outbox_events (aggregate_type,aggregate_id,event_type,payload,status,locked_at,locked_by,attempts) VALUES ('order',$1,$2,jsonb_build_object('order_id',$1::text),'PROCESSING',now()-interval '5 minutes','dead-worker',100)`, id.String(), event); err != nil {
			t.Fatal(err)
		}
	}
	store := NewStore(pool)
	if n, err := store.ReclaimStale(ctx, time.Minute); err != nil || n != 3 {
		t.Fatalf("reclaim: n=%d err=%v", n, err)
	}
	for i, id := range ids {
		var status string
		if err = pool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE aggregate_id=$1", id.String()).Scan(&status); err != nil {
			t.Fatal(err)
		}
		expected := "FAILED"
		if i == 0 {
			expected = "PENDING"
		}
		if status != expected {
			t.Fatalf("event %d status=%s want=%s", i, status, expected)
		}
	}
	events, err := store.ClaimBatch(ctx, "new-worker", 20)
	if err != nil || len(events) != 1 {
		t.Fatalf("claim: %v %v", events, err)
	}
	if err = store.MarkRetry(ctx, events[0].ID, "adapter offline", time.Second); err != nil {
		t.Fatal(err)
	}
	if err = store.MarkDone(ctx, events[0].ID); err == nil {
		t.Fatal("stale completion must not overwrite queued retry")
	}
	if n, err := store.PendingCount(ctx); err != nil || n != 3 {
		t.Fatalf("durable backlog=%d err=%v", n, err)
	}
	// Recover again even beyond the old retry limit, and preserve the completed row.
	if _, err = pool.Exec(ctx, "UPDATE outbox_events SET available_at=now() WHERE status='PENDING'"); err != nil {
		t.Fatal(err)
	}
	events, err = store.ClaimBatch(ctx, "recovered-worker", 20)
	if err != nil || len(events) != 1 || events[0].Attempts != 102 {
		t.Fatalf("retry recovery: %v %v", events, err)
	}
	if err = store.MarkDone(ctx, events[0].ID); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM outbox_events").Scan(&retained); err != nil || retained != 3 {
		t.Fatalf("requests lost: %d %v", retained, err)
	}
}

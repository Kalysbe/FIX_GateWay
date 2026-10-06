//go:build integration

package outbox

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestReconciliationEvidenceAndAtomicClosure(t *testing.T) {
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
	schema := "reconciliation_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
	_, err = pool.Exec(ctx, `CREATE TABLE orders(id uuid PRIMARY KEY,client_order_id text,broker_id bigint,status text,fix_send_attempted_at timestamptz);
 CREATE TABLE order_events(id bigserial PRIMARY KEY,order_id uuid,event_type text,to_status text,created_at timestamptz DEFAULT now());
 CREATE TABLE outbox_events(id bigserial PRIMARY KEY,payload jsonb,event_type text,status text,last_error text,locked_by text,locked_at timestamptz,created_at timestamptz DEFAULT now(),updated_at timestamptz DEFAULT now());`)
	if err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../../migrations/000024_outbox_reconciliation.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	for _, kind := range []string{"ORDER_SEND", "ORDER_CANCEL"} {
		t.Run(kind, func(t *testing.T) {
			id := uuid.New()
			var eventID int64
			_, err := pool.Exec(ctx, `INSERT INTO orders VALUES($1,'client',1,'SENT',now())`, id)
			if err != nil {
				t.Fatal(err)
			}
			err = pool.QueryRow(ctx, `INSERT INTO outbox_events(payload,event_type,status,last_error) VALUES(jsonb_build_object('order_id',$1::text),$2,'FAILED','RECONCILIATION_REQUIRED: unknown') RETURNING id`, id.String(), kind).Scan(&eventID)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.ResolveReconciliation(ctx, eventID, "operator", " "); !errors.Is(err, ErrReconciliationNote) {
				t.Fatalf("blank note: %v", err)
			}
			if err = store.ResolveReconciliation(ctx, eventID, "operator", "checked"); !errors.Is(err, ErrReconciliationConflict) {
				t.Fatalf("no evidence: %v", err)
			}
			// Old or ignored reports do not count.
			_, err = pool.Exec(ctx, `INSERT INTO order_events(order_id,event_type,to_status,created_at) VALUES($1,'ORDER_EXECUTION_REPORT','FILLED',now()-interval '1 day'),($1,'EXECUTION_REPORT','FILLED',now())`, id)
			if err != nil {
				t.Fatal(err)
			}
			if err = store.ResolveReconciliation(ctx, eventID, "operator", "checked"); !errors.Is(err, ErrReconciliationConflict) {
				t.Fatalf("invalid evidence: %v", err)
			}
			_, err = pool.Exec(ctx, `INSERT INTO order_events(order_id,event_type,to_status) VALUES($1,'ORDER_EXECUTION_REPORT','ACKNOWLEDGED')`, id)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "ORDER_CANCEL" {
				if err = store.ResolveReconciliation(ctx, eventID, "operator", "checked"); !errors.Is(err, ErrReconciliationConflict) {
					t.Fatalf("ACK cannot resolve cancel: %v", err)
				}
				_, err = pool.Exec(ctx, `INSERT INTO order_events(order_id,event_type,to_status) VALUES($1,'ORDER_EXECUTION_REPORT','CANCELLED')`, id)
				if err != nil {
					t.Fatal(err)
				}
			}
			cases, err := store.ListReconciliation(ctx, &id, 100)
			if err != nil || len(cases) != 1 || cases[0].EvidenceEventID == nil {
				t.Fatalf("queue: %+v %v", cases, err)
			}
			var wg sync.WaitGroup
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results <- store.ResolveReconciliation(ctx, eventID, "operator", "confirmed FIX report")
				}()
			}
			wg.Wait()
			close(results)
			successes, conflicts := 0, 0
			for result := range results {
				if result == nil {
					successes++
				} else if errors.Is(result, ErrReconciliationConflict) {
					conflicts++
				} else {
					t.Fatal(result)
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("concurrent closure: %d successes %d conflicts", successes, conflicts)
			}
			var status, orderStatus string
			var fenced bool
			var history int
			if err = pool.QueryRow(ctx, `SELECT e.status,o.status,o.fix_send_attempted_at IS NOT NULL,(SELECT count(*) FROM outbox_reconciliations WHERE event_id=e.id) FROM outbox_events e JOIN orders o ON o.id=$2 WHERE e.id=$1`, eventID, id).Scan(&status, &orderStatus, &fenced, &history); err != nil {
				t.Fatal(err)
			}
			if status != "DONE" || orderStatus != "SENT" || !fenced || history != 1 {
				t.Fatalf("closure changed trading state: %s %s %v %d", status, orderStatus, fenced, history)
			}
			cases, err = store.ListReconciliation(ctx, &id, 100)
			if err != nil || cases[0].ResolvedAt == nil || *cases[0].Note != "confirmed FIX report" {
				t.Fatalf("history: %+v %v", cases, err)
			}
		})
	}
	// Audit persistence failure must roll back queue completion.
	_, err = pool.Exec(ctx, `ALTER TABLE outbox_reconciliations ADD CONSTRAINT reject_actor CHECK(actor_id <> 'rejected')`)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	var eventID int64
	_, err = pool.Exec(ctx, `INSERT INTO orders VALUES($1,'atomic',1,'ACKNOWLEDGED',now())`, id)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO outbox_events(payload,event_type,status,last_error) VALUES(jsonb_build_object('order_id',$1::text),'ORDER_SEND','FAILED','RECONCILIATION_REQUIRED: unknown') RETURNING id`, id.String()).Scan(&eventID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO order_events(order_id,event_type,to_status) VALUES($1,'ORDER_EXECUTION_REPORT','ACKNOWLEDGED')`, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.ResolveReconciliation(ctx, eventID, "rejected", "checked"); err == nil {
		t.Fatal("audit constraint must reject closure")
	}
	var status string
	if err = pool.QueryRow(ctx, `SELECT status FROM outbox_events WHERE id=$1`, eventID).Scan(&status); err != nil || status != "FAILED" {
		t.Fatalf("atomic rollback: %s %v", status, err)
	}
	// Automatic closure uses the same evidence gate and preserves unconfirmed cases.
	unknownID := uuid.New()
	if _, err = pool.Exec(ctx, `INSERT INTO orders VALUES($1,'unknown',1,'SENT',now())`, unknownID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO outbox_events(payload,event_type,status,last_error) VALUES(jsonb_build_object('order_id',$1::text),'ORDER_SEND','FAILED','RECONCILIATION_REQUIRED: unknown')`, unknownID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE outbox_reconciliations ADD CONSTRAINT reject_system CHECK(actor_id <> 'system:fix-reconciliation')`); err != nil {
		t.Fatal(err)
	}
	if n, err := store.ReconcileConfirmed(ctx, 1); err == nil || n != 0 {
		t.Fatalf("automatic audit failure: %d %v", n, err)
	}
	if err = pool.QueryRow(ctx, `SELECT status FROM outbox_events WHERE id=$1`, eventID).Scan(&status); err != nil || status != "FAILED" {
		t.Fatalf("automatic audit rollback: %s %v", status, err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE outbox_reconciliations DROP CONSTRAINT reject_system`); err != nil {
		t.Fatal(err)
	}
	if n, err := store.ReconcileConfirmed(ctx, 1); err != nil || n != 1 {
		t.Fatalf("automatic closure: %d %v", n, err)
	}
	if n, err := store.ReconcileConfirmed(ctx, 100); err != nil || n != 0 {
		t.Fatalf("automatic repeat: %d %v", n, err)
	}
	cases, err := store.ListReconciliation(ctx, &id, 100)
	if err != nil || len(cases) != 1 || cases[0].ResolvedAt == nil || cases[0].ActorID == nil || *cases[0].ActorID != "system:fix-reconciliation" {
		t.Fatalf("system history: %+v %v", cases, err)
	}
	cases, err = store.ListReconciliation(ctx, &unknownID, 100)
	if err != nil || len(cases) != 1 || cases[0].ResolvedAt != nil {
		t.Fatalf("unconfirmed case closed: %+v %v", cases, err)
	}
}

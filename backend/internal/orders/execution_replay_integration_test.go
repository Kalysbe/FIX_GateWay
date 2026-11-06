//go:build integration

package orders

import (
	"context"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"os"
	"testing"
	"time"
)

func replayFixture(t *testing.T) (context.Context, *Store, *pgxpool.Pool, func() domain.Order) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable DATABASE_URL required")
	}
	if err := migrate.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var broker int64
	if err = pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'Replay test') RETURNING id`, uuid.NewString()).Scan(&broker); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	create := func() domain.Order {
		t.Helper()
		order, err := store.CreateWithOutbox(ctx, CreateInput{BrokerID: broker, Symbol: uuid.NewString(), Board: "REPLAY", Currency: "KGS", Quantity: decimal.NewFromInt(10), Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, TimeInForce: "DAY"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE orders SET status='SENT' WHERE id=$1`, order.ID); err != nil {
			t.Fatal(err)
		}
		return order
	}
	return ctx, store, pool, create
}

func TestExecutionReplayDoesNotChangeOrderTotals(t *testing.T) {
	ctx, store, _, create := replayFixture(t)
	order := create()
	apply := func(execType string, qty, cum int64) (domain.Order, error) {
		return store.ApplyExecutionReport(ctx, *order.FixClOrdID, "", execType, execType, "", decimal.NewFromInt(qty), decimal.NewFromInt(cum), "", "", "", "")
	}
	if _, err := apply("1", 2, 2); err != nil {
		t.Fatal(err)
	}
	before, err := apply("1", 3, 5)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := apply("1", 3, 5); err != nil || !got.FilledQuantity.Equal(before.FilledQuantity) || got.Status != before.Status {
		t.Fatalf("replay changed totals: before=%+v after=%+v err=%v", before, got, err)
	}
	final, err := apply("2", 5, 10)
	if err != nil || !final.ExecutionBecameTerminal || final.Status != domain.OrderFilled {
		t.Fatalf("terminal transition missing: %+v %v", final, err)
	}
	if replayed, err := apply("2", 5, 10); err != nil || replayed.ExecutionBecameTerminal {
		t.Fatalf("terminal replay re-applied: %+v %v", replayed, err)
	}
}

// Only orders QUIK has finished with go, each with all its local rows, and only
// after the wait for the QUIK answer can no longer be reading them.
func TestPurgeFinishedKeepsOpenOrders(t *testing.T) {
	ctx, store, pool, create := replayFixture(t)
	count := func(query string, id any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	exists := func(o domain.Order) bool {
		return count(`SELECT count(*) FROM orders WHERE id=$1`, o.ID) == 1
	}
	set := func(o domain.Order, status, age string) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE orders SET status=$2, quik_order_id='261102-REPLAY-1', updated_at=now()-$3::interval, created_at=now()-$3::interval WHERE id=$1`, o.ID, status, age); err != nil {
			t.Fatal(err)
		}
		// The send was processed; a pending queue row is tested separately.
		if _, err := pool.Exec(ctx, `UPDATE outbox_events SET status='DONE' WHERE aggregate_id=$1`, o.ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	filled, rejected, fresh, active, oldActive, unanswered, queued := create(), create(), create(), create(), create(), create(), create()
	set(filled, "FILLED", "5 minutes")
	set(rejected, "REJECTED", "5 minutes")
	set(fresh, "FILLED", "0 seconds")
	set(active, "ACKNOWLEDGED", "5 minutes")
	set(oldActive, "ACKNOWLEDGED", "3 days")
	set(queued, "CANCELLED", "5 minutes")
	if _, err := pool.Exec(ctx, `UPDATE orders SET created_at=now()-interval '3 days' WHERE id=$1`, unanswered.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET status='PENDING' WHERE aggregate_id=$1`, queued.ID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO execution_inbox_receipts(id,order_id) VALUES($1,$2)`, uuid.New(), filled.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PurgeFinished(ctx, FinishedOrderGrace); err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]domain.Order{"filled": filled, "rejected": rejected, "past day": oldActive} {
		if exists(o) {
			t.Errorf("%s order kept", name)
		}
		if n := count(`SELECT count(*) FROM order_events WHERE order_id=$1`, o.ID); n != 0 {
			t.Errorf("%s order left %d events", name, n)
		}
		if n := count(`SELECT count(*) FROM outbox_events WHERE aggregate_id=$1`, o.ID.String()); n != 0 {
			t.Errorf("%s order left %d queue rows", name, n)
		}
		if n := count(`SELECT count(*) FROM execution_inbox_receipts WHERE order_id=$1`, o.ID); n != 0 {
			t.Errorf("%s order left %d receipts", name, n)
		}
	}
	for name, o := range map[string]domain.Order{"inside the grace": fresh, "active today": active, "unanswered": unanswered, "queued": queued} {
		if !exists(o) {
			t.Errorf("%s order purged", name)
		}
	}
	if _, err := store.PurgeFinished(ctx, time.Duration(0)); err != nil {
		t.Fatal(err)
	}
	if exists(fresh) {
		t.Error("finished order kept after the grace")
	}
}

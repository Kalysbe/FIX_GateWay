//go:build integration

package orders

import (
	"context"
	"os"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func productionOrdersFixture(t *testing.T) (context.Context, *Store, *pgxpool.Pool, func(string) domain.Order) {
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
	var broker, instrument int64
	if err = pool.QueryRow(ctx, `INSERT INTO brokers(code,name)VALUES($1,'Production integrity test')RETURNING id`, uuid.NewString()).Scan(&broker); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board)VALUES($1,$1,'AUDIT')RETURNING id`, uuid.NewString()).Scan(&instrument); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	create := func(adapter string) domain.Order {
		o, _, err := store.CreateWithOutbox(ctx, CreateInput{BrokerID: broker, InstrumentID: instrument, ClientOrderID: uuid.NewString(), Quantity: decimal.NewFromInt(10), Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, TimeInForce: "GTC"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `UPDATE orders SET status='SENT',adapter_id=$2 WHERE id=$1`, o.ID, adapter); err != nil {
			t.Fatal(err)
		}
		return o
	}
	return ctx, store, pool, create
}

func TestExecutionIDsScopedToLogicalOrderAndDate(t *testing.T) {
	ctx, store, _, create := productionOrdersFixture(t)
	first, second := create("adapter-1"), create("adapter-2")
	id := uuid.NewString()
	apply := func(o domain.Order, date, adapter string, cum int64) (domain.Order, *domain.Execution, error) {
		return store.ApplyExecutionReport(WithExecutionDate(ctx, date), *o.FixClOrdID, "", id, "1", "1", "", decimal.NewFromInt(2), decimal.NewFromInt(100), decimal.NewFromInt(cum), decimal.NewFromInt(10-cum), adapter, "", "", "")
	}
	for _, o := range []domain.Order{first, second} {
		if _, e, err := apply(o, "20261012", "adapter-1", 2); err != nil || e == nil {
			t.Fatalf("distinct order execution lost: %v %v", e, err)
		}
	}
	if o, e, err := apply(first, "20261012", "adapter-2", 2); err != nil || e != nil || !o.FilledQuantity.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("cross-session replay double counted: %v %v", e, err)
	}
	if o, e, err := apply(first, "20261013", "adapter-2", 4); err != nil || e == nil || !o.FilledQuantity.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("next-day execution lost: %v %v", e, err)
	}
	if o, e, err := apply(first, "20261012", "adapter-1", 2); err != nil || e != nil || !o.FilledQuantity.Equal(decimal.NewFromInt(4)) {
		t.Fatalf("old-date replay changed totals: %v %v", e, err)
	}
}

func TestInboxReceiptsAtomicAndCoverNonFillReports(t *testing.T) {
	ctx, store, pool, create := productionOrdersFixture(t)
	o := create("adapter-1")
	receipt := uuid.New()
	apply := func(clID string) (domain.Order, *domain.Execution, error) {
		return store.ApplyExecutionReport(WithExecutionReceipt(ctx, receipt), clID, "", "ACK", "0", "0", "", decimal.Zero, decimal.Zero, decimal.Zero, decimal.NewFromInt(10), "adapter-1", "", "", "")
	}
	if _, _, err := apply("missing-" + uuid.NewString()); err == nil {
		t.Fatal("unknown order accepted")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM execution_inbox_receipts WHERE id=$1`, receipt).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed apply committed receipt")
	}
	if got, _, err := apply(*o.FixClOrdID); err != nil || got.Status != domain.OrderAcknowledged {
		t.Fatalf("retry failed: %+v %v", got, err)
	}
	if _, _, err := apply(*o.FixClOrdID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM order_events WHERE order_id=$1 AND event_type='ORDER_EXECUTION_REPORT'`, o.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ACK reapplied after commit/removal crash: %d %v", count, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='CANCEL_PENDING' WHERE id=$1`, o.ID); err != nil {
		t.Fatal(err)
	}
	cancelCtx := WithExecutionReceipt(ctx, uuid.New())
	for n := 0; n < 2; n++ {
		if _, err := store.ApplyCancelReject(cancelCtx, *o.FixClOrdID+"-C", *o.FixClOrdID, "", "adapter-1", "rejected", ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM order_events WHERE order_id=$1 AND event_type='ORDER_CANCEL_REJECTED'`, o.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("cancel reject duplicated: %d %v", count, err)
	}
}

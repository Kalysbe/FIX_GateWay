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
)

func TestExecutionReplayDoesNotChangeOrderTotals(t *testing.T) {
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
	defer pool.Close()
	var broker, instrument int64
	if err = pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'Replay test') RETURNING id`, uuid.NewString()).Scan(&broker); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board) VALUES($1,$1,'REPLAY') RETURNING id`, uuid.NewString()).Scan(&instrument); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	order, _, err := store.CreateWithOutbox(ctx, CreateInput{BrokerID: broker, InstrumentID: instrument, ClientOrderID: uuid.NewString(), Quantity: decimal.NewFromInt(10), Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, TimeInForce: "DAY"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE orders SET status='SENT' WHERE id=$1`, order.ID); err != nil {
		t.Fatal(err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	apply := func(execID string, qty, px, cum int64) (domain.Order, *domain.Execution, error) {
		return store.ApplyExecutionReport(ctx, *order.FixClOrdID, "", execID, "1", "1", "", decimal.NewFromInt(qty), decimal.NewFromInt(px), decimal.NewFromInt(cum), decimal.NewFromInt(10-cum), "", "", "", "")
	}
	if _, _, err = apply(first, 2, 100, 2); err != nil {
		t.Fatal(err)
	}
	before, _, err := apply(second, 3, 200, 5)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{second, first, second} {
		qty, px, cum := int64(3), int64(200), int64(5)
		if id == first {
			qty, px, cum = 2, 100, 2
		}
		after, execution, err := apply(id, qty, px, cum)
		if err != nil || execution != nil || !after.FilledQuantity.Equal(before.FilledQuantity) || !after.AvgPrice.Equal(before.AvgPrice) || after.Status != before.Status {
			t.Fatalf("replay changed totals: before=%+v after=%+v execution=%+v err=%v", before, after, execution, err)
		}
	}
	if _, _, err = apply(first, 2, 999, 2); err == nil {
		t.Fatal("conflicting ExecID accepted")
	}
	if _, _, err = apply("", 2, 100, 2); err == nil {
		t.Fatal("fill without ExecID accepted")
	}
	var count int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM executions WHERE order_id=$1`, order.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("execution count %d %v", count, err)
	}
	terminalID := uuid.NewString()
	finish := func() (domain.Order, *domain.Execution, error) {
		return store.ApplyExecutionReport(ctx, *order.FixClOrdID, "", terminalID, "2", "2", "", decimal.NewFromInt(5), decimal.NewFromInt(150), decimal.NewFromInt(10), decimal.Zero, "", "", "", "")
	}
	final, _, err := finish()
	if err != nil || !final.ExecutionBecameTerminal {
		t.Fatalf("terminal transition missing: %+v %v", final, err)
	}
	replayed, execution, err := finish()
	if err != nil || replayed.ExecutionBecameTerminal || execution != nil || !replayed.AvgPrice.Equal(final.AvgPrice) {
		t.Fatalf("terminal replay re-applied: %+v %v", replayed, err)
	}
}

func TestAveragePriceUsesRecordedFillsWhenCumQtyJumps(t *testing.T) {
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
	defer pool.Close()
	var broker, instrument int64
	if err = pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'Avg test') RETURNING id`, uuid.NewString()).Scan(&broker); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board) VALUES($1,$1,'AVG') RETURNING id`, uuid.NewString()).Scan(&instrument); err != nil {
		t.Fatal(err)
	}
	store := NewStore(pool)
	order, _, err := store.CreateWithOutbox(ctx, CreateInput{BrokerID: broker, InstrumentID: instrument, ClientOrderID: uuid.NewString(), Quantity: decimal.NewFromInt(10), Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, TimeInForce: "DAY"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE orders SET status='SENT' WHERE id=$1`, order.ID); err != nil {
		t.Fatal(err)
	}
	apply := func(qty, px, cum int64) domain.Order {
		t.Helper()
		o, _, err := store.ApplyExecutionReport(ctx, *order.FixClOrdID, "", uuid.NewString(), "1", "1", "", decimal.NewFromInt(qty), decimal.NewFromInt(px), decimal.NewFromInt(cum), decimal.NewFromInt(10-cum), "", "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	apply(2, 100, 2)
	// A report with CumQty=4 (2 lots) was not applied; this one has CumQty=7.
	o := apply(3, 200, 7)
	if !o.FilledQuantity.Equal(decimal.NewFromInt(7)) {
		t.Fatalf("filled %s", o.FilledQuantity)
	}
	if !o.AvgPrice.Equal(decimal.NewFromInt(160)) { // (2*100+3*200)/5
		t.Fatalf("avg %s, want 160 over recorded fills", o.AvgPrice)
	}
	// The next fill must not weight the old average by the jumped CumQty.
	o = apply(1, 300, 8)
	want := decimal.NewFromInt(1100).Div(decimal.NewFromInt(6)) // (200+600+300)/6
	if !o.AvgPrice.Sub(want).Abs().LessThan(decimal.New(1, -6)) {
		t.Fatalf("avg %s, want %s", o.AvgPrice, want)
	}
}

//go:build integration

package orders

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func TestTradingLimitsIntegration(t *testing.T) {
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
	store := NewStore(pool)
	var brokerID, instrumentID int64
	err = pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'Risk test') RETURNING id`, uuid.NewString()).Scan(&brokerID)
	if err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board) VALUES($1,$1,'TEST') RETURNING id`, uuid.NewString()).Scan(&instrumentID)
	if err != nil {
		t.Fatal(err)
	}
	maxQuantity := decimal.NewFromInt(10)
	maxOpen := 3
	policy := TradingLimit{BrokerID: brokerID, MaxQuantity: &maxQuantity, MaxOpenOrders: &maxOpen}
	if err = store.UpdateTradingLimit(ctx, policy, "operator", "initial limits"); err != nil {
		t.Fatal(err)
	}
	if err = store.UpdateTradingLimit(ctx, policy, "operator", "stale overwrite"); !errors.Is(err, ErrRiskPolicyConflict) {
		t.Fatalf("revision protection: %v", err)
	}
	input := func(key string, qty int64) CreateInput {
		return CreateInput{BrokerID: brokerID, InstrumentID: instrumentID, ClientOrderID: key, Quantity: decimal.NewFromInt(qty), Side: domain.SideBuy, OrderType: domain.OrderTypeMarket, TimeInForce: "DAY", Board: "TEST", FixAccount: "ACCOUNT"}
	}
	assertRisk := func(err error, code string) {
		t.Helper()
		var risk *RiskRejected
		if !errors.As(err, &risk) || risk.Code != code {
			t.Fatalf("want %s got %v", code, err)
		}
	}
	_, _, err = store.CreateWithOutbox(ctx, input("too-big", 11))
	assertRisk(err, "MAX_ORDER_QUANTITY")
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, err := store.CreateWithOutbox(ctx, input(fmt.Sprintf("parallel-%d", i), 10))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			assertRisk(err, "MAX_OPEN_ORDERS")
		}
	}
	if accepted != 3 {
		t.Fatalf("concurrent acceptance bypassed limit: %d", accepted)
	}
	var orders, queued int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE broker_id=$1`, brokerID).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events e JOIN orders o ON o.id::text=e.aggregate_id WHERE o.broker_id=$1`, brokerID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if orders != 3 || queued != 3 {
		t.Fatalf("rejected requests persisted: orders=%d outbox=%d", orders, queued)
	}
	var firstID uuid.UUID
	var firstKey string
	err = pool.QueryRow(ctx, `SELECT id,client_order_id FROM orders WHERE broker_id=$1 ORDER BY id LIMIT 1`, brokerID).Scan(&firstID, &firstKey)
	if err != nil {
		t.Fatal(err)
	}
	policy.Revision = 1
	policy.Paused = true
	if err = store.UpdateTradingLimit(ctx, policy, "operator", "pause broker"); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.CreateWithOutbox(ctx, input("paused-new", 1))
	assertRisk(err, "TRADING_PAUSED")
	replay, idempotent, err := store.CreateWithOutbox(ctx, input(firstKey, 10))
	if err != nil || !idempotent || replay.ID != firstID {
		t.Fatalf("pause blocked replay: %+v %v %v", replay, idempotent, err)
	}
	// Cancellation still works while submission is paused.
	if _, err = pool.Exec(ctx, `UPDATE orders SET status='ACKNOWLEDGED' WHERE id=$1`, firstID); err != nil {
		t.Fatal(err)
	}
	if _, err = store.EnqueueCancel(ctx, firstID); err != nil {
		t.Fatalf("pause blocked cancel: %v", err)
	}
	policy.Revision = 2
	policy.Paused = false
	if err = store.UpdateTradingLimit(ctx, policy, "operator", "resume broker"); err != nil {
		t.Fatal(err)
	}
	// Failed/uncertain and cancel-pending orders continue consuming capacity.
	if _, err = pool.Exec(ctx, `UPDATE orders SET status='FAILED' WHERE broker_id=$1 AND id<>$2`, brokerID, firstID); err != nil {
		t.Fatal(err)
	}
	_, _, err = store.CreateWithOutbox(ctx, input("still-full", 1))
	assertRisk(err, "MAX_OPEN_ORDERS")
	if _, err = pool.Exec(ctx, `UPDATE orders SET status='CANCELLED' WHERE id=$1`, firstID); err != nil {
		t.Fatal(err)
	}
	if _, _, err = store.CreateWithOutbox(ctx, input("capacity-released", 1)); err != nil {
		t.Fatalf("terminal order did not release capacity: %v", err)
	}
	// History failure must leave policy and its revision unchanged.
	if _, err = pool.Exec(ctx, `ALTER TABLE trading_limit_history ADD CONSTRAINT risk_test_actor CHECK(actor_id <> 'reject-test')`); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, `ALTER TABLE trading_limit_history DROP CONSTRAINT risk_test_actor`)
	policy.Revision = 3
	policy.Paused = true
	if err = store.UpdateTradingLimit(ctx, policy, "reject-test", "audit failure"); err == nil {
		t.Fatal("audit failure accepted")
	}
	var paused bool
	var revision int64
	if err = pool.QueryRow(ctx, `SELECT paused,revision FROM trading_limits WHERE broker_id=$1`, brokerID).Scan(&paused, &revision); err != nil || paused || revision != 3 {
		t.Fatalf("audit rollback: paused=%v revision=%d err=%v", paused, revision, err)
	}
	history, err := store.ListTradingLimitHistory(ctx, &brokerID, 0, 2)
	if err != nil || len(history) != 2 || history[0].After.Revision != 3 || history[0].Before == nil || history[0].Before.Revision != 2 || history[0].ActorID != "operator" {
		t.Fatalf("history: %+v %v", history, err)
	}
	older, err := store.ListTradingLimitHistory(ctx, &brokerID, history[1].ID, 2)
	if err != nil || len(older) != 1 || older[0].Before != nil || older[0].After.Revision != 1 {
		t.Fatalf("cursor history: %+v %v", older, err)
	}
	if older[0].BrokerID != brokerID || older[0].Reason != "initial limits" {
		t.Fatalf("history lost actor/reason or filter: %+v", older[0])
	}
}

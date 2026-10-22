//go:build integration

package orders

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
)

func TestListAdminTradesCompletesPairsBeyond500(t *testing.T) {
	ctx, store, pool, create := productionOrdersFixture(t)
	buy, sell, noise := create("adapter-1"), create("adapter-1"), create("adapter-1")
	ids := []uuid.UUID{buy.ID, sell.ID, noise.ID}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM executions WHERE order_id=ANY($1::uuid[])`, ids) })
	var seller int64
	if err := pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'Counterparty') RETURNING id`, uuid.NewString()).Scan(&seller); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET side='SELL',broker_id=$2 WHERE id=$1`, sell.ID, seller); err != nil {
		t.Fatal(err)
	}
	match := "LIST_GBA-" + uuid.NewString()
	for _, leg := range []struct {
		order           domain.Order
		side, timestamp string
	}{
		{buy, "B", "2099-12-31 23:00:00+00"},
		{sell, "S", "2100-01-01 00:00:00+00"},
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO executions(order_id,exec_id,last_qty,last_px,cum_qty,leaves_qty,execution_date,created_at)
VALUES($1,$2,1,99,1,0,'2026-10-06',$3::timestamptz)`, leg.order.ID, match+"-"+leg.side+"-1-1-N", leg.timestamp); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO executions(order_id,exec_id,last_qty,last_px,cum_qty,leaves_qty,execution_date,created_at)
SELECT $1,'unrelated-' || n,1,99,1,0,'2026-10-06','2100-01-01 00:01:00+00'::timestamptz + n*interval '1 millisecond'
FROM generate_series(1,499) n`, noise.ID); err != nil {
		t.Fatal(err)
	}
	window, err := store.ListTrades(ctx, nil, 500)
	if err != nil || len(window) != 500 {
		t.Fatalf("seed window: %d %v", len(window), err)
	}
	for _, trade := range window {
		if trade.OrderID == buy.ID {
			t.Fatal("fixture buy must be beyond the old 500-execution cutoff")
		}
	}
	trades, err := store.ListAdminTrades(ctx, 500)
	if err != nil || len(trades) != 501 {
		t.Fatalf("completed window: %d %v", len(trades), err)
	}
	foundBuy, foundSell := false, false
	var buyID, sellID uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, trade := range trades {
		if seen[trade.ID] {
			t.Fatal("duplicate execution", trade.ID)
		}
		seen[trade.ID] = true
		foundBuy = foundBuy || trade.OrderID == buy.ID
		foundSell = foundSell || trade.OrderID == sell.ID
		if trade.OrderID == buy.ID {
			buyID = trade.ID
		}
		if trade.OrderID == sell.ID {
			sellID = trade.ID
		}
		if trade.ExecutionDate != "2026-10-06" {
			t.Fatalf("execution date was replaced by receipt date: %s", trade.ExecutionDate)
		}
	}
	if !foundBuy || !foundSell {
		t.Fatal("admin response lost a side of the match")
	}
	for _, id := range []uuid.UUID{buyID, sellID} {
		detail, err := store.GetAdminTrade(ctx, id)
		if err != nil || len(detail) != 2 {
			t.Fatalf("historical detail from either leg lost its pair: %d %v", len(detail), err)
		}
		for _, trade := range detail {
			if trade.ID != buyID && trade.ID != sellID {
				t.Fatal("detail includes an unrelated execution", trade.ID)
			}
		}
	}
	if _, err := store.GetAdminTrade(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing detail: %v", err)
	}
	brokerTrades, err := store.ListTrades(ctx, &buy.BrokerID, 500)
	if err != nil {
		t.Fatal(err)
	}
	for _, trade := range brokerTrades {
		if trade.BrokerID != buy.BrokerID {
			t.Fatal("broker list leaked a counterparty", trade.BrokerID)
		}
	}
}

func TestListAdminTradesScopesCounterparts(t *testing.T) {
	ctx, store, pool, create := productionOrdersFixture(t)
	var ids []uuid.UUID
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM executions WHERE order_id=ANY($1::uuid[])`, ids) })
	match := "LIST_GBA-" + uuid.NewString()
	for n, tc := range []struct {
		name, side, date, board string
		otherSymbol             bool
	}{
		{"sell", "SELL", "2026-10-06", "AUDIT", false},
		{"buy", "BUY", "2026-10-06", "AUDIT", false},
		{"other date", "BUY", "2026-10-07", "AUDIT", false},
		{"other board", "BUY", "2026-10-06", "OTHER", false},
		{"other instrument", "BUY", "2026-10-06", "AUDIT", true},
		{"wrong side", "SELL", "2026-10-06", "AUDIT", false},
	} {
		o := create("adapter-1")
		ids = append(ids, o.ID)
		instrument := o.InstrumentID
		if tc.otherSymbol {
			if err := pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board) VALUES($1,$1,'AUDIT') RETURNING id`, uuid.NewString()).Scan(&instrument); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `UPDATE orders SET side=$2,board=$3,instrument_id=$4 WHERE id=$1`, o.ID, tc.side, tc.board, instrument); err != nil {
			t.Fatal(err)
		}
		marker := "B"
		if n == 0 {
			marker = "S"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO executions(order_id,exec_id,last_qty,last_px,cum_qty,leaves_qty,execution_date,created_at)
VALUES($1,$2,1,99,1,0,$3::date,$4::timestamptz)`, o.ID, match+"-"+marker+"-1-1-N", tc.date, fmt.Sprintf("2100-01-01 00:00:%02d+00", 59-n)); err != nil {
			t.Fatal(tc.name, err)
		}
	}
	trades, err := store.ListAdminTrades(ctx, 1)
	if err != nil || len(trades) != 2 {
		t.Fatalf("counterpart query mixed scopes: %d %v", len(trades), err)
	}
	for _, trade := range trades {
		if trade.OrderID != ids[0] && trade.OrderID != ids[1] {
			t.Fatal("unrelated execution joined the pair", trade.OrderID)
		}
	}
	// Both legs already in the seed window must not be duplicated.
	trades, err = store.ListAdminTrades(ctx, 2)
	if err != nil || len(trades) != 2 {
		t.Fatalf("existing pair duplicated: %d %v", len(trades), err)
	}
}

func TestListAdminTradesLegacyExecutionDate(t *testing.T) {
	ctx, store, pool, create := productionOrdersFixture(t)
	buy, sell := create("adapter-1"), create("adapter-1")
	ids := []uuid.UUID{buy.ID, sell.ID}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM executions WHERE order_id=ANY($1::uuid[])`, ids) })
	if _, err := pool.Exec(ctx, `UPDATE orders SET side='SELL' WHERE id=$1`, sell.ID); err != nil {
		t.Fatal(err)
	}
	match := "LIST_GBA-" + uuid.NewString()
	for n, o := range []domain.Order{buy, sell} {
		marker := "B"
		if n == 1 {
			marker = "S"
		}
		if _, err := pool.Exec(ctx, `INSERT INTO executions(order_id,exec_id,last_qty,last_px,cum_qty,leaves_qty,created_at)
VALUES($1,$2,1,99,1,0,$3::timestamptz)`, o.ID, match+"-"+marker+"-1-1-N", fmt.Sprintf("2100-01-01 00:00:%02d+00", n)); err != nil {
			t.Fatal(err)
		}
	}
	trades, err := store.ListAdminTrades(ctx, 1)
	if err != nil || len(trades) != 2 {
		t.Fatalf("legacy pair lost: %d %v", len(trades), err)
	}
	for _, trade := range trades {
		if trade.ExecutionDate != "2100-01-01" {
			t.Fatal("missing legacy UTC day", trade.ExecutionDate)
		}
	}
}

//go:build integration

package orders

import (
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestExecutionMatchMetadataPersistsWithoutCreatingCounterpartyExecution(t *testing.T) {
	ctx, store, pool, create := productionOrdersFixture(t)
	order := create("adapter-1")
	ctx = WithExecutionDate(ctx, "20261006")
	execID := uuid.NewString()
	raw := "8=FIX.4.2|17=" + execID + "|880=261006-LIST_GBA-11094|375=2040|337=contra-trader|"
	apply := func(wire string) error {
		_, _, err := store.ApplyExecutionReport(ctx, *order.FixClOrdID, "", execID, "1", "1", "", decimal.NewFromInt(2), decimal.NewFromInt(99), decimal.NewFromInt(2), decimal.NewFromInt(8), "adapter-1", "", "", wire)
		return err
	}
	if err := apply(raw); err != nil {
		t.Fatal(err)
	}
	if err := apply(raw); err != nil {
		t.Fatal("idempotent replay", err)
	}
	trades, err := store.ListTradesByOrder(ctx, order.ID)
	if err != nil || len(trades) != 1 {
		t.Fatalf("counterparty must not create a second execution: %d %v", len(trades), err)
	}
	trade := trades[0]
	if trade.TradeMatchID != "261006-LIST_GBA-11094" || trade.ContraBroker != "2040" || trade.ContraTrader != "contra-trader" {
		t.Fatalf("FIX metadata lost: %+v", trade)
	}
	if err := apply("17=" + execID + "|880=conflicting-match|"); err == nil {
		t.Fatal("replay changed the exchange match id")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM executions WHERE order_id=$1`, order.ID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("execution count: %d %v", count, err)
	}
}

func TestListAdminTradesUsesTradeMatchIDRatherThanExecIDShape(t *testing.T) {
	ctx, store, pool, create := productionOrdersFixture(t)
	var ids []uuid.UUID
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM executions WHERE order_id=ANY($1::uuid[])`, ids) })
	match := uuid.NewString()
	var selected uuid.UUID
	for n, tc := range []struct{ side, execID, matchID, day string }{
		{"SELL", "different-opaque-sell", match, "2026-10-06"},
		{"BUY", "unrelated-opaque-buy", match, "2026-10-06"},
		{"BUY", "different-opaque-buy", "other-match", "2026-10-06"},
		{"BUY", "same-match-other-day", match, "2026-10-07"},
	} {
		o := create("adapter-1")
		ids = append(ids, o.ID)
		if _, err := pool.Exec(ctx, `UPDATE orders SET side=$2 WHERE id=$1`, o.ID, tc.side); err != nil {
			t.Fatal(err)
		}
		var id uuid.UUID
		if err := pool.QueryRow(ctx, `INSERT INTO executions(order_id,exec_id,last_qty,last_px,cum_qty,leaves_qty,execution_date,created_at,trade_match_id,contra_broker)
VALUES($1,$2,201,99,201,0,$3::date,$4::timestamptz,$5,'2040') RETURNING id`, o.ID, tc.execID, tc.day, fmt.Sprintf("2100-01-01 00:00:%02d+00", 59-n), tc.matchID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			selected = id
		}
	}
	for _, detail := range []bool{false, true} {
		trades, err := store.ListAdminTrades(ctx, 1)
		if detail {
			trades, err = store.GetAdminTrade(ctx, selected)
		}
		if err != nil || len(trades) != 2 {
			t.Fatalf("match lookup: %d %v", len(trades), err)
		}
		for _, trade := range trades {
			if trade.OrderID != ids[0] && trade.OrderID != ids[1] {
				t.Fatal("lookup used a different match or trading day", trade)
			}
		}
	}
}

func TestExecutionMatchMigrationRestoresOnlyExactUnambiguousReport(t *testing.T) {
	ctx, _, pool, _ := productionOrdersFixture(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `CREATE TEMP TABLE executions(id uuid,order_id uuid,exec_id text,created_at timestamptz,execution_date date,last_qty numeric) ON COMMIT DROP;
CREATE TEMP TABLE order_events(order_id uuid,event_type text,payload jsonb,created_at timestamptz) ON COMMIT DROP;`)
	if err != nil {
		t.Fatal(err)
	}
	for n, key := range []string{"exact", "wrong-time", "ambiguous"} {
		id, order := uuid.New(), uuid.New()
		_, err = tx.Exec(ctx, `INSERT INTO executions VALUES($1,$2,$3,'2026-10-06 12:20:33+00',NULL,201)`, id, order, key)
		if err != nil {
			t.Fatal(err)
		}
		stamp := "2026-10-06 12:20:33+00"
		if n == 1 {
			stamp = "2026-10-06 12:20:34+00"
		}
		_, err = tx.Exec(ctx, `INSERT INTO order_events VALUES($1,'ORDER_EXECUTION_REPORT',jsonb_build_object('raw_fix',$2::text),$3::timestamptz)`, order, "8=FIX.4.2|17="+key+"|880=261006-LIST_GBA-11094|375=2040|", stamp)
		if err != nil {
			t.Fatal(err)
		}
		if n == 2 {
			_, err = tx.Exec(ctx, `INSERT INTO order_events VALUES($1,'ORDER_EXECUTION_REPORT',jsonb_build_object('raw_fix',$2::text),$3::timestamptz)`, order, "8=FIX.4.2|17="+key+"|880=other-match|375=2040|", stamp)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	migration, err := os.ReadFile("../../migrations/000029_execution_match_metadata.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"exact", "wrong-time", "ambiguous"} {
		var match, broker, qty string
		if err := tx.QueryRow(ctx, `SELECT trade_match_id,contra_broker,last_qty::text FROM executions WHERE exec_id=$1`, key).Scan(&match, &broker, &qty); err != nil {
			t.Fatal(err)
		}
		if key == "exact" {
			if match != "261006-LIST_GBA-11094" || broker != "2040" {
				t.Fatal("historical match not restored", match, broker)
			}
		} else if match != "" || broker != "" {
			t.Fatal("unrelated or ambiguous report backfilled", key)
		}
		if qty != "201" {
			t.Fatal("migration changed executed quantity", qty)
		}
	}
}

//go:build integration

package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/tradeledger"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A failure after the order transaction commits (LinkExecution, crash) is
// retried with the same inbox receipt. The order is not applied twice, and
// the retry still links the trade side to the local execution.
func TestRetryAfterCommittedFillRestoresTradeSideLink(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if isolated := os.Getenv("TRADE_LEDGER_API_TEST_DATABASE_URL"); isolated != "" {
		dsn = isolated
	}
	if dsn == "" {
		t.Skip("disposable DB required")
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
	s := &Server{tradeLedger: tradeledger.NewStore(pool), orderStore: orders.NewStore(pool), hub: realtime.NewHub(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	var brokerID, instrument int64
	if err := pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'link retry') RETURNING id`, uuid.NewString()).Scan(&brokerID); err != nil {
		t.Fatal(err)
	}
	symbol := "LNK" + uuid.NewString()[:8]
	if err := pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board) VALUES($1,$1,'LINK_TEST') RETURNING id`, symbol).Scan(&instrument); err != nil {
		t.Fatal(err)
	}
	orderID, clOrdID := uuid.New(), "link-"+uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO orders(id,broker_id,client_order_id,instrument_id,side,order_type,time_in_force,quantity,price,status,fix_firm,fix_cl_ord_id)
VALUES($1,$2,$3,$4,'BUY','LIMIT','DAY',5,10,'ACKNOWLEDGED','2040',$5)`, orderID, brokerID, uuid.NewString(), instrument, clOrdID); err != nil {
		t.Fatal(err)
	}
	match := fmt.Sprintf("261023-LINK_TEST-%d", time.Now().UnixNano()%1_000_000_000)
	report := domain.ExecutionReport{AdapterID: "adapter-1", ClOrdID: clOrdID, OrderID: "261023-LINK_TEST-77", ExecID: "link-exec-1", ExecType: "2", OrdStatus: "2",
		TradeDate: "20261023", LastQty: "5", LastPx: "10", CumQty: "5", LeavesQty: "0",
		RawFIX: "8=FIX.4.2|35=8|37=261023-LINK_TEST-77|11=" + clOrdID + "|17=link-exec-1|880=" + match + "|20=0|150=2|39=2|75=20261023|100=LINK_TEST|48=" + symbol + "|54=1|32=5|31=10|14=5|151=0|5001=" + match[len("261023-LINK_TEST-"):] + "|5002=77|109=buyer|5018=2040|"}
	receipt := orders.WithExecutionReceipt(ctx, uuid.New())
	if err := s.OnExecution(receipt, report); err != nil {
		t.Fatal(err)
	}
	linked := func() (bool, uuid.UUID) {
		var execution *uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT s.execution_id FROM trade_sides s JOIN trades t ON t.id=s.trade_id WHERE t.trade_match_id=$1 AND s.side='BUY'`, match).Scan(&execution); err != nil {
			t.Fatal(err)
		}
		if execution == nil {
			return false, uuid.Nil
		}
		return true, *execution
	}
	ok, first := linked()
	if !ok {
		t.Fatal("trade side not linked to the local execution")
	}
	// Simulate the link being lost after the order transaction committed.
	if _, err := pool.Exec(ctx, `UPDATE trade_sides SET execution_id=NULL WHERE execution_id=$1`, first); err != nil {
		t.Fatal(err)
	}
	if err := s.OnExecution(receipt, report); err != nil {
		t.Fatal(err)
	}
	ok, again := linked()
	if !ok || again != first {
		t.Fatalf("retry did not restore the link: %v %v %v", ok, again, first)
	}
	var executions int
	var filled string
	if err := pool.QueryRow(ctx, `SELECT count(*),max(o.filled_quantity)::text FROM executions e JOIN orders o ON o.id=e.order_id WHERE e.order_id=$1`, orderID).Scan(&executions, &filled); err != nil || executions != 1 {
		t.Fatalf("executions %d filled %s %v", executions, filled, err)
	}
}

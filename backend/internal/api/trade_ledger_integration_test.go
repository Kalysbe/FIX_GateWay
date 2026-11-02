//go:build integration

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/tradeledger"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTradeLedgerIncomingExternalOrderPersistsAndDetailReturnsRealSides(t *testing.T) {
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
	ledger := tradeledger.NewStore(pool)
	s := &Server{tradeLedger: ledger, orderStore: orders.NewStore(pool), hub: realtime.NewHub(), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	num := fmt.Sprint(time.Now().UnixNano() % 1_000_000_000)
	match := "261019-LEDGER_TEST-" + num
	report := domain.ExecutionReport{AdapterID: "ledger-test", ClOrdID: "external-sell", OrderID: "261019-LEDGER_TEST-901", ExecID: "external-test-exec", ExecType: "2", OrdStatus: "2", TradeDate: "20261019", LastQty: "2", LastPx: "99", CumQty: "2", LeavesQty: "0",
		RawFIX: "8=FIX.4.2|35=8|37=261019-LEDGER_TEST-901|11=external-sell|17=external-test-exec|880=" + match + "|20=0|150=2|39=2|75=20261019|100=LEDGER_TEST|48=NO_LOCAL_INSTRUMENT|54=2|32=2|31=99|14=2|151=0|5001=" + num + "|5002=901|109=real-seller|5018=2040|"}
	var ordersBefore int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&ordersBefore)
	if err := s.OnExecution(ctx, report); err != nil {
		t.Fatal("external order blocked incoming report", err)
	}
	if err := s.OnExecution(ctx, report); err != nil {
		t.Fatal("replay failed", err)
	}
	var fromFIX int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trades WHERE trade_match_id=$1`, match).Scan(&fromFIX); err != nil || fromFIX != 0 {
		t.Fatal("FIX report created a trade", fromFIX, err)
	}
	for _, row := range []string{
		`{"TradeNum":"` + num + `","TradeDate":"19-10-2026","ClassCode":"LEDGER_TEST","SecCode":"NO_LOCAL_INSTRUMENT","Operation":"Купля","OrderNum":"902","Qty":"2","Price":"99","ClientCode":"real-buyer","FirmId":"2040"}`,
		`{"TradeNum":"` + num + `","TradeDate":"19-10-2026","ClassCode":"LEDGER_TEST","SecCode":"NO_LOCAL_INSTRUMENT","Operation":"Продажа","OrderNum":"901","Qty":"2","Price":"99","ClientCode":"real-seller","FirmId":"2040"}`,
	} {
		if err := ledger.RecordQUIK(ctx, json.RawMessage(row)); err != nil {
			t.Fatal(err)
		}
	}
	var id string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM trades WHERE trade_match_id=$1`, match).Scan(&id); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/admin/trades/"+id, nil)
	req.SetPathValue("id", id)
	w := httptest.NewRecorder()
	s.getAdminTrade(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Trades []map[string]any `json:"trades"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 2 || result.Trades[0]["client_code"] != "real-buyer" || result.Trades[0]["order_id"] != nil {
		t.Fatal(result)
	}
	var after int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&after)
	if after != ordersBefore {
		t.Fatal("fake local order created", ordersBefore, after)
	}
}

//go:build integration

package tradeledger

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
)

func fixture(t *testing.T) (context.Context, *Store) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if isolated := os.Getenv("TRADE_LEDGER_TEST_DATABASE_URL"); isolated != "" {
		dsn = isolated
	}
	if dsn == "" {
		t.Skip("disposable database required")
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
	// This package runs only against the disposable test DB.
	if _, err := pool.Exec(ctx, `TRUNCATE trade_side_reports,trade_sides,trade_reports,trades,trade_sync_runs`); err != nil {
		t.Fatal(err)
	}
	return ctx, NewStore(pool)
}

func TestSnapshotIdentityCoverageCannotRegressAtSameCount(t *testing.T) {
	ctx, s := fixture(t)
	sell := strings.Replace(strings.Replace(sampleQUIK, `"Купля"`, `"Продажа"`, 1), `"22450"`, `"19358"`, 1)
	pair := sampleQUIK + "," + sell
	first := "[" + pair + "," + strings.ReplaceAll(pair, "11094", "11095") + "]"
	var response atomic.Value
	response.Store(first)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/trades" {
			t.Error("unexpected trading operation", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(response.Load().(string)))
	}))
	defer server.Close()
	if _, err := s.Sync(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"[]", "[" + pair + "]", "[" + pair + "," + strings.ReplaceAll(pair, "11094", "11096") + "]", "[" + sampleQUIK} {
		response.Store(bad)
		if _, err := s.Sync(ctx, server.URL); err == nil {
			t.Fatal("incomplete source coverage accepted", bad)
		}
		h, err := s.Health(ctx)
		if err != nil || h.LastSuccess == nil || h.LatestError == "" {
			t.Fatal("failure did not preserve health evidence", h, err)
		}
	}
	response.Store("[" + pair + "," + strings.ReplaceAll(pair, "11094", "11095") + "," + strings.ReplaceAll(pair, "11094", "11096") + "]")
	if _, err := s.Sync(ctx, server.URL); err != nil {
		t.Fatal("complete retry did not recover", err)
	}
	h, err := s.Health(ctx)
	if err != nil || h.LatestError != "" || h.Incomplete != 0 {
		t.Fatal(h, err)
	}
	var sides int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM trade_sides`).Scan(&sides); err != nil || sides != 6 {
		t.Fatal("history deleted or duplicated", sides, err)
	}
}

func TestLedgerRestoresActualBuyerWithoutInventingLocalOrders(t *testing.T) {
	ctx, s := fixture(t)
	if _, err := s.RecordFIX(ctx, domain.ExecutionReport{LastQty: "2"}); err == nil {
		t.Fatal("delivery without original evidence was acknowledged")
	}
	var before int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&before)
	for n := 0; n < 2; n++ {
		if _, err := s.RecordFIX(ctx, domain.ExecutionReport{RawFIX: sampleFIX, AdapterID: "a"}); err != nil {
			t.Fatal(err)
		}
		if err := s.RecordQUIK(ctx, json.RawMessage(sampleQUIK)); err != nil {
			t.Fatal(err)
		}
	}
	var trades, sides, reports, orders int
	_ = s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM trades),(SELECT count(*) FROM trade_sides),(SELECT count(*) FROM trade_reports),(SELECT count(*) FROM orders)`).Scan(&trades, &sides, &reports, &orders)
	if trades != 1 || sides != 2 || reports != 2 || orders != before {
		t.Fatal(trades, sides, reports, orders, before)
	}
	var client, number string
	var local *string
	if err := s.Pool.QueryRow(ctx, `SELECT client_code,quik_order_num,order_id::text FROM trade_sides WHERE side='BUY'`).Scan(&client, &number, &local); err != nil {
		t.Fatal(err)
	}
	if client != "Kalys" || number != "22450" || local != nil {
		t.Fatal(client, number, local)
	}
	conflict := strings.Replace(sampleQUIK, `"99.00000000"`, `"100.00000000"`, 1)
	if err := s.RecordQUIK(ctx, json.RawMessage(conflict)); err == nil {
		t.Fatal("conflicting price silently replaced history")
	}
	var errors int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM trade_reports WHERE status='ERROR'`).Scan(&errors)
	if errors != 1 {
		t.Fatal("conflicting source evidence was lost", errors)
	}
}

func TestLedgerRejectsTruncatedSnapshotAndRetainsSingleReportedSide(t *testing.T) {
	ctx, s := fixture(t)
	if _, _, err := s.Import(ctx, strings.NewReader("["+sampleQUIK)); err == nil {
		t.Fatal("truncated history accepted")
	}
	if n, _, err := s.Import(ctx, strings.NewReader("["+sampleQUIK+"]")); err == nil || n != 1 {
		t.Fatal("one-sided source claimed complete", n, err)
	}
	var sides int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM trade_sides`).Scan(&sides)
	if sides != 1 {
		t.Fatal("unobserved counterparty was invented", sides)
	}
}

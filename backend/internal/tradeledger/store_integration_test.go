//go:build integration

package tradeledger

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
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

// fakeQUIK stands in for public."Trades": a JSON array of rows, or an error.
type fakeQUIK struct{ rows atomic.Value }

func (f *fakeQUIK) Trades(context.Context) ([]json.RawMessage, error) {
	var out []json.RawMessage
	if err := json.Unmarshal([]byte(f.rows.Load().(string)), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func TestSnapshotIdentityCoverageCannotRegressAtSameCount(t *testing.T) {
	ctx, s := fixture(t)
	sell := strings.Replace(strings.Replace(sampleQUIK, `"Купля"`, `"Продажа"`, 1), `"22450"`, `"19358"`, 1)
	pair := sampleQUIK + "," + sell
	first := "[" + pair + "," + strings.ReplaceAll(pair, "11094", "11095") + "]"
	src := &fakeQUIK{}
	src.rows.Store(first)
	if _, err := s.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"[]", "[" + pair + "]", "[" + pair + "," + strings.ReplaceAll(pair, "11094", "11096") + "]", "[" + sampleQUIK} {
		src.rows.Store(bad)
		if _, err := s.Sync(ctx, src); err == nil {
			t.Fatal("incomplete source coverage accepted", bad)
		}
		h, err := s.Health(ctx)
		if err != nil || h.LastSuccess == nil || h.LatestError == "" {
			t.Fatal("failure did not preserve health evidence", h, err)
		}
	}
	src.rows.Store("[" + pair + "," + strings.ReplaceAll(pair, "11094", "11095") + "," + strings.ReplaceAll(pair, "11094", "11096") + "]")
	if _, err := s.Sync(ctx, src); err != nil {
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
	if err := s.RecordFIX(ctx, domain.ExecutionReport{LastQty: "2"}); err == nil {
		t.Fatal("delivery without original evidence was acknowledged")
	}
	var before int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&before)
	for n := 0; n < 2; n++ {
		if err := s.RecordFIX(ctx, domain.ExecutionReport{RawFIX: sampleFIX, AdapterID: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	var trades, sides, reports, orders int
	counts := func() {
		_ = s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM trades),(SELECT count(*) FROM trade_sides),(SELECT count(*) FROM trade_reports),(SELECT count(*) FROM orders)`).Scan(&trades, &sides, &reports, &orders)
	}
	counts()
	if trades != 0 || sides != 0 || reports != 1 {
		t.Fatal("FIX report created a trade", trades, sides, reports)
	}
	for n := 0; n < 2; n++ {
		if err := s.RecordQUIK(ctx, json.RawMessage(sampleQUIK)); err != nil {
			t.Fatal(err)
		}
	}
	counts()
	if trades != 1 || sides != 1 || reports != 2 || orders != before {
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

// Trades saved from FIX before the QUIK-only rule stay in the tables, but they
// are not part of the ledger: no list, card, coverage or alarm uses them.
func TestLegacyFIXOnlyTradeIsOutsideLedger(t *testing.T) {
	ctx, s := fixture(t)
	var id string
	if err := s.Pool.QueryRow(ctx, `INSERT INTO trades(trade_match_id,quik_trade_num,trade_date,symbol,board,quantity,price,created_at)
VALUES('260812-LEGACY-115','115','2026-08-12','LEGACY','LEGACY',1,450,now()-interval '1 hour') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO trade_sides(trade_id,side,quik_order_id,quik_order_num,has_fix_report) VALUES($1,'BUY','','1059',true)`, id); err != nil {
		t.Fatal(err)
	}
	h, err := s.Health(ctx)
	if err != nil || h.Incomplete != 0 {
		t.Fatal(h, err)
	}
	list, err := s.List(ctx, 500)
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	var coverage struct {
		Trades int `json:"trades"`
	}
	raw, err := s.Coverage(ctx)
	if err != nil || json.Unmarshal(raw, &coverage) != nil || coverage.Trades != 0 {
		t.Fatal(string(raw), err)
	}
	// A real one-sided QUIK trade still raises the alarm.
	if err := s.RecordQUIK(ctx, json.RawMessage(sampleQUIK)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE trades SET created_at=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if h, err = s.Health(ctx); err != nil || h.Incomplete != 1 {
		t.Fatal(h, err)
	}
}

// The ledger keeps and checks only the served classes: an unserved one cannot
// add rows or fail the reconciliation, and its class loads once chosen.
func TestLedgerLimitedToServedClasses(t *testing.T) {
	ctx, s := fixture(t)
	sell := strings.Replace(strings.Replace(sampleQUIK, `"Купля"`, `"Продажа"`, 1), `"22450"`, `"19358"`, 1)
	p, err := ParseQUIK(json.RawMessage(sampleQUIK))
	if err != nil {
		t.Fatal(err)
	}
	board := p.Board
	var classes []string
	s.SetClasses(func(context.Context) ([]string, error) { return classes, nil })
	src := &fakeQUIK{}
	// One-sided row of an unserved class, next to no served rows at all.
	classes = []string{"LIST_GD"}
	src.rows.Store("[" + sampleQUIK + "]")
	if n, err := s.Sync(ctx, src); err != nil || n != 1 {
		t.Fatal("unserved class failed the reconciliation", n, err)
	}
	var trades int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM trades`).Scan(&trades); err != nil || trades != 0 {
		t.Fatal("unserved class kept", trades, err)
	}
	if h, err := s.Health(ctx); err != nil || h.LatestError != "" || h.LastSuccess == nil {
		t.Fatal(h, err)
	}
	// Chosen: the whole pair loads and is served.
	classes = []string{board}
	src.rows.Store("[" + sampleQUIK + "," + sell + "]")
	if _, err := s.Sync(ctx, src); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx, 500)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	// Taken out of the choice: kept, hidden, and its absence from QUIK is not checked.
	classes = []string{"LIST_GD"}
	if list, err := s.List(ctx, 500); err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
	var id string
	if err := s.Pool.QueryRow(ctx, `SELECT id::text FROM trades`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	card, err := s.Get(ctx, uuid.MustParse(id))
	if err != nil || len(card) != 0 {
		t.Fatal(card, err)
	}
	var coverage struct {
		Trades int `json:"trades"`
	}
	raw, err := s.Coverage(ctx)
	if err != nil || json.Unmarshal(raw, &coverage) != nil || coverage.Trades != 0 {
		t.Fatal(string(raw), err)
	}
	if _, err := s.Pool.Exec(ctx, `DELETE FROM trade_side_reports WHERE side_id IN (SELECT id FROM trade_sides WHERE side='SELL'); DELETE FROM trade_sides WHERE side='SELL'; UPDATE trades SET created_at=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if h, err := s.Health(ctx); err != nil || h.Incomplete != 0 {
		t.Fatal("unserved class raised the alarm", h, err)
	}
	src.rows.Store(`[{"TradeNum":"1","TradeDate":"06-10-2026","ClassCode":"OTHER","SecCode":"X","Operation":"Купля","OrderNum":"1","Qty":"1","Price":"1"}]`)
	if _, err := s.Sync(ctx, src); err != nil {
		t.Fatal("history of an unserved class was checked", err)
	}
	// Settings failure is an error, never unfiltered rows.
	s.SetClasses(func(context.Context) ([]string, error) { return nil, errors.New("settings down") })
	if _, err := s.List(ctx, 500); err == nil {
		t.Fatal("settings failure served unfiltered rows")
	}
	if _, _, err := s.Import(ctx, nil); err == nil {
		t.Fatal("settings failure imported unfiltered rows")
	}
}

func TestLedgerRejectsOneSidedSnapshotAndRetainsSingleReportedSide(t *testing.T) {
	ctx, s := fixture(t)
	if n, _, err := s.Import(ctx, []json.RawMessage{json.RawMessage(sampleQUIK)}); err == nil || n != 1 {
		t.Fatal("one-sided source claimed complete", n, err)
	}
	var sides int
	_ = s.Pool.QueryRow(ctx, `SELECT count(*) FROM trade_sides`).Scan(&sides)
	if sides != 1 {
		t.Fatal("unobserved counterparty was invented", sides)
	}
}

//go:build integration

package quikexport

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQUIKOrdersFirmIsolationAndPaging(t *testing.T) {
	// go test runs packages in parallel and the api package creates the same
	// table, so CI gives this package its own database.
	dsn := os.Getenv("QUIK_ORDERS_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("QUIK_ORDERS_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("disposable QUIK_ORDERS_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var name string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || !strings.HasSuffix(name, "_test") {
		t.Fatal("disposable database name must end in _test", name, err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE public."Orders" (
"OrderNum" numeric(19,0),"ClassCode" text,"TradeDate" timestamp,"SecCode" text,"FirmId" text,
"Operation" text,"State" text,"Qty" numeric,"Balance" numeric,"Price" numeric,
PRIMARY KEY("OrderNum","ClassCode","TradeDate"));
INSERT INTO public."Orders" VALUES
(9007199254740993123,'LIST_GBA','2026-10-22','GBA02280810','2040','Купля','Активна',10,8,105),
(9007199254740993122,'LIST_GBA','2026-10-22','GBA02280810','9999','Продажа','Активна',1,1,120),
(9007199254740993121,'LIST_GBA','2026-10-22','GBA02280810','2040','Продажа','Снята',1,1,125),
(9007199254740993123,'OTHER','2026-10-21','OTHER','9999','Продажа','Снята',1,1,125);`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Orders"`) })
	readOnly, err := db.ConnectReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	var ro string
	if err := readOnly.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&ro); err != nil || ro != "on" {
		t.Fatal("source session is writable", ro, err)
	}
	s := NewStore(readOnly)
	firm := "2040"
	first, err := s.ListOrders(ctx, OrderFilter{Firm: &firm, Limit: 1})
	if err != nil || len(first.Orders) != 1 || !first.HasMore || first.Orders[0].Text("OrderNum") != "9007199254740993123" {
		t.Fatal(first, err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT row_to_json(o) FROM public."Orders" o WHERE "OrderNum"=9007199254740993123 AND "ClassCode"='LIST_GBA'`).Scan(&raw); err != nil || !bytes.Equal(raw, first.Orders[0].Raw) {
		t.Fatal("source row changed", string(raw), string(first.Orders[0].Raw), err)
	}
	second, err := s.ListOrders(ctx, OrderFilter{Firm: &firm, Limit: 1, Offset: first.NextOffset})
	if err != nil || len(second.Orders) != 1 || second.HasMore || second.Orders[0].Text("FirmId") != firm || second.Orders[0].Text("OrderNum") != "9007199254740993121" {
		t.Fatal(second, err)
	}
	other := "9999"
	if _, err := s.GetOrder(ctx, first.Orders[0].VenueID(), &other); !errors.Is(err, ErrOrderNotFound) {
		t.Fatal("foreign firm accessed order", err)
	}
	if got, err := s.GetOrder(ctx, first.Orders[0].VenueID(), &firm); err != nil || got.Text("OrderNum") != first.Orders[0].Text("OrderNum") {
		t.Fatal(got, err)
	}
	if _, err := s.GetOrder(ctx, "quik-20261021-LIST_GBA-9007199254740993123", nil); !errors.Is(err, ErrOrderNotFound) {
		t.Fatal("number matched wrong trading day", err)
	}
	for _, scope := range []string{"", "2040' OR 1=1 --"} {
		page, err := s.ListOrders(ctx, OrderFilter{Firm: &scope, Limit: 1000})
		if err != nil || len(page.Orders) != 0 {
			t.Fatal("unsafe scope", page, err)
		}
	}
	all, err := s.ListOrders(ctx, OrderFilter{})
	if err != nil || len(all.Orders) != 4 {
		t.Fatal(all, err)
	}
}

func TestQUIKParamsVerbatimFiltersAndPaging(t *testing.T) {
	dsn := os.Getenv("QUIK_ORDERS_STORE_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("QUIK_ORDERS_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("disposable QUIK_ORDERS_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var name string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || !strings.HasSuffix(name, "_test") {
		t.Fatal("disposable database name must end in _test", name, err)
	}
	// char(n) columns, as QUIK exports often use, are matched after trimming.
	_, err = pool.Exec(ctx, `CREATE TABLE public."Params" (
"ClassCode" char(12),"SecCode" char(12),"LongName" text,"Last" numeric,"Bid" numeric,"NumTrades" numeric(19,0),"TradeDate" date,"Status" text);
INSERT INTO public."Params" VALUES
('LIST_GBA','GBA02280810','Облигации ГБА',123.4567890123456789,NULL,9007199254740993,'2026-10-26','торгуется'),
('LIST_GBA','KG0105166315','Акции',10.10,10.00,5,'2026-10-26','торгуется'),
('TQBR','SBER','Сбербанк',1,NULL,1,'2026-10-26',NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Params"`) })
	readOnly, err := db.ConnectReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	s := NewStore(readOnly)
	page, err := s.ListParams(ctx, ParamFilter{Limit: 2})
	if err != nil || len(page.Params) != 2 || !page.HasMore || page.NextOffset != 2 {
		t.Fatal(page, err)
	}
	if !bytes.Contains(page.Params[0].Raw, []byte(`"Last":123.4567890123456789`)) || !bytes.Contains(page.Params[0].Raw, []byte(`"Bid":null`)) ||
		!bytes.Contains(page.Params[0].Raw, []byte(`"NumTrades":9007199254740993`)) || !bytes.Contains(page.Params[0].Raw, []byte(`"LongName":"Облигации ГБА"`)) {
		t.Fatalf("row not verbatim: %s", page.Params[0].Raw)
	}
	page, err = s.ListParams(ctx, ParamFilter{ClassCode: "LIST_GBA", SecCode: "KG0105166315"})
	if err != nil || len(page.Params) != 1 || page.HasMore {
		t.Fatal(page, err)
	}
	p, err := s.GetParam(ctx, "TQBR", "SBER")
	if err != nil || !bytes.Contains(p.Raw, []byte(`"Status":null`)) {
		t.Fatal(string(p.Raw), err)
	}
	if _, err := s.GetParam(ctx, "TQBR", "GAZP"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal(err)
	}
	if _, err := s.GetParam(ctx, "TQBR", "SBER' OR '1'='1"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal("injection-like key matched", err)
	}
}

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

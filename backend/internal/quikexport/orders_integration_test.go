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
"ClassCode" char(12),"SecCode" char(12),"last" numeric,"couponvalue" numeric,"TradeDate" timestamp,"status" text);
INSERT INTO public."Params" SELECT c, s, l, 7, d, st FROM (VALUES
('LIST_GBA','GBA02280810',123.45,0,'торгуется'),
('LIST_GBA','KG0105166315',10.10,0,'торгуется'),
('TQBR','SBER',1,0,NULL),
-- Earlier trading days of the same instruments are history, not current trades.
('TQBR','SBER',2,1,'вчера'),
('TQBR','GAZP',3,1,NULL)) v(c, s, l, ago, st),
LATERAL (SELECT (now() AT TIME ZONE 'Asia/Bishkek')::date - ago AS d) x;
CREATE TABLE public."Securities" ("TradeDate" timestamp,"ClassCode" varchar,"SecCode" varchar,"CouponValue" numeric,"FaceUnit" varchar,"SecShortName" varchar,"SecFullName" varchar,"ISINCode" varchar);
INSERT INTO public."Securities" VALUES
(current_date,'LIST_GBA','GBA02280810',9007199254740993.000001,'KGS','ГКО-2','ГКО-2 летние','KG1'),
(current_date - 1,'LIST_GBA','GBA02280810',1,'KGS','old','old','KG1'),
-- The same SecCode in another class has its own names.
(current_date,'REPO_GBA','GBA02280810',2,'USD','РЕПО','РЕПО ГКО','KG1'),
(current_date - 1,'LIST_GBA','KG0105166315',NULL,'KGS','Акции','Акции (вчера)',NULL);
CREATE TABLE public."Classes" ("TradeDate" timestamp,"ClassCode" varchar,"ClassName" varchar);
INSERT INTO public."Classes" VALUES (current_date,'LIST_GBA','Листинг ГБА'),(current_date - 1,'LIST_GBA','old'),(current_date,'REPO_GBA','РЕПО');`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Params"; DROP TABLE public."Securities"; DROP TABLE public."Classes"`) })
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
	// Only the chosen columns, values as stored; the instrument is matched by
	// ClassCode and SecCode on its latest TradeDate.
	if got, want := string(page.Params[0].Raw), `{"ClassCode":"LIST_GBA    ","SecCode":"GBA02280810 ","CouponValue":9007199254740993.000001,"status":"торгуется","FaceUnit":"KGS","SecShortName":"ГКО-2","SecFullName":"ГКО-2 летние","ClassName":"Листинг ГБА"}`; got != want {
		t.Fatalf("row:\n%s\n%s", got, want)
	}
	page, err = s.ListParams(ctx, ParamFilter{ClassCode: "LIST_GBA", SecCode: "KG0105166315"})
	if err != nil || len(page.Params) != 1 || page.HasMore {
		t.Fatal(page, err)
	}
	if !bytes.Contains(page.Params[0].Raw, []byte(`"SecFullName":"Акции (вчера)","ClassName":"Листинг ГБА"`)) {
		t.Fatalf("earlier reference row not used: %s", page.Params[0].Raw)
	}
	if page, err := s.ListParams(ctx, ParamFilter{}); err != nil || len(page.Params) != 3 {
		t.Fatal("earlier TradeDate rows listed", page, err)
	}
	p, err := s.GetParam(ctx, "TQBR", "SBER")
	if err != nil || !bytes.HasSuffix(p.Raw, []byte(`"CouponValue":null,"status":null,"FaceUnit":null,"SecShortName":null,"SecFullName":null,"ClassName":null}`)) {
		t.Fatal(string(p.Raw), err)
	}
	// Served classes come from the administrator setting.
	var classes []string
	s.SetParamClasses(func(context.Context) ([]string, error) { return classes, nil })
	classes = []string{"TQBR"}
	if page, err := s.ListParams(ctx, ParamFilter{}); err != nil || len(page.Params) != 1 || !bytes.Contains(page.Params[0].Raw, []byte(`"SecCode":"SBER`)) {
		t.Fatal("class filter", page, err)
	}
	if _, err := s.GetParam(ctx, "LIST_GBA", "GBA02280810"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal("instrument of another class served", err)
	}
	classes = []string{}
	if page, err := s.ListParams(ctx, ParamFilter{}); err != nil || len(page.Params) != 0 {
		t.Fatal("empty class list served rows", page, err)
	}
	s.SetParamClasses(func(context.Context) ([]string, error) { return nil, errors.New("settings down") })
	if _, err := s.ListParams(ctx, ParamFilter{}); err == nil {
		t.Fatal("settings failure served unfiltered rows")
	}
	s.SetParamClasses(nil)
	// The instrument page ignores the served classes and keeps every stored column.
	d, err := s.GetParamDetails(ctx, "LIST_GBA", "GBA02280810")
	if err != nil || !bytes.Contains(d.Params.Raw, []byte(`"last":123.45`)) || !bytes.Contains(d.Params.Raw, []byte(`"couponvalue":7`)) ||
		d.Security == nil || !bytes.Contains(d.Security.Raw, []byte(`"ISINCode":"KG1"`)) || !bytes.Contains(d.Security.Raw, []byte(`"SecShortName":"ГКО-2"`)) ||
		d.Class == nil || !bytes.Contains(d.Class.Raw, []byte(`"ClassName":"Листинг ГБА"`)) {
		t.Fatal(d, err)
	}
	if d, err := s.GetParamDetails(ctx, "TQBR", "SBER"); err != nil || d.Security != nil || d.Class != nil {
		t.Fatal("missing reference rows", d, err)
	}
	if _, err := s.GetParamDetails(ctx, "TQBR", "GAZP"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal("yesterday's instrument has a page", err)
	}
	list, err := s.ListClasses(ctx)
	if err != nil || len(list) != 2 || string(list[0].Raw) != `{"ClassCode":"LIST_GBA","ClassName":"Листинг ГБА"}` {
		t.Fatal(list, err)
	}
	if _, err := s.GetParam(ctx, "TQBR", "GAZP"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal(err)
	}
	if _, err := s.GetParam(ctx, "TQBR", "SBER' OR '1'='1"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal("injection-like key matched", err)
	}
}

func TestQUIKTradingAccountsAndFirmsByFirm(t *testing.T) {
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
	_, err = pool.Exec(ctx, `CREATE TABLE public."Trdaccs" ("TrdAccId" char(16),"Type" integer,"FirmId" char(12),"Description" text,"Status" integer,"Flags" bigint);
INSERT INTO public."Trdaccs" VALUES ('1-3001-71',1,'2040','Сенти',0,9007199254740993),('1-3001-72',1,'2040',NULL,0,0),('1-9999-01',1,'9999','other',0,0);
CREATE TABLE public."Firms" ("Exchange" text,"FirmId" char(12),"FirmName" text,"Status" text,"StatusFlag" integer);
INSERT INTO public."Firms" VALUES ('KSE','2040','Сенти',NULL,1),('KSE','9999','Other','x',0);`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Trdaccs"; DROP TABLE public."Firms"`) })
	readOnly, err := db.ConnectReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	s := NewStore(readOnly)
	list, err := s.ListTradingAccounts(ctx, "2040")
	if err != nil || len(list) != 2 || !bytes.Contains(list[0].Raw, []byte(`"Flags":9007199254740993`)) || !bytes.Contains(list[1].Raw, []byte(`"Description":null`)) {
		t.Fatal(list, err)
	}
	if list, err := s.ListTradingAccounts(ctx, ""); err != nil || len(list) != 0 {
		t.Fatal("empty firm matched accounts", list, err)
	}
	acc, err := s.TradingAccount(ctx, "2040", " 1-3001-71 ")
	if err != nil || strings.TrimSpace(acc.Text("TrdAccId")) != "1-3001-71" {
		t.Fatal(acc, err)
	}
	// Another firm's account is not tradable for this firm.
	if _, err := s.TradingAccount(ctx, "2040", "1-9999-01"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatal(err)
	}
	if _, err := s.TradingAccount(ctx, "2040", "1-3001-7"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatal("prefix matched", err)
	}
	firms, err := s.ListFirms(ctx)
	if err != nil || len(firms) != 2 {
		t.Fatal(firms, err)
	}
	f, err := s.Firm(ctx, "2040")
	if err != nil || f.Text("FirmName") != "Сенти" || !bytes.Contains(f.Raw, []byte(`"Status":null`)) {
		t.Fatal(string(f.Raw), err)
	}
	if _, err := s.Firm(ctx, "0000"); !errors.Is(err, ErrFirmNotFound) {
		t.Fatal(err)
	}
}

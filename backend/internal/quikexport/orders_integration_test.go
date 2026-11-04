//go:build integration

package quikexport

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

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
	day, err := s.ListOrders(ctx, OrderFilter{TradeDay: "2026-10-21"})
	if err != nil || len(day.Orders) != 1 || day.Orders[0].Text("ClassCode") != "OTHER" {
		t.Fatal("trade day filter", day, err)
	}
	// The served classes limit every read of Orders, except for cancelling.
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{"OTHER"}, nil })
	if page, err := s.ListOrders(ctx, OrderFilter{}); err != nil || len(page.Orders) != 1 || page.Orders[0].Text("ClassCode") != "OTHER" {
		t.Fatal("class filter", page, err)
	}
	if counts, err := s.CountOrdersByState(ctx); err != nil || len(counts) != 1 || counts["Снята"] != 1 {
		t.Fatal("class filter in counters", counts, err)
	}
	key := first.Orders[0].VenueID()
	if _, err := s.GetOrder(ctx, key, nil); !errors.Is(err, ErrOrderNotFound) {
		t.Fatal("order of another class served", err)
	}
	if _, err := s.GetOrder(WithAllClasses(ctx), key, nil); err != nil {
		t.Fatal("order must stay cancellable", err)
	}
	s.SetServedClasses(func(context.Context) ([]string, error) { return nil, errors.New("settings down") })
	if _, err := s.ListOrders(ctx, OrderFilter{}); err == nil {
		t.Fatal("settings failure served unfiltered rows")
	}
	if _, err := s.GetOrder(ctx, key, nil); err == nil || errors.Is(err, ErrOrderNotFound) {
		t.Fatal("settings failure must not read as a missing order", err)
	}
}

func TestQUIKTradesLimitedToServedClasses(t *testing.T) {
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
	_, err = pool.Exec(ctx, `CREATE TABLE public."Trades" ("TradeNum" numeric,"ClassCode" char(12),"SecCode" char(12),"TradeDate" timestamp,"FirmId" text,"Operation" text);
INSERT INTO public."Trades" VALUES (1,'LIST_GD','GD1','2026-10-22','2040','Купля'),(2,'LIST_GBA','GBA1','2026-10-22','2040','Купля'),(3,'REPO','R1','2026-10-22','2040','Купля'),(4,'LIST_GBA','GBA1','2026-10-21','2040','Купля')`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Trades"`) })
	s := NewStore(pool)
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{"LIST_GD", "LIST_GBA"}, nil })
	page, err := s.ListTrades(ctx, TradeFilter{Limit: 10})
	if err != nil || len(page.Trades) != 3 {
		t.Fatal("class filter", page, err)
	}
	if page, err := s.ListTrades(ctx, TradeFilter{TradeDay: "2026-10-22", ClassCode: "LIST_GBA", SecCode: "GBA1", Limit: 10}); err != nil || len(page.Trades) != 1 || page.Trades[0].Text("TradeNum") != "2" {
		t.Fatal("date and instrument filter", page, err)
	}
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{}, nil })
	if page, err := s.ListTrades(ctx, TradeFilter{Limit: 10}); err != nil || len(page.Trades) != 4 {
		t.Fatal("empty choice serves every class", page, err)
	}
}

func TestSecuritiesCurrentRowsAndOrderCounts(t *testing.T) {
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
	_, err = pool.Exec(ctx, `CREATE TABLE public."Securities" ("TradeDate" timestamp,"ClassCode" char(12),"SecCode" char(12),"FaceUnit" text);
INSERT INTO public."Securities" VALUES ('2026-11-02','LIST_GBA','GBA02281026','EUR'),('2026-11-02','LIST_GD','GD052270215','KGS'),
('2026-11-01','LIST_GBA','GBA02281026','KGS'),('2026-11-01','LIST_GBA','DELISTED','KGS');
CREATE TABLE public."Orders" ("TradeDate" timestamp,"ClassCode" char(12),"FirmId" char(12),"State" text);
INSERT INTO public."Orders" VALUES ('2026-11-02','LIST_GBA','2040','Активна'),('2026-11-02','LIST_GBA','2040','Активна'),
('2026-11-01','LIST_GBA','2040','Снята'),('2026-10-01','LIST_GBA','2040','Снята'),('2026-11-02','LIST_GD','2040','Снята'),('2026-11-02','LIST_GBA','9999','Активна');`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Securities"; DROP TABLE public."Orders"`) })
	s := NewStore(pool)
	page, err := s.ListSecurities(ctx, SecurityFilter{Limit: 1})
	if err != nil || len(page.Securities) != 1 || !page.HasMore || page.NextOffset != 1 {
		t.Fatalf("paging: %+v %v", page, err)
	}
	all, err := s.ListSecurities(ctx, SecurityFilter{})
	if err != nil || len(all.Securities) != 2 || all.HasMore {
		t.Fatalf("only the latest date is current: %+v %v", all, err)
	}
	row, err := s.Instrument(ctx, " LIST_GBA ", "GBA02281026")
	if err != nil || row.Text("FaceUnit") != "EUR" {
		t.Fatalf("instrument of the latest date: %+v %v", row, err)
	}
	if _, err := s.Instrument(ctx, "LIST_GBA", "DELISTED"); !errors.Is(err, ErrInstrumentNotFound) {
		t.Fatal("an instrument QUIK no longer exports must not be tradable", err)
	}
	if _, err := s.Instrument(ctx, "LIST_GD", "GBA02281026"); !errors.Is(err, ErrInstrumentNotFound) {
		t.Fatal("the class is part of the key", err)
	}
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{"LIST_GD"}, nil })
	if all, err = s.ListSecurities(ctx, SecurityFilter{}); err != nil || len(all.Securities) != 1 || strings.TrimSpace(all.Securities[0].Text("ClassCode")) != "LIST_GD" {
		t.Fatalf("served classes: %+v %v", all, err)
	}
	counts, err := s.CountOrdersByDay(ctx, []string{"2040"}, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil || len(counts) != 1 || counts[0] != (OrderDayCount{Date: "2026-11-02", State: "Снята", Count: 1}) {
		t.Fatalf("counts for the firm, served classes and dates: %+v %v", counts, err)
	}
	s.SetServedClasses(nil)
	if counts, err = s.CountOrdersByDay(ctx, []string{"2040"}, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); err != nil || len(counts) != 3 {
		t.Fatalf("counts: %+v %v", counts, err)
	}
}

func TestPositionsCurrentRowsAndFirmScope(t *testing.T) {
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
	_, err = pool.Exec(ctx, `CREATE TABLE public."MoneyLimits" ("LoadDate" timestamp,"FirmId" char(12),"ClientCode" text,"Tag" text,"CurrCode" text,"LimitKind" text,"CurrentBal" numeric(24,8),"Locked" numeric);
INSERT INTO public."MoneyLimits" VALUES ('2026-11-02','2040','C1','EQTV','KGS','T0',42628923.91000000,NULL),('2026-11-01','2040','OLD','EQTV','KGS','T0',1,NULL),('2026-11-02','9999','C2','EQTV','KGS','T0',5,NULL);
CREATE TABLE public."DepoLimits" ("LoadDate" timestamp,"FirmId" char(12),"ClientCode" text,"SecCode" text,"Trdacc" text,"LimitKind" text,"CurrentBal" numeric(24,8));
INSERT INTO public."DepoLimits" VALUES ('2026-11-02','2040','C1','GBA02280810','1-3101-72','T0',17),('2026-11-02','9999','C2','GBA02280810','1-3001-72','T0',3);`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."MoneyLimits"; DROP TABLE public."DepoLimits"`) })
	s := NewStore(pool)
	// The served classes do not limit positions: these tables have no ClassCode.
	s.SetServedClasses(func(context.Context) ([]string, error) { return []string{"LIST_GD"}, nil })
	own, err := s.ListPositions(ctx, []string{" 2040 "})
	if err != nil || len(own.Cash) != 1 || len(own.Instruments) != 1 {
		t.Fatalf("firm scope and the latest date: %+v %v", own, err)
	}
	if !bytes.Contains(own.Cash[0].Raw, []byte(`"CurrentBal":42628923.91000000`)) || !bytes.Contains(own.Cash[0].Raw, []byte(`"Locked":null`)) {
		t.Fatalf("row changed: %s", own.Cash[0].Raw)
	}
	all, err := s.ListPositions(ctx, nil)
	if err != nil || len(all.Cash) != 2 || len(all.Instruments) != 2 {
		t.Fatalf("all firms: %+v %v", all, err)
	}
	for _, firms := range [][]string{{}, {""}, {"  "}} {
		none, err := s.ListPositions(ctx, firms)
		if err != nil || len(none.Cash) != 0 || len(none.Instruments) != 0 {
			t.Fatalf("an empty firm must match nothing, %q: %+v %v", firms, none, err)
		}
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
"ClassCode" char(12),"SecCode" char(12),"last" numeric,"couponvalue" numeric,"TradeDate" timestamp,"status" text,
lotsize numeric(19,0) DEFAULT 1,tradingstatus varchar DEFAULT 'открыта',pricemin numeric(19,8) DEFAULT 86,pricemax numeric,realminstep numeric,bid numeric,biddepth numeric,"offer" numeric,offerdepth numeric,qty numeric,"time" timestamp DEFAULT '1900-01-01 16:40:15',
"open" numeric,high numeric,low numeric,prevprice numeric,waprice numeric,numtrades numeric,voltoday numeric,valtoday numeric,accruedint numeric,yield numeric,nextcoupon timestamp,couponperiod numeric,
theorprice numeric DEFAULT 1);
INSERT INTO public."Params" ("ClassCode","SecCode","last","couponvalue","TradeDate","status") SELECT c, s, l, 7, d, st FROM (VALUES
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
	if got, want := string(page.Params[0].Raw), `{"ClassCode":"LIST_GBA    ","SecCode":"GBA02280810 ","CouponValue":9007199254740993.000001,"status":"торгуется","FaceUnit":"KGS","SecShortName":"ГКО-2","SecFullName":"ГКО-2 летние","ClassName":"Листинг ГБА",` +
		`"lotsize":1,"tradingstatus":"открыта","pricemin":86.00000000,"pricemax":null,"realminstep":null,"bid":null,"biddepth":null,"offer":null,"offerdepth":null,"last":123.45,"qty":null,"time":"1900-01-01T16:40:15",` +
		`"open":null,"high":null,"low":null,"prevprice":null,"waprice":null,"numtrades":null,"voltoday":null,"valtoday":null,"accruedint":null,"yield":null,"nextcoupon":null,"couponperiod":null}`; got != want {
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
	if err != nil || !bytes.Contains(p.Raw, []byte(`"CouponValue":null,"status":null,"FaceUnit":null,"SecShortName":null,"SecFullName":null,"ClassName":null,"lotsize":1`)) {
		t.Fatal(string(p.Raw), err)
	}
	// Served classes come from the administrator setting.
	var classes []string
	s.SetServedClasses(func(context.Context) ([]string, error) { return classes, nil })
	classes = []string{"TQBR"}
	if page, err := s.ListParams(ctx, ParamFilter{}); err != nil || len(page.Params) != 1 || !bytes.Contains(page.Params[0].Raw, []byte(`"SecCode":"SBER`)) {
		t.Fatal("class filter", page, err)
	}
	if _, err := s.GetParam(ctx, "LIST_GBA", "GBA02280810"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal("instrument of another class served", err)
	}
	// The instrument page with its Securities and Classes rows follows the choice;
	// the list to choose from does not.
	if _, err := s.GetParamDetails(ctx, "LIST_GBA", "GBA02280810"); !errors.Is(err, ErrParamNotFound) {
		t.Fatal("instrument page of another class served", err)
	}
	if list, err := s.ListClasses(ctx); err != nil || len(list) != 2 {
		t.Fatal("classes to choose from limited by the choice", list, err)
	}
	classes = []string{}
	if page, err := s.ListParams(ctx, ParamFilter{}); err != nil || len(page.Params) != 3 {
		t.Fatal("empty choice serves every class", page, err)
	}
	s.SetServedClasses(func(context.Context) ([]string, error) { return nil, errors.New("settings down") })
	if _, err := s.ListParams(ctx, ParamFilter{}); err == nil {
		t.Fatal("settings failure served unfiltered rows")
	}
	if _, err := s.GetParamDetails(ctx, "LIST_GBA", "GBA02280810"); err == nil {
		t.Fatal("settings failure served unfiltered rows")
	}
	s.SetServedClasses(nil)
	// The instrument page keeps every stored column.
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

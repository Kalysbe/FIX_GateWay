//go:build integration

package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestQUIKOrdersHTTPIsolationAndDailyClear(t *testing.T) {
	dsn := os.Getenv("QUIK_ORDERS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable QUIK_ORDERS_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var database string
	if err := pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil || !strings.HasSuffix(database, "_test") {
		t.Fatal("disposable database required", database, err)
	}
	if err := migrate.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE public."Orders" (
"OrderNum" numeric(19,0),"ClassCode" text,"TradeDate" timestamp,"SecCode" text,"FirmId" text,
"Operation" text,"State" text,"Qty" numeric,"Balance" numeric,"Price" numeric,
PRIMARY KEY("OrderNum","ClassCode","TradeDate"));
INSERT INTO public."Orders" VALUES
(22455,'LIST_GBA','2026-10-22','GBA02280810','2040','Купля','Активна',1,1,120),
(22456,'LIST_GBA','2026-10-22','GBA02280810','9999','Продажа','Активна',2,2,125);`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Orders"`) })
	readOnly, err := db.ConnectReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(readOnly.Close)
	bs := brokers.NewStore(pool, "source-test-pepper")
	as := audit.NewStore(pool)
	osStore := orders.NewStore(pool)
	svc := orders.NewService(osStore, bs, as)
	s := NewServer(config.Config{AuthFailMaxPerMinute: 100}, slog.New(slog.NewTextHandler(io.Discard, nil)), pool, bs, svc, osStore, nil, nil, nil, as, nil, realtime.NewHub(), nil)
	s.SetQuikExport(quikexport.NewStore(readOnly))
	s.SetAdmission(&ratelimit.Store{Pool: pool, Admission: ratelimit.NewAdmission()})
	b, err := bs.CreateBroker(ctx, uuid.NewString(), "QUIK source test", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := bs.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: b.ID, Name: "source reader", QuikFirm: "2040", Permissions: auth.ReadOnlyPermissions})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, want int) []byte {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Authorization", "Bearer "+token.Raw)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var page []quikexport.Order
	if err := json.Unmarshal(call("/api/v1/orders?firm_id=9999&broker_id=99999", 200), &page); err != nil || len(page) != 1 || page[0].Text("FirmId") != "2040" {
		t.Fatal(page, err)
	}
	call("/api/v1/orders/quik-20261022-LIST_GBA-22456", 404)
	call("/api/v1/orders/quik-20261022-LIST_GBA-22455", 200)
	var instrument int64
	err = pool.QueryRow(ctx, `SELECT id FROM instruments WHERE quik_symbol='GBA02280810' AND board='LIST_GBA' LIMIT 1`).Scan(&instrument)
	if err != nil {
		err = pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board) VALUES($1,'GBA02280810','LIST_GBA') RETURNING id`, uuid.NewString()).Scan(&instrument)
	}
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO orders(id,broker_id,client_order_id,instrument_id,side,order_type,time_in_force,quantity,price,status,fix_firm,quik_order_id)
VALUES($1,$2,$3,$4,'BUY','LIMIT','GTC',196,105,'ACKNOWLEDGED','2040','261022-LIST_GBA-22455')`, id, b.ID, uuid.NewString(), instrument)
	if err != nil {
		t.Fatal(err)
	}
	var detail quikexport.Order
	if err := json.Unmarshal(call("/api/v1/orders/"+id.String(), 200), &detail); err != nil || detail.Text("Qty") != "1" || detail.Text("Price") != "120" || detail.Text("gateway_order_id") != "" {
		t.Fatal("local values replaced authoritative source", detail, err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM public."Orders" WHERE "FirmId"='2040'`); err != nil {
		t.Fatal(err)
	}
	call("/api/v1/orders/"+id.String(), 404)
	if err := json.Unmarshal(call("/api/v1/orders", 200), &page); err != nil || len(page) != 0 {
		t.Fatal(page, err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, id).Scan(&status); err != nil || status != "ACKNOWLEDGED" {
		t.Fatal("daily clear altered local audit history", status, err)
	}
}

//go:build integration

package api

import (
	"context"
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

func TestQUIKParamsHTTPRowsAsStored(t *testing.T) {
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
	var today string
	_, err = pool.Exec(ctx, `CREATE TABLE public."Params" ("ClassCode" text,"SecCode" text,"Last" numeric,"Bid" numeric,"FirmId" text,"TradeDate" timestamp);
INSERT INTO public."Params" SELECT c, s, l, b, f, (now() AT TIME ZONE 'Asia/Bishkek')::date - ago FROM (VALUES
('LIST_GBA','GBA02280810',123.4567890123456789,NULL::numeric,'9999',0),('TQBR','SBER',1,1,NULL,0),
('TQBR','SBER',2,2,NULL,1),('TQBR','GAZP',3,3,NULL,1)) v(c, s, l, b, f, ago);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT to_char((now() AT TIME ZONE 'Asia/Bishkek')::date, 'YYYY-MM-DD"T00:00:00"')`).Scan(&today); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Params"`) })
	readOnly, err := db.ConnectReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(readOnly.Close)
	bs := brokers.NewStore(pool, "params-test-pepper")
	as := audit.NewStore(pool)
	osStore := orders.NewStore(pool)
	s := NewServer(config.Config{AuthFailMaxPerMinute: 100}, slog.New(slog.NewTextHandler(io.Discard, nil)), pool, bs, orders.NewService(osStore, bs, as), osStore, nil, nil, nil, as, nil, realtime.NewHub(), nil)
	s.SetAdmission(&ratelimit.Store{Pool: pool, Admission: ratelimit.NewAdmission()})
	b, err := bs.CreateBroker(ctx, uuid.NewString(), "Params test", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := bs.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: b.ID, Name: "market reader", QuikFirm: "2040", Permissions: auth.ReadOnlyPermissions})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path string, want int) string {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Authorization", "Bearer "+token.Raw)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return strings.TrimSpace(w.Body.String())
	}
	// Before the source is configured: 503, never FIX quotes.
	call("/api/v1/market-data", 503)
	s.SetQuikExport(quikexport.NewStore(readOnly))
	// Market data is not firm-scoped: a FirmId column does not filter rows.
	// Only rows of the current trading day are served; earlier TradeDate rows are history.
	gba := `{"ClassCode":"LIST_GBA","SecCode":"GBA02280810","Last":123.4567890123456789,"Bid":null,"FirmId":"9999","TradeDate":"` + today + `"}`
	sber := `{"ClassCode":"TQBR","SecCode":"SBER","Last":1,"Bid":1,"FirmId":null,"TradeDate":"` + today + `"}`
	if got, want := call("/api/v1/market-data", 200), "["+gba+","+sber+"]"; got != want {
		t.Fatalf("rows changed:\n%s\n%s", got, want)
	}
	if got := call("/api/v1/market-data?board=TQBR", 200); got != "["+sber+"]" {
		t.Fatal(got)
	}
	if got := call("/api/v1/market-data/LIST_GBA/GBA02280810", 200); got != gba {
		t.Fatal(got)
	}
	call("/api/v1/market-data/TQBR/GAZP", 404)
	call("/api/v1/market-data?limit=0", 400)
	if _, err := pool.Exec(ctx, `DROP TABLE public."Params"`); err != nil {
		t.Fatal(err)
	}
	call("/api/v1/market-data", 503)
}

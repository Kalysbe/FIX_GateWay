//go:build integration

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/admin"
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
	// The migration's default; a previous run may have changed it.
	if _, err := pool.Exec(ctx, `UPDATE gateway_settings SET value='LISTBND' WHERE key='market_data_classes'`); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `CREATE TABLE public."Params" ("ClassCode" text,"SecCode" text,"last" numeric,"status" text,"TradeDate" timestamp);
INSERT INTO public."Params" SELECT c, s, l, st, (now() AT TIME ZONE 'Asia/Bishkek')::date - ago FROM (VALUES
('LISTBND','GBA02270915',100.5,'торгуется',0),('LISTBND','GD052260119',99,NULL,0),('AUCT_GBA','GBA02270915',1,'торгуется',0),
('LISTBND','OLD',2,'вчера',1)) v(c, s, l, st, ago);
CREATE TABLE public."Securities" ("TradeDate" timestamp,"ClassCode" text,"SecCode" text,"CouponValue" numeric,"FaceUnit" text,"SecShortName" text,"SecFullName" text);
INSERT INTO public."Securities" VALUES (current_date,'LISTBND','GBA02270915',2.500000,'KGS','ГКО-2','ГКО-2 летние'),
(current_date,'AUCT_GBA','GBA02270915',2.5,'KGS','ГКО аукцион','ГКО аукцион');
CREATE TABLE public."Classes" ("TradeDate" timestamp,"ClassCode" text,"ClassName" text,"FirmsId" text);
INSERT INTO public."Classes" VALUES (current_date,'LISTBND','Листинговые:облигации','KSE'),(current_date,'AUCT_GBA','Аукцион ГКО','KSE');`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS public."Params"; DROP TABLE public."Securities"; DROP TABLE public."Classes"`)
	})
	readOnly, err := db.ConnectReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(readOnly.Close)
	bs := brokers.NewStore(pool, "params-test-pepper")
	as := audit.NewStore(pool)
	osStore := orders.NewStore(pool)
	users := admin.NewUserStore(pool)
	cfg := config.Config{AuthFailMaxPerMinute: 100, JWTSecret: "params-test-jwt-secret-32-bytes!!!!"}
	s := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), pool, bs, orders.NewService(osStore, bs, as), osStore, nil, nil, nil, as, users, realtime.NewHub(), nil)
	s.SetAdmission(&ratelimit.Store{Pool: pool, Admission: ratelimit.NewAdmission()})
	b, err := bs.CreateBroker(ctx, uuid.NewString(), "Params test", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := bs.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: b.ID, Name: "market reader", QuikFirm: "2040", Permissions: auth.ReadOnlyPermissions})
	if err != nil {
		t.Fatal(err)
	}
	email := uuid.NewString() + "@example.test"
	if err := users.EnsureAdmin(ctx, email, "params-test-password"); err != nil {
		t.Fatal(err)
	}
	u, err := users.Authenticate(ctx, email, "params-test-password")
	if err != nil {
		t.Fatal(err)
	}
	jwt, err := admin.IssueToken(cfg.JWTSecret, u)
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, bearer string, body any, want int) string {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		r := httptest.NewRequest(method, path, &buf)
		r.RemoteAddr = "127.0.0.1:12345"
		r.Header.Set("Authorization", "Bearer "+bearer)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return strings.TrimSpace(w.Body.String())
	}
	get := func(path string, want int) string { t.Helper(); return call("GET", path, token.Raw, nil, want) }
	// Before the source is configured: 503, never FIX quotes.
	get("/api/v1/market-data", 503)
	classes := quikexport.NewClassSettings(pool)
	store := quikexport.NewStore(readOnly)
	store.SetParamClasses(classes.Classes)
	s.SetQuikExport(store)
	s.SetMarketDataClasses(classes)
	// Today's LISTBND rows (the default class), only the chosen columns, as stored.
	// Market data is not firm-scoped.
	bond := `{"ClassCode":"LISTBND","SecCode":"GBA02270915","CouponValue":2.500000,"status":"торгуется","FaceUnit":"KGS","SecShortName":"ГКО-2","SecFullName":"ГКО-2 летние","ClassName":"Листинговые:облигации"}`
	bare := `{"ClassCode":"LISTBND","SecCode":"GD052260119","CouponValue":null,"status":null,"FaceUnit":null,"SecShortName":null,"SecFullName":null,"ClassName":"Листинговые:облигации"}`
	if got, want := get("/api/v1/market-data", 200), "["+bond+","+bare+"]"; got != want {
		t.Fatalf("rows changed:\n%s\n%s", got, want)
	}
	if got := get("/api/v1/market-data/LISTBND/GBA02270915", 200); got != bond {
		t.Fatal(got)
	}
	get("/api/v1/market-data/AUCT_GBA/GBA02270915", 404)
	get("/api/v1/market-data/LISTBND/OLD", 404)
	get("/api/v1/market-data?limit=0", 400)

	// Administrators choose the classes; brokers cannot.
	if got := call("GET", "/api/v1/admin/market-data/settings", jwt, nil, 200); got != `{"classes":["LISTBND"],"default_classes":["LISTBND"]}` {
		t.Fatal(got)
	}
	call("PATCH", "/api/v1/admin/market-data/settings", token.Raw, map[string]any{"classes": []string{"AUCT_GBA"}}, 401)
	call("PATCH", "/api/v1/admin/market-data/settings", jwt, map[string]any{"classes": []string{}}, 400)
	call("PATCH", "/api/v1/admin/market-data/settings", jwt, map[string]any{"classes": []string{"LIST BND"}}, 400)
	if got := call("PATCH", "/api/v1/admin/market-data/settings", jwt, map[string]any{"classes": []string{" AUCT_GBA ", "AUCT_GBA"}}, 200); got != `{"classes":["AUCT_GBA"],"default_classes":["LISTBND"]}` {
		t.Fatal(got)
	}
	if got := get("/api/v1/market-data", 200); got != `[{"ClassCode":"AUCT_GBA","SecCode":"GBA02270915","CouponValue":2.5,"status":"торгуется","FaceUnit":"KGS","SecShortName":"ГКО аукцион","SecFullName":"ГКО аукцион","ClassName":"Аукцион ГКО"}]` {
		t.Fatal(got)
	}
	var audited int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='MARKET_DATA_CLASSES_UPDATED' AND entity_id='market_data_classes'`).Scan(&audited); err != nil || audited == 0 {
		t.Fatal("class change not audited", audited, err)
	}
	if got := call("GET", "/api/v1/admin/classes", jwt, nil, 200); got != `[{"ClassCode":"AUCT_GBA","ClassName":"Аукцион ГКО"},{"ClassCode":"LISTBND","ClassName":"Листинговые:облигации"}]` {
		t.Fatal(got)
	}
	call("PATCH", "/api/v1/admin/market-data/settings", jwt, map[string]any{"classes": []string{"LISTBND"}}, 200)

	if _, err := pool.Exec(ctx, `DROP TABLE public."Params"`); err != nil {
		t.Fatal(err)
	}
	get("/api/v1/market-data", 503)
}

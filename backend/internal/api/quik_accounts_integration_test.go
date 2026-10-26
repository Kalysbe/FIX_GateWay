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
	"strconv"
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

func TestQUIKAccountsAndFirmsHTTP(t *testing.T) {
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
	_, err = pool.Exec(ctx, `CREATE TABLE public."Trdaccs" ("TrdAccId" text,"FirmId" text,"Status" integer);
INSERT INTO public."Trdaccs" VALUES ('1-3001-71','2040',0),('1-9999-01','9999',0);
CREATE TABLE public."Firms" ("Exchange" text,"FirmId" text,"FirmName" text,"Status" text,"StatusFlag" integer);
INSERT INTO public."Firms" VALUES ('KSE','2040','Сенти',NULL,1),('KSE','9999','Other',NULL,0);`)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE public."Trdaccs"; DROP TABLE public."Firms"`) })
	readOnly, err := db.ConnectReadOnly(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(readOnly.Close)
	bs := brokers.NewStore(pool, "accounts-test-pepper")
	as := audit.NewStore(pool)
	osStore := orders.NewStore(pool)
	users := admin.NewUserStore(pool)
	cfg := config.Config{AuthFailMaxPerMinute: 100, JWTSecret: "accounts-test-jwt-secret-32-bytes!!"}
	svc := orders.NewService(osStore, bs, as)
	s := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), pool, bs, svc, osStore, nil, nil, nil, as, users, realtime.NewHub(), nil)
	s.SetAdmission(&ratelimit.Store{Pool: pool, Admission: ratelimit.NewAdmission()})
	b, err := bs.CreateBroker(ctx, uuid.NewString(), "Accounts test", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := bs.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: b.ID, Name: "trader", QuikFirm: "2040", Permissions: auth.AllPermissions})
	if err != nil {
		t.Fatal(err)
	}
	email := uuid.NewString() + "@example.test"
	if err := users.EnsureAdmin(ctx, email, "accounts-test-password"); err != nil {
		t.Fatal(err)
	}
	u, err := users.Authenticate(ctx, email, "accounts-test-password")
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
	order := map[string]any{"account_id": "1-3001-71", "client_code": "C1", "client_order_id": uuid.NewString(), "symbol": "X", "board": "Y",
		"side": "BUY", "order_type": "LIMIT", "quantity": "1", "price": "1"}
	// Without the QUIK source nothing can be confirmed.
	call("GET", "/api/v1/accounts", token.Raw, nil, 503)
	call("POST", "/api/v1/orders", token.Raw, order, 503)
	call("POST", "/api/v1/admin/brokers/"+itoa(b.ID)+"/tokens", jwt, map[string]any{"name": "x", "quik_firm": "2040"}, 503)

	s.SetQuikExport(quikexport.NewStore(readOnly))
	svc.SetTradingAccounts(quikexport.NewStore(readOnly))
	if got := call("GET", "/api/v1/accounts", token.Raw, nil, 200); got != `[{"TrdAccId":"1-3001-71","FirmId":"2040","Status":0}]` {
		t.Fatal(got)
	}
	if got := call("GET", "/api/v1/firm", token.Raw, nil, 200); got != `{"Exchange":"KSE","FirmId":"2040","FirmName":"Сенти","Status":null,"StatusFlag":1}` {
		t.Fatal(got)
	}
	if got := call("GET", "/api/v1/admin/brokers/"+itoa(b.ID)+"/accounts", jwt, nil, 200); got != `[{"TrdAccId":"1-3001-71","FirmId":"2040","Status":0}]` {
		t.Fatal(got)
	}
	if got := call("GET", "/api/v1/admin/firms", jwt, nil, 200); !strings.HasPrefix(got, `[{"Exchange":"KSE","FirmId":"2040"`) {
		t.Fatal(got)
	}
	// Another firm's account is refused; the account check passes for its own
	// (the instrument is unknown, so validation stops at the next step).
	order["account_id"] = "1-9999-01"
	if got := call("POST", "/api/v1/orders", token.Raw, order, 400); !strings.Contains(got, "не найден в QUIK Trdaccs") {
		t.Fatal(got)
	}
	order["account_id"] = "1-3001-71"
	if got := call("POST", "/api/v1/orders", token.Raw, order, 400); strings.Contains(got, "Trdaccs") || !strings.Contains(got, "instrument") {
		t.Fatal(got)
	}
	// Token firms must exist in QUIK Firms.
	call("POST", "/api/v1/admin/brokers/"+itoa(b.ID)+"/tokens", jwt, map[string]any{"name": "x", "quik_firm": "0000", "permissions": auth.ReadOnlyPermissions}, 400)
	call("POST", "/api/v1/admin/brokers/"+itoa(b.ID)+"/tokens", jwt, map[string]any{"name": "x", "quik_firm": "9999", "permissions": auth.ReadOnlyPermissions}, 201)
	call("PATCH", "/api/v1/admin/tokens/"+token.Token.ID.String(), jwt, map[string]any{"quik_firm": "0000"}, 400)
	call("POST", "/api/v1/admin/brokers/"+itoa(b.ID)+"/accounts", jwt, map[string]any{"trading_account": "1-3001-71"}, 405)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

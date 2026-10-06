//go:build integration

package api

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Use only a disposable database. Migrations and fixture inserts are intentional.
func TestSecurityAuditDatabase(t *testing.T) {
	dsn := os.Getenv("SECURITY_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable SECURITY_AUDIT_DATABASE_URL required")
	}
	path := os.Getenv("SECURITY_AUDIT_MIGRATIONS")
	if path == "" {
		path = "../../migrations"
	}
	if err := migrate.Up(dsn, path); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bs := brokers.NewStore(pool, "audit-pepper")
	as := audit.NewStore(pool)
	osStore := orders.NewStore(pool)
	svc := orders.NewService(osStore, bs, as)
	users := admin.NewUserStore(pool)
	s := NewServer(config.Config{AuthFailMaxPerMinute: 100, JWTSecret: "security-test-key", CORSOrigins: []string{"https://audit.example.test"}}, slog.New(slog.NewTextHandler(io.Discard, nil)), pool, bs, svc, osStore, nil, nil, nil, as, users, realtime.NewHub(), nil)
	rateStore := &ratelimit.Store{Pool: pool, Admission: ratelimit.NewAdmission()}
	if err := rateStore.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	s.SetAdmission(rateStore)
	a, err := bs.CreateBroker(ctx, uuid.NewString(), "Audit A", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := bs.CreateBroker(ctx, uuid.NewString(), "Audit B", "")
	if err != nil {
		t.Fatal(err)
	}
	token, err := bs.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: b.ID, Name: "audit", QuikFirm: "AUDIT", Permissions: auth.AllPermissions})
	if err != nil {
		t.Fatal(err)
	}
	account, err := bs.CreateAccount(ctx, a.ID, "audit-account", "audit-account", "audit", "audit")
	if err != nil {
		t.Fatal(err)
	}
	var instrument int64
	err = pool.QueryRow(ctx, `INSERT INTO instruments (internal_symbol, quik_symbol) VALUES ($1, $1) RETURNING id`, uuid.NewString()).Scan(&instrument)
	if err != nil {
		t.Fatal(err)
	}
	orderID := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO orders (id,broker_id,account_id,client_order_id,instrument_id,side,order_type,quantity,price,status) VALUES ($1,$2,$3,$4,$5,'BUY','LIMIT',1,1,'NEW')`, orderID, a.ID, account.ID, uuid.NewString(), instrument)
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, raw string, want int) {
		t.Helper()
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "203.0.113.20:1000"
		r.Header.Set("Authorization", "Bearer "+raw)
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, r)
		if rr.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, rr.Code, want, rr.Body.String())
		}
	}
	t.Run("other_broker_order_read_denied", func(t *testing.T) { request("GET", "/api/v1/orders/"+orderID.String(), token.Raw, 404) })
	t.Run("other_broker_order_cancel_denied", func(t *testing.T) { request("POST", "/api/v1/orders/"+orderID.String()+"/cancel", token.Raw, 400) })
	t.Run("readonly_token_cannot_create", func(t *testing.T) {
		ro, err := bs.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: b.ID, Name: "read-only", QuikFirm: "AUDIT", Permissions: auth.ReadOnlyPermissions})
		if err != nil {
			t.Fatal(err)
		}
		request("POST", "/api/v1/orders", ro.Raw, 403)
	})
	t.Run("expired_token_denied", func(t *testing.T) {
		past := time.Now().Add(-time.Minute)
		expired, err := bs.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: b.ID, Name: "expired", QuikFirm: "AUDIT", ExpiresAt: &past})
		if err != nil {
			t.Fatal(err)
		}
		request("GET", "/api/v1/orders", expired.Raw, 401)
	})
	t.Run("ip_whitelist_denied", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `UPDATE brokers SET ip_whitelist_enabled=true WHERE id=$1`, b.ID); err != nil {
			t.Fatal(err)
		}
		request("GET", "/api/v1/orders", token.Raw, 403)
		if _, err := pool.Exec(ctx, `UPDATE brokers SET ip_whitelist_enabled=false WHERE id=$1`, b.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("revoked_token_denied", func(t *testing.T) {
		if err := bs.RevokeToken(ctx, token.Token.ID); err != nil {
			t.Fatal(err)
		}
		request("GET", "/api/v1/orders", token.Raw, 401)
	})
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1`, orderID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "NEW" {
		t.Fatalf("unauthorized request changed order: %s", status)
	}
	t.Run("admin_session_revocation", func(t *testing.T) {
		email := uuid.NewString() + "@example.test"
		if err := users.EnsureAdmin(ctx, email, "audit-test-password"); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET role_code='READ_ONLY' WHERE email=$1`, email); err != nil {
			t.Fatal(err)
		}
		u, err := users.Authenticate(ctx, email, "audit-test-password")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := admin.IssueToken(s.cfg.JWTSecret, u)
		if err != nil {
			t.Fatal(err)
		}
		request("POST", "/api/v1/admin/brokers", raw, 403)
		if _, err := users.AuthenticateSession(ctx, s.cfg.JWTSecret, raw); err != nil {
			t.Fatal(err)
		}
		configureTestRealtime(t, s, users, s.cfg.JWTSecret)
		httpServer := httptest.NewServer(s.Handler())
		defer httpServer.Close()
		streamCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		r, err := http.NewRequestWithContext(streamCtx, "GET", httpServer.URL+"/api/v1/admin/events/stream", nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Authorization", "Bearer "+raw)
		response, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(response.Status)
		}
		scanner := bufio.NewScanner(response.Body)
		if !scanner.Scan() || scanner.Text() != "event: ready" {
			t.Fatal("missing SSE ready", scanner.Err())
		}
		s.hub.Broadcast("obs.event", map[string]any{"private": true})
		s.hub.Broadcast("marketdata.tick", map[string]any{"public": true})
		found := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "private") {
				t.Fatal("audit leaked")
			}
			if strings.Contains(line, "marketdata.tick") {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("public event missing", scanner.Err())
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET status='DISABLED' WHERE id=$1`, u.ID); err != nil {
			t.Fatal(err)
		}
		request("GET", "/api/v1/admin/events/stream", raw, 401)
		if _, err := users.AuthenticateSession(ctx, s.cfg.JWTSecret, raw); err == nil {
			t.Fatal("revoked session accepted")
		}
		for scanner.Scan() {
		}
		if streamCtx.Err() != nil {
			t.Fatal("revoked SSE stream did not close")
		}
	})
	t.Run("password_update_revokes_session_without_explicit_timestamp", func(t *testing.T) {
		email := uuid.NewString() + "@example.test"
		if err := users.EnsureAdmin(ctx, email, "audit-test-password"); err != nil {
			t.Fatal(err)
		}
		u, err := users.Authenticate(ctx, email, "audit-test-password")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := admin.IssueToken(s.cfg.JWTSecret, u)
		if err != nil {
			t.Fatal(err)
		}
		request("GET", "/api/v1/admin/users", raw, 200)
		hash, err := auth.HashPassword("new-audit-test-password")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, u.ID, hash); err != nil {
			t.Fatal(err)
		}
		request("GET", "/api/v1/admin/users", raw, 401)
	})
}

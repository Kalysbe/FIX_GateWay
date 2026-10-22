//go:build integration

package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTokenLookupDatabase(t *testing.T) {
	dsn := os.Getenv("TOKEN_LOOKUP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable TOKEN_LOOKUP_TEST_DATABASE_URL required")
	}
	if err := migrate.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := brokers.NewStore(pool, "lookup-test-pepper")
	broker, err := store.CreateBroker(ctx, "LOOKUP_"+uuid.NewString(), "Lookup test broker", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{pool: pool, brokers: store, audit: audit.NewStore(pool), log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	create := func(status domain.TokenStatus, legacy bool) brokers.CreatedToken {
		t.Helper()
		created, err := store.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: broker.ID, Name: "Lookup fixture", QuikFirm: "2040", Permissions: []string{auth.PermOrdersRead}})
		if err != nil {
			t.Fatal(err)
		}
		hash := auth.HashAPIToken(created.Raw, "lookup-test-pepper")
		if legacy {
			hash = auth.HashToken(created.Raw, "lookup-test-pepper")
		}
		if _, err := pool.Exec(ctx, `UPDATE api_tokens SET status=$2, token_hash=$3 WHERE id=$1`, created.Token.ID, status, hash); err != nil {
			t.Fatal(err)
		}
		return created
	}
	search := func(value, role string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"token": value})
		r := httptest.NewRequest("POST", "/api/v1/admin/tokens/search", strings.NewReader(string(body)))
		r = r.WithContext(context.WithValue(r.Context(), ctxAdmin, AdminPrincipal{RoleCode: role}))
		w := httptest.NewRecorder()
		s.requireAdminRole(auth.RolesAll...)(http.HandlerFunc(s.searchTokens)).ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		name   string
		status domain.TokenStatus
		legacy bool
	}{
		{"active", domain.TokenActive, false}, {"revoked", domain.TokenRevoked, false},
		{"disabled", domain.TokenDisabled, false}, {"legacy", domain.TokenActive, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			created := create(tc.status, tc.legacy)
			for _, value := range []string{created.Raw, "Bearer " + created.Raw, created.Token.ID.String(), created.Token.TokenPrefix + "…"} {
				w := search(value, auth.RoleReadOnly)
				var result struct {
					Tokens  []map[string]any `json:"tokens"`
					HasMore bool             `json:"has_more"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 || len(result.Tokens) != 1 || result.HasMore {
					t.Fatalf("lookup failed: %d %v", w.Code, err)
				}
				if result.Tokens[0]["id"] != created.Token.ID.String() || result.Tokens[0]["broker_name"] != broker.Name || result.Tokens[0]["status"] != string(tc.status) {
					t.Fatal("wrong token owner")
				}
				if strings.Contains(w.Body.String(), created.Raw) || strings.Contains(w.Body.String(), "token_hash") || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("lookup response leaked credential or permitted caching")
				}
			}
			var lastUsed *time.Time
			if err := pool.QueryRow(ctx, `SELECT last_used_at FROM api_tokens WHERE id=$1`, created.Token.ID).Scan(&lastUsed); err != nil || lastUsed != nil {
				t.Fatal("inspection authenticated or touched token", err)
			}
			wrong := created.Raw[:len(created.Raw)-1] + "A"
			// A different valid secret sharing the prefix must never fall back to prefix search.
			if created.Raw[len(created.Raw)-1] == 'A' {
				wrong = created.Raw[:len(created.Raw)-1] + "B"
			}
			if w := search(wrong, auth.RoleAdmin); w.Code != 200 || !strings.Contains(w.Body.String(), `"tokens":[]`) {
				t.Fatal("invalid full token fell back to prefix search")
			}
		})
	}
	created := create(domain.TokenActive, false)
	past := time.Now().Add(-time.Hour)
	if _, err := pool.Exec(ctx, `UPDATE api_tokens SET expires_at=$2 WHERE id=$1`, created.Token.ID, past); err != nil {
		t.Fatal(err)
	}
	if w := search(created.Raw, auth.RoleAdmin); w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"EXPIRED"`) {
		t.Fatal("expired token missing")
	}
	var storedStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM api_tokens WHERE id=$1`, created.Token.ID).Scan(&storedStatus); err != nil || storedStatus != "ACTIVE" {
		t.Fatal("lookup changed stored expiry state")
	}
	if w := search(created.Raw, "BROKER"); w.Code != 403 {
		t.Fatal("non-admin role accepted")
	}

	if _, err := store.AddIPWhitelist(ctx, broker.ID, "203.0.113.0/24", "Office"); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/admin/tokens/"+created.Token.ID.String(), nil)
	r.SetPathValue("id", created.Token.ID.String())
	w := httptest.NewRecorder()
	s.getTokenOverview(w, r)
	var overview struct {
		Token     map[string]any         `json:"token"`
		Broker    domain.Broker          `json:"broker"`
		Whitelist []domain.BrokerIPAllow `json:"ip_whitelist"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil || w.Code != 200 || overview.Broker.ID != broker.ID || len(overview.Whitelist) != 1 || overview.Token["quik_firm"] != "2040" || overview.Token["status"] != "EXPIRED" {
		t.Fatal("incomplete overview", err)
	}
	if strings.Contains(w.Body.String(), created.Raw) || strings.Contains(w.Body.String(), "token_hash") {
		t.Fatal("overview leaked secret")
	}
	var auditCount, unsafeAudit int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE request_payload IS NOT NULL) FROM audit_logs WHERE action IN ('TOKEN_LOOKUP','TOKEN_INSPECTED')`).Scan(&auditCount, &unsafeAudit); err != nil || auditCount == 0 || unsafeAudit != 0 {
		t.Fatal("lookup audit persisted request body")
	}

	prefix := "shared12"
	for i := 0; i < 21; i++ {
		raw := prefix + strings.ReplaceAll(uuid.NewString(), "-", "") + strings.Repeat("a", 24)
		_, err := pool.Exec(ctx, `INSERT INTO api_tokens (broker_id,name,token_hash,token_prefix,permissions,quik_firm) VALUES ($1,'Collision fixture',$2,$3,'["orders:read"]','2040')`, broker.ID, auth.HashAPIToken(raw, "lookup-test-pepper"), prefix)
		if err != nil {
			t.Fatal(err)
		}
	}
	limited := search(prefix, auth.RoleAuditor)
	var result struct {
		Tokens  []map[string]any `json:"tokens"`
		HasMore bool             `json:"has_more"`
	}
	if err := json.Unmarshal(limited.Body.Bytes(), &result); err != nil || limited.Code != 200 || len(result.Tokens) != 20 || !result.HasMore {
		t.Fatal("ambiguous prefix search is not bounded", err)
	}
}

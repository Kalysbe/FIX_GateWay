package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
)

// Regression tests for the security audit.
func TestSecurityAuditHMAC(t *testing.T) {
	s := &Server{cfg: config.Config{HMACRequired: true}, hmacNonces: auth.NewNonceCache(5 * time.Minute)}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	request := func(query, nonce string) *http.Request {
		r := httptest.NewRequest("GET", "/api/v1/orders?"+query, strings.NewReader(""))
		r.Header.Set("X-Timestamp", ts)
		r.Header.Set("X-Nonce", nonce)
		r.Header.Set("X-Signature", auth.SignHMACSHA256("test-secret", auth.BuildHMACPayload(ts, nonce, "GET", "/api/v1/orders?limit=1", "")))
		return r
	}
	t.Run("valid_and_replay_rejected", func(t *testing.T) {
		if err := s.verifyOptionalHMAC(request("limit=1", "nonce-0001"), "test-secret"); err != nil {
			t.Fatal(err)
		}
		if err := s.verifyOptionalHMAC(request("limit=1", "nonce-0001"), "test-secret"); err != auth.ErrNonceReplay {
			t.Fatalf("replay: %v", err)
		}
	})
	t.Run("query_tamper_rejected", func(t *testing.T) {
		if err := s.verifyOptionalHMAC(request("limit=999&status=CANCELLED", "nonce-0002"), "test-secret"); err == nil {
			t.Fatal(err)
		}
	})
	t.Run("body_tamper_rejected", func(t *testing.T) {
		r := request("", "nonce-0003")
		r.Body = io.NopCloser(strings.NewReader("tampered"))
		if err := s.verifyOptionalHMAC(r, "test-secret"); err == nil {
			t.Fatal("tampered body accepted")
		}
	})
	t.Run("incomplete_optional_signature_rejected", func(t *testing.T) {
		optional := &Server{}
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("X-Signature", "invalid")
		if err := optional.verifyOptionalHMAC(r, "secret"); err == nil {
			t.Fatal(err)
		}
	})
}

func TestSecurityAuditHTTPSAndRoles(t *testing.T) {
	s := &Server{cfg: config.Config{RequireHTTPS: true, JWTSecret: "audit-test-secret"}}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	s.requireHTTPS(next).ServeHTTP(rr, r)
	if rr.Code != 403 {
		t.Fatalf("untrusted HTTPS spoof: %d", rr.Code)
	}
	raw, err := auth.IssueAdminJWT(s.cfg.JWTSecret, uuid.New(), "audit@example.test", auth.RoleReadOnly, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+raw)
	rr = httptest.NewRecorder()
	s.requireAdminRole(auth.RolesWrite...)(next).ServeHTTP(rr, r.WithContext(context.WithValue(r.Context(), ctxAdmin, AdminPrincipal{RoleCode: auth.RoleReadOnly})))
	if rr.Code != 403 {
		t.Fatalf("read-only mutation: %d", rr.Code)
	}
}

func TestSecurityAuditReadOnlyCannotReceiveAuditStream(t *testing.T) {
	hub := realtime.NewHub()
	sub, err := hub.Subscribe(auth.RoleReadOnly, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	hub.Broadcast("obs.event", map[string]any{"private": true})
	hub.Broadcast("marketdata.tick", map[string]any{"public": true})
	select {
	case event := <-sub.Events:
		if event.Type != "marketdata.tick" {
			t.Fatal(event.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("event missing")
	}
	select {
	case event := <-sub.Events:
		t.Fatal("unexpected event", event)
	default:
	}
}
func TestSecurityAuditWSTicketRedacted(t *testing.T) {
	if got := observability.RedactQuery("ticket=audit-sensitive-ticket"); got != "ticket=***" {
		t.Fatalf("behavior changed: %s", got)
	}
}

func TestSecurityAuditClientHeaderCannotSuppressHTTPAudit(t *testing.T) {
	r := httptest.NewRequest("POST", "https://gateway.example.test/api/v1/orders", nil)
	r.RemoteAddr = "203.0.113.20:1000"
	if observability.IsInternalUIRequest(r, nil) {
		t.Fatal("ordinary request misclassified")
	}
	r.Header.Set("X-Forwarded-Host", "localhost:5173")
	if observability.IsInternalUIRequest(r, nil) {
		t.Fatal("behavior changed: spoof no longer suppresses HTTP audit")
	}
}

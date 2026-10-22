package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

func TestTokenLookupMetadataIsReadOnlyAndSecretFree(t *testing.T) {
	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	for _, status := range []domain.TokenStatus{domain.TokenActive, domain.TokenRevoked, domain.TokenDisabled} {
		token := domain.APIToken{Status: status, ExpiresAt: &past, TokenHash: "private-storage-digest", TokenPrefix: "prefix12", Permissions: []string{}}
		result := lookupTokenDTO(token, now)
		want := status
		if status == domain.TokenActive {
			want = domain.TokenExpired
		}
		if result["status"] != want || token.Status != status || token.LastUsedAt != nil {
			t.Fatal("effective status changed token data")
		}
		encoded, _ := json.Marshal(result)
		if strings.Contains(string(encoded), "private-storage-digest") || strings.Contains(string(encoded), "token_hash") || result["token"] != nil {
			t.Fatal("secret in lookup metadata")
		}
	}
}

func TestTokenLookupRejectsInvalidRequestsWithoutEchoingInput(t *testing.T) {
	s := &Server{}
	for _, body := range []string{
		`{`, `{"token":5}`, `{"token":"short"}`, `{"token":"invalid sensitive token"}`,
		`{"sensitive-secret-as-key":"value"}`, `{"token":"` + strings.Repeat("s", 3000) + `"}`,
		`{"token":"abcdefgh"} {"token":"second-body-secret"}`,
	} {
		w := httptest.NewRecorder()
		s.searchTokens(w, httptest.NewRequest("POST", "/api/v1/admin/tokens/search", strings.NewReader(body)))
		if w.Code != 400 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("invalid input accepted: %d", w.Code)
		}
		for _, secret := range []string{"invalid sensitive token", "sensitive-secret-as-key", "second-body-secret", strings.Repeat("s", 16)} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("search input echoed in error")
			}
		}
	}
}

func TestTokenLookupRoutesRequireAdminSession(t *testing.T) {
	s := brokerDataTestServer(t)
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/v1/admin/tokens/search"}, {"GET", "/api/v1/admin/tokens/00000000-0000-4000-8000-000000000001"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"token":"abcdefgh"}`))
		r.Header.Set("Authorization", "Bearer broker-token-is-not-admin-session")
		s.Handler().ServeHTTP(w, r)
		if w.Code != 401 {
			t.Fatalf("token lookup exposed without admin auth: %d", w.Code)
		}
	}
}

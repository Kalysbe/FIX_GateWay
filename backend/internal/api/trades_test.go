package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminTradeDetailRequiresAuthentication(t *testing.T) {
	s := brokerDataTestServer(t)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/trades/00000000-0000-0000-0000-000000000001", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated trade detail returned %d", w.Code)
	}
}

func TestAdminTradeDetailRejectsInvalidID(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/v1/admin/trades/not-a-uuid", nil)
	r.SetPathValue("id", "not-a-uuid")
	w := httptest.NewRecorder()
	(&Server{}).getAdminTrade(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid execution id returned %d", w.Code)
	}
}

package api

import (
	"context"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitAdministrationDeniedBeforeMutation(t *testing.T) {
	s := &Server{}
	for _, role := range []string{auth.RoleReadOnly, auth.RoleAuditor, auth.RoleOperator} {
		r := httptest.NewRequest("PUT", "/api/v1/admin/rate-limits", nil)
		ctx := context.WithValue(r.Context(), ctxAdmin, AdminPrincipal{RoleCode: role})
		w := httptest.NewRecorder()
		s.requireAdminRole(auth.RolesWrite...)(http.HandlerFunc(s.updateRateLimit)).ServeHTTP(w, r.WithContext(ctx))
		if w.Code != 403 {
			t.Fatal(role, w.Code)
		}
	}
	r := httptest.NewRequest("PUT", "/api/v1/admin/rate-limits", nil)
	w := httptest.NewRecorder()
	s.requireAdminRole(auth.RolesWrite...)(http.HandlerFunc(s.updateRateLimit)).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestBrokerAdmissionSharedTokensAndSeparateSystem(t *testing.T) {
	a := ratelimit.NewAdmission()
	a.Replace([]ratelimit.Policy{{Scope: "broker", Subject: "1", Operation: "Queries", Rate: 1, Burst: 1, Concurrent: 1, Enabled: true}, {Scope: "broker", Subject: "1", Operation: "System", Rate: 1, Burst: 1, Concurrent: 1, Enabled: true}})
	s := &Server{ratePolicies: &ratelimit.Store{Admission: a}}
	handler := s.admitBroker(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	call := func(path string) int {
		r := httptest.NewRequest("GET", path, nil)
		ctx := context.WithValue(r.Context(), ctxBroker, domain.Broker{ID: 1})
		ctx = context.WithValue(ctx, ctxTokenID, uuid.New())
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r.WithContext(ctx))
		if rec.Code == 429 && rec.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry")
		}
		return rec.Code
	}
	if call("/api/v1/orders") != 204 || call("/api/v1/orders") != 429 || call("/api/v1/firm") != 204 {
		t.Fatal("token sharing/system isolation")
	}
}
func TestRateOperationUsesBoundedNames(t *testing.T) {
	for _, path := range []string{"/api/v1/orders/random1", "/api/v1/orders/random2"} {
		category, op := rateOperation(httptest.NewRequest("GET", path, nil))
		if category != "Queries" || op != "GetOrder" {
			t.Fatal(category, op)
		}
	}
}

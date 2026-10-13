package api

import (
	"context"
	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/config"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnsupportedExecutionSemanticsAreRetained(t *testing.T) {
	s := &Server{}
	for _, report := range []adapters.ExecutionReport{{ExecType: "G"}, {ExecType: "H"}, {ExecType: "5"}, {ExecTransType: "2"}, {ProtocolReject: true}, {LastQty: "-1"}, {LastPx: "garbage"}} {
		if err := s.OnExecution(context.Background(), report); err == nil {
			t.Fatalf("unsupported or malformed report accepted: %+v", report)
		}
	}
}

func TestProductionOperationsHealthAndPublicHTTPS(t *testing.T) {
	s := NewServer(config.Config{AppEnv: "production", RequireHTTPS: true}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	for _, path := range []string{"/health", "/metrics"} {
		w := httptest.NewRecorder()
		s.OperationsHandler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("private operations %s: %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 403 {
		t.Fatalf("public plaintext accepted: %d", w.Code)
	}
	w = httptest.NewRecorder()
	s.OperationsHandler().ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/orders", nil))
	if w.Code != 404 {
		t.Fatalf("operations port exposes trading: %d", w.Code)
	}
}

func TestCORSIncludesPUTForAdminPolicies(t *testing.T) {
	s := NewServer(config.Config{CORSOrigins: []string{"https://console.example"}}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	r := httptest.NewRequest("OPTIONS", "/api/v1/admin/trading-limits", nil)
	r.Header.Set("Origin", "https://console.example")
	r.Header.Set("Access-Control-Request-Method", "PUT")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 204 || !strings.Contains(","+w.Header().Get("Access-Control-Allow-Methods")+",", ",PUT,") {
		t.Fatalf("PUT preflight: %d %s", w.Code, w.Header().Get("Access-Control-Allow-Methods"))
	}
}

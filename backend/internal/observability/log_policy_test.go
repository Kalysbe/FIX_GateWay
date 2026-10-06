package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPLogPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, method, path string
		status, count      int
	}{
		{"admin polling", "GET", "/api/v1/admin/dashboard", 200, 0},
		{"admin head", "HEAD", "/api/v1/admin/orders", 200, 0},
		{"health", "GET", "/health", 200, 0},
		{"preflight", "OPTIONS", "/api/v1/orders", 204, 0},
		{"broker statistics", "GET", "/api/v1/orders", 200, 1},
		{"create order", "POST", "/api/v1/orders", 201, 1},
		{"admin mutation", "PATCH", "/api/v1/admin/brokers/1", 200, 1},
		{"failed polling", "GET", "/api/v1/admin/dashboard", 500, 2},
		{"failed health", "GET", "/health", 503, 2},
		{"denied read", "GET", "/api/v1/admin/orders", 403, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPipeline(nil, nil, nil, Settings{ChannelSize: 8})
			h := p.HTTPMiddleware(HTTPConfig{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(tc.method, tc.path, nil))
			if got := len(p.ch); got != tc.count {
				t.Fatalf("queued %d events, want %d", got, tc.count)
			}
		})
	}
}

func TestEventLogPolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event Event
		keep  bool
	}{
		{"token auth success", Event{Type: TypeAuth, Subtype: AuthSuccess}, false},
		{"admin login", Event{Type: TypeAuth, Subtype: AuthSuccess, ActorType: "admin"}, true},
		{"auth failure", Event{Type: TypeAuth, Subtype: AuthFailed}, true},
		{"audit", Event{Type: TypeAudit}, true},
		{"business", Event{Type: TypeBusiness, Subtype: BizOrderFilled}, true},
		{"routine FIX", Event{Type: TypeFIX, Subtype: "FIX_MESSAGE_SENT"}, false},
		{"order FIX", Event{Type: TypeFIX, Subtype: "FIX_ORDER_SENT"}, true},
		{"session", Event{Type: TypeFIX, Subtype: "FIX_SESSION_CONNECTED"}, true},
		{"slow polling", Event{Type: TypeAPI, HTTPMethod: "GET", Endpoint: "/api/v1/admin/dashboard", PerfClass: PerfSlow}, true},
		{"annotated failure", Event{Type: TypeAPI, HTTPMethod: "GET", Endpoint: "/api/v1/admin/dashboard", Result: ResultFailure}, true},
		{"warning", Event{Type: TypeSystem, Severity: SeverityWarning}, true},
		{"debug", Event{Type: TypeSystem, Severity: SeverityDebug}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPipeline(nil, nil, nil, Settings{ChannelSize: 8})
			p.Emit(tc.event)
			if got := len(p.ch) == 1; got != tc.keep {
				t.Fatalf("kept=%v, want %v", got, tc.keep)
			}
		})
	}
}

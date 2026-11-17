package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSwaggerUsesLocalScripts(t *testing.T) {
	s := &Server{}
	w := httptest.NewRecorder()
	s.handleBrokerSwaggerUI(w, httptest.NewRequest("GET", "/broker-swagger", nil))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `src="/docs/swagger/init.js"`) || !strings.Contains(body, `href="/docs/swagger/swagger-ui.css"`) {
		t.Fatal("missing local assets")
	}
	if strings.Contains(body, "unpkg.com") || strings.Contains(body, "<script>") {
		t.Fatal("Swagger violates script-src self")
	}
}

func TestOnlyBrokerSwaggerIsServed(t *testing.T) {
	handler := brokerDataTestServer(t).Handler()
	for _, path := range []string{"/swagger", "/api/openapi.yaml"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 404 {
			t.Errorf("%s: got %d, want 404", path, w.Code)
		}
	}
}

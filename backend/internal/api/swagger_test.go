package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSwaggerUsesLocalScripts(t *testing.T) {
	s := &Server{}
	for _, path := range []string{"/swagger", "/broker-swagger"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		if path == "/swagger" {
			s.handleSwaggerUI(w, r)
		} else {
			s.handleBrokerSwaggerUI(w, r)
		}
		body := w.Body.String()
		if w.Code != 200 || !strings.Contains(body, `src="/docs/swagger/init.js"`) || !strings.Contains(body, `href="/docs/swagger/swagger-ui.css"`) {
			t.Fatal("missing local assets", path)
		}
		if strings.Contains(body, "unpkg.com") || strings.Contains(body, "<script>") {
			t.Fatal("Swagger violates script-src self", path)
		}
	}
}

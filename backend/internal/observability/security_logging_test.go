package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPAuditCannotBeSuppressedByHeaders(t *testing.T) {
	for _, header := range []string{"Host", "X-Forwarded-Host", "Origin", "Referer"} {
		t.Run(header, func(t *testing.T) {
			p := NewPipeline(nil, nil, nil, Settings{ChannelSize: 8})
			r := httptest.NewRequest("POST", "http://gateway.example.test/api/v1/orders", nil)
			if header == "Host" {
				r.Host = "localhost:5173"
			} else if header == "X-Forwarded-Host" {
				r.Header.Set(header, "localhost:5173")
			} else {
				r.Header.Set(header, "http://localhost:5173")
			}
			p.HTTPMiddleware(HTTPConfig{SkipOrigins: []string{"http://localhost:5173"}}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401) })).ServeHTTP(httptest.NewRecorder(), r)
			select {
			case event := <-p.ch:
				if event.Endpoint != "/api/v1/orders" {
					t.Fatal(event.Endpoint)
				}
			default:
				t.Fatal("HTTP audit missing")
			}
		})
	}
}

func TestEncodedQueryCredentialsRedacted(t *testing.T) {
	for _, raw := range []string{"ticket=private", "%74icket=private", "PASSWORD=private", "api%5Ftoken=private"} {
		if got := RedactQuery(raw); got == raw {
			t.Fatalf("secret retained: %s", got)
		}
	}
}

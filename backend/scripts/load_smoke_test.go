package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSmokeRejectsFailedAndIdempotentResponses(t *testing.T) {
	for _, code := range []int{http.StatusCreated, http.StatusOK, http.StatusBadRequest, http.StatusInternalServerError} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/orders" || r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
				}
				w.WriteHeader(code)
			}))
			defer server.Close()
			err := createSmokeOrder(server.Client(), server.URL, "test-token", []byte(`{"client_code":"006"}`))
			if (err == nil) != (code == http.StatusCreated) {
				t.Fatalf("HTTP %d returned %v", code, err)
			}
		})
	}
}

func TestSmokeRejectsInvalidURL(t *testing.T) {
	for _, raw := range []string{"", "localhost:8080", "ftp://example.com", "http://example.com?token=secret", "http://u:p@example.com"} {
		if _, err := normalizeGatewayURL(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if normalized, err := normalizeGatewayURL("http://localhost:8080/"); err != nil || normalized != "http://localhost:8080" {
		t.Fatalf("normalized=%s err=%v", normalized, err)
	}
}

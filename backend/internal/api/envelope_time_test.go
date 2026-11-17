package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Broker replies give dates and times in one readable form (поручение
// пользователя 17.11.2026); the admin API keeps the stored values.
func TestBrokerEnvelopeFormatsDatesAndTimes(t *testing.T) {
	row := `[{"TradeDate":"2026-11-17T00:00:00","OrderDateTime":"2026-11-17T15:54:45","time":"1900-01-01T16:40:15","Price":94.99000000}]`
	handler := (&Server{}).brokerEnvelope(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(row))
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/orders", nil))
	want := `"data":[{"TradeDate":"17.11.2026","OrderDateTime":"17.11.2026 15:54:45","time":"16:40:15","Price":94.99000000}]`
	if !strings.Contains(w.Body.String(), want) {
		t.Fatal("broker reply:", w.Body.String())
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/admin/orders", nil))
	if w.Body.String() != row {
		t.Fatal("admin reply changed:", w.Body.String())
	}
}

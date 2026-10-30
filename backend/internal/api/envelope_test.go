package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// unwrapBrokerData returns the data of a successful broker reply, so tests can
// compare payloads; other replies are returned unchanged.
func unwrapBrokerData(path string, body []byte) []byte {
	if !brokerPath(strings.SplitN(path, "?", 2)[0]) {
		return body
	}
	var env brokerEnvelope
	if json.Unmarshal(body, &env) != nil || !env.Success {
		return body
	}
	return env.Data
}

func serveEnveloped(h http.HandlerFunc, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	(&Server{}).brokerEnvelope(h).ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestBrokerEnvelopeShapes(t *testing.T) {
	row := `{"OrderNum":9007199254740993123,"Price":120.00000000,"FirmName":"A&B"}`
	for _, tc := range []struct {
		name string
		h    http.HandlerFunc
		code int
		want string
	}{
		{"list keeps raw rows and paging", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Has-More", "true")
			w.Header().Set("X-Next-Offset", "200")
			_, _ = w.Write([]byte("[" + row + "]\n"))
		}, 200, `{"success":true,"code":"OK","message":null,"error_code":null,"data":[` + row + `],"has_more":true,"next_offset":200}`},
		{"created order", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 201, map[string]any{"OrderNum": json.Number("5"), "id": "x"})
		}, 201, `{"success":true,"code":"OK","message":null,"error_code":null,"data":{"OrderNum":5,"id":"x"}}`},
		{"QUIK reject keeps the order as data", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, 422, map[string]any{"code": "ORDER_REJECTED", "error": "No right", "error_code": json.Number("8637225"), "State": "Отклонена"})
		}, 422, `{"success":false,"code":"ORDER_REJECTED","message":"No right","error_code":8637225,"data":{"State":"Отклонена"}}`},
		{"plain error", func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, 401, "invalid token")
		}, 401, `{"success":false,"code":"UNAUTHORIZED","message":"invalid token","error_code":null,"data":null}`},
		{"non-JSON error", func(w http.ResponseWriter, r *http.Request) {
			http.NotFound(w, r)
		}, 404, `{"success":false,"code":"NOT_FOUND","message":"404 page not found","error_code":null,"data":null}`},
	} {
		w := serveEnveloped(tc.h, "/api/v1/orders")
		if w.Code != tc.code || strings.TrimSpace(w.Body.String()) != tc.want || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
}

// Admin routes keep their own replies.
func TestBrokerEnvelopeSkipsAdmin(t *testing.T) {
	w := serveEnveloped(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"brokers": []any{}}) }, "/api/v1/admin/brokers")
	if strings.TrimSpace(w.Body.String()) != `{"brokers":[]}` {
		t.Fatal(w.Body.String())
	}
}

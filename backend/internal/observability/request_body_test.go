package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func runWithBody(t *testing.T, path, body string, status int) []Event {
	t.Helper()
	p := NewPipeline(nil, nil, nil, Settings{ChannelSize: 8})
	h := p.HTTPMiddleware(HTTPConfig{}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", path, strings.NewReader(body)))
	var out []Event
	for len(p.ch) > 0 {
		out = append(out, <-p.ch)
	}
	return out
}

func TestFailedBrokerRequestKeepsSentJSON(t *testing.T) {
	sent := `{ "account_id": "SENTI01", "Price": 100.50, "Qty": 10 }`
	events := runWithBody(t, "/api/v1/orders", sent, 422)
	if len(events) != 2 {
		t.Fatalf("events %d", len(events))
	}
	want := `{"account_id":"SENTI01","Price":100.50,"Qty":10}`
	for _, e := range events {
		if e.RequestBody != want {
			t.Fatalf("%s request_body %s", e.Type, e.RequestBody)
		}
	}
}

func TestRequestBodyNotKept(t *testing.T) {
	if ev := runWithBody(t, "/api/v1/orders", `{"Qty":1}`, 201); len(ev) == 1 && ev[0].RequestBody != "" {
		t.Fatalf("success kept body %s", ev[0].RequestBody)
	}
	for _, e := range runWithBody(t, "/api/v1/admin/login", `{"password":"x"}`, 401) {
		if e.RequestBody != "" {
			t.Fatalf("admin body kept %s", e.RequestBody)
		}
	}
}

func TestRequestBodyMasksSecretsAndKeepsInvalidJSON(t *testing.T) {
	ev := runWithBody(t, "/api/v1/orders", `{"Qty":1,"api_key":"abc"}`, 400)
	if got := ev[0].RequestBody; got != `{"Qty":1,"api_key":"***"}` {
		t.Fatal(got)
	}
	ev = runWithBody(t, "/api/v1/orders", `{"Qty":`, 400)
	if got := ev[0].RequestBody; got != `{"Qty":` {
		t.Fatal(got)
	}
}

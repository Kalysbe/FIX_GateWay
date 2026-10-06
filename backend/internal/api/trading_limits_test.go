package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTradingPolicyRequiresExplicitPauseAndLimits(t *testing.T) {
	// A partial edit must not implicitly lift a pause or erase a numeric limit.
	for _, body := range []string{
		`{"broker_id":0,"max_quantity":null,"max_open_orders":null,"revision":1,"reason":"edit"}`,
		`{"broker_id":0,"paused":false,"max_open_orders":null,"revision":1,"reason":"edit"}`,
		`{"broker_id":0,"paused":false,"max_quantity":null,"revision":1,"reason":"edit"}`,
		`{"broker_id":0,"paused":false,"max_quantity":null,"max_open_orders":null,"reason":"edit"}`,
		`{"broker_id":0,"paused":false,"max_quantity":"oops","max_open_orders":null,"revision":1,"reason":"edit"}`,
	} {
		rr := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/api/v1/admin/trading-limits", strings.NewReader(body))
		(&Server{}).updateTradingLimit(rr, r)
		if rr.Code != 400 {
			t.Fatalf("partial or invalid policy accepted: %s -> %d", body, rr.Code)
		}
	}
}

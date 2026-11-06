package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/quikexport"
)

type testMarketTrades struct {
	filter quikexport.MarketTradeFilter
	calls  int
	err    error
}

const marketTradeRow = `{"TradeNum":26110500000001,"ClassCode":"LIST_GBA","SecCode":"GBA1","TradeDateTime":"2026-11-05T10:41:45.000+06:00","Price":91.71000000,"Qty":1000.00000000,"SettleCurrency":"KGS","Kind":"Обычная","CanceledDateTime":null}`

func (s *testMarketTrades) ListMarketTrades(_ context.Context, f quikexport.MarketTradeFilter) (quikexport.TradePage, error) {
	s.filter, s.calls = f, s.calls+1
	return quikexport.TradePage{Trades: []quikexport.Order{{Raw: json.RawMessage(marketTradeRow)}}, HasMore: true, NextOffset: f.Offset + 1}, s.err
}

// Market trades take an instrument and a period (KSE dates or RFC 3339 times);
// bad bounds never reach QUIK.
func TestMarketTradesEndpoint(t *testing.T) {
	src := &testMarketTrades{}
	s := &Server{quikMarketTrades: src}
	w := httptest.NewRecorder()
	s.listMarketTrades(w, httptest.NewRequest("GET", "/api/v1/market-data/trades?board=+LIST_GBA+&symbol=GBA1&from=2026-11-01&to=2026-11-05&limit=50&offset=5", nil))
	f := src.filter
	if w.Code != 200 || f.ClassCode != "LIST_GBA" || f.SecCode != "GBA1" || f.Limit != 50 || f.Offset != 5 {
		t.Fatal(w.Code, f, w.Body.String())
	}
	if !f.From.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, quikexport.KSETime)) || !f.Before.Equal(time.Date(2026, 11, 6, 0, 0, 0, 0, quikexport.KSETime)) {
		t.Fatal("dates are whole KSE days, to inclusive", f.From, f.Before)
	}
	if strings.TrimSpace(w.Body.String()) != "["+marketTradeRow+"]" || w.Header().Get("X-Has-More") != "true" || w.Header().Get("X-Next-Offset") != "6" {
		t.Fatal("rows as read and paging", w.Body.String(), w.Header())
	}
	w = httptest.NewRecorder()
	s.listMarketTrades(w, httptest.NewRequest("GET", "/api/v1/market-data/trades?from=2026-11-05T04:41:45Z", nil))
	if f := src.filter; w.Code != 200 || f.ClassCode != "" || f.Before != nil || !f.From.Equal(time.Date(2026, 11, 5, 10, 41, 45, 0, quikexport.KSETime)) || f.Limit != 200 {
		t.Fatal("time bound, no instrument", w.Code, f)
	}
	for _, path := range []string{"?from=05.11.2026", "?to=2026-13-01", "?from=2026-11-05&to=2026-11-04", "?from=2026-11-05T10:00:00%2B06:00&to=2026-11-05T09:00:00%2B06:00", "?limit=0"} {
		calls := src.calls
		w = httptest.NewRecorder()
		s.listMarketTrades(w, httptest.NewRequest("GET", "/api/v1/market-data/trades"+path, nil))
		if w.Code != http.StatusBadRequest || src.calls != calls {
			t.Fatal(path, "must be rejected before reading", w.Code)
		}
	}
	src.err = errors.New("down")
	w = httptest.NewRecorder()
	s.listMarketTrades(w, httptest.NewRequest("GET", "/api/v1/market-data/trades", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("outage must be 503", w.Code)
	}
	w = httptest.NewRecorder()
	(&Server{}).listMarketTrades(w, httptest.NewRequest("GET", "/api/v1/market-data/trades", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatal("no QUIK source must be 503", w.Code)
	}
}

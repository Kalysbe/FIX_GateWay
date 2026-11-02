package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/quikexport"
)

type testOrderSource struct {
	filter      quikexport.OrderFilter
	tradeFilter quikexport.TradeFilter
	firm        *string
	err    error
	order  quikexport.Order
}

func (s *testOrderSource) ListOrders(_ context.Context, f quikexport.OrderFilter) (quikexport.OrderPage, error) {
	s.filter = f
	return quikexport.OrderPage{Orders: []quikexport.Order{s.order}}, s.err
}
func (s *testOrderSource) GetOrder(_ context.Context, _ string, firm *string) (quikexport.Order, error) {
	s.firm = firm
	return s.order, s.err
}
func (s *testOrderSource) CountOrdersByState(context.Context) (map[string]int, error) {
	return map[string]int{s.order.Text("State"): 1}, s.err
}
func (s *testOrderSource) ListTrades(_ context.Context, f quikexport.TradeFilter) (quikexport.TradePage, error) {
	s.tradeFilter = f
	return quikexport.TradePage{Trades: []quikexport.Order{s.order}, HasMore: true, NextOffset: f.Offset + 1}, s.err
}

// Broker trades are the token firm's raw QUIK Trades rows, paged.
func TestBrokerTradesAreRawQUIKRowsOfTokenFirm(t *testing.T) {
	row := `{"TradeNum":11094,"Operation":"Купля","ClassCode":"LIST_GBA","SecCode":"GBA02280810","Price":99.00000000,"Qty":201,"FirmId":"2040","CPFirmId":"BAKAI"}`
	src := &testOrderSource{order: quikexport.Order{Raw: json.RawMessage(row)}}
	s := &Server{quikOrders: src}
	w := httptest.NewRecorder()
	s.listTrades(w, brokerOrderRequest("/api/v1/trades?limit=5&offset=10"))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "["+row+"]" {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if src.tradeFilter.Firm == nil || *src.tradeFilter.Firm != "2040" || src.tradeFilter.Limit != 5 || src.tradeFilter.Offset != 10 {
		t.Fatalf("filter %+v", src.tradeFilter)
	}
	if w.Header().Get("X-Has-More") != "true" || w.Header().Get("X-Next-Offset") != "11" {
		t.Fatalf("headers %v", w.Header())
	}
	src.err = errors.New("down")
	w = httptest.NewRecorder()
	s.listTrades(w, brokerOrderRequest("/api/v1/trades"))
	if w.Code != 503 {
		t.Fatalf("outage must be 503, got %d", w.Code)
	}
}

func TestQUIKOrderStatsGroupRawStates(t *testing.T) {
	got := quikOrderStats(map[string]int{"Активна": 2, " ИСПОЛНЕНА ": 3, "Отклонена": 1, "Снята": 4})
	want := map[string]int{"total": 10, "active": 2, "filled": 3, "rejected": 1}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s=%d want %d (%v)", k, got[k], v, got)
		}
	}
}

func TestSetQuikExportNilLeavesNoTypedNil(t *testing.T) {
	s := &Server{}
	s.SetQuikExport(nil)
	if s.quikOrders != nil {
		t.Fatal("nil store must leave the reader interface nil")
	}
}

func brokerOrderRequest(path string) *http.Request {
	r := httptest.NewRequest("GET", path, nil)
	ctx := context.WithValue(r.Context(), ctxBroker, domain.Broker{ID: 7, Code: "BROKER"})
	ctx = context.WithValue(ctx, ctxToken, domain.APIToken{QuikFirm: "2040"})
	return r.WithContext(ctx)
}

func TestBrokerOrdersUseAuthenticatedFirmBeforePagination(t *testing.T) {
	raw := `{"OrderNum":9007199254740993123,"FirmId":"2040","State":"Активна","Price":120.00000000,"Extra":null}`
	source := &testOrderSource{order: quikexport.Order{Raw: json.RawMessage(raw)}}
	s := &Server{quikOrders: source}
	r := brokerOrderRequest("/api/v1/orders?firm_id=9999&broker_id=99&limit=1&offset=2")
	w := httptest.NewRecorder()
	s.listBrokerOrders(w, r)
	if w.Code != 200 || source.filter.Firm == nil || *source.filter.Firm != "2040" || source.filter.Limit != 1 || source.filter.Offset != 2 {
		t.Fatal(w.Code, w.Body.String(), source.filter)
	}
	if strings.TrimSpace(w.Body.String()) != "["+raw+"]" {
		t.Fatal("list added or changed fields", w.Body.String())
	}
	if source.filter.State != "" || source.filter.Today {
		t.Fatal("full list must not filter by state or date", source.filter)
	}
	w = httptest.NewRecorder()
	s.listBrokerActiveOrders(w, brokerOrderRequest("/api/v1/orders/active?firm_id=9999&limit=5"))
	if w.Code != 200 || source.filter.State != "Активна" || source.filter.Firm == nil || *source.filter.Firm != "2040" || source.filter.Limit != 5 {
		t.Fatal(w.Code, w.Body.String(), source.filter)
	}
	if strings.TrimSpace(w.Body.String()) != "["+raw+"]" {
		t.Fatal("active list added or changed fields", w.Body.String())
	}
	if !source.filter.Today {
		t.Fatal("broker active orders must be limited to today")
	}
	// Admin quotes: today's active orders of every firm.
	w = httptest.NewRecorder()
	s.listAdminQuotes(w, httptest.NewRequest("GET", "/api/v1/admin/quotes?board=LIST_GBA&symbol=GBA1", nil))
	if f := source.filter; w.Code != 200 || f.State != "Активна" || !f.Today || f.Firm != nil || f.ClassCode != "LIST_GBA" || f.SecCode != "GBA1" {
		t.Fatal(w.Code, w.Body.String(), source.filter)
	}
	// Broker quotes: every firm's rows of one instrument, reduced to side,
	// price, remaining quantity and volume; nothing identifies the participant.
	quote := `{"OrderNum":7412,"FirmId":"SENTI2004","ClientCode":"006","Account":"1-3101-006","Operation":"Продажа","State":"Активна","Price":100.50000000,"Qty":10,"Balance":4,"Value":1005}`
	brokerSource := &testOrderSource{order: quikexport.Order{Raw: json.RawMessage(quote)}}
	bs := &Server{quikOrders: brokerSource}
	w = httptest.NewRecorder()
	bs.listBrokerQuotes(w, brokerOrderRequest("/api/v1/quotes?board=LIST_GBA&symbol=GBA1&firm_id=9999&broker_id=99&limit=3&offset=6"))
	if f := brokerSource.filter; w.Code != 200 || f.State != "Активна" || !f.Today || f.Firm != nil || f.ClassCode != "LIST_GBA" || f.SecCode != "GBA1" || f.Limit != 3 || f.Offset != 6 {
		t.Fatal(w.Code, w.Body.String(), brokerSource.filter)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `[{"side":"SELL","price":100.50000000,"quantity":4,"volume":402}]` {
		t.Fatal("broker quote must carry only side, price, quantity and volume:", got)
	}
	brokerSource.order = quikexport.Order{Raw: json.RawMessage(`{"FirmId":"X","Operation":"?","Price":null,"Balance":3}`)}
	w = httptest.NewRecorder()
	bs.listBrokerQuotes(w, brokerOrderRequest("/api/v1/quotes?board=LIST_GBA&symbol=GBA1"))
	if got := strings.TrimSpace(w.Body.String()); got != `[{"side":null,"price":null,"quantity":3,"volume":null}]` {
		t.Fatal(got)
	}
	for _, path := range []string{"/api/v1/quotes", "/api/v1/quotes?board=LIST_GBA", "/api/v1/quotes?symbol=GBA1", "/api/v1/quotes?board=LIST_GBA&symbol=GBA1&limit=1001"} {
		w = httptest.NewRecorder()
		bs.listBrokerQuotes(w, brokerOrderRequest(path))
		if w.Code != http.StatusBadRequest {
			t.Fatal(path, "must be rejected", w.Code)
		}
	}
	r.SetPathValue("id", "quik-20261022-LIST_GBA-1")
	w = httptest.NewRecorder()
	s.getBrokerOrder(w, r)
	if w.Code != 200 || source.firm == nil || *source.firm != "2040" {
		t.Fatal(w.Code, w.Body.String(), source.firm)
	}
	if strings.TrimSpace(w.Body.String()) != raw {
		t.Fatal("detail added or changed fields", w.Body.String())
	}
	// Empty authenticated scope must fail closed, not become the admin selection.
	r = httptest.NewRequest("GET", "/api/v1/orders", nil)
	r = r.WithContext(context.WithValue(r.Context(), ctxBroker, domain.Broker{ID: 7}))
	w = httptest.NewRecorder()
	s.listBrokerOrders(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestQUIKOrdersNoFallbackOnMissingRowOrOutage(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{{quikexport.ErrOrderNotFound, 404}, {errors.New("upstream unavailable"), 503}} {
		s := &Server{quikOrders: &testOrderSource{err: tc.err}}
		r := brokerOrderRequest("/api/v1/orders/quik-20261022-LIST_GBA-1")
		r.SetPathValue("id", "quik-20261022-LIST_GBA-1")
		w := httptest.NewRecorder()
		s.getBrokerOrder(w, r)
		if w.Code != tc.want {
			t.Fatal(w.Code, w.Body.String())
		}
		w = httptest.NewRecorder()
		s.cancelBrokerOrder(w, r)
		if w.Code != tc.want {
			t.Fatal("cancel attempted without source evidence", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	(&Server{}).listBrokerOrders(w, brokerOrderRequest("/api/v1/orders"))
	if w.Code != 503 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestQUIKOrderReadsValidatePagesAndExternalCancellation(t *testing.T) {
	s := &Server{quikOrders: &testOrderSource{order: quikexport.Order{Raw: json.RawMessage(`{"State":"Активна"}`)}}}
	for _, query := range []string{"limit=0", "limit=1001", "offset=-1", "offset=x"} {
		w := httptest.NewRecorder()
		s.listBrokerOrders(w, brokerOrderRequest("/api/v1/orders?"+query))
		if w.Code != 400 {
			t.Fatal(query, w.Code, w.Body.String())
		}
	}
	r := brokerOrderRequest("/api/v1/orders/quik-20261022-LIST_GBA-1/cancel")
	r.SetPathValue("id", "quik-20261022-LIST_GBA-1")
	w := httptest.NewRecorder()
	s.cancelBrokerOrder(w, r)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "original Gateway") {
		t.Fatal(w.Code, w.Body.String())
	}
}

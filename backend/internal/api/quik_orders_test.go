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
	filter quikexport.OrderFilter
	firm   *string
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

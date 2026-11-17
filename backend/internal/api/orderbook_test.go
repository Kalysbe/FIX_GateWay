package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/quikexport"
)

// The broker order book is read from QUIK Orders for one instrument: board and
// symbol are required, the class must be served, an outage is a 503.
func TestBrokerOrderBookComesFromQUIKOrders(t *testing.T) {
	level := quikexport.BookLevel{Price: json.RawMessage("95.00000000"), Quantity: json.RawMessage("5878.00000000"), Volume: json.RawMessage("558410")}
	src := &testOrderSource{book: quikexport.OrderBook{Bids: []quikexport.BookLevel{}, Offers: []quikexport.BookLevel{level}}}
	s := &Server{quikOrders: src}
	w := httptest.NewRecorder()
	s.listBrokerOrderBook(w, brokerOrderRequest("/api/v1/orderbook?board=LIST_GBA&symbol=GBA02270915"))
	want := `{"board":"LIST_GBA","symbol":"GBA02270915","bids":[],"offers":[{"price":95.00000000,"quantity":5878.00000000,"volume":558410}]}`
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != want {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if strings.Join(src.bookArgs, "|") != "LIST_GBA|GBA02270915|"+quikActiveState {
		t.Fatalf("args %v", src.bookArgs)
	}

	for _, path := range []string{"/api/v1/orderbook", "/api/v1/orderbook?board=LIST_GBA", "/api/v1/orderbook?symbol=GBA1"} {
		w = httptest.NewRecorder()
		s.listBrokerOrderBook(w, brokerOrderRequest(path))
		if w.Code != 400 {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}

	s.servedClasses = func(context.Context) ([]string, error) { return []string{"QME_FX"}, nil }
	src.bookArgs = nil
	w = httptest.NewRecorder()
	s.listBrokerOrderBook(w, brokerOrderRequest("/api/v1/orderbook?board=LIST_GBA&symbol=GBA1"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"bids":[],"offers":[]`) || src.bookArgs != nil {
		t.Fatalf("class outside the served ones: %d %s %v", w.Code, w.Body.String(), src.bookArgs)
	}

	s.servedClasses = nil
	src.err = errors.New("down")
	w = httptest.NewRecorder()
	s.listBrokerOrderBook(w, brokerOrderRequest("/api/v1/orderbook?board=LIST_GBA&symbol=GBA1"))
	if w.Code != 503 {
		t.Fatalf("outage must be 503, got %d", w.Code)
	}
}

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

func TestBrokerQuotesLimitedToServedClasses(t *testing.T) {
	quote := `{"Operation":"Продажа","Price":100,"Balance":4}`
	source := &testOrderSource{order: quikexport.Order{Raw: json.RawMessage(quote)}}
	s := &Server{quikOrders: source, servedClasses: func(context.Context) ([]string, error) {
		return []string{"LIST_GBA", "LIST_GD"}, nil
	}}

	w := httptest.NewRecorder()
	s.listBrokerQuotes(w, brokerOrderRequest("/api/v1/quotes?board=LIST_GBA&symbol=GBA1"))
	if w.Code != 200 || source.filter.ClassCode != "LIST_GBA" || strings.TrimSpace(w.Body.String()) == "[]" {
		t.Fatal("served class must be read", w.Code, w.Body.String(), source.filter)
	}

	source.filter = quikexport.OrderFilter{}
	w = httptest.NewRecorder()
	s.listBrokerQuotes(w, brokerOrderRequest("/api/v1/quotes?board=QME_FX&symbol=USD&offset=5"))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" || source.filter.ClassCode != "" {
		t.Fatal("other class must be an empty page without reading Orders", w.Code, w.Body.String(), source.filter)
	}
	if w.Header().Get("X-Has-More") != "false" || w.Header().Get("X-Next-Offset") != "5" {
		t.Fatal("empty page headers", w.Header())
	}

	w = httptest.NewRecorder()
	s.listBrokerQuotes(w, brokerOrderRequest("/api/v1/quotes?board=QME_FX&symbol=USD&limit=1001"))
	if w.Code != 400 {
		t.Fatal("a bad page of another class is still rejected", w.Code)
	}

	s.servedClasses = func(context.Context) ([]string, error) { return nil, errors.New("settings down") }
	w = httptest.NewRecorder()
	s.listBrokerQuotes(w, brokerOrderRequest("/api/v1/quotes?board=LIST_GBA&symbol=GBA1"))
	if w.Code != 503 {
		t.Fatal("unreadable settings must not widen access", w.Code)
	}
}

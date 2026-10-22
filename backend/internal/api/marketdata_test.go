package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokerdata"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jackc/pgx/v5/pgxpool"
)

type testMarketDataReader struct {
	quotes    []domain.MarketQuote
	state     domain.MarketDataSubscription
	available bool
}

func (s *testMarketDataReader) LiveQuotes() (string, []domain.MarketQuote, bool) {
	return s.state.AdapterID, s.quotes, s.available
}
func (s *testMarketDataReader) MarketDataSubscription() domain.MarketDataSubscription { return s.state }

func brokerDataTestServer(t *testing.T) *Server {
	t.Helper()
	// Auth rejection writes audit records. A closed pool makes those writes
	// fail immediately without opening a database connection in unit tests.
	pool, err := pgxpool.New(context.Background(), "postgres://test@localhost/unused?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	return &Server{
		audit: audit.NewStore(pool), brokers: brokers.NewStore(pool, "test"),
		authFailLimiter: ratelimit.NewSlidingWindow(time.Minute, 100),
	}
}

func TestBrokerMarketDataSnapshot(t *testing.T) {
	source := &testMarketDataReader{
		available: true,
		state:     domain.MarketDataSubscription{AdapterID: "a1", Status: "ACTIVE", Expected: 3, Confirmed: 3, Error: "private adapter error"},
		quotes: []domain.MarketQuote{
			{AdapterID: "private-adapter", MDReqID: "private-request", Symbol: "TEST", SecurityID: "ISIN1", Board: "A", Currency: "KGS", Name: "Test instrument", Last: "123.4567890123456789", Open: "120", High: "125", Low: "119", Close: "122", WAPR: "123", Volume: "1000", Value: "123456.7890123456789", TradingStatus: "17", TradingSessionID: "MAIN", Bids: []domain.BookLevel{{Price: "123", Size: "10"}}, Offers: []domain.BookLevel{{Price: "124", Size: "11"}}, Entries: map[string]string{"private-tag": "private-value"}, RawFIX: "private wire message", UpdatedAt: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)},
			{Symbol: "TEST", SecurityID: "ISIN1", Board: "B"},
			{Symbol: "OTHER", SecurityID: "ISIN2", Board: "A"},
		},
	}
	for _, tc := range []struct {
		name, board, symbol string
		count               int
	}{
		{"all", "", "", 3}, {"symbol", "", "TEST", 2},
		{"security_id_and_board", "A", "ISIN1", 1}, {"unknown", "A", "MISSING", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, available := brokerMarketDataSnapshot(source, tc.board, tc.symbol)
			if !available || response.Quotes == nil || len(response.Quotes) != tc.count {
				t.Fatal("incorrect snapshot", response)
			}
			if source.state.Expected != 3 {
				t.Fatal("REST filter changed the global subscription")
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var envelope map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &envelope); err != nil || len(envelope) != 1 || envelope["quotes"] == nil {
				t.Fatalf("REST must contain only quotes: %s", encoded)
			}
			for _, internal := range []string{"adapter_id", "md_req_id", "raw_fix", "entries", "subscription", "private"} {
				if strings.Contains(string(encoded), internal) {
					t.Fatalf("internal market data exposed: %s", encoded)
				}
			}
			if tc.count > 0 && response.Quotes[0].Last != source.quotes[0].Last {
				t.Fatal("decimal precision lost")
			}
		})
	}
	if source.quotes[0].RawFIX == "" {
		t.Fatal("adapter snapshot mutated")
	}
	snapshot, _ := brokerMarketDataSnapshot(source, "A", "TEST")
	quote := snapshot.Quotes[0]
	if quote.Currency != "KGS" || quote.Name != "Test instrument" || quote.TradingSessionID != "MAIN" ||
		quote.UpdatedAt != source.quotes[0].UpdatedAt || len(quote.Bids) != 1 || quote.Offers[0].Price != "124" {
		t.Fatal("business market data lost", quote)
	}
	for _, state := range []string{"STARTING", "RETRYING", "OFFLINE", "PARTIAL"} {
		source.state.Status = state
		response, available := brokerMarketDataSnapshot(source, "", "")
		if available != (state == "PARTIAL") || !available && len(response.Quotes) != 0 {
			t.Fatal("unavailable feed exposed as a successful snapshot", response, available)
		}
	}
	source.available = false
	if response, available := brokerMarketDataSnapshot(source, "", ""); available || len(response.Quotes) != 0 {
		t.Fatal("unavailable feed exposed stale quotes", response)
	}
	if response, available := brokerMarketDataSnapshot(nil, "", ""); available || response.Quotes == nil || len(response.Quotes) != 0 {
		t.Fatal(response)
	}
}

func TestBrokerMarketDataRequiresOrdersRead(t *testing.T) {
	s := brokerDataTestServer(t)
	handler := s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerMarketData))
	for _, tc := range []struct {
		permissions []string
		status      int
	}{
		{nil, 403}, {[]string{auth.PermTradesRead}, 403}, {[]string{auth.PermOrdersRead}, 503},
	} {
		r := httptest.NewRequest("GET", "/api/v1/market-data", nil)
		r = r.WithContext(context.WithValue(r.Context(), ctxPermissions, tc.permissions))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("permissions %v: %d", tc.permissions, w.Code)
		}
		if w.Code == 503 && (w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), `"error":"market data unavailable"`)) {
			t.Fatal("missing unavailable response or retry header", w.Body.String())
		}
	}
}

func TestBrokerGRPCChannelsHaveRESTAndSwagger(t *testing.T) {
	t.Chdir("../..")
	s := brokerDataTestServer(t)
	handler := s.Handler()
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/broker-openapi.yaml", nil))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	doc, err := openapi3.NewLoader().LoadFromData(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/openapi.yaml", nil))
	general, err := openapi3.NewLoader().LoadFromData(w.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if err := general.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if item := general.Paths.Value("/api/v1/market-data"); item == nil || item.Get == nil {
		t.Fatal("market data missing from general Swagger")
	}
	sources := map[string]string{
		"order.updated": "/api/v1/orders", "trade.created": "/api/v1/trades",
		"positions.updated": "/api/v1/positions", "marketdata.tick": "/api/v1/market-data",
	}
	hub := realtime.NewHub()
	sub, err := hub.SubscribeBroker(nil, realtime.ConnectionInfo{BrokerID: 1}, auth.ReadOnlyPermissions)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	for _, topic := range sub.Topics() {
		path, ok := sources[topic]
		if !ok {
			t.Fatalf("gRPC topic %s has no REST source", topic)
		}
		item := doc.Paths.Value(path)
		if item == nil || item.Get == nil {
			t.Fatalf("%s missing from broker-swagger", path)
		}
		operation := item.Get
		if operation.Extensions["x-grpc-event"] != topic {
			t.Fatalf("%s missing gRPC correspondence", path)
		}
		response := operation.Responses.Status(200)
		if response == nil || response.Value.Content.Get("application/json") == nil || response.Value.Content.Get("application/json").Schema == nil {
			t.Fatalf("%s missing response schema", path)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 401 {
			t.Fatalf("%s not registered or lacks broker authentication: %d", path, w.Code)
		}
	}
	for path := range doc.Paths.Map() {
		if strings.Contains(path, "/admin/") {
			t.Fatalf("admin method exposed in broker-swagger: %s", path)
		}
	}
	for name, fields := range map[string]map[string]any{"Order": orderDTO(domain.Order{}), "Trade": tradeDTO(domain.Trade{})} {
		for field := range fields {
			if doc.Components.Schemas[name].Value.Properties[field] == nil {
				t.Errorf("%s.%s missing from broker-swagger", name, field)
			}
		}
	}
	for name, value := range map[string]any{
		"MarketQuote": brokerdata.MarketQuote{},
		"Position":    domain.FIXPosition{}, "MarketDataResponse": brokerMarketDataResponse{},
	} {
		typeOf := reflect.TypeOf(value)
		for i := 0; i < typeOf.NumField(); i++ {
			field := strings.Split(typeOf.Field(i).Tag.Get("json"), ",")[0]
			if field == "" || field == "-" {
				continue
			}
			if doc.Components.Schemas[name].Value.Properties[field] == nil {
				t.Errorf("%s.%s missing from broker-swagger", name, field)
			}
		}
	}
	for _, contract := range []*openapi3.T{doc, general} {
		for _, field := range []string{"adapter_id", "md_req_id", "raw_fix", "entries"} {
			if contract.Components.Schemas["MarketQuote"].Value.Properties[field] != nil {
				t.Errorf("internal MarketQuote.%s documented for brokers", field)
			}
		}
		properties := contract.Components.Schemas["MarketDataResponse"].Value.Properties
		if len(properties) != 1 || properties["quotes"] == nil {
			t.Error("REST market data schema must contain only quotes")
		}
	}
}

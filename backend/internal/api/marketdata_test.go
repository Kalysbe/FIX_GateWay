package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func TestBrokerMarketDataRequiresOrdersRead(t *testing.T) {
	s := brokerDataTestServer(t)
	handler := s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listMarketData))
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
	if doc.Paths.Value("/ready") != nil {
		t.Fatal("/ready exposed in broker-swagger")
	}
	for path := range doc.Paths.Map() {
		if strings.Contains(path, "/admin/") {
			t.Fatalf("admin method exposed in broker-swagger: %s", path)
		}
	}
	for name, fields := range map[string]map[string]any{"Order": orderReply(domain.Order{})} {
		for field := range fields {
			if doc.Components.Schemas[name].Value.Properties[field] == nil {
				t.Errorf("%s.%s missing from broker-swagger", name, field)
			}
		}
	}
	if item := doc.Paths.Value("/api/v1/market-data/{board}/{symbol}"); item == nil || item.Get == nil {
		t.Error("market data card missing from broker-swagger")
	}
	// Swagger UI types an example into its field, which would narrow the default
	// request: without values an optional filter must return everything.
	for _, path := range []string{"/api/v1/market-data", "/api/v1/orderbook"} {
		for _, param := range doc.Paths.Value(path).Get.Parameters {
			if param.Value.Required {
				continue
			}
			if param.Value.Example != nil || param.Value.Schema.Value.Example != nil {
				t.Errorf("%s: optional filter %s prefilled in Swagger", path, param.Value.Name)
			}
		}
	}
	// Params rows are passed through: the contract is an open object, not a
	// Gateway field list.
	row := doc.Components.Schemas["MarketDataRow"]
	if row == nil || row.Value.AdditionalProperties.Has == nil || !*row.Value.AdditionalProperties.Has {
		t.Error("MarketDataRow must be an open object of Params columns")
	}
	for _, name := range []string{"MarketQuote", "MarketDataResponse"} {
		if doc.Components.Schemas[name] != nil {
			t.Errorf("FIX schema %s still documented", name)
		}
	}
}

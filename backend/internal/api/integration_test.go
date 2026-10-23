//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/api"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/logx"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/routing"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestOrderIdempotencyIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg := config.Load()
	cfg.DatabaseURL = dsn
	cfg.MigrationsPath = "../../migrations"
	if err := migrate.Up(dsn, cfg.MigrationsPath); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	log := logx.New()
	users := admin.NewUserStore(pool)
	_ = users.EnsureAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword)
	brokerStore := brokers.NewStore(pool, cfg.APITokenPepper)
	orderStore := orders.NewStore(pool)
	auditStore := audit.NewStore(pool)
	orderSvc := orders.NewService(orderStore, brokerStore, auditStore)
	adapterStore := adapters.NewStore(pool)
	outboxStore := outbox.NewStore(pool)
	hub := realtime.NewHub()
	manager := adapters.NewManager(adapterStore, log, routing.New(cfg.LBStrategy), nil, t.TempDir(), time.Second)
	srv := api.NewServer(cfg, log, pool, brokerStore, orderSvc, orderStore, adapterStore, manager, outboxStore, auditStore, users, hub, nil)
	rateStore := &ratelimit.Store{Pool: pool, Admission: ratelimit.NewAdmission()}
	srv.SetAdmission(rateStore)

	b, err := brokerStore.CreateBroker(ctx, "ITEST-"+uuid.NewString(), "Integration", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = brokerStore.CreateAccount(ctx, b.ID, "A1", "A1", "F", "ACC1")
	if err := rateStore.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	tok, err := brokerStore.CreateTokenExt(ctx, brokers.CreateTokenInput{
		BrokerID: b.ID, Name: "t1", QuikFirm: "FIRM1", Permissions: []string{auth.PermOrdersCreate, auth.PermOrdersRead, auth.PermOrdersCancel},
	})
	if err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"account_id": "ACC1", "client_code": "006", "client_order_id": "IDEM-1", "symbol": "SBER", "board": "TQBR",
		"side": "BUY", "order_type": "LIMIT", "quantity": "10", "price": "250",
	}
	payload, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+tok.Raw)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusOK {
		t.Fatalf("first create status %d body %s", rr.Code, rr.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(payload))
	req2.Header.Set("Authorization", "Bearer "+tok.Raw)
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("idempotent create status %d", rr2.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp)
	if resp["idempotent"] != true {
		t.Fatalf("expected idempotent true: %#v", resp)
	}
	orderID := resp["order"].(map[string]any)["id"].(string)
	for field, value := range map[string]any{"quantity": "11", "price": "251", "side": "SELL", "client_code": "007"} {
		changed := map[string]any{}
		for k, v := range body {
			changed[k] = v
		}
		changed[field] = value
		payload, _ := json.Marshal(changed)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+tok.Raw)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusConflict {
			t.Fatalf("reused client_order_id with different %s: status %d body %s", field, rr.Code, rr.Body.String())
		}
	}
	var queued int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1 AND event_type='ORDER_SEND' AND status='PENDING'`, orderID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("accepted/idempotent order must have exactly one durable queue entry: count=%d err=%v", queued, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='ACKNOWLEDGED' WHERE id=$1`, orderID); err != nil {
		t.Fatal(err)
	}
	if already, err := orderStore.EnqueueCancel(ctx, uuid.MustParse(orderID)); err != nil || already {
		t.Fatalf("first cancel: %v %v", already, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET status='DONE',created_at=now()-interval '5 minutes' WHERE aggregate_id=$1 AND event_type='ORDER_CANCEL'`, orderID); err != nil {
		t.Fatal(err)
	}
	if already, err := orderStore.EnqueueCancel(ctx, uuid.MustParse(orderID)); err != nil || !already {
		t.Fatalf("uncertain cancel repeated: %v %v", already, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox_events WHERE aggregate_id=$1 AND event_type='ORDER_CANCEL'`, orderID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("duplicate cancel queue entry: %d %v", queued, err)
	}
	maxQuantity := decimal.NewFromInt(9)
	policy := orders.TradingLimit{BrokerID: b.ID, MaxQuantity: &maxQuantity}
	if err := orderStore.UpdateTradingLimit(ctx, policy, "integration-operator", "test quantity limit"); err != nil {
		t.Fatal(err)
	}
	callCreate := func(body map[string]any) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(payload))
		r.Header.Set("Authorization", "Bearer "+tok.Raw)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, r)
		return rr
	}
	body["client_order_id"] = "RISK-QUANTITY"
	riskResponse := callCreate(body)
	if riskResponse.Code != http.StatusUnprocessableEntity || !bytes.Contains(riskResponse.Body.Bytes(), []byte("MAX_ORDER_QUANTITY")) {
		t.Fatalf("risk rejection must be 422, not storage failure: %d %s", riskResponse.Code, riskResponse.Body.String())
	}
	policy.Revision, policy.Paused = 1, true
	if err := orderStore.UpdateTradingLimit(ctx, policy, "integration-operator", "test broker pause"); err != nil {
		t.Fatal(err)
	}
	body["client_order_id"], body["quantity"] = "RISK-PAUSED", "1"
	riskResponse = callCreate(body)
	if riskResponse.Code != http.StatusUnprocessableEntity || !bytes.Contains(riskResponse.Body.Bytes(), []byte("TRADING_PAUSED")) {
		t.Fatalf("pause rejection: %d %s", riskResponse.Code, riskResponse.Body.String())
	}
	body["client_order_id"], body["quantity"] = "IDEM-1", "10"
	if replay := callCreate(body); replay.Code != http.StatusOK {
		t.Fatalf("paused replay: %d %s", replay.Code, replay.Body.String())
	}
}

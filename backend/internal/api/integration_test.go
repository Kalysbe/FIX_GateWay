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
	"github.com/fix-gateway/backend/internal/quikexport"
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
	orderSvc.SetTradingAccounts(fixedAccounts{"FIRM1": {"ACC1"}})
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

	callCreate := func(body map[string]any, key string) *httptest.ResponseRecorder {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewReader(payload))
		r.Header.Set("Authorization", "Bearer "+tok.Raw)
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, r)
		return rr
	}
	decodeOrder := func(rr *httptest.ResponseRecorder) map[string]any {
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		order, _ := resp["order"].(map[string]any)
		return order
	}

	body := map[string]any{
		"account_id": "ACC1", "client_code": "006", "symbol": "SBER", "board": "TQBR",
		"side": "BUY", "order_type": "LIMIT", "quantity": "10", "price": "250",
	}
	idemKey := "IDEM-" + uuid.NewString()
	rr := callCreate(body, idemKey)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first create status %d body %s", rr.Code, rr.Body.String())
	}
	first := decodeOrder(rr)
	// client_order_id is assigned by the gateway, not taken from the broker.
	if first["client_order_id"] != first["id"] {
		t.Fatalf("client_order_id must be generated as the order id: %#v", first)
	}

	rr2 := callCreate(body, idemKey)
	if rr2.Code != http.StatusOK {
		t.Fatalf("idempotent create status %d", rr2.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(rr2.Body.Bytes(), &resp)
	if resp["idempotent"] != true {
		t.Fatalf("expected idempotent true: %#v", resp)
	}
	orderID := resp["order"].(map[string]any)["id"].(string)
	if orderID != first["id"] {
		t.Fatalf("replay must return the original order: %s != %v", orderID, first["id"])
	}
	for field, value := range map[string]any{"quantity": "11", "price": "251", "side": "SELL", "client_code": "007"} {
		changed := map[string]any{}
		for k, v := range body {
			changed[k] = v
		}
		changed[field] = value
		if rr := callCreate(changed, idemKey); rr.Code != http.StatusConflict {
			t.Fatalf("reused Idempotency-Key with different %s: status %d body %s", field, rr.Code, rr.Body.String())
		}
	}
	// Without the header every request is a new order.
	noKey1, noKey2 := callCreate(body, ""), callCreate(body, "")
	if noKey1.Code != http.StatusCreated || noKey2.Code != http.StatusCreated || decodeOrder(noKey1)["id"] == decodeOrder(noKey2)["id"] {
		t.Fatalf("requests without Idempotency-Key must create distinct orders: %d %d", noKey1.Code, noKey2.Code)
	}
	withID := map[string]any{"client_order_id": "BROKER-1"}
	for k, v := range body {
		withID[k] = v
	}
	if rr := callCreate(withID, ""); rr.Code != http.StatusBadRequest {
		t.Fatalf("broker client_order_id must be refused: %d %s", rr.Code, rr.Body.String())
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
	riskResponse := callCreate(body, "")
	if riskResponse.Code != http.StatusUnprocessableEntity || !bytes.Contains(riskResponse.Body.Bytes(), []byte("MAX_ORDER_QUANTITY")) {
		t.Fatalf("risk rejection must be 422, not storage failure: %d %s", riskResponse.Code, riskResponse.Body.String())
	}
	policy.Revision, policy.Paused = 1, true
	if err := orderStore.UpdateTradingLimit(ctx, policy, "integration-operator", "test broker pause"); err != nil {
		t.Fatal(err)
	}
	body["quantity"] = "1"
	riskResponse = callCreate(body, "")
	if riskResponse.Code != http.StatusUnprocessableEntity || !bytes.Contains(riskResponse.Body.Bytes(), []byte("TRADING_PAUSED")) {
		t.Fatalf("pause rejection: %d %s", riskResponse.Code, riskResponse.Body.String())
	}
	body["quantity"] = "10"
	if replay := callCreate(body, idemKey); replay.Code != http.StatusOK {
		t.Fatalf("paused replay: %d %s", replay.Code, replay.Body.String())
	}
}

// fixedAccounts stands in for QUIK Trdaccs: firm -> TrdAccId values.
type fixedAccounts map[string][]string

func (f fixedAccounts) TradingAccount(_ context.Context, firm, account string) (quikexport.Row, error) {
	for _, a := range f[firm] {
		if a == account {
			return quikexport.Row{Raw: json.RawMessage(`{"TrdAccId":"` + a + `","FirmId":"` + firm + `"}`)}, nil
		}
	}
	return quikexport.Row{}, quikexport.ErrAccountNotFound
}

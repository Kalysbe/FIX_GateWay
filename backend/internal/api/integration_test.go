//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestOrderCreateIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg := config.Load()
	cfg.DatabaseURL = dsn
	cfg.MigrationsPath = "../../migrations"
	// No outbox worker runs here, so QUIK never answers: every create ends in 202.
	cfg.OrderAnswerTimeout = 100 * time.Millisecond
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
	_ = users.EnsureAdmin(ctx, cfg.AdminUsername, cfg.AdminPassword)
	brokerStore := brokers.NewStore(pool, cfg.APITokenPepper)
	orderStore := orders.NewStore(pool)
	auditStore := audit.NewStore(testLogFiles(t))
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
	orderSvc.SetInstruments(fixedInstrument{})
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
		var env struct {
			Code string         `json:"code"`
			Data map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &env)
		return env.Data
	}

	body := map[string]any{
		"Account": "ACC1", "ClientCode": "006", "SecCode": "SBER", "ClassCode": "TQBR",
		"Operation": "Купля", "Qty": 10, "Price": 250,
	}
	rr := callCreate(body, "")
	if rr.Code != http.StatusAccepted || !strings.Contains(rr.Body.String(), "QUIK_ANSWER_PENDING") {
		t.Fatalf("first create status %d body %s", rr.Code, rr.Body.String())
	}
	first := decodeOrder(rr)
	if first["OrderNum"] != nil || first["SecCode"] != "SBER" || first["Operation"] != "Купля" || first["id"] == nil {
		t.Fatalf("reply must use QUIK Orders names: %#v", first)
	}
	orderID := first["id"].(string)
	// Idempotency-Key is no longer read: a repeat with it is a new order.
	again := callCreate(body, "IDEM-"+uuid.NewString())
	if again.Code != http.StatusAccepted || decodeOrder(again)["id"] == orderID {
		t.Fatalf("every request must create a new order: %d %s", again.Code, again.Body.String())
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
		t.Fatalf("accepted order must have exactly one durable queue entry: count=%d err=%v", queued, err)
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
	body["Qty"] = 1
	riskResponse = callCreate(body, "")
	if riskResponse.Code != http.StatusUnprocessableEntity || !bytes.Contains(riskResponse.Body.Bytes(), []byte("TRADING_PAUSED")) {
		t.Fatalf("pause rejection: %d %s", riskResponse.Code, riskResponse.Body.String())
	}
}

// fixedInstrument stands in for QUIK Securities: every instrument exists.
type fixedInstrument struct{}

func (fixedInstrument) Instrument(_ context.Context, classCode, secCode string) (quikexport.Row, error) {
	return quikexport.Row{Raw: json.RawMessage(`{"ClassCode":"` + classCode + `","SecCode":"` + secCode + `","FaceUnit":"RUB"}`)}, nil
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

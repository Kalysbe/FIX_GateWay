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
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/logx"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/routing"
)

func TestOrderIdempotencyIntegration(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg := config.Load()
	cfg.DatabaseURL = dsn
	cfg.MigrationsPath = "../migrations"
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

	b, err := brokerStore.CreateBroker(ctx, "ITEST", "Integration", "")
	if err != nil {
		t.Fatal(err)
	}
	acc, err := brokerStore.CreateAccount(ctx, b.ID, "A1", "A1", "F", "ACC1")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := brokerStore.CreateTokenExt(ctx, brokers.CreateTokenInput{
		BrokerID: b.ID, Name: "t1", QuikFirm: "FIRM1",
	})
	if err != nil {
		t.Fatal(err)
	}

	body := map[string]any{
		"account_id": "1-3101-006", "client_code": "006", "client_order_id": "IDEM-1", "symbol": "SBER", "board": "TQBR",
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
}

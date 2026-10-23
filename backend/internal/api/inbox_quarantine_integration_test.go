//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/executioninbox"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/platform/db"
	"github.com/fix-gateway/backend/internal/platform/logx"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/routing"
	"github.com/google/uuid"
)

func TestExecutionInboxQuarantineAPI(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg := config.Load()
	if err := migrate.Up(dsn, "../../migrations"); err != nil {
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
	brokerStore := brokers.NewStore(pool, cfg.APITokenPepper)
	orderStore := orders.NewStore(pool)
	auditStore := audit.NewStore(pool)
	adapterStore := adapters.NewStore(pool)
	manager := adapters.NewManager(adapterStore, log, routing.New(cfg.LBStrategy), nil, t.TempDir(), time.Second)
	srv := api.NewServer(cfg, log, pool, brokerStore, orders.NewService(orderStore, brokerStore, auditStore), orderStore,
		adapterStore, manager, outbox.NewStore(pool), auditStore, users, realtime.NewHub(), nil)

	inbox, err := executioninbox.Open(t.TempDir(), 1<<20, 100, log)
	if err != nil {
		t.Fatal(err)
	}
	defer inbox.Close()
	cases := executioninbox.NewCaseStore(pool)
	inbox.SetQuarantineCheck(cases.Exists)
	srv.SetExecutionInbox(inbox, cases)
	clOrdID := "Q-" + uuid.NewString()
	if err := inbox.Append(domain.ExecutionReport{AdapterID: "quik-fix-1", ClOrdID: clOrdID, ExecType: "H", RawFIX: "35=8|150=H"}); err != nil {
		t.Fatal(err)
	}
	if err := inbox.Append(domain.ExecutionReport{AdapterID: "quik-fix-1", ClOrdID: clOrdID + "-next"}); err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	applied := make(chan string, 4)
	go inbox.Run(runCtx, func(_ context.Context, e executioninbox.Entry) error {
		if e.Report.ExecType == "H" {
			return errors.New("execution correction requires venue reconciliation")
		}
		applied <- e.Report.ClOrdID
		return nil
	}, nil)

	email := uuid.NewString() + "@example.test"
	if err := users.EnsureAdmin(ctx, email, "quarantine-test-password"); err != nil {
		t.Fatal(err)
	}
	token := func(role string) string {
		if _, err := pool.Exec(ctx, `UPDATE users SET role_code=$2 WHERE email=$1`, email, role); err != nil {
			t.Fatal(err)
		}
		u, err := users.Authenticate(ctx, email, "quarantine-test-password")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := admin.IssueToken(cfg.JWTSecret, u)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	call := func(method, path, tok string, body any) (int, map[string]any) {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		var out map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &out)
		return rr.Code, out
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, ok := inbox.Blocked(); ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("inbox did not block")
		}
		time.Sleep(20 * time.Millisecond)
	}
	readOnly := token("READ_ONLY")
	code, state := call("GET", "/api/v1/admin/execution-inbox", readOnly, nil)
	if code != 200 || state["blocked"] == nil {
		t.Fatalf("state %d %v", code, state)
	}
	entryID := state["blocked"].(map[string]any)["entry_id"].(string)
	if code, _ := call("POST", "/api/v1/admin/execution-inbox/"+entryID+"/quarantine", readOnly, map[string]string{"reason": "x"}); code != 403 {
		t.Fatalf("read-only quarantine: %d", code)
	}
	operator := token("OPERATOR")
	if code, _ := call("POST", "/api/v1/admin/execution-inbox/"+entryID+"/quarantine", operator, map[string]string{"reason": " "}); code != 400 {
		t.Fatalf("empty reason: %d", code)
	}
	if code, _ := call("POST", "/api/v1/admin/execution-inbox/"+uuid.NewString()+"/quarantine", operator, map[string]string{"reason": "x"}); code != 409 {
		t.Fatalf("other entry: %d", code)
	}
	code, c := call("POST", "/api/v1/admin/execution-inbox/"+entryID+"/quarantine", operator, map[string]string{"reason": "ARQA bust, reconcile with QUIK"})
	if code != 202 || c["cl_ord_id"] != clOrdID {
		t.Fatalf("quarantine %d %v", code, c)
	}
	defer pool.Exec(ctx, `DELETE FROM execution_inbox_quarantine WHERE entry_id=$1`, entryID)
	select {
	case got := <-applied:
		if got != clOrdID+"-next" {
			t.Fatalf("applied %s", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("next report not applied after quarantine")
	}
	if !inbox.Status().Healthy() {
		t.Fatal("trading not resumed")
	}
	if code, _ := call("POST", "/api/v1/admin/execution-inbox/cases/"+entryID+"/resolve", operator, map[string]string{"note": "Trade bust confirmed in QUIK"}); code != 200 {
		t.Fatalf("resolve %d", code)
	}
	if code, _ := call("POST", "/api/v1/admin/execution-inbox/cases/"+entryID+"/resolve", operator, map[string]string{"note": "again"}); code != 409 {
		t.Fatalf("double resolve %d", code)
	}
	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE entity_id=$1 AND action IN ('EXECUTION_INBOX_QUARANTINE','EXECUTION_INBOX_CASE_RESOLVE')`, entryID).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("audit rows %d %v", audits, err)
	}
}

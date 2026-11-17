//go:build integration

package ratelimit

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestPolicyPersistenceAuditAndRestart(t *testing.T) {
	dsn := os.Getenv("RATE_LIMIT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database required")
	}
	if !strings.Contains(dsn, "/rate_limit_test?") {
		t.Fatal("database must be named rate_limit_test")
	}
	if err := migrate.Up(dsn, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	actor := uuid.NewString()
	var broker int64
	if err = pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'Rate test') RETURNING id`, actor).Scan(&broker); err != nil {
		t.Fatal(err)
	}
	type auditLine struct {
		actor   string
		payload map[string]json.RawMessage
	}
	var audited []auditLine
	record := func(_ context.Context, actor string, _ *int64, _ string, payload json.RawMessage) error {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(payload, &m); err != nil {
			return err
		}
		audited = append(audited, auditLine{actor, m})
		return nil
	}
	s := &Store{Pool: pool, Admission: NewAdmission(), Audit: record}
	if err = s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	subject := strconv.FormatInt(broker, 10)
	p := policy("broker", subject, "Trading", 10, 2, 2)
	if err = s.Upsert(ctx, p, actor, "load test policy"); err != nil {
		t.Fatal(err)
	}
	if err = s.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	take(t, s.Admission, subject, "SubmitOrder")()
	take(t, s.Admission, subject, "SubmitOrder")()
	if _, r := s.Admission.Acquire("broker", subject, "Trading", "SubmitOrder"); r == nil {
		t.Fatal("policy not enforced")
	}
	p.Concurrent = 5
	p.Enabled = false
	if err = s.Upsert(ctx, p, actor, "disable limit for test"); err != nil {
		t.Fatal(err)
	}
	restarted := &Store{Pool: pool, Admission: NewAdmission()}
	if err = restarted.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	got, ok := restarted.Admission.resolve("broker", subject, "Trading")
	if !ok || got != p {
		t.Fatal("restart policy", got)
	}
	if len(audited) != 2 || audited[0].actor != actor || audited[1].payload["reason"] == nil {
		t.Fatal("audit", audited)
	}
	var before struct{ Rate int }
	if err = json.Unmarshal(audited[1].payload["before"], &before); err != nil || before.Rate != 10 {
		t.Fatal("before state", before, err)
	}
	s.Audit = func(context.Context, string, *int64, string, json.RawMessage) error {
		return errors.New("log file unavailable")
	}
	p.Rate = 99
	if err = s.Upsert(ctx, p, "rate-test-failure", "audit rollback test"); err == nil {
		t.Fatal("expected audit rejection")
	}
	var persisted int
	if err = pool.QueryRow(ctx, `SELECT rate FROM rate_limit_policies WHERE scope='broker' AND subject=$1 AND operation='Trading'`, subject).Scan(&persisted); err != nil || persisted != 10 {
		t.Fatal("policy escaped failed audit transaction", persisted, err)
	}
}

//go:build integration

package admin

import (
	"context"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

func TestExplicitPasswordRotationRevokesSessions(t *testing.T) {
	dsn := os.Getenv("SECURITY_AUDIT_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable security database required")
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
	store := NewUserStore(pool)
	email := uuid.NewString() + "@example.test"
	if err := store.EnsureAdmin(ctx, email, "admin123"); err != nil {
		t.Fatal(err)
	}
	if err := store.RefuseDefaultPassword(ctx, email); err == nil {
		t.Fatal("existing default password accepted")
	}
	if err := store.EnsureAdmin(ctx, email, "bootstrap-change-does-not-rotate"); err != nil {
		t.Fatal(err)
	}
	user, err := store.Authenticate(ctx, email, "admin123")
	if err != nil {
		t.Fatal("bootstrap unexpectedly rotated existing account")
	}
	token, err := IssueToken("fixture-jwt", user)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RotatePassword(ctx, email, "new-explicit-password-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateSession(ctx, "fixture-jwt", token); err == nil {
		t.Fatal("old session survived password rotation")
	}
	if _, err := store.Authenticate(ctx, email, "admin123"); err == nil {
		t.Fatal("old password survived rotation")
	}
	if err := store.RefuseDefaultPassword(ctx, email); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(ctx, email, "new-explicit-password-123"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action='ADMIN_PASSWORD_ROTATED' AND entity_id=$1 AND request_payload IS NULL`, user.ID.String()).Scan(&count); err != nil || count != 1 {
		t.Fatalf("rotation was not safely audited: %d %v", count, err)
	}
}

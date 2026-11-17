//go:build integration

package admin

import (
	"context"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/observability"
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
	username := "user-" + uuid.NewString()
	if err := store.EnsureAdmin(ctx, username, "admin123"); err != nil {
		t.Fatal(err)
	}
	if err := store.RefuseDefaultPassword(ctx, username); err == nil {
		t.Fatal("existing default password accepted")
	}
	if err := store.EnsureAdmin(ctx, username, "bootstrap-change-does-not-rotate"); err != nil {
		t.Fatal(err)
	}
	user, err := store.Authenticate(ctx, username, "admin123")
	if err != nil {
		t.Fatal("bootstrap unexpectedly rotated existing account")
	}
	token, err := IssueToken("fixture-jwt", user)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := observability.OpenFileLog(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer logs.Close()
	auditLog := audit.NewStore(logs)
	if err := store.RotatePassword(ctx, auditLog, username, "new-explicit-password-123"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthenticateSession(ctx, "fixture-jwt", token); err == nil {
		t.Fatal("old session survived password rotation")
	}
	if _, err := store.Authenticate(ctx, username, "admin123"); err == nil {
		t.Fatal("old password survived rotation")
	}
	if err := store.RefuseDefaultPassword(ctx, username); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(ctx, username, "new-explicit-password-123"); err != nil {
		t.Fatal(err)
	}
	records, err := auditLog.Records(ctx)
	count := 0
	for _, e := range records {
		if e.Subtype == "ADMIN_PASSWORD_ROTATED" && e.TargetID == user.ID.String() && len(e.Metadata) == 0 {
			count++
		}
	}
	if err != nil || count != 1 {
		t.Fatalf("rotation was not safely audited: %d %v", count, err)
	}
}

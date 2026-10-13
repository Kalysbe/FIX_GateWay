//go:build integration

package auth

import (
	"context"
	"errors"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPersistentNoncesSurviveNewInstanceAndConcurrentRequests(t *testing.T) {
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
	scope := uuid.NewString()
	nonce := uuid.NewString()
	first := NewPersistentNonceStore(pool, time.Minute)
	if err := first.UseScopedContext(ctx, scope, nonce); err != nil {
		t.Fatal(err)
	}
	if err := NewPersistentNonceStore(pool, time.Minute).UseScopedContext(ctx, scope, nonce); !errors.Is(err, ErrNonceReplay) {
		t.Fatalf("restart replay accepted: %v", err)
	}
	if err := first.UseScopedContext(ctx, uuid.NewString(), nonce); err != nil {
		t.Fatalf("other token scope rejected: %v", err)
	}
	nonce = uuid.NewString()
	var successes atomic.Int64
	var wg sync.WaitGroup
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := first.UseScopedContext(ctx, scope, nonce)
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrNonceReplay) {
				t.Errorf("unexpected nonce error: %v", err)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 {
		t.Fatalf("concurrent accepted=%d", successes.Load())
	}
	pool.Close()
	if err := first.UseScopedContext(ctx, scope, uuid.NewString()); !errors.Is(err, ErrNonceUnavailable) {
		t.Fatalf("nonce DB failure did not fail closed: %v", err)
	}
}

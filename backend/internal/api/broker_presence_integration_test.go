//go:build integration

package api

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// A broker that only reads (GET market data / positions) leaves no persisted
// API_REQUEST rows: keepEvent drops successful GETs. It was invisible on the
// dashboard and the Brokers page while connected on 172.23.10.96.
func TestReadOnlyBrokerIsOnlineFromTokenUse(t *testing.T) {
	dsn := os.Getenv("TOKEN_LOOKUP_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("disposable TOKEN_LOOKUP_TEST_DATABASE_URL required")
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
	store := brokers.NewStore(pool, "presence-test-pepper")
	broker, err := store.CreateBroker(ctx, "PRESENCE_"+uuid.NewString()[:8], "Read-only broker", "")
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: broker.ID, Name: "reader", QuikFirm: "2040", Permissions: auth.ReadOnlyPermissions})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{pool: pool, brokers: store, hub: realtime.NewHub(), log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		obs: observability.NewPipeline(observability.NewStore(pool), slog.New(slog.NewTextHandler(io.Discard, nil)), nil, observability.DefaultSettings())}
	online := func() (observability.ConnectedBroker, bool) {
		for _, c := range s.connectedBrokers(ctx) {
			if c.BrokerID == broker.ID {
				return c, true
			}
		}
		return observability.ConnectedBroker{}, false
	}
	if _, ok := online(); ok {
		t.Fatal("unused token shown as online")
	}
	if _, _, err := store.AuthenticateToken(ctx, created.Raw, "172.23.10.97"); err != nil {
		t.Fatal(err)
	}
	c, ok := online()
	if !ok || c.LastIP != "172.23.10.97" || c.LastActivity.IsZero() || c.BrokerCode != broker.Code {
		t.Fatalf("read-only broker missing from presence: %+v ok=%v", c, ok)
	}
	if _, err := pool.Exec(ctx, `UPDATE api_tokens SET last_used_at=now()-interval '3 minutes' WHERE broker_id=$1`, broker.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := online(); ok {
		t.Fatal("broker idle for longer than the window still shown as online")
	}
}

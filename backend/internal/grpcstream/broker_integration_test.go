//go:build integration

package grpcstream

import (
	"context"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/metadata"
	"os"
	"testing"
)

func TestBrokerTokenDatabaseRevocation(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
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
	store := brokers.NewStore(pool, "grpc-integration-pepper")
	broker, err := store.CreateBroker(ctx, uuid.NewString(), "gRPC integration", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		pool.Exec(ctx, "DELETE FROM api_tokens WHERE broker_id=$1", broker.ID)
		pool.Exec(ctx, "DELETE FROM brokers WHERE id=$1", broker.ID)
	}()
	token, err := store.CreateTokenExt(ctx, brokers.CreateTokenInput{BrokerID: broker.ID, Name: "gRPC", QuikFirm: "TEST", Permissions: auth.ReadOnlyPermissions})
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{Brokers: store}
	identity, err := service.authenticate(ctx, metadata.Pairs("x-client-type", "broker"), token.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if identity.info.BrokerID != broker.ID || identity.role != "BROKER" {
		t.Fatal("wrong principal")
	}
	if err := identity.validate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeToken(ctx, token.Token.ID); err != nil {
		t.Fatal(err)
	}
	if err := identity.validate(ctx); err == nil {
		t.Fatal("database revocation not applied")
	}
	if _, err := service.authenticate(ctx, metadata.Pairs("x-client-type", "broker"), token.Raw); err == nil {
		t.Fatal("revoked token accepted on reconnect")
	}
}

//go:build integration

package positions

import (
	"context"
	"os"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

func TestRealtimeSnapshotFirmIsolation(t *testing.T) {
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
	s := NewStore(pool)
	id, err := s.BeginSnapshot(ctx, "test", "realtime-test", "")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM fix_position_snapshots WHERE id=$1", id)
	items := []domain.FIXPosition{
		{Kind: "CASH", FirmID: "2040", Currency: "KGS", PosAmt: decimal.RequireFromString("123.456"), RawFIX: "private wire"},
		{Kind: "INSTRUMENT", FirmID: "MC003", Symbol: "TEST", NetQty: decimal.NewFromInt(7)},
		{Kind: "CASH", FirmID: "OTHER", Currency: "KGS", PosAmt: decimal.NewFromInt(999)},
	}
	if err := s.InsertPositions(ctx, id, items); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteSnapshot(ctx, id, "OK", "", len(items)); err != nil {
		t.Fatal(err)
	}
	data, err := s.RealtimeSnapshot(ctx, []string{"2040", "MC003"})
	if err != nil {
		t.Fatal(err)
	}
	result := data.(map[string]any)
	cash := result["cash"].([]domain.FIXPosition)
	instruments := result["instruments"].([]domain.FIXPosition)
	if len(cash) != 1 || cash[0].FirmID != "2040" || cash[0].RawFIX != "" || cash[0].PosAmt.String() != "123.456" || len(instruments) != 1 || instruments[0].FirmID != "MC003" {
		t.Fatalf("wrong scope: %+v", result)
	}
	for _, firms := range [][]string{{}, {"UNKNOWN"}} {
		data, err := s.RealtimeSnapshot(ctx, firms)
		if err != nil {
			t.Fatal(err)
		}
		result := data.(map[string]any)
		if len(result["cash"].([]domain.FIXPosition)) != 0 || len(result["instruments"].([]domain.FIXPosition)) != 0 {
			t.Fatal("empty scope leaked positions")
		}
	}
	// Updates replace a single persisted stream snapshot atomically, including
	// deletions. No old rows may reappear in REST or broker gRPC snapshots.
	if err := s.SaveStreamSnapshot(ctx, "stream-test", "subscription", items); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(ctx, "DELETE FROM fix_position_snapshots WHERE adapter_id='stream-test'")
	if err := s.SaveStreamSnapshot(ctx, "stream-test", "subscription", items[:1]); err != nil {
		t.Fatal(err)
	}
	data, err = s.RealtimeSnapshot(ctx, []string{"2040", "MC003"})
	if err != nil {
		t.Fatal(err)
	}
	result = data.(map[string]any)
	if len(result["cash"].([]domain.FIXPosition)) != 1 || len(result["instruments"].([]domain.FIXPosition)) != 0 {
		t.Fatal("deleted instrument retained", result)
	}
	if err := s.SaveStreamSnapshot(ctx, "stream-test", "subscription", nil); err != nil {
		t.Fatal(err)
	}
	data, err = s.RealtimeSnapshot(ctx, []string{"2040"})
	if err != nil {
		t.Fatal(err)
	}
	if len(data.(map[string]any)["cash"].([]domain.FIXPosition)) != 0 {
		t.Fatal("deleted cash retained")
	}
}

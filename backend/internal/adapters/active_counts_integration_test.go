//go:build integration

package adapters

import (
	"context"
	"os"
	"testing"

	"github.com/fix-gateway/backend/internal/platform/migrate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestActiveCountsOnlySentOpenRecentOrders(t *testing.T) {
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
	t.Cleanup(pool.Close)
	adapter := "active-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `INSERT INTO adapters(id,name,type,host,port,sender_comp_id,target_comp_id) VALUES($1,$1,'arqa','localhost',1,'S','T')`, adapter); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM orders WHERE adapter_id=$1`, adapter)
		pool.Exec(ctx, `DELETE FROM adapters WHERE id=$1`, adapter)
	})
	var broker, instrument int64
	if err := pool.QueryRow(ctx, `INSERT INTO brokers(code,name) VALUES($1,'active counts') RETURNING id`, uuid.NewString()).Scan(&broker); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO instruments(internal_symbol,quik_symbol,board) VALUES($1,$1,'ACT') RETURNING id`, uuid.NewString()).Scan(&instrument); err != nil {
		t.Fatal(err)
	}
	for _, o := range []struct {
		status string
		sent   bool
		age    string
	}{
		{"ACKNOWLEDGED", true, "1 minute"},    // counted
		{"PARTIALLY_FILLED", true, "2 hours"}, // counted
		{"FILLED", true, "1 minute"},          // terminal
		{"QUEUED", false, "1 minute"},         // not sent
		{"ACKNOWLEDGED", true, "30 hours"},    // stale, no final report
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO orders(id,broker_id,client_order_id,instrument_id,side,order_type,time_in_force,quantity,price,status,adapter_id,fix_send_attempted_at,created_at)
VALUES($1,$2,$3,$4,'BUY','LIMIT','DAY',1,1,$5,$6,CASE WHEN $7 THEN now() END,now()-$8::interval)`,
			uuid.New(), broker, uuid.NewString(), instrument, o.status, adapter, o.sent, o.age); err != nil {
			t.Fatal(err)
		}
	}
	counts, err := NewStore(pool).ActiveCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if counts[adapter] != 2 {
		t.Fatalf("active %d, want 2", counts[adapter])
	}
}

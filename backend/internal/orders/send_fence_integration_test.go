//go:build integration

package orders

import (
	"errors"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

// A send the adapter refused before the wire is queued again with no fence;
// a sent order keeps its fence.
func TestReleaseSendFenceRequeuesOnlyUnsentOrders(t *testing.T) {
	ctx, store, pool, create := replayFixture(t)
	order := create()
	if _, err := pool.Exec(ctx, `UPDATE orders SET status='QUEUED' WHERE id=$1`, order.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignRoute(ctx, order.ID, "adapter-1", *order.FixClOrdID); err != nil {
		t.Fatal(err)
	}
	if err := store.FenceSendAttempt(ctx, order.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseSendFence(ctx, order.ID, "not logged on"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, order.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.OrderQueued || got.FixSendAttemptedAt != nil || got.AdapterID != nil || !ShouldAttemptFIXSend(got) {
		t.Fatalf("order not requeued: status=%s fence=%v adapter=%v", got.Status, got.FixSendAttemptedAt, got.AdapterID)
	}
	if err := store.FenceSendAttempt(ctx, order.ID); err != nil {
		t.Fatalf("requeued order cannot be fenced again: %v", err)
	}

	sent := create()
	if err := store.ReleaseSendFence(ctx, sent.ID, "not logged on"); !errors.Is(err, ErrCannotFailover) {
		t.Fatalf("sent order released: %v", err)
	}
}

package adapters

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

type positionTestAdapter struct {
	Adapter
	id           string
	status       domain.AdapterStatus
	err          error
	calls, stops int
}

func (a *positionTestAdapter) ID() string                   { return a.id }
func (a *positionTestAdapter) Status() domain.AdapterStatus { return a.status }
func (a *positionTestAdapter) Meta() domain.Adapter         { return domain.Adapter{ID: a.id} }
func (a *positionTestAdapter) EnsurePositionSubscription(context.Context) error {
	a.calls++
	return a.err
}
func (a *positionTestAdapter) UnsubscribePositions(context.Context) error { a.stops++; return nil }

func TestPositionOwnerFailsOverAfterRejectAndStaysOnHealthyFeed(t *testing.T) {
	a := &positionTestAdapter{id: "a1", status: domain.AdapterOnline, err: errors.New("rejected")}
	b := &positionTestAdapter{id: "a2", status: domain.AdapterOnline}
	m := &Manager{adapters: map[string]Adapter{"a1": a, "a2": b}}
	m.setPositionOwner("a1")
	if err := m.EnsurePositionSubscription(context.Background()); err != nil || m.positionOwner() != "a2" || a.stops != 1 {
		t.Fatal("reject did not switch owner", err, a.stops)
	}
	a.err = nil
	if err := m.EnsurePositionSubscription(context.Background()); err != nil || a.calls != 1 || b.calls != 2 {
		t.Fatal("working feed was replaced", err, a.calls, b.calls)
	}
	b.status = domain.AdapterOffline
	if err := m.EnsurePositionSubscription(context.Background()); err != nil || m.positionOwner() != "a1" {
		t.Fatal("offline owner did not switch", err)
	}
	b.err, a.err = errors.New("rejected"), errors.New("rejected")
	b.status = domain.AdapterOnline
	if err := m.EnsurePositionSubscription(context.Background()); err == nil {
		t.Fatal("two rejected feeds reported success")
	}
	if m.positionOwner() != "a1" {
		t.Fatal("failed probes replaced retained owner")
	}
}

func TestPositionOwnerRejectsLateReportsFromOldFeed(t *testing.T) {
	var saves int
	m := &Manager{onPositions: func(string, string, []domain.FIXPosition) error { saves++; return nil }}
	m.setPositionOwner("a2")
	if err := m.publishPositions("a1", "old", nil); err == nil || saves != 0 {
		t.Fatal("old feed overwrote current snapshot")
	}
	if err := m.publishPositions("a2", "new", nil); err != nil || saves != 1 {
		t.Fatal("selected empty snapshot was dropped", err)
	}
}

func TestAutomaticPositionSnapshotStateRequiresOnlineAdapterAndFreshData(t *testing.T) {
	a := &positionTestAdapter{id: "a1", status: domain.AdapterOnline}
	m := &Manager{adapters: map[string]Adapter{"a1": a}}
	received := time.Now().UTC()
	m.SetPositionSnapshotState(domain.PositionSubscription{AdapterID: "a1", RequestID: "snapshot", Status: "ACTIVE", LastReceivedAt: &received})
	if state := m.PositionSubscription(); state.Status != "ACTIVE" || state.Mode != "snapshot" {
		t.Fatal(state)
	}
	a.status = domain.AdapterOffline
	if state := m.PositionSubscription(); state.Status != "OFFLINE" {
		t.Fatal("offline cache was active", state)
	}
	a.status = domain.AdapterOnline
	received = time.Now().Add(-2 * time.Minute)
	if state := m.PositionSubscription(); state.Status != "RETRYING" {
		t.Fatal("expired snapshot was active", state)
	}
}

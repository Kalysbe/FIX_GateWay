package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

type snapshotWriterStore struct{ reqIDs []string }

func (s *snapshotWriterStore) SaveStreamSnapshot(_ context.Context, _ string, reqID string, _ []domain.FIXPosition) error {
	s.reqIDs = append(s.reqIDs, reqID)
	return nil
}

func TestAutomaticSnapshotCannotOverwriteNewerStream(t *testing.T) {
	store, hub := &snapshotWriterStore{}, &testMDHub{}
	w := &automaticPositionSnapshots{store: store, hub: hub}
	stamp := w.stamp()
	if err := w.saveStream(context.Background(), "a1", "stream", nil); err != nil {
		t.Fatal(err)
	}
	if saved, err := w.saveFallback(context.Background(), stamp, "a1", "slow-snapshot", nil); saved || err != nil {
		t.Fatal("stale snapshot was applied", saved, err)
	}
	if len(store.reqIDs) != 1 || store.reqIDs[0] != "stream" {
		t.Fatal(store.reqIDs)
	}
	if saved, err := w.saveFallback(context.Background(), w.stamp(), "a1", "new-snapshot", nil); !saved || err != nil {
		t.Fatal("valid empty baseline dropped", saved, err)
	}
}

type snapshotLoopManager struct {
	checks, calls int
	state         domain.PositionSubscription
	cancel        context.CancelFunc
	err           error
}

func (m *snapshotLoopManager) PositionStreamSubscription() domain.PositionSubscription {
	m.checks++
	if m.err != nil {
		return domain.PositionSubscription{Status: "OFFLINE"}
	}
	// The first check sees a healthy stream, the next checks see a disconnect;
	// after the fallback has saved its baseline the stream recovers.
	if m.checks == 1 || m.checks >= 4 {
		if m.checks >= 4 {
			m.cancel()
		}
		return domain.PositionSubscription{Status: "ACTIVE"}
	}
	return domain.PositionSubscription{Status: "OFFLINE"}
}
func (m *snapshotLoopManager) FetchPositions(context.Context, domain.PositionQuery) (string, string, []domain.FIXPosition, error) {
	m.calls++
	return "a1", "snapshot", nil, m.err
}
func (m *snapshotLoopManager) SetPositionSnapshotState(state domain.PositionSubscription) {
	m.state = state
}

func TestAutomaticSnapshotsResumeOnDisconnectAndStopForConfirmedStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	m := &snapshotLoopManager{cancel: cancel}
	store, hub := &snapshotWriterStore{}, &testMDHub{}
	positionSnapshotLoop(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), m, &automaticPositionSnapshots{store: store, hub: hub}, time.Millisecond)
	if m.calls != 1 || len(store.reqIDs) != 1 || m.state.Status != "ACTIVE" || m.state.Mode != "snapshot" || m.state.Positions != 0 {
		t.Fatal("fallback did not recover empty baseline or stop polling", m.calls, m.state, store.reqIDs)
	}
}

func TestFailedAutomaticSnapshotDoesNotPublishEmptySuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	m := &snapshotLoopManager{cancel: cancel, err: errors.New("rejected")}
	store, hub := &snapshotWriterStore{}, &testMDHub{}
	positionSnapshotLoop(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), m, &automaticPositionSnapshots{store: store, hub: hub}, time.Millisecond)
	if m.calls != 1 || len(store.reqIDs) != 0 || m.state.Status != "RETRYING" || m.state.NextRetryAt == nil {
		t.Fatal("failed snapshot was saved or retry flooded FIX", m.calls, m.state, store.reqIDs)
	}
}

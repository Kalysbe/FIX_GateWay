package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

type positionSnapshotStore interface {
	SaveStreamSnapshot(context.Context, string, string, []domain.FIXPosition) error
}

// Both write paths share a lock and revision: a slow snapshot must not overwrite
// a newer streaming update. Persistence always publishes a complete replacement.
type automaticPositionSnapshots struct {
	store    positionSnapshotStore
	hub      marketDataPublisher
	mu       sync.Mutex
	revision uint64
}

func (w *automaticPositionSnapshots) stamp() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.revision
}

func (w *automaticPositionSnapshots) persist(ctx context.Context, adapterID, reqID string, rows []domain.FIXPosition, source string) error {
	if err := w.store.SaveStreamSnapshot(ctx, adapterID, reqID, rows); err != nil {
		return err
	}
	w.revision++
	w.hub.Broadcast("positions.updated", map[string]any{"adapter_id": adapterID, "pos_req_id": reqID, "received": len(rows), "source": source})
	return nil
}

func (w *automaticPositionSnapshots) saveStream(ctx context.Context, adapterID, reqID string, rows []domain.FIXPosition) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.persist(ctx, adapterID, reqID, rows, "fix-stream")
}

func (w *automaticPositionSnapshots) saveFallback(ctx context.Context, stamp uint64, adapterID, reqID string, rows []domain.FIXPosition) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.revision != stamp {
		return false, nil
	}
	if err := w.persist(ctx, adapterID, reqID, rows, "fix-snapshot"); err != nil {
		return false, err
	}
	return true, nil
}

type positionSnapshotManager interface {
	FetchPositions(context.Context, domain.PositionQuery) (string, string, []domain.FIXPosition, error)
	PositionStreamSubscription() domain.PositionSubscription
	SetPositionSnapshotState(domain.PositionSubscription)
}

// An automatic baseline also confirms an empty account when QUIK sends no
// initial AP for a subscription. Polling stops as soon as the stream is
// confirmed, and resumes automatically when that stream is lost.
func positionSnapshotLoop(ctx context.Context, log *slog.Logger, manager positionSnapshotManager, writer *automaticPositionSnapshots, interval time.Duration) {
	if interval <= 0 || interval > 30*time.Second {
		interval = 30 * time.Second
	}
	tick := 5 * time.Second
	if interval < tick {
		tick = interval
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	var next time.Time
	var state domain.PositionSubscription
	var lastError string
	attempts := 0
	run := func() {
		if manager.PositionStreamSubscription().Status == "ACTIVE" {
			next = time.Time{}
			return
		}
		now := time.Now().UTC()
		if now.Before(next) {
			return
		}
		if state.Status == "" {
			state = domain.PositionSubscription{Status: "STARTING", Mode: "snapshot"}
			manager.SetPositionSnapshotState(state)
		}
		stamp := writer.stamp()
		work, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		adapterID, reqID, rows, err := manager.FetchPositions(work, domain.PositionQuery{})
		if ctx.Err() != nil {
			return
		}
		if err == nil && manager.PositionStreamSubscription().Status == "ACTIVE" {
			next = now.Add(interval)
			return
		}
		var saved bool
		if err == nil {
			saved, err = writer.saveFallback(work, stamp, adapterID, reqID, rows)
		}
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			attempts++
			delay := 5 * time.Second
			for n := 1; n < attempts && delay < time.Minute; n++ {
				delay *= 2
			}
			if delay > time.Minute {
				delay = time.Minute
			}
			retryAt := time.Now().UTC().Add(delay)
			next = retryAt
			state.Status, state.Error, state.NextRetryAt = "RETRYING", err.Error(), &retryAt
			manager.SetPositionSnapshotState(state)
			writer.hub.Broadcast("positions.updated", map[string]any{"subscription": state})
			if err.Error() != lastError {
				log.Warn("automatic position snapshot recovery", "error", err)
				lastError = err.Error()
			}
			return
		}
		next = time.Now().UTC().Add(interval)
		if !saved {
			return
		}
		attempts, lastError = 0, ""
		received := time.Now().UTC()
		state = domain.PositionSubscription{AdapterID: adapterID, RequestID: reqID, Status: "ACTIVE", Mode: "snapshot", Positions: len(rows), LastReceivedAt: &received}
		manager.SetPositionSnapshotState(state)
		writer.hub.Broadcast("positions.updated", map[string]any{"subscription": state})
	}
	for {
		if ctx.Err() != nil {
			return
		}
		run()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

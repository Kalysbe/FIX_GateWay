package adapters

import (
	"context"
	"fmt"
	"sync"

	"github.com/fix-gateway/backend/internal/domain"
)

// PositionSource is implemented by adapters that can request positions via FIX (35=AN/AP).
type PositionSource interface {
	FetchPositions(ctx context.Context, q domain.PositionQuery) (reqID string, items []domain.FIXPosition, err error)
}

// posFetchMu serializes AN/AP waits — each session has a single posWaiter.
var posFetchMu sync.Mutex

func (m *Manager) FetchPositions(ctx context.Context, q domain.PositionQuery) (adapterID, reqID string, items []domain.FIXPosition, err error) {
	posFetchMu.Lock()
	defer posFetchMu.Unlock()

	type cand struct {
		id  string
		src PositionSource
	}
	m.mu.RLock()
	var list []cand
	for _, ad := range m.adapters {
		if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
			continue
		}
		src, ok := ad.(PositionSource)
		if !ok {
			continue
		}
		list = append(list, cand{id: ad.ID(), src: src})
	}
	m.mu.RUnlock()

	var firstErr error
	for _, c := range list {
		reqID, items, err = c.src.FetchPositions(ctx, q)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.log.Warn("position request failed", "adapter_id", c.id, "error", err)
			continue
		}
		return c.id, reqID, items, nil
	}
	if firstErr != nil {
		return "", "", nil, firstErr
	}
	return "", "", nil, fmt.Errorf("no online FIX adapter available for Request For Positions")
}

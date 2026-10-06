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

// PositionMaintainer is implemented by adapters that can set positions in QUIK (35=AL/AM).
type PositionMaintainer interface {
	MaintainPositions(ctx context.Context, req domain.PositionMaintainRequest) (reqID string, err error)
}

func (m *Manager) MaintainPositions(ctx context.Context, req domain.PositionMaintainRequest) (adapterID, reqID string, err error) {
	posFetchMu.Lock()
	defer posFetchMu.Unlock()

	m.mu.RLock()
	var list []struct {
		id  string
		src PositionMaintainer
	}
	for _, ad := range m.adapters {
		if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
			continue
		}
		src, ok := ad.(PositionMaintainer)
		if !ok {
			continue
		}
		list = append(list, struct {
			id  string
			src PositionMaintainer
		}{ad.ID(), src})
	}
	m.mu.RUnlock()

	var firstErr error
	for _, c := range list {
		reqID, err = c.src.MaintainPositions(ctx, req)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.log.Warn("position maintenance failed", "adapter_id", c.id, "error", err)
			// Same ARQA acceptor on every instance: retrying another session only doubles the wait.
			return c.id, reqID, err
		}
		return c.id, reqID, nil
	}
	if firstErr != nil {
		return "", "", firstErr
	}
	return "", "", fmt.Errorf("no online FIX adapter available for Position Maintenance")
}

package adapters

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

type PositionSubscriber interface {
	EnsurePositionSubscription(context.Context) error
	UnsubscribePositions(context.Context) error
}

type positionStateSource interface {
	PositionSubscription() domain.PositionSubscription
}

func (m *Manager) positionOwner() string {
	m.positionOwnerMu.RLock()
	defer m.positionOwnerMu.RUnlock()
	return m.positionAdapter
}

func (m *Manager) setPositionOwner(id string) {
	m.positionOwnerMu.Lock()
	m.positionAdapter = id
	m.positionOwnerMu.Unlock()
}

func (m *Manager) publishPositions(id, reqID string, rows []domain.FIXPosition) error {
	if m.positionOwner() != id {
		return fmt.Errorf("position feed is no longer selected")
	}
	if m.onPositions == nil {
		return fmt.Errorf("position listener unavailable")
	}
	return m.onPositions(id, reqID, rows)
}

func (m *Manager) PositionStreamSubscription() domain.PositionSubscription {
	id := m.positionOwner()
	m.mu.RLock()
	ad := m.adapters[id]
	m.mu.RUnlock()
	if ad == nil {
		return domain.PositionSubscription{Status: "WAITING"}
	}
	if source, ok := ad.(positionStateSource); ok {
		return source.PositionSubscription()
	}
	status := "STARTING"
	if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
		status = "OFFLINE"
	}
	return domain.PositionSubscription{AdapterID: id, Status: status}
}

func (m *Manager) SetPositionSnapshotState(state domain.PositionSubscription) {
	state.Mode = "snapshot"
	m.positionOwnerMu.Lock()
	m.positionSnapshot = &state
	m.positionOwnerMu.Unlock()
}

func (m *Manager) PositionSubscription() domain.PositionSubscription {
	stream := m.PositionStreamSubscription()
	if stream.Status == "ACTIVE" {
		return stream
	}
	m.positionOwnerMu.RLock()
	var snapshot domain.PositionSubscription
	if m.positionSnapshot != nil {
		snapshot = *m.positionSnapshot
	}
	m.positionOwnerMu.RUnlock()
	if snapshot.Status == "" {
		return stream
	}
	if snapshot.Status == "ACTIVE" {
		m.mu.RLock()
		ad := m.adapters[snapshot.AdapterID]
		m.mu.RUnlock()
		if ad == nil || (ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded) {
			snapshot.Status = "OFFLINE"
		}
		if snapshot.LastReceivedAt != nil && time.Since(*snapshot.LastReceivedAt) > 90*time.Second {
			snapshot.Status, snapshot.Error = "RETRYING", "ожидается свежий автоматический снимок позиций"
		}
	}
	return snapshot
}

// One session owns the all-firm position stream. A healthy owner is retained;
// failure switches to another session, avoiding conflicting duplicate streams.
func (m *Manager) EnsurePositionSubscription(ctx context.Context) error {
	m.positionMu.Lock()
	defer m.positionMu.Unlock()
	m.mu.RLock()
	type candidate struct {
		id       string
		src      PositionSubscriber
		priority int
	}
	var list []candidate
	for id, ad := range m.adapters {
		if ad.Status() == domain.AdapterOnline || ad.Status() == domain.AdapterDegraded {
			if src, ok := ad.(PositionSubscriber); ok {
				list = append(list, candidate{id, src, ad.Meta().Priority})
			}
		}
	}
	owner := m.positionOwner()
	previous, _ := m.adapters[owner].(PositionSubscriber)
	m.mu.RUnlock()
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].id == owner || list[j].id == owner {
			return list[i].id == owner
		}
		if list[i].priority != list[j].priority {
			return list[i].priority < list[j].priority
		}
		return list[i].id < list[j].id
	})
	if len(list) == 0 {
		return fmt.Errorf("no online FIX adapter for position subscription")
	}
	var firstError error
	for _, candidate := range list {
		if err := ctx.Err(); err != nil {
			return err
		}
		// Register ownership before AN: even a synchronous empty confirmation
		// must be persisted, while late reports from old sessions are rejected.
		m.setPositionOwner(candidate.id)
		if err := candidate.src.EnsurePositionSubscription(ctx); err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		if candidate.id != owner && previous != nil {
			_ = previous.UnsubscribePositions(ctx)
		}
		return nil
	}
	m.setPositionOwner(list[0].id)
	return firstError
}

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

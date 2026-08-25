package adapters

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/adapters/arqa"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/routing"
)

type StatusListener func(adapterID string, status domain.AdapterStatus)

type Manager struct {
	store        *Store
	log          *slog.Logger
	balancer     routing.Balancer
	onExec       ExecutionHandler
	onStatus     StatusListener
	onMD         domain.MarketDataHandler
	storeDir     string
	interval     time.Duration
	hostOverride string
	onFIX        func(kind, adapterID, sessionID, msgType, direction, clOrdID, reason string, seq int)
	onObs        func(eventType, subtype, adapterID, message string)

	mu       sync.RWMutex
	adapters map[string]Adapter
}

func NewManager(store *Store, log *slog.Logger, balancer routing.Balancer, onExec ExecutionHandler, storeDir string, interval time.Duration) *Manager {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &Manager{
		store: store, log: log, balancer: balancer, onExec: onExec,
		storeDir: storeDir, interval: interval, adapters: map[string]Adapter{},
	}
}

func (m *Manager) SetHostOverride(host string) { m.hostOverride = host }

func (m *Manager) SetStatusListener(fn StatusListener) { m.onStatus = fn }

func (m *Manager) SetMarketDataListener(fn domain.MarketDataHandler) { m.onMD = fn }

func (m *Manager) SetFIXHandler(fn func(kind, adapterID, sessionID, msgType, direction, clOrdID, reason string, seq int)) {
	m.onFIX = fn
}

func (m *Manager) SetObserver(fn func(eventType, subtype, adapterID, message string)) {
	m.onObs = fn
}

func (m *Manager) StartAll(ctx context.Context) error {
	list, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	for _, meta := range list {
		if !meta.Enabled {
			continue
		}
		if err := m.startOne(ctx, meta); err != nil {
			m.log.Error("adapter start failed", "adapter_id", meta.ID, "error", err)
			_ = m.store.UpdateStatus(ctx, meta.ID, domain.AdapterOffline, false)
			_ = m.store.AddEvent(ctx, meta.ID, "START_FAILED", err.Error())
			if m.onObs != nil {
				m.onObs("ADAPTER", "ADAPTER_ERROR", meta.ID, err.Error())
			}
		}
	}
	go m.healthLoop(ctx)
	return nil
}

func (m *Manager) startOne(ctx context.Context, meta domain.Adapter) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.adapters[meta.ID]; ok {
		_ = existing.Stop(ctx)
		delete(m.adapters, meta.ID)
	}
	_ = m.store.MarkStarted(ctx, meta.ID)
	if m.hostOverride != "" {
		meta.Host = m.hostOverride
	}
	var ad Adapter
	switch meta.Type {
	case "arqa", "simulator":
		ad = arqa.New(meta, m.log, m.onExec, m.onMD, m.storeDir)
	default:
		return fmt.Errorf("unknown adapter type %s", meta.Type)
	}
	if impl, ok := ad.(*arqa.Adapter); ok && m.onFIX != nil {
		id := meta.ID
		impl.SetFIXHandler(func(kind, sessionID, msgType, direction, clOrdID, reason string, seq int) {
			m.onFIX(kind, id, sessionID, msgType, direction, clOrdID, reason, seq)
		})
	}
	if err := ad.Start(ctx); err != nil {
		return err
	}
	m.adapters[meta.ID] = ad
	_ = m.store.AddEvent(ctx, meta.ID, "STARTED", "adapter started")
	if m.onObs != nil {
		m.onObs("ADAPTER", "ADAPTER_STARTED", meta.ID, "adapter started")
	}
	return nil
}

func (m *Manager) StopAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, ad := range m.adapters {
		_ = ad.Stop(ctx)
		_ = m.store.UpdateStatus(ctx, id, domain.AdapterOffline, false)
		delete(m.adapters, id)
	}
}

func (m *Manager) healthLoop(ctx context.Context) {
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.checkAll(ctx)
		}
	}
}

func (m *Manager) checkAll(ctx context.Context) {
	m.mu.RLock()
	ids := make([]string, 0, len(m.adapters))
	for id := range m.adapters {
		ids = append(ids, id)
	}
	m.mu.RUnlock()

	for _, id := range ids {
		m.mu.RLock()
		ad := m.adapters[id]
		m.mu.RUnlock()
		if ad == nil {
			continue
		}
		prev := ad.Status()
		h, err := ad.HealthCheck(ctx)
		status := ad.Status()
		if err != nil && status != domain.AdapterStarting {
			status = domain.AdapterOffline
		}
		if h.SessionLoggedOn {
			status = domain.AdapterOnline
		}
		_ = m.store.UpdateStatus(ctx, id, status, h.SessionLoggedOn)
		_ = m.store.UpdateRuntime(ctx, id, ad.ActiveOrders(), ad.Meta().ProcessedOrders, ad.Meta().ErrorCount)
		if prev != status && m.onStatus != nil {
			m.onStatus(id, status)
			_ = m.store.AddEvent(ctx, id, "STATUS_CHANGED", string(status))
			if m.onObs != nil {
				sub := "ADAPTER_CONNECTED"
				if status == domain.AdapterOffline {
					sub = "ADAPTER_DISCONNECTED"
				}
				m.onObs("ADAPTER", sub, id, string(status))
			}
		}
	}
}

func (m *Manager) SelectAdapter(_ context.Context) (Adapter, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var cands []routing.Candidate
	for _, ad := range m.adapters {
		meta := ad.Meta()
		cands = append(cands, routing.Candidate{
			ID: meta.ID, Status: ad.Status(), Weight: meta.Weight,
			ActiveOrders: ad.ActiveOrders(), Priority: meta.Priority, Enabled: meta.Enabled,
		})
	}
	id, err := m.balancer.Select(cands)
	if err != nil {
		return nil, err
	}
	ad, ok := m.adapters[id]
	if !ok {
		return nil, fmt.Errorf("selected adapter missing: %s", id)
	}
	return ad, nil
}

func (m *Manager) Get(id string) (Adapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ad, ok := m.adapters[id]
	return ad, ok
}

func (m *Manager) ListRuntime(ctx context.Context) ([]domain.Adapter, error) {
	list, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for i := range list {
		if ad, ok := m.adapters[list[i].ID]; ok {
			list[i].Status = ad.Status()
			list[i].ActiveOrders = ad.ActiveOrders()
			list[i].ProcessedOrders = ad.Meta().ProcessedOrders
			list[i].ErrorCount = ad.Meta().ErrorCount
		}
	}
	return list, nil
}

func (m *Manager) AvailableCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, ad := range m.adapters {
		st := ad.Status()
		if st == domain.AdapterOnline || st == domain.AdapterDegraded {
			n++
		}
	}
	return n
}

func (m *Manager) Disable(ctx context.Context, id string) error {
	online, err := m.store.CountOnlineEnabled(ctx)
	if err != nil {
		return err
	}
	ad, ok := m.Get(id)
	if ok && (ad.Status() == domain.AdapterOnline || ad.Status() == domain.AdapterDegraded) && online <= 1 {
		return fmt.Errorf("cannot disable the last available adapter")
	}
	if err := m.store.SetEnabled(ctx, id, false); err != nil {
		return err
	}
	m.mu.Lock()
	if existing, ok := m.adapters[id]; ok {
		_ = existing.Stop(ctx)
		delete(m.adapters, id)
	}
	m.mu.Unlock()
	if m.onObs != nil {
		m.onObs("ADAPTER", "ADAPTER_STOPPED", id, "adapter disabled")
	}
	return m.store.UpdateStatus(ctx, id, domain.AdapterOffline, false)
}

func (m *Manager) Enable(ctx context.Context, id string) error {
	if err := m.store.SetEnabled(ctx, id, true); err != nil {
		return err
	}
	meta, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	return m.startOne(ctx, meta)
}

func (m *Manager) Restart(ctx context.Context, id string) error {
	meta, err := m.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if !meta.Enabled {
		return fmt.Errorf("adapter disabled")
	}
	return m.startOne(ctx, meta)
}

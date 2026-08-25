package observability

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
)

type Settings struct {
	FastMS         int
	NormalMS       int
	SlowMS         int
	ChannelSize    int
	FixRawLogs     bool
	AlertWindow    time.Duration
	Retention      Retention
	ErrorWarnPct   float64
	ErrorErrorPct  float64
	ErrorCritPct   float64
	LatencyWarnMS  int
	LatencyErrorMS int
	ExportMaxSync  int
}

func DefaultSettings() Settings {
	return Settings{
		FastMS: 100, NormalMS: 500, SlowMS: 2000,
		ChannelSize: 4096, AlertWindow: time.Minute,
		Retention:    Retention{APIDays: 15, BusinessDays: 15, AuthDays: 15, SystemDays: 15, ErrorDays: 15, DebugDays: 15, AuditDays: 15},
		ErrorWarnPct: 5, ErrorErrorPct: 10, ErrorCritPct: 20,
		LatencyWarnMS: 1000, LatencyErrorMS: 3000,
		ExportMaxSync: 5000,
	}
}

type Pipeline struct {
	store    *Store
	log      *slog.Logger
	hub      *realtime.Hub
	settings Settings
	ch       chan Event

	mu      sync.Mutex
	rateKey map[string]time.Time
}

func NewPipeline(store *Store, log *slog.Logger, hub *realtime.Hub, settings Settings) *Pipeline {
	if settings.ChannelSize <= 0 {
		settings.ChannelSize = 4096
	}
	if settings.AlertWindow <= 0 {
		settings.AlertWindow = time.Minute
	}
	return &Pipeline{
		store: store, log: log, hub: hub, settings: settings,
		ch:      make(chan Event, settings.ChannelSize),
		rateKey: map[string]time.Time{},
	}
}

func (p *Pipeline) Store() *Store { return p.store }

func (p *Pipeline) Settings() Settings { return p.settings }

func (p *Pipeline) Run(ctx context.Context) {
	_ = p.store.EnsurePartitions(ctx)
	{
		c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := p.store.ApplyRetention(c, p.settings.Retention); err != nil && p.log != nil {
			p.log.Warn("obs retention failed", "error", err)
		} else if p.log != nil {
			p.log.Info("obs retention applied", "days", p.settings.Retention.KeepDays())
		}
		cancel()
	}
	t := time.NewTicker(80 * time.Millisecond)
	defer t.Stop()
	retain := time.NewTicker(6 * time.Hour)
	defer retain.Stop()
	part := time.NewTicker(24 * time.Hour)
	defer part.Stop()
	alerts := time.NewTicker(time.Minute)
	defer alerts.Stop()
	buf := make([]Event, 0, 128)
	flush := func() {
		if len(buf) == 0 {
			return
		}
		batch := buf
		buf = make([]Event, 0, 128)
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := p.store.InsertBatch(c, batch); err != nil && p.log != nil {
			p.log.Error("obs insert failed", "error", err, "n", len(batch))
		}
		cancel()
		for i := range batch {
			p.afterPersist(batch[i])
		}
	}
	for {
		select {
		case <-ctx.Done():
			for {
				select {
				case e := <-p.ch:
					buf = append(buf, e)
				default:
					flush()
					return
				}
			}
		case e := <-p.ch:
			buf = append(buf, e)
			if len(buf) >= 64 {
				flush()
			}
		case <-t.C:
			flush()
		case <-retain.C:
			c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			if err := p.store.ApplyRetention(c, p.settings.Retention); err != nil && p.log != nil {
				p.log.Warn("obs retention failed", "error", err)
			}
			cancel()
		case <-part.C:
			_ = p.store.EnsurePartitions(context.Background())
		case <-alerts.C:
			p.checkBrokerThresholds(ctx)
		}
	}
}

func (p *Pipeline) Emit(e Event) {
	e.Normalize()
	select {
	case p.ch <- e:
	default:
		if e.Type == TypeBusiness || e.Severity == SeverityCritical {
			c, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			if err := p.store.Insert(c, e); err != nil && p.log != nil {
				p.log.Error("obs sync insert failed", "error", err, "type", e.Type, "subtype", e.Subtype)
			}
			cancel()
			p.afterPersist(e)
			return
		}
		if p.log != nil {
			p.log.Warn("obs channel full, dropping event", "type", e.Type, "subtype", e.Subtype)
		}
	}
}

func (p *Pipeline) afterPersist(e Event) {
	if p.hub != nil {
		p.hub.Broadcast("obs.event", map[string]any{
			"id": e.ID, "type": e.Type, "subtype": e.Subtype, "severity": e.Severity,
			"broker_id": e.BrokerID, "broker_name": e.BrokerName, "order_id": e.OrderID,
			"request_id": e.RequestID, "status_code": e.StatusCode, "message": e.Message,
			"timestamp": e.Timestamp, "adapter_id": e.AdapterID, "client_ip": e.ClientIP,
			"endpoint": e.Endpoint, "http_method": e.HTTPMethod, "result": e.Result,
			"error_code": e.ErrorCode,
		})
	}
	p.maybeNotify(e)
}

func shouldNotify(e Event) bool {
	if strings.HasPrefix(e.Endpoint, "/api/v1/admin/notifications") {
		return false
	}
	switch e.Severity {
	case SeverityCritical, SeverityError:
		return true
	case SeverityWarning:
		switch e.Subtype {
		case AuthFailed, AuthIPNotAllowed, AuthRateLimitExceeded, AuthHMACFailed,
			"ADAPTER_DISCONNECTED", "ADAPTER_OVERLOAD", "FIX_SESSION_DISCONNECTED",
			"ADAPTER_HIGH_LOAD", "RATE_LIMIT_NEAR_LIMIT":
			return true
		}
	}
	return false
}

func (p *Pipeline) maybeNotify(e Event) {
	if !shouldNotify(e) {
		return
	}
	code := e.ErrorCode
	if code == "" {
		code = e.Subtype
	}
	fp := strings.Join([]string{e.Severity, e.Type, e.Subtype, code, fmt.Sprintf("%v", e.BrokerID)}, "|")
	title := e.Message
	if title == "" {
		title = e.Subtype
	}
	n := Notification{
		Fingerprint: fp,
		Severity:    e.Severity,
		EventType:   e.Type,
		Subtype:     e.Subtype,
		ErrorCode:   code,
		BrokerID:    e.BrokerID,
		BrokerName:  e.BrokerName,
		Title:       title,
		Message:     e.ErrorMessage,
	}
	if n.Message == "" {
		n.Message = e.Message
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, created, err := p.store.UpsertNotification(ctx, n, p.settings.AlertWindow)
	if err != nil {
		if p.log != nil {
			p.log.Warn("obs notification failed", "error", err)
		}
		return
	}
	if p.hub != nil && (created || out.Count == 10 || out.Count == 100 || out.Count%100 == 0) {
		p.hub.Broadcast("notification.new", out)
	}
}

func (p *Pipeline) checkBrokerThresholds(ctx context.Context) {
	ids, err := p.store.DistinctBrokersSince(ctx, time.Now().Add(-5*time.Minute))
	if err != nil || len(ids) == 0 {
		return
	}
	for _, id := range ids {
		total, failed, avgMs, err := p.store.BrokerErrorRate(ctx, id, 5*time.Minute)
		if err != nil || total < 20 {
			continue
		}
		pct := float64(failed) * 100 / float64(total)
		bid := id
		switch {
		case pct >= p.settings.ErrorCritPct:
			p.Emit(Event{Type: TypeError, Subtype: CritHighErrorRate, Severity: SeverityCritical, BrokerID: &bid,
				Message: fmt.Sprintf("error rate %.1f%% over 5 minutes", pct), ErrorCode: CritHighErrorRate, FailureStage: "gateway"})
		case pct >= p.settings.ErrorErrorPct:
			p.Emit(Event{Type: TypeError, Subtype: CritHighErrorRate, Severity: SeverityError, BrokerID: &bid,
				Message: fmt.Sprintf("error rate %.1f%% over 5 minutes", pct), ErrorCode: CritHighErrorRate, FailureStage: "gateway"})
		case pct >= p.settings.ErrorWarnPct:
			p.Emit(Event{Type: TypeSystem, Subtype: CritHighErrorRate, Severity: SeverityWarning, BrokerID: &bid,
				Message: fmt.Sprintf("error rate %.1f%% over 5 minutes", pct), ErrorCode: CritHighErrorRate})
		}
		switch {
		case int(avgMs) >= p.settings.LatencyErrorMS && p.settings.LatencyErrorMS > 0:
			p.Emit(Event{Type: TypeError, Subtype: ErrTimeout, Severity: SeverityError, BrokerID: &bid,
				Message: fmt.Sprintf("average latency %.0f ms over 5 minutes", avgMs), ErrorCode: "HIGH_LATENCY"})
		case int(avgMs) >= p.settings.LatencyWarnMS && p.settings.LatencyWarnMS > 0:
			p.Emit(Event{Type: TypeSystem, Subtype: "ADAPTER_HIGH_LOAD", Severity: SeverityWarning, BrokerID: &bid,
				Message: fmt.Sprintf("average latency %.0f ms over 5 minutes", avgMs), ErrorCode: "HIGH_LATENCY"})
		}
	}
}

func (p *Pipeline) Throttled(key string, every time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if t, ok := p.rateKey[key]; ok && time.Since(t) < every {
		return true
	}
	p.rateKey[key] = time.Now()
	return false
}

func EncodeCursor(ts time.Time, id uuid.UUID) string {
	return ts.UTC().Format(time.RFC3339Nano) + "|" + id.String()
}

func DecodeCursor(s string) (time.Time, uuid.UUID, error) {
	i := strings.LastIndex(s, "|")
	if i <= 0 {
		return time.Time{}, uuid.Nil, fmt.Errorf("bad cursor")
	}
	ts, err := time.Parse(time.RFC3339Nano, s[:i])
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(s[i+1:])
	return ts, id, err
}

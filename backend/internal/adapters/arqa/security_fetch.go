package arqa

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/quickfix"
)

type secDefWaiter struct {
	reqID string
	mu    sync.Mutex
	items []domain.SecurityRef
	// expected > 0 means tag 393 is dictionary-wide total
	expected int
	done     chan struct{}
	closed   bool
	ping     chan struct{} // receives on every batch
}

func newSecDefWaiter(reqID string) *secDefWaiter {
	return &secDefWaiter{
		reqID: reqID,
		done:  make(chan struct{}),
		ping:  make(chan struct{}, 8),
	}
}

func (w *secDefWaiter) add(msgTotal int, batch []domain.SecurityRef) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	// If 393 > batch size, treat as total dictionary size (protocol wording).
	// If 393 == batch size, treat as per-message count (HowTo wording) → rely on idle.
	if msgTotal > len(batch) && w.expected == 0 {
		w.expected = msgTotal
	}
	w.items = append(w.items, batch...)
	select {
	case w.ping <- struct{}{}:
	default:
	}
	if w.expected > 0 && len(w.items) >= w.expected {
		w.finishLocked()
	}
}

func (w *secDefWaiter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finishLocked()
}

func (w *secDefWaiter) finishLocked() {
	if w.closed {
		return
	}
	w.closed = true
	close(w.done)
}

func (w *secDefWaiter) snapshot() []domain.SecurityRef {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]domain.SecurityRef, len(w.items))
	copy(out, w.items)
	return out
}

func (a *sessionApp) beginSecDefWait(reqID string) *secDefWaiter {
	w := newSecDefWaiter(reqID)
	a.secDefMu.Lock()
	a.secDefWait = w
	a.secDefMu.Unlock()
	return w
}

func (a *sessionApp) clearSecDefWait(reqID string) {
	a.secDefMu.Lock()
	if a.secDefWait != nil && a.secDefWait.reqID == reqID {
		a.secDefWait = nil
	}
	a.secDefMu.Unlock()
}

func (a *sessionApp) handleSecurityDefinition(msg *quickfix.Message) {
	reqID, totalHint, items := parseSecurityDefinitionMessage(msg)
	a.secDefMu.Lock()
	w := a.secDefWait
	a.secDefMu.Unlock()
	if w == nil || (reqID != "" && w.reqID != reqID) {
		a.log.Info("security definition (no waiter)", "adapter_id", a.adapterID, "req_id", reqID, "items", len(items), "total_hint", totalHint)
		return
	}
	a.log.Info("security definition batch", "adapter_id", a.adapterID, "req_id", reqID, "batch", len(items), "total_hint", totalHint)
	w.add(totalHint, items)
}

// FetchSecurities sends Security Definition Request (35=c) and collects Security Definition (35=d) replies.
func (a *Adapter) FetchSecurities(ctx context.Context) ([]domain.SecurityRef, error) {
	if !a.app.isLoggedOn() {
		return nil, fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	reqID := newSecurityReqID()
	w := a.app.beginSecDefWait(reqID)
	defer a.app.clearSecDefWait(reqID)

	msg := buildSecurityDefinitionRequest(reqID)
	if _, err := a.app.send(msg); err != nil {
		return nil, err
	}
	a.log.Info("security definition request sent", "adapter_id", a.meta.ID, "req_id", reqID)

	hardDeadline := time.NewTimer(60 * time.Second)
	defer hardDeadline.Stop()
	idle := time.NewTimer(3 * time.Second)
	idle.Stop()
	gotAny := false

	for {
		select {
		case <-ctx.Done():
			items := dedupeSecurities(w.snapshot())
			if len(items) == 0 {
				return nil, ctx.Err()
			}
			return items, nil
		case <-w.done:
			return dedupeSecurities(w.snapshot()), nil
		case <-w.ping:
			gotAny = true
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(3 * time.Second)
		case <-idle.C:
			if gotAny {
				w.finish()
				return dedupeSecurities(w.snapshot()), nil
			}
		case <-hardDeadline.C:
			items := dedupeSecurities(w.snapshot())
			if len(items) == 0 {
				return nil, fmt.Errorf("security definition timeout: no instruments from %s", a.meta.ID)
			}
			a.log.Warn("security definition hard timeout", "adapter_id", a.meta.ID, "count", len(items))
			return items, nil
		}
	}
}

func dedupeSecurities(in []domain.SecurityRef) []domain.SecurityRef {
	seen := make(map[string]struct{}, len(in))
	out := make([]domain.SecurityRef, 0, len(in))
	for _, s := range in {
		sym := firstNonEmpty(s.SecurityID, s.Symbol)
		if sym == "" {
			continue
		}
		key := s.Board + "|" + sym
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	return out
}

var _ = enum.MsgType_SECURITY_DEFINITION

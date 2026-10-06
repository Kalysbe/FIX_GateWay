package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/google/uuid"
)

type eventStore interface {
	ClaimBatch(context.Context, string, int) ([]domain.OutboxEvent, error)
	MarkDone(context.Context, int64) error
	MarkRetry(context.Context, int64, string, time.Duration) error
	MarkFailed(context.Context, int64, string) error
	ReclaimStale(context.Context, time.Duration) (int64, error)
	PendingCount(context.Context) (int64, error)
}

var errReconciliationRequired = errors.New("RECONCILIATION_REQUIRED")

type Worker struct {
	store    eventStore
	orders   *orders.Store
	manager  *adapters.Manager
	log      *slog.Logger
	workerID string
	interval time.Duration
	batch    int
	obs      observability.Emitter
	wake     chan struct{}
}

func NewWorker(store *Store, orderStore *orders.Store, manager *adapters.Manager, log *slog.Logger, workerID string, interval time.Duration, batch int) *Worker {
	if batch <= 0 {
		batch = 20
	}
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	return &Worker{
		store: store, orders: orderStore, manager: manager, log: log,
		workerID: workerID, interval: interval, batch: batch,
		wake: make(chan struct{}, 1),
	}
}

func (w *Worker) Run(ctx context.Context) {
	if reconciler, ok := w.store.(interface {
		ReconcileConfirmed(context.Context, int) (int, error)
	}); ok {
		reconcileCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			t := time.NewTicker(5 * time.Second)
			defer t.Stop()
			for {
				attemptCtx, cancel := context.WithTimeout(reconcileCtx, 2*time.Second)
				n, err := reconciler.ReconcileConfirmed(attemptCtx, 20)
				cancel()
				if err != nil && reconcileCtx.Err() == nil {
					w.log.Error("automatic FIX reconciliation failed", "resolved", n, "error", err)
				} else if n > 0 {
					w.log.Info("automatically reconciled FIX outcomes", "count", n)
				}
				select {
				case <-reconcileCtx.Done():
					return
				case <-t.C:
				}
			}
		}()
		defer func() { stop(); <-done }()
	}
	t := time.NewTicker(w.interval)
	defer t.Stop()
	w.Notify()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		case <-w.wake:
			w.tick(ctx)
		}
	}
}

// This is only a latency hint. The durable DB queue and periodic scan remain
// authoritative when notifications coalesce or the process restarts.
func (w *Worker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) tick(ctx context.Context) {
	defer func() {
		if count, err := w.store.PendingCount(ctx); err == nil {
			metrics.OutboxQueueSize.Set(float64(count))
		}
	}()
	if n, err := w.store.ReclaimStale(ctx, DefaultStaleAfter); err != nil {
		w.log.Error("outbox reclaim failed", "error", err)
	} else if n > 0 {
		w.log.Warn("reclaimed stale outbox events", "count", n)
	}
	events, err := w.store.ClaimBatch(ctx, w.workerID, w.batch)
	if err != nil {
		w.log.Error("outbox claim failed", "error", err)
		return
	}
	for _, ev := range events {
		w.finish(ctx, ev, w.process(ctx, ev))
	}
	if len(events) == w.batch && ctx.Err() == nil {
		w.Notify()
	}
}

// Pre-send failures remain retryable indefinitely. Uncertain wire outcomes
// remain durably parked for reconciliation, never automatically resent.
func (w *Worker) finish(ctx context.Context, ev domain.OutboxEvent, processErr error) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	var err error
	if processErr == nil {
		err = w.store.MarkDone(persistCtx, ev.ID)
	} else {
		w.log.Error("outbox process failed", "event_id", ev.ID, "error", processErr)
		w.emit(observability.Event{Type: observability.TypeError, Subtype: observability.ErrQueue, ErrorCode: "OUTBOX_PROCESS_FAILED", ErrorMessage: processErr.Error(), FailureStage: "outbox", Message: processErr.Error()})
		if errors.Is(processErr, errReconciliationRequired) {
			err = w.store.MarkFailed(persistCtx, ev.ID, processErr.Error())
		} else {
			delay := time.Duration(min(max(ev.Attempts, 1), 30)) * time.Second
			err = w.store.MarkRetry(persistCtx, ev.ID, processErr.Error(), delay)
		}
	}
	if err != nil {
		w.log.Error("outbox outcome persistence failed; event retained for reconciliation", "event_id", ev.ID, "error", err)
		w.emit(observability.Event{Type: observability.TypeError, Subtype: observability.ErrQueue, ErrorCode: "OUTBOX_PERSIST_FAILED", ErrorMessage: err.Error(), FailureStage: "outbox", Message: fmt.Sprintf("event %d outcome could not be persisted: %v", ev.ID, err)})
	}
}

func (w *Worker) process(ctx context.Context, ev domain.OutboxEvent) error {
	var payload struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		return err
	}
	orderID, err := uuid.Parse(payload.OrderID)
	if err != nil {
		return err
	}
	order, err := w.orders.Get(ctx, orderID)
	if err != nil {
		return err
	}
	if order.RequestID != "" || order.TraceID != "" {
		ctx = observability.WithIDs(ctx, order.RequestID, order.TraceID)
	}

	switch ev.EventType {
	case "ORDER_SEND":
		return w.processSend(ctx, order)
	case "ORDER_CANCEL":
		return w.processCancel(ctx, order)
	default:
		return fmt.Errorf("unknown outbox event %s", ev.EventType)
	}
}

func (w *Worker) processSend(ctx context.Context, order domain.Order) error {
	if !orders.ShouldAttemptFIXSend(order) {
		if order.FixSendAttemptedAt != nil && !orders.IsExternallyAccepted(order) && !orders.IsTerminal(order.Status) {
			return fmt.Errorf("%w: order %s has prior wire attempt without venue confirmation", errReconciliationRequired, order.ID)
		}
		return nil
	}

	var ad adapters.Adapter
	var err error
	if order.AdapterID != nil && *order.AdapterID != "" {
		var ok bool
		ad, ok = w.manager.Get(*order.AdapterID)
		if !ok || (ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded) {
			if err := w.orders.ClearRouteForFailover(ctx, order.ID, "assigned adapter unavailable before send"); err != nil {
				return err
			}
			order, err = w.orders.Get(ctx, order.ID)
			if err != nil {
				return err
			}
			ad = nil
		}
	}
	if ad == nil {
		ad, err = w.manager.SelectAdapter(ctx)
		if err != nil {
			w.emitOrder(order, observability.TypeError, observability.ErrAdapter, "NO_ADAPTER", err.Error(), "load_balancer")
			if w.manager.AvailableCount() == 0 {
				w.emitOrder(order, observability.TypeError, observability.CritAllAdaptersOffline, observability.CritAllAdaptersOffline, "all adapters offline", "load_balancer")
			}
			return err
		}
	}

	fixClOrdID := ""
	if order.FixClOrdID != nil {
		fixClOrdID = *order.FixClOrdID
	}
	if fixClOrdID == "" {
		fixClOrdID = fmt.Sprintf("G%s", order.ID.String()[:32])
	}

	routed, err := w.orders.AssignRoute(ctx, order.ID, ad.ID(), fixClOrdID)
	if err != nil {
		w.emitOrder(order, observability.TypeError, observability.ErrAdapter, "ROUTE_FAILED", err.Error(), "load_balancer")
		return err
	}
	w.emitOrder(routed, observability.TypeBusiness, observability.BizOrderRouted, "", "routed to "+ad.ID(), "load_balancer")
	w.emitOrder(routed, observability.TypeBusiness, observability.BizOrderAssigned, "", "assigned "+ad.ID(), "load_balancer")

	if err := w.orders.FenceSendAttempt(ctx, routed.ID); err != nil {
		if errors.Is(err, orders.ErrSendAlreadyAttempted) {
			return fmt.Errorf("%w: order %s send already fenced", errReconciliationRequired, routed.ID)
		}
		w.emitOrder(routed, observability.TypeError, observability.ErrDatabase, "SEND_FENCE_FAILED", err.Error(), "order_service")
		return err
	}

	metrics.AdapterRequests.WithLabelValues(ad.ID()).Inc()
	rawFIX, err := ad.SendOrder(ctx, routed)
	if err != nil {
		metrics.AdapterErrors.WithLabelValues(ad.ID()).Inc()
		w.emitOrder(routed, observability.TypeError, observability.ErrFIX, "FIX_SEND_FAILED", err.Error(), "fix_adapter")
		return fmt.Errorf("%w: FIX send for order %s: %v", errReconciliationRequired, routed.ID, err)
	}
	sent, err := w.orders.MarkSent(ctx, order.ID, ad.ID(), rawFIX)
	if err != nil {
		w.emitOrder(routed, observability.TypeError, observability.ErrDatabase, "MARK_SENT_FAILED", err.Error(), "order_service")
		return fmt.Errorf("%w: FIX sent but order update failed: %v", errReconciliationRequired, err)
	}
	w.emitOrder(sent, observability.TypeBusiness, observability.BizOrderSent, "", "sent to FIX adapter", "fix_adapter")
	metrics.OrdersProcessed.Inc()
	w.emitOrder(sent, observability.TypeFIX, "FIX_ORDER_SENT", "", "NewOrderSingle", "fix_adapter")
	return nil
}

func (w *Worker) processCancel(ctx context.Context, order domain.Order) error {
	if order.AdapterID == nil {
		err := fmt.Errorf("no adapter assigned for cancel")
		w.emitOrder(order, observability.TypeError, observability.ErrAdapter, "CANCEL_NO_ADAPTER", err.Error(), "fix_adapter")
		return err
	}
	ad, ok := w.manager.Get(*order.AdapterID)
	if !ok {
		err := fmt.Errorf("adapter %s not running", *order.AdapterID)
		w.emitOrder(order, observability.TypeError, observability.ErrAdapter, "CANCEL_ADAPTER_MISSING", err.Error(), "fix_adapter")
		return err
	}
	metrics.AdapterRequests.WithLabelValues(ad.ID()).Inc()
	if err := ad.CancelOrder(ctx, order); err != nil {
		metrics.AdapterErrors.WithLabelValues(ad.ID()).Inc()
		w.emitOrder(order, observability.TypeError, observability.ErrFIX, "FIX_CANCEL_FAILED", err.Error(), "fix_adapter")
		return fmt.Errorf("%w: cancel result for order %s: %v", errReconciliationRequired, order.ID, err)
	}
	w.emitOrder(order, observability.TypeFIX, "FIX_ORDER_SENT", "", "OrderCancelRequest", "fix_adapter")
	return nil
}

func (w *Worker) SetObserver(obs observability.Emitter) { w.obs = obs }

func (w *Worker) emit(e observability.Event) {
	if w.obs == nil {
		return
	}
	w.obs.Emit(e)
}

func (w *Worker) emitOrder(order domain.Order, typ, subtype, code, message, stage string) {
	oid := order.ID
	bid := order.BrokerID
	e := observability.Event{
		Type: typ, Subtype: subtype, BrokerID: &bid, OrderID: &oid,
		ClientOrderID: order.ClientOrderID, RequestID: order.RequestID, TraceID: order.TraceID,
		AdapterID: derefStr(order.AdapterID), Instrument: order.Symbol, Side: string(order.Side),
		Quantity: order.Quantity.String(), AccountID: order.TradingAccount,
		ErrorCode: code, ErrorMessage: message, FailureStage: stage, Message: message,
	}
	if order.Price != nil {
		e.Price = order.Price.String()
	}
	w.emit(e)
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

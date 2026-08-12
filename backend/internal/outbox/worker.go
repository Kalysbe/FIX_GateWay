package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/google/uuid"
)

type Worker struct {
	store    *Store
	orders   *orders.Store
	manager  *adapters.Manager
	log      *slog.Logger
	workerID string
	interval time.Duration
	batch    int
	maxRetry int
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
		workerID: workerID, interval: interval, batch: batch, maxRetry: 20,
	}
}

func (w *Worker) Run(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	events, err := w.store.ClaimBatch(ctx, w.workerID, w.batch)
	if err != nil {
		w.log.Error("outbox claim failed", "error", err)
		return
	}
	for _, ev := range events {
		if err := w.process(ctx, ev); err != nil {
			w.log.Error("outbox process failed", "event_id", ev.ID, "error", err)
			if ev.Attempts >= w.maxRetry {
				_ = w.store.MarkFailed(ctx, ev.ID, err.Error())
				continue
			}
			delay := time.Duration(ev.Attempts) * time.Second
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			_ = w.store.MarkRetry(ctx, ev.ID, err.Error(), delay)
			continue
		}
		_ = w.store.MarkDone(ctx, ev.ID)
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
	if orders.IsTerminal(order.Status) {
		return nil
	}
	if orders.IsExternallyAccepted(order) {
		return nil
	}
	if order.Status == domain.OrderSent {
		// Already sent — do not duplicate ClOrdID send.
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
		return err
	}
	rawFIX, err := ad.SendOrder(ctx, routed)
	if err != nil {
		_ = w.orders.ClearRouteForFailover(ctx, order.ID, "send failed: "+err.Error())
		return err
	}
	_, err = w.orders.MarkSent(ctx, order.ID, ad.ID(), rawFIX)
	return err
}

func (w *Worker) processCancel(ctx context.Context, order domain.Order) error {
	if order.AdapterID == nil {
		return fmt.Errorf("no adapter assigned for cancel")
	}
	ad, ok := w.manager.Get(*order.AdapterID)
	if !ok {
		return fmt.Errorf("adapter %s not running", *order.AdapterID)
	}
	return ad.CancelOrder(ctx, order)
}

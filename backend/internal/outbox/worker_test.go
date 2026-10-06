package outbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
)

type outcomeStore struct {
	action     string
	delay      time.Duration
	reason     string
	err        error
	contextErr error
}

type backgroundReconciliationStore struct {
	outcomeStore
	started chan struct{}
	claimed chan struct{}
}

func (s *backgroundReconciliationStore) ReconcileConfirmed(ctx context.Context, limit int) (int, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return 0, ctx.Err()
}

func (s *backgroundReconciliationStore) ClaimBatch(context.Context, string, int) ([]domain.OutboxEvent, error) {
	select {
	case s.claimed <- struct{}{}:
	default:
	}
	return nil, nil
}

func TestReconciliationDoesNotBlockDispatchAndStopsWithWorker(t *testing.T) {
	s := &backgroundReconciliationStore{started: make(chan struct{}, 1), claimed: make(chan struct{}, 1)}
	w := &Worker{store: s, log: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), interval: time.Millisecond, batch: 20, wake: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	select {
	case <-s.started:
	case <-time.After(time.Second):
		t.Fatal("reconciliation did not start")
	}
	select {
	case <-s.claimed:
	case <-time.After(time.Second):
		t.Fatal("reconciliation blocked dispatch")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker left reconciliation running after shutdown")
	}
}

func (s *outcomeStore) ClaimBatch(context.Context, string, int) ([]domain.OutboxEvent, error) {
	return nil, nil
}
func (s *outcomeStore) ReclaimStale(context.Context, time.Duration) (int64, error) { return 0, nil }
func (s *outcomeStore) PendingCount(context.Context) (int64, error)                { return 0, nil }
func (s *outcomeStore) record(ctx context.Context, action, reason string) error {
	s.action = action
	s.reason = reason
	s.contextErr = ctx.Err()
	return s.err
}
func (s *outcomeStore) MarkDone(ctx context.Context, _ int64) error { return s.record(ctx, "done", "") }
func (s *outcomeStore) MarkRetry(ctx context.Context, _ int64, reason string, delay time.Duration) error {
	s.delay = delay
	return s.record(ctx, "retry", reason)
}
func (s *outcomeStore) MarkFailed(ctx context.Context, _ int64, reason string) error {
	return s.record(ctx, "reconcile", reason)
}

func TestOutcomePersistence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		processErr error
		attempts   int
		want       string
		delay      time.Duration
	}{
		{"ordinary outage beyond previous retry limit", errors.New("no adapter"), 100, "retry", 30 * time.Second},
		{"first transient failure", errors.New("database unavailable"), 1, "retry", time.Second},
		{"ambiguous delivery", fmt.Errorf("%w: connection lost", errReconciliationRequired), 1, "reconcile", 0},
		{"successful dispatch", nil, 1, "done", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			s := &outcomeStore{}
			w := &Worker{store: s, log: slog.New(slog.NewTextHandler(&logs, nil))}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			w.finish(ctx, domain.OutboxEvent{ID: 42, Attempts: tc.attempts}, tc.processErr)
			if s.action != tc.want || s.delay != tc.delay {
				t.Fatalf("action=%s delay=%s", s.action, s.delay)
			}
			if s.contextErr != nil {
				t.Fatalf("shutdown prevented durable outcome: %v", s.contextErr)
			}
			if tc.processErr != nil && s.reason != tc.processErr.Error() {
				t.Fatalf("failure reason lost: %s", s.reason)
			}
		})
	}
}

func TestOutcomePersistenceFailureIsVisible(t *testing.T) {
	for _, processErr := range []error{nil, errors.New("offline"), errReconciliationRequired} {
		var logs bytes.Buffer
		s := &outcomeStore{err: errors.New("database down")}
		w := &Worker{store: s, log: slog.New(slog.NewTextHandler(&logs, nil))}
		w.finish(context.Background(), domain.OutboxEvent{ID: 42, Attempts: 1}, processErr)
		if !strings.Contains(logs.String(), "outcome persistence failed") {
			t.Fatalf("persistence failure ignored: %s", logs.String())
		}
	}
}

func TestFencedSendNeedsVenueConfirmation(t *testing.T) {
	now := time.Now()
	w := &Worker{} // No stores/adapter: these paths must never attempt another send.
	for _, status := range []domain.OrderStatus{domain.OrderRouting, domain.OrderSent} {
		err := w.processSend(context.Background(), domain.Order{ID: uuid.New(), Status: status, FixSendAttemptedAt: &now})
		if !errors.Is(err, errReconciliationRequired) {
			t.Fatalf("status %s: uncertain send treated as complete: %v", status, err)
		}
	}
	for _, status := range []domain.OrderStatus{domain.OrderAcknowledged, domain.OrderFilled, domain.OrderCancelled, domain.OrderRejected} {
		if err := w.processSend(context.Background(), domain.Order{ID: uuid.New(), Status: status, FixSendAttemptedAt: &now}); err != nil {
			t.Fatalf("confirmed status %s: %v", status, err)
		}
	}
}

type scanStore struct {
	outcomeStore
	scanned chan struct{}
}

func (s *scanStore) ClaimBatch(context.Context, string, int) ([]domain.OutboxEvent, error) {
	select {
	case s.scanned <- struct{}{}:
	default:
	}
	return nil, nil
}
func TestWakeupScansDurableQueueBeforePollAndCoalesces(t *testing.T) {
	w := NewWorker(nil, nil, nil, slog.Default(), "test", time.Hour, 20)
	for i := 0; i < 10000; i++ {
		w.Notify()
	}
	if len(w.wake) != 1 {
		t.Fatal("unbounded or lost wake signal")
	}
	s := &scanStore{scanned: make(chan struct{}, 2)}
	w.store = s
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-s.scanned:
	case <-time.After(time.Second):
		t.Fatal("startup queue scan waited for polling interval")
	}
	w.Notify()
	select {
	case <-s.scanned:
	case <-time.After(time.Second):
		t.Fatal("committed enqueue did not wake worker")
	}
}

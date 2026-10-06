package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) ClaimBatch(ctx context.Context, workerID string, limit int) ([]domain.OutboxEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, event_type, payload, status, attempts,
		       available_at, locked_by, locked_at, last_error, created_at, updated_at
		FROM outbox_events
		WHERE status = 'PENDING' AND available_at <= now()
		ORDER BY id
		FOR UPDATE SKIP LOCKED
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	var ids []int64
	for rows.Next() {
		var e domain.OutboxEvent
		if err := rows.Scan(&e.ID, &e.AggregateType, &e.AggregateID, &e.EventType, &e.Payload, &e.Status,
			&e.Attempts, &e.AvailableAt, &e.LockedBy, &e.LockedAt, &e.LastError, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
		ids = append(ids, e.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `
		UPDATE outbox_events
		SET status='PROCESSING', locked_by=$2, locked_at=now(), attempts=attempts+1, updated_at=now()
		WHERE id = ANY($1)`, ids, workerID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	for i := range events {
		events[i].Status = domain.OutboxProcessing
		events[i].Attempts++
	}
	return events, nil
}

func (s *Store) MarkDone(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE outbox_events SET status='DONE', locked_by=NULL, locked_at=NULL, last_error=NULL, updated_at=now()
		WHERE id=$1 AND status='PROCESSING'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("outbox event %d is missing or no longer PROCESSING", id)
	}
	return nil
}

func (s *Store) MarkRetry(ctx context.Context, id int64, errMsg string, delay time.Duration) error {
	secs := int64(delay / time.Second)
	if secs < 1 {
		secs = 1
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE outbox_events
		SET status='PENDING', locked_by=NULL, locked_at=NULL, last_error=$2,
		    available_at=now() + make_interval(secs => $3::int), updated_at=now()
		WHERE id=$1 AND status='PROCESSING'`, id, errMsg, secs)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("outbox event %d is missing or no longer PROCESSING", id)
	}
	return nil
}

func (s *Store) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE outbox_events
		SET status='FAILED', locked_by=NULL, locked_at=NULL, last_error=$2, updated_at=now()
		WHERE id=$1 AND status='PROCESSING'`, id, errMsg)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("outbox event %d is missing or no longer PROCESSING", id)
	}
	return nil
}

const DefaultStaleAfter = 2 * time.Minute

// ReclaimStale retries only a send whose durable order fence proves that no
// wire attempt began. Cancels and fenced sends require reconciliation.
func (s *Store) ReclaimStale(ctx context.Context, staleAfter time.Duration) (int64, error) {
	if staleAfter <= 0 {
		staleAfter = DefaultStaleAfter
	}
	secs := int64(staleAfter / time.Second)
	if secs < 1 {
		secs = 1
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE outbox_events e
        SET status=CASE WHEN e.event_type='ORDER_SEND' AND EXISTS (
                SELECT 1 FROM orders o WHERE o.id::text = e.payload->>'order_id'
                AND o.fix_send_attempted_at IS NULL
            ) THEN 'PENDING' ELSE 'FAILED' END,
            locked_by=NULL, locked_at=NULL,
            last_error=CASE WHEN e.event_type='ORDER_SEND' AND EXISTS (
                SELECT 1 FROM orders o WHERE o.id::text = e.payload->>'order_id'
                AND o.fix_send_attempted_at IS NULL
            ) THEN 'reclaimed stale pre-send PROCESSING'
            ELSE 'RECONCILIATION_REQUIRED: stale PROCESSING; wire result unknown; ' || COALESCE(e.last_error, '') END,
		    available_at=now(), updated_at=now()
		WHERE status='PROCESSING' AND locked_at IS NOT NULL
		  AND locked_at < now() - make_interval(secs => $1::int)`, secs)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Store) PendingCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM outbox_events WHERE status IN ('PENDING','PROCESSING','FAILED')`).Scan(&n)
	return n, err
}

// Dispatcher abstracts the outbound queue so Order Service does not depend on outbox internals.
// Future RabbitMQ/Redis implementations can satisfy the same interface.
type Dispatcher interface {
	// Enqueue is intentionally unused by Order Service create path (same TX insert).
	// Kept for cancel/replace helpers and future producers.
	Enqueue(ctx context.Context, aggregateType, aggregateID, eventType string, payload []byte) error
}

type PGDispatcher struct {
	pool *pgxpool.Pool
}

func NewPGDispatcher(pool *pgxpool.Pool) *PGDispatcher {
	return &PGDispatcher{pool: pool}
}

func (d *PGDispatcher) Enqueue(ctx context.Context, aggregateType, aggregateID, eventType string, payload []byte) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
		VALUES ($1,$2,$3,$4)`, aggregateType, aggregateID, eventType, payload)
	return err
}

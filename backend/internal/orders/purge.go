package orders

import (
	"context"
	"log/slog"
	"time"
)

const (
	// FinishedOrderGrace outlives the wait of POST /orders and of cancel for the
	// QUIK answer (ORDER_ANSWER_TIMEOUT, bounded by the HTTP write timeout). It is
	// the shortest allowed; FINISHED_ORDER_GRACE can only lengthen it.
	FinishedOrderGrace = 2 * time.Minute
	purgeInterval      = 30 * time.Second
)

// PurgeFinished deletes orders QUIK has finished with, together with their
// events and queue rows; the rows of QUIK Orders remain the record. An order goes
// when QUIK closed it, or when a DAY order it accepted belongs to a past trading
// day. An order QUIK has not answered stays: only reconciliation can settle it.
func (s *Store) PurgeFinished(ctx context.Context, grace time.Duration) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
WITH finished AS (
	SELECT o.id FROM orders o
	WHERE (
		(o.status IN ('FILLED','CANCELLED','EXPIRED','REJECTED','FAILED') AND o.updated_at < now() - make_interval(secs => $1::double precision))
		OR (o.status IN ('ACKNOWLEDGED','PARTIALLY_FILLED','CANCEL_PENDING')
			AND (o.created_at AT TIME ZONE 'Asia/Bishkek')::date < (now() AT TIME ZONE 'Asia/Bishkek')::date)
	) AND NOT EXISTS (SELECT 1 FROM outbox_events e WHERE e.aggregate_id = o.id::text AND e.status IN ('PENDING','PROCESSING'))
	FOR UPDATE OF o SKIP LOCKED
), queue AS (
	DELETE FROM outbox_events e USING finished f WHERE e.aggregate_type = 'order' AND e.aggregate_id = f.id::text
)
DELETE FROM orders o USING finished f WHERE o.id = f.id`, grace.Seconds())
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RunPurge keeps the table down to the orders QUIK still has open.
func (s *Store) RunPurge(ctx context.Context, log *slog.Logger, grace time.Duration) {
	grace = max(grace, FinishedOrderGrace)
	ticker := time.NewTicker(purgeInterval)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		n, err := s.PurgeFinished(work, grace)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Warn("finished orders purge failed; will retry", "error", err)
		} else if n > 0 {
			log.Info("purged orders finished in QUIK", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

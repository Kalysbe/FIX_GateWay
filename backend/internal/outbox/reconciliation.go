package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var ErrReconciliationConflict = errors.New("case is no longer open or has no conclusive FIX evidence")
var ErrReconciliationNote = errors.New("operator and a note of 1–2000 characters are required")

type ReconciliationCase struct {
	EventID         int64      `json:"event_id"`
	OrderID         uuid.UUID  `json:"order_id"`
	EventType       string     `json:"event_type"`
	ClientOrderID   string     `json:"client_order_id"`
	BrokerID        int64      `json:"broker_id"`
	OrderStatus     string     `json:"order_status"`
	LastError       string     `json:"last_error"`
	CreatedAt       time.Time  `json:"created_at"`
	EvidenceEventID *int64     `json:"evidence_event_id"`
	ResolvedAt      *time.Time `json:"resolved_at"`
	ActorID         *string    `json:"actor_id"`
	Note            *string    `json:"note"`
}

// Only applied reports count. SENT, a venue ID alone, and ignored illegal
// transitions do not establish an outcome. A cancel requires a terminal report;
// an ordinary acknowledgement cannot establish that the cancel succeeded.
const evidenceSQL = `SELECT oe.id FROM order_events oe
 WHERE oe.order_id=o.id AND oe.created_at>=e.created_at
 AND oe.event_type='ORDER_EXECUTION_REPORT'
 AND oe.to_status IN ('ACKNOWLEDGED','PARTIALLY_FILLED','FILLED','CANCELLED','REJECTED','EXPIRED')
 AND (e.event_type='ORDER_SEND' OR (e.event_type='ORDER_CANCEL'
      AND oe.to_status IN ('FILLED','CANCELLED','REJECTED','EXPIRED')))
 ORDER BY oe.id DESC LIMIT 1`

// ReconcileConfirmed examines only cases with conclusive evidence, so older
// unresolved cases cannot starve confirmed cases. ResolveReconciliation checks
// again under locks and records the same durable history as an operator action.
func (s *Store) ReconcileConfirmed(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id FROM outbox_events e
 JOIN orders o ON o.id::text=e.payload->>'order_id'
 WHERE e.status='FAILED' AND e.last_error LIKE 'RECONCILIATION_REQUIRED:%'
 AND (`+evidenceSQL+`) IS NOT NULL ORDER BY e.id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	resolved := 0
	var failures []error
	for _, id := range ids {
		err := s.ResolveReconciliation(ctx, id, "system:fix-reconciliation", "Automatically reconciled against a persisted FIX execution report")
		if errors.Is(err, ErrReconciliationConflict) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("reconcile event %d: %w", id, err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		resolved++
	}
	return resolved, errors.Join(failures...)
}

func (s *Store) ListReconciliation(ctx context.Context, orderID *uuid.UUID, limit int) ([]ReconciliationCase, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT e.id,o.id,e.event_type,o.client_order_id,o.broker_id,o.status,
 COALESCE(r.previous_error,e.last_error,''),e.created_at,
 COALESCE(r.evidence_event_id,(`+evidenceSQL+`)),r.created_at,r.actor_id,r.note
 FROM outbox_events e JOIN orders o ON o.id::text=e.payload->>'order_id'
 LEFT JOIN outbox_reconciliations r ON r.event_id=e.id
 WHERE ($1::uuid IS NULL OR o.id=$1) AND
 ((e.status='FAILED' AND e.last_error LIKE 'RECONCILIATION_REQUIRED:%') OR r.id IS NOT NULL)
 ORDER BY (r.id IS NULL) DESC,e.id DESC LIMIT $2`, orderID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cases := make([]ReconciliationCase, 0)
	for rows.Next() {
		var c ReconciliationCase
		if err := rows.Scan(&c.EventID, &c.OrderID, &c.EventType, &c.ClientOrderID, &c.BrokerID, &c.OrderStatus, &c.LastError, &c.CreatedAt, &c.EvidenceEventID, &c.ResolvedAt, &c.ActorID, &c.Note); err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, rows.Err()
}

// Closing never changes the order, clears its send fence or requeues a request.
// Evidence, operator history and queue completion commit atomically.
func (s *Store) ResolveReconciliation(ctx context.Context, eventID int64, actorID, note string) error {
	note = strings.TrimSpace(note)
	if strings.TrimSpace(actorID) == "" || note == "" || utf8.RuneCountInString(note) > 2000 {
		return ErrReconciliationNote
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var orderID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT o.id FROM orders o JOIN outbox_events e ON o.id::text=e.payload->>'order_id' WHERE e.id=$1 FOR UPDATE OF o`, eventID).Scan(&orderID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrReconciliationConflict
	}
	if err != nil {
		return err
	}
	var previousError string
	err = tx.QueryRow(ctx, `SELECT last_error FROM outbox_events WHERE id=$1 AND status='FAILED' AND last_error LIKE 'RECONCILIATION_REQUIRED:%' FOR UPDATE`, eventID).Scan(&previousError)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrReconciliationConflict
	}
	if err != nil {
		return err
	}
	var evidenceID *int64
	err = tx.QueryRow(ctx, `SELECT (`+evidenceSQL+`) FROM outbox_events e JOIN orders o ON o.id::text=e.payload->>'order_id' WHERE e.id=$1`, eventID).Scan(&evidenceID)
	if err != nil {
		return err
	}
	if evidenceID == nil {
		return ErrReconciliationConflict
	}
	_, err = tx.Exec(ctx, `INSERT INTO outbox_reconciliations(event_id,order_id,evidence_event_id,actor_id,note,previous_error) VALUES($1,$2,$3,$4,$5,$6)`, eventID, orderID, evidenceID, actorID, note, previousError)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE outbox_events SET status='DONE',locked_by=NULL,locked_at=NULL,last_error=NULL,updated_at=now() WHERE id=$1`, eventID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

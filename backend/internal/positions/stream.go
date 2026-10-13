package positions

import (
	"context"
	"errors"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/jackc/pgx/v5"
)

// SaveStreamSnapshot atomically replaces the materialized snapshot. Readers
// see the previous complete state or the next complete state, never half a batch.
func (s *Store) SaveStreamSnapshot(ctx context.Context, adapterID, reqID string, items []domain.FIXPosition) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `SELECT id FROM fix_position_snapshots WHERE adapter_id=$1 AND pos_req_id=$2 ORDER BY id DESC LIMIT 1 FOR UPDATE`, adapterID, reqID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO fix_position_snapshots(adapter_id,pos_req_id,clearing_date,status) VALUES($1,$2,$3,'PENDING') RETURNING id`, adapterID, reqID, time.Now().Format("20060102")).Scan(&id)
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM fix_positions WHERE snapshot_id=$1`, id); err != nil {
		return err
	}
	if err := insertPositions(ctx, tx, id, items); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE fix_position_snapshots SET status='OK', report_count=$2, requested_at=now(),completed_at=now() WHERE id=$1`, id, len(items)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

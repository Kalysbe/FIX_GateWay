package tradeledger

import (
	"context"
	"time"
)

type Health struct {
	Incomplete   int
	ReportErrors int
	LastSuccess  *time.Time
	LatestError  string
}

func (s *Store) Health(ctx context.Context) (Health, error) {
	var h Health
	err := s.Pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM trades t WHERE t.created_at < now()-interval '2 minutes'
 AND (SELECT count(*) FROM trade_sides s WHERE s.trade_id=t.id)<>2),
(SELECT count(*) FROM trade_reports WHERE status='ERROR'),
(SELECT max(finished_at) FROM trade_sync_runs WHERE finished_at IS NOT NULL AND error='' AND errors=0 AND records>0),
COALESCE((SELECT error FROM trade_sync_runs WHERE finished_at IS NOT NULL ORDER BY id DESC LIMIT 1),'')`).Scan(&h.Incomplete, &h.ReportErrors, &h.LastSuccess, &h.LatestError)
	return h, err
}

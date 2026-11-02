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
	classes, err := s.servedClasses(ctx)
	if err != nil {
		return h, err
	}
	err = s.Pool.QueryRow(ctx, `SELECT
(SELECT count(*) FROM trades t WHERE t.created_at < now()-interval '2 minutes'
 AND ($1::text[] IS NULL OR btrim(t.board)=ANY($1::text[]))
 AND (SELECT count(*) FROM trade_sides s WHERE s.trade_id=t.id AND s.has_quik_record)=1),
(SELECT count(*) FROM trade_reports WHERE status='ERROR'),
(SELECT max(finished_at) FROM trade_sync_runs WHERE finished_at IS NOT NULL AND error='' AND errors=0 AND records>0),
COALESCE((SELECT error FROM trade_sync_runs WHERE finished_at IS NOT NULL ORDER BY id DESC LIMIT 1),'')`, classes).Scan(&h.Incomplete, &h.ReportErrors, &h.LastSuccess, &h.LatestError)
	return h, err
}

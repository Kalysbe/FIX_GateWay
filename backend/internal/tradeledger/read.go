package tradeledger

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

const sideJSON = `SELECT jsonb_build_object(
'id',s.id,'ledger_trade_id',t.id,'execution_id',s.execution_id,'order_id',s.order_id,
'exec_id',s.exec_id,'trade_match_id',t.trade_match_id,'quik_trade_num',t.quik_trade_num,
'execution_date',t.trade_date::text,'symbol',t.symbol,'board',t.board,'side',s.side,
'last_qty',t.quantity::text,'last_px',t.price::text,'cum_qty',s.cum_qty::text,'leaves_qty',s.leaves_qty::text,
'created_at',COALESCE(t.traded_at,t.trade_date::timestamp AT TIME ZONE 'UTC'),'received_at',s.created_at,'trade_time_known',t.traded_at IS NOT NULL,
'quik_order_id',NULLIF(s.quik_order_id,''),'quik_order_num',s.quik_order_num,'fix_cl_ord_id',NULLIF(s.fix_cl_ord_id,''),
'quik_firm',s.firm_id,'firm_name',COALESCE(NULLIF(s.firm_name,''),f.firm_name,''),'client_code',s.client_code,
'account',s.account,'contra_broker',s.contra_broker,'adapter_id',NULLIF(s.adapter_id,''),
'client_order_id',o.client_order_id,'broker_id',o.broker_id,'has_fix_report',s.has_fix_report,'has_quik_record',s.has_quik_record)
FROM trades t JOIN trade_sides s ON s.trade_id=t.id
LEFT JOIN orders o ON o.id=s.order_id LEFT JOIN quik_firms f ON f.firm_id=s.firm_id `

func (s *Store) List(ctx context.Context, limit int) ([]json.RawMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	return s.query(ctx, sideJSON+`WHERE t.id IN (SELECT id FROM trades ORDER BY trade_date DESC,length(quik_trade_num) DESC,quik_trade_num DESC,id LIMIT $1)
ORDER BY t.trade_date DESC,length(t.quik_trade_num) DESC,t.quik_trade_num DESC,s.side`, limit)
}
func (s *Store) Get(ctx context.Context, id uuid.UUID) ([]json.RawMessage, error) {
	return s.query(ctx, sideJSON+`WHERE t.id=$1 OR t.id IN(SELECT trade_id FROM trade_sides WHERE id=$1 OR execution_id=$1) ORDER BY s.side`, id)
}
func (s *Store) query(ctx context.Context, sql string, args ...any) ([]json.RawMessage, error) {
	rows, err := s.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]json.RawMessage, 0)
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
}
func (s *Store) Coverage(ctx context.Context) (json.RawMessage, error) {
	var out json.RawMessage
	err := s.Pool.QueryRow(ctx, `WITH counts AS(SELECT t.id,count(s.id) sides FROM trades t LEFT JOIN trade_sides s ON s.trade_id=t.id GROUP BY t.id),
last_sync AS(SELECT * FROM trade_sync_runs ORDER BY id DESC LIMIT 1)
SELECT jsonb_build_object('trades',count(*),'complete',count(*) FILTER(WHERE sides=2),'incomplete',count(*) FILTER(WHERE sides<>2),
'sides',COALESCE(sum(sides),0),'report_errors',(SELECT count(*) FROM trade_reports WHERE status='ERROR'),
'last_sync_at',(SELECT finished_at FROM last_sync),'last_sync_records',(SELECT records FROM last_sync),
'last_sync_error',(SELECT error FROM last_sync),'sync_running',COALESCE((SELECT finished_at IS NULL FROM last_sync),false),
'last_success_at',(SELECT max(finished_at) FROM trade_sync_runs WHERE error='' AND errors=0 AND records>0),
'sync_stale',COALESCE((SELECT max(finished_at)<now()-interval '5 minutes' FROM trade_sync_runs WHERE error='' AND errors=0 AND records>0),true)) FROM counts`).Scan(&out)
	return out, err
}

func (s *Store) FinishRun(ctx context.Context, id int64, records, failures int, problem error) error {
	message := ""
	if problem != nil {
		message = problem.Error()
	}
	_, err := s.Pool.Exec(ctx, `UPDATE trade_sync_runs SET finished_at=now(),records=$2,errors=$3,error=$4 WHERE id=$1`, id, records, failures, message)
	return err
}
func (s *Store) StartRun(ctx context.Context, url string) (int64, error) {
	var id int64
	err := s.Pool.QueryRow(ctx, `INSERT INTO trade_sync_runs(source_url) VALUES($1) RETURNING id`, url).Scan(&id)
	return id, err
}
func snapshotError(n int) error {
	return fmt.Errorf("QUIK snapshot has %d trades without both sides", n)
}

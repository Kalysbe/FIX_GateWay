package tradeledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool    *pgxpool.Pool
	classes func(context.Context) ([]string, error)
}

func NewStore(pool *pgxpool.Pool) *Store { return &Store{Pool: pool} }

// SetClasses limits the ledger to the served QUIK classes; it is called once
// before the store is used. Without it, or with an empty choice, every class is served.
func (s *Store) SetClasses(classes func(context.Context) ([]string, error)) { s.classes = classes }

// servedClasses returns nil when every class is served: the queries read NULL so.
func (s *Store) servedClasses(ctx context.Context) ([]string, error) {
	if s.classes == nil {
		return nil, nil
	}
	list, err := s.classes(ctx)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list, nil
}

func (s *Store) archive(ctx context.Context, source, adapter string, payload json.RawMessage) (int64, string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(payload, &object); err != nil {
		return 0, "", err
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return 0, "", err
	}
	sum := sha256.Sum256(append([]byte(adapter+":"), canonical...))
	key := hex.EncodeToString(sum[:])
	var id int64
	var state string
	// Most reconciliation polls have no new rows. Do not rewrite stored evidence.
	err = s.Pool.QueryRow(ctx, `SELECT id,status FROM trade_reports WHERE source=$1 AND source_key=$2`, source, key).Scan(&id, &state)
	if err == nil {
		return id, state, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", err
	}
	err = s.Pool.QueryRow(ctx, `INSERT INTO trade_reports(source,source_key,payload,adapter_id) VALUES($1,$2,$3,$4)
ON CONFLICT(source,source_key) DO UPDATE SET source_key=EXCLUDED.source_key RETURNING id,status`, source, key, payload, adapter).Scan(&id, &state)
	return id, state, err
}

func (s *Store) failure(ctx context.Context, id int64, err error) error {
	_, e := s.Pool.Exec(ctx, `UPDATE trade_reports SET status='ERROR',error=$2 WHERE id=$1`, id, err.Error())
	return errors.Join(err, e)
}

// RecordFIX keeps the original report as evidence, independently of the local
// order state machine. Trades come only from QUIK public."Trades": a FIX report
// never creates a trade or a side.
func (s *Store) RecordFIX(ctx context.Context, r domain.ExecutionReport) error {
	if r.RawFIX == "" {
		return fmt.Errorf("original FIX report is required; delivery retained for reconciliation")
	}
	raw, _ := json.Marshal(map[string]string{"raw_fix": r.RawFIX})
	id, _, err := s.archive(ctx, "fix", r.AdapterID, raw)
	if err != nil {
		return err
	}
	_, fill, err := ParseFIX(r.RawFIX, r.AdapterID)
	if err != nil {
		return s.failure(ctx, id, err)
	}
	status := "NO_FILL"
	if fill {
		status = "ARCHIVED"
	}
	_, err = s.Pool.Exec(ctx, `UPDATE trade_reports SET status=$2,error='' WHERE id=$1`, id, status)
	return err
}

func (s *Store) RecordQUIK(ctx context.Context, raw json.RawMessage) error {
	id, state, err := s.archive(ctx, "quik", "", raw)
	if err != nil {
		return err
	}
	if state == "RECORDED" {
		return nil
	}
	p, err := ParseQUIK(raw)
	if err == nil {
		err = s.record(ctx, id, p)
	}
	if err != nil {
		return s.failure(ctx, id, err)
	}
	return nil
}

func (s *Store) record(ctx context.Context, report int64, p Input) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var trade uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO trades(trade_match_id,quik_trade_num,trade_date,symbol,board,quantity,price,traded_at)
VALUES($1,$2,$3::date,$4,$5,$6,$7,$8) ON CONFLICT(trade_match_id,trade_date,symbol,board)
DO UPDATE SET traded_at=COALESCE(trades.traded_at,EXCLUDED.traded_at),updated_at=now()
WHERE trades.quantity=EXCLUDED.quantity AND trades.price=EXCLUDED.price AND trades.quik_trade_num=EXCLUDED.quik_trade_num RETURNING id`,
		p.Match, p.Number, p.Date, p.Symbol, p.Board, p.Qty, p.Price, p.Time).Scan(&trade)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("conflicting quantity/price for trade %s", p.Match)
	}
	if err != nil {
		return err
	}
	var order, execution *uuid.UUID
	candidate := p.OrderID
	if candidate == "" {
		d, _ := time.Parse(time.DateOnly, p.Date)
		candidate = d.Format("060102") + "-" + p.Board + "-" + p.OrderNumber
	}
	rows, err := tx.Query(ctx, `SELECT o.id,o.quik_order_id FROM orders o JOIN instruments i ON i.id=o.instrument_id
WHERE (o.quik_order_id=$1 OR NULLIF($2,'')=o.fix_cl_ord_id) AND o.side=$3 AND i.quik_symbol=$4
AND COALESCE(NULLIF(o.board,''),i.board)=$5 AND ($6='' OR o.fix_firm=$6) LIMIT 2`, candidate, p.ClOrdID, p.Side, p.Symbol, p.Board, p.Firm)
	if err != nil {
		return err
	}
	var matches []uuid.UUID
	var actualOrder string
	for rows.Next() {
		var id uuid.UUID
		var q *string
		if err := rows.Scan(&id, &q); err != nil {
			rows.Close()
			return err
		}
		matches = append(matches, id)
		if q != nil {
			actualOrder = *q
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(matches) == 1 {
		order = &matches[0]
		if p.OrderID == "" {
			p.OrderID = actualOrder
		}
		err = tx.QueryRow(ctx, `SELECT id FROM executions WHERE order_id=$1 AND trade_match_id=$2 AND last_qty=$3 AND last_px=$4 LIMIT 1`, order, p.Match, p.Qty, p.Price).Scan(&execution)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	var side uuid.UUID
	err = tx.QueryRow(ctx, `INSERT INTO trade_sides(trade_id,side,quik_order_id,quik_order_num,order_id,execution_id,exec_id,fix_cl_ord_id,
firm_id,firm_name,client_code,account,contra_broker,cum_qty,leaves_qty,adapter_id,has_fix_report,has_quik_record)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,false,true)
ON CONFLICT(trade_id,side) DO UPDATE SET
order_id=COALESCE(trade_sides.order_id,EXCLUDED.order_id), execution_id=COALESCE(trade_sides.execution_id,EXCLUDED.execution_id),
quik_order_id=COALESCE(NULLIF(trade_sides.quik_order_id,''),EXCLUDED.quik_order_id),
exec_id=COALESCE(NULLIF(trade_sides.exec_id,''),EXCLUDED.exec_id),fix_cl_ord_id=COALESCE(NULLIF(trade_sides.fix_cl_ord_id,''),EXCLUDED.fix_cl_ord_id),
firm_id=COALESCE(NULLIF(trade_sides.firm_id,''),EXCLUDED.firm_id),firm_name=COALESCE(NULLIF(EXCLUDED.firm_name,''),trade_sides.firm_name),
client_code=COALESCE(NULLIF(trade_sides.client_code,''),EXCLUDED.client_code),account=COALESCE(NULLIF(trade_sides.account,''),EXCLUDED.account),
contra_broker=COALESCE(NULLIF(trade_sides.contra_broker,''),EXCLUDED.contra_broker),
cum_qty=COALESCE(trade_sides.cum_qty,EXCLUDED.cum_qty),leaves_qty=COALESCE(trade_sides.leaves_qty,EXCLUDED.leaves_qty),
adapter_id=COALESCE(NULLIF(trade_sides.adapter_id,''),EXCLUDED.adapter_id),
has_fix_report=trade_sides.has_fix_report OR EXCLUDED.has_fix_report,has_quik_record=trade_sides.has_quik_record OR EXCLUDED.has_quik_record
WHERE trade_sides.quik_order_num=EXCLUDED.quik_order_num
AND (trade_sides.firm_id='' OR EXCLUDED.firm_id='' OR trade_sides.firm_id=EXCLUDED.firm_id)
AND (trade_sides.client_code='' OR EXCLUDED.client_code='' OR trade_sides.client_code=EXCLUDED.client_code)
AND (trade_sides.exec_id='' OR EXCLUDED.exec_id='' OR trade_sides.exec_id=EXCLUDED.exec_id)
RETURNING id`, trade, p.Side, p.OrderID, p.OrderNumber, order, execution, p.ExecID, p.ClOrdID, p.Firm, p.FirmName, p.Client, p.Account, p.Contra, p.Cum, p.Leaves, p.Adapter).Scan(&side)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("conflicting %s party/order for %s", p.Side, p.Match)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO trade_side_reports(report_id,side_id) VALUES($1,$2) ON CONFLICT(report_id) DO NOTHING`, report, side); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE trade_reports SET status='RECORDED',error='' WHERE id=$1`, report); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// LinkLocal attaches QUIK sides to the local order and execution of the same
// trade. It runs after each QUIK import, so it does not matter whether the FIX
// report or the QUIK row arrived first. It never creates a side or changes
// amounts.
func (s *Store) LinkLocal(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `UPDATE trade_sides s SET execution_id=e.id,order_id=e.order_id,exec_id=e.exec_id,
quik_order_id=COALESCE(NULLIF(o.quik_order_id,''),s.quik_order_id),fix_cl_ord_id=COALESCE(o.fix_cl_ord_id,s.fix_cl_ord_id),
adapter_id=COALESCE(e.adapter_id::text,s.adapter_id),cum_qty=e.cum_qty,leaves_qty=e.leaves_qty
FROM trades t,executions e JOIN orders o ON o.id=e.order_id
WHERE t.id=s.trade_id AND s.execution_id IS NULL AND s.has_quik_record
AND e.trade_match_id=t.trade_match_id AND e.last_qty=t.quantity AND e.last_px=t.price
AND o.side::text=s.side AND substring(o.quik_order_id from '[0-9]+$')=s.quik_order_num`)
	return err
}

package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

var ErrSendAlreadyAttempted = errors.New("fix send already attempted")
var ErrCannotFailover = errors.New("cannot failover after send attempted")

var ErrNotFound = errors.New("order not found")
var ErrInvalidCancel = errors.New("invalid cancel")
var ErrInstrumentNotFound = errors.New("instrument not found")

// ErrIdempotencyConflict means client_order_id is already used by an order
// with different parameters. The existing order is not returned as a replay.
var ErrIdempotencyConflict = errors.New("client_order_id already used with different order parameters")

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) FindByClientOrderID(ctx context.Context, brokerID int64, clientOrderID string) (domain.Order, error) {
	return s.scanOrder(s.pool.QueryRow(ctx, orderSelect+` WHERE o.broker_id=$1 AND o.client_order_id=$2`, brokerID, clientOrderID))
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	return s.scanOrder(s.pool.QueryRow(ctx, orderSelect+` WHERE o.id=$1`, id))
}

func (s *Store) GetForBroker(ctx context.Context, brokerID int64, id uuid.UUID) (domain.Order, error) {
	return s.scanOrder(s.pool.QueryRow(ctx, orderSelect+` WHERE o.id=$1 AND o.broker_id=$2`, id, brokerID))
}

const orderSelect = `
SELECT o.id, o.broker_id, COALESCE(o.account_id, 0), o.client_order_id, o.instrument_id, o.side, o.order_type,
       o.time_in_force, o.quantity, o.price, o.filled_quantity, o.avg_price, o.status, o.adapter_id,
       o.fix_cl_ord_id, o.quik_order_id, o.reject_reason, o.created_at, o.updated_at,
       i.quik_symbol, COALESCE(NULLIF(o.board, ''), i.board) AS board,
       COALESCE(NULLIF(o.fix_account, ''), a.trading_account, '') AS trading_account,
       i.currency,
       COALESCE(NULLIF(o.fix_firm, ''), '') AS quik_firm,
       COALESCE(NULLIF(o.fix_client_id, ''), '') AS client_code,
       COALESCE(NULLIF(f.firm_name, ''), NULLIF(bf.name, ''), NULLIF(b.name, ''), '') AS firm_name,
       COALESCE(o.request_id, ''), COALESCE(o.trace_id, ''),
       o.fix_send_attempted_at
FROM orders o
JOIN instruments i ON i.id = o.instrument_id
LEFT JOIN brokers b ON b.id = o.broker_id
LEFT JOIN brokers bf ON bf.code = NULLIF(o.fix_firm, '')
LEFT JOIN quik_firms f ON f.firm_id = NULLIF(o.fix_firm, '')
LEFT JOIN LATERAL (
	SELECT ba.trading_account
	FROM broker_accounts ba
	WHERE ba.broker_id = o.broker_id
	  AND (
		(o.account_id IS NOT NULL AND ba.id = o.account_id)
		OR ba.trading_account = o.fix_account
		OR ba.code = o.fix_account
	  )
	ORDER BY CASE WHEN o.account_id IS NOT NULL AND ba.id = o.account_id THEN 0 ELSE 1 END
	LIMIT 1
) a ON true`

func (s *Store) scanOrder(row pgx.Row) (domain.Order, error) {
	var o domain.Order
	var price *decimal.Decimal
	err := row.Scan(
		&o.ID, &o.BrokerID, &o.AccountID, &o.ClientOrderID, &o.InstrumentID, &o.Side, &o.OrderType,
		&o.TimeInForce, &o.Quantity, &price, &o.FilledQuantity, &o.AvgPrice, &o.Status, &o.AdapterID,
		&o.FixClOrdID, &o.QuikOrderID, &o.RejectReason, &o.CreatedAt, &o.UpdatedAt,
		&o.Symbol, &o.Board, &o.TradingAccount, &o.Currency, &o.QuikFirm, &o.ClientCode, &o.FirmName,
		&o.RequestID, &o.TraceID, &o.FixSendAttemptedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	o.Price = price
	return o, nil
}

type CreateInput struct {
	BrokerID      int64
	FixAccount    string
	ClientCode    string // FIX tag 109 вЂ” arbitrary broker client code
	QuikFirm      string // FIX tag 5018 вЂ” from API token
	ClientOrderID string
	InstrumentID  int64
	Board         string
	Side          domain.Side
	OrderType     domain.OrderType
	TimeInForce   string
	Quantity      decimal.Decimal
	Price         *decimal.Decimal
	RequestID     string
	TraceID       string
}

func (s *Store) CreateWithOutbox(ctx context.Context, in CreateInput) (domain.Order, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, false, err
	}
	defer tx.Rollback(ctx)
	policies, err := lockTradingLimits(ctx, tx, in.BrokerID)
	if err != nil {
		return domain.Order{}, false, err
	}

	existingID, same, err := findReplay(ctx, tx, in)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return domain.Order{}, false, err
		}
		if !same {
			return domain.Order{}, false, ErrIdempotencyConflict
		}
		o, err := s.Get(ctx, existingID)
		return o, true, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, false, err
	}
	if err := checkTradingLimits(ctx, tx, in, policies); err != nil {
		return domain.Order{}, false, err
	}

	id := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO orders (
			id, broker_id, account_id, client_order_id, instrument_id, side, order_type,
			time_in_force, quantity, price, status, fix_cl_ord_id, board, fix_account,
			fix_client_id, fix_firm, request_id, trace_id
		) VALUES ($1,$2,NULL,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		id, in.BrokerID, in.ClientOrderID, in.InstrumentID, in.Side, in.OrderType,
		in.TimeInForce, in.Quantity, in.Price, domain.OrderQueued,
		fmt.Sprintf("G%s", id.String()[:32]), in.Board, in.FixAccount,
		in.ClientCode, in.QuikFirm, nullIfEmpty(in.RequestID), nullIfEmpty(in.TraceID),
	)
	if err != nil {
		_ = tx.Rollback(ctx)
		if isUniqueViolation(err) {
			existingID, same, gerr := findReplay(ctx, s.pool, in)
			if gerr != nil {
				return domain.Order{}, false, gerr
			}
			if !same {
				return domain.Order{}, false, ErrIdempotencyConflict
			}
			o, gerr := s.Get(ctx, existingID)
			return o, true, gerr
		}
		return domain.Order{}, false, err
	}

	for _, ev := range []struct {
		t, from, to, msg string
	}{
		{"ORDER_CREATED", "", string(domain.OrderReceived), "order received"},
		{"ORDER_VALIDATED", string(domain.OrderReceived), string(domain.OrderValidated), "order validated"},
		{"ORDER_QUEUED", string(domain.OrderValidated), string(domain.OrderQueued), "order queued for dispatch"},
	} {
		var from any
		if ev.from != "" {
			from = ev.from
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_events (order_id, event_type, from_status, to_status, message)
			VALUES ($1,$2,$3,$4,$5)`, id, ev.t, from, ev.to, ev.msg); err != nil {
			return domain.Order{}, false, err
		}
	}

	payload, _ := json.Marshal(map[string]any{"order_id": id.String(), "request_id": in.RequestID, "trace_id": in.TraceID})
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
		VALUES ('order', $1, 'ORDER_SEND', $2)`, id.String(), payload)
	if err != nil {
		return domain.Order{}, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, false, err
	}
	o, err := s.Get(ctx, id)
	return o, false, err
}

// findReplay looks up an order with the same client_order_id and reports
// whether its parameters match the request (a retry rather than a new order).
func findReplay(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, in CreateInput) (uuid.UUID, bool, error) {
	var id uuid.UUID
	var same bool
	err := q.QueryRow(ctx, `SELECT id, instrument_id=$3 AND COALESCE(board,'')=$4 AND side=$5 AND order_type=$6
 AND COALESCE(time_in_force,'')=$7 AND quantity=$8 AND price IS NOT DISTINCT FROM $9::numeric
 AND COALESCE(fix_account,'')=$10 AND COALESCE(fix_client_id,'')=$11 AND COALESCE(fix_firm,'')=$12
 FROM orders WHERE broker_id=$1 AND client_order_id=$2`,
		in.BrokerID, in.ClientOrderID, in.InstrumentID, in.Board, in.Side, in.OrderType, in.TimeInForce,
		in.Quantity, in.Price, in.FixAccount, in.ClientCode, in.QuikFirm).Scan(&id, &same)
	return id, same, err
}

func (s *Store) Transition(ctx context.Context, id uuid.UUID, to domain.OrderStatus, eventType, message string, adapterID *string) (domain.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)

	var from domain.OrderStatus
	err = tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, ErrNotFound
	}
	if err != nil {
		return domain.Order{}, err
	}
	if !CanTransition(from, to) {
		return domain.Order{}, fmt.Errorf("illegal transition %s -> %s", from, to)
	}
	_, err = tx.Exec(ctx, `UPDATE orders SET status=$2, adapter_id=COALESCE($3, adapter_id), updated_at=now() WHERE id=$1`, id, to, adapterID)
	if err != nil {
		return domain.Order{}, err
	}
	fromStr := string(from)
	toStr := string(to)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message)
		VALUES ($1,$2,$3,$4,$5,$6)`, id, eventType, fromStr, toStr, adapterID, message)
	if err != nil {
		return domain.Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) AssignRoute(ctx context.Context, id uuid.UUID, adapterID, fixClOrdID string) (domain.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)

	var from domain.OrderStatus
	var quik *string
	err = tx.QueryRow(ctx, `SELECT status, quik_order_id FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&from, &quik)
	if err != nil {
		return domain.Order{}, err
	}
	if IsExternallyAccepted(domain.Order{Status: from, QuikOrderID: quik}) {
		return domain.Order{}, fmt.Errorf("order already externally accepted")
	}
	if !CanTransition(from, domain.OrderRouting) && from != domain.OrderRouting {
		return domain.Order{}, fmt.Errorf("cannot route from %s", from)
	}
	_, err = tx.Exec(ctx, `
		UPDATE orders SET status=$2, adapter_id=$3, fix_cl_ord_id=$4, updated_at=now() WHERE id=$1`,
		id, domain.OrderRouting, adapterID, fixClOrdID)
	if err != nil {
		return domain.Order{}, err
	}
	fromStr := string(from)
	toStr := string(domain.OrderRouting)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message)
		VALUES ($1,'ORDER_ROUTED',$2,$3,$4,$5)`, id, fromStr, toStr, adapterID, "routed to adapter "+adapterID)
	if err != nil {
		return domain.Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return s.Get(ctx, id)
}

func (s *Store) MarkSent(ctx context.Context, id uuid.UUID, adapterID, rawFIX string) (domain.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)

	var from domain.OrderStatus
	err = tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Order{}, ErrNotFound
	}
	if err != nil {
		return domain.Order{}, err
	}
	to := domain.OrderSent
	if from == to || IsExternallyAccepted(domain.Order{Status: from}) || IsTerminal(from) {
		if err := tx.Commit(ctx); err != nil {
			return domain.Order{}, err
		}
		return s.Get(ctx, id)
	}
	if !CanTransition(from, to) {
		return domain.Order{}, fmt.Errorf("illegal transition %s -> %s", from, to)
	}
	_, err = tx.Exec(ctx, `UPDATE orders SET status=$2, adapter_id=COALESCE($3, adapter_id), updated_at=now() WHERE id=$1`, id, to, adapterID)
	if err != nil {
		return domain.Order{}, err
	}
	fromStr := string(from)
	toStr := string(to)
	payload := fixWirePayload(rawFIX)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message, payload)
		VALUES ($1,'ORDER_SENT',$2,$3,$4,$5,$6)`,
		id, fromStr, toStr, adapterID, "sent to FIX adapter", payload)
	if err != nil {
		return domain.Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return s.Get(ctx, id)
}

func fixWirePayload(rawFIX string) []byte {
	if rawFIX == "" {
		return nil
	}
	b, _ := json.Marshal(map[string]string{"raw_fix": rawFIX})
	return b
}

func (s *Store) ClearRouteForFailover(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status domain.OrderStatus
	var quik *string
	var attempted *time.Time
	err = tx.QueryRow(ctx, `SELECT status, quik_order_id, fix_send_attempted_at FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&status, &quik, &attempted)
	if err != nil {
		return err
	}
	cur := domain.Order{Status: status, QuikOrderID: quik, FixSendAttemptedAt: attempted}
	if !CanClearRouteForFailover(cur) {
		if IsExternallyAccepted(cur) {
			return fmt.Errorf("cannot failover externally accepted order")
		}
		return ErrCannotFailover
	}
	_, err = tx.Exec(ctx, `
		UPDATE orders SET status=$2, adapter_id=NULL, updated_at=now() WHERE id=$1`, id, domain.OrderQueued)
	if err != nil {
		return err
	}
	from := string(status)
	to := string(domain.OrderQueued)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, message)
		VALUES ($1,'ORDER_FAILOVER',$2,$3,$4)`, id, from, to, reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FenceSendAttempt records that a FIX NewOrderSingle is about to go on the wire.
// After this succeeds the worker must never SendOrder or failover this ClOrdID.
func (s *Store) FenceSendAttempt(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE orders SET fix_send_attempted_at=now(), updated_at=now()
		WHERE id=$1 AND fix_send_attempted_at IS NULL
		  AND status IN ('QUEUED', 'ROUTING')`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSendAlreadyAttempted
	}
	return nil
}

const orderLockSelect = `
		SELECT id, broker_id, COALESCE(account_id, 0), client_order_id, instrument_id, side, order_type,
		       time_in_force, quantity, price, filled_quantity, avg_price, status, adapter_id,
		       fix_cl_ord_id, quik_order_id, reject_reason, created_at, updated_at
		FROM orders `

func scanLockedOrder(row pgx.Row) (domain.Order, error) {
	var o domain.Order
	var price *decimal.Decimal
	err := row.Scan(&o.ID, &o.BrokerID, &o.AccountID, &o.ClientOrderID, &o.InstrumentID, &o.Side, &o.OrderType,
		&o.TimeInForce, &o.Quantity, &price, &o.FilledQuantity, &o.AvgPrice, &o.Status, &o.AdapterID,
		&o.FixClOrdID, &o.QuikOrderID, &o.RejectReason, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		return o, err
	}
	o.Price = price
	return o, nil
}

func uniqueNonEmpty(vals ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func clOrdLookupKeys(clOrdID, origClOrdID string) []string {
	keys := uniqueNonEmpty(clOrdID, origClOrdID)
	if i := strings.LastIndex(clOrdID, "-C"); i > 0 {
		keys = uniqueNonEmpty(append(keys, clOrdID[:i])...)
	}
	return keys
}

// OrderIDForFIX finds the local order a FIX report refers to, without locking.
func (s *Store) OrderIDForFIX(ctx context.Context, clOrdID, origClOrdID, quikOrderID string) (*uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM orders WHERE fix_cl_ord_id = ANY($1)
 OR (btrim($2) <> '' AND quik_order_id = $2) ORDER BY COALESCE(fix_cl_ord_id = ANY($1), false) DESC LIMIT 1`,
		clOrdLookupKeys(clOrdID, origClOrdID), quikOrderID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func lockOrderForFIX(ctx context.Context, tx pgx.Tx, clOrdID, origClOrdID, quikOrderID string) (domain.Order, error) {
	for _, k := range clOrdLookupKeys(clOrdID, origClOrdID) {
		o, err := scanLockedOrder(tx.QueryRow(ctx, orderLockSelect+` WHERE fix_cl_ord_id=$1 FOR UPDATE`, k))
		if err == nil {
			return o, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Order{}, err
		}
	}
	if strings.TrimSpace(quikOrderID) != "" {
		o, err := scanLockedOrder(tx.QueryRow(ctx, orderLockSelect+` WHERE quik_order_id=$1 FOR UPDATE`, quikOrderID))
		if err == nil {
			return o, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return domain.Order{}, err
		}
	}
	return domain.Order{}, ErrNotFound
}

func (s *Store) ApplyCancelReject(ctx context.Context, clOrdID, origClOrdID, quikOrderID, adapterID, reason, rawFIX string) (domain.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)
	o, err := lockOrderForFIX(ctx, tx, clOrdID, origClOrdID, quikOrderID)
	if err != nil {
		return domain.Order{}, err
	}
	to := o.Status
	claimed, err := claimExecutionReceipt(ctx, tx, o.ID)
	if err != nil {
		return domain.Order{}, err
	}
	if !claimed {
		return domain.Order{}, nil
	}
	if o.Status == domain.OrderCancelPending {
		if o.FilledQuantity.GreaterThan(decimal.Zero) {
			to = domain.OrderPartiallyFilled
		} else {
			to = domain.OrderAcknowledged
		}
	}
	msg := "cancel rejected"
	if strings.TrimSpace(reason) != "" {
		msg = "cancel rejected: " + reason
	}
	if to != o.Status {
		_, err = tx.Exec(ctx, `UPDATE orders SET status=$2, updated_at=now() WHERE id=$1`, o.ID, to)
		if err != nil {
			return domain.Order{}, err
		}
	}
	payload := fixWirePayload(rawFIX)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message, payload)
		VALUES ($1,'ORDER_CANCEL_REJECTED',$2,$3,$4,$5,$6)`,
		o.ID, string(o.Status), string(to), adapterID, msg, payload)
	if err != nil {
		return domain.Order{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return s.Get(ctx, o.ID)
}

func (s *Store) ApplyExecutionReport(ctx context.Context, fixClOrdID, origClOrdID, execID, execType, ordStatus, quikOrderID string, lastQty, lastPx, cumQty, leavesQty decimal.Decimal, adapterID, text, ordRejReason, rawFIX string) (domain.Order, *domain.Execution, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, nil, err
	}
	defer tx.Rollback(ctx)

	o, err := lockOrderForFIX(ctx, tx, fixClOrdID, origClOrdID, quikOrderID)
	if err != nil {
		return domain.Order{}, nil, err
	}

	claimed, err := claimExecutionReceipt(ctx, tx, o.ID)
	if err != nil {
		return domain.Order{}, nil, err
	}
	if !claimed {
		return domain.Order{}, nil, nil
	}
	to := mapExecStatus(execType, ordStatus, o.Status)
	erMsg := formatExecutionMessage(execType, ordStatus, text, ordRejReason)
	payload := fixWirePayload(rawFIX)
	filled := effectiveFillQty(o.FilledQuantity, lastQty, cumQty)
	var exec *domain.Execution
	if lastQty.GreaterThan(decimal.Zero) {
		if execID == "" {
			return domain.Order{}, nil, fmt.Errorf("fill report missing ExecID")
		}
		if lastPx.IsNegative() || lastQty.IsNegative() || cumQty.IsNegative() || leavesQty.IsNegative() {
			return domain.Order{}, nil, fmt.Errorf("negative execution quantities or price")
		}
		exec, err = insertExecution(ctx, tx, o.ID, execID, lastQty, lastPx, filled, leavesQty, adapterID, rawFIX)
		if err != nil {
			return domain.Order{}, nil, err
		}
		if exec == nil {
			// Replayed fills must not recalculate totals, average price or status.
			if err := tx.Commit(ctx); err != nil {
				return domain.Order{}, nil, err
			}
			out, err := s.Get(ctx, o.ID)
			return out, nil, err
		}
	}

	avg := o.AvgPrice
	if exec != nil {
		// Weighted over the recorded fills, so a jump in CumQty (a missed or
		// quarantined report) cannot skew the price of the fills we know.
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(last_qty*last_px)/NULLIF(sum(last_qty),0),0)
 FROM executions WHERE order_id=$1`, o.ID).Scan(&avg); err != nil {
			return domain.Order{}, nil, err
		}
	}

	if !CanTransition(o.Status, to) && o.Status != to {
		if lastQty.GreaterThan(decimal.Zero) {
			_, err = tx.Exec(ctx, `
				UPDATE orders SET filled_quantity=$2, avg_price=$3,
				                  quik_order_id=COALESCE(NULLIF($4,''), quik_order_id),
				                  adapter_id=COALESCE(NULLIF($5,''), adapter_id), updated_at=now()
				WHERE id=$1`, o.ID, filled, avg, quikOrderID, adapterID)
			if err != nil {
				return domain.Order{}, nil, err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message, payload)
			VALUES ($1,'EXECUTION_REPORT',$2,$3,$4,$5,$6)`,
			o.ID, string(o.Status), string(to), adapterID, "ignored illegal ER transition; "+erMsg, payload); err != nil {
			return domain.Order{}, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Order{}, nil, err
		}
		out, err := s.Get(ctx, o.ID)
		return out, exec, err
	}

	rejectReason := o.RejectReason
	if to == domain.OrderRejected {
		if r := composeRejectReason(text, ordRejReason); r != nil {
			rejectReason = r
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE orders SET status=$2, filled_quantity=$3, avg_price=$4, quik_order_id=COALESCE(NULLIF($5,''), quik_order_id),
		                adapter_id=COALESCE(NULLIF($6,''), adapter_id), reject_reason=$7, updated_at=now()
		WHERE id=$1`, o.ID, to, filled, avg, quikOrderID, adapterID, rejectReason)
	if err != nil {
		return domain.Order{}, nil, err
	}
	fromStr := string(o.Status)
	toStr := string(to)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message, payload)
		VALUES ($1,'ORDER_EXECUTION_REPORT',$2,$3,$4,$5,$6)`,
		o.ID, fromStr, toStr, adapterID, erMsg, payload)
	if err != nil {
		return domain.Order{}, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, nil, err
	}
	out, err := s.Get(ctx, o.ID)
	out.ExecutionBecameTerminal = !IsTerminal(o.Status) && IsTerminal(to)
	return out, exec, err
}

func mapExecStatus(execType, ordStatus string, current domain.OrderStatus) domain.OrderStatus {
	switch ordStatus {
	case "0":
		return domain.OrderAcknowledged
	case "1":
		return domain.OrderPartiallyFilled
	case "2":
		return domain.OrderFilled
	case "4":
		return domain.OrderCancelled
	case "8":
		return domain.OrderRejected
	case "C":
		return domain.OrderExpired
	case "5":
		return domain.OrderAcknowledged
	}
	switch execType {
	case "0":
		return domain.OrderAcknowledged
	case "1":
		return domain.OrderPartiallyFilled
	case "2":
		return domain.OrderFilled
	case "4":
		return domain.OrderCancelled
	case "8":
		return domain.OrderRejected
	case "C":
		return domain.OrderExpired
	}
	return current
}

func composeRejectReason(text, ordRejReason string) *string {
	parts := make([]string, 0, 2)
	if t := strings.TrimSpace(text); t != "" {
		parts = append(parts, t)
	}
	if r := strings.TrimSpace(ordRejReason); r != "" {
		parts = append(parts, "OrdRejReason="+r)
	}
	if len(parts) == 0 {
		return nil
	}
	s := strings.Join(parts, "; ")
	return &s
}

func formatExecutionMessage(execType, ordStatus, text, ordRejReason string) string {
	msg := fmt.Sprintf("execType=%s ordStatus=%s", execType, ordStatus)
	if t := strings.TrimSpace(text); t != "" {
		msg += " text=" + t
	}
	if r := strings.TrimSpace(ordRejReason); r != "" {
		msg += " ordRejReason=" + r
	}
	return msg
}

const tradeSelect = `
SELECT e.id, e.order_id, e.exec_id, e.last_qty, e.last_px, e.cum_qty, e.leaves_qty, e.adapter_id, e.created_at,
       o.broker_id, o.client_order_id, i.quik_symbol,
       COALESCE(NULLIF(o.board, ''), i.board) AS board,
       o.side,
       COALESCE(NULLIF(o.fix_client_id, ''), '') AS client_code,
       COALESCE(NULLIF(o.fix_firm, ''), '') AS quik_firm,
       COALESCE(NULLIF(f.firm_name, ''), NULLIF(bf.name, ''), NULLIF(b.name, ''), '') AS firm_name,
       o.fix_cl_ord_id, o.quik_order_id,
       COALESCE(e.execution_date, (e.created_at AT TIME ZONE 'UTC')::date)::text,
       e.trade_match_id, e.contra_broker, e.contra_trader
FROM executions e
JOIN orders o ON o.id = e.order_id
JOIN instruments i ON i.id = o.instrument_id
LEFT JOIN brokers b ON b.id = o.broker_id
LEFT JOIN brokers bf ON bf.code = NULLIF(o.fix_firm, '')
LEFT JOIN quik_firms f ON f.firm_id = NULLIF(o.fix_firm, '')
`

func (s *Store) ListTrades(ctx context.Context, brokerID *int64, limit int) ([]domain.Trade, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows pgx.Rows
	var err error
	if brokerID != nil {
		rows, err = s.pool.Query(ctx, tradeSelect+` WHERE o.broker_id=$1 ORDER BY e.created_at DESC, e.id DESC LIMIT $2`, *brokerID, limit)
	} else {
		rows, err = s.pool.Query(ctx, tradeSelect+` ORDER BY e.created_at DESC, e.id DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Trade
	for rows.Next() {
		t, err := scanTrade(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ListTradesByOrder(ctx context.Context, orderID uuid.UUID) ([]domain.Trade, error) {
	rows, err := s.pool.Query(ctx, tradeSelect+` WHERE e.order_id=$1 ORDER BY e.created_at, e.exec_id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Trade
	for rows.Next() {
		t, err := scanTrade(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) List(ctx context.Context, brokerID *int64, limit int) ([]domain.Order, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows pgx.Rows
	var err error
	if brokerID != nil {
		rows, err = s.pool.Query(ctx, orderSelect+` WHERE o.broker_id=$1 ORDER BY o.created_at DESC LIMIT $2`, *brokerID, limit)
	} else {
		rows, err = s.pool.Query(ctx, orderSelect+` ORDER BY o.created_at DESC LIMIT $1`, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Order
	for rows.Next() {
		o, err := s.scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) ListEvents(ctx context.Context, orderID uuid.UUID) ([]domain.OrderEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, order_id, event_type, from_status, to_status, adapter_id, message, payload, created_at
		FROM order_events WHERE order_id=$1 ORDER BY created_at, id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OrderEvent
	for rows.Next() {
		var e domain.OrderEvent
		if err := rows.Scan(&e.ID, &e.OrderID, &e.EventType, &e.FromStatus, &e.ToStatus, &e.AdapterID, &e.Message, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ResolveInstrument(ctx context.Context, brokerID int64, brokerSymbol, board string) (domain.Instrument, error) {
	var i domain.Instrument
	err := s.pool.QueryRow(ctx, `
		SELECT i.id, i.internal_symbol, i.quik_symbol, i.board, i.currency, i.security_type, i.status
		FROM broker_instrument_mapping m
		JOIN instruments i ON i.id = m.instrument_id
		WHERE m.broker_id=$1 AND m.broker_symbol=$2`, brokerID, brokerSymbol,
	).Scan(&i.ID, &i.InternalSymbol, &i.QuikSymbol, &i.Board, &i.Currency, &i.SecurityType, &i.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.pool.QueryRow(ctx, `
			SELECT id, internal_symbol, quik_symbol, board, currency, security_type, status
			FROM instruments
			WHERE status = 'ACTIVE'
			  AND (internal_symbol=$1 OR quik_symbol=$1 OR internal_symbol LIKE '%/' || $1)
			  AND ($2 = '' OR board = $2)
			ORDER BY CASE WHEN board = $2 THEN 0 ELSE 1 END, id
			LIMIT 1`, brokerSymbol, board,
		).Scan(&i.ID, &i.InternalSymbol, &i.QuikSymbol, &i.Board, &i.Currency, &i.SecurityType, &i.Status)
		if errors.Is(err, pgx.ErrNoRows) {
			return i, ErrInstrumentNotFound
		}
	}
	return i, err
}

func (s *Store) EnqueueCancel(ctx context.Context, orderID uuid.UUID) (already bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var status domain.OrderStatus
	if err := tx.QueryRow(ctx, `SELECT status FROM orders WHERE id=$1 FOR UPDATE`, orderID).Scan(&status); err != nil {
		return false, err
	}
	if status == domain.OrderCancelled {
		return true, nil
	}
	// A retry of the HTTP request must not create a second cancellation while
	// the first is queued or has an uncertain wire outcome.
	var unresolved bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM outbox_events WHERE aggregate_id=$1 AND event_type='ORDER_CANCEL'
		AND status IN ('PENDING','PROCESSING','FAILED'))`, orderID.String()).Scan(&unresolved); err != nil {
		return false, err
	}
	if unresolved {
		return true, nil
	}
	var last *time.Time
	if err := tx.QueryRow(ctx, `
		SELECT MAX(created_at)
		FROM outbox_events WHERE aggregate_id=$1 AND event_type='ORDER_CANCEL'`, orderID.String()).Scan(&last); err != nil {
		return false, err
	}
	// Client pollers retry HTTP cancel ~every 250ms. After QUIK reject the
	// order returns to ACKNOWLEDGED, which would otherwise enqueue a new FIX F.
	if last != nil && time.Since(*last) < 2*time.Second {
		return true, nil
	}
	if status == domain.OrderCancelPending {
		if last != nil {
			return true, nil
		}
		payload, _ := json.Marshal(map[string]any{"order_id": orderID.String()})
		_, err = tx.Exec(ctx, `
			INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
			VALUES ('order', $1, 'ORDER_CANCEL', $2)`, orderID.String(), payload)
		if err != nil {
			return false, err
		}
		return false, tx.Commit(ctx)
	}
	if !CanTransition(status, domain.OrderCancelPending) {
		return false, fmt.Errorf("%w: cannot cancel from status %s", ErrInvalidCancel, status)
	}
	_, err = tx.Exec(ctx, `UPDATE orders SET status=$2, updated_at=now() WHERE id=$1`, orderID, domain.OrderCancelPending)
	if err != nil {
		return false, err
	}
	from := string(status)
	to := string(domain.OrderCancelPending)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, message)
		VALUES ($1,'ORDER_CANCEL_REQUESTED',$2,$3,'cancel requested')`, orderID, from, to)
	if err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]any{"order_id": orderID.String()})
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
		VALUES ('order', $1, 'ORDER_CANCEL', $2)`, orderID.String(), payload)
	if err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

func (s *Store) StatsToday(ctx context.Context) (map[string]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT status, COUNT(*) FROM orders
		WHERE created_at >= date_trunc('day', now())
		GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{"total": 0, "filled": 0, "rejected": 0, "active": 0}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out["total"] += n
		switch domain.OrderStatus(status) {
		case domain.OrderFilled:
			out["filled"] += n
		case domain.OrderRejected, domain.OrderFailed:
			out["rejected"] += n
		case domain.OrderQueued, domain.OrderRouting, domain.OrderSent, domain.OrderAcknowledged,
			domain.OrderPartiallyFilled, domain.OrderCancelPending:
			out["active"] += n
		}
	}
	return out, rows.Err()
}

type DayCount struct {
	Date   string `json:"date"`
	Orders int64  `json:"orders"`
}

type BrokerPeriodStats struct {
	Orders   int64 `json:"orders"`
	Filled   int64 `json:"filled"`
	Rejected int64 `json:"rejected"`
	Active   int64 `json:"active"`
}

type BrokerStats struct {
	Days     int               `json:"days"`
	Today    BrokerPeriodStats `json:"today"`
	Period   BrokerPeriodStats `json:"period"`
	Previous BrokerPeriodStats `json:"previous"`
	Series   []DayCount        `json:"series"`
}

func (s *Store) periodStats(ctx context.Context, brokerID int64, from, to time.Time) (BrokerPeriodStats, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT status, COUNT(*) FROM orders
		WHERE broker_id=$1 AND created_at >= $2 AND created_at < $3
		GROUP BY status`, brokerID, from, to)
	if err != nil {
		return BrokerPeriodStats{}, err
	}
	defer rows.Close()
	var st BrokerPeriodStats
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			return BrokerPeriodStats{}, err
		}
		st.Orders += n
		switch domain.OrderStatus(status) {
		case domain.OrderFilled:
			st.Filled += n
		case domain.OrderRejected, domain.OrderFailed:
			st.Rejected += n
		case domain.OrderQueued, domain.OrderRouting, domain.OrderSent, domain.OrderAcknowledged,
			domain.OrderPartiallyFilled, domain.OrderCancelPending:
			st.Active += n
		}
	}
	return st, rows.Err()
}

func (s *Store) StatsForBroker(ctx context.Context, brokerID int64, days int) (BrokerStats, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	periodStart := dayStart.AddDate(0, 0, -(days - 1))
	prevStart := periodStart.AddDate(0, 0, -days)
	end := dayStart.AddDate(0, 0, 1)

	out := BrokerStats{Days: days, Series: []DayCount{}}
	var err error
	out.Today, err = s.periodStats(ctx, brokerID, dayStart, end)
	if err != nil {
		return out, err
	}
	out.Period, err = s.periodStats(ctx, brokerID, periodStart, end)
	if err != nil {
		return out, err
	}
	out.Previous, err = s.periodStats(ctx, brokerID, prevStart, periodStart)
	if err != nil {
		return out, err
	}

	_, offsetSec := now.Zone()
	rows, err := s.pool.Query(ctx, `
		SELECT to_char(o.created_at + make_interval(secs => $4), 'YYYY-MM-DD') AS d, COUNT(*)
		FROM orders o
		WHERE o.broker_id=$1
		  AND o.created_at >= $2
		  AND o.created_at < $3
		GROUP BY d
		ORDER BY d`, brokerID, periodStart, end, offsetSec)
	if err != nil {
		return out, err
	}
	counts := map[string]int64{}
	for rows.Next() {
		var d string
		var n int64
		if err := rows.Scan(&d, &n); err != nil {
			rows.Close()
			return out, err
		}
		counts[d] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}
	for d := periodStart; !d.After(dayStart); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out.Series = append(out.Series, DayCount{Date: key, Orders: counts[key]})
	}
	return out, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func effectiveFillQty(prior, lastQty, cumQty decimal.Decimal) decimal.Decimal {
	if !cumQty.IsZero() {
		return cumQty
	}
	if lastQty.GreaterThan(decimal.Zero) {
		return prior.Add(lastQty)
	}
	return prior
}

func insertExecution(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, execID string, lastQty, lastPx, cumQty, leavesQty decimal.Decimal, adapterID, rawFIX string) (*domain.Execution, error) {
	match := executionMatchFromFIX(rawFIX)
	executionDate, err := executionDateFromContext(ctx)
	if err != nil {
		return nil, err
	}
	// Legacy executions have no reliable venue date. Preserve their original
	// per-order replay protection rather than guessing a date during migration.
	var legacyID uuid.UUID
	var legacyMatchID string
	var legacyQty, legacyPx, legacyCum, legacyLeaves decimal.Decimal
	err = tx.QueryRow(ctx, `SELECT id,last_qty,last_px,cum_qty,leaves_qty,trade_match_id FROM executions WHERE order_id=$1 AND exec_id=$2 AND execution_date IS NULL FOR UPDATE`, orderID, execID).Scan(&legacyID, &legacyQty, &legacyPx, &legacyCum, &legacyLeaves, &legacyMatchID)
	if err == nil {
		if legacyMatchID != "" && match.ID != "" && legacyMatchID != match.ID {
			return nil, fmt.Errorf("conflicting legacy TradeMatchID for execution %s", execID)
		}
		if !legacyQty.Equal(lastQty) || !legacyPx.Equal(lastPx) || (executionDate != nil && (!legacyCum.Equal(cumQty) || !legacyLeaves.Equal(leavesQty))) {
			return nil, fmt.Errorf("conflicting legacy execution ID %s; operator date reconciliation required", execID)
		}
		return nil, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	eid := uuid.New()
	result, err := tx.Exec(ctx, `
		INSERT INTO executions (id, order_id, exec_id, last_qty, last_px, cum_qty, leaves_qty, adapter_id,execution_date,trade_match_id,contra_broker,contra_trader)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(order_id,execution_date,exec_id) DO NOTHING`, eid, orderID, execID, lastQty, lastPx, cumQty, leavesQty, adapterID, executionDate, match.ID, match.Broker, match.Trader)
	if err != nil {
		return nil, err
	}
	if result.RowsAffected() == 0 {
		var previousOrder uuid.UUID
		var previousMatchID string
		var qty, px decimal.Decimal
		if err := tx.QueryRow(ctx, `SELECT order_id,last_qty,last_px,trade_match_id FROM executions WHERE order_id=$1 AND execution_date IS NOT DISTINCT FROM $2::date AND exec_id=$3`, orderID, executionDate, execID).Scan(&previousOrder, &qty, &px, &previousMatchID); err != nil {
			return nil, err
		}
		if previousOrder != orderID || !qty.Equal(lastQty) || !px.Equal(lastPx) {
			return nil, fmt.Errorf("conflicting execution ID %s", execID)
		}
		if previousMatchID != "" && match.ID != "" && previousMatchID != match.ID {
			return nil, fmt.Errorf("conflicting TradeMatchID for execution %s", execID)
		}
		return nil, nil
	}
	ad := adapterID
	return &domain.Execution{
		ID: eid, OrderID: orderID, ExecID: execID, LastQty: lastQty, LastPx: lastPx,
		TradeMatchID: match.ID, ContraBroker: match.Broker, ContraTrader: match.Trader,
		CumQty: cumQty, LeavesQty: leavesQty, AdapterID: &ad, CreatedAt: time.Now().UTC(),
	}, nil
}

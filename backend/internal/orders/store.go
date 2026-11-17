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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

var ErrSendAlreadyAttempted = errors.New("fix send already attempted")
var ErrCannotFailover = errors.New("cannot failover after send attempted")

var ErrNotFound = errors.New("order not found")
var ErrInvalidCancel = errors.New("invalid cancel")
var ErrInstrumentNotFound = errors.New("instrument not found")

// Store holds an order only until QUIK closes it: a broker cancels through our
// ClOrdID (FIX 41), which QUIK Orders does not keep. Everything else about an
// order, its trades and references is read from QUIK. See PurgeFinished.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (domain.Order, error) {
	return s.scanOrder(s.pool.QueryRow(ctx, orderSelect+` WHERE o.id=$1`, id))
}

func (s *Store) GetForBroker(ctx context.Context, brokerID int64, id uuid.UUID) (domain.Order, error) {
	return s.scanOrder(s.pool.QueryRow(ctx, orderSelect+` WHERE o.id=$1 AND o.broker_id=$2`, id, brokerID))
}

const orderSelect = `
SELECT o.id, o.broker_id, o.client_order_id, o.side, o.order_type,
       o.time_in_force, o.quantity, o.price, o.filled_quantity, o.status, o.adapter_id,
       o.fix_cl_ord_id, o.quik_order_id, o.reject_reason, o.created_at, o.updated_at,
       o.symbol, o.board,
       COALESCE(o.fix_account, '') AS trading_account,
       o.currency,
       COALESCE(NULLIF(o.fix_firm, ''), '') AS quik_firm,
       COALESCE(NULLIF(o.fix_client_id, ''), '') AS client_code,
       COALESCE(o.request_id, ''), COALESCE(o.trace_id, ''),
       o.fix_send_attempted_at
FROM orders o`

func (s *Store) scanOrder(row pgx.Row) (domain.Order, error) {
	var o domain.Order
	var price *decimal.Decimal
	err := row.Scan(
		&o.ID, &o.BrokerID, &o.ClientOrderID, &o.Side, &o.OrderType,
		&o.TimeInForce, &o.Quantity, &price, &o.FilledQuantity, &o.Status, &o.AdapterID,
		&o.FixClOrdID, &o.QuikOrderID, &o.RejectReason, &o.CreatedAt, &o.UpdatedAt,
		&o.Symbol, &o.Board, &o.TradingAccount, &o.Currency, &o.QuikFirm, &o.ClientCode,
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
	BrokerID   int64
	FixAccount string
	ClientCode string // FIX tag 109 вЂ” arbitrary broker client code
	QuikFirm   string // FIX tag 5018 вЂ” from API token
	// Symbol, Board and Currency come from QUIK Securities (FIX 48, 100, 15).
	Symbol      string
	Board       string
	Currency    string
	Side        domain.Side
	OrderType   domain.OrderType
	TimeInForce string
	Quantity    decimal.Decimal
	Price       *decimal.Decimal
	RequestID   string
	TraceID     string
}

func (s *Store) CreateWithOutbox(ctx context.Context, in CreateInput) (domain.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)
	policies, err := lockTradingLimits(ctx, tx, in.BrokerID)
	if err != nil {
		return domain.Order{}, err
	}
	if err := checkTradingLimits(ctx, tx, in, policies); err != nil {
		return domain.Order{}, err
	}

	id := uuid.New()
	_, err = tx.Exec(ctx, `
		INSERT INTO orders (
			id, broker_id, client_order_id, symbol, currency, side, order_type,
			time_in_force, quantity, price, status, fix_cl_ord_id, board, fix_account,
			fix_client_id, fix_firm, request_id, trace_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		id, in.BrokerID, id.String(), in.Symbol, in.Currency, in.Side, in.OrderType,
		in.TimeInForce, in.Quantity, in.Price, domain.OrderQueued,
		fmt.Sprintf("G%s", id.String()[:32]), in.Board, in.FixAccount,
		in.ClientCode, in.QuikFirm, nullIfEmpty(in.RequestID), nullIfEmpty(in.TraceID),
	)
	if err != nil {
		return domain.Order{}, err
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
			return domain.Order{}, err
		}
	}

	payload, _ := json.Marshal(map[string]any{"order_id": id.String(), "request_id": in.RequestID, "trace_id": in.TraceID})
	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
		VALUES ('order', $1, 'ORDER_SEND', $2)`, id.String(), payload)
	if err != nil {
		return domain.Order{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	return s.Get(ctx, id)
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

// ReleaseSendFence undoes FenceSendAttempt when the adapter proved that nothing
// reached the wire, and returns the order to the queue for another adapter.
func (s *Store) ReleaseSendFence(ctx context.Context, id uuid.UUID, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var from domain.OrderStatus
	var attempted *time.Time
	if err := tx.QueryRow(ctx, `SELECT status, fix_send_attempted_at FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&from, &attempted); err != nil {
		return err
	}
	if attempted == nil || (from != domain.OrderQueued && from != domain.OrderRouting) {
		return ErrCannotFailover
	}
	_, err = tx.Exec(ctx, `
		UPDATE orders SET fix_send_attempted_at=NULL, status=$2, adapter_id=NULL, updated_at=now() WHERE id=$1`, id, domain.OrderQueued)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, message)
		VALUES ($1,'ORDER_FAILOVER',$2,$3,$4)`, id, string(from), string(domain.OrderQueued), reason)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

const orderLockSelect = `
		SELECT id, broker_id, client_order_id, side, order_type,
		       time_in_force, quantity, price, filled_quantity, status, adapter_id,
		       fix_cl_ord_id, quik_order_id, reject_reason, created_at, updated_at
		FROM orders `

func scanLockedOrder(row pgx.Row) (domain.Order, error) {
	var o domain.Order
	var price *decimal.Decimal
	err := row.Scan(&o.ID, &o.BrokerID, &o.ClientOrderID, &o.Side, &o.OrderType,
		&o.TimeInForce, &o.Quantity, &price, &o.FilledQuantity, &o.Status, &o.AdapterID,
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

// ApplyExecutionReport moves the order along. Fills are not recorded: trades are
// read from QUIK Trades. A replay cannot move totals, as CumQty is absolute.
func (s *Store) ApplyExecutionReport(ctx context.Context, fixClOrdID, origClOrdID, execType, ordStatus, quikOrderID string, lastQty, cumQty decimal.Decimal, adapterID, text, ordRejReason, rawFIX string) (domain.Order, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Order{}, err
	}
	defer tx.Rollback(ctx)

	o, err := lockOrderForFIX(ctx, tx, fixClOrdID, origClOrdID, quikOrderID)
	if err != nil {
		return domain.Order{}, err
	}

	claimed, err := claimExecutionReceipt(ctx, tx, o.ID)
	if err != nil {
		return domain.Order{}, err
	}
	if !claimed {
		return domain.Order{}, nil
	}
	to := mapExecStatus(execType, ordStatus, o.Status)
	erMsg := formatExecutionMessage(execType, ordStatus, text, ordRejReason)
	payload := fixWirePayload(rawFIX)
	filled := effectiveFillQty(o.FilledQuantity, lastQty, cumQty)

	if !CanTransition(o.Status, to) && o.Status != to {
		if lastQty.GreaterThan(decimal.Zero) {
			_, err = tx.Exec(ctx, `
				UPDATE orders SET filled_quantity=$2,
				                  quik_order_id=COALESCE(NULLIF($3,''), quik_order_id),
				                  adapter_id=COALESCE(NULLIF($4,''), adapter_id), updated_at=now()
				WHERE id=$1`, o.ID, filled, quikOrderID, adapterID)
			if err != nil {
				return domain.Order{}, err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message, payload)
			VALUES ($1,'EXECUTION_REPORT',$2,$3,$4,$5,$6)`,
			o.ID, string(o.Status), string(to), adapterID, "ignored illegal ER transition; "+erMsg, payload); err != nil {
			return domain.Order{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return domain.Order{}, err
		}
		return s.Get(ctx, o.ID)
	}

	rejectReason := o.RejectReason
	if to == domain.OrderRejected {
		if r := composeRejectReason(text, ordRejReason); r != nil {
			rejectReason = r
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE orders SET status=$2, filled_quantity=$3, quik_order_id=COALESCE(NULLIF($4,''), quik_order_id),
		                adapter_id=COALESCE(NULLIF($5,''), adapter_id), reject_reason=$6, updated_at=now()
		WHERE id=$1`, o.ID, to, filled, quikOrderID, adapterID, rejectReason)
	if err != nil {
		return domain.Order{}, err
	}
	fromStr := string(o.Status)
	toStr := string(to)
	_, err = tx.Exec(ctx, `
		INSERT INTO order_events (order_id, event_type, from_status, to_status, adapter_id, message, payload)
		VALUES ($1,'ORDER_EXECUTION_REPORT',$2,$3,$4,$5,$6)`,
		o.ID, fromStr, toStr, adapterID, erMsg, payload)
	if err != nil {
		return domain.Order{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.Order{}, err
	}
	out, err := s.Get(ctx, o.ID)
	out.ExecutionBecameTerminal = !IsTerminal(o.Status) && IsTerminal(to)
	return out, err
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

// CancelRejectReason is the reason of the latest cancel reject QUIK sent for the
// order since the given time; "" when there is none.
func (s *Store) CancelRejectReason(ctx context.Context, orderID uuid.UUID, since time.Time) (string, error) {
	var msg string
	err := s.pool.QueryRow(ctx, `
		SELECT message FROM order_events
		WHERE order_id=$1 AND event_type='ORDER_CANCEL_REJECTED' AND created_at >= $2
		ORDER BY created_at DESC, id DESC LIMIT 1`, orderID, since).Scan(&msg)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	msg = strings.TrimPrefix(msg, "cancel rejected")
	return strings.TrimSpace(strings.TrimPrefix(msg, ":")), nil
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

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
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

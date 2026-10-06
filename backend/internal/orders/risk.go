package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

var ErrRiskPolicyConflict = errors.New("trading policy changed; reload before saving")
var ErrInvalidRiskPolicy = errors.New("invalid trading policy, operator or change reason")

type RiskRejected struct {
	Code    string
	Message string
}

func (e *RiskRejected) Error() string { return e.Message }

type TradingLimit struct {
	BrokerID      int64            `json:"broker_id"`
	Paused        bool             `json:"paused"`
	MaxQuantity   *decimal.Decimal `json:"max_quantity"`
	MaxOpenOrders *int             `json:"max_open_orders"`
	Revision      int64            `json:"revision"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

func scanTradingLimit(row pgx.Row) (TradingLimit, error) {
	var p TradingLimit
	err := row.Scan(&p.BrokerID, &p.Paused, &p.MaxQuantity, &p.MaxOpenOrders, &p.Revision, &p.UpdatedAt)
	return p, err
}

const tradingLimitColumns = `broker_id,paused,max_quantity,max_open_orders,revision,updated_at`

type TradingLimitChange struct {
	ID        int64         `json:"id"`
	BrokerID  int64         `json:"broker_id"`
	ActorID   string        `json:"actor_id"`
	Reason    string        `json:"reason"`
	Before    *TradingLimit `json:"before"`
	After     TradingLimit  `json:"after"`
	CreatedAt time.Time     `json:"created_at"`
}

// A descending ID cursor provides stable pages while new changes arrive.
func (s *Store) ListTradingLimitHistory(ctx context.Context, brokerID *int64, beforeID int64, limit int) ([]TradingLimitChange, error) {
	if limit <= 0 || limit > 101 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id,broker_id,actor_id,reason,before_policy,after_policy,created_at
 FROM trading_limit_history WHERE ($1::bigint IS NULL OR broker_id=$1)
 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT $3`, brokerID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TradingLimitChange, 0)
	for rows.Next() {
		var c TradingLimitChange
		var before, after []byte
		if err := rows.Scan(&c.ID, &c.BrokerID, &c.ActorID, &c.Reason, &before, &after, &c.CreatedAt); err != nil {
			return nil, err
		}
		if len(before) > 0 {
			if err := json.Unmarshal(before, &c.Before); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(after, &c.After); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ListTradingLimits(ctx context.Context) ([]TradingLimit, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+tradingLimitColumns+` FROM trading_limits ORDER BY broker_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TradingLimit, 0)
	for rows.Next() {
		p, err := scanTradingLimit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpdateTradingLimit(ctx context.Context, p TradingLimit, actor, reason string) error {
	reason = strings.TrimSpace(reason)
	if p.BrokerID < 0 || p.Revision < 0 || strings.TrimSpace(actor) == "" || utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 1000 ||
		(p.MaxQuantity != nil && (!p.MaxQuantity.IsPositive() || p.MaxQuantity.GreaterThan(decimal.NewFromInt(10000000000000000)))) ||
		(p.MaxOpenOrders != nil && (*p.MaxOpenOrders < 1 || *p.MaxOpenOrders > 1000000)) {
		return ErrInvalidRiskPolicy
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// All writers acquire the global gate first. Order creation holds it shared,
	// making updates effective at a committed order acceptance boundary.
	if _, err = tx.Exec(ctx, `SELECT broker_id FROM trading_limits WHERE broker_id=0 FOR UPDATE`); err != nil {
		return err
	}
	if p.BrokerID > 0 {
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM brokers WHERE id=$1)`, p.BrokerID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return ErrInvalidRiskPolicy
		}
	}
	before, err := scanTradingLimit(tx.QueryRow(ctx, `SELECT `+tradingLimitColumns+` FROM trading_limits WHERE broker_id=$1 FOR UPDATE`, p.BrokerID))
	var beforeJSON []byte
	if errors.Is(err, pgx.ErrNoRows) {
		if p.Revision != 0 {
			return ErrRiskPolicyConflict
		}
	} else if err != nil {
		return err
	} else {
		if p.Revision != before.Revision {
			return ErrRiskPolicyConflict
		}
		beforeJSON, err = json.Marshal(before)
		if err != nil {
			return err
		}
	}
	saved, err := scanTradingLimit(tx.QueryRow(ctx, `INSERT INTO trading_limits(broker_id,paused,max_quantity,max_open_orders)
 VALUES($1,$2,$3,$4) ON CONFLICT(broker_id) DO UPDATE SET paused=EXCLUDED.paused,max_quantity=EXCLUDED.max_quantity,
 max_open_orders=EXCLUDED.max_open_orders,revision=trading_limits.revision+1,updated_at=now() RETURNING `+tradingLimitColumns, p.BrokerID, p.Paused, p.MaxQuantity, p.MaxOpenOrders))
	if err != nil {
		return err
	}
	afterJSON, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO trading_limit_history(broker_id,actor_id,reason,before_policy,after_policy) VALUES($1,$2,$3,$4,$5)`, p.BrokerID, actor, reason, beforeJSON, afterJSON); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func lockTradingLimits(ctx context.Context, tx pgx.Tx, brokerID int64) ([]TradingLimit, error) {
	global, err := scanTradingLimit(tx.QueryRow(ctx, `SELECT `+tradingLimitColumns+` FROM trading_limits WHERE broker_id=0 FOR SHARE`))
	if err != nil {
		return nil, err
	}
	policies := []TradingLimit{global}
	broker, err := scanTradingLimit(tx.QueryRow(ctx, `SELECT `+tradingLimitColumns+` FROM trading_limits WHERE broker_id=$1 FOR SHARE`, brokerID))
	if err == nil {
		policies = append(policies, broker)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	for _, p := range policies {
		if p.MaxOpenOrders != nil {
			// Serialize creates for this broker across gateway replicas; cancellation and
			// executions may release capacity concurrently and never increase this count.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('gateway-trading-limit:' || $1::bigint::text,0))`, brokerID); err != nil {
				return nil, err
			}
			break
		}
	}
	return policies, nil
}

func checkTradingLimits(ctx context.Context, tx pgx.Tx, in CreateInput, policies []TradingLimit) error {
	return evaluateTradingLimits(in, policies, func() (int, error) {
		var n int
		err := tx.QueryRow(ctx, `SELECT count(*) FROM orders WHERE broker_id=$1 AND status NOT IN ('FILLED','CANCELLED','REJECTED','EXPIRED')`, in.BrokerID).Scan(&n)
		return n, err
	})
}

func evaluateTradingLimits(in CreateInput, policies []TradingLimit, countOpen func() (int, error)) error {
	var openCount *int
	for _, p := range policies {
		scope := "global"
		if p.BrokerID != 0 {
			scope = fmt.Sprintf("broker %d", p.BrokerID)
		}
		if p.Paused {
			return &RiskRejected{Code: "TRADING_PAUSED", Message: scope + ": new orders are paused"}
		}
		if p.MaxQuantity != nil && in.Quantity.GreaterThan(*p.MaxQuantity) {
			return &RiskRejected{Code: "MAX_ORDER_QUANTITY", Message: scope + ": quantity exceeds trading limit"}
		}
		if p.MaxOpenOrders != nil {
			if openCount == nil {
				n, err := countOpen()
				if err != nil {
					return err
				}
				openCount = &n
			}
			if *openCount >= *p.MaxOpenOrders {
				return &RiskRejected{Code: "MAX_OPEN_ORDERS", Message: scope + ": maximum open orders reached"}
			}
		}
	}
	return nil
}

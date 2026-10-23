package orders

import (
	"context"
	"fmt"

	"github.com/fix-gateway/backend/internal/domain"
)

// A source row may have no local request. Never manufacture one to attach FIX
// history or permit cancellation of an order created outside this Gateway.
func (s *Store) FindByQuikOrderID(ctx context.Context, venueID, firm string, brokerID *int64) (domain.Order, error) {
	rows, err := s.pool.Query(ctx, orderSelect+` WHERE o.quik_order_id=$1 AND o.fix_firm=$2
AND ($3::bigint IS NULL OR o.broker_id=$3) LIMIT 2`, venueID, firm, brokerID)
	if err != nil {
		return domain.Order{}, err
	}
	defer rows.Close()
	var result domain.Order
	n := 0
	for rows.Next() {
		result, err = s.scanOrder(rows)
		if err != nil {
			return domain.Order{}, err
		}
		n++
	}
	if err := rows.Err(); err != nil {
		return domain.Order{}, err
	}
	if n == 0 {
		return domain.Order{}, ErrNotFound
	}
	if n > 1 {
		return domain.Order{}, fmt.Errorf("ambiguous local QUIK order reference")
	}
	return result, nil
}

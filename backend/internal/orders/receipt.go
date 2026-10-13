package orders

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type receiptKey struct{}
type executionDateKey struct{}

func WithExecutionDate(ctx context.Context, date string) context.Context {
	return context.WithValue(ctx, executionDateKey{}, date)
}

func executionDateFromContext(ctx context.Context) (any, error) {
	value, _ := ctx.Value(executionDateKey{}).(string)
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("20060102", value)
	if err != nil {
		return nil, fmt.Errorf("invalid FIX execution date")
	}
	return t.Format("2006-01-02"), nil
}

// WithExecutionReceipt binds a local inbox delivery to the order transaction.
// It protects all report types if the process dies after commit but before the
// journal file is removed, including ACKs and cancellation rejections.
func WithExecutionReceipt(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, receiptKey{}, id)
}

func claimExecutionReceipt(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error) {
	id, _ := ctx.Value(receiptKey{}).(uuid.UUID)
	if id == uuid.Nil {
		return true, nil
	}
	r, err := tx.Exec(ctx, `INSERT INTO execution_inbox_receipts(id,order_id) VALUES($1,$2) ON CONFLICT(id) DO NOTHING`, id, orderID)
	if err != nil {
		return false, err
	}
	return r.RowsAffected() == 1, nil
}

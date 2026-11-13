package orders

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type receiptKey struct{}

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

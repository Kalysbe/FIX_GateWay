package adapters

import (
	"context"

	"github.com/fix-gateway/backend/internal/domain"
)

// Adapter is the replaceable FIX adapter abstraction.
// Order Service never depends on a concrete ARQA or simulator type.
type Adapter interface {
	ID() string
	Name() string
	Status() domain.AdapterStatus
	// SendOrder transmits NewOrderSingle and returns the outbound FIX wire string (SOH→|).
	SendOrder(ctx context.Context, order domain.Order) (rawFIX string, err error)
	CancelOrder(ctx context.Context, order domain.Order) error
	ReplaceOrder(ctx context.Context, order domain.Order) error
	HealthCheck(ctx context.Context) (domain.HealthDetails, error)
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	ActiveOrders() int64
	IncActive(delta int64)
	Meta() domain.Adapter
}

type ExecutionHandler = domain.ExecutionHandler
type ExecutionReport = domain.ExecutionReport

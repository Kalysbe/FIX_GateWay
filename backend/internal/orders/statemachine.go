package orders

import "github.com/fix-gateway/backend/internal/domain"

var transitions = map[domain.OrderStatus]map[domain.OrderStatus]bool{
	domain.OrderReceived: {
		domain.OrderValidating: true,
		domain.OrderRejected:   true,
		domain.OrderFailed:     true,
	},
	domain.OrderValidating: {
		domain.OrderValidated: true,
		domain.OrderRejected:  true,
		domain.OrderFailed:    true,
	},
	domain.OrderValidated: {
		domain.OrderQueued:   true,
		domain.OrderRejected: true,
		domain.OrderFailed:   true,
	},
	domain.OrderQueued: {
		domain.OrderRouting: true,
		domain.OrderFailed:  true,
		domain.OrderCancelled: true,
	},
	domain.OrderRouting: {
		domain.OrderSent:    true,
		domain.OrderQueued:  true, // failover requeue
		domain.OrderFailed:  true,
		domain.OrderRejected: true,
	},
	domain.OrderSent: {
		domain.OrderAcknowledged:    true,
		domain.OrderRejected:        true,
		domain.OrderFailed:          true,
		domain.OrderPartiallyFilled: true,
		domain.OrderFilled:          true,
		domain.OrderCancelled:       true,
		domain.OrderExpired:         true,
	},
	domain.OrderAcknowledged: {
		domain.OrderPartiallyFilled: true,
		domain.OrderFilled:          true,
		domain.OrderCancelPending:   true,
		domain.OrderCancelled:       true,
		domain.OrderRejected:        true,
		domain.OrderExpired:         true,
	},
	domain.OrderPartiallyFilled: {
		domain.OrderPartiallyFilled: true,
		domain.OrderFilled:          true,
		domain.OrderCancelPending:   true,
		domain.OrderCancelled:       true,
		domain.OrderExpired:         true,
	},
	domain.OrderCancelPending: {
		domain.OrderCancelled:       true,
		domain.OrderAcknowledged:    true,
		domain.OrderPartiallyFilled: true,
		domain.OrderFilled:          true,
		domain.OrderRejected:        true,
	},
}

func CanTransition(from, to domain.OrderStatus) bool {
	if from == to {
		return true
	}
	next, ok := transitions[from]
	if !ok {
		return false
	}
	return next[to]
}

func IsTerminal(s domain.OrderStatus) bool {
	switch s {
	case domain.OrderFilled, domain.OrderCancelled, domain.OrderRejected, domain.OrderExpired, domain.OrderFailed:
		return true
	default:
		return false
	}
}

func IsExternallyAccepted(o domain.Order) bool {
	if o.QuikOrderID != nil && *o.QuikOrderID != "" {
		return true
	}
	switch o.Status {
	case domain.OrderAcknowledged, domain.OrderPartiallyFilled, domain.OrderFilled,
		domain.OrderCancelPending, domain.OrderCancelled, domain.OrderExpired:
		return true
	default:
		return false
	}
}

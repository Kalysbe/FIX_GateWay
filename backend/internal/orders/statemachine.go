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
		domain.OrderSent:            true,
		domain.OrderQueued:          true, // pre-send failover only
		domain.OrderFailed:          true,
		domain.OrderRejected:        true,
		domain.OrderAcknowledged:    true, // venue ER can beat MarkSent
		domain.OrderPartiallyFilled: true,
		domain.OrderFilled:          true,
		domain.OrderCancelled:       true,
		domain.OrderExpired:         true,
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

// ShouldAttemptFIXSend is the at-most-once guard: once a send is fenced or the
// order is already on the wire / terminal, the worker must not call SendOrder.
func ShouldAttemptFIXSend(o domain.Order) bool {
	if IsTerminal(o.Status) || IsExternallyAccepted(o) || o.Status == domain.OrderSent {
		return false
	}
	if o.FixSendAttemptedAt != nil {
		return false
	}
	return true
}

// CanClearRouteForFailover allows picking another adapter only before the wire send.
func CanClearRouteForFailover(o domain.Order) bool {
	if !ShouldAttemptFIXSend(o) {
		return false
	}
	return o.Status == domain.OrderQueued || o.Status == domain.OrderRouting
}

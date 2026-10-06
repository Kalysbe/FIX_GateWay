package orders

import (
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

func TestCanTransitionHappyPath(t *testing.T) {
	path := []domain.OrderStatus{
		domain.OrderReceived, domain.OrderValidating, domain.OrderValidated,
		domain.OrderQueued, domain.OrderRouting, domain.OrderSent,
		domain.OrderAcknowledged, domain.OrderFilled,
	}
	for i := 0; i < len(path)-1; i++ {
		if !CanTransition(path[i], path[i+1]) {
			t.Fatalf("expected transition %s -> %s", path[i], path[i+1])
		}
	}
}

func TestIllegalTransition(t *testing.T) {
	if CanTransition(domain.OrderFilled, domain.OrderQueued) {
		t.Fatal("filled -> queued must be illegal")
	}
}

func TestExternallyAccepted(t *testing.T) {
	qid := "123"
	o := domain.Order{Status: domain.OrderSent, QuikOrderID: &qid}
	if !IsExternallyAccepted(o) {
		t.Fatal("expected accepted when quik id set")
	}
}

func TestShouldAttemptFIXSendFence(t *testing.T) {
	now := domain.Order{Status: domain.OrderRouting}
	if !ShouldAttemptFIXSend(now) {
		t.Fatal("unfenced ROUTING must send")
	}
	ts := now.CreatedAt
	fenced := domain.Order{Status: domain.OrderRouting, FixSendAttemptedAt: &ts}
	if ShouldAttemptFIXSend(fenced) {
		t.Fatal("fenced order must not send again")
	}
	if CanClearRouteForFailover(fenced) {
		t.Fatal("fenced order must not failover")
	}
	sent := domain.Order{Status: domain.OrderSent}
	if ShouldAttemptFIXSend(sent) || CanClearRouteForFailover(sent) {
		t.Fatal("SENT must not send or failover")
	}
}

func TestRoutingAcceptsVenueER(t *testing.T) {
	if !CanTransition(domain.OrderRouting, domain.OrderAcknowledged) {
		t.Fatal("ROUTING -> ACKNOWLEDGED (ER before MarkSent)")
	}
	if !CanTransition(domain.OrderRouting, domain.OrderPartiallyFilled) {
		t.Fatal("ROUTING -> PARTIALLY_FILLED")
	}
}

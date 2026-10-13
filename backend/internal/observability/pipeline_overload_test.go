package observability

import (
	"fmt"
	"testing"
)

func TestNotificationFloodCoalescesByBrokerValue(t *testing.T) {
	p := NewPipeline(nil, nil, nil, DefaultSettings())
	broker := int64(42)
	for i := 0; i < 10000; i++ {
		// Each request holds a different pointer to the same broker ID.
		id := broker
		p.maybeNotify(Event{Type: TypeAuth, Subtype: AuthRateLimitExceeded, Severity: SeverityWarning, BrokerID: &id})
	}
	if len(p.pending) != 1 {
		t.Fatalf("notification keys = %d, want 1", len(p.pending))
	}
	for _, n := range p.pending {
		if n.Count != 10000 {
			t.Fatalf("count = %d", n.Count)
		}
	}
}

func TestNotificationFloodBoundsDistinctKeys(t *testing.T) {
	p := NewPipeline(nil, nil, nil, DefaultSettings())
	for i := 0; i < 10000; i++ {
		p.maybeNotify(Event{Type: TypeError, Subtype: fmt.Sprintf("error_%d", i), Severity: SeverityError})
	}
	if len(p.pending) != 128 {
		t.Fatalf("notification keys = %d, want 128", len(p.pending))
	}
}

func TestDiagnosticFloodDoesNotBlockProducer(t *testing.T) {
	settings := DefaultSettings()
	settings.ChannelSize = 1
	p := NewPipeline(nil, nil, nil, settings)
	for i := 0; i < 10000; i++ {
		p.Emit(Event{Type: TypeAPI, HTTPMethod: "POST"})
	}
	if len(p.ch) != 1 {
		t.Fatalf("queued = %d", len(p.ch))
	}
}

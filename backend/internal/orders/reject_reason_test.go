package orders

import "testing"

func TestComposeRejectReason(t *testing.T) {
	if got := composeRejectReason("", ""); got != nil {
		t.Fatalf("expected nil, got %q", *got)
	}
	got := composeRejectReason(" insufficient funds ", "")
	if got == nil || *got != "insufficient funds" {
		t.Fatalf("unexpected: %#v", got)
	}
	got = composeRejectReason("bad account", "1")
	if got == nil || *got != "bad account; OrdRejReason=1" {
		t.Fatalf("unexpected: %#v", got)
	}
	got = composeRejectReason("", "3")
	if got == nil || *got != "OrdRejReason=3" {
		t.Fatalf("unexpected: %#v", got)
	}
}

func TestFormatExecutionMessage(t *testing.T) {
	got := formatExecutionMessage("8", "8", "no money", "1")
	want := "execType=8 ordStatus=8 text=no money ordRejReason=1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

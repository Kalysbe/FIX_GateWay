package quikexport

import "testing"

func TestNormalizeSymbol(t *testing.T) {
	got, err := NormalizeSymbol(" mair ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "MAIR" {
		t.Fatalf("got %q", got)
	}
	if _, err := NormalizeSymbol("MAI"); err != ErrBadSymbol {
		t.Fatalf("want ErrBadSymbol, got %v", err)
	}
	if _, err := NormalizeSymbol("MAIRS"); err != ErrBadSymbol {
		t.Fatalf("want ErrBadSymbol, got %v", err)
	}
}

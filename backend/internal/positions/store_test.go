package positions

import "testing"

func TestNormalizeFirmIDs(t *testing.T) {
	got := NormalizeFirmIDs(" 2040 ", "2040", "MC0031400000", "", "mc0031400000")
	if len(got) != 2 || got[0] != "2040" || got[1] != "MC0031400000" {
		t.Fatalf("got %#v", got)
	}
	if got := NormalizeFirmIDs("", "  "); len(got) != 0 {
		t.Fatalf("empty %#v", got)
	}
}

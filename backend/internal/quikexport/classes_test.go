package quikexport

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeClasses(t *testing.T) {
	got, err := NormalizeClasses([]string{" LIST_GBA ", "AUCT_GBA", "LIST_GBA"})
	if err != nil || strings.Join(got, ",") != "LIST_GBA,AUCT_GBA" {
		t.Fatal(got, err)
	}
	// An empty choice serves every class; it must stay an empty JSON list.
	for _, empty := range [][]string{nil, {}} {
		if got, err := NormalizeClasses(empty); err != nil || got == nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
	many := make([]string, maxParamClasses+1)
	for i := range many {
		many[i] = "C" + strings.Repeat("X", i%30) + string(rune('A'+i%26))
	}
	for _, bad := range [][]string{{""}, {"LIST GBA"}, {"LIST_GBA,AUCT"}, {"'; DROP"}, {strings.Repeat("A", 33)}, many} {
		if _, err := NormalizeClasses(bad); !errors.Is(err, ErrInvalidClasses) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

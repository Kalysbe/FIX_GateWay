package quikexport

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeClasses(t *testing.T) {
	got, err := NormalizeClasses([]string{" LISTBND ", "AUCT_GBA", "LISTBND"})
	if err != nil || strings.Join(got, ",") != "LISTBND,AUCT_GBA" {
		t.Fatal(got, err)
	}
	many := make([]string, maxParamClasses+1)
	for i := range many {
		many[i] = "C" + strings.Repeat("X", i%30) + string(rune('A'+i%26))
	}
	for _, bad := range [][]string{nil, {}, {""}, {"LIST BND"}, {"LISTBND,AUCT"}, {"'; DROP"}, {strings.Repeat("A", 33)}, many} {
		if _, err := NormalizeClasses(bad); !errors.Is(err, ErrInvalidClasses) {
			t.Fatalf("%q accepted", bad)
		}
	}
}

package orders

import "testing"

func TestExecutionMatchFromFIX(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      executionMatch
	}{
		{"trade and contra", "8=FIX.4.2|17=LIST_GBA-2sw-S-1-1-N|880=261006-LIST_GBA-11094|375=2040|337=trader|", executionMatch{"261006-LIST_GBA-11094", "2040", "trader"}},
		{"SOH", "8=FIX.4.2\x01880=M1\x01375=2040\x01", executionMatch{"M1", "2040", ""}},
		{"opaque exec is not match", "17=exec-B-1|", executionMatch{}},
		{"multiple brokers not guessed", "880=M1|375=2040|375=3101|", executionMatch{"M1", "", ""}},
		{"conflicting match not guessed", "880=M1|880=M2|375=2040|", executionMatch{"", "2040", ""}},
		{"empty", "", executionMatch{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := executionMatchFromFIX(tc.raw); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

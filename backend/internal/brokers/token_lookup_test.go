package brokers

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeTokenLookup(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"  AbCd_123  ", "AbCd_123"}, {"AbCd_123…", "AbCd_123"}, {"AbCd_123...", "AbCd_123"},
		{"bearer " + strings.Repeat("a", 64), strings.Repeat("a", 64)},
		{"Bearer\t" + strings.Repeat("B", 43), strings.Repeat("B", 43)},
		{"AABBCCDD-0000-4000-8000-000000000001", "aabbccdd-0000-4000-8000-000000000001"},
	} {
		got, err := NormalizeTokenLookup(tc.input)
		if err != nil || got != tc.want {
			t.Fatalf("normalization failed: %v", err)
		}
	}
	for _, input := range []string{"", "short", "AbCd_1234", "%' OR 1=1 --", "header.payload.signature", "invalid-token with-spaces", strings.Repeat("x", 513)} {
		if _, err := NormalizeTokenLookup(input); !errors.Is(err, ErrTokenLookup) {
			t.Fatal("invalid search accepted")
		}
	}
}

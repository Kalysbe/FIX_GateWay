package domain

import (
	"strings"
	"testing"
)

func TestCheckFIXTextRejectsFramingCharacters(t *testing.T) {
	for _, v := range []string{"006\x015018=OTHER", "a\nb", "a\rb", "a\x00", "a\x7f", "a\tb", string([]byte{0xff})} {
		if err := CheckFIXText("client_code", v, MaxFIXTextLen); err == nil {
			t.Fatalf("%q must be rejected", v)
		}
	}
	for _, v := range []string{"", "1-3101-006", "kalys", "LISTEDSTOCKK", "Клиент 1", "a=b"} {
		if err := CheckFIXText("client_code", v, MaxFIXTextLen); err != nil {
			t.Fatalf("%q: %v", v, err)
		}
	}
	if err := CheckFIXText("board", strings.Repeat("Я", MaxFIXTextLen), MaxFIXTextLen); err != nil {
		t.Fatalf("limit counts runes: %v", err)
	}
	if err := CheckFIXText("board", strings.Repeat("A", MaxFIXTextLen+1), MaxFIXTextLen); err == nil {
		t.Fatal("too long value must be rejected")
	}
	if err := CheckFIXText("client_order_id", strings.Repeat("A", 500), 0); err != nil {
		t.Fatalf("maxLen 0 disables the limit: %v", err)
	}
}

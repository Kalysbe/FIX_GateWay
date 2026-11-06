package domain

import (
	"fmt"
	"unicode/utf8"
)

// MaxFIXTextLen bounds free-form values copied into outgoing FIX tags.
const MaxFIXTextLen = 64

// CheckFIXText rejects values that could break FIX framing: SOH (\x01) or any
// other control character would let a caller append extra tags (5018, 1, ...)
// to the outgoing message.
func CheckFIXText(name, v string, maxLen int) error {
	if !utf8.ValidString(v) {
		return fmt.Errorf("%s: недопустимая кодировка", name)
	}
	for _, r := range v {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%s: управляющие символы недопустимы", name)
		}
	}
	if maxLen > 0 && utf8.RuneCountInString(v) > maxLen {
		return fmt.Errorf("%s: не длиннее %d символов", name, maxLen)
	}
	return nil
}

// CheckFIXTexts checks name/value pairs with the default length limit.
func CheckFIXTexts(pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := CheckFIXText(pairs[i], pairs[i+1], MaxFIXTextLen); err != nil {
			return err
		}
	}
	return nil
}

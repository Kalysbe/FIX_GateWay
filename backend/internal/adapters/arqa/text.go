package arqa

import (
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// QUIK can send display text in Windows-1251. Keep UTF-8 and ASCII unchanged,
// and decode legacy text before JSON serialization or database sanitization.
func decodeFIXText(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	decoded, err := charmap.Windows1251.NewDecoder().String(value)
	if err != nil {
		return value
	}
	return decoded
}

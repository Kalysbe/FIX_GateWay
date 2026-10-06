package domain

import "strings"

// SecurityRef is one instrument from FIX Security Definition (35=d).
type SecurityRef struct {
	Symbol       string `json:"symbol"`
	SecurityID   string `json:"security_id"`
	Board        string `json:"board"` // class code (ExDestination / UnderlyingSecurityExchange)
	Currency     string `json:"currency"`
	SecurityType string `json:"security_type"`
	Name         string `json:"name"`
}

// NormalizeSecurityType trims FIX/ARQA SecurityType (167 / 310).
// ARQA government bonds use "GO" (not FIX "GOVT").
func NormalizeSecurityType(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch s {
	case "GOVT", "GOV":
		return "GO"
	default:
		return s
	}
}

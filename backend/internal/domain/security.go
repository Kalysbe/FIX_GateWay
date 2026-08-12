package domain

// SecurityRef is one instrument from FIX Security Definition (35=d).
type SecurityRef struct {
	Symbol       string `json:"symbol"`
	SecurityID   string `json:"security_id"`
	Board        string `json:"board"` // class code (ExDestination / UnderlyingSecurityExchange)
	Currency     string `json:"currency"`
	SecurityType string `json:"security_type"`
	Name         string `json:"name"`
}

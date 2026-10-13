package domain

import "time"

// MarketDataSubscription describes the server-owned FIX stream, independently
// of the browser connection and the instrument dictionary.
type MarketDataSubscription struct {
	AdapterID      string     `json:"adapter_id,omitempty"`
	Status         string     `json:"status"`
	Expected       int        `json:"expected"`
	Confirmed      int        `json:"confirmed"`
	Error          string     `json:"error,omitempty"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	LastReceivedAt *time.Time `json:"last_received_at,omitempty"`
}

func (q MarketQuote) HasData() bool {
	return q.MDReqID != "" || q.RawFIX != "" || q.TradingStatus != "" || q.TradingSessionID != "" ||
		q.Bid != "" || q.Offer != "" || q.Last != "" || q.Volume != "" || q.Value != "" ||
		q.Open != "" || q.High != "" || q.Low != "" || q.Close != "" || q.WAPR != "" ||
		q.CurrentPrice != "" || q.OfficialPrice != "" || len(q.Bids) > 0 || len(q.Offers) > 0
}

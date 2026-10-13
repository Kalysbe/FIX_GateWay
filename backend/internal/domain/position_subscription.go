package domain

import "time"

// PositionSubscription describes the gateway-owned stream, including an empty
// confirmed snapshot. Browser sessions do not control its lifecycle.
type PositionSubscription struct {
	AdapterID      string     `json:"adapter_id,omitempty"`
	RequestID      string     `json:"pos_req_id,omitempty"`
	Status         string     `json:"status"`
	Mode           string     `json:"mode,omitempty"`
	Positions      int        `json:"positions"`
	Error          string     `json:"error,omitempty"`
	NextRetryAt    *time.Time `json:"next_retry_at,omitempty"`
	LastReceivedAt *time.Time `json:"last_received_at,omitempty"`
}

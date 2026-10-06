package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/fix-gateway/backend/internal/orders"
)

func (s *Server) tradingLimitHistory(w http.ResponseWriter, r *http.Request) {
	var brokerID *int64
	if raw := r.URL.Query().Get("broker_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 0 {
			writeErr(w, 400, "invalid broker_id")
			return
		}
		brokerID = &id
	}
	var beforeID int64
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 {
			writeErr(w, 400, "invalid before_id")
			return
		}
		beforeID = id
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeErr(w, 400, "limit must be 1–100")
			return
		}
		limit = n
	}
	history, err := s.orderStore.ListTradingLimitHistory(r.Context(), brokerID, beforeID, limit+1)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	var next *int64
	if len(history) > limit {
		id := history[limit-1].ID
		next = &id
		history = history[:limit]
	}
	writeJSON(w, 200, map[string]any{"history": history, "next_before_id": next})
}

func (s *Server) listTradingLimits(w http.ResponseWriter, r *http.Request) {
	policies, err := s.orderStore.ListTradingLimits(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"policies": policies})
}

func (s *Server) updateTradingLimit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BrokerID      *int64          `json:"broker_id"`
		Paused        *bool           `json:"paused"`
		MaxQuantity   json.RawMessage `json:"max_quantity"`
		MaxOpenOrders json.RawMessage `json:"max_open_orders"`
		Revision      *int64          `json:"revision"`
		Reason        string          `json:"reason"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if body.BrokerID == nil || body.Paused == nil || body.Revision == nil || len(body.MaxQuantity) == 0 || len(body.MaxOpenOrders) == 0 {
		writeErr(w, 400, "broker_id, paused, max_quantity, max_open_orders and revision are required")
		return
	}
	policy := orders.TradingLimit{BrokerID: *body.BrokerID, Paused: *body.Paused, Revision: *body.Revision}
	if err := json.Unmarshal(body.MaxQuantity, &policy.MaxQuantity); err != nil {
		writeErr(w, 400, "invalid max_quantity")
		return
	}
	if err := json.Unmarshal(body.MaxOpenOrders, &policy.MaxOpenOrders); err != nil {
		writeErr(w, 400, "invalid max_open_orders")
		return
	}
	principal, ok := AdminFromContext(r.Context())
	if !ok {
		writeErr(w, 401, "unauthorized")
		return
	}
	err := s.orderStore.UpdateTradingLimit(r.Context(), policy, principal.UserID, body.Reason)
	if errors.Is(err, orders.ErrRiskPolicyConflict) {
		writeErr(w, 409, err.Error())
		return
	}
	if errors.Is(err, orders.ErrInvalidRiskPolicy) {
		writeErr(w, 400, err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "saved"})
}

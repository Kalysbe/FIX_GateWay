package api

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/ratelimit"
)

func (s *Server) SetAdmission(store *ratelimit.Store) { s.ratePolicies = store }
func rateOperation(r *http.Request) (string, string) {
	path := r.URL.Path
	if strings.HasPrefix(path, "/api/v1/positions") && r.Method != http.MethodGet {
		return "Trading", "MaintainPositions"
	}
	if tradingRequest(r) {
		if strings.HasSuffix(path, "/cancel") {
			return "Trading", "CancelOrder"
		}
		return "Trading", "SubmitOrder"
	}
	switch path {
	case "/api/v1/firm":
		return "System", "GetFirm"
	case "/api/v1/positions":
		return "Queries", "GetPositions"
	case "/api/v1/instruments":
		return "Queries", "GetSecurities"
	case "/api/v1/orders", "/api/v1/orders/active":
		return "Queries", "GetOrders"
	case "/api/v1/accounts":
		return "Queries", "GetAccounts"
	case "/api/v1/trades":
		return "Queries", "GetTrades"
	case "/api/v1/orderbook":
		return "Queries", "GetOrderBook"
	}
	if strings.HasPrefix(path, "/api/v1/orders/") {
		return "Queries", "GetOrder"
	}
	return "Queries", "OtherQuery"
}
func (s *Server) admitBroker(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := BrokerFromContext(r.Context())
		if !ok {
			writeErr(w, 401, "unauthorized")
			return
		}
		if s.ratePolicies == nil {
			writeErr(w, 503, "rate limit configuration unavailable")
			return
		}
		category, operation := rateOperation(r)
		release, rejection := s.ratePolicies.Admission.Acquire("broker", strconv.FormatInt(b.ID, 10), category, operation)
		if rejection != nil {
			if bag := observability.BagFrom(r.Context()); bag != nil {
				bag.SkipAPILog = !rejection.Sample
			}
			observability.AnnotateError(r, "RATE_LIMIT_EXCEEDED", rejection.Error(), "admission")
			retry := int(math.Ceil(rejection.RetryAfter.Seconds()))
			if retry < 1 {
				retry = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			if rejection.Sample && s.log != nil {
				s.log.Warn("RATE_LIMIT_EXCEEDED", "broker_id", b.ID, "method", operation, "limit", rejection.Policy.Rate, "burst", rejection.Policy.Burst, "current_rate", rejection.CurrentRate, "reason", rejection.Reason, "request_id", observability.RequestIDFrom(r.Context()))
			}
			// Do not emit multiple persisted diagnostics for every rejected message.
			writeJSON(w, 429, map[string]any{"error": rejection.Error(), "code": "RESOURCE_EXHAUSTED", "operation": operation, "retry_after_ms": rejection.RetryAfter.Milliseconds()})
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) listRateLimits(w http.ResponseWriter, r *http.Request) {
	policies, err := s.ratePolicies.List(r.Context())
	if err != nil {
		writeErr(w, 500, "cannot read rate limits")
		return
	}
	writeJSON(w, 200, map[string]any{"policies": policies, "usage": s.ratePolicies.Admission.Snapshot(), "operations": []string{"*", "Trading", "Queries", "System", "SubmitOrder", "CancelOrder", "MaintainPositions", "GetOrder", "GetOrders", "GetPositions", "GetSecurities", "GetFirm", "GetAccounts", "GetTrades", "GetOrderBook", "OtherQuery", "Connect", "Subscribe", "Ping"}})
}
func (s *Server) updateRateLimit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ratelimit.Policy
		Reason string `json:"reason"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeErr(w, 400, "invalid policy")
		return
	}
	if err := body.Policy.Validate(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	valid := map[string]bool{"*": true, "Trading": true, "Queries": true, "System": true}
	if body.Scope == "broker" {
		for _, op := range []string{"SubmitOrder", "CancelOrder", "MaintainPositions", "GetOrder", "GetOrders", "GetPositions", "GetSecurities", "GetFirm", "GetAccounts", "GetTrades", "GetOrderBook", "OtherQuery"} {
			valid[op] = true
		}
	}
	if body.Scope == "admin" {
		valid = map[string]bool{"*": true, "System": true, "Connect": true, "Subscribe": true, "Ping": true}
	}
	if !valid[body.Operation] {
		writeErr(w, 400, "operation is not available for this scope")
		return
	}
	if len(strings.TrimSpace(body.Reason)) < 3 || len(body.Reason) > 1000 {
		writeErr(w, 400, "change reason required (3..1000 characters)")
		return
	}
	principal, _ := AdminFromContext(r.Context())
	if err := s.ratePolicies.Upsert(r.Context(), body.Policy, principal.UserID, body.Reason); err != nil {
		writeErr(w, 400, "policy update failed: "+err.Error())
		return
	}
	if err := s.ratePolicies.Refresh(r.Context()); err != nil {
		writeErr(w, 503, "policy saved; refresh pending")
		return
	}
	writeJSON(w, 200, map[string]any{"policy": body.Policy})
}

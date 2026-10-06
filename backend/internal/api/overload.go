package api

import (
	"context"
	"net/http"
	"strings"
	"time"
)

// Trading submissions have their own capacity so a polling flood cannot occupy
// every admitted request. Admission happens before authentication's DB queries.
func tradingRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && (r.URL.Path == "/api/v1/orders" ||
		(strings.HasPrefix(r.URL.Path, "/api/v1/orders/") && strings.HasSuffix(r.URL.Path, "/cancel")))
}

func (s *Server) protectOverload(next http.Handler) http.Handler {
	reads, trades, timeout := s.cfg.APIReadConcurrency, s.cfg.APITradeConcurrency, s.cfg.APIRequestTimeout
	if reads <= 0 {
		reads = 32
	}
	if trades <= 0 {
		trades = 16
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	readSlots, tradeSlots := make(chan struct{}, reads), make(chan struct{}, trades)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SSE owns a long-lived context. Health and metrics must remain available
		// when API capacity is exhausted; readiness already uses a short DB timeout.
		if !strings.HasPrefix(r.URL.Path, "/api/v1/") || r.URL.Path == "/api/v1/admin/events/stream" {
			next.ServeHTTP(w, r)
			return
		}
		slots := readSlots
		if tradingRequest(r) {
			slots = tradeSlots
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusServiceUnavailable, "server busy; request not accepted, retry later")
			return
		}
		// pgx respects the context for pool acquisition and queries. Do not use
		// TimeoutHandler: its detached handler could commit after a timeout reply.
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

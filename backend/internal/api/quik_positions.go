package api

import (
	"errors"
	"net/http"

	"github.com/fix-gateway/backend/internal/quikexport"
)

var errQuikPositionsUnavailable = errors.New("QUIK positions source unavailable")

// listBrokerPositionTable serves the token firm's current rows of one QUIK table,
// "MoneyLimits" or "DepoLimits", as stored: money and securities are separate
// methods (поручение пользователя 04.11.2026).
func (s *Server) listBrokerPositionTable(table string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firm, err := s.brokerFirm(r)
		if err != nil {
			s.writeOrderReadError(w, err)
			return
		}
		if s.quikRefs == nil {
			writeErr(w, http.StatusServiceUnavailable, errQuikPositionsUnavailable.Error())
			return
		}
		rows, err := s.quikRefs.ListPositionTable(r.Context(), table, []string{firm})
		if err != nil {
			if s.log != nil && !errors.Is(err, quikexport.ErrNotConfigured) {
				s.log.Error("QUIK positions read failed", "table", table, "error", err)
			}
			writeErr(w, http.StatusServiceUnavailable, errQuikPositionsUnavailable.Error())
			return
		}
		writeQuikJSON(w, rows)
	})
}

// listAdminPositions serves the rows of every firm; firm_id and broker_id narrow them.
func (s *Server) listAdminPositions(w http.ResponseWriter, r *http.Request) {
	firm, _, err := s.orderReadScope(r, true)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	var firms []string
	if firm != nil {
		firms = []string{*firm}
	}
	s.writeQuikPositions(w, r, firms)
}

func (s *Server) writeQuikPositions(w http.ResponseWriter, r *http.Request, firms []string) {
	if s.quikRefs == nil {
		writeErr(w, http.StatusServiceUnavailable, errQuikPositionsUnavailable.Error())
		return
	}
	positions, err := s.quikRefs.ListPositions(r.Context(), firms)
	if err != nil {
		if s.log != nil && !errors.Is(err, quikexport.ErrNotConfigured) {
			s.log.Error("QUIK MoneyLimits/DepoLimits read failed", "error", err)
		}
		writeErr(w, http.StatusServiceUnavailable, errQuikPositionsUnavailable.Error())
		return
	}
	writeQuikJSON(w, positions)
}

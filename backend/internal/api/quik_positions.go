package api

import (
	"errors"
	"net/http"

	"github.com/fix-gateway/backend/internal/quikexport"
)

var errQuikPositionsUnavailable = errors.New("QUIK positions source unavailable")

// listBrokerPositions serves the token firm's current rows of QUIK "MoneyLimits"
// (cash) and "DepoLimits" (instruments) as stored.
func (s *Server) listBrokerPositions(w http.ResponseWriter, r *http.Request) {
	firm, err := s.brokerFirm(r)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	s.writeQuikPositions(w, r, []string{firm})
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

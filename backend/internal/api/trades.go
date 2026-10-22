package api

import (
	"net/http"

	"github.com/google/uuid"
)

func (s *Server) getAdminTrade(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid trade id")
		return
	}
	trades, err := s.tradeLedger.Get(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if len(trades) == 0 {
		writeErr(w, http.StatusNotFound, "trade not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": trades})
}

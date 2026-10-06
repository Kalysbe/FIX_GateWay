package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/google/uuid"
)

func (s *Server) listReconciliation(w http.ResponseWriter, r *http.Request) {
	var orderID *uuid.UUID
	if raw := r.URL.Query().Get("order_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			writeErr(w, 400, "invalid order_id")
			return
		}
		orderID = &id
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			writeErr(w, 400, "limit must be 1–500")
			return
		}
		limit = n
	}
	cases, err := s.outbox.ListReconciliation(r.Context(), orderID, limit)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": cases})
}

func (s *Server) resolveReconciliation(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, 400, "invalid event id")
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	principal, ok := AdminFromContext(r.Context())
	if !ok {
		writeErr(w, 401, "unauthorized")
		return
	}
	err = s.outbox.ResolveReconciliation(r.Context(), id, principal.UserID, body.Note)
	if errors.Is(err, outbox.ErrReconciliationConflict) {
		writeErr(w, 409, err.Error())
		return
	}
	if errors.Is(err, outbox.ErrReconciliationNote) {
		writeErr(w, 400, err.Error())
		return
	}
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"status": "resolved"})
}

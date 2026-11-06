package api

import (
	"errors"
	"net/http"

	"github.com/fix-gateway/backend/internal/executioninbox"
	"github.com/google/uuid"
)

// Incoming FIX reports that cannot be applied pause trading. An operator can
// set the blocking report aside with a reason; it then becomes a manual
// reconciliation case and the next report is applied.

func (s *Server) getExecutionInbox(w http.ResponseWriter, r *http.Request) {
	if s.executionInbox == nil || s.inboxCases == nil {
		writeErr(w, http.StatusServiceUnavailable, "execution inbox unavailable")
		return
	}
	state := s.executionInbox.Status()
	out := map[string]any{
		"status": map[string]any{
			"pending": state.Pending, "bytes": state.Bytes, "failed": state.Failed,
			"healthy": state.Healthy(), "oldest_age_seconds": state.OldestAge.Seconds(),
		},
		"blocked": nil,
	}
	if b, ok := s.executionInbox.Blocked(); ok {
		out["blocked"] = blockedDTO(b)
	}
	cases, err := s.inboxCases.List(r.Context(), r.URL.Query().Get("open") == "true", 200)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	out["cases"] = cases
	writeJSON(w, http.StatusOK, out)
}

func blockedDTO(b executioninbox.Blocked) map[string]any {
	id, _ := executioninbox.EntryID(b.Name)
	m := map[string]any{
		"entry_id": id, "entry_name": b.Name, "error": b.Error, "failed_at": b.FailedAt,
		"attempts": b.Attempts, "read_error": b.ReadError,
	}
	if !b.ReadError {
		r := b.Entry.Report
		m["received_at"] = b.Entry.ReceivedAt
		m["report"] = map[string]any{
			"adapter_id": r.AdapterID, "cl_ord_id": r.ClOrdID, "orig_cl_ord_id": r.OrigClOrdID,
			"order_id": r.OrderID, "exec_id": r.ExecID, "exec_type": r.ExecType, "exec_trans_type": r.ExecTransType,
			"ord_status": r.OrdStatus, "last_qty": r.LastQty, "last_px": r.LastPx, "cum_qty": r.CumQty,
			"leaves_qty": r.LeavesQty, "text": r.Text, "protocol_reject": r.ProtocolReject,
			"ref_msg_type": r.RefMessageType, "ref_seq_num": r.RefSeqNum, "cancel_reject": r.CancelReject,
			"raw_fix": r.RawFIX,
		}
	}
	return m
}

func (s *Server) quarantineExecutionInboxEntry(w http.ResponseWriter, r *http.Request) {
	if s.executionInbox == nil || s.inboxCases == nil {
		writeErr(w, http.StatusServiceUnavailable, "execution inbox unavailable")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "invalid entry id")
		return
	}
	var body struct {
		Reason string `json:"reason"`
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
	var c executioninbox.Case
	err = s.executionInbox.WithBlocked(id, func(b executioninbox.Blocked) error {
		var orderID *uuid.UUID
		if !b.ReadError {
			rep := b.Entry.Report
			var err error
			if orderID, err = s.orderStore.OrderIDForFIX(r.Context(), rep.ClOrdID, rep.OrigClOrdID, rep.OrderID); err != nil {
				return err
			}
		}
		var err error
		c, err = s.inboxCases.Open(r.Context(), id, b, orderID, principal.UserID, body.Reason)
		return err
	})
	switch {
	case errors.Is(err, executioninbox.ErrNotBlocked):
		writeErr(w, http.StatusConflict, "entry is not the report currently blocking the execution inbox")
		return
	case errors.Is(err, executioninbox.ErrCaseNote):
		writeErr(w, 400, "reason must be 1–2000 characters")
		return
	case err != nil:
		s.writeInternal(w, err)
		return
	}
	s.writeAdminAudit(r, "EXECUTION_INBOX_QUARANTINE", "execution_inbox_entry", id.String(), nil, nil, map[string]any{
		"entry_name": c.EntryName, "reason": c.Reason, "apply_error": c.ApplyError, "order_id": c.OrderID,
		"cl_ord_id": c.ClOrdID, "exec_id": c.ExecID, "adapter_id": c.AdapterID,
	})
	writeJSON(w, http.StatusAccepted, c)
}

func (s *Server) resolveExecutionInboxCase(w http.ResponseWriter, r *http.Request) {
	if s.inboxCases == nil {
		writeErr(w, http.StatusServiceUnavailable, "execution inbox unavailable")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, "invalid entry id")
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
	c, err := s.inboxCases.Resolve(r.Context(), id, principal.UserID, body.Note)
	switch {
	case errors.Is(err, executioninbox.ErrCaseNote):
		writeErr(w, 400, "note must be 1–2000 characters")
		return
	case errors.Is(err, executioninbox.ErrCaseNotFound):
		writeErr(w, 404, err.Error())
		return
	case errors.Is(err, executioninbox.ErrCaseResolved):
		writeErr(w, 409, err.Error())
		return
	case err != nil:
		s.writeInternal(w, err)
		return
	}
	s.writeAdminAudit(r, "EXECUTION_INBOX_CASE_RESOLVE", "execution_inbox_entry", id.String(), nil,
		map[string]any{"resolved_at": nil}, map[string]any{"resolved_at": c.ResolvedAt, "note": c.ResolutionNote})
	writeJSON(w, http.StatusOK, c)
}

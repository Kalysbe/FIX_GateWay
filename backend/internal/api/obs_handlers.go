package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/google/uuid"
)

func (s *Server) parseLogFilter(r *http.Request) observability.Filter {
	q := r.URL.Query()
	f := observability.Filter{
		Type: q.Get("type"), Subtype: q.Get("subtype"), Severity: q.Get("severity"),
		Status: q.Get("status"), Result: q.Get("result"), ClientIP: q.Get("ip"),
		RequestID: q.Get("request_id"), TraceID: q.Get("trace_id"),
		ClientOrderID: q.Get("client_order_id"), AdapterID: q.Get("adapter_id"),
		Endpoint: q.Get("endpoint"), HTTPMethod: q.Get("http_method"), ErrorCode: q.Get("error_code"),
	}
	if v := q.Get("broker_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			f.BrokerID = &id
		}
	}
	if v := q.Get("status_code"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.StatusCode = &n
		}
	}
	if v := q.Get("order_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			f.OrderID = &id
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.From = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			f.To = t
		}
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.Limit = n
		}
	}
	if c := q.Get("cursor"); c != "" {
		if ts, id, err := observability.DecodeCursor(c); err == nil {
			f.CursorTS, f.CursorID = ts, id
		}
	}
	kind := q.Get("kind")
	if f.Type == "" && kind != "" && kind != "all" {
		switch kind {
		case "api":
			f.Type = observability.TypeAPI
		case "orders", "business":
			f.Type = observability.TypeBusiness
		case "errors":
			f.Type = observability.TypeError
		case "auth":
			f.Type = observability.TypeAuth
		case "fix":
			f.Types = []string{observability.TypeFIX, observability.TypeAdapter}
		case "system":
			f.Type = observability.TypeSystem
		case "audit":
			f.Type = observability.TypeAudit
		}
	}
	return f
}

func (s *Server) listLogs(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []any{}, "next_cursor": nil})
		return
	}
	s.writeLogList(w, r, s.parseLogFilter(r))
}

func (s *Server) writeLogList(w http.ResponseWriter, r *http.Request, f observability.Filter) {
	list, more, err := s.obs.Store().List(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	var next any
	if more && len(list) > 0 {
		last := list[len(list)-1]
		next = observability.EncodeCursor(last.Timestamp, last.ID)
	}
	out := map[string]any{"logs": list, "next_cursor": next}
	if r.URL.Query().Get("include_total") == "1" {
		cf := f
		cf.CursorTS = time.Time{}
		cf.CursorID = uuid.Nil
		ctx, cancel := context.WithTimeout(r.Context(), 1500*time.Millisecond)
		defer cancel()
		if total, err := s.obs.Store().Count(ctx, cf); err == nil {
			out["total"] = total
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getLog(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	e, err := s.obs.Store().Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"log": e})
}

func (s *Server) listBrokerLogs(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []any{}})
		return
	}
	f := s.parseLogFilter(r)
	f.BrokerID = &id
	s.writeLogList(w, r, f)
}

func (s *Server) listErrors(w http.ResponseWriter, r *http.Request) {
	f := s.parseLogFilter(r)
	f.Type = observability.TypeError
	s.listWithFilter(w, r, f)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	f := s.parseLogFilter(r)
	if f.Type == "" {
		f.Type = observability.TypeBusiness
	}
	s.listWithFilter(w, r, f)
}

func (s *Server) listWithFilter(w http.ResponseWriter, r *http.Request, f observability.Filter) {
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"logs": []any{}})
		return
	}
	list, more, err := s.obs.Store().List(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	var next any
	if more && len(list) > 0 {
		last := list[len(list)-1]
		next = observability.EncodeCursor(last.Timestamp, last.ID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": list, "events": list, "next_cursor": next})
}

func (s *Server) listObsAudit(w http.ResponseWriter, r *http.Request) {
	f := s.parseLogFilter(r)
	f.Type = observability.TypeAudit
	s.listWithFilter(w, r, f)
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"notifications": []any{}, "unread": 0})
		return
	}
	unreadOnly := r.URL.Query().Get("unread") == "1" || r.URL.Query().Get("unread") == "true"
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 20 {
		limit = 20
	}
	if page < 1 {
		page = 1
	}
	if r.URL.Query().Get("offset") == "" {
		offset = (page - 1) * limit
	}
	if offset < 0 {
		offset = 0
	}
	list, unread, total, err := s.obs.Store().ListNotifications(r.Context(), unreadOnly, limit, offset)
	if err != nil {
		observability.AnnotateError(r, observability.ErrDatabase, err.Error(), "observability")
		s.writeInternal(w, err)
		return
	}
	page = offset/limit + 1
	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": list,
		"unread":        unread,
		"total":         total,
		"limit":         limit,
		"offset":        offset,
		"page":          page,
		"has_more":      int64(offset+len(list)) < total,
	})
}

func (s *Server) readNotification(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.obs.Store().MarkNotificationRead(r.Context(), id); err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) readAllNotifications(w http.ResponseWriter, r *http.Request) {
	if s.obs != nil {
		_ = s.obs.Store().MarkAllNotificationsRead(r.Context())
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) brokerStatistics(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"statistics": observability.BrokerStats{BrokerID: id}})
		return
	}
	rangeQ := r.URL.Query().Get("range")
	trunc := "hour"
	from := time.Now().Add(-24 * time.Hour)
	switch rangeQ {
	case "7d", "week", "weekly":
		trunc, from = "day", time.Now().Add(-7*24*time.Hour)
	case "30d", "month", "monthly":
		trunc, from = "day", time.Now().Add(-30*24*time.Hour)
	}
	st, err := s.obs.Store().BrokerStatistics(r.Context(), id, from, trunc)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"statistics": st})
}

func (s *Server) brokerStatisticsPeriod(w http.ResponseWriter, r *http.Request, trunc string, from time.Time) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"series": []any{}})
		return
	}
	st, err := s.obs.Store().BrokerStatistics(r.Context(), id, from, trunc)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"statistics": st, "series": st.Series})
}

func (s *Server) brokerStatsDaily(w http.ResponseWriter, r *http.Request) {
	s.brokerStatisticsPeriod(w, r, "hour", time.Now().Add(-24*time.Hour))
}
func (s *Server) brokerStatsWeekly(w http.ResponseWriter, r *http.Request) {
	s.brokerStatisticsPeriod(w, r, "day", time.Now().Add(-7*24*time.Hour))
}
func (s *Server) brokerStatsMonthly(w http.ResponseWriter, r *http.Request) {
	s.brokerStatisticsPeriod(w, r, "day", time.Now().Add(-30*24*time.Hour))
}

func (s *Server) exportLogs(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeErr(w, http.StatusServiceUnavailable, "observability disabled")
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format != "json" {
		format = "csv"
	}
	f := s.parseLogFilter(r)
	count, err := s.obs.Store().Count(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	max := s.obs.Settings().ExportMaxSync
	if max <= 0 {
		max = 5000
	}
	if count > int64(max) {
		adminID := ""
		if a, ok := AdminFromContext(r.Context()); ok {
			adminID = a.UserID
		}
		job, err := s.obs.StartExport(r.Context(), adminID, format, f)
		if err != nil {
			if errors.Is(err, observability.ErrExportBusy) {
				w.Header().Set("Retry-After", "60")
				writeErr(w, http.StatusTooManyRequests, "export capacity exhausted")
				return
			}
			s.writeInternal(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job, "async": true})
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename="+observability.ExportFilename(format))
	if format == "json" {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header().Set("Content-Type", "text/csv")
	}
	w.WriteHeader(http.StatusOK)
	if _, err := s.obs.WriteSyncExport(r.Context(), format, f, w); err != nil {
		s.log.Error("export response interrupted", "error", err)
	}
}

func (s *Server) getExportJob(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	job, err := s.obs.Store().GetExportJob(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": job})
}

func (s *Server) downloadExportJob(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	job, err := s.obs.Store().GetExportJob(r.Context(), id)
	if err != nil || job.Status != "DONE" || job.FilePath == "" {
		writeErr(w, http.StatusNotFound, "export not ready")
		return
	}
	http.ServeFile(w, r, job.FilePath)
}

func (s *Server) orderTrace(w http.ResponseWriter, r *http.Request) {
	if s.obs == nil {
		writeJSON(w, http.StatusOK, map[string]any{"events": []any{}})
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	f := observability.Filter{OrderID: &id, Limit: 200}
	list, _, err := s.obs.Store().List(r.Context(), f)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": list})
}

func (s *Server) emitObs(e observability.Event) {
	if s.obs == nil {
		return
	}
	s.obs.Emit(e)
}

func (s *Server) writeAdminAudit(r *http.Request, action, targetType, targetID string, brokerID *int64, before, after any) {
	admin, _ := AdminFromContext(r.Context())
	ip, _ := s.clientIP(r)
	_ = s.audit.Write(r.Context(), audit.Entry{
		ActorType: "admin", ActorID: admin.UserID, BrokerID: brokerID,
		Action: action, EntityType: targetType, EntityID: targetID, IP: ip, Result: "ok",
	})
	var bRaw, aRaw json.RawMessage
	if before != nil {
		bRaw = observability.SafeJSON(toMap(before))
	}
	if after != nil {
		aRaw = observability.SafeJSON(toMap(after))
	}
	s.emitObs(observability.Event{
		Type: observability.TypeAudit, Subtype: action, ActorType: "admin", ActorID: admin.UserID,
		BrokerID: brokerID, TargetType: targetType, TargetID: targetID, ClientIP: ip,
		RequestID: observability.RequestIDFrom(r.Context()), TraceID: observability.TraceIDFrom(r.Context()),
		Before: bRaw, After: aRaw, Message: action, Endpoint: r.URL.Path, HTTPMethod: r.Method,
	})
}

func toMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func (s *Server) clientIP(r *http.Request) (string, string) {
	return observability.ClientIP(r, s.trusted)
}

func fmtInt(n int64) string { return strconv.FormatInt(n, 10) }

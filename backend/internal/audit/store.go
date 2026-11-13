package audit

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/fix-gateway/backend/internal/observability"
	"github.com/google/uuid"
)

type Entry struct {
	ActorType       string
	ActorID         string
	BrokerID        *int64
	Action          string
	EntityType      string
	EntityID        string
	IP              string
	ClientOrderID   string
	InternalOrderID string
	AdapterID       string
	RequestPayload  any
	Result          string
	Error           string
	// Optional request context and state change of an admin action.
	RequestID  string
	TraceID    string
	Endpoint   string
	HTTPMethod string
	Before     json.RawMessage
	After      json.RawMessage
}

// Store writes the audit trail into the daily log files (поручение
// пользователя 06.11.2026), as AUDIT events next to the other logs.
type Store struct {
	files *observability.FileLog
}

func NewStore(files *observability.FileLog) *Store {
	return &Store{files: files}
}

func (s *Store) Write(ctx context.Context, e Entry) error {
	if s == nil || s.files == nil {
		return errors.New("audit log unavailable")
	}
	ev := observability.Event{
		Type: observability.TypeAudit, Subtype: e.Action, Severity: observability.SeverityInfo,
		ActorType: e.ActorType, ActorID: e.ActorID, BrokerID: e.BrokerID,
		TargetType: e.EntityType, TargetID: e.EntityID, ClientIP: e.IP,
		ClientOrderID: e.ClientOrderID, AdapterID: e.AdapterID,
		Result: e.Result, ErrorMessage: e.Error, Message: e.Action,
		RequestID: e.RequestID, TraceID: e.TraceID, Endpoint: e.Endpoint, HTTPMethod: e.HTTPMethod,
		Before: e.Before, After: e.After,
	}
	if ev.Result == "" {
		ev.Result = observability.ResultSuccess
	}
	if id, err := uuid.Parse(e.InternalOrderID); err == nil {
		ev.OrderID = &id
	}
	if e.RequestPayload != nil {
		payload, err := json.Marshal(e.RequestPayload)
		if err != nil {
			return err
		}
		ev.Metadata = payload
	}
	return s.files.Append(ev)
}

// RateLimitUpdated is the ratelimit.AuditFunc of this store.
func (s *Store) RateLimitUpdated(ctx context.Context, actor string, brokerID *int64, policyKey string, payload json.RawMessage) error {
	return s.Write(ctx, Entry{
		ActorType: "admin", ActorID: actor, BrokerID: brokerID, Action: "RATE_LIMIT_UPDATED",
		EntityType: "rate_limit", EntityID: policyKey, RequestPayload: payload, Result: "success",
	})
}

// Records returns every audit event still on disk, newest day first.
func (s *Store) Records(ctx context.Context) ([]observability.Event, error) {
	var out []observability.Event
	if s == nil || s.files == nil {
		return out, nil
	}
	days, err := s.files.Events(timeZero, timeZero)
	if err != nil {
		return nil, err
	}
	for _, events := range days {
		for i := len(events) - 1; i >= 0; i-- {
			if events[i].Type == observability.TypeAudit {
				out = append(out, events[i])
			}
		}
	}
	return out, nil
}

// List returns the newest audit records in the shape of the former audit_logs rows.
func (s *Store) List(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	out := []map[string]any{}
	if s == nil || s.files == nil {
		return out, nil
	}
	days, err := s.files.Events(timeZero, timeZero)
	if err != nil {
		return nil, err
	}
	for _, events := range days {
		for i := len(events) - 1; i >= 0 && len(out) < limit; i-- {
			e := events[i]
			if e.Type != observability.TypeAudit {
				continue
			}
			var orderID any
			if e.OrderID != nil {
				orderID = e.OrderID.String()
			}
			out = append(out, map[string]any{
				"id": e.ID, "actor_type": e.ActorType, "actor_id": nullable(e.ActorID), "broker_id": e.BrokerID,
				"action": e.Subtype, "entity_type": e.TargetType, "entity_id": nullable(e.TargetID), "ip": nullable(e.ClientIP),
				"client_order_id": nullable(e.ClientOrderID), "internal_order_id": orderID, "adapter_id": nullable(e.AdapterID),
				"result": nullable(e.Result), "error": nullable(e.ErrorMessage), "created_at": e.Timestamp,
			})
		}
	}
	return out, nil
}

var timeZero time.Time

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

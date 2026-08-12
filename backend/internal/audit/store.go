package audit

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"
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
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Write(ctx context.Context, e Entry) error {
	var payload []byte
	var err error
	if e.RequestPayload != nil {
		payload, err = json.Marshal(e.RequestPayload)
		if err != nil {
			return err
		}
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO audit_logs (
			actor_type, actor_id, broker_id, action, entity_type, entity_id,
			ip, client_order_id, internal_order_id, adapter_id, request_payload, result, error
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		e.ActorType, nullIfEmpty(e.ActorID), e.BrokerID, e.Action, e.EntityType, nullIfEmpty(e.EntityID),
		nullIfEmpty(e.IP), nullIfEmpty(e.ClientOrderID), nullIfEmpty(e.InternalOrderID),
		nullIfEmpty(e.AdapterID), payload, nullIfEmpty(e.Result), nullIfEmpty(e.Error),
	)
	return err
}

func (s *Store) List(ctx context.Context, limit int) ([]map[string]any, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, actor_type, actor_id, broker_id, action, entity_type, entity_id,
		       ip, client_order_id, internal_order_id, adapter_id, result, error, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []map[string]any
	for rows.Next() {
		var (
			id                                                int64
			actorType, action, entityType                     string
			actorID, entityID, ip, coid, ioid, adapterID      *string
			brokerID                                          *int64
			result, errMsg                                    *string
			createdAt                                         any
		)
		if err := rows.Scan(&id, &actorType, &actorID, &brokerID, &action, &entityType, &entityID,
			&ip, &coid, &ioid, &adapterID, &result, &errMsg, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "actor_type": actorType, "actor_id": actorID, "broker_id": brokerID,
			"action": action, "entity_type": entityType, "entity_id": entityID, "ip": ip,
			"client_order_id": coid, "internal_order_id": ioid, "adapter_id": adapterID,
			"result": result, "error": errMsg, "created_at": createdAt,
		})
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

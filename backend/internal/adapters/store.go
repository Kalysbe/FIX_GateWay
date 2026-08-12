package adapters

import (
	"context"
	"errors"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) List(ctx context.Context) ([]domain.Adapter, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, type, host, port, begin_string, sender_comp_id, target_comp_id,
		       heartbeat_interval, reset_on_logon, username_env, password_env, status,
		       priority, weight, max_concurrent_orders, enabled, active_orders, processed_orders,
		       error_count, last_heartbeat_at, started_at, created_at, updated_at
		FROM adapters ORDER BY priority DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Adapter
	for rows.Next() {
		a, err := scanAdapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (domain.Adapter, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, name, type, host, port, begin_string, sender_comp_id, target_comp_id,
		       heartbeat_interval, reset_on_logon, username_env, password_env, status,
		       priority, weight, max_concurrent_orders, enabled, active_orders, processed_orders,
		       error_count, last_heartbeat_at, started_at, created_at, updated_at
		FROM adapters WHERE id=$1`, id)
	a, err := scanAdapter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, errors.New("adapter not found")
	}
	return a, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAdapter(row scannable) (domain.Adapter, error) {
	var a domain.Adapter
	err := row.Scan(
		&a.ID, &a.Name, &a.Type, &a.Host, &a.Port, &a.BeginString, &a.SenderCompID, &a.TargetCompID,
		&a.HeartbeatInterval, &a.ResetOnLogon, &a.UsernameEnv, &a.PasswordEnv, &a.Status,
		&a.Priority, &a.Weight, &a.MaxConcurrentOrders, &a.Enabled, &a.ActiveOrders, &a.ProcessedOrders,
		&a.ErrorCount, &a.LastHeartbeatAt, &a.StartedAt, &a.CreatedAt, &a.UpdatedAt,
	)
	return a, err
}

func (s *Store) UpdateStatus(ctx context.Context, id string, status domain.AdapterStatus, heartbeat bool) error {
	if heartbeat {
		_, err := s.pool.Exec(ctx, `
			UPDATE adapters SET status=$2, last_heartbeat_at=now(), updated_at=now() WHERE id=$1`, id, status)
		return err
	}
	_, err := s.pool.Exec(ctx, `UPDATE adapters SET status=$2, updated_at=now() WHERE id=$1`, id, status)
	return err
}

func (s *Store) UpdateRuntime(ctx context.Context, id string, active, processed, errors int64) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE adapters SET active_orders=$2, processed_orders=$3, error_count=$4, updated_at=now() WHERE id=$1`,
		id, active, processed, errors)
	return err
}

func (s *Store) SetEnabled(ctx context.Context, id string, enabled bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE adapters SET enabled=$2, updated_at=now() WHERE id=$1`, id, enabled)
	return err
}

func (s *Store) UpdateWeight(ctx context.Context, id string, weight, priority *int) error {
	a, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if weight != nil {
		a.Weight = *weight
	}
	if priority != nil {
		a.Priority = *priority
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE adapters SET weight=$2, priority=$3, updated_at=now() WHERE id=$1`, id, a.Weight, a.Priority)
	return err
}

func (s *Store) MarkStarted(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE adapters SET started_at=$2, status='STARTING', updated_at=now() WHERE id=$1`, id, now)
	return err
}

func (s *Store) AddEvent(ctx context.Context, adapterID, eventType, message string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO adapter_events (adapter_id, event_type, message) VALUES ($1,$2,$3)`,
		adapterID, eventType, message)
	return err
}

func (s *Store) Create(ctx context.Context, a domain.Adapter) (domain.Adapter, error) {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO adapters (
			id, name, type, host, port, begin_string, sender_comp_id, target_comp_id,
			heartbeat_interval, reset_on_logon, username_env, password_env, status,
			priority, weight, max_concurrent_orders, enabled
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		a.ID, a.Name, a.Type, a.Host, a.Port, a.BeginString, a.SenderCompID, a.TargetCompID,
		a.HeartbeatInterval, a.ResetOnLogon, a.UsernameEnv, a.PasswordEnv, domain.AdapterUnknown,
		a.Priority, a.Weight, a.MaxConcurrentOrders, a.Enabled,
	)
	if err != nil {
		return a, err
	}
	return s.Get(ctx, a.ID)
}

func (s *Store) CountOnlineEnabled(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM adapters WHERE enabled=true AND status IN ('ONLINE','DEGRADED')`).Scan(&n)
	return n, err
}

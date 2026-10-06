package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	Pool         *pgxpool.Pool
	Admission    *Admission
	DefaultRead  int
	DefaultTrade int
	refreshMu    sync.Mutex
}

func (s *Store) List(ctx context.Context) ([]Policy, error) {
	rows, err := s.Pool.Query(ctx, `SELECT scope,subject,operation,rate,burst,concurrent,enabled FROM rate_limit_policies ORDER BY scope,subject,operation`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Policy{}
	for rows.Next() {
		var p Policy
		if err := rows.Scan(&p.Scope, &p.Subject, &p.Operation, &p.Rate, &p.Burst, &p.Concurrent, &p.Enabled); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Refresh is a background configuration read, never a DB call on admission.
// Legacy broker settings are inherited unless an explicit policy overrides them.
func (s *Store) Refresh(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,rate_limit_rps,rate_limit_orders_per_sec FROM brokers`)
	if err != nil {
		return err
	}
	out := []Policy{}
	if s.DefaultRead > 0 && s.DefaultTrade > 0 {
		for _, v := range []struct {
			op   string
			rate int
		}{{"Trading", s.DefaultTrade}, {"Queries", s.DefaultRead}, {"System", s.DefaultRead}} {
			out = append(out, Policy{Scope: "broker", Subject: "*", Operation: v.op, Rate: v.rate, Burst: v.rate, Concurrent: v.rate, Enabled: true})
		}
	}
	for rows.Next() {
		var id int64
		var reads, trades int
		if err = rows.Scan(&id, &reads, &trades); err != nil {
			rows.Close()
			return err
		}
		if reads < 1 {
			reads = s.DefaultRead
		}
		if trades < 1 {
			trades = s.DefaultTrade
		}
		if reads < 1 || trades < 1 {
			rows.Close()
			return fmt.Errorf("broker %d has invalid legacy limits", id)
		}
		subject := strconv.FormatInt(id, 10)
		for _, v := range []struct {
			op   string
			rate int
		}{{"Queries", reads}, {"Trading", trades}, {"System", reads}} {
			out = append(out, Policy{Scope: "broker", Subject: subject, Operation: v.op, Rate: v.rate, Burst: v.rate, Concurrent: v.rate, Enabled: true})
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query(ctx, `SELECT scope,subject,operation,rate,burst,concurrent,enabled FROM rate_limit_policies`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p Policy
		if err = rows.Scan(&p.Scope, &p.Subject, &p.Operation, &p.Rate, &p.Burst, &p.Concurrent, &p.Enabled); err != nil {
			rows.Close()
			return err
		}
		out = append(out, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.Admission.Replace(out)
	return nil
}
func (s *Store) Run(ctx context.Context, onError func(error)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			check, cancel := context.WithTimeout(ctx, 2*time.Second)
			err := s.Refresh(check)
			cancel()
			if err != nil && onError != nil {
				onError(err)
			}
		}
	}
}

// Upsert and audit commit atomically. Serialized administration avoids losing
// the before-state when two admins update the same policy concurrently.
func (s *Store) Upsert(ctx context.Context, p Policy, actor, reason string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(72914520)`); err != nil {
		return err
	}
	var bid *int64
	if p.Scope == "broker" {
		id, err := strconv.ParseInt(p.Subject, 10, 64)
		if err != nil || id < 1 {
			return fmt.Errorf("invalid broker")
		}
		var exists bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM brokers WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("unknown broker")
		}
		bid = &id
	}
	var text *string
	err = tx.QueryRow(ctx, `SELECT (SELECT row_to_json(p)::text FROM rate_limit_policies p WHERE scope=$1 AND subject=$2 AND operation=$3)`, p.Scope, p.Subject, p.Operation).Scan(&text)
	if err != nil {
		return err
	}
	var before []byte
	if text != nil {
		before = []byte(*text)
	}
	_, err = tx.Exec(ctx, `INSERT INTO rate_limit_policies(scope,subject,operation,rate,burst,concurrent,enabled) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(scope,subject,operation) DO UPDATE SET rate=EXCLUDED.rate,burst=EXCLUDED.burst,concurrent=EXCLUDED.concurrent,enabled=EXCLUDED.enabled,updated_at=now()`, p.Scope, p.Subject, p.Operation, p.Rate, p.Burst, p.Concurrent, p.Enabled)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{"before": json.RawMessage(beforeOrNull(before)), "after": p, "reason": reason})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_logs(actor_type,actor_id,broker_id,action,entity_type,entity_id,request_payload,result) VALUES('admin',$1,$2,'RATE_LIMIT_UPDATED','rate_limit',$3,$4,'success')`, actor, bid, p.Key(), payload)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func beforeOrNull(b []byte) []byte {
	if len(b) == 0 {
		return []byte("null")
	}
	return b
}

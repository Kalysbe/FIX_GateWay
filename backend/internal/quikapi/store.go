package quikapi

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool        *pgxpool.Pool
	defaultBase string

	mu  sync.Mutex
	mem string
}

func NewStore(pool *pgxpool.Pool, defaultBase string) *Store {
	base, err := NormalizeBaseURL(defaultBase)
	if err != nil {
		base = DefaultBaseURL
	}
	return &Store{pool: pool, defaultBase: base}
}

func (s *Store) DefaultBaseURL() string {
	if s == nil {
		return DefaultBaseURL
	}
	return s.defaultBase
}

func (s *Store) GetBaseURL(ctx context.Context) (string, error) {
	if s == nil {
		return DefaultBaseURL, nil
	}
	if s.pool == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.mem != "" {
			return s.mem, nil
		}
		return s.defaultBase, nil
	}
	var value string
	err := s.pool.QueryRow(ctx, `SELECT value FROM gateway_settings WHERE key=$1`, SettingKey).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.defaultBase, nil
	}
	if err != nil {
		return "", err
	}
	base, nerr := NormalizeBaseURL(value)
	if nerr != nil {
		return s.defaultBase, nil
	}
	return base, nil
}

func (s *Store) SetBaseURL(ctx context.Context, raw, updatedBy string) (string, error) {
	base, err := NormalizeBaseURL(raw)
	if err != nil {
		return "", err
	}
	if s == nil {
		return "", ErrNotConfigured
	}
	if s.pool == nil {
		s.mu.Lock()
		s.mem = base
		s.mu.Unlock()
		return base, nil
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO gateway_settings (key, value, updated_at, updated_by)
		VALUES ($1, $2, now(), $3)
		ON CONFLICT (key) DO UPDATE
		SET value = EXCLUDED.value, updated_at = now(), updated_by = EXCLUDED.updated_by`,
		SettingKey, base, strings.TrimSpace(updatedBy),
	)
	if err != nil {
		return "", err
	}
	return base, nil
}

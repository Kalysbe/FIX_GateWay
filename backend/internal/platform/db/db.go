package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return connect(ctx, databaseURL, false)
}

// ConnectReadOnly opens a pool that forces every session into read-only mode.
// Use for external sources (e.g. QUIK export) where writes are forbidden.
func ConnectReadOnly(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return connect(ctx, databaseURL, true)
}

// ReadOnlyStatementTimeout bounds every query on an external read-only source,
// so a slow source cannot hold the small pool indefinitely.
const ReadOnlyStatementTimeout = 10 * time.Second

// OpenReadOnly builds the read-only pool without requiring the source to be up.
// pgxpool dials lazily, so an outage at startup heals by itself; the returned
// ping error is for logging only. Only an unusable URL returns a nil pool.
func OpenReadOnly(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(databaseURL, true)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	ping, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(ping); err != nil {
		return pool, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

func poolConfig(databaseURL string, readOnly bool) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if readOnly {
		cfg.MaxConns = 4
		cfg.MinConns = 0
		cfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprint(ReadOnlyStatementTimeout.Milliseconds())
		cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
			_, err := conn.Exec(ctx, "SET default_transaction_read_only = on")
			return err
		}
	} else {
		cfg.MaxConns = 20
		cfg.MinConns = 2
	}
	cfg.MaxConnLifetime = time.Hour
	return cfg, nil
}

func connect(ctx context.Context, databaseURL string, readOnly bool) (*pgxpool.Pool, error) {
	cfg, err := poolConfig(databaseURL, readOnly)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

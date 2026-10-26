package quikexport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotConfigured = errors.New("quik export database is not configured")
	ErrNotFound      = errors.New("emitent not found")
	ErrBadSymbol     = errors.New("symbol must be exactly 4 characters")
)

const maxOrders = 1000

type Store struct {
	pool         *pgxpool.Pool
	paramClasses func(context.Context) ([]string, error)
}

func NewStore(pool *pgxpool.Pool) *Store {
	if pool == nil {
		return nil
	}
	return &Store{pool: pool}
}

type Emitent struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	NameEng string `json:"name_eng"`
	Code    string `json:"code"`
	Symbol  string `json:"symbol"`
}

type OrdersResult struct {
	Symbol  string            `json:"symbol"`
	Code    string            `json:"code"`
	Emitent Emitent           `json:"emitent"`
	Quotes  []json.RawMessage `json:"quotes"`
	Count   int               `json:"count"`
}

func NormalizeSymbol(raw string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	if utf8.RuneCountInString(s) != 4 {
		return "", ErrBadSymbol
	}
	return s, nil
}

func (s *Store) OrdersBySymbol(ctx context.Context, rawSymbol string) (OrdersResult, error) {
	var out OrdersResult
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	symbol, err := NormalizeSymbol(rawSymbol)
	if err != nil {
		return out, err
	}

	var e Emitent
	err = s.pool.QueryRow(ctx, `
SELECT id,
       COALESCE(name, ''),
       COALESCE(name_eng, ''),
       btrim(code::text),
       btrim(symbol)
FROM emitents
WHERE upper(btrim(symbol)) = $1
ORDER BY id
LIMIT 1`, symbol).Scan(&e.ID, &e.Name, &e.NameEng, &e.Code, &e.Symbol)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, fmt.Errorf("lookup emitent: %w", err)
	}

	rows, err := s.pool.Query(ctx, `
SELECT to_jsonb(q)
FROM public."Quotes"($1) AS q
LIMIT $2`, symbol, maxOrders)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "22023" {
			return out, ErrBadSymbol
		}
		return out, fmt.Errorf(`"Quotes": %w`, err)
	}
	defer rows.Close()

	quotes := make([]json.RawMessage, 0)
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return out, fmt.Errorf("scan quote: %w", err)
		}
		quotes = append(quotes, raw)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}

	out.Symbol = e.Symbol
	out.Code = e.Code
	out.Emitent = e
	out.Quotes = quotes
	out.Count = len(quotes)
	return out, nil
}

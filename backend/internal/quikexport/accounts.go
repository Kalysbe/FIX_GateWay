package quikexport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	ErrAccountNotFound = errors.New("trading account is not present in QUIK Trdaccs for the firm")
	ErrFirmNotFound    = errors.New("firm is not present in QUIK Firms")
)

// Row is a QUIK export row kept verbatim as the database JSON.
type Row struct {
	Raw json.RawMessage
}

func (r Row) MarshalJSON() ([]byte, error) {
	if len(r.Raw) == 0 {
		return []byte("null"), nil
	}
	return r.Raw, nil
}
func (r *Row) UnmarshalJSON(raw []byte) error { r.Raw = append(r.Raw[:0], raw...); return nil }
func (r Row) Text(key string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(r.Raw, &fields) != nil {
		return ""
	}
	return fieldText(fields[key])
}

func (s *Store) rows(ctx context.Context, query string, args ...any) ([]Row, error) {
	out := []Row{}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		out = append(out, Row{Raw: raw})
	}
	return out, rows.Err()
}

func (s *Store) row(ctx context.Context, missing error, query string, args ...any) (Row, error) {
	if s == nil || s.pool == nil {
		return Row{}, ErrNotConfigured
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, query, args...).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Row{}, missing
	}
	if err != nil {
		return Row{}, err
	}
	return Row{Raw: raw}, nil
}

// ListTradingAccounts returns the Trdaccs rows of one firm. An empty firm
// matches nothing.
func (s *Store) ListTradingAccounts(ctx context.Context, firm string) ([]Row, error) {
	return s.rows(ctx, `SELECT row_to_json(t) FROM public."Trdaccs" t
WHERE btrim(t."FirmId"::text)=$1 AND $1<>'' ORDER BY t."TrdAccId"`, strings.TrimSpace(firm))
}

// TradingAccount finds the account a broker may trade on: TrdAccId within
// the firm of the broker's token. Values are compared after trimming only.
func (s *Store) TradingAccount(ctx context.Context, firm, account string) (Row, error) {
	firm, account = strings.TrimSpace(firm), strings.TrimSpace(account)
	if firm == "" || account == "" {
		return Row{}, ErrAccountNotFound
	}
	return s.row(ctx, ErrAccountNotFound, `SELECT row_to_json(t) FROM public."Trdaccs" t
WHERE btrim(t."FirmId"::text)=$1 AND btrim(t."TrdAccId"::text)=$2 LIMIT 1`, firm, account)
}

func (s *Store) ListFirms(ctx context.Context) ([]Row, error) {
	return s.rows(ctx, `SELECT row_to_json(f) FROM public."Firms" f ORDER BY f."FirmId"`)
}

func (s *Store) Firm(ctx context.Context, firm string) (Row, error) {
	firm = strings.TrimSpace(firm)
	if firm == "" {
		return Row{}, ErrFirmNotFound
	}
	return s.row(ctx, ErrFirmNotFound, `SELECT row_to_json(f) FROM public."Firms" f WHERE btrim(f."FirmId"::text)=$1 LIMIT 1`, firm)
}

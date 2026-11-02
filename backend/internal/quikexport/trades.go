package quikexport

import (
	"context"
	"encoding/json"
	"fmt"
)

// MaxTradeRows bounds one read of the QUIK trade history; a larger table is
// reported instead of being silently cut.
const MaxTradeRows = 200000

// Trades reads the whole QUIK trade history quik_export2.public."Trades" as
// database JSON rows (row_to_json, unchanged) over the read-only connection.
// It is the only source of QUIK trades for the trade ledger reconciliation. The
// read stays whole, so an empty table can be told from a class without trades;
// the ledger itself keeps only the served classes (tradeledger.Import).
func (s *Store) Trades(ctx context.Context) ([]json.RawMessage, error) {
	if s == nil || s.pool == nil {
		return nil, ErrNotConfigured
	}
	rows, err := s.pool.Query(ctx, `SELECT row_to_json(t) FROM public."Trades" t LIMIT $1`, MaxTradeRows+1)
	if err != nil {
		return nil, fmt.Errorf(`QUIK "Trades": %w`, err)
	}
	defer rows.Close()
	out := make([]json.RawMessage, 0, 1024)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf(`QUIK "Trades": %w`, err)
		}
		out = append(out, json.RawMessage(raw))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(`QUIK "Trades": %w`, err)
	}
	if len(out) > MaxTradeRows {
		return nil, fmt.Errorf(`QUIK "Trades" exceeds %d rows`, MaxTradeRows)
	}
	return out, nil
}

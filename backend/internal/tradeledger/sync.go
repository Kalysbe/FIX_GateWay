package tradeledger

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// MaxSnapshotRows bounds one QUIK trade snapshot.
const MaxSnapshotRows = 200000

// SourceLabel names the QUIK trade table in trade_sync_runs.
const SourceLabel = `quik_db:public."Trades"`

// Source returns the complete QUIK trade history, one JSON object per row.
type Source interface {
	Trades(ctx context.Context) ([]json.RawMessage, error)
}

// Import keeps each source record even on validation failure. It does not invent
// local orders or update their fills from a historical snapshot.
func (s *Store) Import(ctx context.Context, records []json.RawMessage) (int, int, error) {
	count, failures := 0, 0
	seen := make(map[string]int)
	for _, raw := range records {
		count++
		if count > MaxSnapshotRows {
			return count, failures, fmt.Errorf("QUIK trade snapshot exceeds record limit")
		}
		p, parseErr := ParseQUIK(raw)
		if parseErr == nil {
			key := p.Match + "\x00" + p.Symbol + "\x00" + p.Board
			mask := 1
			if p.Side == "SELL" {
				mask = 2
			}
			seen[key] |= mask
		}
		if err := s.RecordQUIK(ctx, raw); err != nil {
			if ctx.Err() != nil {
				return count, failures, ctx.Err()
			}
			failures++
		}
	}
	if failures > 0 {
		return count, failures, fmt.Errorf("%d source records need reconciliation; see trade_reports", failures)
	}
	if count == 0 {
		return 0, 1, fmt.Errorf("empty QUIK trade snapshot cannot establish coverage")
	}
	unpaired := 0
	for _, mask := range seen {
		if mask != 3 {
			unpaired++
		}
	}
	if unpaired > 0 {
		return count, unpaired, snapshotError(unpaired)
	}
	// A complete-looking set of pairs can still omit whole trades.
	// Compare identities, not just counts: an omitted old pair must not be hidden
	// by a newly added pair. Every previously observed QUIK side remains required.
	rows, err := s.Pool.Query(ctx, `SELECT t.trade_match_id,t.symbol,t.board,s.side
FROM trade_sides s JOIN trades t ON t.id=s.trade_id WHERE s.has_quik_record`)
	if err != nil {
		return count, 0, err
	}
	defer rows.Close()
	missing := 0
	for rows.Next() {
		var match, symbol, board, side string
		if err := rows.Scan(&match, &symbol, &board, &side); err != nil {
			return count, 0, err
		}
		mask := 1
		if side == "SELL" {
			mask = 2
		}
		if seen[match+"\x00"+symbol+"\x00"+board]&mask == 0 {
			missing++
		}
	}
	if err := rows.Err(); err != nil {
		return count, 0, err
	}
	if missing > 0 {
		return count, missing, fmt.Errorf("QUIK snapshot omitted %d previously observed sides; history coverage regressed", missing)
	}
	return count, 0, nil
}

// Sync reads the QUIK trade table directly (read-only) and reconciles it.
func (s *Store) Sync(ctx context.Context, src Source) (int, error) {
	id, err := s.StartRun(ctx, SourceLabel)
	if err != nil {
		return 0, err
	}
	records, failures := 0, 0
	runErr := func() error {
		rows, err := src.Trades(ctx)
		if err != nil {
			return err
		}
		records, failures, err = s.Import(ctx, rows)
		return err
	}()
	// Preserve failure evidence even when the request context expired.
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.FinishRun(finish, id, records, failures, runErr); err != nil {
		return records, err
	}
	return records, runErr
}

package tradeledger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Import keeps each source record even on validation failure. It does not invent
// local orders or update their fills from a historical snapshot.
func (s *Store) Import(ctx context.Context, reader io.Reader) (int, int, error) {
	dec := json.NewDecoder(io.LimitReader(reader, 128<<20))
	token, err := dec.Token()
	if err != nil {
		return 0, 0, err
	}
	if token != json.Delim('[') {
		return 0, 0, fmt.Errorf("QUIK trades must be a JSON array")
	}
	count, failures := 0, 0
	seen := make(map[string]int)
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return count, failures, err
		}
		count++
		if count > 200000 {
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
	if _, err := dec.Token(); err != nil {
		return count, failures, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return count, failures, fmt.Errorf("unexpected trailing QUIK content: %v", err)
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
	// A syntactically valid array of complete pairs can still omit whole trades.
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

func (s *Store) Sync(ctx context.Context, base string) (int, error) {
	id, err := s.StartRun(ctx, base)
	if err != nil {
		return 0, err
	}
	records, failures := 0, 0
	runErr := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/trades", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		client := &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("QUIK trade redirects are not allowed")
		}}
		res, err := client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return fmt.Errorf("QUIK trades HTTP %d", res.StatusCode)
		}
		records, failures, err = s.Import(ctx, res.Body)
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

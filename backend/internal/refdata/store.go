package refdata

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

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

type SyncResult struct {
	InstrumentsUpserted int    `json:"instruments_upserted"`
	InstrumentsRemoved  int    `json:"instruments_removed"`
	InstrumentsInactive int    `json:"instruments_inactive"`
	AdapterID           string `json:"adapter_id,omitempty"`
}

// SyncFromFIX replaces the local instrument book with the FIX snapshot.
func (s *Store) SyncFromFIX(ctx context.Context, items []domain.SecurityRef) (SyncResult, error) {
	var res SyncResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback(ctx)

	type key struct{ board, sym string }
	keep := make(map[key]struct{}, len(items))
	keepIDs := make([]int64, 0, len(items))

	hasBoard := map[string]bool{}
	for _, it := range items {
		sym := fixText(firstNonEmpty(it.SecurityID, it.Symbol))
		board := fixText(it.Board)
		if usableSymbol(sym) && board != "" && !strings.EqualFold(board, "UNKNOWN") {
			hasBoard[sym] = true
		}
	}

	for _, it := range items {
		sym := fixText(firstNonEmpty(it.SecurityID, it.Symbol))
		board := fixText(it.Board)
		if !usableSymbol(sym) {
			continue
		}
		if board == "" {
			board = "UNKNOWN"
		}
		if strings.EqualFold(board, "UNKNOWN") && hasBoard[sym] {
			continue
		}
		currency := fixText(it.Currency)
		if currency == "" {
			currency = "KGS"
		}
		secType := domain.NormalizeSecurityType(fixText(it.SecurityType))
		if secType == "" {
			if strings.Contains(strings.ToUpper(board), "BND") || strings.EqualFold(board, "LIST_GBA") {
				secType = "GO"
			} else {
				secType = "CS"
			}
		}
		internal := board + "/" + sym
		k := key{board: board, sym: sym}
		if _, dup := keep[k]; dup {
			continue
		}
		keep[k] = struct{}{}

		id, err := upsertInstrumentTx(ctx, tx, internal, sym, board, currency, secType)
		if err != nil {
			continue
		}
		keepIDs = append(keepIDs, id)
		res.InstrumentsUpserted++
	}

	if len(keepIDs) == 0 {
		return res, fmt.Errorf("empty FIX security snapshot — refuse to wipe instruments")
	}

	tag, err := tx.Exec(ctx, `
		UPDATE instruments i
		SET status = 'INACTIVE'
		WHERE i.id <> ALL($1)
		  AND i.status <> 'INACTIVE'
		  AND (
		    EXISTS (SELECT 1 FROM orders o WHERE o.instrument_id = i.id)
		    OR EXISTS (SELECT 1 FROM broker_instrument_mapping m WHERE m.instrument_id = i.id)
		  )`, keepIDs)
	if err != nil {
		return res, fmt.Errorf("deactivate stale instruments: %w", err)
	}
	res.InstrumentsInactive = int(tag.RowsAffected())

	tag, err = tx.Exec(ctx, `
		DELETE FROM instruments i
		WHERE i.id <> ALL($1)
		  AND NOT EXISTS (SELECT 1 FROM orders o WHERE o.instrument_id = i.id)
		  AND NOT EXISTS (SELECT 1 FROM broker_instrument_mapping m WHERE m.instrument_id = i.id)`, keepIDs)
	if err != nil {
		return res, fmt.Errorf("delete stale instruments: %w", err)
	}
	res.InstrumentsRemoved = int(tag.RowsAffected())

	if err := tx.Commit(ctx); err != nil {
		return res, err
	}
	return res, nil
}

func upsertInstrumentTx(ctx context.Context, tx pgx.Tx, internal, quikSymbol, board, currency, secType string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		UPDATE instruments
		SET internal_symbol=$1, currency=$4, security_type=$5, status='ACTIVE'
		WHERE quik_symbol=$2 AND board=$3
		RETURNING id`,
		internal, quikSymbol, board, currency, secType,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("update instrument %s/%s: %w", board, quikSymbol, err)
	}

	err = tx.QueryRow(ctx, `
		INSERT INTO instruments (internal_symbol, quik_symbol, board, currency, security_type, status)
		VALUES ($1,$2,$3,$4,$5,'ACTIVE')
		ON CONFLICT (internal_symbol) DO UPDATE SET
			quik_symbol = EXCLUDED.quik_symbol,
			board = EXCLUDED.board,
			currency = EXCLUDED.currency,
			security_type = EXCLUDED.security_type,
			status = 'ACTIVE'
		RETURNING id`,
		internal, quikSymbol, board, currency, secType,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert instrument %s/%s: %w", board, quikSymbol, err)
	}
	return id, nil
}

func (s *Store) ListFirms(ctx context.Context) ([]Firm, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT firm_id, exchange, firm_name, status, status_flag
		FROM quik_firms ORDER BY firm_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Firm
	for rows.Next() {
		var f Firm
		if err := rows.Scan(&f.FirmID, &f.Exchange, &f.FirmName, &f.Status, &f.StatusFlag); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *Store) ListActiveInstruments(ctx context.Context) ([]domain.SecurityRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT quik_symbol, board, currency, security_type
		FROM instruments
		WHERE status='ACTIVE' AND board <> '' AND board <> 'UNKNOWN'
		ORDER BY board, quik_symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SecurityRef
	for rows.Next() {
		var r domain.SecurityRef
		if err := rows.Scan(&r.Symbol, &r.Board, &r.Currency, &r.SecurityType); err != nil {
			return nil, err
		}
		r.SecurityID = r.Symbol
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetFirmByID returns a firm from the local quik_firms cache.
// ok=false if firm_id is empty or not found (not an error).
func (s *Store) GetFirmByID(ctx context.Context, firmID string) (Firm, bool, error) {
	firmID = strings.TrimSpace(firmID)
	if firmID == "" {
		return Firm{}, false, nil
	}
	var f Firm
	err := s.pool.QueryRow(ctx, `
		SELECT firm_id, exchange, firm_name, status, status_flag
		FROM quik_firms WHERE firm_id=$1`, firmID).Scan(
		&f.FirmID, &f.Exchange, &f.FirmName, &f.Status, &f.StatusFlag,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Firm{}, false, nil
	}
	if err != nil {
		return Firm{}, false, err
	}
	return f, true, nil
}

type Firm struct {
	Exchange   string `json:"exchange"`
	FirmID     string `json:"firm_id"`
	FirmName   string `json:"firm_name"`
	Status     string `json:"status"`
	StatusFlag *int32 `json:"status_flag,omitempty"`
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fixText(s string) string {
	s = strings.ToValidUTF8(strings.TrimSpace(s), "")
	s = strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func usableSymbol(s string) bool {
	if utf8.RuneCountInString(s) < 3 {
		return false
	}
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

package marketdata

import (
	"context"
	"encoding/json"
	"strconv"
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

type Snapshot struct {
	ID          int64      `json:"id"`
	AdapterID   string     `json:"adapter_id"`
	MDReqID     string     `json:"md_req_id"`
	RequestedAt time.Time  `json:"requested_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Status      string     `json:"status"`
	Error       string     `json:"error,omitempty"`
	ReportCount int        `json:"report_count"`
}

func (s *Store) BeginSnapshot(ctx context.Context, adapterID, mdReqID string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO fix_md_snapshots (adapter_id, md_req_id, status)
		VALUES ($1,$2,'PENDING') RETURNING id`, adapterID, mdReqID,
	).Scan(&id)
	return id, err
}

func (s *Store) CompleteSnapshot(ctx context.Context, id int64, status, errMsg string, count int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE fix_md_snapshots
		SET status=$2, error=$3, report_count=$4, completed_at=now()
		WHERE id=$1`, id, status, errMsg, count)
	return err
}

func (s *Store) InsertQuotes(ctx context.Context, snapshotID int64, items []domain.MarketQuote) error {
	for _, q := range items {
		entriesJSON, _ := json.Marshal(q.Entries)
		if q.Entries == nil {
			entriesJSON = []byte("{}")
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO fix_md_quotes (
				snapshot_id, adapter_id, md_req_id, symbol, security_id, board, currency, name,
				trade_date, trading_status, trading_session_id, trading_session_sub,
				bid, bid_size, offer, offer_size, last, last_size, last_time,
				open, high, low, close, wapr, settle, current_price, official_price,
				volume, value, entries, raw_fix
			) VALUES (
				$1,$2,$3,$4,$5,$6,$7,$8,
				$9,$10,$11,$12,
				$13,$14,$15,$16,$17,$18,$19,
				$20,$21,$22,$23,$24,$25,$26,$27,
				$28,$29,$30,$31
			)`,
			snapshotID, q.AdapterID, q.MDReqID, q.Symbol, q.SecurityID, q.Board, q.Currency, q.Name,
			q.TradeDate, q.TradingStatus, q.TradingSessionID, q.TradingSessionSub,
			q.Bid, q.BidSize, q.Offer, q.OfferSize, q.Last, q.LastSize, q.LastTime,
			q.Open, q.High, q.Low, q.Close, q.WAPR, q.Settle, q.CurrentPrice, q.OfficialPrice,
			q.Volume, q.Value, entriesJSON, q.RawFIX,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListFromLatest(ctx context.Context, board, symbol string) (Snapshot, []domain.MarketQuote, error) {
	var sn Snapshot
	err := s.pool.QueryRow(ctx, `
		SELECT id, adapter_id, md_req_id, requested_at, completed_at, status, error, report_count
		FROM fix_md_snapshots
		WHERE status='OK'
		ORDER BY requested_at DESC LIMIT 1`).Scan(
		&sn.ID, &sn.AdapterID, &sn.MDReqID, &sn.RequestedAt, &sn.CompletedAt,
		&sn.Status, &sn.Error, &sn.ReportCount,
	)
	if err != nil {
		return sn, nil, err
	}

	q := `
		SELECT adapter_id, md_req_id, symbol, security_id, board, currency, name,
		       trade_date, trading_status, trading_session_id, trading_session_sub,
		       bid, bid_size, offer, offer_size, last, last_size, last_time,
		       open, high, low, close, wapr, settle, current_price, official_price,
		       volume, value, entries
		FROM fix_md_quotes WHERE snapshot_id=$1`
	args := []any{sn.ID}
	n := 1
	if board != "" {
		n++
		args = append(args, board)
		q += ` AND board=$` + strconv.Itoa(n)
	}
	if symbol != "" {
		n++
		args = append(args, symbol)
		q += ` AND (symbol=$` + strconv.Itoa(n) + ` OR security_id=$` + strconv.Itoa(n) + `)`
	}
	q += ` ORDER BY board, symbol`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return sn, nil, err
	}
	defer rows.Close()
	var out []domain.MarketQuote
	for rows.Next() {
		var item domain.MarketQuote
		var entriesRaw []byte
		if err := rows.Scan(
			&item.AdapterID, &item.MDReqID, &item.Symbol, &item.SecurityID, &item.Board, &item.Currency, &item.Name,
			&item.TradeDate, &item.TradingStatus, &item.TradingSessionID, &item.TradingSessionSub,
			&item.Bid, &item.BidSize, &item.Offer, &item.OfferSize, &item.Last, &item.LastSize, &item.LastTime,
			&item.Open, &item.High, &item.Low, &item.Close, &item.WAPR, &item.Settle, &item.CurrentPrice, &item.OfficialPrice,
			&item.Volume, &item.Value, &entriesRaw,
		); err != nil {
			return sn, nil, err
		}
		_ = json.Unmarshal(entriesRaw, &item.Entries)
		out = append(out, item)
	}
	return sn, out, rows.Err()
}

func IsNoSnapshot(err error) bool {
	return err == pgx.ErrNoRows
}

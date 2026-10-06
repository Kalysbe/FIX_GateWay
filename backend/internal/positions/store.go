package positions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

type Snapshot struct {
	ID           int64      `json:"id"`
	AdapterID    string     `json:"adapter_id"`
	PosReqID     string     `json:"pos_req_id"`
	ClearingDate string     `json:"clearing_date"`
	RequestedAt  time.Time  `json:"requested_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	Status       string     `json:"status"`
	Error        string     `json:"error,omitempty"`
	ReportCount  int        `json:"report_count"`
}

func (s *Store) BeginSnapshot(ctx context.Context, adapterID, posReqID, clearingDate string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO fix_position_snapshots (adapter_id, pos_req_id, clearing_date, status)
		VALUES ($1,$2,$3,'PENDING') RETURNING id`, adapterID, posReqID, clearingDate,
	).Scan(&id)
	return id, err
}

func (s *Store) CompleteSnapshot(ctx context.Context, id int64, status, errMsg string, count int) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE fix_position_snapshots
		SET status=$2, error=$3, report_count=$4, completed_at=now()
		WHERE id=$1`, id, status, errMsg, count)
	return err
}

func (s *Store) InsertPositions(ctx context.Context, snapshotID int64, items []domain.FIXPosition) error {
	for _, p := range items {
		keysJSON, _ := json.Marshal(p.Keys)
		itemsJSON, _ := json.Marshal(p.Items)
		if p.Keys == nil {
			keysJSON = []byte("{}")
		}
		if p.Items == nil {
			itemsJSON = []byte("{}")
		}
		_, err := s.pool.Exec(ctx, `
			INSERT INTO fix_positions (
				snapshot_id, adapter_id, pos_maint_rpt_id, pos_req_id, clearing_date, kind,
				firm_id, client_code, account_code, symbol, security_id, board, currency,
				pos_type, update_action, long_qty, short_qty, net_qty, pos_amt_type, pos_amt,
				settl_price, keys, items, raw_fix
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				$7,$8,$9,$10,$11,$12,$13,
				$14,$15,$16,$17,$18,$19,$20,
				$21,$22,$23,$24
			)`,
			snapshotID, p.AdapterID, p.PosMaintRptID, p.PosReqID, p.ClearingDate, p.Kind,
			p.FirmID, p.ClientCode, p.AccountCode, p.Symbol, p.SecurityID, p.Board, p.Currency,
			p.PosType, p.UpdateAction, p.LongQty, p.ShortQty, p.NetQty, p.PosAmtType, p.PosAmt,
			p.SettlPrice, keysJSON, itemsJSON, p.RawFIX,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// NormalizeFirmIDs de-duplicates firm identifiers used to filter a snapshot
// (broker.code and/or token quik_firm). Empty values are dropped.
func NormalizeFirmIDs(ids ...string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		key := strings.ToLower(id)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *Store) ListFromLatest(ctx context.Context, firmID string, kind string) (Snapshot, []domain.FIXPosition, error) {
	return s.ListFromLatestByFirms(ctx, NormalizeFirmIDs(firmID), kind)
}

func (s *Store) ListFromLatestByFirms(ctx context.Context, firms []string, kind string) (Snapshot, []domain.FIXPosition, error) {
	var sn Snapshot
	err := s.pool.QueryRow(ctx, `
		SELECT id, adapter_id, pos_req_id, clearing_date, requested_at, completed_at, status, error, report_count
		FROM fix_position_snapshots
		WHERE status='OK'
		ORDER BY requested_at DESC LIMIT 1`).Scan(
		&sn.ID, &sn.AdapterID, &sn.PosReqID, &sn.ClearingDate, &sn.RequestedAt, &sn.CompletedAt,
		&sn.Status, &sn.Error, &sn.ReportCount,
	)
	if err != nil {
		return sn, nil, err
	}

	q := `
		SELECT adapter_id, pos_maint_rpt_id, pos_req_id, clearing_date, kind,
		       firm_id, client_code, account_code, symbol, security_id, board, currency,
		       pos_type, update_action, long_qty, short_qty, net_qty, pos_amt_type, pos_amt, settl_price,
		       keys, items
		FROM fix_positions WHERE snapshot_id=$1`
	args := []any{sn.ID}
	n := 1
	if firms := NormalizeFirmIDs(firms...); len(firms) > 0 {
		n++
		lowered := make([]string, len(firms))
		for i, f := range firms {
			lowered[i] = strings.ToLower(f)
		}
		args = append(args, lowered)
		// Broker code (e.g. 2040) and token quik_firm both map to PartyRole 13 / FIRMID.
		q += fmt.Sprintf(` AND (
			lower(trim(firm_id)) = ANY($%d::text[])
			OR lower(trim(COALESCE(keys->>'FIRMID', ''))) = ANY($%d::text[])
		)`, n, n)
	}
	if kind != "" {
		n++
		args = append(args, kind)
		q += fmt.Sprintf(` AND kind=$%d`, n)
	}
	q += ` ORDER BY kind, firm_id, client_code, symbol, pos_type`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return sn, nil, err
	}
	defer rows.Close()
	var out []domain.FIXPosition
	for rows.Next() {
		var p domain.FIXPosition
		var longQ, shortQ, netQ, amt, px decimal.Decimal
		var keysRaw, itemsRaw []byte
		if err := rows.Scan(
			&p.AdapterID, &p.PosMaintRptID, &p.PosReqID, &p.ClearingDate, &p.Kind,
			&p.FirmID, &p.ClientCode, &p.AccountCode, &p.Symbol, &p.SecurityID, &p.Board, &p.Currency,
			&p.PosType, &p.UpdateAction, &longQ, &shortQ, &netQ, &p.PosAmtType, &amt, &px,
			&keysRaw, &itemsRaw,
		); err != nil {
			return sn, nil, err
		}
		p.LongQty, p.ShortQty, p.NetQty, p.PosAmt, p.SettlPrice = longQ, shortQ, netQ, amt, px
		_ = json.Unmarshal(keysRaw, &p.Keys)
		_ = json.Unmarshal(itemsRaw, &p.Items)
		p.Source = "fix"
		out = append(out, p)
	}
	return sn, out, rows.Err()
}

func IsNoSnapshot(err error) bool {
	return err == pgx.ErrNoRows
}

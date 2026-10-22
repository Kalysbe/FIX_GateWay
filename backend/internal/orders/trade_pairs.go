package orders

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ARQA encodes the match and side: LIST_GBA-2sw-B-1-1-N / LIST_GBA-2sw-S-1-1-N.
// Anchor the suffix so a B/S segment in the market prefix is not replaced.
var tradeSideExecID = regexp.MustCompile(`^(.+)-([BS])(-[0-9]+-[0-9]+-[^-]+)$`)

func oppositeTradeExecID(t domain.Trade) string {
	parts := tradeSideExecID.FindStringSubmatch(strings.TrimSpace(t.ExecID))
	if parts == nil {
		return ""
	}
	if parts[2] == "B" && t.Side == string(domain.SideBuy) {
		return parts[1] + "-S" + parts[3]
	}
	if parts[2] == "S" && t.Side == string(domain.SideSell) {
		return parts[1] + "-B" + parts[3]
	}
	return ""
}

func scanTrade(row pgx.Row) (domain.Trade, error) {
	var t domain.Trade
	err := row.Scan(
		&t.ID, &t.OrderID, &t.ExecID, &t.LastQty, &t.LastPx, &t.CumQty, &t.LeavesQty, &t.AdapterID, &t.CreatedAt,
		&t.BrokerID, &t.ClientOrderID, &t.Symbol, &t.Board, &t.Side,
		&t.ClientCode, &t.QuikFirm, &t.FirmName, &t.FixClOrdID, &t.QuikOrderID, &t.ExecutionDate,
		&t.TradeMatchID, &t.ContraBroker, &t.ContraTrader,
	)
	return t, err
}

// ListAdminTrades completes the latest execution window with recorded counterpart
// legs. Only the admin list can cross broker boundaries; broker lists stay scoped.
func (s *Store) ListAdminTrades(ctx context.Context, limit int) ([]domain.Trade, error) {
	trades, err := s.ListTrades(ctx, nil, limit)
	if err != nil {
		return nil, err
	}
	return s.completeTradePairs(ctx, trades)
}

// GetAdminTrade loads a match directly, independently of the recent list window.
func (s *Store) GetAdminTrade(ctx context.Context, id uuid.UUID) ([]domain.Trade, error) {
	trade, err := scanTrade(s.pool.QueryRow(ctx, tradeSelect+` WHERE e.id=$1`, id))
	if err == pgx.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.completeTradePairs(ctx, []domain.Trade{trade})
}

func (s *Store) completeTradePairs(ctx context.Context, trades []domain.Trade) ([]domain.Trade, error) {
	type match struct{ matchID, execID, date, symbol, board, side string }
	requested := make(map[match]bool)
	seen := make(map[uuid.UUID]bool, len(trades))
	var matchIDs, execIDs, dates, symbols, boards, sides []string
	for _, t := range trades {
		seen[t.ID] = true
		execID := oppositeTradeExecID(t)
		if t.TradeMatchID == "" && execID == "" {
			continue
		}
		side := string(domain.SideBuy)
		if t.Side == string(domain.SideBuy) {
			side = string(domain.SideSell)
		}
		key := match{t.TradeMatchID, execID, t.ExecutionDate, t.Symbol, t.Board, side}
		if requested[key] {
			continue
		}
		requested[key] = true
		matchIDs = append(matchIDs, t.TradeMatchID)
		execIDs = append(execIDs, execID)
		dates = append(dates, t.ExecutionDate)
		symbols = append(symbols, t.Symbol)
		boards = append(boards, t.Board)
		sides = append(sides, side)
	}
	if len(execIDs) == 0 {
		return trades, nil
	}
	rows, err := s.pool.Query(ctx, tradeSelect+`
WHERE (e.trade_match_id = ANY($6::text[]) AND e.trade_match_id<>'' OR e.exec_id = ANY($1::text[])) AND EXISTS (
    SELECT 1 FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[], $6::text[])
        AS p(exec_id, execution_date, symbol, board, side, match_id)
    WHERE (p.match_id<>'' AND p.match_id=e.trade_match_id
           OR p.match_id='' AND e.trade_match_id='' AND p.exec_id=e.exec_id)
      AND p.execution_date = COALESCE(e.execution_date, (e.created_at AT TIME ZONE 'UTC')::date)::text
      AND p.symbol = i.quik_symbol
      AND p.board = COALESCE(NULLIF(o.board, ''), i.board)
      AND p.side = o.side
)`, execIDs, dates, symbols, boards, sides, matchIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanTrade(rows)
		if err != nil {
			return nil, err
		}
		if !seen[t.ID] {
			trades = append(trades, t)
			seen[t.ID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(trades, func(i, j int) bool {
		if trades[i].CreatedAt.Equal(trades[j].CreatedAt) {
			return trades[i].ID.String() > trades[j].ID.String()
		}
		return trades[i].CreatedAt.After(trades[j].CreatedAt)
	})
	return trades, nil
}

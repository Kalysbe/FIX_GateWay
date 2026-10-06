package positions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

const (
	SourceFIX    = "fix"
	SourceManual = "manual"
)

var (
	ErrInvalid  = errors.New("некорректный запрос")
	ErrNotFound = errors.New("позиция не найдена")
)

type invalidError string

func (e invalidError) Error() string { return string(e) }
func (e invalidError) Unwrap() error { return ErrInvalid }

func invalid(msg string) error { return invalidError(msg) }

// CashSet is a request to set a money (ML) position.
type CashSet struct {
	FirmID      string
	ClientCode  string
	AccountCode string
	Currency    string
	Tag         string
	Amount      decimal.Decimal
	OpenBal     *decimal.Decimal
	Limit       *decimal.Decimal
	Locked      *decimal.Decimal
	UpdatedBy   string
}

// InstrumentSet is a request to set a securities (DL) position.
type InstrumentSet struct {
	FirmID      string
	ClientCode  string
	AccountCode string
	Symbol      string
	SecurityID  string
	Board       string
	Currency    string
	Quantity    *decimal.Decimal
	LongQty     *decimal.Decimal
	ShortQty    *decimal.Decimal
	SettlPrice  *decimal.Decimal
	UpdatedBy   string
}

func BuildCash(in CashSet) (domain.FIXPosition, error) {
	in.FirmID = strings.TrimSpace(in.FirmID)
	in.ClientCode = strings.TrimSpace(in.ClientCode)
	in.AccountCode = strings.TrimSpace(in.AccountCode)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.Tag = strings.TrimSpace(in.Tag)
	if in.FirmID == "" {
		return domain.FIXPosition{}, invalid("firm_id обязателен")
	}
	if in.ClientCode == "" {
		return domain.FIXPosition{}, invalid("client_code обязателен")
	}
	if in.Currency == "" {
		return domain.FIXPosition{}, invalid("currency обязателен")
	}
	if in.Tag == "" {
		in.Tag = "EQTV"
	}
	open := in.Amount
	if in.OpenBal != nil {
		open = *in.OpenBal
	}
	lim := decimal.Zero
	if in.Limit != nil {
		lim = *in.Limit
	}
	locked := decimal.Zero
	if in.Locked != nil {
		locked = *in.Locked
	}
	keys := map[string]string{
		"FIRMID": in.FirmID,
		"CLIENT": in.ClientCode,
		"CUR":    in.Currency,
		"TAG":    in.Tag,
		"KIND":   "T0",
	}
	if in.AccountCode != "" {
		keys["ACC"] = in.AccountCode
	}
	items := map[string]string{
		"CUR_BAL":  in.Amount.String(),
		"OPEN_BAL": open.String(),
		"CUR_LIM":  lim.String(),
		"LOCKED":   locked.String(),
	}
	return domain.FIXPosition{
		Kind:        "CASH",
		PosType:     "ML",
		FirmID:      in.FirmID,
		ClientCode:  in.ClientCode,
		AccountCode: in.AccountCode,
		Symbol:      in.Tag,
		Currency:    in.Currency,
		NetQty:      in.Amount,
		PosAmt:      in.Amount,
		PosAmtType:  "CASH",
		Keys:        keys,
		Items:       items,
		Source:      SourceManual,
	}, nil
}

func BuildInstrument(in InstrumentSet) (domain.FIXPosition, error) {
	in.FirmID = strings.TrimSpace(in.FirmID)
	in.ClientCode = strings.TrimSpace(in.ClientCode)
	in.AccountCode = strings.TrimSpace(in.AccountCode)
	in.Symbol = strings.TrimSpace(in.Symbol)
	in.SecurityID = strings.TrimSpace(in.SecurityID)
	in.Board = strings.TrimSpace(in.Board)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.FirmID == "" {
		return domain.FIXPosition{}, invalid("firm_id обязателен")
	}
	if in.ClientCode == "" {
		return domain.FIXPosition{}, invalid("client_code обязателен")
	}
	if in.Symbol == "" {
		return domain.FIXPosition{}, invalid("symbol обязателен")
	}
	if in.SecurityID == "" {
		in.SecurityID = in.Symbol
	}
	net, longQ, shortQ, err := instrumentQty(in.Quantity, in.LongQty, in.ShortQty)
	if err != nil {
		return domain.FIXPosition{}, err
	}
	px := decimal.Zero
	if in.SettlPrice != nil {
		px = *in.SettlPrice
	}
	keys := map[string]string{
		"FIRMID": in.FirmID,
		"CLIENT": in.ClientCode,
		"SEC":    in.SecurityID,
		"KIND":   "T0",
	}
	if in.Board != "" {
		keys["BOARD"] = in.Board
	}
	if in.AccountCode != "" {
		keys["ACC"] = in.AccountCode
	}
	if in.Currency != "" {
		keys["CUR"] = in.Currency
	}
	// §2.33 DL items: OPEN_BAL / CUR_BAL / AVG_POS_PRICE. Qty is current balance.
	items := map[string]string{
		"OPEN_BAL": net.String(),
		"CUR_BAL":  net.String(),
	}
	if !px.IsZero() {
		items["AVG_POS_PRICE"] = px.String()
	}
	return domain.FIXPosition{
		Kind:        "INSTRUMENT",
		PosType:     "DL",
		FirmID:      in.FirmID,
		ClientCode:  in.ClientCode,
		AccountCode: in.AccountCode,
		Symbol:      in.Symbol,
		SecurityID:  in.SecurityID,
		Board:       in.Board,
		Currency:    in.Currency,
		LongQty:     longQ,
		ShortQty:    shortQ,
		NetQty:      net,
		SettlPrice:  px,
		Keys:        keys,
		Items:       items,
		Source:      SourceManual,
	}, nil
}

func instrumentQty(qty, longQ, shortQ *decimal.Decimal) (net, long, short decimal.Decimal, err error) {
	if qty == nil && longQ == nil && shortQ == nil {
		return decimal.Zero, decimal.Zero, decimal.Zero, invalid("укажите quantity или long_qty/short_qty")
	}
	if longQ != nil {
		long = *longQ
	}
	if shortQ != nil {
		short = *shortQ
	}
	if long.IsNegative() || short.IsNegative() {
		return decimal.Zero, decimal.Zero, decimal.Zero, invalid("long_qty и short_qty должны быть >= 0")
	}
	if qty != nil {
		net = *qty
		if longQ == nil && shortQ == nil {
			if net.GreaterThanOrEqual(decimal.Zero) {
				long = net
			} else {
				short = net.Abs()
			}
		} else if !net.Equal(long.Sub(short)) {
			return decimal.Zero, decimal.Zero, decimal.Zero, invalid("quantity должен равняться long_qty - short_qty")
		}
		return net, long, short, nil
	}
	return long.Sub(short), long, short, nil
}

func BlocksFromSets(cash []CashSet, instr []InstrumentSet, updateAction string) ([]domain.PositionBlock, error) {
	if updateAction == "" {
		updateAction = "1"
	}
	var blocks []domain.PositionBlock
	for _, in := range cash {
		p, err := BuildCash(in)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, domain.PositionBlock{
			PosType: "ML", UpdateAction: updateAction,
			Keys: p.Keys, Items: p.Items, LongQty: p.LongQty, ShortQty: p.ShortQty,
		})
	}
	for _, in := range instr {
		p, err := BuildInstrument(in)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, domain.PositionBlock{
			PosType: "DL", UpdateAction: updateAction,
			Keys: p.Keys, Items: p.Items, LongQty: p.LongQty, ShortQty: p.ShortQty,
		})
	}
	if len(blocks) == 0 {
		return nil, invalid("укажите cash и/или instruments")
	}
	return blocks, nil
}

type FIXMaintainer interface {
	MaintainPositions(ctx context.Context, req domain.PositionMaintainRequest) (adapterID, reqID string, err error)
}

// SetViaFIX sends Position Maintenance Request (35=AL) to QUIK and refreshes the snapshot via AN/AP.
func (s *Store) SetViaFIX(ctx context.Context, maint FIXMaintainer, fetcher FIXFetcher, req domain.PositionMaintainRequest) (adapterID, reqID string, sn Snapshot, items []domain.FIXPosition, err error) {
	if strings.TrimSpace(req.Firm) == "" {
		return "", "", sn, nil, invalid("firm_id обязателен")
	}
	if strings.TrimSpace(req.ClientCode) == "" {
		return "", "", sn, nil, invalid("client_code обязателен")
	}
	if len(req.Blocks) == 0 {
		return "", "", sn, nil, invalid("укажите cash и/или instruments")
	}
	adapterID, reqID, err = maint.MaintainPositions(ctx, req)
	if err != nil {
		return adapterID, reqID, sn, nil, err
	}
	alAdapter := adapterID
	if fetcher != nil {
		q := domain.PositionQuery{
			Firm:                 req.Firm,
			ClientCode:           req.ClientCode,
			Account:              req.Account,
			ClearingBusinessDate: req.ClearingBusinessDate,
		}
		if _, _, _, sn, syncErr := s.SyncFromFIX(ctx, fetcher, q); syncErr != nil {
			// Maintenance already accepted in QUIK; snapshot refresh is best-effort.
			sn, items, _ = s.ListFromLatest(ctx, req.Firm, "")
			return alAdapter, reqID, sn, items, nil
		}
	}
	sn, items, _ = s.ListFromLatest(ctx, req.Firm, "")
	return alAdapter, reqID, sn, items, nil
}

func (s *Store) SetCash(ctx context.Context, in CashSet) (domain.FIXPosition, error) {
	p, err := BuildCash(in)
	if err != nil {
		return p, err
	}
	return s.upsertOverride(ctx, s.pool, p, in.UpdatedBy)
}

func (s *Store) SetInstrument(ctx context.Context, in InstrumentSet) (domain.FIXPosition, error) {
	p, err := BuildInstrument(in)
	if err != nil {
		return p, err
	}
	return s.upsertOverride(ctx, s.pool, p, in.UpdatedBy)
}

func (s *Store) SetMany(ctx context.Context, cash []CashSet, instr []InstrumentSet) (cashOut, instrOut []domain.FIXPosition, err error) {
	builtCash := make([]domain.FIXPosition, 0, len(cash))
	for _, in := range cash {
		p, err := BuildCash(in)
		if err != nil {
			return nil, nil, err
		}
		builtCash = append(builtCash, p)
	}
	builtInstr := make([]domain.FIXPosition, 0, len(instr))
	for _, in := range instr {
		p, err := BuildInstrument(in)
		if err != nil {
			return nil, nil, err
		}
		builtInstr = append(builtInstr, p)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for i, p := range builtCash {
		p, err = s.upsertOverride(ctx, tx, p, cash[i].UpdatedBy)
		if err != nil {
			return nil, nil, err
		}
		cashOut = append(cashOut, p)
	}
	for i, p := range builtInstr {
		p, err = s.upsertOverride(ctx, tx, p, instr[i].UpdatedBy)
		if err != nil {
			return nil, nil, err
		}
		instrOut = append(instrOut, p)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	if cashOut == nil {
		cashOut = []domain.FIXPosition{}
	}
	if instrOut == nil {
		instrOut = []domain.FIXPosition{}
	}
	return cashOut, instrOut, nil
}

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) upsertOverride(ctx context.Context, q rowQuerier, p domain.FIXPosition, updatedBy string) (domain.FIXPosition, error) {
	keysJSON, itemsJSON := marshalMaps(p.Keys, p.Items)
	var id int64
	err := q.QueryRow(ctx, `
		INSERT INTO position_overrides (
			kind, firm_id, client_code, account_code, symbol, security_id, board, currency,
			long_qty, short_qty, net_qty, pos_amt, settl_price, keys, items, updated_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		ON CONFLICT (kind, (lower(trim(firm_id))), (lower(trim(client_code))), (lower(trim(symbol))), (lower(trim(board))), (lower(trim(currency))))
		DO UPDATE SET
			account_code = EXCLUDED.account_code,
			security_id = EXCLUDED.security_id,
			long_qty = EXCLUDED.long_qty,
			short_qty = EXCLUDED.short_qty,
			net_qty = EXCLUDED.net_qty,
			pos_amt = EXCLUDED.pos_amt,
			settl_price = EXCLUDED.settl_price,
			keys = EXCLUDED.keys,
			items = EXCLUDED.items,
			updated_by = EXCLUDED.updated_by,
			updated_at = now()
		RETURNING id`,
		p.Kind, p.FirmID, p.ClientCode, p.AccountCode, p.Symbol, p.SecurityID, p.Board, p.Currency,
		p.LongQty, p.ShortQty, p.NetQty, p.PosAmt, p.SettlPrice, keysJSON, itemsJSON, strings.TrimSpace(updatedBy),
	).Scan(&id)
	if err != nil {
		return p, err
	}
	p.Source = SourceManual
	return p, nil
}

func (s *Store) DeleteOverride(ctx context.Context, kind, firmID, clientCode, symbol, board, currency string) error {
	kind = strings.ToUpper(strings.TrimSpace(kind))
	firmID = strings.TrimSpace(firmID)
	clientCode = strings.TrimSpace(clientCode)
	symbol = strings.TrimSpace(symbol)
	board = strings.TrimSpace(board)
	currency = strings.TrimSpace(currency)
	if kind == "" || firmID == "" || clientCode == "" {
		return invalid("kind, firm_id и client_code обязательны")
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM position_overrides
		WHERE kind=$1
		  AND lower(trim(firm_id)) = lower(trim($2))
		  AND lower(trim(client_code)) = lower(trim($3))
		  AND lower(trim(symbol)) = lower(trim($4))
		  AND lower(trim(board)) = lower(trim($5))
		  AND lower(trim(currency)) = lower(trim($6))`,
		kind, firmID, clientCode, symbol, board, currency,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListOverrides(ctx context.Context, firms []string, kind string) ([]domain.FIXPosition, error) {
	q := `
		SELECT kind, firm_id, client_code, account_code, symbol, security_id, board, currency,
		       long_qty, short_qty, net_qty, pos_amt, settl_price, keys, items
		FROM position_overrides WHERE 1=1`
	args := []any{}
	n := 0
	if firms := NormalizeFirmIDs(firms...); len(firms) > 0 {
		n++
		lowered := make([]string, len(firms))
		for i, f := range firms {
			lowered[i] = strings.ToLower(f)
		}
		args = append(args, lowered)
		q += fmt.Sprintf(` AND lower(trim(firm_id)) = ANY($%d::text[])`, n)
	}
	if kind != "" {
		n++
		args = append(args, kind)
		q += fmt.Sprintf(` AND kind=$%d`, n)
	}
	q += ` ORDER BY kind, firm_id, client_code, symbol`

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.FIXPosition
	for rows.Next() {
		var p domain.FIXPosition
		var longQ, shortQ, netQ, amt, px decimal.Decimal
		var keysRaw, itemsRaw []byte
		if err := rows.Scan(
			&p.Kind, &p.FirmID, &p.ClientCode, &p.AccountCode, &p.Symbol, &p.SecurityID, &p.Board, &p.Currency,
			&longQ, &shortQ, &netQ, &amt, &px, &keysRaw, &itemsRaw,
		); err != nil {
			return nil, err
		}
		p.LongQty, p.ShortQty, p.NetQty, p.PosAmt, p.SettlPrice = longQ, shortQ, netQ, amt, px
		_ = json.Unmarshal(keysRaw, &p.Keys)
		_ = json.Unmarshal(itemsRaw, &p.Items)
		if p.Kind == "CASH" {
			p.PosType = "ML"
			p.PosAmtType = "CASH"
		} else {
			p.PosType = "DL"
		}
		p.Source = SourceManual
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListMergedByFirms returns the latest FIX snapshot (QUIK is the source of truth).
// Missing snapshot is not an error: sn.ID == 0 and items is empty.
func (s *Store) ListMergedByFirms(ctx context.Context, firms []string, kind string) (Snapshot, []domain.FIXPosition, error) {
	sn, items, err := s.ListFromLatestByFirms(ctx, firms, kind)
	if err != nil {
		if !IsNoSnapshot(err) && !errors.Is(err, pgx.ErrNoRows) {
			return sn, nil, err
		}
		return Snapshot{}, []domain.FIXPosition{}, nil
	}
	if items == nil {
		items = []domain.FIXPosition{}
	}
	return sn, items, nil
}

func MergePositions(fix, overrides []domain.FIXPosition) []domain.FIXPosition {
	if len(overrides) == 0 {
		if fix == nil {
			return []domain.FIXPosition{}
		}
		return fix
	}
	used := make([]bool, len(overrides))
	out := make([]domain.FIXPosition, 0, len(fix)+len(overrides))
	for _, p := range fix {
		replaced := false
		for i, o := range overrides {
			if used[i] {
				continue
			}
			if matchPosition(p, o) {
				out = append(out, o)
				used[i] = true
				replaced = true
				break
			}
		}
		if !replaced {
			out = append(out, p)
		}
	}
	for i, o := range overrides {
		if !used[i] {
			out = append(out, o)
		}
	}
	return out
}

func matchPosition(a, b domain.FIXPosition) bool {
	if !eqFold(a.Kind, b.Kind) || !eqFold(a.FirmID, b.FirmID) || !eqFold(a.ClientCode, b.ClientCode) {
		return false
	}
	if strings.EqualFold(a.Kind, "CASH") {
		if !eqFold(a.Currency, b.Currency) && a.Currency != "" && b.Currency != "" {
			return false
		}
		ta, tb := cashTag(a), cashTag(b)
		if ta != "" && tb != "" {
			return eqFold(ta, tb)
		}
		return true
	}
	return eqFold(a.Symbol, b.Symbol) && eqFold(a.Board, b.Board)
}

func cashTag(p domain.FIXPosition) string {
	if t := strings.TrimSpace(p.Symbol); t != "" {
		return t
	}
	if p.Keys != nil {
		if t := strings.TrimSpace(p.Keys["TAG"]); t != "" {
			return t
		}
	}
	return ""
}

func eqFold(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func marshalMaps(keys, items map[string]string) ([]byte, []byte) {
	if keys == nil {
		keys = map[string]string{}
	}
	if items == nil {
		items = map[string]string{}
	}
	k, _ := json.Marshal(keys)
	i, _ := json.Marshal(items)
	return k, i
}

package quikexport

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrOrderNotFound = errors.New("order is not present in QUIK Orders")
var ErrOrderKey = errors.New("invalid QUIK order key")

// Order retains the database JSON verbatim. Helpers are only for routing and
// authorization; no derived fields are added to the serialized row.
type Order struct {
	Raw json.RawMessage
}

func (o Order) MarshalJSON() ([]byte, error) {
	if len(o.Raw) == 0 {
		return []byte("null"), nil
	}
	return o.Raw, nil
}
func (o *Order) UnmarshalJSON(raw []byte) error { o.Raw = append(o.Raw[:0], raw...); return nil }
func (o Order) Text(key string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(o.Raw, &fields) != nil {
		return ""
	}
	return fieldText(fields[key])
}

func fieldText(value json.RawMessage) string {
	if len(value) == 0 || string(value) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(value, &text) == nil {
		return text
	}
	return string(value)
}

// Key decodes the row once; the change feed calls it for every row each poll.
func (o Order) Key() string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(o.Raw, &fields) != nil {
		return "\x00\x00"
	}
	return fieldText(fields["TradeDate"]) + "\x00" + fieldText(fields["ClassCode"]) + "\x00" + fieldText(fields["OrderNum"])
}
func (o Order) VenueID() string {
	d := o.Text("TradeDate")
	if len(d) < 10 {
		return ""
	}
	parsed, err := time.Parse("2006-01-02", d[:10])
	if err != nil {
		return ""
	}
	return parsed.Format("060102") + "-" + o.Text("ClassCode") + "-" + o.Text("OrderNum")
}

func (o Order) Side() string {
	switch strings.ToUpper(o.Text("Operation")) {
	case "КУПЛЯ", "ПОКУПКА", "BUY":
		return "BUY"
	case "ПРОДАЖА", "SELL":
		return "SELL"
	}
	return ""
}

type OrderFilter struct {
	// nil is admin/all firms. A non-nil empty string matches no firm.
	Firm *string
	// State matches the raw "State" column; empty means any state.
	State  string
	Limit  int
	Offset int
}

type OrderPage struct {
	Orders     []Order `json:"orders"`
	HasMore    bool    `json:"has_more"`
	NextOffset int     `json:"next_offset"`
}

func BrokerOrderFirm(brokerCode, tokenFirm string) string {
	if firm := strings.TrimSpace(tokenFirm); firm != "" {
		return firm
	}
	return strings.TrimSpace(brokerCode)
}

// ListOrders applies the authorization predicate in SQL, before pagination.
// Limit=0 is reserved for the internal change reader; HTTP callers are paged.
func (s *Store) ListOrders(ctx context.Context, f OrderFilter) (OrderPage, error) {
	out := OrderPage{Orders: []Order{}}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	limit := f.Limit
	if limit > 0 {
		limit++
	}
	rows, err := s.pool.Query(ctx, `SELECT row_to_json(o) FROM public."Orders" o
WHERE ($1::text IS NULL OR (btrim(o."FirmId")=$1 AND $1<>''))
AND ($4::text='' OR btrim(o."State"::text)=$4)
ORDER BY o."TradeDate" DESC, o."OrderNum" DESC, o."ClassCode"
LIMIT NULLIF($2,0) OFFSET $3`, f.Firm, limit, f.Offset, f.State)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		out.Orders = append(out.Orders, Order{Raw: raw})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if f.Limit > 0 && len(out.Orders) > f.Limit {
		out.HasMore = true
		out.Orders = out.Orders[:f.Limit]
	}
	out.NextOffset = f.Offset + len(out.Orders)
	return out, nil
}

// CountOrdersByState groups the whole table by its raw "State" value in the
// database, so dashboard counters do not transfer every row.
func (s *Store) CountOrdersByState(ctx context.Context) (map[string]int, error) {
	out := map[string]int{}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	rows, err := s.pool.Query(ctx, `SELECT COALESCE(o."State"::text, ''), count(*) FROM public."Orders" o GROUP BY 1`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		if err := rows.Scan(&state, &n); err != nil {
			return out, err
		}
		out[state] += n
	}
	return out, rows.Err()
}

// Keys include all three columns of Orders_pkey, so reused numbers cannot match
// another class or trading day. FIX venue IDs are accepted for local lookups.
func ParseOrderKey(key string) (date, board, number string, err error) {
	key = strings.TrimPrefix(key, "quik-")
	first, last := strings.IndexByte(key, '-'), strings.LastIndexByte(key, '-')
	if first < 0 || last <= first {
		return "", "", "", ErrOrderKey
	}
	d, board, number := key[:first], key[first+1:last], key[last+1:]
	layout := "20060102"
	if len(d) == 6 {
		layout = "060102"
	} else if len(d) != 8 {
		return "", "", "", ErrOrderKey
	}
	parsed, e := time.Parse(layout, d)
	if e != nil || board == "" || len(board) > 12 || strings.ContainsAny(board, "/\\\x00\r\n") || number == "" || len(number) > 19 {
		return "", "", "", ErrOrderKey
	}
	for _, c := range number {
		if c < '0' || c > '9' {
			return "", "", "", ErrOrderKey
		}
	}
	return parsed.Format("2006-01-02"), board, number, nil
}

func (s *Store) GetOrder(ctx context.Context, key string, firm *string) (Order, error) {
	if s == nil || s.pool == nil {
		return Order{}, ErrNotConfigured
	}
	date, board, number, err := ParseOrderKey(key)
	if err != nil {
		return Order{}, err
	}
	var raw []byte
	err = s.pool.QueryRow(ctx, `SELECT row_to_json(o) FROM public."Orders" o
WHERE o."TradeDate"=$1::date AND o."ClassCode"=$2 AND o."OrderNum"=$3::numeric
AND ($4::text IS NULL OR (btrim(o."FirmId")=$4 AND $4<>''))`, date, board, number, firm).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrOrderNotFound
	}
	if err != nil {
		return Order{}, err
	}
	return Order{Raw: raw}, nil
}

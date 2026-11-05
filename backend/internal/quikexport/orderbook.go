package quikexport

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
)

// BookLevel is one price of an order book: the remaining quantity (Balance)
// of the active orders at that price, its volume (price × quantity) and how
// many orders make it up. Participants are not part of it.
type BookLevel struct {
	Price    json.RawMessage `json:"price"`
	Quantity json.RawMessage `json:"quantity"`
	Volume   json.RawMessage `json:"volume"`
	Orders   int             `json:"orders"`
}

// OrderBook is an instrument's book (поручение пользователя 05.11.2026): bids
// (BUY) from the highest price, offers (SELL) from the lowest.
type OrderBook struct {
	Bids   []BookLevel `json:"bids"`
	Offers []BookLevel `json:"offers"`
}

// orderBookRows groups today's active Orders rows of one instrument, every
// firm, by side and price. Rows without a price or remaining quantity hold no
// place in the book.
const orderBookRows = `SELECT btrim(o."Operation"::text), o."Price"::text, sum(o."Balance")::text, count(*)
FROM public."Orders" o
WHERE btrim(o."State"::text)=$1
AND o."TradeDate"::date=(now() AT TIME ZONE 'Asia/Bishkek')::date
AND btrim(o."ClassCode"::text)=$2 AND btrim(o."SecCode"::text)=$3
AND ($4::text[] IS NULL OR btrim(o."ClassCode"::text)=ANY($4::text[]))
AND o."Price" IS NOT NULL AND o."Balance">0
GROUP BY 1, o."Price"`

// GetOrderBook builds the book from the Orders rows in State activeState.
func (s *Store) GetOrderBook(ctx context.Context, classCode, secCode, activeState string) (OrderBook, error) {
	out := OrderBook{Bids: []BookLevel{}, Offers: []BookLevel{}}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	classes, err := s.servedClasses(ctx)
	if err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, orderBookRows, activeState, strings.TrimSpace(classCode), strings.TrimSpace(secCode), classes)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	type level struct {
		BookLevel
		price decimal.Decimal
	}
	var bids, offers []level
	for rows.Next() {
		var operation, price, quantity string
		var n int
		if err := rows.Scan(&operation, &price, &quantity, &n); err != nil {
			return out, err
		}
		px, err := decimal.NewFromString(price)
		if err != nil {
			continue
		}
		qty, err := decimal.NewFromString(quantity)
		if err != nil {
			continue
		}
		l := level{BookLevel{Price: json.RawMessage(price), Quantity: json.RawMessage(quantity), Volume: json.RawMessage(px.Mul(qty).String()), Orders: n}, px}
		switch OperationSide(operation) {
		case "BUY":
			bids = append(bids, l)
		case "SELL":
			offers = append(offers, l)
		}
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	sort.Slice(bids, func(i, j int) bool { return bids[i].price.GreaterThan(bids[j].price) })
	sort.Slice(offers, func(i, j int) bool { return offers[i].price.LessThan(offers[j].price) })
	for _, l := range bids {
		out.Bids = append(out.Bids, l.BookLevel)
	}
	for _, l := range offers {
		out.Offers = append(out.Offers, l.BookLevel)
	}
	return out, nil
}

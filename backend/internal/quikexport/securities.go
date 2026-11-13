package quikexport

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrInstrumentNotFound = errors.New("instrument is not present in QUIK Securities")

// Securities keeps a row per instrument and TradeDate; the current dictionary is
// the latest date, so an instrument QUIK no longer exports is not offered.
const securitiesCurrent = `s."TradeDate"=(SELECT max(x."TradeDate") FROM public."Securities" x)`

type SecurityFilter struct {
	Limit  int
	Offset int
}

type SecurityPage struct {
	Securities []Row
	HasMore    bool
	NextOffset int
}

// ListSecurities pages the current rows of the served classes as stored. Limit=0
// reads them all, for the market data subscription; HTTP callers are paged.
func (s *Store) ListSecurities(ctx context.Context, f SecurityFilter) (SecurityPage, error) {
	out := SecurityPage{Securities: []Row{}}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	classes, err := s.servedClasses(ctx)
	if err != nil {
		return out, err
	}
	limit := f.Limit
	if limit > 0 {
		limit++
	}
	rows, err := s.rows(ctx, `SELECT row_to_json(s) FROM public."Securities" s
WHERE `+securitiesCurrent+` AND ($1::text[] IS NULL OR btrim(s."ClassCode"::text)=ANY($1::text[]))
ORDER BY s."ClassCode", s."SecCode" LIMIT NULLIF($2,0) OFFSET $3`, classes, limit, f.Offset)
	if err != nil {
		return out, err
	}
	out.Securities = rows
	if f.Limit > 0 && len(rows) > f.Limit {
		out.HasMore = true
		out.Securities = rows[:f.Limit]
	}
	out.NextOffset = f.Offset + len(out.Securities)
	return out, nil
}

// Instrument finds the instrument of a new order in the current dictionary. The
// served classes are checked by the order service, which also knows the broker.
func (s *Store) Instrument(ctx context.Context, classCode, secCode string) (Row, error) {
	classCode, secCode = strings.TrimSpace(classCode), strings.TrimSpace(secCode)
	if classCode == "" || secCode == "" {
		return Row{}, ErrInstrumentNotFound
	}
	return s.row(ctx, ErrInstrumentNotFound, `SELECT row_to_json(s) FROM public."Securities" s
WHERE `+securitiesCurrent+` AND btrim(s."ClassCode"::text)=$1 AND btrim(s."SecCode"::text)=$2 LIMIT 1`, classCode, secCode)
}

// OrderDayCount is the number of Orders rows of one trading day and raw State.
type OrderDayCount struct {
	Date  string
	State string
	Count int64
}

// CountOrdersByDay groups the rows of the given firms since a trading date.
func (s *Store) CountOrdersByDay(ctx context.Context, firms []string, since time.Time) ([]OrderDayCount, error) {
	out := []OrderDayCount{}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	classes, err := s.servedClasses(ctx)
	if err != nil {
		return out, err
	}
	rows, err := s.pool.Query(ctx, `SELECT to_char(o."TradeDate"::date,'YYYY-MM-DD'), COALESCE(o."State"::text,''), count(*) FROM public."Orders" o
WHERE btrim(o."FirmId"::text)=ANY($1::text[]) AND o."TradeDate">=$2::date
AND ($3::text[] IS NULL OR btrim(o."ClassCode"::text)=ANY($3::text[])) GROUP BY 1,2`, firms, since.Format("2006-01-02"), classes)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c OrderDayCount
		if err := rows.Scan(&c.Date, &c.State, &c.Count); err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

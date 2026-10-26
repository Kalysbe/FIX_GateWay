package quikexport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrParamNotFound = errors.New("instrument is not present in QUIK Params")

// Param is one row of the QUIK current trades table, kept verbatim as the
// database JSON. Rows are identified by ClassCode and SecCode.
type Param struct {
	Raw json.RawMessage
}

func (p Param) MarshalJSON() ([]byte, error) {
	if len(p.Raw) == 0 {
		return []byte("null"), nil
	}
	return p.Raw, nil
}
func (p *Param) UnmarshalJSON(raw []byte) error { p.Raw = append(p.Raw[:0], raw...); return nil }

func (p Param) Key() string {
	var fields map[string]json.RawMessage
	if json.Unmarshal(p.Raw, &fields) != nil {
		return "\x00"
	}
	return fieldText(fields["ClassCode"]) + "\x00" + fieldText(fields["SecCode"])
}

// paramsToday keeps only rows of the current KSE trading day: Params holds a
// row per instrument and TradeDate, and the QUIK server's own time zone is not
// the exchange's (UTC+6, Asia/Bishkek).
const paramsToday = `p."TradeDate"::date=(now() AT TIME ZONE 'Asia/Bishkek')::date`

type ParamFilter struct {
	// Empty values match any class or instrument.
	ClassCode string
	SecCode   string
	Limit     int
	Offset    int
}

type ParamPage struct {
	Params     []Param
	HasMore    bool
	NextOffset int
}

// ListParams pages the table in SQL. Limit=0 is reserved for the change reader.
func (s *Store) ListParams(ctx context.Context, f ParamFilter) (ParamPage, error) {
	out := ParamPage{Params: []Param{}}
	if s == nil || s.pool == nil {
		return out, ErrNotConfigured
	}
	limit := f.Limit
	if limit > 0 {
		limit++
	}
	rows, err := s.pool.Query(ctx, `SELECT row_to_json(p) FROM public."Params" p
WHERE `+paramsToday+` AND ($1='' OR btrim(p."ClassCode"::text)=$1) AND ($2='' OR btrim(p."SecCode"::text)=$2)
ORDER BY p."ClassCode", p."SecCode"
LIMIT NULLIF($3,0) OFFSET $4`, f.ClassCode, f.SecCode, limit, f.Offset)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return out, err
		}
		out.Params = append(out.Params, Param{Raw: raw})
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if f.Limit > 0 && len(out.Params) > f.Limit {
		out.HasMore = true
		out.Params = out.Params[:f.Limit]
	}
	out.NextOffset = f.Offset + len(out.Params)
	return out, nil
}

func (s *Store) GetParam(ctx context.Context, classCode, secCode string) (Param, error) {
	if s == nil || s.pool == nil {
		return Param{}, ErrNotConfigured
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT row_to_json(p) FROM public."Params" p
WHERE `+paramsToday+` AND btrim(p."ClassCode"::text)=$1 AND btrim(p."SecCode"::text)=$2 LIMIT 1`, classCode, secCode).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return Param{}, ErrParamNotFound
	}
	if err != nil {
		return Param{}, err
	}
	return Param{Raw: raw}, nil
}

type ParamChange struct {
	Param   Param
	Removed bool
}

// RunParams publishes row changes once per second. As with Orders, the first
// complete selection is a baseline and a failed read is never an empty table.
func (s *Store) RunParams(ctx context.Context, publish func(ParamChange), failure func(error)) {
	runParamFeed(ctx, s.ListParams, time.Second, publish, failure)
}

func runParamFeed(ctx context.Context, read func(context.Context, ParamFilter) (ParamPage, error), interval time.Duration, publish func(ParamChange), failure func(error)) {
	previous := map[string]Param{}
	primed := false
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		page, err := read(work, ParamFilter{})
		cancel()
		if err != nil {
			if ctx.Err() == nil && failure != nil {
				failure(err)
			}
		} else {
			current := make(map[string]Param, len(page.Params))
			for _, p := range page.Params {
				current[p.Key()] = p
			}
			if primed {
				for _, c := range paramChanges(previous, current) {
					publish(c)
				}
			}
			previous, primed = current, true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func paramChanges(previous, current map[string]Param) []ParamChange {
	changes := []ParamChange{}
	for key, p := range current {
		if old, ok := previous[key]; !ok || !bytes.Equal(old.Raw, p.Raw) {
			changes = append(changes, ParamChange{Param: p})
		}
	}
	for key, p := range previous {
		if _, ok := current[key]; !ok {
			changes = append(changes, ParamChange{Param: p, Removed: true})
		}
	}
	return changes
}

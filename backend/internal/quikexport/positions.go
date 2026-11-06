package quikexport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Positions are the current rows of QUIK "MoneyLimits" (cash) and "DepoLimits"
// (instruments), as stored. Both tables have no ClassCode, so the served classes
// do not limit them.
type Positions struct {
	Cash        []Row `json:"cash"`
	Instruments []Row `json:"instruments"`
}

// Both tables key rows by LoadDate; the current state is the latest date of each,
// so a position QUIK no longer exports is not offered.
const (
	moneyLimitsCurrent = `SELECT row_to_json(m) FROM public."MoneyLimits" m
WHERE m."LoadDate"=(SELECT max(x."LoadDate") FROM public."MoneyLimits" x)
AND ($1::text[] IS NULL OR btrim(m."FirmId"::text)=ANY($1::text[]))
ORDER BY m."FirmId", m."ClientCode", m."Tag", m."CurrCode", m."LimitKind"`
	depoLimitsCurrent = `SELECT row_to_json(d) FROM public."DepoLimits" d
WHERE d."LoadDate"=(SELECT max(x."LoadDate") FROM public."DepoLimits" x)
AND ($1::text[] IS NULL OR btrim(d."FirmId"::text)=ANY($1::text[]))
ORDER BY d."FirmId", d."ClientCode", d."SecCode", d."Trdacc", d."LimitKind"`
)

// Position tables, as the broker API names them: GET /api/v1/positions/<table>
// and the gRPC event <table>.updated (поручение пользователя 04.11.2026).
const (
	MoneyLimits = "moneylimits"
	DepoLimits  = "depolimits"
)

// ListPositions applies the firm predicate in SQL. nil is admin/all firms; a
// non-nil list matches only its firms, so an empty one matches nothing.
func (s *Store) ListPositions(ctx context.Context, firms []string) (Positions, error) {
	out := Positions{Cash: []Row{}, Instruments: []Row{}}
	var err error
	if out.Cash, err = s.ListMoneyLimits(ctx, firms); err != nil {
		return Positions{Cash: []Row{}, Instruments: []Row{}}, err
	}
	if out.Instruments, err = s.ListDepoLimits(ctx, firms); err != nil {
		return Positions{Cash: []Row{}, Instruments: []Row{}}, err
	}
	return out, nil
}

// ListMoneyLimits returns the current "MoneyLimits" rows; firms as in ListPositions.
func (s *Store) ListMoneyLimits(ctx context.Context, firms []string) ([]Row, error) {
	return s.positionRows(ctx, moneyLimitsCurrent, firms)
}

// ListDepoLimits returns the current "DepoLimits" rows; firms as in ListPositions.
func (s *Store) ListDepoLimits(ctx context.Context, firms []string) ([]Row, error) {
	return s.positionRows(ctx, depoLimitsCurrent, firms)
}

// ListPositionTable reads one table by its API name.
func (s *Store) ListPositionTable(ctx context.Context, table string, firms []string) ([]Row, error) {
	switch table {
	case MoneyLimits:
		return s.ListMoneyLimits(ctx, firms)
	case DepoLimits:
		return s.ListDepoLimits(ctx, firms)
	}
	return nil, ErrUnknownPositionTable
}

// ErrUnknownPositionTable is returned for a table other than MoneyLimits or DepoLimits.
var ErrUnknownPositionTable = errors.New("unknown position table")

func (s *Store) positionRows(ctx context.Context, query string, firms []string) ([]Row, error) {
	if firms != nil {
		trimmed := make([]string, 0, len(firms))
		for _, firm := range firms {
			if firm = strings.TrimSpace(firm); firm != "" {
				trimmed = append(trimmed, firm)
			}
		}
		firms = trimmed
	}
	rows, err := s.rows(ctx, query, firms)
	if err != nil {
		return []Row{}, err
	}
	return rows, nil
}

// RunPositions reports which table changed (MoneyLimits or DepoLimits), once per
// second at most. The first complete selection is a baseline and a failed read is
// never an empty table.
func (s *Store) RunPositions(ctx context.Context, changed func(table string), failure func(error)) {
	runPositionFeed(ctx, s.ListPositions, time.Second, changed, failure)
}

func runPositionFeed(ctx context.Context, read func(context.Context, []string) (Positions, error), interval time.Duration, changed func(table string), failure func(error)) {
	var previousCash, previousDepo []byte
	primed := false
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		positions, err := read(work, nil)
		cancel()
		var cash, depo []byte
		if err == nil {
			if cash, err = json.Marshal(positions.Cash); err == nil {
				depo, err = json.Marshal(positions.Instruments)
			}
		}
		if err != nil {
			if ctx.Err() == nil && failure != nil {
				failure(err)
			}
		} else {
			if primed && !bytes.Equal(previousCash, cash) {
				changed(MoneyLimits)
			}
			if primed && !bytes.Equal(previousDepo, depo) {
				changed(DepoLimits)
			}
			previousCash, previousDepo, primed = cash, depo, true
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

package quikexport

import (
	"bytes"
	"context"
	"encoding/json"
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

// ListPositions applies the firm predicate in SQL. nil is admin/all firms; a
// non-nil list matches only its firms, so an empty one matches nothing.
func (s *Store) ListPositions(ctx context.Context, firms []string) (Positions, error) {
	out := Positions{Cash: []Row{}, Instruments: []Row{}}
	if firms != nil {
		trimmed := make([]string, 0, len(firms))
		for _, firm := range firms {
			if firm = strings.TrimSpace(firm); firm != "" {
				trimmed = append(trimmed, firm)
			}
		}
		firms = trimmed
	}
	var err error
	if out.Cash, err = s.rows(ctx, moneyLimitsCurrent, firms); err != nil {
		return Positions{Cash: []Row{}, Instruments: []Row{}}, err
	}
	if out.Instruments, err = s.rows(ctx, depoLimitsCurrent, firms); err != nil {
		return Positions{Cash: []Row{}, Instruments: []Row{}}, err
	}
	return out, nil
}

// RunPositions reports that positions changed, once per second at most. The first
// complete selection is a baseline and a failed read is never an empty table.
func (s *Store) RunPositions(ctx context.Context, changed func(), failure func(error)) {
	runPositionFeed(ctx, s.ListPositions, time.Second, changed, failure)
}

func runPositionFeed(ctx context.Context, read func(context.Context, []string) (Positions, error), interval time.Duration, changed func(), failure func(error)) {
	var previous []byte
	primed := false
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		positions, err := read(work, nil)
		cancel()
		var current []byte
		if err == nil {
			current, err = json.Marshal(positions)
		}
		if err != nil {
			if ctx.Err() == nil && failure != nil {
				failure(err)
			}
		} else {
			if primed && !bytes.Equal(previous, current) {
				changed()
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

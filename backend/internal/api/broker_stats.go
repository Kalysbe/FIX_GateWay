package api

import (
	"context"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/quikexport"
)

type dayCount struct {
	Date   string `json:"date"`
	Orders int64  `json:"orders"`
}

type brokerPeriodStats struct {
	Orders   int64 `json:"orders"`
	Filled   int64 `json:"filled"`
	Rejected int64 `json:"rejected"`
	Active   int64 `json:"active"`
}

func (p *brokerPeriodStats) add(state string, n int64) {
	p.Orders += n
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "ИСПОЛНЕНА", "FILLED":
		p.Filled += n
	case "ОТКЛОНЕНА", "REJECTED":
		p.Rejected += n
	case "АКТИВНА", "ACTIVE":
		p.Active += n
	}
}

type brokerStats struct {
	Days     int               `json:"days"`
	Today    brokerPeriodStats `json:"today"`
	Period   brokerPeriodStats `json:"period"`
	Previous brokerPeriodStats `json:"previous"`
	Series   []dayCount        `json:"series"`
}

var kseZone = time.FixedZone("Asia/Bishkek", 6*60*60)

// brokerOrderStats counts the QUIK Orders rows of the broker's firms over the
// last days and the same number of days before them, by trading date.
func brokerOrderStats(ctx context.Context, src quikRefReader, firms []string, days int, now time.Time) (brokerStats, error) {
	if days <= 0 {
		days = 7
	}
	if days > 90 {
		days = 90
	}
	now = now.In(kseZone)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, kseZone)
	periodStart := today.AddDate(0, 0, -(days - 1))
	out := brokerStats{Days: days, Series: []dayCount{}}
	byDay := map[string]int64{}
	if len(firms) > 0 {
		counts, err := src.CountOrdersByDay(ctx, firms, periodStart.AddDate(0, 0, -days))
		if err != nil {
			return out, err
		}
		todayKey, startKey := today.Format("2006-01-02"), periodStart.Format("2006-01-02")
		for _, c := range counts {
			// ISO dates compare in calendar order.
			switch {
			case c.Date < startKey:
				out.Previous.add(c.State, c.Count)
				continue
			case c.Date == todayKey:
				out.Today.add(c.State, c.Count)
			}
			out.Period.add(c.State, c.Count)
			byDay[c.Date] += c.Count
		}
	}
	for d := periodStart; !d.After(today); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out.Series = append(out.Series, dayCount{Date: key, Orders: byDay[key]})
	}
	return out, nil
}

var _ quikRefReader = (*quikexport.Store)(nil)

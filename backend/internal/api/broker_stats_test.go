package api

import (
	"context"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/quikexport"
)

type statsReader struct {
	quikRefReader
	counts []quikexport.OrderDayCount
	since  time.Time
}

func (r *statsReader) CountOrdersByDay(_ context.Context, _ []string, since time.Time) ([]quikexport.OrderDayCount, error) {
	r.since = since
	return r.counts, nil
}

func TestBrokerOrderStatsSplitsByTradingDay(t *testing.T) {
	// 20:00 UTC on 1 Nov is already 2 Nov on the exchange.
	now := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)
	src := &statsReader{counts: []quikexport.OrderDayCount{
		{Date: "2026-11-02", State: "Активна", Count: 2},
		{Date: "2026-11-02", State: "Исполнена", Count: 1},
		{Date: "2026-11-01", State: "Отклонена", Count: 4},
		{Date: "2026-10-31", State: "Снята", Count: 8},
	}}
	st, err := brokerOrderStats(context.Background(), src, []string{"2040"}, 2, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := src.since.Format("2006-01-02"); got != "2026-10-30" {
		t.Fatalf("read since %s", got)
	}
	if st.Today != (brokerPeriodStats{Orders: 3, Filled: 1, Active: 2}) {
		t.Fatalf("today %+v", st.Today)
	}
	if st.Period != (brokerPeriodStats{Orders: 7, Filled: 1, Active: 2, Rejected: 4}) {
		t.Fatalf("period %+v", st.Period)
	}
	if st.Previous != (brokerPeriodStats{Orders: 8}) {
		t.Fatalf("previous %+v", st.Previous)
	}
	if len(st.Series) != 2 || st.Series[0] != (dayCount{"2026-11-01", 4}) || st.Series[1] != (dayCount{"2026-11-02", 3}) {
		t.Fatalf("series %+v", st.Series)
	}
}

func TestBrokerOrderStatsWithoutFirmsReadsNothing(t *testing.T) {
	src := &statsReader{}
	st, err := brokerOrderStats(context.Background(), src, nil, 1, time.Now())
	if err != nil || !src.since.IsZero() || st.Period.Orders != 0 || len(st.Series) != 1 {
		t.Fatalf("%+v %v", st, err)
	}
}

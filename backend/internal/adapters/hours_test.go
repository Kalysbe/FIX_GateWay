package adapters

import (
	"testing"
	"time"
)

func TestSessionHoursAcrossMidnight(t *testing.T) {
	bishkek := time.FixedZone("Asia/Bishkek", 6*60*60)
	h, err := ParseSessionHours("09:05-03:59", bishkek)
	if err != nil {
		t.Fatal(err)
	}
	at := func(hh, mm int) time.Time { return time.Date(2026, 11, 5, hh, mm, 0, 0, bishkek) }
	for _, c := range []struct {
		t    time.Time
		open bool
	}{
		{at(9, 4), false}, {at(9, 5), true}, {at(15, 0), true}, {at(23, 59), true},
		{at(0, 30), true}, {at(3, 58), true}, {at(3, 59), false}, {at(4, 0), false}, {at(6, 0), false},
		{at(9, 5).UTC(), true}, // the zone of the argument does not matter
	} {
		if got := h.Open(c.t); got != c.open {
			t.Errorf("%s: open=%v", c.t.In(bishkek).Format("15:04"), got)
		}
	}
	day, _ := ParseSessionHours("10:00-16:00", bishkek)
	if day.Open(at(9, 59)) || !day.Open(at(10, 0)) || day.Open(at(16, 0)) {
		t.Error("same-day window")
	}
	if none, err := ParseSessionHours("", bishkek); err != nil || none != nil || !none.Open(at(5, 0)) {
		t.Error("empty spec is always open")
	}
	for _, bad := range []string{"9-4", "09:05", "25:00-04:00", "09:05-09:05"} {
		if _, err := ParseSessionHours(bad, bishkek); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

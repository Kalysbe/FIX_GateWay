package adapters

import (
	"fmt"
	"strings"
	"time"
)

// SessionHours is the daily window in which the QUIK FIX adapters work
// (поручение пользователя 05.11.2026: они выключаются в 04:00 и запускаются в
// 09:05 по Бишкеку). Outside it the gateway holds no FIX session and does not
// try to connect. A window may cross midnight. nil means always open.
type SessionHours struct {
	start, end time.Duration
	loc        *time.Location
	spec       string
}

// ParseSessionHours reads "HH:MM-HH:MM" in loc; an empty spec is always open.
func ParseSessionHours(spec string, loc *time.Location) (*SessionHours, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, nil
	}
	from, to, ok := strings.Cut(spec, "-")
	if !ok {
		return nil, fmt.Errorf("FIX session hours %q: want HH:MM-HH:MM", spec)
	}
	start, err := clockTime(from)
	if err != nil {
		return nil, fmt.Errorf("FIX session hours %q: %w", spec, err)
	}
	end, err := clockTime(to)
	if err != nil {
		return nil, fmt.Errorf("FIX session hours %q: %w", spec, err)
	}
	if start == end {
		return nil, fmt.Errorf("FIX session hours %q: start equals end", spec)
	}
	return &SessionHours{start: start, end: end, loc: loc, spec: spec}, nil
}

func clockTime(s string) (time.Duration, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("bad time %q", s)
	}
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute, nil
}

// Open reports whether t is inside the window.
func (h *SessionHours) Open(t time.Time) bool {
	if h == nil {
		return true
	}
	local := t.In(h.loc)
	tod := time.Duration(local.Hour())*time.Hour + time.Duration(local.Minute())*time.Minute + time.Duration(local.Second())*time.Second
	if h.start < h.end {
		return tod >= h.start && tod < h.end
	}
	return tod >= h.start || tod < h.end
}

func (h *SessionHours) String() string {
	if h == nil {
		return "always"
	}
	return h.spec + " " + h.loc.String()
}

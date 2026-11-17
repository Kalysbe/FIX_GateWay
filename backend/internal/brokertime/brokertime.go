// Package brokertime gives dates and times in broker replies one readable form
// (поручение пользователя 17.11.2026). QUIK stores KSE local time (UTC+6)
// without a zone, a date as midnight ("2026-11-17T00:00:00") and a time of day
// with a dummy date ("1900-01-01T16:40:15"). Brokers get instead, always in
// KSE time (Bishkek, UTC+6), the zone not written:
//
//	date         "17.11.2026"
//	time of day  "16:40:15"
//	date-time    "17.11.2026 15:54:45"
//
// The gateway's own UTC timestamps ("…Z") and values carrying a zone are
// shown in KSE time too. Milliseconds are kept only when not zero.
package brokertime

import (
	"regexp"
	"strings"
	"time"
)

// Zone is KSE time; QUIK writes its timestamps in it.
var Zone = time.FixedZone("Asia/Bishkek", 6*60*60)

// A JSON member whose value is a timestamp. JSON written by encoding/json and
// by PostgreSQL row_to_json is compact, so a member is exactly "key":"value";
// a quote inside a string is escaped and cannot start a match.
var member = regexp.MustCompile(`"([^"\\]{1,128})":"(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})?"`)

// Format rewrites every timestamp member of a compact JSON document.
func Format(doc []byte) []byte {
	if len(doc) == 0 {
		return doc
	}
	return member.ReplaceAllFunc(doc, func(m []byte) []byte {
		p := member.FindSubmatch(m)
		key, date, clock, frac, zone := string(p[1]), string(p[2]), string(p[3]), string(p[4]), string(p[5])
		value, ok := formatValue(key, date, clock, frac, zone)
		if !ok {
			return m
		}
		return []byte(`"` + key + `":"` + value + `"`)
	})
}

// FormatString is Format for a JSON document held as a string.
func FormatString(doc string) string {
	return string(Format([]byte(doc)))
}

func formatValue(key, date, clock, frac, zone string) (string, bool) {
	frac = trimFraction(frac)
	if zone != "" {
		// A moment with its zone (gateway UTC, market trades +06:00).
		if zone == "Z" || zone == "-00:00" {
			zone = "+00:00"
		}
		t, err := time.Parse(time.RFC3339Nano, date+"T"+clock+frac+zone)
		if err != nil {
			return "", false
		}
		return t.In(Zone).Format(DateTimeLayout) + frac, true
	}
	// QUIK local time.
	if date == "1900-01-01" {
		return clock + frac, true
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", false
	}
	if clock == "00:00:00" && frac == "" && !dateTimeKey(key) {
		return d.Format(DateLayout), true
	}
	return d.Format(DateLayout) + " " + clock + frac, true
}

// Layouts of broker dates and times; ParseInput accepts them back.
const (
	DateLayout     = "02.01.2006"
	DateTimeLayout = "02.01.2006 15:04:05"
)

// ParseInput reads a broker-given date ("17.11.2026") or date-time
// ("17.11.2026 15:54:45", seconds optional) as KSE time. date tells a date.
func ParseInput(value string) (t time.Time, date bool, ok bool) {
	value = strings.TrimSpace(value)
	if t, err := time.ParseInLocation(DateLayout, value, Zone); err == nil {
		return t, true, true
	}
	for _, layout := range []string{DateTimeLayout, "02.01.2006 15:04"} {
		if t, err := time.ParseInLocation(layout, value, Zone); err == nil {
			return t, false, true
		}
	}
	return time.Time{}, false, false
}

// trimFraction drops an empty fraction (".000"); a real one stays as given.
func trimFraction(frac string) string {
	if strings.TrimRight(frac, "0") == "." {
		return ""
	}
	return frac
}

// dateTimeKey names columns holding a moment, not a date, so that midnight in
// them stays a date-time: OrderDateTime, ExpiryTime, lotsizechangedat, created_at…
func dateTimeKey(key string) bool {
	k := strings.ToLower(key)
	return strings.HasSuffix(k, "time") || strings.HasSuffix(k, "_at") || strings.HasSuffix(k, "changedat") || strings.HasSuffix(k, "_ti")
}

package api

import (
	"testing"
	"time"
)

// A failure is repeated every five minutes while it lasts; a configuration
// notice (ResetOnLogon) is reported once when it appears and again only after
// it was cleared.
func TestFIXAlarmRepeatsFailuresButNotNotices(t *testing.T) {
	now := time.Now()
	fresh := fixAlarm{}
	shown := fixAlarm{active: true, notified: now.Add(-6 * time.Minute)}
	recent := fixAlarm{active: true, notified: now.Add(-time.Minute)}
	failure := fixProblem{code: "EXECUTION_INBOX_BLOCKED", active: true}
	notice := fixProblem{code: "FIX_SEQUENCE_RESET_a", active: true, notice: true}
	for _, c := range []struct {
		name     string
		previous fixAlarm
		problem  fixProblem
		want     bool
	}{
		{"new failure", fresh, failure, true},
		{"failure within five minutes", recent, failure, false},
		{"failure after five minutes", shown, failure, true},
		{"new notice", fresh, notice, true},
		{"notice after five minutes", shown, notice, false},
		{"inactive", fresh, fixProblem{code: "x"}, false},
	} {
		if got := fixAlarmDue(c.previous, c.problem, now); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

package api

import (
	"github.com/fix-gateway/backend/internal/tradeledger"
	"testing"
	"time"
)

func TestTradeSafetyRequiresFreshSuccessfulReconciliation(t *testing.T) {
	now := time.Now()
	old := now.Add(-6 * time.Minute)
	recent := now.Add(-time.Minute)
	for _, test := range []struct {
		name    string
		h       tradeledger.Health
		started time.Time
		want    map[string]bool
	}{
		{"startup", tradeledger.Health{}, now, map[string]bool{}},
		{"never succeeded", tradeledger.Health{}, old, map[string]bool{"TRADE_SYNC_STALE": true}},
		{"stale success", tradeledger.Health{LastSuccess: &old}, now, map[string]bool{"TRADE_SYNC_STALE": true}},
		{"failed after success", tradeledger.Health{LastSuccess: &recent, LatestError: "history regressed"}, old, map[string]bool{"TRADE_SYNC_FAILED": true}},
		{"missing and conflicting", tradeledger.Health{LastSuccess: &recent, Incomplete: 1, ReportErrors: 2}, old, map[string]bool{"TRADE_PAIR_INCOMPLETE": true, "TRADE_SOURCE_ERROR": true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, p := range tradeProblems(test.h, now, test.started) {
				if p.active != test.want[p.code] {
					t.Fatalf("%s active=%v", p.code, p.active)
				}
			}
		})
	}
}

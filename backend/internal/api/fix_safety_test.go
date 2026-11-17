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

// The served gRPC certificate warns once 30 days ahead and is critical once expired.
func TestGRPCCertProblems(t *testing.T) {
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	state := func(notAfter time.Time) map[string]fixProblem {
		out := map[string]fixProblem{}
		for _, p := range grpcCertProblems(notAfter, now) {
			out[p.code] = p
		}
		return out
	}
	if got := grpcCertProblems(time.Time{}, now); got != nil {
		t.Fatalf("unknown expiry reported %v", got)
	}
	far := state(now.Add(200 * 24 * time.Hour))
	if far["GRPC_CERT_EXPIRING"].active || far["GRPC_CERT_EXPIRED"].active {
		t.Fatalf("a distant expiry is not a problem: %+v", far)
	}
	soon := state(now.Add(10 * 24 * time.Hour))
	if p := soon["GRPC_CERT_EXPIRING"]; !p.active || !p.notice {
		t.Fatalf("expiry in 10 days must warn once: %+v", p)
	}
	if soon["GRPC_CERT_EXPIRED"].active {
		t.Fatal("not expired yet")
	}
	gone := state(now.Add(-26 * time.Hour))
	if p := gone["GRPC_CERT_EXPIRED"]; !p.active || p.notice {
		t.Fatalf("an expired certificate is a repeated failure: %+v", p)
	}
	if gone["GRPC_CERT_EXPIRING"].active {
		t.Fatal("expired supersedes expiring")
	}
}

package arqa

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

type mdFixture struct {
	adapter *Adapter
	now     time.Time
	sent    []*quickfix.Message
}

func newMDFixture() *mdFixture {
	f := &mdFixture{now: time.Date(2026, 10, 7, 4, 0, 0, 0, time.UTC)}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.adapter = New(domain.Adapter{ID: "a1"}, log, nil, nil, "")
	f.adapter.app.onLogon = nil
	f.adapter.app.loggedOn.Store(true)
	f.adapter.app.mdNow = func() time.Time { return f.now }
	f.adapter.mdSend = func(msg *quickfix.Message) error { f.sent = append(f.sent, msg); return nil }
	return f
}
func mdQuery(symbols ...string) domain.MarketDataQuery {
	q := domain.MarketDataQuery{}
	for _, symbol := range symbols {
		q.Instruments = append(q.Instruments, domain.SecurityRef{SecurityID: symbol, Symbol: symbol, Board: "BOARD", SecurityType: "CS"})
	}
	return q
}
func mdReply(id, symbol, status string) *quickfix.Message {
	m := quickfix.NewMessage()
	m.Header.SetString(tag.MsgType, "W")
	m.Body.SetString(tag.MDReqID, id)
	m.Body.SetString(tag.SecurityID, symbol)
	m.Body.SetString(tag.ExDestination, "BOARD")
	m.Body.SetString(tag.NoMDEntries, "0")
	if status != "" {
		m.Body.SetString(tag.SecurityTradingStatus, status)
	}
	return m
}
func (f *mdFixture) ensure(t *testing.T, q domain.MarketDataQuery) string {
	t.Helper()
	id, _, err := f.adapter.SubscribeMarketData(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func (f *mdFixture) subscribeCount() int {
	count := 0
	for _, msg := range f.sent {
		if sub, _ := msg.Body.GetString(tag.SubscriptionRequestType); sub == "1" {
			count++
		}
	}
	return count
}

func TestAutomaticMDRejectRecoveryKeepsHealthyStreamsAndRejectsLateReplies(t *testing.T) {
	f := newMDFixture()
	id := f.ensure(t, mdQuery("AAA"))
	if state := f.adapter.MarketDataSubscription(); state.Status != "STARTING" || state.Confirmed != 0 {
		t.Fatal(state)
	}
	reject := quickfix.NewMessage()
	reject.Body.SetString(tag.MDReqID, id)
	reject.Body.SetString(tag.MDReqRejReason, "0")
	reject.Body.SetString(tag.Text, "Unknown ExDestination [BOARD]")
	f.adapter.app.handleMarketDataReject(reject)
	if state := f.adapter.MarketDataSubscription(); state.Status != "RETRYING" || state.Error == "" || state.NextRetryAt == nil {
		t.Fatal(state)
	}
	f.now = f.now.Add(4 * time.Second)
	_, _, _ = f.adapter.SubscribeMarketData(context.Background(), mdQuery("AAA"))
	if f.subscribeCount() != 1 {
		t.Fatal("retry before backoff")
	}
	f.now = f.now.Add(time.Second)
	_, _, _ = f.adapter.SubscribeMarketData(context.Background(), mdQuery("AAA"))
	if f.subscribeCount() != 2 {
		t.Fatal("reject did not cause automatic retry")
	}
	f.adapter.app.handleMarketDataSnapshot(mdReply(id, "AAA", "17"))
	if f.adapter.MarketDataSubscription().Confirmed != 0 {
		t.Fatal("late old reply confirmed replacement")
	}
	current := f.adapter.app.mdSubscriptions["BOARD|AAA"].reqID
	f.adapter.app.handleMarketDataSnapshot(mdReply(current, "AAA", "18"))
	if state := f.adapter.MarketDataSubscription(); state.Status != "ACTIVE" || state.Error != "" {
		t.Fatal(state)
	}
	// A closed market can be quiet for hours without a broken subscription.
	f.now = f.now.Add(4 * time.Hour)
	f.ensure(t, mdQuery("AAA"))
	if f.subscribeCount() != 2 {
		t.Fatal("healthy quiet stream was restarted")
	}
}

func TestAutomaticMDTimeoutAndEmptySnapshotConfirmation(t *testing.T) {
	f := newMDFixture()
	f.ensure(t, mdQuery("AAA"))
	f.now = f.now.Add(mdResponseTimeout)
	if _, _, err := f.adapter.SubscribeMarketData(context.Background(), mdQuery("AAA")); err == nil {
		t.Fatal("seed suppressed response timeout")
	}
	f.now = f.now.Add(5 * time.Second)
	_, _, _ = f.adapter.SubscribeMarketData(context.Background(), mdQuery("AAA"))
	id := f.adapter.app.mdSubscriptions["BOARD|AAA"].reqID
	f.adapter.app.handleMarketDataSnapshot(mdReply(id, "AAA", ""))
	if state := f.adapter.MarketDataSubscription(); state.Confirmed != 1 || state.Status != "ACTIVE" {
		t.Fatal(state)
	}
	if rows := f.adapter.LiveQuotes(); len(rows) != 1 || !rows[0].HasData() {
		t.Fatal("real empty book must count as received", rows)
	}
}

func TestAutomaticMDReconcilesDictionaryAndReconnectsWithoutBrowser(t *testing.T) {
	f := newMDFixture()
	id := f.ensure(t, mdQuery("AAA"))
	f.adapter.app.handleMarketDataSnapshot(mdReply(id, "AAA", "17"))
	f.ensure(t, mdQuery("BBB", "AAA", "AAA"))
	if f.subscribeCount() != 2 {
		t.Fatal("dictionary change restarted healthy instrument or duplicated request")
	}
	f.ensure(t, mdQuery("AAA"))
	if len(f.adapter.app.mdSubscriptions) != 1 || len(f.adapter.LiveQuotes()) != 1 {
		t.Fatal("removed instrument still subscribed")
	}
	f.adapter.app.OnLogout(quickfix.SessionID{})
	if len(f.adapter.app.mdSubscriptions) != 0 {
		t.Fatal("old session subscriptions survived")
	}
	f.adapter.app.loggedOn.Store(true)
	f.ensure(t, mdQuery("AAA"))
	if f.subscribeCount() != 3 {
		t.Fatal("reconnected session was not resubscribed")
	}
}

func TestAutomaticMDBackoffIsBounded(t *testing.T) {
	if mdRetryDelay(1) != 5*time.Second || mdRetryDelay(2) != 10*time.Second || mdRetryDelay(20) != time.Minute {
		t.Fatal("retry interval must grow and remain bounded")
	}
}

func TestReleasedMDFeedDoesNotLeaveQuotesForReactivation(t *testing.T) {
	f := newMDFixture()
	id := f.ensure(t, mdQuery("AAA"))
	f.adapter.app.handleMarketDataSnapshot(mdReply(id, "AAA", "17"))
	if err := f.adapter.UnsubscribeMarketData(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.adapter.LiveQuotes()) != 0 || len(f.adapter.lastMDQuery.Instruments) != 0 {
		t.Fatal("released feed retained stale data or reconnect query")
	}
	assertMDCancelInstrument(t, f.sent[len(f.sent)-1], id, "AAA")
	f.ensure(t, mdQuery("BBB"))
	if rows := f.adapter.LiveQuotes(); len(rows) != 1 || rows[0].SecurityID != "BBB" {
		t.Fatal("reactivated feed contains an old instrument", rows)
	}
}

func assertMDCancelInstrument(t *testing.T, msg *quickfix.Message, reqID, securityID string) {
	t.Helper()
	for key, want := range map[quickfix.Tag]string{
		tag.SubscriptionRequestType: "2", tag.MDReqID: reqID,
		tag.SecurityID: securityID, tag.ExDestination: "BOARD", tag.SecurityIDSource: "8",
	} {
		if got, _ := msg.Body.GetString(key); got != want {
			t.Fatalf("ARQA cancellation tag %d = %q, want %q", key, got, want)
		}
	}
}

func TestDictionaryReconciliationCancelsTheRemovedInstrument(t *testing.T) {
	f := newMDFixture()
	id := f.ensure(t, mdQuery("AAA"))
	f.adapter.app.handleMarketDataSnapshot(mdReply(id, "AAA", "17"))
	f.ensure(t, mdQuery("BBB"))
	if len(f.sent) != 3 {
		t.Fatal("expected subscribe AAA, unsubscribe AAA, subscribe BBB", len(f.sent))
	}
	assertMDCancelInstrument(t, f.sent[1], id, "AAA")
}

package arqa

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
)

const mdResponseTimeout = 15 * time.Second

type mdSubscription struct {
	query      domain.MarketDataQuery
	reqID      string
	sentAt     time.Time
	receivedAt time.Time
	retryAt    time.Time
	err        string
	attempts   int
	waiting    bool
}

func mdRetryDelay(attempt int) time.Duration {
	delay := 5 * time.Second
	for i := 1; i < attempt && delay < time.Minute; i++ {
		delay *= 2
	}
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}

func (a *sessionApp) marketDataNow() time.Time {
	if a.mdNow != nil {
		return a.mdNow().UTC()
	}
	return time.Now().UTC()
}

func mdDesiredSubscriptions(q domain.MarketDataQuery) map[string]domain.MarketDataQuery {
	desired := map[string]domain.MarketDataQuery{}
	list := q.Instruments
	if len(list) == 0 && strings.TrimSpace(q.Symbol) != "" {
		list = []domain.SecurityRef{{Symbol: q.Symbol, SecurityID: q.Symbol, Board: q.Board, SecurityType: q.SecurityType}}
	}
	for _, inst := range list {
		id := mdInstrumentID(inst)
		if id == "" || id == "*" {
			continue
		}
		inst.SecurityID, inst.Symbol, inst.Board = id, firstNonEmpty(inst.Symbol, id), mdInstrumentBoard(inst)
		key := inst.Board + "|" + id
		// One instrument per request makes rejects and empty-book confirmations
		// unambiguous; a failed class cannot invalidate another class's stream.
		desired[key] = domain.MarketDataQuery{Instruments: []domain.SecurityRef{inst}, SubscriptionType: "1"}
	}
	return desired
}

func mdSeed(adapterID string, q domain.MarketDataQuery) domain.MarketQuote {
	inst := firstInst(q)
	return domain.MarketQuote{AdapterID: adapterID, Symbol: firstNonEmpty(inst.Symbol, inst.SecurityID), SecurityID: mdInstrumentID(inst), Board: mdInstrumentBoard(inst), Currency: inst.Currency, Name: inst.Name}
}

func (a *sessionApp) rejectSubscriptionLocked(sub *mdSubscription, reason string, now time.Time) {
	sub.err, sub.waiting, sub.receivedAt = reason, false, time.Time{}
	sub.retryAt = now.Add(mdRetryDelay(sub.attempts))
	a.mdBook[quoteKey(mdSeed(a.adapterID, sub.query))] = mdSeed(a.adapterID, sub.query)
}

func (a *sessionApp) rejectMarketDataSubscription(reqID, reason string) bool {
	a.mdMu.Lock()
	defer a.mdMu.Unlock()
	for _, sub := range a.mdSubscriptions {
		if sub.reqID == reqID {
			a.rejectSubscriptionLocked(sub, reason, a.marketDataNow())
			return true
		}
	}
	return false
}

// confirmMarketData ignores late replies to a replaced subscription. An actual
// empty snapshot is valid; a dictionary seed alone never confirms a stream.
func (a *sessionApp) confirmMarketData(reqID string, quotes []domain.MarketQuote) []domain.MarketQuote {
	a.mdMu.Lock()
	defer a.mdMu.Unlock()
	if len(a.mdSubscriptions) == 0 {
		return quotes
	}
	now := a.marketDataNow()
	var accepted []domain.MarketQuote
	for key, sub := range a.mdSubscriptions {
		if reqID != "" && sub.reqID != reqID {
			continue
		}
		rows := quotes
		if len(rows) == 0 && reqID != "" {
			row := mdSeed(a.adapterID, sub.query)
			row.MDReqID = reqID
			rows = []domain.MarketQuote{row}
		}
		for _, row := range rows {
			if row.Board == "" && reqID != "" {
				row.Board = firstInst(sub.query).Board
			}
			if firstNonEmpty(row.SecurityID, row.Symbol) == "" && reqID != "" {
				inst := firstInst(sub.query)
				row.SecurityID, row.Symbol = inst.SecurityID, inst.Symbol
			}
			if quoteKey(row) != key {
				continue
			}
			sub.receivedAt, sub.waiting, sub.err, sub.attempts = now, false, "", 0
			sub.retryAt = time.Time{}
			accepted = append(accepted, row)
		}
	}
	return accepted
}

func (a *Adapter) MarketDataSubscription() domain.MarketDataSubscription {
	a.app.mdMu.Lock()
	defer a.app.mdMu.Unlock()
	state := domain.MarketDataSubscription{AdapterID: a.meta.ID, Status: "STARTING", Expected: len(a.app.mdSubscriptions)}
	if !a.app.isLoggedOn() {
		state.Status = "OFFLINE"
		return state
	}
	var errors []string
	for _, sub := range a.app.mdSubscriptions {
		if !sub.receivedAt.IsZero() {
			state.Confirmed++
			if state.LastReceivedAt == nil || sub.receivedAt.After(*state.LastReceivedAt) {
				t := sub.receivedAt
				state.LastReceivedAt = &t
			}
		}
		if sub.err != "" {
			errors = append(errors, sub.err)
		}
		next := sub.retryAt
		if sub.waiting {
			next = sub.sentAt.Add(mdResponseTimeout)
		}
		if !next.IsZero() && (state.NextRetryAt == nil || next.Before(*state.NextRetryAt)) {
			t := next
			state.NextRetryAt = &t
		}
	}
	sort.Strings(errors)
	unique := errors[:0]
	for _, reason := range errors {
		if len(unique) == 0 || unique[len(unique)-1] != reason {
			unique = append(unique, reason)
		}
	}
	state.Error = strings.Join(unique, "; ")
	switch {
	case state.Expected == 0:
		state.Status = "WAITING"
	case state.Confirmed == state.Expected:
		state.Status = "ACTIVE"
	case state.Confirmed > 0:
		state.Status = "PARTIAL"
	case state.Error != "":
		state.Status = "RETRYING"
	}
	return state
}

func (a *Adapter) sendMarketData(msg *quickfix.Message) error {
	if a.mdSend != nil {
		return a.mdSend(msg)
	}
	_, err := a.app.send(msg)
	return err
}

// Ensure all desired instruments remain subscribed. This operation is called
// continuously by the gateway, and is idempotent while a stream is healthy.
func (a *Adapter) ensureMarketDataSubscriptions(ctx context.Context, q domain.MarketDataQuery) (string, []domain.MarketQuote, error) {
	a.mdRequestMu.Lock()
	defer a.mdRequestMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if !a.app.isLoggedOn() {
		return "", nil, fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	desired := mdDesiredSubscriptions(q)
	if len(desired) == 0 {
		return "", nil, fmt.Errorf("market data subscription: instrument dictionary is not ready")
	}
	a.subMu.Lock()
	a.lastMDQuery = q
	a.subMu.Unlock()
	now := a.app.marketDataNow()
	a.app.mdMu.Lock()
	if a.app.mdSubscriptions == nil {
		a.app.mdSubscriptions = map[string]*mdSubscription{}
	}
	if a.app.mdSubIDs == nil {
		a.app.mdSubIDs = map[string]struct{}{}
	}
	if a.app.mdBook == nil {
		a.app.mdBook = map[string]domain.MarketQuote{}
	}
	var cancellations []mdCancellation
	for key, sub := range a.app.mdSubscriptions {
		if next, ok := desired[key]; !ok || firstInst(next).SecurityType != firstInst(sub.query).SecurityType {
			if sub.reqID != "" {
				cancellations = append(cancellations, mdCancellation{id: sub.reqID, query: sub.query})
			}
			delete(a.app.mdSubIDs, sub.reqID)
			delete(a.app.mdSubscriptions, key)
			delete(a.app.mdBook, key)
		}
	}
	var keys []string
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	type request struct {
		id    string
		query domain.MarketDataQuery
	}
	var requests []request
	var lastID string
	for _, key := range keys {
		sub := a.app.mdSubscriptions[key]
		if sub == nil {
			sub = &mdSubscription{query: desired[key]}
			a.app.mdSubscriptions[key] = sub
		}
		if sub.waiting && !now.Before(sub.sentAt.Add(mdResponseTimeout)) {
			a.app.rejectSubscriptionLocked(sub, "нет ответа FIX по "+key, now)
		}
		lastID = sub.reqID
		if !sub.receivedAt.IsZero() || sub.waiting || now.Before(sub.retryAt) {
			continue
		}
		if sub.reqID != "" {
			cancellations = append(cancellations, mdCancellation{id: sub.reqID, query: sub.query})
			delete(a.app.mdSubIDs, sub.reqID)
		}
		sub.reqID, sub.sentAt, sub.waiting = newMDReqID(), now, true
		sub.attempts++
		a.app.mdSubIDs[sub.reqID] = struct{}{}
		a.app.mdBook[key] = mdSeed(a.meta.ID, sub.query)
		requests = append(requests, request{sub.reqID, sub.query})
		lastID = sub.reqID
	}
	a.app.mdMu.Unlock()
	for _, req := range cancellations {
		if err := ctx.Err(); err != nil {
			return lastID, a.app.liveQuotes(), err
		}
		if err := a.sendMarketData(buildMDUnsubscribe(req.id, req.query)); err != nil {
			a.log.Warn("market data unsubscribe failed", "adapter_id", a.meta.ID, "error", err)
		}
	}
	for _, req := range requests {
		if err := ctx.Err(); err != nil {
			a.app.rejectMarketDataSubscription(req.id, err.Error())
			return lastID, a.app.liveQuotes(), err
		}
		if err := a.sendMarketData(buildMarketDataRequest(req.id, req.query)); err != nil {
			a.app.rejectMarketDataSubscription(req.id, err.Error())
		}
	}
	state := a.MarketDataSubscription()
	if state.Error != "" {
		return lastID, a.app.liveQuotes(), fmt.Errorf("%s", state.Error)
	}
	return lastID, a.app.liveQuotes(), nil
}

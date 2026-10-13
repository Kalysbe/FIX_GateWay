package arqa

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
)

type positionStream struct {
	reqID                         string
	started                       time.Time
	book                          map[string]domain.FIXPosition
	ready, acknowledged, rejected bool
	reports                       int
	lastReport                    time.Time
	phased                        bool
}

func (a *Adapter) SetPositionListener(fn func(string, string, []domain.FIXPosition) error) {
	a.app.onPositions = fn
}

const positionSnapshotTimeout = 60 * time.Second

func (a *sessionApp) positionNow() time.Time {
	if a.posNow != nil {
		return a.posNow().UTC()
	}
	return time.Now().UTC()
}

func (a *sessionApp) rejectPositionStreamLocked(reason string) {
	if a.posStream != nil {
		a.posStream.rejected = true
		a.posStream.ready = false
		a.posStream.started = time.Time{}
	}
	a.posError = reason
	a.posRetryAt = a.positionNow().Add(mdRetryDelay(a.posAttempts))
}

func (a *Adapter) sendPositionSubscription(msg *quickfix.Message) error {
	if a.posSend != nil {
		return a.posSend(msg)
	}
	_, err := a.app.send(msg)
	return err
}

func (a *Adapter) PositionSubscription() domain.PositionSubscription {
	a.app.posMu.Lock()
	defer a.app.posMu.Unlock()
	state := domain.PositionSubscription{AdapterID: a.meta.ID, Status: "STARTING", Mode: "stream", Error: a.app.posError}
	if !a.app.posReceived.IsZero() {
		t := a.app.posReceived
		state.LastReceivedAt = &t
	}
	if !a.app.posRetryAt.IsZero() {
		t := a.app.posRetryAt
		state.NextRetryAt = &t
	}
	if stream := a.app.posStream; stream != nil {
		state.RequestID = stream.reqID
		if stream.ready {
			state.Status, state.Positions = "ACTIVE", len(stream.book)
		}
		if !stream.ready && !stream.started.IsZero() {
			t := stream.started.Add(positionSnapshotTimeout)
			state.NextRetryAt = &t
		}
	}
	if state.Status != "ACTIVE" && state.Error != "" {
		state.Status = "RETRYING"
	}
	if !a.app.isLoggedOn() {
		state.Status = "OFFLINE"
	}
	return state
}

// EnsurePositionSubscription never polls positions: it only opens/reopens AN/263=1.
func (a *Adapter) EnsurePositionSubscription(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.app.isLoggedOn() {
		return fmt.Errorf("adapter not logged on")
	}
	a.app.posMu.Lock()
	defer a.app.posMu.Unlock()
	now := a.app.positionNow()
	if a.app.posError != "" && now.Before(a.app.posRetryAt) {
		return fmt.Errorf("%s", a.app.posError)
	}
	if s := a.app.posStream; s != nil {
		// Optional phase tags are absent in some ARQA schemas. Only the
		// initial snapshot uses a quiet period; later reports publish immediately.
		if !s.rejected && !s.ready && !s.started.IsZero() && !s.phased && s.reports > 0 && now.Sub(s.lastReport) >= 5*time.Second {
			a.log.Warn("QUIK omits snapshot phase markers; completing initial position snapshot after quiet period", "adapter_id", a.meta.ID, "req_id", s.reqID)
			s.ready = true
			if err := a.app.publishPositionStreamLocked(); err != nil {
				return err
			}
		}
		if !s.rejected && (s.ready || now.Sub(s.started) < positionSnapshotTimeout) {
			return nil
		}
		if !s.rejected {
			// An incomplete initial snapshot must never be published.
			a.app.rejectPositionStreamLocked("QUIK не завершил начальный снимок позиций за 60 секунд")
			return fmt.Errorf("%s", a.app.posError)
		}
		if err := a.sendPositionSubscription(buildRequestForPositions(s.reqID, domain.PositionQuery{SubscriptionType: "2"})); err != nil {
			a.log.Warn("position unsubscribe failed", "adapter_id", a.meta.ID, "error", err)
		}
		a.app.posStream = nil
	}
	if a.app.onPositions == nil {
		return fmt.Errorf("position listener unavailable")
	}
	reqID := newPosReqID()
	a.app.posAttempts++
	a.app.posStream = &positionStream{reqID: reqID, started: now, book: map[string]domain.FIXPosition{}}
	if err := a.sendPositionSubscription(buildRequestForPositions(reqID, domain.PositionQuery{SubscriptionType: "1"})); err != nil {
		a.app.rejectPositionStreamLocked(err.Error())
		return err
	}
	a.log.Info("position subscription requested", "adapter_id", a.meta.ID, "req_id", reqID)
	return nil
}

func (a *Adapter) UnsubscribePositions(ctx context.Context) error {
	a.app.posMu.Lock()
	defer a.app.posMu.Unlock()
	s := a.app.posStream
	a.app.posStream = nil
	if s == nil || !a.app.isLoggedOn() {
		return nil
	}
	return a.sendPositionSubscription(buildRequestForPositions(s.reqID, domain.PositionQuery{SubscriptionType: "2"}))
}

func positionKey(p domain.FIXPosition) string {
	// ARQA identifies a position by PartyIDs, PosType and every PosKey,
	// including settlement kind/date and futures limit type.
	b, _ := json.Marshal([]any{p.FirmID, p.ClientCode, p.AccountCode, p.PosType, p.Keys})
	return string(b)
}

func mergePosition(previous, next domain.FIXPosition) domain.FIXPosition {
	items := make(map[string]string, len(previous.Items)+len(next.Items))
	for k, v := range previous.Items {
		items[k] = v
	}
	for k, v := range next.Items {
		items[k] = v
	}
	next.Items = items
	next.LongQty = decFromMap(items, "CUR_LONG_POS", "START_LONG_POS", "OPEN_LONG")
	next.ShortQty = decFromMap(items, "CUR_SHORT_POS", "START_SHORT_POS", "OPEN_SHORT")
	next.NetQty = decFromMap(items, "CUR_NET_POS", "START_NET_POS", "CUR_BAL", "OPEN_BAL")
	if next.NetQty.IsZero() && (!next.LongQty.IsZero() || !next.ShortQty.IsZero()) {
		next.NetQty = next.LongQty.Sub(next.ShortQty)
	}
	next.PosAmt = decFromMap(items, "CUR_BAL", "OPEN_BAL", "CUR_LIM", "CUR_NET_POS_EVAL")
	next.SettlPrice = decFromMap(items, "AVG_POS_PRICE", "EFFECT_POS_PRICE")
	if next.Currency == "" {
		next.Currency = previous.Currency
	}
	return next
}

// Called with posMu held: FIX reports and persistence are ordered, never dropped.
func (a *sessionApp) handleStreamPositionReportLocked(msg *quickfix.Message, total int, end bool, items []domain.FIXPosition) {
	s := a.posStream
	if s == nil || s.rejected {
		return
	}
	phase, _ := msg.Body.GetString(tagStreamPhaseIndic)
	s.reports++
	s.lastReport = a.positionNow()
	if phase != "" {
		s.phased = true
	}
	if !s.ready {
		a.log.Info("position subscription initial report", "adapter_id", a.adapterID, "req_id", s.reqID, "phase", phase, "end", end, "reports", s.reports, "total", total, "items", len(items))
	}
	if phase == "OU" && !s.ready {
		a.log.Error("position update arrived before complete snapshot", "adapter_id", a.adapterID)
		a.rejectPositionStreamLocked("QUIK передал изменение позиции до завершения начального снимка")
		return
	}
	for _, p := range items {
		key := positionKey(p)
		if p.UpdateAction == "2" {
			delete(s.book, key)
			continue
		}
		if old, ok := s.book[key]; ok {
			p = mergePosition(old, p)
		}
		s.book[key] = p
	}
	wasReady := s.ready
	if end {
		s.ready = true
	}
	if !s.ready {
		return
	}
	if err := a.publishPositionStreamLocked(); err == nil && !wasReady {
		a.log.Info("position subscription snapshot received", "adapter_id", a.adapterID, "req_id", s.reqID, "positions", len(s.book))
	}
}

func (a *sessionApp) publishPositionStreamLocked() error {
	s := a.posStream
	keys := make([]string, 0, len(s.book))
	for key := range s.book {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]domain.FIXPosition, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, s.book[key])
	}
	if err := a.onPositions(a.adapterID, s.reqID, rows); err != nil {
		a.log.Error("position stream persistence failed; requesting fresh snapshot", "adapter_id", a.adapterID, "error", err)
		a.rejectPositionStreamLocked("не удалось сохранить позиции: " + err.Error())
		return err
	}
	a.posError, a.posRetryAt, a.posAttempts = "", time.Time{}, 0
	a.posReceived = a.positionNow()
	return nil
}

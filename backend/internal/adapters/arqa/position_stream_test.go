package arqa

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

func TestPositionStreamSnapshotUpdatesAndDeletion(t *testing.T) {
	a := newSessionApp("test", slog.Default(), nil)
	a.posStream = &positionStream{reqID: "stream", started: time.Now(), book: map[string]domain.FIXPosition{}}
	var saved [][]domain.FIXPosition
	a.onPositions = func(_, _ string, rows []domain.FIXPosition) error { saved = append(saved, rows); return nil }
	p := domain.FIXPosition{FirmID: "2040", ClientCode: "client", PosType: "ML", Kind: "CASH", Keys: map[string]string{"CUR": "KGS", "KIND": "T0"}, Items: map[string]string{"CUR_BAL": "10", "LOCKED": "2"}}
	report := func(phase string, rows ...domain.FIXPosition) {
		msg := quickfix.NewMessage()
		msg.Body.SetString(tagStreamPhaseIndic, phase)
		a.handleStreamPositionReportLocked(msg, 0, phase == "ES", rows)
	}
	report("PS", p)
	if len(saved) != 0 {
		t.Fatal("partial snapshot published")
	}
	other := p
	other.Keys = map[string]string{"CUR": "KGS", "KIND": "T1"}
	other.Items = map[string]string{"CUR_BAL": "20"}
	report("ES", other)
	if len(saved) != 1 || len(saved[0]) != 2 {
		t.Fatal("settlement kinds merged", saved)
	}
	p.UpdateAction = "1"
	p.Items = map[string]string{"CUR_BAL": "0"}
	report("OU", p)
	if len(saved) != 2 || len(saved[1]) != 2 {
		t.Fatal(saved)
	}
	for _, row := range saved[1] {
		if row.Keys["KIND"] == "T0" && (row.Items["LOCKED"] != "2" || !row.PosAmt.IsZero()) {
			t.Fatal("partial change lost fields or zero balance", row)
		}
	}
	p.UpdateAction = "2"
	p.Items = nil
	report("OU", p)
	if len(saved[2]) != 1 || saved[2][0].Keys["KIND"] != "T1" {
		t.Fatal("delete affected wrong settlement", saved)
	}
	other.UpdateAction = "2"
	report("OU", other)
	if len(saved[3]) != 0 {
		t.Fatal("empty snapshot not published")
	}
	a.OnLogout(quickfix.SessionID{})
	if a.posStream != nil {
		t.Fatal("old subscription retained after logout")
	}
}

func TestPositionStreamPersistenceFailureRequiresFreshSnapshot(t *testing.T) {
	a := newSessionApp("test", slog.Default(), nil)
	a.posStream = &positionStream{reqID: "stream", started: time.Now(), book: map[string]domain.FIXPosition{}}
	a.onPositions = func(string, string, []domain.FIXPosition) error { return errors.New("database unavailable") }
	msg := quickfix.NewMessage()
	msg.Body.SetString(tagStreamPhaseIndic, "ES")
	a.handleStreamPositionReportLocked(msg, 0, true, nil)
	if a.posStream.ready || !a.posStream.started.IsZero() {
		t.Fatal("failed persistence left stream live")
	}
}

func TestPositionStreamRejectAndRequestModes(t *testing.T) {
	for _, mode := range []string{"1", "2"} {
		msg := buildRequestForPositions("stream", domain.PositionQuery{SubscriptionType: mode})
		if got, _ := msg.Body.GetString(tag.SubscriptionRequestType); got != mode {
			t.Fatal(got)
		}
		if got, _ := msg.Body.GetString(tag.PosReqID); got != "stream" {
			t.Fatal(got)
		}
	}
	a := newSessionApp("test", slog.Default(), nil)
	a.posStream = &positionStream{reqID: "stream", book: map[string]domain.FIXPosition{}}
	msg := quickfix.NewMessage()
	msg.Body.SetString(tag.PosReqID, "stream")
	msg.Body.SetString(tag.PosReqResult, "1")
	a.handleRequestForPositionsAck(msg)
	if !a.posStream.rejected {
		t.Fatal("subscription rejection ignored")
	}
}

func TestPositionParserRetainsDeleteForStream(t *testing.T) {
	msg := quickfix.NewMessage()
	msg.Header.SetString(tag.MsgType, "AP")
	msg.Body.SetString(tag.PosReqID, "stream")
	msg.Body.SetString(tag.PosType, "ML")
	msg.Body.SetString(quickfix.Tag(5024), "2")
	msg.Body.SetString(quickfix.Tag(5026), "CUR")
	msg.Body.SetString(quickfix.Tag(5027), "KGS")
	_, _, _, rows := parseARQAPositionReport(msg, "test")
	if len(rows) != 1 || rows[0].UpdateAction != "2" {
		t.Fatal("delete disappeared during parsing", rows)
	}
}

func TestUnmarkedPositionStreamInitialQuietPeriod(t *testing.T) {
	a := New(domain.Adapter{ID: "test"}, slog.Default(), nil, nil, "")
	a.app.loggedOn.Store(true)
	a.app.posStream = &positionStream{reqID: "stream", started: time.Now(), lastReport: time.Now().Add(-6 * time.Second), reports: 1, book: map[string]domain.FIXPosition{}}
	saves := 0
	a.SetPositionListener(func(string, string, []domain.FIXPosition) error { saves++; return nil })
	if err := a.EnsurePositionSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !a.app.posStream.ready || saves != 1 {
		t.Fatal("unmarked snapshot never completed")
	}
	if err := a.EnsurePositionSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	if saves != 1 {
		t.Fatal("working subscription polled positions")
	}
	msg := quickfix.NewMessage()
	a.app.handleStreamPositionReportLocked(msg, 0, false, nil)
	if saves != 2 {
		t.Fatal("unmarked update was delayed")
	}
	a.app.posStream.ready = false
	a.app.posStream.phased = true
	if err := a.EnsurePositionSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.app.posStream.ready || saves != 2 {
		t.Fatal("quiet period bypassed PS/ES boundary")
	}
}

type positionFixture struct {
	adapter *Adapter
	now     time.Time
	sent    []*quickfix.Message
	saves   int
}

func newPositionFixture() *positionFixture {
	f := &positionFixture{now: time.Date(2026, 10, 7, 5, 0, 0, 0, time.UTC)}
	f.adapter = New(domain.Adapter{ID: "test"}, slog.Default(), nil, nil, "")
	f.adapter.app.loggedOn.Store(true)
	f.adapter.app.posNow = func() time.Time { return f.now }
	f.adapter.posSend = func(msg *quickfix.Message) error { f.sent = append(f.sent, msg); return nil }
	f.adapter.SetPositionListener(func(string, string, []domain.FIXPosition) error { f.saves++; return nil })
	return f
}
func (f *positionFixture) requests() int {
	count := 0
	for _, msg := range f.sent {
		if mode, _ := msg.Body.GetString(tag.SubscriptionRequestType); mode == "1" {
			count++
		}
	}
	return count
}
func positionAck(id, result, status, total string) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Body.SetString(tag.PosReqID, id)
	msg.Body.SetString(tag.PosReqResult, result)
	msg.Body.SetString(quickfix.Tag(729), status)
	msg.Body.SetString(tag.TotalNumPosReports, total)
	msg.Body.SetString(tag.Text, "upstream rejection")
	return msg
}

func TestAutomaticPositionRejectRecoveryAndQuietEmptySnapshot(t *testing.T) {
	f := newPositionFixture()
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	old := f.adapter.app.posStream.reqID
	f.adapter.app.handleRequestForPositionsAck(positionAck(old, "1", "2", "0"))
	if state := f.adapter.PositionSubscription(); state.Status != "RETRYING" || state.Error == "" || state.NextRetryAt == nil {
		t.Fatal(state)
	}
	f.now = f.now.Add(4 * time.Second)
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err == nil || f.requests() != 1 {
		t.Fatal("retry ignored backoff", err)
	}
	f.now = f.now.Add(time.Second)
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err != nil || f.requests() != 2 {
		t.Fatal("reject was permanent", err)
	}
	f.adapter.app.handleRequestForPositionsAck(positionAck(old, "0", "0", "0"))
	if f.saves != 0 {
		t.Fatal("late acknowledgement overwrote snapshot")
	}
	id := f.adapter.app.posStream.reqID
	f.adapter.app.handleRequestForPositionsAck(positionAck(id, "0", "0", "0"))
	if state := f.adapter.PositionSubscription(); state.Status != "ACTIVE" || state.Positions != 0 || state.Error != "" || f.saves != 1 {
		t.Fatal("real empty snapshot did not confirm", state)
	}
	f.now = f.now.Add(4 * time.Hour)
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err != nil || f.requests() != 2 || f.saves != 1 {
		t.Fatal("quiet complete stream was polled", err)
	}
	f.adapter.app.OnLogout(quickfix.SessionID{})
	if f.adapter.PositionSubscription().Status != "OFFLINE" {
		t.Fatal("offline data called active")
	}
	f.adapter.app.loggedOn.Store(true)
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err != nil || f.requests() != 3 {
		t.Fatal("reconnected stream not restored", err)
	}
}

func TestAutomaticPositionTimeoutNeverPublishesIncompleteSnapshot(t *testing.T) {
	f := newPositionFixture()
	_ = f.adapter.EnsurePositionSubscription(context.Background())
	msg := quickfix.NewMessage()
	msg.Body.SetString(tagStreamPhaseIndic, "PS")
	f.adapter.app.handleStreamPositionReportLocked(msg, 2, false, []domain.FIXPosition{{FirmID: "FIRM", PosType: "ML", Keys: map[string]string{"CUR": "KGS"}}})
	f.now = f.now.Add(positionSnapshotTimeout)
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err == nil || f.saves != 0 {
		t.Fatal("partial snapshot presented as complete", err)
	}
	f.now = f.now.Add(5 * time.Second)
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err != nil || f.requests() != 2 {
		t.Fatal("incomplete snapshot not retried", err)
	}
	if len(f.adapter.app.posStream.book) != 0 {
		t.Fatal("new snapshot reused incomplete positions")
	}
}

func TestAutomaticPositionSendAndPersistenceFailureAreRetried(t *testing.T) {
	f := newPositionFixture()
	f.adapter.posSend = func(*quickfix.Message) error { return errors.New("send failed") }
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err == nil || f.adapter.PositionSubscription().Status != "RETRYING" {
		t.Fatal("send failure lost", err)
	}
	f.now = f.now.Add(5 * time.Second)
	f.adapter.posSend = func(msg *quickfix.Message) error { f.sent = append(f.sent, msg); return nil }
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.adapter.SetPositionListener(func(string, string, []domain.FIXPosition) error { return errors.New("database failed") })
	f.adapter.app.handleRequestForPositionsAck(positionAck(f.adapter.app.posStream.reqID, "0", "0", "0"))
	if state := f.adapter.PositionSubscription(); state.Status != "RETRYING" || state.Error == "" {
		t.Fatal(state)
	}
	f.now = f.now.Add(10 * time.Second)
	f.adapter.SetPositionListener(func(string, string, []domain.FIXPosition) error { f.saves++; return nil })
	if err := f.adapter.EnsurePositionSubscription(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.adapter.app.handleRequestForPositionsAck(positionAck(f.adapter.app.posStream.reqID, "0", "0", "0"))
	if f.adapter.PositionSubscription().Status != "ACTIVE" {
		t.Fatal("persistence failure prevented automatic recovery")
	}
}

func TestSnapshotRejectDoesNotReturnEmptySuccess(t *testing.T) {
	f := newPositionFixture()
	f.adapter.posSend = func(msg *quickfix.Message) error {
		id, _ := msg.Body.GetString(tag.PosReqID)
		f.adapter.app.handleRequestForPositionsAck(positionAck(id, "1", "2", "0"))
		return nil
	}
	if _, rows, err := f.adapter.FetchPositions(context.Background(), domain.PositionQuery{}); err == nil || rows != nil {
		t.Fatal("rejected request became empty snapshot", err, rows)
	}
}

func TestSnapshotCancellationDoesNotReturnPartialSuccess(t *testing.T) {
	f := newPositionFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.adapter.posSend = func(msg *quickfix.Message) error {
		id, _ := msg.Body.GetString(tag.PosReqID)
		w := f.adapter.app.posWait
		w.phased = true
		w.add(2, false, []domain.FIXPosition{{FirmID: "FIRM", PosReqID: id}})
		cancel()
		return nil
	}
	if _, rows, err := f.adapter.FetchPositions(ctx, domain.PositionQuery{}); err == nil || rows != nil {
		t.Fatal("cancelled snapshot returned partial positions", err, rows)
	}
}

func TestPositionSnapshotCountsReportsAndRespectsPhaseBoundary(t *testing.T) {
	w := newPosWaiter("snapshot")
	w.add(1, false, []domain.FIXPosition{{FirmID: "A"}, {FirmID: "B"}})
	if !w.closed || len(w.snapshot()) != 2 {
		t.Fatal("total report count treated as number of positions")
	}
	w = newPosWaiter("phased")
	w.phased = true
	w.add(1, false, []domain.FIXPosition{{FirmID: "A"}})
	if w.closed {
		t.Fatal("report count bypassed phase boundary")
	}
	w.add(1, true, nil)
	if !w.closed {
		t.Fatal("snapshot end did not complete waiter")
	}
}

func TestNoMatchingPositionsIsConfirmedEmptySnapshotButNotActiveSubscription(t *testing.T) {
	f := newPositionFixture()
	f.adapter.posSend = func(msg *quickfix.Message) error {
		id, _ := msg.Body.GetString(tag.PosReqID)
		f.adapter.app.handleRequestForPositionsAck(positionAck(id, "2", "2", "0"))
		return nil
	}
	if _, rows, err := f.adapter.FetchPositions(context.Background(), domain.PositionQuery{}); err != nil || len(rows) != 0 {
		t.Fatal("no-position result was treated as connection failure", err, rows)
	}
	f.adapter.app.posStream = &positionStream{reqID: "stream", book: map[string]domain.FIXPosition{}}
	f.adapter.app.handleRequestForPositionsAck(positionAck("stream", "2", "2", "0"))
	if f.adapter.PositionSubscription().Status != "RETRYING" {
		t.Fatal("rejected stream announced active")
	}
}

func TestUnsupportedAndUnauthorizedPositionSnapshotsAreNotEmptySuccess(t *testing.T) {
	for _, result := range []string{"1", "3", "4", "99"} {
		f := newPositionFixture()
		f.adapter.posSend = func(msg *quickfix.Message) error {
			id, _ := msg.Body.GetString(tag.PosReqID)
			f.adapter.app.handleRequestForPositionsAck(positionAck(id, result, "0", "0"))
			return nil
		}
		if _, rows, err := f.adapter.FetchPositions(context.Background(), domain.PositionQuery{}); err == nil || rows != nil {
			t.Fatal("failure became empty result", result, err, rows)
		}
	}
}

package arqa

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/quickfix/store/file"
	"github.com/quickfixgo/tag"
	"github.com/shopspring/decimal"
)

// venue is a quickfixgo acceptor standing in for the ARQA FIX adapter. It
// records what the gateway sends and lets a test send reports back.
type venue struct {
	t        *testing.T
	acceptor *quickfix.Acceptor
	session  quickfix.SessionID
	mu       sync.Mutex
	received []*quickfix.Message
	logons   int
	in       chan *quickfix.Message
}

func (v *venue) OnCreate(quickfix.SessionID) {}
func (v *venue) OnLogon(quickfix.SessionID) {
	v.mu.Lock()
	v.logons++
	v.mu.Unlock()
}
func (v *venue) OnLogout(quickfix.SessionID)                       {}
func (v *venue) ToAdmin(*quickfix.Message, quickfix.SessionID)     {}
func (v *venue) ToApp(*quickfix.Message, quickfix.SessionID) error { return nil }
func (v *venue) FromAdmin(*quickfix.Message, quickfix.SessionID) quickfix.MessageRejectError {
	return nil
}
func (v *venue) FromApp(msg *quickfix.Message, _ quickfix.SessionID) quickfix.MessageRejectError {
	cp := quickfix.NewMessage()
	msg.CopyInto(cp)
	v.in <- cp
	return nil
}

func startVenue(t *testing.T, port int, dir string) *venue {
	t.Helper()
	cfg := fmt.Sprintf(`[DEFAULT]
ConnectionType=acceptor
SocketAcceptPort=%d
FileStorePath=%s
StartTime=00:00:00
EndTime=00:00:00
UseDataDictionary=N
ResetOnLogon=N

[SESSION]
BeginString=FIX.4.2
SenderCompID=QUIK
TargetCompID=GW
`, port, filepath.ToSlash(dir))
	settings, err := quickfix.ParseSettings(strings.NewReader(cfg))
	if err != nil {
		t.Fatal(err)
	}
	v := &venue{t: t, session: quickfix.SessionID{BeginString: "FIX.4.2", SenderCompID: "QUIK", TargetCompID: "GW"}, in: make(chan *quickfix.Message, 32)}
	v.acceptor, err = quickfix.NewAcceptor(v, file.NewStoreFactory(settings), settings, quickfix.NewNullLogFactory())
	if err != nil {
		t.Fatal(err)
	}
	if err := v.acceptor.Start(); err != nil {
		t.Fatal(err)
	}
	return v
}

func (v *venue) next(want enum.MsgType) *quickfix.Message {
	v.t.Helper()
	select {
	case m := <-v.in:
		if got, _ := m.MsgType(); got != string(want) {
			v.t.Fatalf("venue got %s, want %s", got, want)
		}
		return m
	case <-time.After(10 * time.Second):
		v.t.Fatalf("venue did not receive %s", want)
		return nil
	}
}

func (v *venue) send(msgType enum.MsgType, fields map[quickfix.Tag]string, header map[quickfix.Tag]string) {
	v.t.Helper()
	m := quickfix.NewMessage()
	m.Header.SetField(tag.MsgType, field.NewMsgType(msgType))
	for k, val := range header {
		m.Header.SetString(k, val)
	}
	for k, val := range fields {
		m.Body.SetString(k, val)
	}
	if err := quickfix.SendToTarget(m, v.session); err != nil {
		v.t.Fatal(err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

type reports struct {
	ch chan domain.ExecutionReport
}

func (r reports) next(t *testing.T) domain.ExecutionReport {
	t.Helper()
	select {
	case rep := <-r.ch:
		return rep
	case <-time.After(10 * time.Second):
		t.Fatal("no execution report delivered to the gateway")
		return domain.ExecutionReport{}
	}
}

func waitLoggedOn(t *testing.T, a *Adapter, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !a.app.isLoggedOn() {
		if time.Now().After(deadline) {
			t.Fatal("FIX session did not log on")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestARQASessionOrderLifecycleRejectsAndReconnect(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("FIX_TRANSPORT_CONFIG_FILE", "")
	port := freePort(t)
	venueDir := t.TempDir()
	v := startVenue(t, port, venueDir)
	got := reports{ch: make(chan domain.ExecutionReport, 32)}
	a := New(domain.Adapter{ID: "e2e", Name: "e2e", Host: "127.0.0.1", Port: port, BeginString: "FIX.4.2", SenderCompID: "GW", TargetCompID: "QUIK", HeartbeatInterval: 30},
		slog.New(slog.NewTextHandler(io.Discard, nil)), func(r domain.ExecutionReport) { got.ch <- r }, nil, t.TempDir())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())
	waitLoggedOn(t, a, 10*time.Second)

	clOrdID := "GE2E0001"
	price := decimal.NewFromInt(10)
	order := domain.Order{FixClOrdID: &clOrdID, TradingAccount: "L01-00000F00", ClientCode: "C-1", QuikFirm: "2040",
		Symbol: "SBER", Board: "TQBR", Side: domain.SideBuy, OrderType: domain.OrderTypeLimit, TimeInForce: "DAY",
		Quantity: decimal.NewFromInt(5), Price: &price}
	raw, err := a.SendOrder(context.Background(), order)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "35=D") || !strings.Contains(raw, "11="+clOrdID) {
		t.Fatalf("outbound wire not captured: %s", raw)
	}
	d := v.next(enum.MsgType_ORDER_SINGLE)
	for k, want := range map[quickfix.Tag]string{tag.ClOrdID: clOrdID, tag.Side: "1", tag.OrderQty: "5", tag.Price: "10",
		tag.SecurityID: "SBER", tag.ExDestination: "TQBR", tag.ClientID: "C-1", tag.Account: "L01-00000F00", 5018: "2040"} {
		if value, _ := d.Body.GetString(k); value != want {
			t.Errorf("35=D tag %d = %q, want %q", k, value, want)
		}
	}
	dSeq, _ := d.Header.GetString(tag.MsgSeqNum)

	er := func(execID, execType, status, last, cum, leaves string) map[quickfix.Tag]string {
		return map[quickfix.Tag]string{tag.OrderID: "261023-TQBR-1", tag.ClOrdID: clOrdID, tag.ExecID: execID,
			tag.ExecTransType: "0", tag.ExecType: execType, tag.OrdStatus: status, tag.Symbol: "SBER", tag.Side: "1",
			tag.LastShares: last, tag.LastPx: "10", tag.CumQty: cum, tag.LeavesQty: leaves, tag.AvgPx: "10", tag.TradeDate: "20261023"}
	}
	v.send(enum.MsgType_EXECUTION_REPORT, er("E-ACK", "0", "0", "0", "0", "5"), nil)
	v.send(enum.MsgType_EXECUTION_REPORT, er("E-FILL1", "1", "1", "2", "2", "3"), nil)
	ack, fill := got.next(t), got.next(t)
	if ack.ExecType != "0" || ack.ClOrdID != clOrdID || ack.OrderID != "261023-TQBR-1" || ack.RawFIX == "" {
		t.Fatalf("ack = %+v", ack)
	}
	if fill.ExecID != "E-FILL1" || fill.LastQty != "2" || fill.CumQty != "2" || fill.LeavesQty != "3" || fill.TradeDate != "20261023" {
		t.Fatalf("partial fill = %+v", fill)
	}

	// A possible duplicate keeps its ExecID and carries the original time.
	v.send(enum.MsgType_EXECUTION_REPORT, er("E-FILL1", "1", "1", "2", "2", "3"),
		map[quickfix.Tag]string{tag.PossDupFlag: "Y", tag.OrigSendingTime: "20261023-07:00:00.000"})
	dup := got.next(t)
	if dup.ExecID != "E-FILL1" || dup.OriginalTime != "20261023-07:00:00.000" {
		t.Fatalf("PossDup report = %+v", dup)
	}

	// Cancel: a new ClOrdID referencing the original; then a cancel reject.
	if err := a.CancelOrder(context.Background(), order); err != nil {
		t.Fatal(err)
	}
	f := v.next(enum.MsgType_ORDER_CANCEL_REQUEST)
	cancelID, _ := f.Body.GetString(tag.ClOrdID)
	if orig, _ := f.Body.GetString(tag.OrigClOrdID); orig != clOrdID || !strings.HasPrefix(cancelID, clOrdID+"-C") {
		t.Fatalf("35=F ClOrdID %q OrigClOrdID %q", cancelID, orig)
	}
	v.send(enum.MsgType_ORDER_CANCEL_REJECT, map[quickfix.Tag]string{tag.OrderID: "261023-TQBR-1", tag.ClOrdID: cancelID,
		tag.OrigClOrdID: clOrdID, tag.OrdStatus: "1", tag.CxlRejResponseTo: "1", tag.CxlRejReason: "0", tag.Text: "too late"}, nil)
	if rej := got.next(t); !rej.CancelReject || rej.OrigClOrdID != clOrdID || rej.Text != "too late" {
		t.Fatalf("cancel reject = %+v", rej)
	}

	// Session rejects without RefMsgType: the Logon (seq 1) is not a trading
	// message; the 35=D sequence is.
	v.send(enum.MsgType_REJECT, map[quickfix.Tag]string{tag.RefSeqNum: "1", tag.Text: "logon field"}, nil)
	v.send(enum.MsgType_REJECT, map[quickfix.Tag]string{tag.RefSeqNum: dSeq, tag.Text: "bad order"}, nil)
	if rej := got.next(t); !rej.ProtocolReject || rej.RefMessageType != "D" || rej.ClOrdID != clOrdID || rej.RefSeqNum != dSeq {
		t.Fatalf("session reject of 35=D = %+v", rej)
	}

	// Venue restart: the session logs on again with persisted sequence numbers
	// (no reset) and reports keep flowing.
	v.acceptor.Stop()
	v = startVenue(t, port, venueDir)
	defer v.acceptor.Stop()
	deadline := time.Now().Add(20 * time.Second)
	for {
		v.mu.Lock()
		logons := v.logons
		v.mu.Unlock()
		if logons > 0 && a.app.isLoggedOn() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no re-logon after venue restart")
		}
		time.Sleep(100 * time.Millisecond)
	}
	v.send(enum.MsgType_EXECUTION_REPORT, er("E-FILL2", "2", "2", "3", "5", "0"), nil)
	if final := got.next(t); final.ExecID != "E-FILL2" || final.OrdStatus != "2" {
		t.Fatalf("report after reconnect = %+v", final)
	}
	select {
	case extra := <-got.ch:
		t.Fatalf("unexpected report: %+v", extra)
	default:
	}
}

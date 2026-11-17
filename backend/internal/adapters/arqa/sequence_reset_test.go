package arqa

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

func adminMessage(msgType enum.MsgType, text string) *quickfix.Message {
	m := quickfix.NewMessage()
	m.Header.SetField(tag.MsgType, field.NewMsgType(msgType))
	m.Header.SetInt(tag.MsgSeqNum, 7)
	if text != "" {
		m.Body.SetString(tag.Text, text)
	}
	return m
}

func logonResets(a *sessionApp) bool {
	m := adminMessage(enum.MsgType_LOGON, "")
	a.ToAdmin(m, quickfix.SessionID{})
	reset, _ := m.Body.GetBool(tag.ResetSeqNumFlag)
	return reset
}

// A Logon carries 141=Y for the first logon of the day, after our "MsgSeqNum
// too low" refusal of the venue's Logon (the QUIK adapter restarted from 1),
// and after three logons that never succeeded; a successful logon clears it.
func TestLogonResetsSequenceOnlyWhenOutOfStep(t *testing.T) {
	a := newSessionApp("quik-fix-1", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if logonResets(a) {
		t.Fatal("a normal logon must keep the numbers")
	}
	a.OnLogon(quickfix.SessionID{})
	a.loggedOn.Store(false)

	a.ToAdmin(adminMessage(enum.MsgType_LOGOUT, "MsgSeqNum too low, expecting 1217 but received 1"), quickfix.SessionID{})
	if !logonResets(a) {
		t.Fatal("after the venue restarted from 1 the next logon must reset")
	}
	a.OnLogon(quickfix.SessionID{})
	if logonResets(a) {
		t.Fatal("a successful logon clears the reset")
	}

	a.OnLogon(quickfix.SessionID{}) // counter back to zero
	a.loggedOn.Store(false)
	for i := 1; i <= maxLogonsBeforeReset; i++ {
		if logonResets(a) {
			t.Fatalf("logon %d must not reset yet", i)
		}
	}
	if !logonResets(a) {
		t.Fatal("after three refused logons the next one resets")
	}

	a.OnLogon(quickfix.SessionID{})
	a.ToAdmin(adminMessage(enum.MsgType_LOGOUT, "MsgSeqNum too low"), quickfix.SessionID{})
	if logonResets(a) {
		t.Fatal("a logout while logged on is not a refused logon")
	}
}

// The venue restarts with its numbers from 1, as the QUIK adapter does every
// morning: the gateway refuses its low Logon once, then logs on with 141=Y.
func TestGatewayRecoversWhenVenueRestartsNumbers(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("FIX_TRANSPORT_CONFIG_FILE", "")
	port := freePort(t)
	v := startVenue(t, port, t.TempDir())
	a := New(domain.Adapter{ID: "e2e-reset", Name: "e2e", Host: "127.0.0.1", Port: port, BeginString: "FIX.4.2", SenderCompID: "GW", TargetCompID: "QUIK", HeartbeatInterval: 30},
		slog.New(slog.NewTextHandler(io.Discard, nil)), nil, t.TempDir())
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())
	waitLoggedOn(t, a, 10*time.Second)
	for i := 0; i < 3; i++ { // move the numbers past 1 on both sides
		v.send(enum.MsgType_TEST_REQUEST, map[quickfix.Tag]string{tag.TestReqID: "T"}, nil)
	}
	time.Sleep(300 * time.Millisecond)

	v.acceptor.Stop()
	v = startVenue(t, port, t.TempDir()) // fresh store: the venue starts from 1
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
			t.Fatal("no logon after the venue restarted its numbers")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if a.app.resetNextLogon.Load() {
		t.Fatal("the reset must be cleared after the logon")
	}
}

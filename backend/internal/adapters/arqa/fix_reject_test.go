package arqa

import (
	"log/slog"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

func TestSessionRejectWithoutRefMsgTypeResolvesBySequence(t *testing.T) {
	var got []domain.ExecutionReport
	a := newSessionApp("t", slog.Default(), func(r domain.ExecutionReport) { got = append(got, r) })
	sent := func(seq int, msgType enum.MsgType, clOrdID string) {
		msg := quickfix.NewMessage()
		msg.Header.SetField(tag.MsgType, field.NewMsgType(msgType))
		msg.Header.SetInt(tag.MsgSeqNum, seq)
		if clOrdID != "" {
			msg.Body.SetString(tag.ClOrdID, clOrdID)
		}
		a.rememberSent(msg)
	}
	sent(7, enum.MsgType_HEARTBEAT, "")
	sent(8, enum.MsgType_ORDER_SINGLE, "ORD-1")
	reject := func(seq string) {
		msg := quickfix.NewMessage()
		msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_REJECT))
		msg.Body.SetString(tag.RefSeqNum, seq)
		a.handleFIXReject(msg)
	}
	reject("7")
	if len(got) != 0 {
		t.Fatalf("reject of a heartbeat paused trading: %+v", got)
	}
	reject("8")
	if len(got) != 1 || got[0].RefMessageType != "D" || got[0].ClOrdID != "ORD-1" || !got[0].ProtocolReject {
		t.Fatalf("order reject = %+v", got)
	}
	reject("99") // unknown, e.g. sent before a restart: stays conservative
	if len(got) != 2 || got[1].RefMessageType != "" || !got[1].ProtocolReject {
		t.Fatalf("unknown reject = %+v", got)
	}
}

func TestResentOrdersDoNotAccumulateOutboundCopies(t *testing.T) {
	a := newSessionApp("t", slog.Default(), nil)
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_ORDER_SINGLE))
	msg.Header.SetBool(tag.PossDupFlag, true)
	msg.Body.SetString(tag.ClOrdID, "RESENT-1")
	if err := a.ToApp(msg, quickfix.SessionID{}); err != nil {
		t.Fatal(err)
	}
	if len(a.pendingRaw) != 0 {
		t.Fatalf("resend kept %d outbound copies", len(a.pendingRaw))
	}
}

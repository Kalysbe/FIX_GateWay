package arqa

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
	"github.com/shopspring/decimal"
)

func TestBuildPositionMaintenanceCashAndInstrument(t *testing.T) {
	qty := decimal.RequireFromString("100")
	cash := domain.PositionBlock{
		PosType:      "ML",
		UpdateAction: "1",
		Keys: map[string]string{
			"FIRMID": "2040",
			"CLIENT": "kalys",
			"CUR":    "KGS",
			"TAG":    "EQTV",
			"KIND":   "T0",
			"ACC":    "1-3101-2040",
		},
		Items: map[string]string{
			"CUR_BAL":  "1500000",
			"OPEN_BAL": "1500000",
			"CUR_LIM":  "0",
			"LOCKED":   "0",
		},
	}
	instr := domain.PositionBlock{
		PosType:      "DL",
		UpdateAction: "1",
		Keys: map[string]string{
			"SEC":    "KG0105166315",
			"BOARD":  "LISTEDSTOCKK",
			"FIRMID": "2040",
			"CLIENT": "kalys",
		},
		Items: map[string]string{
			"CUR_NET_POS":   "100",
			"CUR_LONG_POS":  "100",
			"CUR_BAL":       "100",
		},
		LongQty: qty,
	}
	msg := buildPositionMaintenanceRequest("POS1", domain.PositionMaintainRequest{
		Firm:       "2040",
		ClientCode: "kalys",
		Account:    "1-3101-2040",
		Action:     "2",
		TransType:  "3",
		Blocks:     []domain.PositionBlock{cash, instr},
	})
	wire := strings.ReplaceAll(msg.String(), "\x01", "|")
	for _, want := range []string{
		"35=AL",
		"710=POS1",
		"709=3",
		"712=2",
		"715=",
		"703=ML",
		"703=DL",
		"5024=1",
		"5026=CUR",
		"5027=KGS",
		"5026=TAG",
		"5027=EQTV",
		"5026=KIND",
		"5027=T0",
		"5029=CUR_BAL",
		"5030=1500000",
		"5026=SEC",
		"5027=KG0105166315",
		"5029=CUR_BAL",
		"448=kalys",
		"452=3",
		"448=2040",
		"452=13",
		"1=1-3101-2040",
	} {
		if !strings.Contains(wire, want) {
			t.Errorf("missing %s in %s", want, wire)
		}
	}
	if strings.Contains(wire, "704=") {
		t.Errorf("ARQA §2.33 does not use LongQty 704: %s", wire)
	}
}

func TestMaintWaiterFinishesOnSessionReject(t *testing.T) {
	a := newSessionApp("t", slog.Default(), nil)
	w := a.beginMaintWait("POS1")
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_REJECT))
	msg.Body.SetString(tag.RefMsgType, "AL")
	msg.Body.SetString(tag.Text, "Invalid MsgType")
	a.handleFIXReject(msg)
	select {
	case <-w.done:
	default:
		t.Fatal("waiter not finished")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.rejected || !strings.Contains(w.text, "Invalid MsgType") {
		t.Fatalf("rejected=%v text=%q", w.rejected, w.text)
	}
}

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

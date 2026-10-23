package arqa

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
)

// ARQA Position Report / Maintenance custom tags (HowTo §2.33).
const (
	tagPosUpdateAction quickfix.Tag = 5024
	tagNoPosKeys       quickfix.Tag = 5025
	tagPosKeyType      quickfix.Tag = 5026
	tagPosKeyValue     quickfix.Tag = 5027
	tagNoPosItems      quickfix.Tag = 5028
	tagPosItemType     quickfix.Tag = 5029
	tagPosItemValue    quickfix.Tag = 5030
)

type maintWaiter struct {
	reqID    string
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
	status   string
	result   string
	text     string
	rptID    string
	rejected bool
}

func newMaintWaiter(reqID string) *maintWaiter {
	return &maintWaiter{reqID: reqID, done: make(chan struct{})}
}

func (w *maintWaiter) finish(status, result, text, rptID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.status, w.result, w.text, w.rptID = status, result, text, rptID
	// 722 PosMaintStatus: 0 Accepted, 1 Completed, 2 Rejected
	w.rejected = status == "2" || result == "1"
	w.closed = true
	close(w.done)
}

func (a *sessionApp) beginMaintWait(reqID string) *maintWaiter {
	w := newMaintWaiter(reqID)
	a.posMu.Lock()
	a.maintWait = w
	a.posMu.Unlock()
	return w
}

func (a *sessionApp) clearMaintWait(reqID string) {
	a.posMu.Lock()
	if a.maintWait != nil && a.maintWait.reqID == reqID {
		a.maintWait = nil
	}
	a.posMu.Unlock()
}

func (a *sessionApp) handlePositionMaintenanceReport(msg *quickfix.Message) {
	get := func(t quickfix.Tag) string {
		v, err := msg.Body.GetString(t)
		if err != nil {
			return ""
		}
		return v
	}
	reqID := get(tag.PosReqID)
	status := get(tag.PosMaintStatus)
	result := get(tag.PosMaintResult)
	text := get(tag.Text)
	rptID := get(tag.PosMaintRptID)
	a.posMu.Lock()
	w := a.maintWait
	a.posMu.Unlock()
	if w == nil || (reqID != "" && w.reqID != reqID) {
		a.log.Info("position maintenance report (no waiter)",
			"adapter_id", a.adapterID, "req_id", reqID, "status", status, "result", result, "text", text)
		return
	}
	a.log.Info("position maintenance report",
		"adapter_id", a.adapterID, "req_id", reqID, "status", status, "result", result, "rpt_id", rptID)
	w.finish(status, result, text, rptID)
}

func (a *sessionApp) handleFIXReject(msg *quickfix.Message) {
	text, _ := msg.Body.GetString(tag.Text)
	refType, _ := msg.Body.GetString(tag.RefMsgType)
	if a.onExec != nil && (refType == "" || refType == "D" || refType == "F" || refType == "G") {
		seq, _ := msg.Body.GetString(tag.RefSeqNum)
		ref, _ := msg.Body.GetString(tag.BusinessRejectRefID)
		a.onExec(domain.ExecutionReport{AdapterID: a.adapterID, ProtocolReject: true, RefMessageType: refType, RefSeqNum: seq, ClOrdID: ref, Text: text, RawFIX: formatWireFIX(msg)})
	}
	a.posMu.Lock()
	mw := a.maintWait
	pw := a.posWait
	a.posMu.Unlock()
	if mw != nil {
		a.log.Warn("fix reject during position maintenance",
			"adapter_id", a.adapterID, "ref_msg_type", refType, "text", text)
		mw.finish("2", "1", firstNonEmpty(text, "FIX Reject"), "")
	}
	if pw != nil {
		pw.mu.Lock()
		pw.result, pw.status = "1", "2"
		pw.mu.Unlock()
		pw.finish()
	}
}

// buildPositionMaintenanceRequest builds ARQA Position Maintenance Request (35=AL).
// Same party layout as Request for Positions (HowTo §2.31: Client 452=3 + Firm 452=13)
// and the same PosType / PosKey / PosItem blocks as Position Report (§2.33).
func buildPositionMaintenanceRequest(reqID string, req domain.PositionMaintainRequest) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_POSITION_MAINTENANCE_REQUEST))
	msg.Body.SetString(tag.PosReqID, reqID)
	trans := req.TransType
	if trans == "" {
		trans = "3" // Position Adjustment
	}
	msg.Body.SetString(tag.PosTransType, trans)
	action := req.Action
	if action == "" {
		action = "2" // Replace — set absolute value
	}
	msg.Body.SetString(tag.PosMaintAction, action)
	biz := req.ClearingBusinessDate
	if biz == "" {
		biz = time.Now().UTC().Format("20060102")
	}
	msg.Body.SetString(tag.ClearingBusinessDate, biz)
	msg.Body.Set(field.NewTransactTimeNoMillis(time.Now().UTC()))
	if acc := strings.TrimSpace(req.Account); acc != "" {
		msg.Body.Set(field.NewAccount(acc))
	}

	if req.ClientCode != "" && req.Firm != "" {
		rg := quickfix.NewRepeatingGroup(tag.NoPartyIDs, quickfix.GroupTemplate{
			quickfix.GroupElement(tag.PartyID),
			quickfix.GroupElement(tag.PartyIDSource),
			quickfix.GroupElement(tag.PartyRole),
		})
		for _, p := range []struct{ id, role string }{
			{req.ClientCode, "3"},
			{req.Firm, "13"},
		} {
			g := rg.Add()
			g.SetString(tag.PartyID, p.id)
			g.SetString(tag.PartyIDSource, "D")
			g.SetString(tag.PartyRole, p.role)
		}
		msg.Body.SetGroup(rg)
	}

	keysT := quickfix.GroupTemplate{
		quickfix.GroupElement(tagPosKeyType),
		quickfix.GroupElement(tagPosKeyValue),
	}
	itemsT := quickfix.GroupTemplate{
		quickfix.GroupElement(tagPosItemType),
		quickfix.GroupElement(tagPosItemValue),
	}
	// §2.33 position block: 703 PosType, 5024 PosUpdateAction, 5025–5030 keys/items.
	// LongQty/ShortQty (704/705) are not in the ARQA Position Report layout.
	posRG := quickfix.NewRepeatingGroup(tag.NoPositions, quickfix.GroupTemplate{
		quickfix.GroupElement(tag.PosType),
		quickfix.GroupElement(tagPosUpdateAction),
		quickfix.NewRepeatingGroup(tagNoPosKeys, keysT),
		quickfix.NewRepeatingGroup(tagNoPosItems, itemsT),
	})
	for _, b := range req.Blocks {
		g := posRG.Add()
		g.SetString(tag.PosType, b.PosType)
		action := b.UpdateAction
		if action == "" {
			action = "1" // Change
		}
		g.SetString(tagPosUpdateAction, action)

		keyRG := quickfix.NewRepeatingGroup(tagNoPosKeys, keysT)
		for _, kv := range orderedPairs(b.Keys, posKeyOrder) {
			kg := keyRG.Add()
			kg.SetString(tagPosKeyType, kv.k)
			kg.SetString(tagPosKeyValue, kv.v)
		}
		if keyRG.Len() > 0 {
			g.SetGroup(keyRG)
		}
		itemRG := quickfix.NewRepeatingGroup(tagNoPosItems, itemsT)
		for _, kv := range orderedPairs(b.Items, posItemOrder) {
			ig := itemRG.Add()
			ig.SetString(tagPosItemType, kv.k)
			ig.SetString(tagPosItemValue, kv.v)
		}
		if itemRG.Len() > 0 {
			g.SetGroup(itemRG)
		}
	}
	if posRG.Len() > 0 {
		msg.Body.SetGroup(posRG)
	}
	return msg
}

var posKeyOrder = []string{"FIRMID", "CLIENT", "ACC", "CUR", "TAG", "KIND", "SEC", "BOARD"}
var posItemOrder = []string{"CUR_BAL", "OPEN_BAL", "CUR_LIM", "LOCKED", "CUR_NET_POS", "CUR_LONG_POS", "CUR_SHORT_POS", "AVG_POS_PRICE"}

type kv struct{ k, v string }

func orderedPairs(m map[string]string, order []string) []kv {
	if len(m) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []kv
	for _, k := range order {
		if v, ok := m[k]; ok && v != "" {
			out = append(out, kv{k, v})
			seen[k] = true
		}
	}
	for k, v := range m {
		if seen[k] || v == "" {
			continue
		}
		out = append(out, kv{k, v})
	}
	return out
}

// MaintainPositions sends 35=AL and waits for Position Maintenance Report (35=AM).
func (a *Adapter) MaintainPositions(ctx context.Context, req domain.PositionMaintainRequest) (string, error) {
	if !a.app.isLoggedOn() {
		return "", fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	if len(req.Blocks) == 0 {
		return "", fmt.Errorf("no position blocks")
	}
	if err := domain.CheckPositionMaintainText(req); err != nil {
		return "", err
	}
	reqID := newPosReqID()
	w := a.app.beginMaintWait(reqID)
	defer a.app.clearMaintWait(reqID)

	msg := buildPositionMaintenanceRequest(reqID, req)
	raw, err := a.app.send(msg)
	if err != nil {
		return reqID, err
	}
	a.log.Info("position maintenance sent",
		"adapter_id", a.meta.ID, "req_id", reqID, "firm", req.Firm, "client", req.ClientCode,
		"blocks", len(req.Blocks), "action", req.Action, "wire", raw)

	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return reqID, ctx.Err()
	case <-w.done:
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.rejected {
			text := w.text
			if text == "" {
				text = fmt.Sprintf("status=%s result=%s", w.status, w.result)
			}
			return reqID, fmt.Errorf("QUIK отклонил установку позиции: %s", text)
		}
		return reqID, nil
	case <-timer.C:
		return reqID, fmt.Errorf("ARQA FIX Adapter не ответил на 35=AL (нет 35=AM) req_id=%s. В протоколе адаптера позиции только читаются (35=AN→AP); Position Maintenance в HowTo §2.31–2.33 нет", reqID)
	}
}

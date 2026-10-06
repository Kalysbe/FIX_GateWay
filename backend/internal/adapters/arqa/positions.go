package arqa

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/enum"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
	"github.com/shopspring/decimal"
)

// ARQA custom tags for Position Report (protocol §2.33).
const (
	tagStreamPhaseIndic quickfix.Tag = 5110
)

type posWaiter struct {
	reqID    string
	mu       sync.Mutex
	items    []domain.FIXPosition
	expected int
	done     chan struct{}
	closed   bool
	ping     chan struct{}
	result   string
	status   string
}

func newPosWaiter(reqID string) *posWaiter {
	return &posWaiter{
		reqID: reqID,
		done:  make(chan struct{}),
		ping:  make(chan struct{}, 32),
	}
}

func (w *posWaiter) add(totalHint int, endSnapshot bool, batch []domain.FIXPosition) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	if totalHint > 0 && w.expected == 0 {
		w.expected = totalHint
	}
	w.items = append(w.items, batch...)
	select {
	case w.ping <- struct{}{}:
	default:
	}
	if endSnapshot || (w.expected > 0 && len(w.items) >= w.expected) {
		w.finishLocked()
	}
}

func (w *posWaiter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finishLocked()
}

func (w *posWaiter) finishLocked() {
	if w.closed {
		return
	}
	w.closed = true
	close(w.done)
}

func (w *posWaiter) snapshot() []domain.FIXPosition {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]domain.FIXPosition, len(w.items))
	copy(out, w.items)
	return out
}

func (a *sessionApp) beginPosWait(reqID string) *posWaiter {
	w := newPosWaiter(reqID)
	a.posMu.Lock()
	a.posWait = w
	a.posMu.Unlock()
	return w
}

func (a *sessionApp) clearPosWait(reqID string) {
	a.posMu.Lock()
	if a.posWait != nil && a.posWait.reqID == reqID {
		a.posWait = nil
	}
	a.posMu.Unlock()
}

func (a *sessionApp) handlePositionReport(msg *quickfix.Message) {
	reqID, total, endSnap, items := parseARQAPositionReport(msg, a.adapterID)
	a.posMu.Lock()
	w := a.posWait
	a.posMu.Unlock()
	if w == nil || (reqID != "" && w.reqID != reqID) {
		a.log.Debug("position report (no waiter)", "adapter_id", a.adapterID, "req_id", reqID, "items", len(items))
		return
	}
	a.log.Debug("position report", "adapter_id", a.adapterID, "req_id", reqID, "batch", len(items), "total", total, "end", endSnap)
	w.add(total, endSnap, items)
}

func (a *sessionApp) handleRequestForPositionsAck(msg *quickfix.Message) {
	get := func(t quickfix.Tag) string {
		v, err := msg.Body.GetString(t)
		if err != nil {
			return ""
		}
		return v
	}
	reqID := get(tag.PosReqID)
	result := get(tag.PosReqResult)
	status := get(quickfix.Tag(729)) // PosReqStatus
	total := 0
	if n := get(tag.TotalNumPosReports); n != "" {
		total, _ = strconv.Atoi(n)
	}
	a.posMu.Lock()
	w := a.posWait
	a.posMu.Unlock()
	if w == nil || (reqID != "" && w.reqID != reqID) {
		a.log.Debug("positions ack (no waiter)", "adapter_id", a.adapterID, "req_id", reqID, "result", result, "status", status)
		return
	}
	w.mu.Lock()
	w.result = result
	w.status = status
	if total > 0 {
		w.expected = total
	}
	w.mu.Unlock()
	a.log.Debug("positions ack", "adapter_id", a.adapterID, "req_id", reqID, "result", result, "status", status, "total", total)

	// ARQA: result 1 = invalid; status 2 = rejected; status 0 + total 0 = completed empty
	if result == "1" || status == "2" {
		w.finish()
		return
	}
	if status == "0" && total == 0 {
		w.finish()
	}
}

var posReqSeq atomic.Uint64

func newPosReqID() string {
	n := posReqSeq.Add(1)
	return fmt.Sprintf("POS%d-%d", time.Now().UTC().Unix()%1_000_000, n)
}

// buildRequestForPositions builds ARQA Request for Positions (35=AN).
// HowTo example (client 5253 + firm BrokerQuote):
//
//	35=AN|710=…|263=0|448=5253|447=D|452=3|453=2|724=0|448=BrokerQuote|447=D|452=13|
//
// Snapshot of all clients: 35=AN|710=…|724=0|263=0|
func buildRequestForPositions(reqID string, q domain.PositionQuery) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_REQUEST_FOR_POSITIONS))
	msg.Body.SetString(tag.PosReqID, reqID)
	msg.Body.SetString(tag.PosReqType, "0") // Positions
	sub := q.SubscriptionType
	if sub == "" {
		sub = "0" // SNAPSHOT
	}
	msg.Body.SetString(tag.SubscriptionRequestType, sub)

	type party struct{ id, role string }
	var parties []party
	// Doc: if NoPartyIDs is used, value is 2 — typically Client(3) + Firm(13).
	if q.ClientCode != "" && q.Firm != "" {
		parties = append(parties,
			party{q.ClientCode, "3"},
			party{q.Firm, "13"},
		)
	} else if q.Account != "" && q.Firm != "" {
		// Futures: trading account (38) + firm (13)
		parties = append(parties,
			party{q.Account, "38"},
			party{q.Firm, "13"},
		)
	}

	if len(parties) == 2 {
		rg := quickfix.NewRepeatingGroup(tag.NoPartyIDs, quickfix.GroupTemplate{
			quickfix.GroupElement(tag.PartyID),
			quickfix.GroupElement(tag.PartyIDSource),
			quickfix.GroupElement(tag.PartyRole),
		})
		for _, p := range parties {
			g := rg.Add()
			g.SetString(tag.PartyID, p.id)
			g.SetString(tag.PartyIDSource, "D")
			g.SetString(tag.PartyRole, p.role)
		}
		msg.Body.SetGroup(rg)
	}
	return msg
}

// FetchPositions sends Request For Positions (35=AN) and collects Position Report (35=AP).
func (a *Adapter) FetchPositions(ctx context.Context, q domain.PositionQuery) (string, []domain.FIXPosition, error) {
	if !a.app.isLoggedOn() {
		return "", nil, fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	reqID := newPosReqID()
	w := a.app.beginPosWait(reqID)
	defer a.app.clearPosWait(reqID)

	msg := buildRequestForPositions(reqID, q)
	if _, err := a.app.send(msg); err != nil {
		return reqID, nil, err
	}
	a.log.Debug("request for positions sent",
		"adapter_id", a.meta.ID, "req_id", reqID, "firm", q.Firm, "client", q.ClientCode, "account", q.Account)

	hardDeadline := time.NewTimer(60 * time.Second)
	defer hardDeadline.Stop()
	idle := time.NewTimer(5 * time.Second)
	idle.Stop()
	gotAny := false

	for {
		select {
		case <-ctx.Done():
			items := w.snapshot()
			if len(items) == 0 {
				return reqID, nil, ctx.Err()
			}
			return reqID, items, nil
		case <-w.done:
			return reqID, w.snapshot(), nil
		case <-w.ping:
			gotAny = true
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(5 * time.Second)
		case <-idle.C:
			if gotAny {
				w.finish()
				return reqID, w.snapshot(), nil
			}
		case <-hardDeadline.C:
			items := w.snapshot()
			if len(items) == 0 {
				w.mu.Lock()
				res, st := w.result, w.status
				w.mu.Unlock()
				if res == "1" || st == "2" {
					return reqID, nil, fmt.Errorf("position request rejected (result=%s status=%s)", res, st)
				}
				if st == "0" {
					return reqID, nil, nil
				}
				return reqID, nil, fmt.Errorf("position request timeout on %s (req_id=%s result=%s status=%s)", a.meta.ID, reqID, res, st)
			}
			a.log.Warn("position request hard timeout", "adapter_id", a.meta.ID, "count", len(items))
			return reqID, items, nil
		}
	}
}

func parseARQAPositionReport(msg *quickfix.Message, adapterID string) (reqID string, total int, endSnapshot bool, items []domain.FIXPosition) {
	raw := msg.String()
	get := func(t quickfix.Tag) string {
		v, err := msg.Body.GetString(t)
		if err != nil {
			return ""
		}
		return v
	}
	reqID = get(tag.PosReqID)
	if n := get(tag.TotalNumPosReports); n != "" {
		total, _ = strconv.Atoi(n)
	}
	phase := firstNonEmpty(get(tagStreamPhaseIndic), getFieldFromRaw(raw, "5110"))
	endSnapshot = phase == "ES" // last message in snapshot
	if strings.EqualFold(get(tag.LastRptRequested), "Y") {
		endSnapshot = true
	}

	firm, client, account := parsePartiesFromRaw(raw)
	clearing := get(tag.ClearingBusinessDate)
	rptID := get(tag.PosMaintRptID)
	wire := formatWireFIX(msg)

	for _, block := range parsePosBlocksFromRaw(raw) {
		kind := kindFromPosType(block.posType)
		p := domain.FIXPosition{
			AdapterID:     adapterID,
			PosMaintRptID: rptID,
			PosReqID:      reqID,
			ClearingDate:  clearing,
			Kind:          kind,
			PosType:       block.posType,
			UpdateAction:  block.updateAction,
			FirmID:        firstNonEmpty(firm, block.keys["FIRMID"]),
			ClientCode:    firstNonEmpty(client, block.keys["CLIENT"]),
			AccountCode:   firstNonEmpty(account, block.keys["ACC"]),
			Symbol:        firstNonEmpty(block.keys["SEC"], block.keys["TAG"]),
			SecurityID:    block.keys["SEC"],
			Currency:      block.keys["CUR"],
			Board:         block.keys["BOARD"],
			Keys:          block.keys,
			Items:         block.items,
			RawFIX:        wire,
		}
		// Flatten common numeric fields for list UI
		p.LongQty = decFromMap(block.items, "CUR_LONG_POS", "START_LONG_POS", "OPEN_LONG")
		p.ShortQty = decFromMap(block.items, "CUR_SHORT_POS", "START_SHORT_POS", "OPEN_SHORT")
		p.NetQty = decFromMap(block.items, "CUR_NET_POS", "START_NET_POS", "CUR_BAL", "OPEN_BAL")
		if p.NetQty.IsZero() && (!p.LongQty.IsZero() || !p.ShortQty.IsZero()) {
			p.NetQty = p.LongQty.Sub(p.ShortQty)
		}
		p.PosAmt = decFromMap(block.items, "CUR_BAL", "OPEN_BAL", "CUR_LIM", "CUR_NET_POS_EVAL")
		p.SettlPrice = decFromMap(block.items, "AVG_POS_PRICE", "EFFECT_POS_PRICE")
		if p.Currency == "" {
			p.Currency = firstNonEmpty(block.items["AVG_POS_PRICE_CCY"], block.keys["CUR"])
		}
		if block.updateAction == "2" { // Delete
			continue
		}
		items = append(items, p)
	}
	return reqID, total, endSnapshot, items
}

func kindFromPosType(posType string) string {
	switch strings.ToUpper(posType) {
	case "ML":
		return "CASH"
	case "DL":
		return "INSTRUMENT"
	case "FL", "FP":
		return "FUTURES"
	default:
		if posType == "" {
			return "INSTRUMENT"
		}
		return posType
	}
}

type posBlock struct {
	posType      string
	updateAction string
	keys         map[string]string
	items        map[string]string
}

func parsePosBlocksFromRaw(raw string) []posBlock {
	parts := strings.Split(strings.ReplaceAll(raw, "|", "\x01"), "\x01")
	var out []posBlock
	var cur *posBlock
	flush := func() {
		if cur == nil {
			return
		}
		if cur.posType == "" && len(cur.keys) == 0 && len(cur.items) == 0 {
			cur = nil
			return
		}
		out = append(out, *cur)
		cur = nil
	}
	ensure := func() {
		if cur == nil {
			cur = &posBlock{keys: map[string]string{}, items: map[string]string{}}
		}
	}

	var pendingKeyType, pendingItemType string
	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		switch k {
		case "703": // PosType — start of a position block
			flush()
			ensure()
			cur.posType = v
		case "5024": // PosUpdateAction
			ensure()
			cur.updateAction = v
		case "5026": // PosKeyType
			pendingKeyType = v
		case "5027": // PosKeyValue
			ensure()
			if pendingKeyType != "" {
				cur.keys[pendingKeyType] = v
				pendingKeyType = ""
			}
		case "5029": // PosItemType
			pendingItemType = v
		case "5030": // PosItemValue
			ensure()
			if pendingItemType != "" {
				cur.items[pendingItemType] = v
				pendingItemType = ""
			}
		}
	}
	flush()
	return out
}

func parsePartiesFromRaw(raw string) (firm, client, account string) {
	parts := strings.Split(strings.ReplaceAll(raw, "|", "\x01"), "\x01")
	var id, role string
	flush := func() {
		switch role {
		case "13":
			firm = id
		case "3":
			client = id
		case "37", "38":
			account = id
		}
		id, role = "", ""
	}
	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		switch k {
		case "448":
			if id != "" || role != "" {
				flush()
			}
			id = v
		case "452":
			role = v
			flush()
		}
	}
	return firm, client, account
}

func getFieldFromRaw(raw, tagNum string) string {
	for _, p := range strings.Split(strings.ReplaceAll(raw, "|", "\x01"), "\x01") {
		k, v, ok := strings.Cut(p, "=")
		if ok && k == tagNum {
			return v
		}
	}
	return ""
}

func decFromMap(m map[string]string, keys ...string) decimal.Decimal {
	for _, k := range keys {
		if v, ok := m[k]; ok && strings.TrimSpace(v) != "" {
			d, err := decimal.NewFromString(strings.TrimSpace(v))
			if err == nil {
				return d
			}
		}
	}
	return decimal.Zero
}

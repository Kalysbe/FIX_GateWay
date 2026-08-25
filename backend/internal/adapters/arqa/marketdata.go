package arqa

import (
	"context"
	"fmt"
	"sort"
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
)

// Current-trades MDEntryTypes (QUIK «Текущие торги», not Level II book).
var currentTradesEntryTypes = []string{
	"0", // bid
	"1", // offer
	"2", // last / trade
	"4", // open
	"5", // close
	"6", // settle
	"7", // high
	"8", // low
	"9", // waprice
	"o", // Current Price
	"O", // Official Current Price
}

type mdWaiter struct {
	reqID  string
	mu     sync.Mutex
	byKey  map[string]domain.MarketQuote
	done   chan struct{}
	closed bool
	ping   chan struct{}
	err    string
}

func newMDWaiter(reqID string) *mdWaiter {
	return &mdWaiter{
		reqID: reqID,
		byKey: map[string]domain.MarketQuote{},
		done:  make(chan struct{}),
		ping:  make(chan struct{}, 64),
	}
}

func (w *mdWaiter) add(quotes []domain.MarketQuote) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	for _, q := range quotes {
		key := quoteKey(q)
		if key == "|" || key == "" {
			continue
		}
		if prev, ok := w.byKey[key]; ok {
			w.byKey[key] = mergeQuote(prev, q)
		} else {
			w.byKey[key] = q
		}
	}
	select {
	case w.ping <- struct{}{}:
	default:
	}
}

func (w *mdWaiter) reject(text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.err = text
	w.finishLocked()
}

func (w *mdWaiter) finish() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.finishLocked()
}

func (w *mdWaiter) finishLocked() {
	if w.closed {
		return
	}
	w.closed = true
	close(w.done)
}

func (w *mdWaiter) snapshot() ([]domain.MarketQuote, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]domain.MarketQuote, 0, len(w.byKey))
	for _, q := range w.byKey {
		out = append(out, q)
	}
	sortQuotes(out)
	return out, w.err
}

func (a *sessionApp) beginMDWait(reqID string) *mdWaiter {
	w := newMDWaiter(reqID)
	a.mdMu.Lock()
	a.mdWait = w
	a.mdMu.Unlock()
	return w
}

func (a *sessionApp) clearMDWait(reqID string) {
	a.mdMu.Lock()
	if a.mdWait != nil && a.mdWait.reqID == reqID {
		a.mdWait = nil
	}
	a.mdMu.Unlock()
}

func (a *sessionApp) handleMarketDataSnapshot(msg *quickfix.Message) {
	reqID, quotes := parseMarketDataSnapshot(msg, a.adapterID)
	a.applyLive(quotes)
	a.mdMu.Lock()
	w := a.mdWait
	a.mdMu.Unlock()
	if w != nil && (reqID == "" || w.reqID == reqID) {
		w.add(quotes)
	}
}

func (a *sessionApp) handleMarketDataIncremental(msg *quickfix.Message) {
	reqID, quotes := parseMarketDataIncremental(msg, a.adapterID)
	a.applyLive(quotes)
	a.mdMu.Lock()
	w := a.mdWait
	a.mdMu.Unlock()
	if w != nil && (reqID == "" || w.reqID == reqID) {
		w.add(quotes)
	}
}

func (a *sessionApp) handleMarketDataReject(msg *quickfix.Message) {
	get := func(t quickfix.Tag) string {
		v, err := msg.Body.GetString(t)
		if err != nil {
			return ""
		}
		return v
	}
	reqID := get(tag.MDReqID)
	reason := get(tag.MDReqRejReason)
	text := get(tag.Text)
	a.mdMu.Lock()
	w := a.mdWait
	a.mdMu.Unlock()
	if w == nil || (reqID != "" && w.reqID != reqID) {
		a.log.Warn("market data reject (no waiter)", "adapter_id", a.adapterID, "req_id", reqID, "reason", reason, "text", text)
		return
	}
	msgText := text
	if msgText == "" {
		msgText = "Market Data Request Reject"
	}
	if reason != "" {
		msgText = fmt.Sprintf("%s (reason=%s)", msgText, reason)
	}
	a.log.Warn("market data reject", "adapter_id", a.adapterID, "req_id", reqID, "text", msgText)
	w.reject(msgText)
}

var mdReqSeq atomic.Uint64

func newMDReqID() string {
	n := mdReqSeq.Add(1)
	return fmt.Sprintf("MD%d-%d", time.Now().UTC().Unix()%1_000_000, n)
}

// buildMarketDataRequest builds ARQA Market Data Request (35=V) for «Текущие торги».
// HowTo example (one instrument LKOH / BQUOTE, bid+offer, snapshot+updates):
//
//	35=V|22=8|48=LKOH|100=BQUOTE|262=…|263=1|264=0|265=0|267=2|269=1|269=0|
//
// Snapshot of current trades (top of book): 263=0, 264=1, entry types 0/1/2/4/5/7/8/9/o/O.
func buildMarketDataRequest(reqID string, q domain.MarketDataQuery) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_MARKET_DATA_REQUEST))
	msg.Body.SetString(tag.MDReqID, reqID)
	sub := q.SubscriptionType
	if sub == "" {
		sub = "0" // SNAPSHOT
	}
	msg.Body.SetString(tag.SubscriptionRequestType, sub)
	msg.Body.SetString(tag.MarketDepth, "1") // Top of Book
	if sub == "1" {
		// Incremental (1): 35=X on each change of last/bid/offer/volume — not a delayed full snapshot.
		msg.Body.SetString(tag.MDUpdateType, "1")
	}

	types := currentTradesEntryTypes
	rg := quickfix.NewRepeatingGroup(tag.NoMDEntryTypes, quickfix.GroupTemplate{
		quickfix.GroupElement(tag.MDEntryType),
	})
	for _, t := range types {
		g := rg.Add()
		g.SetString(tag.MDEntryType, t)
	}
	msg.Body.SetGroup(rg)

	switch {
	case len(q.Instruments) > 0:
		symRG := quickfix.NewRepeatingGroup(tag.NoRelatedSym, quickfix.GroupTemplate{
			quickfix.GroupElement(tag.SecurityIDSource),
			quickfix.GroupElement(tag.SecurityID),
			quickfix.GroupElement(tag.ExDestination),
		})
		for _, inst := range q.Instruments {
			sym := firstNonEmpty(inst.SecurityID, inst.Symbol)
			if sym == "" {
				continue
			}
			board := strings.TrimSpace(inst.Board)
			if strings.EqualFold(board, "UNKNOWN") {
				board = ""
			}
			g := symRG.Add()
			g.SetString(tag.SecurityIDSource, string(enum.SecurityIDSource_EXCHANGE_SYMBOL)) // 8
			g.SetString(tag.SecurityID, sym)
			if board != "" {
				g.SetString(tag.ExDestination, board)
			}
		}
		if symRG.Len() > 0 {
			msg.Body.SetGroup(symRG)
		}
	case q.Symbol != "":
		// HowTo: instrument tags at body level (no 146).
		msg.Body.SetString(tag.SecurityIDSource, string(enum.SecurityIDSource_EXCHANGE_SYMBOL))
		msg.Body.SetString(tag.SecurityID, q.Symbol)
		if q.Board != "" {
			msg.Body.SetString(tag.ExDestination, q.Board)
		}
	}

	return msg
}

const mdInstrumentBatch = 30

// FetchMarketData sends Market Data Request (35=V) and collects Snapshot (35=W)
// / Incremental (35=X) until idle, or Reject (35=Y).
func (a *Adapter) FetchMarketData(ctx context.Context, q domain.MarketDataQuery) (string, []domain.MarketQuote, error) {
	if len(q.Instruments) <= mdInstrumentBatch {
		return a.fetchMarketDataOnce(ctx, q)
	}
	merged := map[string]domain.MarketQuote{}
	var lastID string
	var lastErr error
	for i := 0; i < len(q.Instruments); i += mdInstrumentBatch {
		end := i + mdInstrumentBatch
		if end > len(q.Instruments) {
			end = len(q.Instruments)
		}
		batch := q
		batch.Instruments = q.Instruments[i:end]
		reqID, items, err := a.fetchMarketDataOnce(ctx, batch)
		lastID = reqID
		if err != nil {
			lastErr = err
			if len(merged) == 0 && i == 0 {
				return reqID, nil, err
			}
			continue
		}
		for _, item := range items {
			key := quoteKey(item)
			if prev, ok := merged[key]; ok {
				merged[key] = mergeQuote(prev, item)
			} else {
				merged[key] = item
			}
		}
	}
	out := make([]domain.MarketQuote, 0, len(merged))
	for _, q := range merged {
		out = append(out, q)
	}
	if len(out) == 0 && lastErr != nil {
		return lastID, nil, lastErr
	}
	return lastID, out, nil
}

func (a *Adapter) fetchMarketDataOnce(ctx context.Context, q domain.MarketDataQuery) (string, []domain.MarketQuote, error) {
	if !a.app.isLoggedOn() {
		return "", nil, fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	reqID := newMDReqID()
	w := a.app.beginMDWait(reqID)
	defer a.app.clearMDWait(reqID)

	msg := buildMarketDataRequest(reqID, q)
	if _, err := a.app.send(msg); err != nil {
		return reqID, nil, err
	}
	a.log.Info("market data request sent",
		"adapter_id", a.meta.ID, "req_id", reqID, "symbol", q.Symbol, "board", q.Board, "instruments", len(q.Instruments))

	hardDeadline := time.NewTimer(45 * time.Second)
	defer hardDeadline.Stop()
	idle := time.NewTimer(4 * time.Second)
	idle.Stop()
	gotAny := false

	for {
		select {
		case <-ctx.Done():
			items, rej := w.snapshot()
			if rej != "" && len(items) == 0 {
				return reqID, nil, fmt.Errorf("%s", rej)
			}
			if len(items) == 0 {
				return reqID, nil, ctx.Err()
			}
			return reqID, items, nil
		case <-w.done:
			items, rej := w.snapshot()
			if rej != "" && len(items) == 0 {
				return reqID, nil, fmt.Errorf("%s", rej)
			}
			return reqID, items, nil
		case <-w.ping:
			gotAny = true
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(4 * time.Second)
		case <-idle.C:
			if gotAny {
				w.finish()
				items, rej := w.snapshot()
				if rej != "" && len(items) == 0 {
					return reqID, nil, fmt.Errorf("%s", rej)
				}
				return reqID, items, nil
			}
		case <-hardDeadline.C:
			items, rej := w.snapshot()
			if rej != "" && len(items) == 0 {
				return reqID, nil, fmt.Errorf("%s", rej)
			}
			if len(items) == 0 {
				return reqID, nil, fmt.Errorf("market data timeout on %s (req_id=%s)", a.meta.ID, reqID)
			}
			a.log.Warn("market data hard timeout", "adapter_id", a.meta.ID, "count", len(items))
			return reqID, items, nil
		}
	}
}

func parseMarketDataSnapshot(msg *quickfix.Message, adapterID string) (string, []domain.MarketQuote) {
	return parseMarketDataFromRaw(msg.String(), adapterID, false)
}

func parseMarketDataIncremental(msg *quickfix.Message, adapterID string) (string, []domain.MarketQuote) {
	return parseMarketDataFromRaw(msg.String(), adapterID, true)
}

type mdEntry struct {
	typ, px, size, tm, action           string
	symbol, securityID, board, currency string
	volume, value, tradeVol             string
}

func parseMarketDataFromRaw(raw, adapterID string, incremental bool) (reqID string, quotes []domain.MarketQuote) {
	parts := strings.Split(strings.ReplaceAll(raw, "|", "\x01"), "\x01")
	base := domain.MarketQuote{AdapterID: adapterID, Entries: map[string]string{}}
	var entries []mdEntry
	var cur *mdEntry
	inEntries := false

	flushEntry := func() {
		if cur == nil {
			return
		}
		if cur.typ != "" || cur.px != "" {
			entries = append(entries, *cur)
		}
		cur = nil
	}

	for _, p := range parts {
		k, v, ok := strings.Cut(p, "=")
		if !ok || v == "" {
			continue
		}
		switch k {
		case "262":
			reqID = v
			base.MDReqID = v
		case "75":
			base.TradeDate = v
		case "326":
			base.TradingStatus = v
		case "336":
			base.TradingSessionID = v
		case "625":
			base.TradingSessionSub = v
		case "387":
			if inEntries && cur != nil {
				cur.volume = v
			} else {
				base.Volume = v
			}
		case "6143":
			if inEntries && cur != nil {
				cur.value = v
			} else {
				base.Value = v
			}
		case "1020": // TradeVolume
			if cur != nil {
				cur.tradeVol = v
			}
		case "15":
			if !inEntries || cur == nil {
				base.Currency = v
			} else {
				cur.currency = v
			}
		case "55":
			if inEntries && cur != nil {
				cur.symbol = v
			} else {
				base.Symbol = v
			}
		case "48":
			if inEntries && cur != nil {
				cur.securityID = v
			} else {
				base.SecurityID = v
			}
		case "100", "207":
			if inEntries && cur != nil {
				cur.board = v
			} else if base.Board == "" {
				base.Board = v
			}
		case "107":
			base.Name = v
		case "268": // NoMDEntries
			inEntries = true
		case "279": // MDUpdateAction — start of incremental entry
			flushEntry()
			inEntries = true
			cur = &mdEntry{action: v}
		case "269": // MDEntryType
			if incremental && cur != nil && cur.typ == "" {
				cur.typ = v
			} else {
				flushEntry()
				inEntries = true
				cur = &mdEntry{typ: v}
			}
		case "270":
			if cur != nil {
				cur.px = v
			}
		case "271":
			if cur != nil {
				cur.size = v
			}
		case "273":
			if cur != nil {
				cur.tm = v
			}
		}
	}
	flushEntry()

	type bucket struct {
		q       domain.MarketQuote
		entries []mdEntry
	}
	groups := map[string]*bucket{}
	ensure := func(sym, sec, board string) *bucket {
		q := base
		q.Entries = cloneStrMap(base.Entries)
		if sec != "" {
			q.SecurityID = sec
		}
		if sym != "" {
			q.Symbol = sym
		}
		if board != "" {
			q.Board = board
		}
		if q.Symbol == "" {
			q.Symbol = q.SecurityID
		}
		if q.SecurityID == "" {
			q.SecurityID = q.Symbol
		}
		key := q.Board + "|" + firstNonEmpty(q.SecurityID, q.Symbol)
		if b, ok := groups[key]; ok {
			return b
		}
		b := &bucket{q: q}
		groups[key] = b
		return b
	}

	if len(entries) == 0 {
		if firstNonEmpty(base.SecurityID, base.Symbol) != "" {
			quotes = append(quotes, base)
		}
		return reqID, quotes
	}

	for _, e := range entries {
		if e.action == "2" { // Delete
			continue
		}
		b := ensure(e.symbol, e.securityID, e.board)
		if e.currency != "" {
			b.q.Currency = e.currency
		}
		if e.volume != "" {
			b.q.Volume = e.volume
		}
		if e.value != "" {
			b.q.Value = e.value
		}
		size := firstNonEmpty(e.size, e.tradeVol)
		applyMDEntry(&b.q, e.typ, e.px, size, e.tm)
	}

	for _, b := range groups {
		b.q.RawFIX = strings.ReplaceAll(strings.TrimRight(raw, "\x01"), "\x01", "|")
		if !strings.HasSuffix(b.q.RawFIX, "|") && b.q.RawFIX != "" {
			b.q.RawFIX += "|"
		}
		quotes = append(quotes, b.q)
	}
	return reqID, quotes
}

func applyMDEntry(q *domain.MarketQuote, typ, px, size, tm string) {
	if q.Entries == nil {
		q.Entries = map[string]string{}
	}
	if typ != "" && px != "" {
		q.Entries[typ] = px
	}
	switch typ {
	case "0":
		q.Bid, q.BidSize = firstNonEmpty(px, q.Bid), firstNonEmpty(size, q.BidSize)
	case "1":
		q.Offer, q.OfferSize = firstNonEmpty(px, q.Offer), firstNonEmpty(size, q.OfferSize)
	case "2":
		q.Last, q.LastSize, q.LastTime = firstNonEmpty(px, q.Last), firstNonEmpty(size, q.LastSize), firstNonEmpty(tm, q.LastTime)
		if size != "" {
			q.DeltaVolume = addNum(q.DeltaVolume, size)
			if px != "" {
				q.DeltaValue = addNum(q.DeltaValue, mulNum(px, size))
			}
		}
	case "4":
		q.Open = firstNonEmpty(px, q.Open)
	case "5":
		q.Close = firstNonEmpty(px, q.Close)
	case "6":
		q.Settle = firstNonEmpty(px, q.Settle)
	case "7":
		q.High = firstNonEmpty(px, q.High)
	case "8":
		q.Low = firstNonEmpty(px, q.Low)
	case "9":
		q.WAPR = firstNonEmpty(px, q.WAPR)
	case "o":
		q.CurrentPrice = firstNonEmpty(px, q.CurrentPrice)
	case "O":
		q.OfficialPrice = firstNonEmpty(px, q.OfficialPrice)
	}
}

func mergeQuote(dst, src domain.MarketQuote) domain.MarketQuote {
	out := dst
	out.AdapterID = firstNonEmpty(src.AdapterID, out.AdapterID)
	out.MDReqID = firstNonEmpty(src.MDReqID, out.MDReqID)
	out.Symbol = firstNonEmpty(src.Symbol, out.Symbol)
	out.SecurityID = firstNonEmpty(src.SecurityID, out.SecurityID)
	out.Board = firstNonEmpty(src.Board, out.Board)
	out.Currency = firstNonEmpty(src.Currency, out.Currency)
	out.Name = firstNonEmpty(src.Name, out.Name)
	out.TradeDate = firstNonEmpty(src.TradeDate, out.TradeDate)
	out.TradingStatus = firstNonEmpty(src.TradingStatus, out.TradingStatus)
	out.TradingSessionID = firstNonEmpty(src.TradingSessionID, out.TradingSessionID)
	out.TradingSessionSub = firstNonEmpty(src.TradingSessionSub, out.TradingSessionSub)
	out.Bid = firstNonEmpty(src.Bid, out.Bid)
	out.BidSize = firstNonEmpty(src.BidSize, out.BidSize)
	out.Offer = firstNonEmpty(src.Offer, out.Offer)
	out.OfferSize = firstNonEmpty(src.OfferSize, out.OfferSize)
	out.Last = firstNonEmpty(src.Last, out.Last)
	out.LastSize = firstNonEmpty(src.LastSize, out.LastSize)
	out.LastTime = firstNonEmpty(src.LastTime, out.LastTime)
	out.Open = firstNonEmpty(src.Open, out.Open)
	out.High = firstNonEmpty(src.High, out.High)
	out.Low = firstNonEmpty(src.Low, out.Low)
	out.Close = firstNonEmpty(src.Close, out.Close)
	out.WAPR = firstNonEmpty(src.WAPR, out.WAPR)
	out.Settle = firstNonEmpty(src.Settle, out.Settle)
	out.CurrentPrice = firstNonEmpty(src.CurrentPrice, out.CurrentPrice)
	out.OfficialPrice = firstNonEmpty(src.OfficialPrice, out.OfficialPrice)
	out.Volume = firstNonEmpty(src.Volume, out.Volume)
	out.Value = firstNonEmpty(src.Value, out.Value)
	if len(src.Entries) > 0 {
		if out.Entries == nil {
			out.Entries = map[string]string{}
		}
		for k, v := range src.Entries {
			out.Entries[k] = v
		}
	}
	if src.RawFIX != "" {
		out.RawFIX = src.RawFIX
	}
	if !src.UpdatedAt.IsZero() {
		out.UpdatedAt = src.UpdatedAt
	} else {
		out.UpdatedAt = time.Now().UTC()
	}
	return out
}

func quoteKey(q domain.MarketQuote) string {
	return q.Board + "|" + firstNonEmpty(q.SecurityID, q.Symbol)
}

func sortQuotes(out []domain.MarketQuote) {
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Board != out[j].Board {
			return out[i].Board < out[j].Board
		}
		si := firstNonEmpty(out[i].Symbol, out[i].SecurityID)
		sj := firstNonEmpty(out[j].Symbol, out[j].SecurityID)
		if si != sj {
			return si < sj
		}
		return out[i].SecurityID < out[j].SecurityID
	})
}

func cloneStrMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func parseFixNum(s string) (float64, bool) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func addNum(a, b string) string {
	x, okX := parseFixNum(a)
	y, okY := parseFixNum(b)
	if !okY {
		return a
	}
	if !okX {
		return b
	}
	return strconv.FormatFloat(x+y, 'f', -1, 64)
}

func mulNum(a, b string) string {
	x, okX := parseFixNum(a)
	y, okY := parseFixNum(b)
	if !okX || !okY {
		return ""
	}
	return strconv.FormatFloat(x*y, 'f', -1, 64)
}

func (a *sessionApp) applyLive(quotes []domain.MarketQuote) {
	if len(quotes) == 0 {
		return
	}
	now := time.Now().UTC()
	a.mdMu.Lock()
	changed := make([]domain.MarketQuote, 0, len(quotes))
	for _, q := range quotes {
		key := quoteKey(q)
		if key == "" || key == "|" {
			continue
		}
		incoming := q
		if prev, ok := a.mdBook[key]; ok {
			q = mergeQuote(prev, incoming)
			// 387/6143 are often throttled; fold last-trade prints into volume/value.
			if strings.TrimSpace(incoming.Volume) == "" && incoming.DeltaVolume != "" {
				q.Volume = addNum(prev.Volume, incoming.DeltaVolume)
			}
			if strings.TrimSpace(incoming.Value) == "" && incoming.DeltaValue != "" {
				q.Value = addNum(prev.Value, incoming.DeltaValue)
			}
		}
		q.DeltaVolume, q.DeltaValue = "", ""
		q.UpdatedAt = now
		a.mdBook[key] = q
		changed = append(changed, q)
	}
	cb := a.onMD
	a.mdMu.Unlock()
	if cb != nil && len(changed) > 0 {
		cb(changed)
	}
}

func (a *sessionApp) trackSub(reqID string) {
	a.mdMu.Lock()
	a.mdSubIDs[reqID] = struct{}{}
	a.mdMu.Unlock()
}

func (a *sessionApp) takeSubIDs() []string {
	a.mdMu.Lock()
	defer a.mdMu.Unlock()
	out := make([]string, 0, len(a.mdSubIDs))
	for id := range a.mdSubIDs {
		out = append(out, id)
	}
	a.mdSubIDs = map[string]struct{}{}
	return out
}

func (a *sessionApp) liveQuotes() []domain.MarketQuote {
	a.mdMu.Lock()
	defer a.mdMu.Unlock()
	out := make([]domain.MarketQuote, 0, len(a.mdBook))
	for _, q := range a.mdBook {
		out = append(out, q)
	}
	sortQuotes(out)
	return out
}

func buildMDUnsubscribe(reqID string) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_MARKET_DATA_REQUEST))
	msg.Body.SetString(tag.MDReqID, reqID)
	msg.Body.SetString(tag.SubscriptionRequestType, "2") // DISABLE_PREVIOUS
	return msg
}

// SubscribeMarketData sends 35=V with 263=1 (SNAPSHOT+UPDATES) and keeps the book live.
func (a *Adapter) SubscribeMarketData(ctx context.Context, q domain.MarketDataQuery) (string, []domain.MarketQuote, error) {
	if !a.app.isLoggedOn() {
		return "", nil, fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	q.SubscriptionType = "1"
	a.subMu.Lock()
	a.lastMDQuery = q
	a.subMu.Unlock()

	_ = a.UnsubscribeMarketData(ctx)

	batches := []domain.MarketDataQuery{q}
	if len(q.Instruments) > mdInstrumentBatch {
		batches = nil
		for i := 0; i < len(q.Instruments); i += mdInstrumentBatch {
			end := i + mdInstrumentBatch
			if end > len(q.Instruments) {
				end = len(q.Instruments)
			}
			b := q
			b.Instruments = q.Instruments[i:end]
			batches = append(batches, b)
		}
	}

	var lastID string
	for _, batch := range batches {
		reqID := newMDReqID()
		lastID = reqID
		a.app.trackSub(reqID)
		msg := buildMarketDataRequest(reqID, batch)
		if _, err := a.app.send(msg); err != nil {
			return reqID, nil, err
		}
		a.log.Info("market data subscribe sent",
			"adapter_id", a.meta.ID, "req_id", reqID, "instruments", len(batch.Instruments), "symbol", batch.Symbol)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items := a.app.liveQuotes()
		if len(items) > 0 {
			return lastID, items, nil
		}
		select {
		case <-ctx.Done():
			return lastID, a.app.liveQuotes(), ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return lastID, a.app.liveQuotes(), nil
}

func (a *Adapter) UnsubscribeMarketData(_ context.Context) error {
	ids := a.app.takeSubIDs()
	for _, id := range ids {
		if _, err := a.app.send(buildMDUnsubscribe(id)); err != nil {
			a.log.Warn("market data unsubscribe failed", "adapter_id", a.meta.ID, "req_id", id, "error", err)
		}
	}
	return nil
}

func (a *Adapter) LiveQuotes() []domain.MarketQuote {
	return a.app.liveQuotes()
}

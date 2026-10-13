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

// Current-trades + book MDEntryTypes (QUIK «Текущие торги» plus bid/offer depth).
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
	if len(quotes) == 0 {
		a.log.Warn("market data snapshot produced no quotes", "adapter_id", a.adapterID, "req_id", reqID)
	} else {
		a.log.Debug("market data snapshot", "adapter_id", a.adapterID, "req_id", reqID,
			"quotes", len(quotes), "symbol", quotes[0].Symbol, "board", quotes[0].Board)
	}
	a.applyLive(a.confirmMarketData(reqID, quotes))
	a.mdMu.Lock()
	w := a.mdWait
	a.mdMu.Unlock()
	if w != nil && (reqID == "" || w.reqID == reqID) {
		w.add(quotes)
	}
}

func (a *sessionApp) handleMarketDataIncremental(msg *quickfix.Message) {
	reqID, quotes := parseMarketDataIncremental(msg, a.adapterID)
	a.applyLive(a.confirmMarketData(reqID, quotes))
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
	msgText := text
	if msgText == "" {
		msgText = "Market Data Request Reject"
	}
	if reason != "" {
		msgText = fmt.Sprintf("%s (reason=%s)", msgText, reason)
	}
	tracked := a.rejectMarketDataSubscription(reqID, msgText)
	a.mdMu.Lock()
	w := a.mdWait
	a.mdMu.Unlock()
	a.log.Warn("market data reject", "adapter_id", a.adapterID, "req_id", reqID, "text", msgText, "subscription", tracked)
	if w != nil && (reqID == "" || w.reqID == reqID) {
		w.reject(msgText)
	}

}

var mdReqSeq atomic.Uint64

func newMDReqID() string {
	n := mdReqSeq.Add(1)
	return fmt.Sprintf("MD%d-%d", time.Now().UTC().Unix()%1_000_000, n)
}

// buildMarketDataRequest builds ARQA Market Data Request (35=V) for «Текущие торги»
// plus the visible order book. HowTo example (one instrument, full book, bid+offer):
//
//	35=V|22=8|48=LKOH|100=BQUOTE|262=…|263=1|264=0|265=0|267=2|269=1|269=0|
//
// 264=0 = Full Book (стакан). Entry types also include last/OHLC for the trades table.
func buildMarketDataRequest(reqID string, q domain.MarketDataQuery) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_MARKET_DATA_REQUEST))
	msg.Body.SetString(tag.MDReqID, reqID)
	sub := q.SubscriptionType
	if sub == "" {
		sub = "0" // SNAPSHOT
	}
	msg.Body.SetString(tag.SubscriptionRequestType, sub)
	msg.Body.SetString(tag.MarketDepth, "0") // Full Book
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
	case len(q.Instruments) == 1:
		// HowTo: one instrument at body level, no 146.
		// ARQA rejects 146=1 without tag 48 ("NoRelatedSym dont match … InstrmtMDReqGrps").
		setMDInstrumentBody(msg, q.Instruments[0])
	case len(q.Instruments) > 1:
		symRG := quickfix.NewRepeatingGroup(tag.NoRelatedSym, quickfix.GroupTemplate{
			quickfix.GroupElement(tag.SecurityIDSource),
			quickfix.GroupElement(tag.SecurityID),
			quickfix.GroupElement(tag.SecurityType),
			quickfix.GroupElement(tag.ExDestination),
		})
		for _, inst := range q.Instruments {
			if firstNonEmpty(inst.SecurityID, inst.Symbol) == "" {
				continue
			}
			g := symRG.Add()
			setMDInstrumentGroup(g, inst)
		}
		if symRG.Len() > 0 {
			msg.Body.SetGroup(symRG)
		}
	case strings.TrimSpace(q.Symbol) != "":
		setMDInstrumentBody(msg, domain.SecurityRef{
			Symbol: q.Symbol, SecurityID: q.Symbol, Board: q.Board, SecurityType: q.SecurityType,
		})
	}
	// Board-only / empty query: do not send 146. ARQA counts 146 against valid
	// groups that must include SecurityID (48).

	return msg
}

func mdInstrumentID(inst domain.SecurityRef) string {
	return firstNonEmpty(inst.SecurityID, inst.Symbol)
}

func mdInstrumentBoard(inst domain.SecurityRef) string {
	board := strings.TrimSpace(inst.Board)
	if strings.EqualFold(board, "UNKNOWN") {
		return ""
	}
	return board
}

func setMDInstrumentBody(msg *quickfix.Message, inst domain.SecurityRef) {
	sym := mdInstrumentID(inst)
	if sym == "" {
		return
	}
	msg.Body.SetString(tag.SecurityIDSource, string(enum.SecurityIDSource_EXCHANGE_SYMBOL)) // 8
	msg.Body.SetString(tag.SecurityID, sym)
	if st := domain.NormalizeSecurityType(inst.SecurityType); st != "" {
		msg.Body.SetString(tag.SecurityType, st)
	}
	if board := mdInstrumentBoard(inst); board != "" {
		msg.Body.SetString(tag.ExDestination, board)
	}
}

func setMDInstrumentGroup(g *quickfix.Group, inst domain.SecurityRef) {
	sym := mdInstrumentID(inst)
	g.SetString(tag.SecurityIDSource, string(enum.SecurityIDSource_EXCHANGE_SYMBOL))
	g.SetString(tag.SecurityID, sym)
	if st := domain.NormalizeSecurityType(inst.SecurityType); st != "" {
		g.SetString(tag.SecurityType, st)
	}
	if board := mdInstrumentBoard(inst); board != "" {
		g.SetString(tag.ExDestination, board)
	}
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
	a.log.Debug("market data request sent",
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
		case "55", "311": // Symbol / UnderlyingSymbol
			if inEntries && cur != nil {
				cur.symbol = v
			} else {
				base.Symbol = v
			}
		case "48", "309": // SecurityID / UnderlyingSecurityID
			if inEntries && cur != nil {
				cur.securityID = v
			} else {
				base.SecurityID = v
			}
		case "100", "207", "308": // ExDestination / exchange / UnderlyingSecurityExchange
			if inEntries && cur != nil {
				cur.board = v
			} else if base.Board == "" {
				base.Board = v
			}
		case "318":
			if base.Currency == "" {
				base.Currency = v
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
		q                domain.MarketQuote
		sawBid, sawOffer bool
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
		deleted := e.action == "2" || (incremental && isZeroSize(size) && (e.typ == "0" || e.typ == "1"))
		if e.typ == "0" {
			b.sawBid = true
			if deleted {
				if e.px != "" {
					b.q.DeletedBids = append(b.q.DeletedBids, e.px)
				}
				continue
			}
		}
		if e.typ == "1" {
			b.sawOffer = true
			if deleted {
				if e.px != "" {
					b.q.DeletedOffers = append(b.q.DeletedOffers, e.px)
				}
				continue
			}
		}
		if e.action == "2" {
			continue
		}
		applyMDEntry(&b.q, e.typ, e.px, size, e.tm)
	}

	for _, b := range groups {
		if !incremental {
			if b.sawBid {
				b.q.ReplaceBids = true
			}
			if b.sawOffer {
				b.q.ReplaceOffers = true
			}
		}
		finalizeBook(&b.q)
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
		if px != "" && !isZeroSize(size) {
			q.Bids = addBookLevel(q.Bids, px, size)
		}
	case "1":
		q.Offer, q.OfferSize = firstNonEmpty(px, q.Offer), firstNonEmpty(size, q.OfferSize)
		if px != "" && !isZeroSize(size) {
			q.Offers = addBookLevel(q.Offers, px, size)
		}
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
	if src.ReplaceBids {
		out.Bids = cloneBookLevels(src.Bids)
		out.ReplaceBids = true
	} else {
		out.Bids = upsertBookLevels(out.Bids, src.Bids, src.DeletedBids)
	}
	if src.ReplaceOffers {
		out.Offers = cloneBookLevels(src.Offers)
		out.ReplaceOffers = true
	} else {
		out.Offers = upsertBookLevels(out.Offers, src.Offers, src.DeletedOffers)
	}
	finalizeBook(&out)
	out.ReplaceBids, out.ReplaceOffers = false, false
	out.DeletedBids, out.DeletedOffers = nil, nil
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

func isZeroSize(s string) bool {
	n, ok := parseFixNum(s)
	return ok && n == 0
}

func addBookLevel(levels []domain.BookLevel, px, size string) []domain.BookLevel {
	if px == "" {
		return levels
	}
	for i, l := range levels {
		if l.Price == px {
			if size != "" {
				levels[i].Size = addNum(l.Size, size)
			}
			return levels
		}
	}
	return append(levels, domain.BookLevel{Price: px, Size: size})
}

func cloneBookLevels(in []domain.BookLevel) []domain.BookLevel {
	if in == nil {
		return nil
	}
	out := make([]domain.BookLevel, len(in))
	copy(out, in)
	return out
}

func upsertBookLevels(dst, src []domain.BookLevel, deleted []string) []domain.BookLevel {
	if len(src) == 0 && len(deleted) == 0 {
		return cloneBookLevels(dst)
	}
	byPx := make(map[string]domain.BookLevel, len(dst)+len(src))
	for _, l := range dst {
		if l.Price != "" {
			byPx[l.Price] = l
		}
	}
	for _, px := range deleted {
		delete(byPx, px)
	}
	for _, l := range src {
		if l.Price == "" {
			continue
		}
		if isZeroSize(l.Size) {
			delete(byPx, l.Price)
			continue
		}
		byPx[l.Price] = l
	}
	out := make([]domain.BookLevel, 0, len(byPx))
	for _, l := range byPx {
		out = append(out, l)
	}
	return out
}

func sortBookLevels(levels []domain.BookLevel, bids bool) []domain.BookLevel {
	sort.SliceStable(levels, func(i, j int) bool {
		ni, okI := parseFixNum(levels[i].Price)
		nj, okJ := parseFixNum(levels[j].Price)
		if okI && okJ && ni != nj {
			if bids {
				return ni > nj
			}
			return ni < nj
		}
		if bids {
			return levels[i].Price > levels[j].Price
		}
		return levels[i].Price < levels[j].Price
	})
	return levels
}

func finalizeBook(q *domain.MarketQuote) {
	q.Bids = sortBookLevels(q.Bids, true)
	q.Offers = sortBookLevels(q.Offers, false)
	if len(q.Bids) > 0 {
		q.Bid, q.BidSize = q.Bids[0].Price, q.Bids[0].Size
	} else if q.ReplaceBids {
		q.Bid, q.BidSize = "", ""
	}
	if len(q.Offers) > 0 {
		q.Offer, q.OfferSize = q.Offers[0].Price, q.Offers[0].Size
	} else if q.ReplaceOffers {
		q.Offer, q.OfferSize = "", ""
	}
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
		q.ReplaceBids, q.ReplaceOffers = false, false
		q.DeletedBids, q.DeletedOffers = nil, nil
		q.Bids = cloneBookLevels(q.Bids)
		q.Offers = cloneBookLevels(q.Offers)
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
	a.mdSubscriptions = map[string]*mdSubscription{}
	a.mdBook = map[string]domain.MarketQuote{}
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
	return a.ensureMarketDataSubscriptions(ctx, q)
}

func firstInst(q domain.MarketDataQuery) domain.SecurityRef {
	if len(q.Instruments) > 0 {
		return q.Instruments[0]
	}
	return domain.SecurityRef{Symbol: q.Symbol, Board: q.Board, SecurityType: q.SecurityType}
}

func mdQueryHasSecurityID(q domain.MarketDataQuery) bool {
	if strings.TrimSpace(q.Symbol) != "" {
		return true
	}
	for _, inst := range q.Instruments {
		if mdInstrumentID(inst) != "" {
			return true
		}
	}
	return false
}

func marketDataSubscribeBatches(q domain.MarketDataQuery) []domain.MarketDataQuery {
	var batches []domain.MarketDataQuery
	if len(q.Instruments) > 0 {
		type bucket struct {
			board string
			list  []domain.SecurityRef
		}
		var order []string
		byBoard := map[string]*bucket{}
		for _, inst := range q.Instruments {
			board := strings.TrimSpace(inst.Board)
			if board == "" || strings.EqualFold(board, "UNKNOWN") {
				board = ""
			}
			key := strings.ToUpper(board)
			b := byBoard[key]
			if b == nil {
				b = &bucket{board: board}
				byBoard[key] = b
				order = append(order, key)
			}
			b.list = append(b.list, inst)
		}
		for _, key := range order {
			list := byBoard[key].list
			for i := 0; i < len(list); i += mdInstrumentBatch {
				end := i + mdInstrumentBatch
				if end > len(list) {
					end = len(list)
				}
				batches = append(batches, domain.MarketDataQuery{
					Instruments:      list[i:end],
					SubscriptionType: q.SubscriptionType,
				})
			}
		}
	}
	if strings.TrimSpace(q.Symbol) != "" && len(q.Instruments) == 0 {
		batches = append(batches, q)
	}
	return batches
}

func (a *Adapter) UnsubscribeMarketData(_ context.Context) error {
	a.mdRequestMu.Lock()
	defer a.mdRequestMu.Unlock()
	a.subMu.Lock()
	a.lastMDQuery = domain.MarketDataQuery{}
	a.subMu.Unlock()
	ids := a.app.takeSubIDs()
	for _, id := range ids {
		if err := a.sendMarketData(buildMDUnsubscribe(id)); err != nil {
			a.log.Warn("market data unsubscribe failed", "adapter_id", a.meta.ID, "req_id", id, "error", err)
		}
	}
	return nil
}

func (a *Adapter) seedSubscribedInstruments(batch domain.MarketDataQuery) {
	var list []domain.SecurityRef
	if len(batch.Instruments) > 0 {
		list = batch.Instruments
	} else if strings.TrimSpace(batch.Symbol) != "" {
		list = []domain.SecurityRef{{
			Symbol: batch.Symbol, SecurityID: batch.Symbol,
			Board: batch.Board, SecurityType: batch.SecurityType,
		}}
	}
	now := time.Now().UTC()
	a.app.mdMu.Lock()
	defer a.app.mdMu.Unlock()
	for _, inst := range list {
		q := domain.MarketQuote{
			AdapterID:  a.meta.ID,
			Symbol:     firstNonEmpty(inst.Symbol, inst.SecurityID),
			SecurityID: firstNonEmpty(inst.SecurityID, inst.Symbol),
			Board:      mdInstrumentBoard(inst),
			UpdatedAt:  now,
		}
		key := quoteKey(q)
		if key == "" || key == "|" {
			continue
		}
		if _, ok := a.app.mdBook[key]; !ok {
			a.app.mdBook[key] = q
		}
	}
}

func (a *Adapter) LiveQuotes() []domain.MarketQuote {
	return a.app.liveQuotes()
}

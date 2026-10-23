package arqa

import (
	"fmt"
	"log/slog"
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

type sessionApp struct {
	username  string
	password  string
	adapterID string
	log       *slog.Logger
	onExec    domain.ExecutionHandler

	mu         sync.RWMutex
	loggedOn   atomic.Bool
	sessionID  quickfix.SessionID
	hasSession atomic.Bool
	lastError  atomic.Value
	msgSent    atomic.Int64
	msgRecv    atomic.Int64
	errCount   atomic.Int64
	pendingRaw map[string]string // ClOrdID → outbound wire (captured in ToApp)

	sentMu   sync.Mutex
	sent     map[int]sentMessage // outbound MsgSeqNum → type, for 35=3 without 372
	sentRing []int

	secDefMu   sync.Mutex
	secDefWait *secDefWaiter

	posMu       sync.Mutex
	posWait     *posWaiter
	maintWait   *maintWaiter
	posStream   *positionStream
	posRetryAt  time.Time
	posError    string
	posAttempts int
	posReceived time.Time
	posNow      func() time.Time
	onPositions func(string, string, []domain.FIXPosition) error

	mdMu            sync.Mutex
	mdWait          *mdWaiter
	mdBook          map[string]domain.MarketQuote
	mdSubIDs        map[string]struct{}
	mdSubscriptions map[string]*mdSubscription
	mdNow           func() time.Time
	onMD            domain.MarketDataHandler
	onLogon         func()
	onFIX           func(kind, sessionID, msgType, direction, clOrdID, reason string, seq int)
}

func newSessionApp(adapterID string, log *slog.Logger, onExec domain.ExecutionHandler) *sessionApp {
	a := &sessionApp{
		adapterID:       adapterID,
		log:             log,
		onExec:          onExec,
		pendingRaw:      map[string]string{},
		sent:            map[int]sentMessage{},
		mdBook:          map[string]domain.MarketQuote{},
		mdSubIDs:        map[string]struct{}{},
		mdSubscriptions: map[string]*mdSubscription{},
	}
	a.lastError.Store("")
	return a
}

func (a *sessionApp) OnCreate(sessionID quickfix.SessionID) {
	a.mu.Lock()
	a.sessionID = sessionID
	a.mu.Unlock()
	a.hasSession.Store(true)
	a.log.Info("fix session created", "adapter_id", a.adapterID, "session", sessionID.String())
	a.emitFIX("FIX_SESSION_CREATED", sessionID.String(), "", "", "", "", 0)
}

func (a *sessionApp) OnLogon(sessionID quickfix.SessionID) {
	a.loggedOn.Store(true)
	a.log.Info("fix session logon", "adapter_id", a.adapterID, "session", sessionID.String())
	a.emitFIX("FIX_SESSION_CONNECTED", sessionID.String(), "A", "IN", "", "", 0)
	if a.onLogon != nil {
		go a.onLogon()
	}
}

func (a *sessionApp) OnLogout(sessionID quickfix.SessionID) {
	a.loggedOn.Store(false)
	a.posMu.Lock()
	a.posStream = nil
	a.posRetryAt, a.posError, a.posAttempts = time.Time{}, "", 0
	a.posMu.Unlock()
	// Quotes from the old session must not suppress background resubscription.
	a.mdMu.Lock()
	a.mdBook = map[string]domain.MarketQuote{}
	a.mdSubIDs = map[string]struct{}{}
	a.mdSubscriptions = map[string]*mdSubscription{}
	a.mdMu.Unlock()
	a.log.Info("fix session logout", "adapter_id", a.adapterID, "session", sessionID.String())
	a.emitFIX("FIX_SESSION_DISCONNECTED", sessionID.String(), "5", "IN", "", "logout", 0)
}

// sentMessage identifies an outbound message a session Reject may refer to.
type sentMessage struct {
	msgType string
	clOrdID string
}

const sentHistory = 8192

// rememberSent records the type of an outbound message by sequence number.
// quickfixgo assigns MsgSeqNum before ToAdmin/ToApp.
func (a *sessionApp) rememberSent(msg *quickfix.Message) {
	seq, err := msg.Header.GetInt(tag.MsgSeqNum)
	if err != nil {
		return
	}
	msgType, _ := msg.MsgType()
	clOrdID, _ := msg.Body.GetString(tag.ClOrdID)
	a.sentMu.Lock()
	defer a.sentMu.Unlock()
	if _, ok := a.sent[seq]; !ok {
		a.sentRing = append(a.sentRing, seq)
		if len(a.sentRing) > sentHistory {
			delete(a.sent, a.sentRing[0])
			a.sentRing = a.sentRing[1:]
		}
	}
	a.sent[seq] = sentMessage{msgType: msgType, clOrdID: clOrdID}
}

func (a *sessionApp) sentBySeq(raw string) (sentMessage, bool) {
	seq, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return sentMessage{}, false
	}
	a.sentMu.Lock()
	defer a.sentMu.Unlock()
	m, ok := a.sent[seq]
	return m, ok
}

func (a *sessionApp) ToAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) {
	a.rememberSent(msg)
	if kind, _ := msg.MsgType(); kind == "A" {
		if a.username != "" {
			msg.Body.SetString(tag.Username, a.username)
		}
		if a.password != "" {
			msg.Body.SetString(tag.Password, a.password)
		}
	}
}
func (a *sessionApp) ToApp(msg *quickfix.Message, sessionID quickfix.SessionID) error {
	a.rememberSent(msg)
	clOrdID, err := msg.Body.GetString(tag.ClOrdID)
	// Resends (PossDup) are not taken by send(); keep only first transmissions.
	possDup, _ := msg.Header.GetBool(tag.PossDupFlag)
	if err == nil && clOrdID != "" && !possDup {
		a.mu.Lock()
		a.pendingRaw[clOrdID] = formatWireFIX(msg)
		a.mu.Unlock()
	}
	msgType, _ := msg.MsgType()
	seq := 0
	if n, err := msg.Header.GetInt(tag.MsgSeqNum); err == nil {
		seq = int(n)
	}
	kind := "FIX_MESSAGE_SENT"
	switch msgType {
	case string(enum.MsgType_ORDER_SINGLE), string(enum.MsgType_ORDER_CANCEL_REQUEST), string(enum.MsgType_ORDER_CANCEL_REPLACE_REQUEST):
		kind = "FIX_ORDER_SENT"
	}
	if clOrdID == "" {
		clOrdID, _ = msg.Body.GetString(tag.ClOrdID)
	}
	a.emitFIX(kind, sessionID.String(), msgType, "OUT", clOrdID, "", seq)
	return nil
}

func (a *sessionApp) FromAdmin(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	msgType, err := msg.MsgType()
	if err != nil {
		return nil
	}
	switch msgType {
	case string(enum.MsgType_HEARTBEAT), string(enum.MsgType_TEST_REQUEST),
		string(enum.MsgType_RESEND_REQUEST), string(enum.MsgType_SEQUENCE_RESET),
		string(enum.MsgType_LOGON), string(enum.MsgType_LOGOUT):
		return nil
	case string(enum.MsgType_REJECT):
		a.log.Warn("fix session reject", "adapter_id", a.adapterID, "wire", sanitizeFIX(msg))
		a.handleFIXReject(msg)
		a.emitAppFIX("FIX_REJECT", msg, sessionID)
		return nil
	default:
		a.log.Debug("fix admin message", "adapter_id", a.adapterID, "msg_type", msgType, "wire", sanitizeFIX(msg))
		return nil
	}
}

func (a *sessionApp) FromApp(msg *quickfix.Message, sessionID quickfix.SessionID) quickfix.MessageRejectError {
	a.msgRecv.Add(1)
	msgType, err := msg.MsgType()
	if err != nil {
		return err
	}
	switch msgType {
	case string(enum.MsgType_EXECUTION_REPORT):
		a.handleExecutionReport(msg)
		a.emitAppFIX("FIX_EXECUTION_REPORT", msg, sessionID)
	case string(enum.MsgType_ORDER_CANCEL_REJECT):
		a.handleOrderCancelReject(msg)
		a.emitAppFIX("FIX_REJECT", msg, sessionID)
	case string(enum.MsgType_REJECT):
		a.handleFIXReject(msg)
		a.emitAppFIX("FIX_REJECT", msg, sessionID)
	case string(enum.MsgType_BUSINESS_MESSAGE_REJECT):
		a.handleFIXReject(msg)
		a.emitAppFIX("FIX_REJECT", msg, sessionID)
	case string(enum.MsgType_SECURITY_DEFINITION):
		a.handleSecurityDefinition(msg)
	case string(enum.MsgType_POSITION_REPORT):
		a.handlePositionReport(msg)
	case string(enum.MsgType_REQUEST_FOR_POSITIONS_ACK):
		a.handleRequestForPositionsAck(msg)
	case string(enum.MsgType_POSITION_MAINTENANCE_REPORT):
		a.handlePositionMaintenanceReport(msg)
	case string(enum.MsgType_MARKET_DATA_SNAPSHOT_FULL_REFRESH):
		a.handleMarketDataSnapshot(msg)
	case string(enum.MsgType_MARKET_DATA_INCREMENTAL_REFRESH):
		a.handleMarketDataIncremental(msg)
	case string(enum.MsgType_MARKET_DATA_REQUEST_REJECT):
		a.handleMarketDataReject(msg)
	default:
		a.log.Debug("unhandled fix app message", "adapter_id", a.adapterID, "msg_type", msgType, "wire", sanitizeFIX(msg))
		if msgType == "AM" {
			a.handlePositionMaintenanceReport(msg)
		}
	}
	return nil
}

func (a *sessionApp) handleExecutionReport(msg *quickfix.Message) {
	if a.onExec == nil {
		return
	}
	get := func(t quickfix.Tag) string {
		v, err := msg.Body.GetString(t)
		if err != nil {
			return ""
		}
		return v
	}
	a.onExec(domain.ExecutionReport{
		AdapterID:     a.adapterID,
		ClOrdID:       get(tag.ClOrdID),
		OrigClOrdID:   get(tag.OrigClOrdID),
		OrderID:       get(tag.OrderID),
		ExecID:        get(tag.ExecID),
		ExecType:      get(tag.ExecType),
		ExecTransType: get(tag.ExecTransType),
		ExecRefID:     get(tag.ExecRefID),
		TradeDate:     get(tag.TradeDate),
		TransactTime:  get(tag.TransactTime),
		OriginalTime:  executionOriginalTime(msg),
		OrdStatus:     get(tag.OrdStatus),
		LastQty:       get(tag.LastQty),
		LastPx:        get(tag.LastPx),
		CumQty:        get(tag.CumQty),
		LeavesQty:     get(tag.LeavesQty),
		Text:          get(tag.Text),
		OrdRejReason:  get(tag.OrdRejReason),
		RawFIX:        formatWireFIX(msg),
	})
}

func executionOriginalTime(msg *quickfix.Message) string {
	if value, err := msg.Header.GetString(tag.OrigSendingTime); err == nil && value != "" {
		return value
	}
	value, _ := msg.Header.GetString(tag.SendingTime)
	return value
}

func (a *sessionApp) handleOrderCancelReject(msg *quickfix.Message) {
	if a.onExec == nil {
		return
	}
	get := func(t quickfix.Tag) string {
		v, err := msg.Body.GetString(t)
		if err != nil {
			return ""
		}
		return v
	}
	a.log.Warn("order cancel reject", "adapter_id", a.adapterID, "msg", sanitizeFIX(msg))
	a.onExec(domain.ExecutionReport{
		AdapterID:    a.adapterID,
		ClOrdID:      get(tag.ClOrdID),
		OrigClOrdID:  get(tag.OrigClOrdID),
		OrderID:      get(tag.OrderID),
		Text:         get(tag.Text),
		CxlRejReason: get(tag.CxlRejReason),
		CancelReject: true,
		RawFIX:       formatWireFIX(msg),
	})
}

func (a *sessionApp) send(msg *quickfix.Message) (string, error) {
	if !a.loggedOn.Load() {
		return "", fmt.Errorf("session not logged on")
	}
	clOrdID, _ := msg.Body.GetString(tag.ClOrdID)
	a.mu.RLock()
	sid := a.sessionID
	a.mu.RUnlock()
	if err := quickfix.SendToTarget(msg, sid); err != nil {
		a.takeSentRaw(clOrdID)
		a.errCount.Add(1)
		a.lastError.Store(err.Error())
		return "", err
	}
	a.msgSent.Add(1)
	raw := a.takeSentRaw(clOrdID)
	if raw == "" {
		raw = formatWireFIX(msg)
	}
	return raw, nil
}

func (a *sessionApp) takeSentRaw(clOrdID string) string {
	if clOrdID == "" {
		return ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	raw := a.pendingRaw[clOrdID]
	delete(a.pendingRaw, clOrdID)
	return raw
}

func (a *sessionApp) isLoggedOn() bool { return a.loggedOn.Load() }

// formatWireFIX renders a FIX message as tag=value pairs separated by | (SOH replaced).
func formatWireFIX(msg *quickfix.Message) string {
	s := strings.ReplaceAll(msg.String(), "\x01", "|")
	s = strings.TrimRight(s, "|")
	if s != "" && !strings.HasSuffix(s, "|") {
		s += "|"
	}
	return s
}

func sanitizeFIX(msg *quickfix.Message) string {
	s := formatWireFIX(msg)
	parts := strings.Split(s, "|")
	for i, p := range parts {
		key, _, ok := strings.Cut(p, "=")
		if ok && (key == "553" || key == "554" || key == "925" || key == "96") {
			parts[i] = key + "=[redacted]"
		}
	}
	s = strings.Join(parts, "|")
	if len(s) > 500 {
		return s[:500]
	}
	return s
}

// buildNewOrderSingle builds MsgType=D with the ARQA wire tag set:
// 11, 21, 54, 60, 40, 44, 38, 1, 22=8, 48, 100, 15, 59, 109, 5018 (35 in header).
// No Symbol (55) / SecurityExchange (207) — class code goes in ExDestination (100).
func buildNewOrderSingle(clOrdID, account, clientID, firm, symbol, board, side, ordType, qty, price, tif, currency string) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_ORDER_SINGLE))
	msg.Body.Set(field.NewClOrdID(clOrdID))
	msg.Body.Set(field.NewHandlInst(enum.HandlInst_AUTOMATED_EXECUTION_ORDER_PRIVATE_NO_BROKER_INTERVENTION))
	msg.Body.Set(field.NewSide(mapSide(side)))
	msg.Body.Set(field.NewTransactTimeNoMillis(time.Now().UTC()))
	msg.Body.Set(field.NewOrdType(mapOrdType(ordType)))
	if ordType == "LIMIT" && price != "" {
		msg.Body.SetString(tag.Price, price)
	}
	msg.Body.SetString(tag.OrderQty, qty)
	msg.Body.Set(field.NewAccount(account))
	// ARQA sample: SecurityIDSource=8 (Exchange Symbol) even for ISIN-shaped codes.
	msg.Body.Set(field.NewSecurityIDSource(enum.SecurityIDSource_EXCHANGE_SYMBOL))
	msg.Body.Set(field.NewSecurityID(symbol))
	if board != "" {
		msg.Body.SetString(tag.ExDestination, board)
	}
	if currency != "" {
		msg.Body.Set(field.NewCurrency(currency))
	}
	msg.Body.Set(field.NewTimeInForce(mapTIF(tif)))
	if clientID != "" {
		msg.Body.Set(field.NewClientID(clientID))
	}
	if firm != "" {
		// ARQA custom: firm code (код фирмы) from API token
		msg.Body.SetField(quickfix.Tag(5018), quickfix.FIXString(firm))
	}
	return msg
}

func buildCancel(clOrdID, origClOrdID, orderID, symbol, board, side, account, clientID, firm, qty string) *quickfix.Message {
	msg := quickfix.NewMessage()
	msg.Header.SetField(tag.MsgType, field.NewMsgType(enum.MsgType_ORDER_CANCEL_REQUEST))
	msg.Body.Set(field.NewClOrdID(clOrdID))
	msg.Body.Set(field.NewOrigClOrdID(origClOrdID))
	if orderID != "" {
		msg.Body.Set(field.NewOrderID(orderID))
	}
	if account != "" {
		msg.Body.Set(field.NewAccount(account))
	}
	if clientID != "" {
		msg.Body.Set(field.NewClientID(clientID))
	}
	msg.Body.Set(field.NewSide(mapSide(side)))
	msg.Body.Set(field.NewTransactTimeNoMillis(time.Now().UTC()))
	// Same instrument identification as NewOrderSingle — no tag 55 Symbol.
	msg.Body.Set(field.NewSecurityIDSource(enum.SecurityIDSource_EXCHANGE_SYMBOL))
	msg.Body.Set(field.NewSecurityID(symbol))
	if board != "" {
		msg.Body.SetString(tag.ExDestination, board)
	}
	if qty != "" {
		msg.Body.SetString(tag.OrderQty, qty)
	}
	if firm != "" {
		msg.Body.SetField(quickfix.Tag(5018), quickfix.FIXString(firm))
	}
	return msg
}

// securityIDSourceFor picks FIX tag 22. ISIN-shaped codes use 4; otherwise exchange symbol (8).
func securityIDSourceFor(symbol string) enum.SecurityIDSource {
	if looksLikeISIN(symbol) {
		return enum.SecurityIDSource_ISIN_NUMBER
	}
	return enum.SecurityIDSource_EXCHANGE_SYMBOL
}

func looksLikeISIN(symbol string) bool {
	s := strings.TrimSpace(symbol)
	if len(s) != 12 {
		return false
	}
	for i := 0; i < 2; i++ {
		c := s[i]
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	for i := 2; i < 12; i++ {
		c := s[i]
		if (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

func mapSide(side string) enum.Side {
	if side == "SELL" {
		return enum.Side_SELL
	}
	return enum.Side_BUY
}

func mapOrdType(t string) enum.OrdType {
	if t == "MARKET" {
		return enum.OrdType_MARKET
	}
	return enum.OrdType_LIMIT
}

func mapTIF(t string) enum.TimeInForce {
	switch t {
	case "IOC":
		return enum.TimeInForce_IMMEDIATE_OR_CANCEL
	case "FOK":
		return enum.TimeInForce_FILL_OR_KILL
	case "GTC":
		return enum.TimeInForce_GOOD_TILL_CANCEL
	default:
		return enum.TimeInForce_DAY
	}
}

func (a *sessionApp) emitFIX(kind, sessionID, msgType, direction, clOrdID, reason string, seq int) {
	if a.onFIX == nil {
		return
	}
	a.onFIX(kind, sessionID, msgType, direction, clOrdID, reason, seq)
}

func (a *sessionApp) emitAppFIX(kind string, msg *quickfix.Message, sessionID quickfix.SessionID) {
	msgType, _ := msg.MsgType()
	seq := 0
	if n, err := msg.Header.GetInt(tag.MsgSeqNum); err == nil {
		seq = int(n)
	}
	clOrdID, _ := msg.Body.GetString(tag.ClOrdID)
	a.emitFIX(kind, sessionID.String(), msgType, "IN", clOrdID, "", seq)
}

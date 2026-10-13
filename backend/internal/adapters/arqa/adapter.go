package arqa

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/quickfix/store/file"
	"github.com/shopspring/decimal"
)

// Adapter implements the Adapter interface using QuickFIX/Go against ARQA or a FIX.4.4 simulator.
type Adapter struct {
	meta      domain.Adapter
	log       *slog.Logger
	onExec    domain.ExecutionHandler
	app       *sessionApp
	initiator *quickfix.Initiator
	status    atomic.Value
	active    atomic.Int64
	processed atomic.Int64
	errors    atomic.Int64
	storeDir  string

	subMu       sync.Mutex
	mdRequestMu sync.Mutex
	lastMDQuery domain.MarketDataQuery
	mdSend      func(*quickfix.Message) error
	posSend     func(*quickfix.Message) error
}

func New(meta domain.Adapter, log *slog.Logger, onExec domain.ExecutionHandler, onMD domain.MarketDataHandler, storeDir string) *Adapter {
	a := &Adapter{meta: meta, log: log, onExec: onExec, storeDir: storeDir}
	a.status.Store(domain.AdapterUnknown)
	a.app = newSessionApp(meta.ID, log, onExec)
	a.app.onMD = onMD
	a.app.onLogon = func() {
		a.subMu.Lock()
		q := a.lastMDQuery
		a.subMu.Unlock()
		if q.Symbol == "" && q.Board == "" && len(q.Boards) == 0 && len(q.Instruments) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, _, err := a.SubscribeMarketData(ctx, q); err != nil {
			a.log.Warn("market data resubscribe failed", "adapter_id", a.meta.ID, "error", err)
		}
	}
	return a
}

func (a *Adapter) SetFIXHandler(fn func(kind, sessionID, msgType, direction, clOrdID, reason string, seq int)) {
	if a.app != nil {
		a.app.onFIX = fn
	}
}

func (a *Adapter) ID() string   { return a.meta.ID }
func (a *Adapter) Name() string { return a.meta.Name }
func (a *Adapter) Meta() domain.Adapter {
	m := a.meta
	m.Status = a.Status()
	m.ActiveOrders = a.ActiveOrders()
	m.ProcessedOrders = a.processed.Load()
	m.ErrorCount = a.errors.Load()
	return m
}

func (a *Adapter) Status() domain.AdapterStatus {
	if v, ok := a.status.Load().(domain.AdapterStatus); ok {
		return v
	}
	return domain.AdapterUnknown
}

func (a *Adapter) ActiveOrders() int64   { return a.active.Load() }
func (a *Adapter) IncActive(delta int64) { a.active.Add(delta) }

func (a *Adapter) Start(ctx context.Context) error {
	a.status.Store(domain.AdapterStarting)
	settings, err := a.buildSettings()
	if err != nil {
		a.status.Store(domain.AdapterOffline)
		return err
	}
	storeFactory := file.NewStoreFactory(settings)
	logFactory := quickfix.NewNullLogFactory()
	initiator, err := quickfix.NewInitiator(a.app, storeFactory, settings, logFactory)
	if err != nil {
		a.status.Store(domain.AdapterOffline)
		return err
	}
	a.initiator = initiator
	if err := initiator.Start(); err != nil {
		a.status.Store(domain.AdapterOffline)
		return err
	}
	go func() {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if a.app.isLoggedOn() {
				a.status.Store(domain.AdapterOnline)
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(200 * time.Millisecond):
			}
		}
		if !a.app.isLoggedOn() {
			a.status.Store(domain.AdapterOffline)
		}
	}()
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	a.status.Store(domain.AdapterStopping)
	_ = a.UnsubscribePositions(ctx)
	_ = a.UnsubscribeMarketData(ctx)
	if a.initiator != nil {
		a.initiator.Stop()
	}
	a.status.Store(domain.AdapterOffline)
	return nil
}

func (a *Adapter) HealthCheck(ctx context.Context) (domain.HealthDetails, error) {
	loggedOn := a.app != nil && a.app.isLoggedOn()
	h := domain.HealthDetails{
		ProcessAlive:    a.initiator != nil,
		TCPConnected:    loggedOn || a.Status() == domain.AdapterStarting,
		SessionLoggedOn: loggedOn,
	}
	if loggedOn {
		a.status.Store(domain.AdapterOnline)
		h.Message = "session logged on"
		return h, nil
	}
	if a.Status() != domain.AdapterStarting && a.Status() != domain.AdapterStopping {
		a.status.Store(domain.AdapterOffline)
	}
	h.Message = "session not logged on"
	return h, fmt.Errorf("%s", h.Message)
}

func (a *Adapter) SendOrder(ctx context.Context, order domain.Order) (string, error) {
	if !a.app.isLoggedOn() {
		a.errors.Add(1)
		return "", fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	clOrdID := ""
	if order.FixClOrdID != nil {
		clOrdID = *order.FixClOrdID
	}
	if clOrdID == "" {
		return "", fmt.Errorf("missing fix_cl_ord_id")
	}
	price := ""
	if order.Price != nil {
		price = order.Price.String()
	}
	msg := buildNewOrderSingle(
		clOrdID, order.TradingAccount, order.ClientCode, order.QuikFirm, order.Symbol, order.Board,
		string(order.Side), string(order.OrderType), order.Quantity.String(), price,
		order.TimeInForce, order.Currency,
	)
	raw, err := a.app.send(msg)
	if err != nil {
		a.errors.Add(1)
		return "", err
	}
	a.processed.Add(1)
	a.active.Add(1)
	return raw, nil
}

func (a *Adapter) CancelOrder(ctx context.Context, order domain.Order) error {
	if !a.app.isLoggedOn() {
		return fmt.Errorf("adapter %s not logged on", a.meta.ID)
	}
	orig := ""
	if order.FixClOrdID != nil {
		orig = *order.FixClOrdID
	}
	quik := ""
	if order.QuikOrderID != nil {
		quik = *order.QuikOrderID
	}
	qty := order.Quantity.String()
	if leaves := order.Quantity.Sub(order.FilledQuantity); leaves.GreaterThan(decimal.Zero) {
		qty = leaves.String()
	}
	msg := buildCancel(orig+"-C", orig, quik, order.Symbol, order.Board, string(order.Side), order.TradingAccount, order.ClientCode, order.QuikFirm, qty)
	if _, err := a.app.send(msg); err != nil {
		a.errors.Add(1)
		return err
	}
	return nil
}

func (a *Adapter) ReplaceOrder(ctx context.Context, order domain.Order) error {
	return fmt.Errorf("replace not implemented in MVP")
}

func (a *Adapter) buildSettings() (*quickfix.Settings, error) {
	if a.meta.ID == "" || a.meta.ID == "." || a.meta.ID == ".." || strings.ContainsAny(a.meta.ID, "/\\\r\n\x00") {
		return nil, fmt.Errorf("invalid adapter ID")
	}
	for _, value := range []string{a.meta.BeginString, a.meta.SenderCompID, a.meta.TargetCompID, a.meta.Host, a.storeDir} {
		if strings.ContainsAny(value, "\r\n\x00") {
			return nil, fmt.Errorf("invalid FIX setting")
		}
	}
	transport, err := transportSettings(a.meta.ID, a.meta.ResetOnLogon)
	if err != nil {
		return nil, err
	}
	username, err := credential(a.meta.UsernameEnv)
	if err != nil {
		return nil, err
	}
	password, err := credential(a.meta.PasswordEnv)
	if err != nil {
		return nil, err
	}
	a.app.username = username
	a.app.password = password
	dir := filepath.Join(a.storeDir, a.meta.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	reset := "N"
	if a.meta.ResetOnLogon {
		reset = "Y"
	}
	hb := a.meta.HeartbeatInterval
	if hb <= 0 {
		hb = 30
	}
	// SessionQualifier is local-only (not on the wire). Required when two adapters
	// share the same BeginString/SenderCompID/TargetCompID on different ports
	// (e.g. QuikFIXAdapterService1 :15101 and Service2 :15102 both use TEST->QUIK).
	cfg := fmt.Sprintf(`[DEFAULT]
ConnectionType=initiator
ReconnectInterval=5
FileStorePath=%s
FileLogPath=%s
StartTime=00:00:00
EndTime=00:00:00
UseDataDictionary=N
ValidateFieldsOutOfOrder=N
ValidateUserDefinedFields=N
ResetOnLogon=%s
HeartBtInt=%d

[SESSION]
BeginString=%s
SenderCompID=%s
TargetCompID=%s
SessionQualifier=%s
SocketConnectHost=%s
SocketConnectPort=%d
`, filepath.ToSlash(dir), filepath.ToSlash(filepath.Join(dir, "log")), reset, hb,
		a.meta.BeginString, a.meta.SenderCompID, a.meta.TargetCompID, a.meta.ID, a.meta.Host, a.meta.Port)

	cfg += transport
	return quickfix.ParseSettings(strings.NewReader(cfg))
}

func envOrEmpty(name string) string {
	if name == "" {
		return ""
	}
	return os.Getenv(name)
}

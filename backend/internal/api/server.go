package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb"github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/executioninbox"
	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shopspring/decimal"
)

type Server struct {
	dashboardCache     snapshotCache
	monitoringCache    snapshotCache
	cfg                config.Config
	log                *slog.Logger
	pool               *pgxpool.Pool
	brokers            *brokers.Store
	orders             *orders.Service
	orderStore         *orders.Store
	adapters           *adapters.Store
	manager            *adapters.Manager
	outbox             *outbox.Store
	audit              *audit.Store
	users              *admin.UserStore
	hub                *realtime.Hub
	quikExport         *quikexport.Store
	quikOrders         quikOrderReader
	quikParams         quikParamReader
	quikRefs           quikRefReader
	quikMarketTrades   quikMarketTradeReader
	marketClasses     *quikexport.ClassSettings
	servedClasses      func(context.Context) ([]string, error)
	obs                *observability.Pipeline
	realtimeClient     pb.RealtimeServiceClient
	ratePolicies       *ratelimit.Store
	executionInbox     *executioninbox.Inbox
	inboxCases         *executioninbox.CaseStore
	// Unix seconds when the served gRPC certificate expires; 0 while unknown.
	grpcCertNotAfter atomic.Int64

	trusted            []*net.IPNet
	hmacNonces          auth.NonceStore
	maxBody             int64
	authFailLimiter     *ratelimit.SlidingWindow
	authSuccessThrottle sync.Map
}

func NewServer(
	cfg config.Config, log *slog.Logger, pool *pgxpool.Pool,
	brokerStore *brokers.Store, orderSvc *orders.Service, orderStore *orders.Store,
	adapterStore *adapters.Store, manager *adapters.Manager, outboxStore *outbox.Store,
	auditStore *audit.Store, users *admin.UserStore, hub *realtime.Hub,
	obs *observability.Pipeline,
) *Server {
	s := &Server{
		cfg: cfg, log: log, pool: pool, brokers: brokerStore, orders: orderSvc, orderStore: orderStore,
		adapters: adapterStore, manager: manager, outbox: outboxStore, audit: auditStore, users: users, hub: hub,
		obs:             obs,
		trusted:         observability.ParseTrustedProxies(cfg.TrustedProxies),
		hmacNonces:      auth.NewNonceCache(10 * time.Minute),
		maxBody:         1 << 20,
		authFailLimiter: ratelimit.NewSlidingWindow(time.Minute, cfg.AuthFailMaxPerMinute),
	}
	return s
}

func (s *Server) SetQuikExport(store *quikexport.Store) {
	s.quikExport = store
	// A typed nil *Store in the interface would defeat the `quikOrders == nil` checks.
	if store == nil {
		s.quikOrders, s.quikParams, s.quikRefs, s.quikMarketTrades = nil, nil, nil, nil
		return
	}
	s.quikOrders, s.quikParams, s.quikRefs, s.quikMarketTrades = store, store, store, store
}

func (s *Server) SetMarketDataClasses(settings *quikexport.ClassSettings) {
	s.marketClasses = settings
	s.servedClasses = settings.Classes
}

func (s *Server) SetExecutionInbox(in *executioninbox.Inbox, cases *executioninbox.CaseStore) {
	s.executionInbox, s.inboxCases = in, cases
}

func (s *Server) SetNonceStore(store auth.NonceStore) { s.hmacNonces = store }

// SetGRPCCertificateExpiry tells the safety monitor when the certificate the
// gRPC server serves expires. A renewed file only counts after a restart.
func (s *Server) SetGRPCCertificateExpiry(notAfter time.Time) { s.grpcCertNotAfter.Store(notAfter.Unix()) }

// OperationsHandler is served exclusively on the private operations listener.
// Production container probes and Prometheus do not traverse the public API's
// HTTPS/proxy chain. Deployment must not publish this listener to clients.
func (s *Server) OperationsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	metricsHandler := promhttp.Handler()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		databaseAvailable := float64(0)
		if s.pool != nil {
			probe, cancel := context.WithTimeout(r.Context(), time.Second)
			if s.pool.Ping(probe) == nil {
				databaseAvailable = 1
			}
			cancel()
		}
		metrics.DatabaseAvailable.Set(databaseAvailable)
		availableSessions, paused := 0, 0.0
		if s.manager != nil {
			availableSessions = s.manager.AvailableCount()
			if s.manager.Paused() {
				paused = 1
			}
		}
		metrics.FIXAvailable.Set(float64(availableSessions))
		metrics.FIXSessionPaused.Set(paused)
		if s.cfg.BackupStatusFile != "" {
			age := float64(1e12)
			if b, err := os.ReadFile(s.cfg.BackupStatusFile); err == nil {
				if stamp, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil {
					age = time.Since(time.Unix(stamp, 0)).Seconds()
					if age < 0 {
						age = 1e12
					}
				}
			}
			metrics.BackupAge.Set(age)
		}
		metricsHandler.ServeHTTP(w, r)
	})
	return mux
}

func (s *Server) Handler() http.Handler {
	mux := newDocMux()

	mux.Handle("GET /health", http.HandlerFunc(s.handleHealth))
	mux.Handle("GET /ready", http.HandlerFunc(s.handleReady))
	mux.Handle("GET /metrics", s.restrictInternal(promhttp.Handler()))
	mux.HandleFunc("GET /api/broker-openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		s.serveMergedOpenAPI(w, r, mux.routes, "docs/broker-openapi.yaml")
	})
	mux.HandleFunc("GET /broker-swagger", s.handleBrokerSwaggerUI)
	mux.HandleFunc("GET /docs/kse-logo.png", s.handleDocsAsset)
	mux.HandleFunc("GET /docs/logo_KSE.png", s.handleDocsAsset)
	mux.HandleFunc("GET /docs/kse-favicon.png", s.handleDocsAsset)
	mux.HandleFunc("GET /docs/swagger/{asset}", s.handleDocsAsset)

	mux.Handle("POST /api/v1/orders", s.brokerAuth(s.requirePermission(auth.PermOrdersCreate)(http.HandlerFunc(s.createOrder))))
	mux.Handle("GET /api/v1/orders", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerOrders))))
	mux.Handle("GET /api/v1/orders/active", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerActiveOrders))))
	mux.Handle("GET /api/v1/orders/{id}",s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.getBrokerOrder))))
	mux.Handle("POST /api/v1/orders/{id}/cancel", s.brokerAuth(s.requirePermission(auth.PermOrdersCancel)(http.HandlerFunc(s.cancelBrokerOrder))))
	mux.Handle("GET /api/v1/accounts", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerAccounts))))
	mux.Handle("GET /api/v1/token", s.brokerAuth(http.HandlerFunc(s.getBrokerToken)))
	mux.Handle("GET /api/v1/errors", s.brokerAuth(http.HandlerFunc(s.listErrorCodes)))
	mux.Handle("GET /api/v1/instruments",s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listInstruments))))
	mux.Handle("GET /api/v1/quotes", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerQuotes))))
	mux.Handle("GET /api/v1/orderbook",s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerOrderBook))))
	mux.Handle("GET /api/v1/market-data", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listMarketData))))
	mux.Handle("GET /api/v1/market-data/{board}/{symbol}", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.getMarketData))))
	mux.Handle("GET /api/v1/market-data/trades", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listMarketTrades))))
	mux.Handle("GET /api/v1/trades", s.brokerAuth(s.requirePermission(auth.PermTradesRead)(http.HandlerFunc(s.listTrades))))
	mux.Handle("GET /api/v1/positions/moneylimits", s.brokerAuth(s.requirePermission(auth.PermPositionsRead)(s.listBrokerPositionTable(quikexport.MoneyLimits))))
	mux.Handle("GET /api/v1/positions/depolimits", s.brokerAuth(s.requirePermission(auth.PermPositionsRead)(s.listBrokerPositionTable(quikexport.DepoLimits))))

	mux.HandleFunc("POST /api/v1/admin/login", s.adminLogin)
	mux.Handle("GET /api/v1/admin/dashboard", s.adminRO(s.dashboard))
	mux.Handle("GET /api/v1/admin/monitoring", s.adminRO(s.monitoring))
	mux.Handle("GET /api/v1/admin/grpc/connections", s.adminAudit(s.grpcConnections))
	mux.Handle("GET /api/v1/admin/monitoring/history", s.adminRO(s.monitoringHistory))
	mux.Handle("POST /api/v1/admin/brokers", s.adminWrite(s.createBroker))
	mux.Handle("GET /api/v1/admin/brokers", s.adminRO(s.listBrokers))
	mux.Handle("GET /api/v1/admin/brokers/{id}", s.adminRO(s.getBroker))
	mux.Handle("PATCH /api/v1/admin/brokers/{id}", s.adminWrite(s.patchBroker))
	mux.Handle("POST /api/v1/admin/brokers/{id}/tokens", s.adminWrite(s.createToken))
	mux.Handle("GET /api/v1/admin/tokens", s.adminRO(s.listAllTokens))
	mux.Handle("POST /api/v1/admin/tokens/search", s.adminRO(s.searchTokens))
	mux.Handle("GET /api/v1/admin/tokens/{id}", s.adminRO(s.getTokenOverview))
	mux.Handle("GET /api/v1/admin/brokers/{id}/tokens", s.adminRO(s.listTokens))
	mux.Handle("GET /api/v1/admin/brokers/{id}/security", s.adminRO(s.brokerSecurity))
	mux.Handle("GET /api/v1/admin/brokers/{id}/stats", s.adminRO(s.brokerStats))
	mux.Handle("GET /api/v1/admin/brokers/{id}/ip-whitelist", s.adminRO(s.listIPWhitelist))
	mux.Handle("POST /api/v1/admin/brokers/{id}/ip-whitelist", s.adminWrite(s.addIPWhitelist))
	mux.Handle("DELETE /api/v1/admin/brokers/{id}/ip-whitelist/{entry_id}", s.adminWrite(s.removeIPWhitelist))
	mux.Handle("GET /api/v1/admin/brokers/{id}/accounts", s.adminRO(s.listAccounts))
	mux.Handle("DELETE /api/v1/admin/tokens/{id}", s.adminWrite(s.revokeToken))
	mux.Handle("POST /api/v1/admin/tokens/{id}/revoke", s.adminWrite(s.revokeToken))
	mux.Handle("POST /api/v1/admin/tokens/{id}/rotate", s.adminWrite(s.rotateToken))
	mux.Handle("PATCH /api/v1/admin/tokens/{id}", s.adminWrite(s.patchToken))
	mux.Handle("GET /api/v1/admin/orders", s.adminRO(s.listAdminOrders))
	mux.Handle("GET /api/v1/admin/reconciliation", s.adminRO(s.listReconciliation))
	mux.Handle("POST /api/v1/admin/reconciliation/{id}/resolve", s.adminOps(s.resolveReconciliation))
	mux.Handle("GET /api/v1/admin/execution-inbox", s.adminRO(s.getExecutionInbox))
	mux.Handle("POST /api/v1/admin/execution-inbox/{id}/quarantine", s.adminOps(s.quarantineExecutionInboxEntry))
	mux.Handle("POST /api/v1/admin/execution-inbox/cases/{id}/resolve", s.adminOps(s.resolveExecutionInboxCase))
	mux.Handle("GET /api/v1/admin/rate-limits", s.adminRO(s.listRateLimits))
	mux.Handle("GET /api/v1/admin/trading-limits", s.adminRO(s.listTradingLimits))
	mux.Handle("GET /api/v1/admin/trading-limits/history", s.adminAudit(s.tradingLimitHistory))
	mux.Handle("PUT /api/v1/admin/trading-limits", s.adminWrite(s.updateTradingLimit))
	mux.Handle("PUT /api/v1/admin/rate-limits", s.adminWrite(s.updateRateLimit))
	mux.Handle("GET /api/v1/admin/orders/{id}", s.adminRO(s.getAdminOrder))
	mux.Handle("GET /api/v1/admin/emitents/{symbol}/orders", s.adminRO(s.listEmitentOrders))
	mux.Handle("GET /api/v1/admin/trades", s.adminRO(s.listAdminTrades))
	mux.Handle("GET /api/v1/admin/positions", s.adminRO(s.listAdminPositions))
	mux.Handle("GET /api/v1/admin/market-data", s.adminRO(s.listMarketData))
	mux.Handle("GET /api/v1/admin/market-data/{board}/{symbol}", s.adminRO(s.getMarketData))
	mux.Handle("GET /api/v1/admin/market-data/{board}/{symbol}/details", s.adminRO(s.getMarketInstrument))
	mux.Handle("GET /api/v1/admin/quotes", s.adminRO(s.listAdminQuotes))
	mux.Handle("GET /api/v1/admin/market-data/settings", s.adminRO(s.getMarketDataSettings))
	mux.Handle("PATCH /api/v1/admin/market-data/settings", s.adminOps(s.patchMarketDataSettings))
	mux.Handle("GET /api/v1/admin/classes", s.adminRO(s.listQuikClasses))
	mux.Handle("GET /api/v1/admin/adapters", s.adminRO(s.listAdapters))
	mux.Handle("POST /api/v1/admin/adapters", s.adminWrite(s.createAdapter))
	mux.Handle("PATCH /api/v1/admin/adapters/{id}", s.adminWrite(s.patchAdapter))
	mux.Handle("POST /api/v1/admin/adapters/{id}/enable", s.adminOps(s.enableAdapter))
	mux.Handle("POST /api/v1/admin/adapters/{id}/disable", s.adminOps(s.disableAdapter))
	mux.Handle("POST /api/v1/admin/adapters/{id}/restart", s.adminOps(s.restartAdapter))
	mux.Handle("GET /api/v1/admin/audit", s.adminAudit(s.listAudit))
	mux.Handle("GET /api/v1/admin/logs", s.adminAudit(s.listLogs))
	mux.Handle("GET /api/v1/admin/logs/export", s.adminAudit(s.exportLogs))
	mux.Handle("GET /api/v1/admin/logs/export/{id}", s.adminAudit(s.getExportJob))
	mux.Handle("GET /api/v1/admin/logs/export/{id}/download", s.adminAudit(s.downloadExportJob))
	mux.Handle("GET /api/v1/admin/logs/{id}", s.adminAudit(s.getLog))
	mux.Handle("GET /api/v1/admin/brokers/{id}/logs", s.adminAudit(s.listBrokerLogs))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics", s.adminRO(s.brokerStatistics))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics/daily", s.adminRO(s.brokerStatsDaily))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics/weekly", s.adminRO(s.brokerStatsWeekly))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics/monthly", s.adminRO(s.brokerStatsMonthly))
	mux.Handle("GET /api/v1/admin/errors", s.adminAudit(s.listErrors))
	mux.Handle("GET /api/v1/admin/events", s.adminAudit(s.listEvents))
	mux.Handle("GET /api/v1/admin/obs-audit", s.adminAudit(s.listObsAudit))
	mux.Handle("GET /api/v1/admin/notifications", s.adminRO(s.listNotifications))
	mux.Handle("POST /api/v1/admin/notifications/read-all", s.adminRO(s.readAllNotifications))
	mux.Handle("POST /api/v1/admin/notifications/{id}/read", s.adminRO(s.readNotification))
	mux.Handle("GET /api/v1/admin/orders/{id}/trace", s.adminRO(s.orderTrace))
	mux.Handle("GET /api/v1/admin/users", s.adminUsers(s.listUsers))
	mux.Handle("GET /api/v1/admin/instruments", s.adminRO(s.listInstruments))
	mux.Handle("GET /api/v1/admin/firms", s.adminRO(s.listQuikFirms))
	mux.Handle("GET /api/v1/admin/events/stream", s.adminRO(s.streamAdminEvents))

	h := s.protectOverload(http.Handler(mux))
	h = metrics.Middleware(h)
	if s.obs != nil {
		h = s.obs.HTTPMiddleware(observability.HTTPConfig{
			TrustedProxies: s.trusted,
			FastMS:         s.cfg.ObsFastMS,
			NormalMS:       s.cfg.ObsNormalMS,
			SlowMS:         s.cfg.ObsSlowMS,
			SkipOrigins:    s.cfg.ObsSkipOrigins,
		}, h)
	}
	h = observability.RequestIDMiddleware(h)
	return s.brokerEnvelope(s.requireHTTPS(s.cors(s.limitBody(h))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	dbOK := s.pool.Ping(r.Context()) == nil
	pending, _ := s.outbox.PendingCount(r.Context())
	available := s.manager.AvailableCount()
	ready := dbOK && available > 0
	inboxHealthy := true
	inboxPending := 0
	if s.executionInbox != nil {
		state := s.executionInbox.Status()
		inboxHealthy = state.Healthy()
		inboxPending = state.Pending
		ready = ready && inboxHealthy
	}
	status := "ready"
	code := http.StatusOK
	if !ready {
		status = "not_ready"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status": status, "database": dbOK, "queue": dbOK,
		"outbox_pending": pending, "adapters_available": available,
		"execution_inbox_pending": inboxPending, "execution_inbox_healthy": inboxHealthy,
	})
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	if s.executionInbox != nil && !s.executionInbox.Status().Healthy() {
		writeErr(w, http.StatusServiceUnavailable, "execution recovery in progress; new trading is paused")
		return
	}
	b, _ := BrokerFromContext(r.Context())
	tok, _ := TokenFromContext(r.Context())
	var req orders.CreateOrderRequest
	if err := decodeJSONBody(r, &req); err != nil {
		observability.AnnotateError(r, "INVALID_JSON", err.Error(), "api")
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ip, _ := s.clientIP(r)
	order, err := s.orders.Create(r.Context(), b, ip, req, tok.QuikFirm)
	if err != nil {
		var risk *orders.RiskRejected
		if errors.As(err, &risk) {
			observability.AnnotateError(r, risk.Code, risk.Message, "risk")
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": risk.Message, "code": risk.Code})
			return
		}
		if errors.Is(err, orders.ErrPersistenceUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "order storage unavailable; check GET /api/v1/orders before sending the order again")
			return
		}
		observability.AnnotateError(r, "ORDER_CREATE_FAILED", err.Error(), "order_service")
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			s.writeInternal(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	qty := order.Quantity.String()
	price := ""
	if order.Price != nil {
		price = order.Price.String()
	}
	observability.AnnotateOrder(r, order.ID, order.ClientOrderID, req.AccountID, order.Symbol, string(order.Side), qty, price)
	metrics.OrdersReceived.Inc()
	order, err = s.awaitOrderAnswer(r.Context(), order, orders.AwaitingVenue)
	if err != nil {
		observability.AnnotateError(r, "ORDER_STATUS_UNAVAILABLE", err.Error(), "order_service")
		body := orderReply(order)
		body["code"], body["error"] = "QUIK_ANSWER_UNKNOWN", "order is stored; its QUIK status could not be read, check GET /api/v1/orders/{id}"
		writeJSON(w, http.StatusAccepted, body)
		return
	}
	writeOrderAnswer(w, r, order)
}

// awaitOrderAnswer holds the reply until QUIK answers the order or cancel,
// leaving the request a margin to write the reply before its deadline.
func (s *Server) awaitOrderAnswer(ctx context.Context, order domain.Order, pending func(domain.OrderStatus) bool) (domain.Order, error) {
	wait := s.cfg.OrderAnswerTimeout
	if wait <= 0 {
		return order, nil
	}
	if dl, ok := ctx.Deadline(); ok {
		wait = min(wait, time.Until(dl)-time.Second)
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	return s.orders.AwaitVenueAnswer(ctx, order, pending)
}

// writeOrderAnswer: 201 accepted by QUIK, 422 rejected by QUIK, 502 not
// delivered, 202 QUIK has not answered in time (the order stays live).
// A reject or a failed delivery is recorded in the request's event with
// QUIK's own text, so the event log shows why the order did not pass.
func writeOrderAnswer(w http.ResponseWriter, r *http.Request, order domain.Order) {
	body := orderReply(order)
	switch {
	case orders.AwaitingVenue(order.Status):
		body["code"] = "QUIK_ANSWER_PENDING"
		body["error"] = "QUIK has not answered yet; the order is not cancelled, check GET /api/v1/orders/{id}"
		writeJSON(w, http.StatusAccepted, body)
	case order.Status == domain.OrderRejected:
		body["code"] = "ORDER_REJECTED"
		body["error"] = "rejected by QUIK"
		if text := quikRejectText(order.RejectReason); text != "" {
			body["error"] = text
		}
		body["error_code"] = quikRejectCode(order.RejectReason, "OrdRejReason=")
		observability.AnnotateError(r, "ORDER_REJECTED", rejectEventMessage(body["error"], body["error_code"]), "quik")
		writeJSON(w, http.StatusUnprocessableEntity, body)
	case order.Status == domain.OrderFailed:
		body["code"] = "ORDER_FAILED"
		body["error"] = "order was not delivered to QUIK"
		observability.AnnotateError(r, "ORDER_FAILED", "order was not delivered to QUIK", "fix")
		writeJSON(w, http.StatusBadGateway, body)
	default:
		writeJSON(w, http.StatusCreated, body)
	}
}

// orderReply is the POST /orders answer in QUIK Orders column names; numbers
// are JSON numbers as in Orders rows. id is the gateway key for GET/cancel
// while OrderNum is still unknown.
func orderReply(o domain.Order) map[string]any {
	num := func(d decimal.Decimal) json.Number { return json.Number(d.String()) }
	var price, orderNum any
	if o.Price != nil {
		price = num(*o.Price)
	}
	// QUIK reports 37=0 on a rejected order: there is no QUIK order number.
	if o.QuikOrderID != nil && strings.TrimLeft(*o.QuikOrderID, "0 ") != "" {
		orderNum = *o.QuikOrderID
		if n, err := decimal.NewFromString(*o.QuikOrderID); err == nil && n.IsInteger() {
			orderNum = json.Number(*o.QuikOrderID)
		}
	}
	operation := orders.OperationBuy
	if o.Side == domain.SideSell {
		operation = orders.OperationSell
	}
	var rejectReason any
	if text := quikRejectText(o.RejectReason); text != "" {
		rejectReason = text
	}
	return map[string]any{
		"OrderNum": orderNum, "State": quikOrderState(o.Status),
		"ClassCode": o.Board, "SecCode": o.Symbol, "Operation": operation,
		"Price": price, "Qty": num(o.Quantity), "Balance": num(o.Quantity.Sub(o.FilledQuantity)),
		"Account": o.TradingAccount, "ClientCode": o.ClientCode, "FirmId": o.QuikFirm,
		"RejectReason": rejectReason, "id": o.ID,
	}
}

// rejectCodeMarkers are the FIX reason codes the gateway appends to QUIK's
// reject text: OrdRejReason (103) on orders, CxlRejReason (102) on cancels.
var rejectCodeMarkers = []string{"OrdRejReason=", "CxlRejReason="}

// quikRejectText is QUIK's own reject text without the appended FIX code.
func quikRejectText(reason *string) string {
	if reason == nil {
		return ""
	}
	text := *reason
	for _, marker := range rejectCodeMarkers {
		if i := strings.Index(text, marker); i >= 0 {
			text = text[:i]
		}
	}
	return strings.TrimRight(strings.TrimSpace(text), ";")
}

// cancelRejectReason stores QUIK's cancel-reject text with its FIX 102 code
// the way order rejects keep 103.
func cancelRejectReason(text, cxlRejReason string) string {
	parts := make([]string, 0, 2)
	if t := strings.TrimSpace(text); t != "" {
		parts = append(parts, t)
	}
	if c := strings.TrimSpace(cxlRejReason); c != "" {
		parts = append(parts, "CxlRejReason="+c)
	}
	return strings.Join(parts, "; ")
}

// quikRejectCode is the QUIK error code (see GET /api/v1/errors) as a JSON
// number, or nil when QUIK sent none.
func quikRejectCode(reason *string, marker string) any {
	if reason == nil {
		return nil
	}
	i := strings.Index(*reason, marker)
	if i < 0 {
		return nil
	}
	code := strings.TrimSpace((*reason)[i+len(marker):])
	if j := strings.IndexAny(code, "; "); j >= 0 {
		code = code[:j]
	}
	if _, err := strconv.ParseInt(code, 10, 64); err != nil {
		return nil
	}
	return json.Number(code)
}

// quikOrderState names the gateway status as QUIK Orders.State does; nil
// while QUIK has not answered or the order never reached it.
func quikOrderState(st domain.OrderStatus) any {
	switch st {
	case domain.OrderAcknowledged, domain.OrderPartiallyFilled, domain.OrderCancelPending:
		return "Активна"
	case domain.OrderFilled:
		return "Исполнена"
	case domain.OrderCancelled, domain.OrderExpired:
		return "Снята"
	case domain.OrderRejected:
		return "Отклонена"
	}
	return nil
}

// listBrokerOrders serves the token firm's Orders rows; date (YYYY-MM-DD),
// board and symbol narrow them before pagination.
func (s *Server) listBrokerOrders(w http.ResponseWriter, r *http.Request) {
	day, board, symbol, ok := rowFilterParams(w, r, true)
	if !ok {
		return
	}
	s.listQuikOrdersMatching(w, r, false, quikexport.OrderFilter{TradeDay: day, ClassCode: board, SecCode: symbol})
}

// listBrokerActiveOrders serves today's active rows; board and symbol narrow them.
func (s *Server) listBrokerActiveOrders(w http.ResponseWriter, r *http.Request) {
	_, board, symbol, ok := rowFilterParams(w, r, false)
	if !ok {
		return
	}
	s.listQuikOrdersMatching(w, r, false, quikexport.OrderFilter{State: quikActiveState, Today: true, ClassCode: board, SecCode: symbol})
}

// rowFilterParams reads the optional board and symbol filters of the broker
// order and trade lists and, when withDate, the date filter (YYYY-MM-DD,
// TradeDate). A malformed date is answered with 400 and ok=false.
func rowFilterParams(w http.ResponseWriter, r *http.Request, withDate bool) (day, board, symbol string, ok bool) {
	q := r.URL.Query()
	if withDate {
		day = strings.TrimSpace(q.Get("date"))
		if day != "" {
			if _, err := time.Parse("2006-01-02", day); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid date: expected YYYY-MM-DD")
				return "", "", "", false
			}
		}
	}
	return day, strings.TrimSpace(q.Get("board")), strings.TrimSpace(q.Get("symbol")), true
}

func (s *Server) getBrokerOrder(w http.ResponseWriter, r *http.Request) {
	s.getQuikOrder(w, r, false)
}

func (s *Server) cancelBrokerOrder(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	firm, brokerID, err := s.orderReadScope(r, false)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	// An active order stays cancellable after its class leaves the served choice.
	current, local, err := s.currentQuikOrder(quikexport.WithAllClasses(r.Context()), r.PathValue("id"), firm, brokerID, true)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	state := strings.ToUpper(current.Text("State"))
	if state == "СНЯТА" || state == "CANCELLED" || state == "CANCELED" {
		writeJSON(w, http.StatusOK, current)
		return
	}
	if state != "АКТИВНА" && state != "ACTIVE" {
		writeErr(w, http.StatusConflict, "QUIK order is not active")
		return
	}
	if local == nil {
		writeErr(w, http.StatusConflict, "cancellation requires an original Gateway request")
		return
	}
	id := local.ID
	ip, _ := s.clientIP(r)
	requested := time.Now()
	order, err := s.orders.Cancel(r.Context(), b.ID, id, ip)
	if err != nil {
		if errors.Is(err, orders.ErrPersistenceUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "order storage unavailable; verify cancellation result and retry later")
			return
		}
		observability.AnnotateError(r, "ORDER_CANCEL_FAILED", err.Error(), "order_service")
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			s.writeInternal(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	observability.AnnotateOrder(r, order.ID, order.ClientOrderID, order.TradingAccount, order.Symbol, string(order.Side), order.Quantity.String(), "")
	if waited, err := s.awaitOrderAnswer(r.Context(), order, orders.AwaitingCancel); err == nil {
		order = waited
	}
	var rejectReason string
	if s.orderStore != nil && (order.Status == domain.OrderAcknowledged || order.Status == domain.OrderPartiallyFilled) {
		// The clock margin keeps a reject written by the database within the window.
		rejectReason, _ = s.orderStore.CancelRejectReason(r.Context(), order.ID, requested.Add(-2*time.Second))
	}
	writeCancelAnswer(w, r, order, rejectReason)
}

// rejectEventMessage is the event-log text of a QUIK refusal: the reply's
// error text and, when QUIK sent one, its reason code.
func rejectEventMessage(text, code any) string {
	if code == nil {
		return fmt.Sprint(text)
	}
	return fmt.Sprintf("%v (код QUIK %v)", text, code)
}

// writeCancelAnswer: 200 cancelled by QUIK, 409 QUIK refused the cancel (the
// order stays active or was filled), 202 QUIK has not answered in time.
// rejectReason is QUIK's cancel-reject text with its CxlRejReason code.
func writeCancelAnswer(w http.ResponseWriter, r *http.Request, order domain.Order, rejectReason string) {
	body := orderReply(order)
	switch order.Status {
	case domain.OrderCancelled, domain.OrderExpired:
		writeJSON(w, http.StatusOK, body)
	case domain.OrderCancelPending:
		body["code"] = "QUIK_ANSWER_PENDING"
		body["error"] = "QUIK has not answered the cancel yet; check GET /api/v1/orders/{id}"
		writeJSON(w, http.StatusAccepted, body)
	default:
		body["code"] = "CANCEL_REJECTED"
		body["error"] = "QUIK did not cancel the order"
		if text := quikRejectText(&rejectReason); text != "" {
			body["error"] = text
		}
		body["error_code"] = quikRejectCode(&rejectReason, "CxlRejReason=")
		observability.AnnotateError(r, "CANCEL_REJECTED", rejectEventMessage(body["error"], body["error_code"]), "quik")
		writeJSON(w, http.StatusConflict, body)
	}
}

// listInstruments serves the current QUIK Securities rows of the served classes
// as stored, to brokers and administrators alike, paged like the Orders list.
func (s *Server) listInstruments(w http.ResponseWriter, r *http.Request) {
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	if s.quikRefs == nil {
		s.writeQuikRefError(w, errQuikRefsUnavailable)
		return
	}
	page, err := s.quikRefs.ListSecurities(r.Context(), quikexport.SecurityFilter{Limit: limit, Offset: offset})
	if err != nil {
		s.writeQuikRefError(w, err)
		return
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(page.HasMore))
	w.Header().Set("X-Next-Offset", strconv.Itoa(page.NextOffset))
	writeQuikJSON(w, page.Securities)
}

// listBrokerOrderBook serves one instrument's book (поручение пользователя
// 05.11.2026): today's active QUIK Orders of every firm grouped by price, as
// the quotes are; participants stay hidden. A class outside the served ones
// has an empty book.
func (s *Server) listBrokerOrderBook(w http.ResponseWriter, r *http.Request) {
	board := strings.TrimSpace(r.URL.Query().Get("board"))
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if board == "" || symbol == "" {
		writeErr(w, http.StatusBadRequest, "board and symbol are required")
		return
	}
	served, err := s.servedClassSet(r.Context())
	if err != nil {
		s.writeParamReadError(w, err)
		return
	}
	book := quikexport.OrderBook{Bids: []quikexport.BookLevel{}, Offers: []quikexport.BookLevel{}}
	if served == nil || served[board] {
		if s.quikOrders == nil {
			s.writeOrderReadError(w, quikexport.ErrNotConfigured)
			return
		}
		if book, err = s.quikOrders.GetOrderBook(r.Context(), board, symbol, quikActiveState); err != nil {
			s.writeOrderReadError(w, err)
			return
		}
	}
	writeQuikJSON(w, struct {
		Board  string `json:"board"`
		Symbol string `json:"symbol"`
		quikexport.OrderBook
	}{board, symbol, book})
}

// listTrades serves the token firm's rows of QUIK "Trades" as stored, newest
// first, paged like the Orders list.
func (s *Server) listTrades(w http.ResponseWriter, r *http.Request) {
	s.listQuikTrades(w, r, false)
}

// listAdminTrades serves the same rows of every firm; firm_id and broker_id narrow them.
func (s *Server) listAdminTrades(w http.ResponseWriter, r *http.Request) {
	s.listQuikTrades(w, r, true)
}

func (s *Server) listQuikTrades(w http.ResponseWriter, r *http.Request, admin bool) {
	firm, _, err := s.orderReadScope(r, admin)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	filter := quikexport.TradeFilter{Firm: firm}
	if !admin {
		// The admin page keeps its own firm_id/broker_id selection only.
		var ok bool
		if filter.TradeDay, filter.ClassCode, filter.SecCode, ok = rowFilterParams(w, r, true); !ok {
			return
		}
	}
	limit, offset, ok := pageParams(w, r)
	if !ok {
		return
	}
	if s.quikOrders == nil {
		s.writeTradeReadError(w, quikexport.ErrNotConfigured)
		return
	}
	filter.Limit, filter.Offset = limit, offset
	page, err := s.quikOrders.ListTrades(r.Context(), filter)
	if err != nil {
		s.writeTradeReadError(w, err)
		return
	}
	w.Header().Set("X-Has-More", strconv.FormatBool(page.HasMore))
	w.Header().Set("X-Next-Offset", strconv.Itoa(page.NextOffset))
	writeQuikJSON(w, page.Trades)
}

func (s *Server) writeTradeReadError(w http.ResponseWriter, err error) {
	if s.log != nil {
		s.log.Error("QUIK Trades read failed", "error", err)
	}
	writeErr(w, http.StatusServiceUnavailable, "QUIK Trades source unavailable")
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.clientIP(r)
	if s.authFailLimiter.Blocked(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many failed authentication attempts")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.users.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, admin.ErrAuthenticationUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "authentication temporarily unavailable")
			return
		}
		s.authFailLimiter.Record(ip)
		s.emitObs(observability.Event{
			Type: observability.TypeAuth, Subtype: observability.AuthFailed, ClientIP: ip,
			Endpoint: r.URL.Path, HTTPMethod: r.Method, RequestID: observability.RequestIDFrom(r.Context()),
			ErrorCode: observability.AuthFailed, ErrorMessage: "invalid credentials", FailureStage: "auth",
			Message: "admin login failed",
		})
		writeErr(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	token, err := admin.IssueToken(s.cfg.JWTSecret, u)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  map[string]any{"id": u.ID, "username": u.Username, "role": u.RoleCode, "full_name": u.FullName},
	})
	s.emitObs(observability.Event{
		Type: observability.TypeAuth, Subtype: observability.AuthSuccess, ClientIP: ip,
		Endpoint: r.URL.Path, HTTPMethod: r.Method, RequestID: observability.RequestIDFrom(r.Context()),
		ActorType: "admin", ActorID: u.ID.String(), Message: "admin login",
	})
	s.writeAdminAudit(r, "USER_LOGIN", "user", u.ID.String(), nil, nil, map[string]any{"username": u.Username})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.dashboardCache.get(r.Context(), 5*time.Second, func(ctx context.Context) (any, error) {
		snapshot, err := s.dashboardSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		ads, err := s.manager.ListRuntime(ctx)
		if err != nil {
			return nil, err
		}
		snapshot["adapters"] = ads
		snapshot["adapters_available"] = s.manager.AvailableCount()
		// The log only sees logged write requests; presence must match the
		// Brokers page (token use and live gRPC streams).
		live := s.connectedBrokers(ctx)
		snapshot["connected_brokers"] = live
		if o, ok := snapshot["observability"].(map[string]any); ok {
			o["connected_now"] = len(live)
		}
		snapshot["system"] = map[string]any{"api": "ONLINE", "database": "ONLINE", "queue": "ONLINE"}
		return snapshot, nil
	})
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// Current order counters must come from the same source as the order list; when
	// that source is down only the counters are missing, never substituted.
	response := map[string]any{}
	for key, value := range data.(map[string]any) {
		response[key] = value
	}
	response["orders_source"] = "quik_db"
	err = quikexport.ErrNotConfigured
	if s.quikOrders != nil {
		var byState map[string]int
		if byState, err = s.quikOrders.CountOrdersByState(r.Context()); err == nil {
			response["stats"] = quikOrderStats(byState)
		}
	}
	if err != nil {
		if s.log != nil {
			s.log.Warn("dashboard QUIK Orders counters unavailable", "error", err)
		}
		response["stats"] = nil
		response["orders_source_error"] = "QUIK Orders source unavailable"
	}
	writeJSON(w, http.StatusOK, response)
}

// dashboardSnapshot counts today's activity from today's log file.
func (s *Server) dashboardSnapshot(ctx context.Context) (map[string]any, error) {
	var brokerCount int64
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM brokers`).Scan(&brokerCount); err != nil {
		return nil, err
	}
	snapshot := map[string]any{"brokers": brokerCount, "generated_at": time.Now().UTC()}
	overview := observability.OverviewStats{SuccessRate: 100}
	topBrokers, topErrors := []observability.TopBroker{}, []observability.ErrorCount{}
	if s.obs != nil {
		store := s.obs.Store()
		var err error
		if overview, err = store.Overview(ctx); err != nil {
			return nil, err
		}
		today := observability.StartOfDay(time.Now())
		if topBrokers, err = store.TopBrokers(ctx, today, 10); err != nil {
			return nil, err
		}
		if topErrors, err = store.TopErrors(ctx, today, 10); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(overview)
	if err != nil {
		return nil, err
	}
	var obs map[string]any
	if err := json.Unmarshal(raw, &obs); err != nil {
		return nil, err
	}
	snapshot["observability"] = obs
	snapshot["top_brokers"] = topBrokers
	snapshot["top_errors"] = topErrors
	return snapshot, nil
}
func (s *Server) connectedBrokers(ctx context.Context) []observability.ConnectedBroker {
	list := []observability.ConnectedBroker{}
	if s.obs != nil {
		if recent, err := s.obs.Store().ConnectedBrokers(ctx, observability.ConnectedWindow); err == nil {
			list = append(list, recent...)
		}
	}
	if s.hub != nil {
		index := map[int64]int{}
		for i, c := range list {
			index[c.BrokerID] = i
		}
		for _, c := range s.hub.Connections().Connections {
			if c.BrokerID <= 0 || c.Transport != "gRPC" {
				continue
			}
			i, exists := index[c.BrokerID]
			if !exists {
				i = len(list)
				index[c.BrokerID] = i
				list = append(list, observability.ConnectedBroker{BrokerID: c.BrokerID, BrokerCode: c.BrokerCode, BrokerName: c.BrokerName})
			}
			list[i].LastActivity = time.Now().UTC()
			list[i].LastIP = c.Peer
		}
	}
	return list
}

func boolStatus(ok bool) string {
	if ok {
		return "ONLINE"
	}
	return "OFFLINE"
}

func (s *Server) createBroker(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code, Name, Description string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	b, err := s.brokers.CreateBroker(r.Context(), req.Code, req.Name, req.Description)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAdminAudit(r, "BROKER_CREATED", "broker", fmtInt(b.ID), &b.ID, nil, map[string]any{"code": b.Code, "name": b.Name})
	writeJSON(w, http.StatusCreated, map[string]any{"broker": b})
}

func (s *Server) listBrokers(w http.ResponseWriter, r *http.Request) {
	list, err := s.brokers.ListBrokers(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	live := map[int64]observability.ConnectedBroker{}
	for _, c := range s.connectedBrokers(r.Context()) {
		live[c.BrokerID] = c
	}
	out := make([]map[string]any, 0, len(list))
	for _, b := range list {
		item := map[string]any{
			"id": b.ID, "code": b.Code, "name": b.Name, "description": b.Description,
			"status": b.Status, "created_at": b.CreatedAt, "updated_at": b.UpdatedAt,
			"connected": false,
		}
		if c, ok := live[b.ID]; ok {
			item["connected"] = true
			item["last_activity"] = c.LastActivity
			item["last_ip"] = c.LastIP
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"brokers": out})
}

func (s *Server) getBroker(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	b, err := s.brokers.GetBroker(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"broker": b})
}

func (s *Server) patchBroker(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name                  *string              `json:"name"`
		Description           *string              `json:"description"`
		Status                *domain.BrokerStatus `json:"status"`
		IPWhitelistEnabled    *bool                `json:"ip_whitelist_enabled"`
		RateLimitRPS          *int                 `json:"rate_limit_rps"`
		RateLimitOrdersPerSec *int                 `json:"rate_limit_orders_per_sec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	b, err := s.brokers.UpdateBrokerExt(r.Context(), id, brokers.BrokerUpdate{
		Name: req.Name, Description: req.Description, Status: req.Status,
		IPWhitelistEnabled: req.IPWhitelistEnabled,
		RateLimitRPS:       req.RateLimitRPS, RateLimitOrdersPerSec: req.RateLimitOrdersPerSec,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAdminAudit(r, "BROKER_UPDATED", "broker", r.PathValue("id"), &id, nil, map[string]any{"name": b.Name, "status": b.Status})
	writeJSON(w, http.StatusOK, map[string]any{"broker": b})
}

func (s *Server) createToken(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Name          string     `json:"name"`
		Description   string     `json:"description"`
		QuikFirm      string     `json:"quik_firm"`
		ExpiresInDays *int       `json:"expires_in_days"`
		ExpiresAt     *time.Time `json:"expires_at"`
		Permissions   []string   `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !s.checkTokenFirm(w, r, req.QuikFirm) {
		return
	}
	var createdBy *uuid.UUID
	if admin, ok := AdminFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(admin.UserID); err == nil {
			createdBy = &uid
		}
	}
	created, err := s.brokers.CreateTokenExt(r.Context(), brokers.CreateTokenInput{
		BrokerID: id, Name: req.Name, Description: req.Description, QuikFirm: req.QuikFirm,
		ExpiresInDays: req.ExpiresInDays, ExpiresAt: req.ExpiresAt,
		Permissions: req.Permissions, CreatedBy: createdBy,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t := created.Token
	bid := t.BrokerID
	tid := t.ID
	s.auditToken(r, "TOKEN_CREATED", &bid, &tid, "ok", "")
	s.writeAdminAudit(r, "TOKEN_CREATED", "api_token", t.ID.String(), &bid, nil, map[string]any{"name": t.Name, "token_prefix": t.TokenPrefix})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": t.ID, "name": t.Name, "token": created.Raw, "expires_at": t.ExpiresAt,
		"token_meta": safeToken(t),
	})
}

func (s *Server) listTokens(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := s.brokers.ListTokens(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	safe := make([]map[string]any, 0, len(list))
	for _, t := range list {
		safe = append(safe, safeToken(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": safe})
}

func (s *Server) listAllTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.brokers.ListAllTokens(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	safe := make([]map[string]any, 0, len(list))
	for _, row := range list {
		item := safeToken(row.Token)
		item["broker_code"] = row.BrokerCode
		item["broker_name"] = row.BrokerName
		safe = append(safe, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tokens": safe})
}

func safeToken(t domain.APIToken) map[string]any {
	return map[string]any{
		"id": t.ID, "broker_id": t.BrokerID, "name": t.Name, "description": t.Description,
		"token_prefix": t.TokenPrefix, "status": t.Status, "permissions": t.Permissions,
		"quik_firm":  t.QuikFirm,
		"expires_at": t.ExpiresAt, "created_at": t.CreatedAt,
		"revoked_at": t.RevokedAt, "last_used_at": t.LastUsedAt, "last_used_ip": t.LastUsedIP,
		"rotated_from": t.RotatedFrom,
	}
}

func (s *Server) patchToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		QuikFirm *string `json:"quik_firm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.QuikFirm == nil {
		writeErr(w, http.StatusBadRequest, "quik_firm required")
		return
	}
	if !s.checkTokenFirm(w, r, *req.QuikFirm) {
		return
	}
	t, err := s.brokers.UpdateTokenQuikFirm(r.Context(), id, *req.QuikFirm)
	if err != nil {
		if err.Error() == "quik_firm is required" {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	bid := t.BrokerID
	s.auditToken(r, "TOKEN_UPDATED", &bid, &id, "ok", "quik_firm")
	writeJSON(w, http.StatusOK, map[string]any{"token_meta": safeToken(t)})
}

func (s *Server) revokeToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	tok, err := s.brokers.GetToken(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	var revokedBy *uuid.UUID
	if admin, ok := AdminFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(admin.UserID); err == nil {
			revokedBy = &uid
		}
	}
	if err := s.brokers.RevokeTokenBy(r.Context(), id, revokedBy); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	bid := tok.BrokerID
	s.auditToken(r, "TOKEN_REVOKED", &bid, &id, "ok", "")
	s.writeAdminAudit(r, "TOKEN_REVOKED", "api_token", id.String(), &bid, nil, map[string]any{"id": id.String()})
	writeJSON(w, http.StatusOK, map[string]any{"status": "REVOKED", "id": id})
}

func (s *Server) rotateToken(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	var createdBy *uuid.UUID
	if admin, ok := AdminFromContext(r.Context()); ok {
		if uid, err := uuid.Parse(admin.UserID); err == nil {
			createdBy = &uid
		}
	}
	created, err := s.brokers.RotateToken(r.Context(), id, createdBy)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	t := created.Token
	bid := t.BrokerID
	tid := t.ID
	s.auditToken(r, "TOKEN_ROTATED", &bid, &tid, "ok", "old_token_still_active="+id.String())
	s.writeAdminAudit(r, "TOKEN_ROTATED", "api_token", t.ID.String(), &bid, nil, map[string]any{"old_token_id": id.String(), "new_token_prefix": t.TokenPrefix})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": t.ID, "name": t.Name, "token": created.Raw, "expires_at": t.ExpiresAt,
		"old_token_id": id, "old_token_status": "ACTIVE",
		"message":    "Old token remains ACTIVE. Revoke it after the broker switches to the new token.",
		"token_meta": safeToken(t),
	})
}

func (s *Server) brokerSecurity(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	st, err := s.brokers.SecurityStats(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"security": st})
}

func (s *Server) brokerStats(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	b, err := s.brokers.GetBroker(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	firms, err := s.brokerFirms(r.Context(), b)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if s.quikRefs == nil {
		s.writeOrderReadError(w, quikexport.ErrNotConfigured)
		return
	}
	st, err := brokerOrderStats(r.Context(), s.quikRefs, firms, days, time.Now())
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": st})
}

func (s *Server) listIPWhitelist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := s.brokers.ListIPWhitelist(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	b, _ := s.brokers.GetBroker(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"ip_whitelist_enabled": b.IPWhitelistEnabled,
		"entries":              list,
	})
}

func (s *Server) addIPWhitelist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		IP   string `json:"ip"`
		Note string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	entry, err := s.brokers.AddIPWhitelist(r.Context(), id, req.IP, req.Note)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"entry": entry})
}

func (s *Server) removeIPWhitelist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	entryID, _ := strconv.ParseInt(r.PathValue("entry_id"), 10, 64)
	if err := s.brokers.RemoveIPWhitelist(r.Context(), id, entryID); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "removed"})
}

// listAdminOrders serves the admin "Заявки" page: only today's QUIK Orders
// rows still in the book (State = "Активна", TradeDate = today in Bishkek),
// as stored; broker_id narrows them to one firm.
func (s *Server) listAdminOrders(w http.ResponseWriter, r *http.Request) {
	s.listQuikOrdersMatching(w, r, true, quotesFilter("", ""))
}

func (s *Server) listEmitentOrders(w http.ResponseWriter, r *http.Request) {
	if s.quikExport == nil {
		writeErr(w, http.StatusServiceUnavailable, quikexport.ErrNotConfigured.Error())
		return
	}
	res, err := s.quikExport.OrdersBySymbol(r.Context(), r.PathValue("symbol"))
	if err != nil {
		switch {
		case errors.Is(err, quikexport.ErrBadSymbol):
			writeErr(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, quikexport.ErrNotFound):
			writeErr(w, http.StatusNotFound, err.Error())
		default:
			s.writeInternal(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) getAdminOrder(w http.ResponseWriter, r *http.Request) {
	s.getQuikOrder(w, r, true)
}

func (s *Server) listAdapters(w http.ResponseWriter, r *http.Request) {
	list, err := s.manager.ListRuntime(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"adapters": list})
}

func (s *Server) createAdapter(w http.ResponseWriter, r *http.Request) {
	var a domain.Adapter
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if a.BeginString == "" {
		a.BeginString = "FIX.4.4"
	}
	if a.HeartbeatInterval == 0 {
		a.HeartbeatInterval = 30
	}
	if a.Weight == 0 {
		a.Weight = 100
	}
	if a.MaxConcurrentOrders == 0 {
		a.MaxConcurrentOrders = 1000
	}
	created, err := s.adapters.Create(r.Context(), a)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"adapter": created})
}

func (s *Server) patchAdapter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		Weight   *int `json:"weight"`
		Priority *int `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.adapters.UpdateWeight(r.Context(), id, req.Weight, req.Priority); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	a, _ := s.adapters.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"adapter": a})
}

func (s *Server) enableAdapter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.manager.Enable(r.Context(), id); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeAdminAudit(r, "ADAPTER_ENABLED", "adapter", id, nil, nil, map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"status": "enabled"})
}

func (s *Server) disableAdapter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.manager.Disable(r.Context(), id); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	s.writeAdminAudit(r, "ADAPTER_DISABLED", "adapter", id, nil, nil, map[string]any{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"status": "disabled"})
}

func (s *Server) restartAdapter(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.Restart(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "restarted"})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	list, err := s.audit.List(r.Context(), 200)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": list})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.users.List(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}


// OnExecution is invoked by the durable inbox worker, never by the FIX callback.
// A report of an order not held here needs nothing: the order was made outside
// this Gateway or is finished and purged, and QUIK Orders and Trades hold its result.
func (s *Server) OnExecution(ctx context.Context, report adapters.ExecutionReport) error {
	if report.ProtocolReject {
		return fmt.Errorf("FIX protocol reject requires venue reconciliation (message %s sequence %s); report retained", report.RefMessageType, report.RefSeqNum)
	}
	if report.ExecType == "G" || report.ExecType == "H" || report.ExecType == "5" {
		return fmt.Errorf("execution correction, cancellation, or replacement requires venue reconciliation; report retained")
	}
	if report.ExecTransType != "" && report.ExecTransType != "0" {
		return fmt.Errorf("unsupported execution transaction type %s (reference %s); report retained for reconciliation", report.ExecTransType, report.ExecRefID)
	}
	values := make([]decimal.Decimal, 4)
	for i, value := range []string{report.LastQty, report.LastPx, report.CumQty, report.LeavesQty} {
		if value == "" {
			continue
		}
		parsed, err := decimal.NewFromString(value)
		if err != nil || parsed.IsNegative() {
			return fmt.Errorf("invalid FIX execution decimal field %d", i)
		}
		values[i] = parsed
	}
	lastQty, cumQty := values[0], values[2]
	if report.CancelReject {
		order, err := s.orderStore.ApplyCancelReject(ctx, report.ClOrdID, report.OrigClOrdID, report.OrderID, report.AdapterID,
			cancelRejectReason(report.Text, report.CxlRejReason), report.RawFIX)
		if err != nil {
			if errors.Is(err, orders.ErrNotFound) {
				return nil
			}
			s.log.Error("apply cancel reject failed", "error", err, "cl_ord_id", report.ClOrdID, "orig_cl_ord_id", report.OrigClOrdID)
			return err
		}
		if order.ID == uuid.Nil {
			return nil
		}
		reason := strings.TrimSpace(report.Text)
		if reason == "" {
			reason = "cancel rejected"
		}
		oid := order.ID
		bid := order.BrokerID
		s.emitObs(observability.Event{
			Type: observability.TypeBusiness, Subtype: observability.BizOrderCancelRejected,
			BrokerID: &bid, OrderID: &oid, ClientOrderID: order.ClientOrderID,
			RequestID: order.RequestID, TraceID: order.TraceID, AdapterID: report.AdapterID,
			Instrument: order.Symbol, Side: string(order.Side), Quantity: order.Quantity.String(),
			Message: reason, ErrorCode: "CANCEL_REJECTED", ErrorMessage: reason,
		})
		return nil
	}
	order, err := s.orderStore.ApplyExecutionReport(ctx, report.ClOrdID, report.OrigClOrdID, report.ExecType, report.OrdStatus,
		report.OrderID, lastQty, cumQty, report.AdapterID, report.Text, report.OrdRejReason, report.RawFIX)
	if err != nil {
		if errors.Is(err, orders.ErrNotFound) {
			return nil
		}
		s.log.Error("apply execution report failed", "error", err, "cl_ord_id", report.ClOrdID,
			"text", report.Text, "ord_rej_reason", report.OrdRejReason)
		return err
	}
	if order.ID == uuid.Nil {
		return nil
	}
	if s.manager != nil {
		if ad, ok := s.manager.Get(report.AdapterID); ok {
			if order.ExecutionBecameTerminal {
				ad.IncActive(-1)
			}
		}
	}
	s.emitOrderLifecycle(order, report)
	return nil
}

func (s *Server) emitOrderLifecycle(order domain.Order, report adapters.ExecutionReport) {
	oid := order.ID
	bid := order.BrokerID
	sub := observability.BizOrderAcknowledged
	switch order.Status {
	case domain.OrderPartiallyFilled:
		sub = observability.BizOrderPartiallyFilled
	case domain.OrderFilled:
		sub = observability.BizOrderFilled
	case domain.OrderRejected:
		sub = observability.BizOrderRejected
	case domain.OrderCancelled:
		sub = observability.BizOrderCancelled
	case domain.OrderFailed:
		sub = observability.BizOrderFailed
	}
	adapterID := report.AdapterID
	s.emitObs(observability.Event{
		Type: observability.TypeBusiness, Subtype: sub, BrokerID: &bid, OrderID: &oid,
		ClientOrderID: order.ClientOrderID, RequestID: order.RequestID, TraceID: order.TraceID,
		AdapterID: adapterID, Instrument: order.Symbol, Side: string(order.Side),
		Quantity: order.Quantity.String(), Message: string(order.Status),
	})
	s.emitObs(observability.Event{
		Type: observability.TypeFIX, Subtype: "FIX_EXECUTION_REPORT", BrokerID: &bid, OrderID: &oid,
		ClientOrderID: order.ClientOrderID, RequestID: order.RequestID, TraceID: order.TraceID,
		AdapterID: adapterID, FIXMsgType: "8", FIXDirection: "IN", Message: report.ExecType + "/" + report.OrdStatus,
	})
	if order.Status == domain.OrderRejected || order.Status == domain.OrderFailed {
		msg := ""
		if order.RejectReason != nil {
			msg = *order.RejectReason
		}
		s.emitObs(observability.Event{
			Type: observability.TypeError, Subtype: observability.ErrFIX, BrokerID: &bid, OrderID: &oid,
			ClientOrderID: order.ClientOrderID, RequestID: order.RequestID, AdapterID: adapterID,
			ErrorCode: "FIX_ORDER_REJECTED", ErrorMessage: msg, FailureStage: "fix_adapter", Message: msg,
		})
	}
}

func parseDec(s string) decimal.Decimal {
	if s == "" {
		return decimal.Zero
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return decimal.Zero
	}
	return d
}

func (s *Server) handleDocsAsset(w http.ResponseWriter, r *http.Request) {
	if !s.docsEnabled() {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	file, ok := docsAssets[r.URL.Path]
	if !ok {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	http.ServeFile(w, r, file)
}

// docsAssets maps public URLs to files; the request path never becomes a file path.
var docsAssets = map[string]string{
	"/docs/kse-logo.png":                 "docs/kse-logo.png",
	"/docs/logo_KSE.png":                 "docs/logo_KSE.png",
	"/docs/kse-favicon.png":              "docs/kse-favicon.png",
	"/docs/swagger/swagger-ui.css":       "docs/swagger/swagger-ui.css",
	"/docs/swagger/swagger-ui-bundle.js": "docs/swagger/swagger-ui-bundle.js",
	"/docs/swagger/init.js":              "docs/swagger/init.js",
}

func (s *Server) handleBrokerSwaggerUI(w http.ResponseWriter, r *http.Request) {
	if !s.docsEnabled() {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head>
<meta charset="utf-8"/>
<title>KSE Broker API</title>
<link rel="icon" type="image/png" href="/docs/kse-favicon.png"/>
<link rel="stylesheet" href="/docs/swagger/swagger-ui.css"/>
<style>
  body { margin: 0; background: #fff; }
  .kse-header {
    background: #fff;
    padding: 16px 32px;
    display: flex;
    align-items: center;
    gap: 18px;
    border-bottom: 3px solid #57b6c0;
  }
  .kse-header img { height: 48px; }
  .kse-header h1 {
    color: #57b6c0;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    font-size: 20px;
    font-weight: 600;
    margin: 0;
  }
  .kse-header p {
    color: #8899aa;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    font-size: 13px;
    margin: 2px 0 0;
  }
  /* Brand color overrides */
  .swagger-ui .topbar { display: none; }
  .swagger-ui .info .title { color: #1a1a2e; }
  .swagger-ui .scheme-container { background: #f0f8f9; border-bottom: 2px solid #57b6c0; }
  .swagger-ui .btn.authorize { color: #57b6c0; border-color: #57b6c0; }
  .swagger-ui .btn.authorize svg { fill: #57b6c0; }
  .swagger-ui .opblock.opblock-post { border-color: #57b6c0; }
  .swagger-ui .opblock.opblock-post .opblock-summary-method { background: #57b6c0; }
  .swagger-ui .opblock.opblock-get { border-color: #3a9ba8; }
  .swagger-ui .opblock.opblock-get .opblock-summary-method { background: #3a9ba8; }
  .swagger-ui .btn.execute { background: #57b6c0; border-color: #57b6c0; }
  .swagger-ui .btn.execute:hover { background: #469da6; }
  .swagger-ui .opblock-tag { color: #1a1a2e; border-bottom: 1px solid #e0e0e0; }
  .swagger-ui a.nostyle { color: #57b6c0; }
  .swagger-ui .response-col_status { color: #57b6c0; }
</style>
</head>
<body>
<div class="kse-header">
  <img src="/docs/logo_KSE.png" alt="KSE"/>
  <div>
    <h1>Кыргыз Фондулук Биржасы — Broker API</h1>
    <p>Кыргызская Фондовая Биржа · Kyrgyz Stock Exchange</p>
  </div>
</div>
<div id="swagger-ui"></div>
<script src="/docs/swagger/swagger-ui-bundle.js"></script>
<script src="/docs/swagger/init.js"></script>
</body></html>`))
}

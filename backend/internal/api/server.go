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
	"time"

	pb "github.com/fix-gateway/backend/gen/realtime/v1"
	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/executioninbox"
	"github.com/fix-gateway/backend/internal/marketdata"
	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/positions"
	"github.com/fix-gateway/backend/internal/quikapi"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/refdata"
	"github.com/fix-gateway/backend/internal/tradeledger"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shopspring/decimal"
)

type Server struct {
	dashboardCache     snapshotCache
	dashboardRefreshMu sync.Mutex
	monitoringCache    snapshotCache
	cfg                config.Config
	log                *slog.Logger
	pool               *pgxpool.Pool
	brokers            *brokers.Store
	orders             *orders.Service
	orderStore         *orders.Store
	tradeLedger        *tradeledger.Store
	adapters           *adapters.Store
	manager            *adapters.Manager
	outbox             *outbox.Store
	audit              *audit.Store
	users              *admin.UserStore
	hub                *realtime.Hub
	refdata            *refdata.Store
	positions          *positions.Store
	marketdata         *marketdata.Store
	quikExport         *quikexport.Store
	quikOrders         quikOrderReader
	quikAPI            *quikapi.Client
	obs                *observability.Pipeline
	realtimeClient     pb.RealtimeServiceClient
	ratePolicies       *ratelimit.Store
	executionInbox     *executioninbox.Inbox

	trusted             []*net.IPNet
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
		refdata:         refdata.NewStore(pool),
		positions:       positions.NewStore(pool),
		marketdata:      marketdata.NewStore(pool),
		tradeLedger:     tradeledger.NewStore(pool),
		quikAPI:         quikapi.NewClient(quikapi.NewStore(pool, cfg.QuikAPIBaseURL), nil),
		obs:             obs,
		trusted:         observability.ParseTrustedProxies(cfg.TrustedProxies),
		hmacNonces:      auth.NewNonceCache(10 * time.Minute),
		maxBody:         1 << 20,
		authFailLimiter: ratelimit.NewSlidingWindow(time.Minute, cfg.AuthFailMaxPerMinute),
	}
	// Refill an expired persisted snapshot before the first dashboard visit.
	if pool != nil {
		s.refreshDashboardSnapshot()
	}
	return s
}

func (s *Server) SetQuikExport(store *quikexport.Store) {
	s.quikExport = store
	// A typed nil *Store in the interface would defeat the `quikOrders == nil` checks.
	if store == nil {
		s.quikOrders = nil
		return
	}
	s.quikOrders = store
}

func (s *Server) SetExecutionInbox(in *executioninbox.Inbox) { s.executionInbox = in }

func (s *Server) SetNonceStore(store auth.NonceStore) { s.hmacNonces = store }

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
		availableSessions := 0
		if s.manager != nil {
			availableSessions = s.manager.AvailableCount()
		}
		metrics.FIXAvailable.Set(float64(availableSessions))
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
	mux.HandleFunc("GET /api/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		s.serveMergedOpenAPI(w, r, mux.routes, "docs/openapi.yaml", false)
	})
	mux.HandleFunc("GET /swagger", s.handleSwaggerUI)
	mux.HandleFunc("GET /api/broker-openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		s.serveMergedOpenAPI(w, r, mux.routes, "docs/broker-openapi.yaml", true)
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
	mux.Handle("GET /api/v1/firm", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.getBrokerFirm))))
	mux.Handle("GET /api/v1/instruments", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listInstruments))))
	mux.Handle("GET /api/v1/orderbook", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerOrderBook))))
	mux.Handle("GET /api/v1/market-data", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerMarketData))))
	mux.Handle("GET /api/v1/trades", s.brokerAuth(s.requirePermission(auth.PermTradesRead)(http.HandlerFunc(s.listTrades))))
	mux.Handle("GET /api/v1/positions", s.brokerAuth(s.requirePermission(auth.PermPositionsRead)(http.HandlerFunc(s.listBrokerPositions))))
	mux.Handle("PUT /api/v1/positions", s.brokerAuth(s.requirePermission(auth.PermPositionsWrite)(http.HandlerFunc(s.setBrokerPositions))))
	mux.Handle("PUT /api/v1/positions/cash", s.brokerAuth(s.requirePermission(auth.PermPositionsWrite)(http.HandlerFunc(s.setBrokerCash))))
	mux.Handle("DELETE /api/v1/positions/cash", s.brokerAuth(s.requirePermission(auth.PermPositionsWrite)(http.HandlerFunc(s.deleteBrokerCash))))
	mux.Handle("PUT /api/v1/positions/instruments", s.brokerAuth(s.requirePermission(auth.PermPositionsWrite)(http.HandlerFunc(s.setBrokerInstrument))))
	mux.Handle("DELETE /api/v1/positions/instruments", s.brokerAuth(s.requirePermission(auth.PermPositionsWrite)(http.HandlerFunc(s.deleteBrokerInstrument))))

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
	mux.Handle("POST /api/v1/admin/brokers/{id}/accounts", s.adminWrite(s.createAccount))
	mux.Handle("GET /api/v1/admin/brokers/{id}/accounts", s.adminRO(s.listAccounts))
	mux.Handle("PATCH /api/v1/admin/brokers/{id}/accounts/{account_id}", s.adminWrite(s.patchAccount))
	mux.Handle("DELETE /api/v1/admin/tokens/{id}", s.adminWrite(s.revokeToken))
	mux.Handle("POST /api/v1/admin/tokens/{id}/revoke", s.adminWrite(s.revokeToken))
	mux.Handle("POST /api/v1/admin/tokens/{id}/rotate", s.adminWrite(s.rotateToken))
	mux.Handle("PATCH /api/v1/admin/tokens/{id}", s.adminWrite(s.patchToken))
	mux.Handle("GET /api/v1/admin/orders", s.adminRO(s.listAdminOrders))
	mux.Handle("GET /api/v1/admin/reconciliation", s.adminRO(s.listReconciliation))
	mux.Handle("POST /api/v1/admin/reconciliation/{id}/resolve", s.adminOps(s.resolveReconciliation))
	mux.Handle("GET /api/v1/admin/rate-limits", s.adminRO(s.listRateLimits))
	mux.Handle("GET /api/v1/admin/trading-limits", s.adminRO(s.listTradingLimits))
	mux.Handle("GET /api/v1/admin/trading-limits/history", s.adminAudit(s.tradingLimitHistory))
	mux.Handle("PUT /api/v1/admin/trading-limits", s.adminWrite(s.updateTradingLimit))
	mux.Handle("PUT /api/v1/admin/rate-limits", s.adminWrite(s.updateRateLimit))
	mux.Handle("GET /api/v1/admin/orders/{id}", s.adminRO(s.getAdminOrder))
	mux.Handle("GET /api/v1/admin/emitents/{symbol}/orders", s.adminRO(s.listEmitentOrders))
	mux.Handle("GET /api/v1/admin/trades", s.adminRO(s.listAdminTrades))
	mux.Handle("GET /api/v1/admin/trades/{id}", s.adminRO(s.getAdminTrade))
	mux.Handle("GET /api/v1/admin/positions", s.adminRO(s.listAdminPositions))
	mux.Handle("PUT /api/v1/admin/positions", s.adminWrite(s.setAdminPositions))
	mux.Handle("PUT /api/v1/admin/positions/cash", s.adminWrite(s.setAdminCash))
	mux.Handle("DELETE /api/v1/admin/positions/cash", s.adminWrite(s.deleteAdminCash))
	mux.Handle("PUT /api/v1/admin/positions/instruments", s.adminWrite(s.setAdminInstrument))
	mux.Handle("DELETE /api/v1/admin/positions/instruments", s.adminWrite(s.deleteAdminInstrument))
	mux.Handle("POST /api/v1/admin/positions/sync", s.adminOps(s.syncPositionsFIX))
	mux.Handle("GET /api/v1/admin/market-data", s.adminRO(s.listAdminMarketData))
	mux.Handle("POST /api/v1/admin/market-data/sync", s.adminOps(s.syncMarketDataFIX))
	mux.Handle("GET /api/v1/admin/quik-api/settings", s.adminRO(s.getQuikAPISettings))
	mux.Handle("PATCH /api/v1/admin/quik-api/settings", s.adminOps(s.patchQuikAPISettings))
	mux.Handle("GET /api/v1/admin/quik-api/quotes", s.adminRO(s.listQuikAPIQuotes))
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
	mux.Handle("GET /api/v1/admin/instruments", s.adminRO(s.listAdminInstruments))
	mux.Handle("GET /api/v1/admin/firms", s.adminRO(s.listAdminFirms))
	mux.Handle("POST /api/v1/admin/instruments/sync", s.adminOps(s.syncInstrumentsFIX))
	mux.Handle("POST /api/v1/admin/quik/sync", s.adminOps(s.syncInstrumentsFIX)) // alias
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
	return s.requireHTTPS(s.cors(s.limitBody(h)))
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
		writeErrDetails(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	ip, _ := s.clientIP(r)
	order, idempotent, err := s.orders.Create(r.Context(), b, ip, req, tok.QuikFirm)
	if err != nil {
		var risk *orders.RiskRejected
		if errors.As(err, &risk) {
			observability.AnnotateError(r, risk.Code, risk.Message, "risk")
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": risk.Message, "code": risk.Code})
			return
		}
		if errors.Is(err, orders.ErrPersistenceUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "order storage unavailable; verify result and retry with the same client_order_id")
			return
		}
		observability.AnnotateError(r, "ORDER_CREATE_FAILED", err.Error(), "order_service")
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			s.writeInternal(w, err)
			return
		}
		writeErrDetails(w, http.StatusBadRequest, err.Error(), orderFieldHints())
		return
	}
	qty := order.Quantity.String()
	price := ""
	if order.Price != nil {
		price = order.Price.String()
	}
	observability.AnnotateOrder(r, order.ID, order.ClientOrderID, req.AccountID, order.Symbol, string(order.Side), qty, price)
	metrics.OrdersReceived.Inc()
	code := http.StatusCreated
	if idempotent {
		code = http.StatusOK
	}
	writeJSON(w, code, map[string]any{"order": orderDTO(order), "idempotent": idempotent})
}

func (s *Server) listBrokerOrders(w http.ResponseWriter, r *http.Request) {
	s.listQuikOrders(w, r, false)
}

func (s *Server) listBrokerActiveOrders(w http.ResponseWriter, r *http.Request) {
	s.listQuikOrdersInState(w, r, false, quikActiveState)
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
	current, local, err := s.currentQuikOrder(r.Context(), r.PathValue("id"), firm, brokerID, true)
	if err != nil {
		s.writeOrderReadError(w, err)
		return
	}
	state := strings.ToUpper(current.Text("State"))
	if state == "СНЯТА" || state == "CANCELLED" || state == "CANCELED" {
		writeJSON(w, http.StatusOK, map[string]any{"order": current})
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
	writeJSON(w, http.StatusOK, map[string]any{"order": orderDTO(order)})
}

func (s *Server) listBrokerAccounts(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	list, err := s.brokers.ListAccounts(r.Context(), b.ID)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": list})
}

// getBrokerFirm returns firm code (from token) and firm name (from quik_firms cache).
func (s *Server) getBrokerFirm(w http.ResponseWriter, r *http.Request) {
	tok, ok := TokenFromContext(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	firmName := ""
	if f, found, err := s.refdata.GetFirmByID(r.Context(), tok.QuikFirm); err != nil {
		s.writeInternal(w, err)
		return
	} else if found {
		firmName = f.FirmName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"quik_firm": tok.QuikFirm,
		"firm_name": firmName,
	})
}

func (s *Server) listInstruments(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, internal_symbol, quik_symbol, board, currency, security_type, status
		FROM instruments WHERE status='ACTIVE' ORDER BY id`)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	defer rows.Close()
	var out []domain.Instrument
	for rows.Next() {
		var i domain.Instrument
		if err := rows.Scan(&i.ID, &i.InternalSymbol, &i.QuikSymbol, &i.Board, &i.Currency, &i.SecurityType, &i.Status); err != nil {
			s.writeInternal(w, err)
			return
		}
		out = append(out, i)
	}
	writeJSON(w, http.StatusOK, map[string]any{"instruments": out})
}

func (s *Server) listBrokerOrderBook(w http.ResponseWriter, r *http.Request) {
	board := strings.TrimSpace(r.URL.Query().Get("board"))
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if s.manager != nil {
		if _, items, ok := s.manager.LiveQuotes(); ok {
			items = filterQuotes(items, board, symbol)
			books := make([]map[string]any, 0, len(items))
			for _, q := range items {
				books = append(books, orderBookDTO(q))
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"source": "live",
				"live":   true,
				"books":  books,
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": "live",
		"live":   false,
		"books":  []any{},
		"note":   "нет живого стакана — шлюз автоматически восстанавливает подписку на рыночные данные",
	})
}

func orderBookDTO(q domain.MarketQuote) map[string]any {
	return map[string]any{
		"symbol":      q.Symbol,
		"security_id": q.SecurityID,
		"board":       q.Board,
		"currency":    q.Currency,
		"name":        q.Name,
		"last":        q.Last,
		"last_size":   q.LastSize,
		"last_time":   q.LastTime,
		"bid":         q.Bid,
		"bid_size":    q.BidSize,
		"offer":       q.Offer,
		"offer_size":  q.OfferSize,
		"bids":        bookLevelsOrTop(q.Bids, q.Bid, q.BidSize),
		"offers":      bookLevelsOrTop(q.Offers, q.Offer, q.OfferSize),
		"updated_at":  q.UpdatedAt,
	}
}

func bookLevelsOrTop(levels []domain.BookLevel, px, sz string) []domain.BookLevel {
	if len(levels) > 0 {
		return levels
	}
	if strings.TrimSpace(px) == "" {
		return []domain.BookLevel{}
	}
	return []domain.BookLevel{{Price: px, Size: sz}}
}

func (s *Server) listAdminInstruments(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, internal_symbol, quik_symbol, board, currency, security_type, status
		FROM instruments
		WHERE status = 'ACTIVE'
		ORDER BY board, quik_symbol, id`)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	defer rows.Close()
	var out []domain.Instrument
	for rows.Next() {
		var i domain.Instrument
		if err := rows.Scan(&i.ID, &i.InternalSymbol, &i.QuikSymbol, &i.Board, &i.Currency, &i.SecurityType, &i.Status); err != nil {
			s.writeInternal(w, err)
			return
		}
		out = append(out, i)
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": "local", "instruments": out, "count": len(out)})
}

func (s *Server) listAdminFirms(w http.ResponseWriter, r *http.Request) {
	list, err := s.refdata.ListFirms(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"source": "local",
		"firms":  list,
		"count":  len(list),
		"note":   "фирмы не приходят через FIX Security Definition; в таблице — ранее сохранённый локальный кэш",
	})
}

func (s *Server) syncInstrumentsFIX(w http.ResponseWriter, r *http.Request) {
	adapterID, items, err := s.manager.FetchSecurities(r.Context())
	if err != nil {
		s.writeUpstream(w, err)
		return
	}
	res, err := s.refdata.SyncFromFIX(r.Context(), items)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	res.AdapterID = adapterID
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}

func (s *Server) listTrades(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	list, err := s.orderStore.ListTrades(r.Context(), &b.ID, 100)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": mapTrades(list)})
}

func (s *Server) listBrokerPositions(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	tok, _ := TokenFromContext(r.Context())
	// QUIK «Фирма» in the snapshot is broker.code (e.g. 2040). Token quik_firm
	// (FIX 5018) may be the same or a longer firm id — match either.
	firms := positions.NormalizeFirmIDs(b.Code, tok.QuikFirm)
	if len(firms) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"source": "fix", "firm_id": "", "instruments": []any{}, "cash": []any{},
			"note": "у брокера не задан код фирмы",
		})
		return
	}
	sn, items, err := s.positions.ListMergedByFirms(r.Context(), firms, "")
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	resp := mapPositionsResponse(sn, items)
	resp["firm_id"] = firms[0]
	if len(items) == 0 && sn.ID == 0 {
		resp["note"] = "Шлюз автоматически получает позиции из QUIK и восстанавливает поток после разрыва связи. Ожидается полный снимок."
	} else if len(items) == 0 && sn.ReportCount > 0 {
		resp["note"] = "снимок позиций получен, но нет строк по фирме " + strings.Join(firms, "/")
	}
	writeJSON(w, http.StatusOK, resp)
}

func mapPositionsResponse(sn positions.Snapshot, items []domain.FIXPosition) map[string]any {
	var instruments []domain.FIXPosition
	var cash []domain.FIXPosition
	for _, p := range items {
		switch p.Kind {
		case "CASH": // PosType ML
			cash = append(cash, p)
		default: // INSTRUMENT (DL) / FUTURES (FL, FP)
			instruments = append(instruments, p)
		}
	}
	if instruments == nil {
		instruments = []domain.FIXPosition{}
	}
	if cash == nil {
		cash = []domain.FIXPosition{}
	}
	resp := map[string]any{
		"source":      positionsResponseSource(sn, items),
		"instruments": instruments,
		"cash":        cash,
	}
	if sn.ID != 0 {
		resp["snapshot"] = sn
	}
	return resp
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	ip, _ := s.clientIP(r)
	if s.authFailLimiter.Blocked(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many failed authentication attempts")
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	u, err := s.users.Authenticate(r.Context(), req.Email, req.Password)
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
		"user":  map[string]any{"id": u.ID, "email": u.Email, "role": u.RoleCode, "full_name": u.FullName},
	})
	s.emitObs(observability.Event{
		Type: observability.TypeAuth, Subtype: observability.AuthSuccess, ClientIP: ip,
		Endpoint: r.URL.Path, HTTPMethod: r.Method, RequestID: observability.RequestIDFrom(r.Context()),
		ActorType: "admin", ActorID: u.ID.String(), Message: "admin login",
	})
	s.writeAdminAudit(r, "USER_LOGIN", "user", u.ID.String(), nil, nil, map[string]any{"email": u.Email})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	data, err := s.dashboardCache.get(r.Context(), 5*time.Second, func(ctx context.Context) (any, error) {
		s.refreshDashboardSnapshot()
		var raw []byte
		if err := s.pool.QueryRow(ctx, `SELECT gateway_dashboard_snapshot()`).Scan(&raw); err != nil {
			return nil, err
		}
		snapshot, err := decodeDashboardSnapshot(raw)
		if err != nil {
			return nil, err
		}
		ads, err := s.manager.ListRuntime(ctx)
		if err != nil {
			return nil, err
		}
		snapshot["adapters"] = ads
		snapshot["adapters_available"] = s.manager.AvailableCount()
		// The persisted snapshot only sees logged write requests; presence must
		// match the Brokers page (token use and live gRPC streams).
		live := s.connectedBrokers(ctx)
		snapshot["connected_brokers"] = live
		if o, ok := snapshot["observability"].(map[string]any); ok {
			o["connected_now"] = len(live)
		}
		snapshot["system"] = map[string]any{"api": "ONLINE", "database": "ONLINE", "queue": "ONLINE"}
		return snapshot, nil
	})
	if err != nil {
		if errors.Is(err, errDashboardPending) {
			writeErr(w, http.StatusServiceUnavailable, "Статистика рассчитывается. Повторите запрос через несколько секунд.")
			return
		}
		s.writeInternal(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// The persisted dashboard remains an audit/operations snapshot. Current
	// order counters must come from the same source as the order list; when
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

var errDashboardPending = errors.New("dashboard snapshot pending")

func decodeDashboardSnapshot(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, errDashboardPending
	}
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	if snapshot == nil {
		return nil, errDashboardPending
	}
	return snapshot, nil
}

// Keep persisted statistics warm even when nobody has the dashboard open.
func (s *Server) RunDashboardRefresh(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.refreshDashboardSnapshotContext(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) refreshDashboardSnapshot() {
	s.refreshDashboardSnapshotContext(context.Background())
}

func (s *Server) refreshDashboardSnapshotContext(parent context.Context) {
	if !s.dashboardRefreshMu.TryLock() {
		return
	}
	go func() {
		defer s.dashboardRefreshMu.Unlock()
		ctx, cancel := context.WithTimeout(parent, 60*time.Second)
		defer cancel()
		if _, err := s.pool.Exec(ctx, `SELECT gateway_refresh_dashboard_snapshot()`); err != nil && s.log != nil {
			s.log.Warn("dashboard snapshot refresh failed", "error", err)
		}
	}()
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
	if _, err := s.brokers.GetBroker(r.Context(), id); err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 {
		days = 7
	}
	st, err := s.orderStore.StatsForBroker(r.Context(), id, days)
	if err != nil {
		s.writeInternal(w, err)
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

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Code           string `json:"code"`
		Name           string `json:"name"`
		QuikFirm       string `json:"quik_firm"`
		TradingAccount string `json:"trading_account"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a, err := s.brokers.CreateAccount(r.Context(), id, req.Code, req.Name, req.QuikFirm, req.TradingAccount)
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	s.writeAdminAudit(r, "ACCOUNT_CREATED", "broker_account", fmtInt(a.ID), &id, nil, map[string]any{
		"code": a.Code, "trading_account": a.TradingAccount, "status": a.Status,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"account": a})
}

func (s *Server) patchAccount(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	accountID, err := strconv.ParseInt(r.PathValue("account_id"), 10, 64)
	if err != nil || accountID <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid account_id")
		return
	}
	var req struct {
		Code           *string `json:"code"`
		Name           *string `json:"name"`
		QuikFirm       *string `json:"quik_firm"`
		TradingAccount *string `json:"trading_account"`
		Status         *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a, err := s.brokers.UpdateAccount(r.Context(), id, accountID, brokers.AccountUpdate{
		Code: req.Code, Name: req.Name, QuikFirm: req.QuikFirm,
		TradingAccount: req.TradingAccount, Status: req.Status,
	})
	if err != nil {
		writeAccountErr(w, err)
		return
	}
	s.writeAdminAudit(r, "ACCOUNT_UPDATED", "broker_account", fmtInt(a.ID), &id, nil, map[string]any{
		"code": a.Code, "trading_account": a.TradingAccount, "status": a.Status,
	})
	writeJSON(w, http.StatusOK, map[string]any{"account": a})
}

func writeAccountErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, brokers.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, brokers.ErrAccountExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, brokers.ErrAccountFields), errors.Is(err, brokers.ErrAccountStatus):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := s.brokers.ListAccounts(r.Context(), id)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	if list == nil {
		list = []domain.BrokerAccount{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": list})
}

func (s *Server) listAdminOrders(w http.ResponseWriter, r *http.Request) {
	s.listQuikOrders(w, r, true)
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

func (s *Server) listAdminTrades(w http.ResponseWriter, r *http.Request) {
	list, err := s.tradeLedger.List(r.Context(), 500)
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	coverage, err := s.tradeLedger.Coverage(r.Context())
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": list, "coverage": coverage})
}

func (s *Server) listAdminPositions(w http.ResponseWriter, r *http.Request) {
	firm := r.URL.Query().Get("firm_id")
	if firm == "" {
		if raw := r.URL.Query().Get("broker_id"); raw != "" {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err == nil {
				if b, err := s.brokers.GetBroker(r.Context(), id); err == nil {
					firm = b.Code
				}
			}
		}
	}
	firms := positions.NormalizeFirmIDs(firm)
	sn, items, err := s.positions.ListMergedByFirms(r.Context(), firms, "")
	if err != nil {
		s.writeInternal(w, err)
		return
	}
	resp := mapAdminPositionsResponse(sn, items, s.manager.PositionSubscription())
	writeJSON(w, http.StatusOK, resp)
}

func mapAdminPositionsResponse(sn positions.Snapshot, items []domain.FIXPosition, subscription domain.PositionSubscription) map[string]any {
	resp := mapPositionsResponse(sn, items)
	resp["subscription"] = subscription
	resp["live"] = subscription.Status == "ACTIVE" && sn.ID != 0 && sn.AdapterID == subscription.AdapterID && sn.PosReqID == subscription.RequestID
	if len(items) == 0 && sn.ID == 0 {
		resp["note"] = "Шлюз автоматически получает позиции из QUIK и восстанавливает поток после разрыва связи. Ожидается полный снимок."
	}
	return resp
}

func (s *Server) syncPositionsFIX(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Firm     string `json:"firm_id"`
		Client   string `json:"client_code"`
		Account  string `json:"account"`
		BrokerID *int64 `json:"broker_id"`
		Date     string `json:"clearing_business_date"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Firm == "" && req.BrokerID != nil {
		if b, err := s.brokers.GetBroker(r.Context(), *req.BrokerID); err == nil {
			req.Firm = b.Code
		}
	}
	q := domain.PositionQuery{
		Firm:                 req.Firm,
		ClientCode:           req.Client,
		Account:              req.Account,
		ClearingBusinessDate: req.Date,
	}

	adapterID, reqID, count, sn, err := s.positions.SyncFromFIX(r.Context(), s.manager, q)
	if err != nil {
		s.writeUpstream(w, err)
		return
	}
	_, listed, _ := s.positions.ListMergedByFirms(r.Context(), positions.NormalizeFirmIDs(req.Firm), "")
	cash, instr, fut := 0, 0, 0
	for _, p := range listed {
		switch p.Kind {
		case "CASH":
			cash++
		case "FUTURES":
			fut++
		default:
			instr++
		}
	}
	s.hub.Broadcast("positions.updated", map[string]any{
		"adapter_id": adapterID, "pos_req_id": reqID, "received": count,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "adapter_id": adapterID, "pos_req_id": reqID,
		"received": count, "cash": cash, "instruments": instr, "futures": fut,
		"summary": positions.FormatReceived(count, cash, instr, fut),
		"result":  mapPositionsResponse(sn, listed),
	})
}

func mapMarketDataResponse(sn marketdata.Snapshot, items []domain.MarketQuote) map[string]any {
	return map[string]any{
		"source":   "fix",
		"snapshot": sn,
		"quotes":   items,
	}
}

func filterQuotes(items []domain.MarketQuote, board, symbol string) []domain.MarketQuote {
	if board == "" && symbol == "" {
		return items
	}
	out := make([]domain.MarketQuote, 0, len(items))
	for _, q := range items {
		if board != "" && q.Board != board {
			continue
		}
		if symbol != "" && q.Symbol != symbol && q.SecurityID != symbol {
			continue
		}
		out = append(out, q)
	}
	return out
}

func (s *Server) listAdminMarketData(w http.ResponseWriter, r *http.Request) {
	board := r.URL.Query().Get("board")
	symbol := r.URL.Query().Get("symbol")
	subscription := s.manager.MarketDataSubscription()
	if adapterID, items, ok := s.manager.LiveQuotes(); ok {
		items = filterQuotes(items, board, symbol)
		writeJSON(w, http.StatusOK, map[string]any{
			"source":       "live",
			"live":         subscription.Status == "ACTIVE" || subscription.Status == "PARTIAL",
			"subscription": subscription,
			"snapshot": marketdata.Snapshot{
				AdapterID:   adapterID,
				Status:      subscription.Status,
				ReportCount: len(items),
				RequestedAt: time.Now().UTC(),
			},
			"quotes": items,
		})
		return
	}
	sn, items, err := s.marketdata.ListFromLatest(r.Context(), board, symbol)
	if err != nil {
		if marketdata.IsNoSnapshot(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"source": "fix", "quotes": []any{}, "live": false,
				"subscription": subscription,
				"note":         "Шлюз автоматически поддерживает подписку на инструменты и восстанавливает её при разрыве связи.",
			})
			return
		}
		s.writeInternal(w, err)
		return
	}
	result := mapMarketDataResponse(sn, items)
	result["subscription"], result["live"] = subscription, false
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) syncMarketDataFIX(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Symbol string `json:"symbol"`
		Board  string `json:"board"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	adapterID, items, ok := s.manager.LiveQuotes()
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "FIX market data subscription is warming up; retry shortly")
		return
	}
	items = filterQuotes(items, strings.TrimSpace(req.Board), strings.TrimSpace(req.Symbol))
	subscription := s.manager.MarketDataSubscription()
	live := subscription.Status == "ACTIVE" || subscription.Status == "PARTIAL"
	received := 0
	for _, row := range items {
		if row.HasData() {
			received++
		}
	}
	// UI refreshes read the persistent backend subscription; they must not replace it.
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "adapter_id": adapterID, "live": live, "subscription": subscription,
		"received": received, "summary": marketdata.FormatReceived(received),
		"result": map[string]any{"source": "live", "live": live, "quotes": items, "subscription": subscription},
	})
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

func orderFieldHints() map[string]string {
	return map[string]string{
		"account_id":         "торговый код → FIX tag 1",
		"client_code":        "произвольный код клиента → FIX tag 109 ClientID",
		"symbol":             "инструмент (тикер/ISIN)",
		"board / код класса": "в body заявки → FIX tag 100 ExDestination",
		"currency / валюта":  "не в body — instruments.currency по symbol → FIX tag 15",
		"quik_firm / 5018":   "код фирмы с API-токена (не из body) → FIX tag 5018",
	}
}

func orderDTO(o domain.Order) map[string]any {
	return map[string]any{
		"id": o.ID, "broker_id": o.BrokerID, "account_id": o.TradingAccount,
		"client_order_id": o.ClientOrderID, "instrument_id": o.InstrumentID,
		"symbol": o.Symbol, "board": o.Board, "currency": o.Currency,
		"side": o.Side, "order_type": o.OrderType,
		"time_in_force": o.TimeInForce, "quantity": o.Quantity, "price": o.Price,
		"filled_quantity": o.FilledQuantity, "avg_price": o.AvgPrice, "status": o.Status,
		"adapter_id": o.AdapterID, "fix_cl_ord_id": o.FixClOrdID, "quik_order_id": o.QuikOrderID,
		"client_code": o.ClientCode, "quik_firm": o.QuikFirm, "firm_name": o.FirmName,
		"reject_reason": o.RejectReason, "created_at": o.CreatedAt, "updated_at": o.UpdatedAt,
		"request_id": o.RequestID, "trace_id": o.TraceID,
	}
}

func mapOrders(list []domain.Order) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, o := range list {
		out = append(out, orderDTO(o))
	}
	return out
}

func tradeDTO(t domain.Trade) map[string]any {
	return map[string]any{
		"id": t.ID, "order_id": t.OrderID, "exec_id": t.ExecID,
		"last_qty": t.LastQty, "last_px": t.LastPx, "cum_qty": t.CumQty, "leaves_qty": t.LeavesQty,
		"adapter_id": t.AdapterID, "created_at": t.CreatedAt,
		"execution_date": t.ExecutionDate,
		"trade_match_id": t.TradeMatchID, "contra_broker": t.ContraBroker, "contra_trader": t.ContraTrader,
		"broker_id": t.BrokerID, "client_order_id": t.ClientOrderID,
		"symbol": t.Symbol, "board": t.Board, "side": t.Side,
		"client_code": t.ClientCode, "quik_firm": t.QuikFirm, "firm_name": t.FirmName,
		"fix_cl_ord_id": t.FixClOrdID, "quik_order_id": t.QuikOrderID,
	}
}

func mapTrades(list []domain.Trade) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, t := range list {
		out = append(out, tradeDTO(t))
	}
	return out
}

func eventDTO(e domain.OrderEvent) map[string]any {
	m := map[string]any{
		"id": e.ID, "order_id": e.OrderID, "event_type": e.EventType,
		"from_status": e.FromStatus, "to_status": e.ToStatus,
		"adapter_id": e.AdapterID, "message": e.Message, "created_at": e.CreatedAt,
	}
	if len(e.Payload) > 0 {
		var p map[string]any
		if err := json.Unmarshal(e.Payload, &p); err == nil {
			if raw, ok := p["raw_fix"].(string); ok && raw != "" {
				m["raw_fix"] = raw
			}
		}
	}
	return m
}

func mapEvents(list []domain.OrderEvent) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		out = append(out, eventDTO(e))
	}
	return out
}

// OnExecution is invoked by the durable inbox worker, never by the FIX callback.
func (s *Server) OnExecution(ctx context.Context, report adapters.ExecutionReport) error {
	ledgerSaved := false
	if s.tradeLedger != nil && s.tradeLedger.Pool != nil {
		var err error
		ledgerSaved, err = s.tradeLedger.RecordFIX(ctx, report)
		if err != nil {
			return err
		}
	}
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
	lastQty, lastPx, cumQty, leaves := values[0], values[1], values[2], values[3]
	date, err := reportExecutionDate(report)
	if err != nil {
		return err
	}
	ctx = orders.WithExecutionDate(ctx, date)
	if report.CancelReject {
		order, err := s.orderStore.ApplyCancelReject(ctx, report.ClOrdID, report.OrigClOrdID, report.OrderID, report.AdapterID,
			strings.TrimSpace(report.Text+" "+report.CxlRejReason), report.RawFIX)
		if err != nil {
			if errors.Is(err, orders.ErrNotFound) && s.tradeLedger != nil && s.tradeLedger.Pool != nil && report.RawFIX != "" {
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
	order, exec, err := s.orderStore.ApplyExecutionReport(ctx, report.ClOrdID, report.OrigClOrdID, report.ExecID, report.ExecType, report.OrdStatus,
		report.OrderID, lastQty, lastPx, cumQty, leaves, report.AdapterID, report.Text, report.OrdRejReason, report.RawFIX)
	if err != nil {
		if errors.Is(err, orders.ErrNotFound) && s.tradeLedger != nil && s.tradeLedger.Pool != nil && report.RawFIX != "" {
			// Raw evidence has been committed even for external orders. A real fill
			// has also been recorded without manufacturing a local order.
			if ledgerSaved && s.hub != nil {
				s.hub.Broadcast("trade.created", map[string]any{"source": "fix"})
			}
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
	if exec != nil {
		if ledgerSaved {
			if err := s.tradeLedger.LinkExecution(ctx, exec.ID, order.ID, exec.ExecID, exec.TradeMatchID); err != nil {
				return err
			}
		}
		s.hub.BroadcastForBroker("trade.created", exec, order.BrokerID)
	}
	s.emitOrderLifecycle(order, report)
	return nil
}

func reportExecutionDate(report adapters.ExecutionReport) (string, error) {
	if report.TradeDate != "" {
		if _, err := time.Parse("20060102", report.TradeDate); err != nil {
			return "", fmt.Errorf("invalid FIX TradeDate")
		}
		return report.TradeDate, nil
	}
	value := report.TransactTime
	if value == "" {
		value = report.OriginalTime
	}
	if value == "" {
		return "", nil
	}
	if _, err := time.Parse("20060102-15:04:05.999999999", value); err != nil {
		return "", fmt.Errorf("invalid FIX execution timestamp")
	}
	return value[:8], nil
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
	switch r.URL.Path {
	case "/docs/kse-logo.png", "/docs/logo_KSE.png", "/docs/kse-favicon.png", "/docs/swagger/swagger-ui.css", "/docs/swagger/swagger-ui-bundle.js", "/docs/swagger/init.js":
		http.ServeFile(w, r, strings.TrimPrefix(r.URL.Path, "/"))
	default:
		writeErr(w, http.StatusNotFound, "not found")
	}
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	if !s.docsEnabled() {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><meta charset="utf-8"/><title>FIX Gateway API</title>
<link rel="stylesheet" href="/docs/swagger/swagger-ui.css"/></head>
<body><div id="swagger-ui"></div>
<script src="/docs/swagger/swagger-ui-bundle.js"></script>
<script src="/docs/swagger/init.js"></script>
</body></html>`))
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

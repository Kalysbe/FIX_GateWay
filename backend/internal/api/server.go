package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/marketdata"
	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
	"github.com/fix-gateway/backend/internal/positions"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/ratelimit"
	"github.com/fix-gateway/backend/internal/realtime"
	"github.com/fix-gateway/backend/internal/refdata"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shopspring/decimal"
)

type Server struct {
	cfg        config.Config
	log        *slog.Logger
	pool       *pgxpool.Pool
	brokers    *brokers.Store
	orders     *orders.Service
	orderStore *orders.Store
	adapters   *adapters.Store
	manager    *adapters.Manager
	outbox     *outbox.Store
	audit      *audit.Store
	users      *admin.UserStore
	hub        *realtime.Hub
	refdata    *refdata.Store
	positions  *positions.Store
	marketdata *marketdata.Store
	quikExport *quikexport.Store
	obs        *observability.Pipeline
	upgrader   websocket.Upgrader

	authFailLimiter     *ratelimit.SlidingWindow
	brokerReqLimiters   sync.Map
	brokerOrderLimiters sync.Map
	authSuccessThrottle sync.Map
}

func NewServer(
	cfg config.Config, log *slog.Logger, pool *pgxpool.Pool,
	brokerStore *brokers.Store, orderSvc *orders.Service, orderStore *orders.Store,
	adapterStore *adapters.Store, manager *adapters.Manager, outboxStore *outbox.Store,
	auditStore *audit.Store, users *admin.UserStore, hub *realtime.Hub,
	obs *observability.Pipeline,
) *Server {
	return &Server{
		cfg: cfg, log: log, pool: pool, brokers: brokerStore, orders: orderSvc, orderStore: orderStore,
		adapters: adapterStore, manager: manager, outbox: outboxStore, audit: auditStore, users: users, hub: hub,
		refdata:         refdata.NewStore(pool),
		positions:       positions.NewStore(pool),
		marketdata:      marketdata.NewStore(pool),
		obs:             obs,
		upgrader:        websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		authFailLimiter: ratelimit.NewSlidingWindow(time.Minute, cfg.AuthFailMaxPerMinute),
	}
}

func (s *Server) SetQuikExport(store *quikexport.Store) {
	s.quikExport = store
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /api/openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("GET /swagger", s.handleSwaggerUI)
	mux.HandleFunc("GET /api/broker-openapi.yaml", s.handleBrokerOpenAPI)
	mux.HandleFunc("GET /broker-swagger", s.handleBrokerSwaggerUI)
	mux.HandleFunc("GET /docs/kse-logo.png", s.handleDocsAsset)
	mux.HandleFunc("GET /docs/kse-favicon.png", s.handleDocsAsset)

	mux.Handle("POST /api/v1/orders", s.brokerAuth(s.requirePermission(auth.PermOrdersCreate)(s.orderRateLimit(http.HandlerFunc(s.createOrder)))))
	mux.Handle("GET /api/v1/orders", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerOrders))))
	mux.Handle("GET /api/v1/orders/{id}", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.getBrokerOrder))))
	mux.Handle("POST /api/v1/orders/{id}/cancel", s.brokerAuth(s.requirePermission(auth.PermOrdersCancel)(http.HandlerFunc(s.cancelBrokerOrder))))
	mux.Handle("GET /api/v1/accounts", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerAccounts))))
	mux.Handle("GET /api/v1/firm", s.brokerAuth(http.HandlerFunc(s.getBrokerFirm)))
	mux.Handle("GET /api/v1/instruments", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listInstruments))))
	mux.Handle("GET /api/v1/trades", s.brokerAuth(s.requirePermission(auth.PermTradesRead)(http.HandlerFunc(s.listTrades))))
	mux.Handle("GET /api/v1/positions", s.brokerAuth(s.requirePermission(auth.PermPositionsRead)(http.HandlerFunc(s.listBrokerPositions))))

	mux.HandleFunc("POST /api/v1/admin/login", s.adminLogin)
	mux.Handle("GET /api/v1/admin/dashboard", s.adminAuth(http.HandlerFunc(s.dashboard)))
	mux.Handle("POST /api/v1/admin/brokers", s.adminAuth(http.HandlerFunc(s.createBroker)))
	mux.Handle("GET /api/v1/admin/brokers", s.adminAuth(http.HandlerFunc(s.listBrokers)))
	mux.Handle("GET /api/v1/admin/brokers/{id}", s.adminAuth(http.HandlerFunc(s.getBroker)))
	mux.Handle("PATCH /api/v1/admin/brokers/{id}", s.adminAuth(http.HandlerFunc(s.patchBroker)))
	mux.Handle("POST /api/v1/admin/brokers/{id}/tokens", s.adminAuth(http.HandlerFunc(s.createToken)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/tokens", s.adminAuth(http.HandlerFunc(s.listTokens)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/security", s.adminAuth(http.HandlerFunc(s.brokerSecurity)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/stats", s.adminAuth(http.HandlerFunc(s.brokerStats)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/ip-whitelist", s.adminAuth(http.HandlerFunc(s.listIPWhitelist)))
	mux.Handle("POST /api/v1/admin/brokers/{id}/ip-whitelist", s.adminAuth(http.HandlerFunc(s.addIPWhitelist)))
	mux.Handle("DELETE /api/v1/admin/brokers/{id}/ip-whitelist/{entry_id}", s.adminAuth(http.HandlerFunc(s.removeIPWhitelist)))
	mux.Handle("POST /api/v1/admin/brokers/{id}/accounts", s.adminAuth(http.HandlerFunc(s.createAccount)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/accounts", s.adminAuth(http.HandlerFunc(s.listAccounts)))
	mux.Handle("DELETE /api/v1/admin/tokens/{id}", s.adminAuth(http.HandlerFunc(s.revokeToken)))
	mux.Handle("POST /api/v1/admin/tokens/{id}/revoke", s.adminAuth(http.HandlerFunc(s.revokeToken)))
	mux.Handle("POST /api/v1/admin/tokens/{id}/rotate", s.adminAuth(http.HandlerFunc(s.rotateToken)))
	mux.Handle("PATCH /api/v1/admin/tokens/{id}", s.adminAuth(http.HandlerFunc(s.patchToken)))
	mux.Handle("GET /api/v1/admin/orders", s.adminAuth(http.HandlerFunc(s.listAdminOrders)))
	mux.Handle("GET /api/v1/admin/orders/{id}", s.adminAuth(http.HandlerFunc(s.getAdminOrder)))
	mux.Handle("GET /api/v1/admin/emitents/{symbol}/orders", s.adminAuth(http.HandlerFunc(s.listEmitentOrders)))
	mux.Handle("GET /api/v1/admin/trades", s.adminAuth(http.HandlerFunc(s.listAdminTrades)))
	mux.Handle("GET /api/v1/admin/positions", s.adminAuth(http.HandlerFunc(s.listAdminPositions)))
	mux.Handle("POST /api/v1/admin/positions/sync", s.adminAuth(http.HandlerFunc(s.syncPositionsFIX)))
	mux.Handle("GET /api/v1/admin/market-data", s.adminAuth(http.HandlerFunc(s.listAdminMarketData)))
	mux.Handle("POST /api/v1/admin/market-data/sync", s.adminAuth(http.HandlerFunc(s.syncMarketDataFIX)))
	mux.Handle("GET /api/v1/admin/adapters", s.adminAuth(http.HandlerFunc(s.listAdapters)))
	mux.Handle("POST /api/v1/admin/adapters", s.adminAuth(http.HandlerFunc(s.createAdapter)))
	mux.Handle("PATCH /api/v1/admin/adapters/{id}", s.adminAuth(http.HandlerFunc(s.patchAdapter)))
	mux.Handle("POST /api/v1/admin/adapters/{id}/enable", s.adminAuth(http.HandlerFunc(s.enableAdapter)))
	mux.Handle("POST /api/v1/admin/adapters/{id}/disable", s.adminAuth(http.HandlerFunc(s.disableAdapter)))
	mux.Handle("POST /api/v1/admin/adapters/{id}/restart", s.adminAuth(http.HandlerFunc(s.restartAdapter)))
	mux.Handle("GET /api/v1/admin/audit", s.adminAuth(http.HandlerFunc(s.listAudit)))
	mux.Handle("GET /api/v1/admin/logs", s.adminAuth(http.HandlerFunc(s.listLogs)))
	mux.Handle("GET /api/v1/admin/logs/export", s.adminAuth(http.HandlerFunc(s.exportLogs)))
	mux.Handle("GET /api/v1/admin/logs/export/{id}", s.adminAuth(http.HandlerFunc(s.getExportJob)))
	mux.Handle("GET /api/v1/admin/logs/export/{id}/download", s.adminAuth(http.HandlerFunc(s.downloadExportJob)))
	mux.Handle("GET /api/v1/admin/logs/{id}", s.adminAuth(http.HandlerFunc(s.getLog)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/logs", s.adminAuth(http.HandlerFunc(s.listBrokerLogs)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics", s.adminAuth(http.HandlerFunc(s.brokerStatistics)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics/daily", s.adminAuth(http.HandlerFunc(s.brokerStatsDaily)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics/weekly", s.adminAuth(http.HandlerFunc(s.brokerStatsWeekly)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/statistics/monthly", s.adminAuth(http.HandlerFunc(s.brokerStatsMonthly)))
	mux.Handle("GET /api/v1/admin/errors", s.adminAuth(http.HandlerFunc(s.listErrors)))
	mux.Handle("GET /api/v1/admin/events", s.adminAuth(http.HandlerFunc(s.listEvents)))
	mux.Handle("GET /api/v1/admin/obs-audit", s.adminAuth(http.HandlerFunc(s.listObsAudit)))
	mux.Handle("GET /api/v1/admin/notifications", s.adminAuth(http.HandlerFunc(s.listNotifications)))
	mux.Handle("POST /api/v1/admin/notifications/read-all", s.adminAuth(http.HandlerFunc(s.readAllNotifications)))
	mux.Handle("POST /api/v1/admin/notifications/{id}/read", s.adminAuth(http.HandlerFunc(s.readNotification)))
	mux.Handle("GET /api/v1/admin/orders/{id}/trace", s.adminAuth(http.HandlerFunc(s.orderTrace)))
	mux.Handle("GET /api/v1/admin/users", s.adminAuth(http.HandlerFunc(s.listUsers)))
	mux.Handle("GET /api/v1/admin/instruments", s.adminAuth(http.HandlerFunc(s.listAdminInstruments)))
	mux.Handle("GET /api/v1/admin/firms", s.adminAuth(http.HandlerFunc(s.listAdminFirms)))
	mux.Handle("POST /api/v1/admin/instruments/sync", s.adminAuth(http.HandlerFunc(s.syncInstrumentsFIX)))
	mux.Handle("POST /api/v1/admin/quik/sync", s.adminAuth(http.HandlerFunc(s.syncInstrumentsFIX))) // alias
	mux.HandleFunc("GET /api/v1/admin/ws", s.handleWS)

	h := http.Handler(mux)
	h = metrics.Middleware(h)
	if s.obs != nil {
		h = s.obs.HTTPMiddleware(observability.HTTPConfig{
			TrustedProxies: observability.ParseTrustedProxies(s.cfg.TrustedProxies),
			FastMS:         s.cfg.ObsFastMS,
			NormalMS:       s.cfg.ObsNormalMS,
			SlowMS:         s.cfg.ObsSlowMS,
			SkipOrigins:    s.cfg.ObsSkipOrigins,
		}, h)
	}
	h = observability.RequestIDMiddleware(h)
	return s.requireHTTPS(s.cors(h))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	dbOK := s.pool.Ping(r.Context()) == nil
	pending, _ := s.outbox.PendingCount(r.Context())
	available := s.manager.AvailableCount()
	ready := dbOK && available > 0
	status := "ready"
	code := http.StatusOK
	if !ready {
		status = "not_ready"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"status": status, "database": dbOK, "queue": dbOK,
		"outbox_pending": pending, "adapters_available": available,
	})
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
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
		observability.AnnotateError(r, "ORDER_CREATE_FAILED", err.Error(), "order_service")
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
	s.hub.Broadcast("order.updated", orderDTO(order))
	code := http.StatusCreated
	if idempotent {
		code = http.StatusOK
	}
	writeJSON(w, code, map[string]any{"order": orderDTO(order), "idempotent": idempotent})
}

func (s *Server) listBrokerOrders(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	list, err := s.orders.ListForBroker(r.Context(), b.ID, 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": mapOrders(list)})
}

func (s *Server) getBrokerOrder(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	order, err := s.orders.GetForBroker(r.Context(), b.ID, id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	events, _ := s.orderStore.ListEvents(r.Context(), id)
	trades, _ := s.orderStore.ListTradesByOrder(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"order":  orderDTO(order),
		"events": mapEvents(events),
		"trades": mapTrades(trades),
	})
}

func (s *Server) cancelBrokerOrder(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	order, err := s.orders.Cancel(r.Context(), b.ID, id, clientIP(r))
	if err != nil {
		observability.AnnotateError(r, "ORDER_CANCEL_FAILED", err.Error(), "order_service")
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	observability.AnnotateOrder(r, order.ID, order.ClientOrderID, order.TradingAccount, order.Symbol, string(order.Side), order.Quantity.String(), "")
	s.hub.Broadcast("order.updated", orderDTO(order))
	writeJSON(w, http.StatusOK, map[string]any{"order": orderDTO(order)})
}

func (s *Server) listBrokerAccounts(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	list, err := s.brokers.ListAccounts(r.Context(), b.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []domain.Instrument
	for rows.Next() {
		var i domain.Instrument
		if err := rows.Scan(&i.ID, &i.InternalSymbol, &i.QuikSymbol, &i.Board, &i.Currency, &i.SecurityType, &i.Status); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, i)
	}
	writeJSON(w, http.StatusOK, map[string]any{"instruments": out})
}

func (s *Server) listAdminInstruments(w http.ResponseWriter, r *http.Request) {
	rows, err := s.pool.Query(r.Context(), `
		SELECT id, internal_symbol, quik_symbol, board, currency, security_type, status
		FROM instruments
		WHERE status = 'ACTIVE'
		ORDER BY board, quik_symbol, id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	var out []domain.Instrument
	for rows.Next() {
		var i domain.Instrument
		if err := rows.Scan(&i.ID, &i.InternalSymbol, &i.QuikSymbol, &i.Board, &i.Currency, &i.SecurityType, &i.Status); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, i)
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": "local", "instruments": out, "count": len(out)})
}

func (s *Server) listAdminFirms(w http.ResponseWriter, r *http.Request) {
	list, err := s.refdata.ListFirms(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	res, err := s.refdata.SyncFromFIX(r.Context(), items)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	res.AdapterID = adapterID
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}

func (s *Server) listTrades(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	list, err := s.orderStore.ListTrades(r.Context(), &b.ID, 100)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": mapTrades(list)})
}

func (s *Server) listBrokerPositions(w http.ResponseWriter, r *http.Request) {
	b, _ := BrokerFromContext(r.Context())
	// Token → broker; broker.code is the same firm id as in the positions snapshot (e.g. 2040).
	firm := strings.TrimSpace(b.Code)
	if firm == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"source": "fix", "firm_id": "", "instruments": []any{}, "cash": []any{},
			"note": "у брокера не задан код фирмы",
		})
		return
	}
	sn, items, err := s.positions.ListFromLatest(r.Context(), firm, "")
	if err != nil {
		if positions.IsNoSnapshot(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"source": "fix", "firm_id": firm, "instruments": []any{}, "cash": []any{},
				"note": "нет снимка позиций — выполните POST /api/v1/admin/positions/sync",
			})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := mapPositionsResponse(sn, items)
	resp["firm_id"] = firm
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
	return map[string]any{
		"source":      "fix",
		"snapshot":    sn,
		"instruments": instruments,
		"cash":        cash,
	}
}

func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
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
		ip, _ := s.clientIP(r)
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
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"token": token,
		"user":  map[string]any{"id": u.ID, "email": u.Email, "role": u.RoleCode, "full_name": u.FullName},
	})
	ip, _ := s.clientIP(r)
	s.emitObs(observability.Event{
		Type: observability.TypeAuth, Subtype: observability.AuthSuccess, ClientIP: ip,
		Endpoint: r.URL.Path, HTTPMethod: r.Method, RequestID: observability.RequestIDFrom(r.Context()),
		ActorType: "admin", ActorID: u.ID.String(), Message: "admin login",
	})
	s.writeAdminAudit(r, "USER_LOGIN", "user", u.ID.String(), nil, nil, map[string]any{"email": u.Email})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	stats, _ := s.orderStore.StatsToday(r.Context())
	ads, _ := s.manager.ListRuntime(r.Context())
	brokersList, _ := s.brokers.ListBrokers(r.Context())
	var accounts int64
	_ = s.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM broker_accounts WHERE status='ACTIVE'`).Scan(&accounts)
	dbOK := s.pool.Ping(r.Context()) == nil
	obs := map[string]any{}
	var topBrokers any = []any{}
	var topErrors any = []any{}
	connected := s.connectedBrokers(r.Context())
	if s.obs != nil {
		if ov, err := s.obs.Store().Overview(r.Context()); err == nil {
			obs = map[string]any{
				"requests_today": ov.RequestsToday, "orders_today": ov.OrdersToday,
				"successful": ov.Successful, "failed": ov.Failed, "success_rate": ov.SuccessRate,
				"avg_latency_ms": ov.AvgLatencyMs, "p95_latency_ms": ov.P95LatencyMs, "p99_latency_ms": ov.P99LatencyMs,
				"errors_today": ov.ErrorsToday, "critical_today": ov.CriticalToday,
				"auth_failures_today": ov.AuthFailuresToday, "active_brokers": ov.ActiveBrokers,
				"connected_now": len(connected),
			}
		}
		day := time.Now().Add(-24 * time.Hour)
		if tb, err := s.obs.Store().TopBrokers(r.Context(), day, 10); err == nil {
			topBrokers = tb
		}
		if te, err := s.obs.Store().TopErrors(r.Context(), day, 10); err == nil {
			topErrors = te
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"system": map[string]any{
			"api": "ONLINE", "database": boolStatus(dbOK), "queue": boolStatus(dbOK),
		},
		"adapters":           ads,
		"stats":              stats,
		"brokers":            len(brokersList),
		"active_accounts":    accounts,
		"adapters_available": s.manager.AvailableCount(),
		"observability":      obs,
		"connected_brokers":  connected,
		"top_brokers":        topBrokers,
		"top_errors":         topErrors,
	})
}

func (s *Server) connectedBrokers(ctx context.Context) []observability.ConnectedBroker {
	if s.obs == nil {
		return []observability.ConnectedBroker{}
	}
	list, err := s.obs.Store().ConnectedBrokers(ctx, observability.ConnectedWindow)
	if err != nil || list == nil {
		return []observability.ConnectedBroker{}
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
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	safe := make([]map[string]any, 0, len(list))
	for _, t := range list {
		safe = append(safe, safeToken(t))
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
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"stats": st})
}

func (s *Server) listIPWhitelist(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := s.brokers.ListIPWhitelist(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		Code, Name, QuikFirm, TradingAccount string
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	a, err := s.brokers.CreateAccount(r.Context(), id, req.Code, req.Name, req.QuikFirm, req.TradingAccount)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"account": a})
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	list, err := s.brokers.ListAccounts(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": list})
}

func (s *Server) listAdminOrders(w http.ResponseWriter, r *http.Request) {
	list, err := s.orderStore.List(r.Context(), nil, 200)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": mapOrders(list)})
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
			writeErr(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listAdminTrades(w http.ResponseWriter, r *http.Request) {
	list, err := s.orderStore.ListTrades(r.Context(), nil, 500)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": mapTrades(list)})
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
	sn, items, err := s.positions.ListFromLatest(r.Context(), firm, "")
	if err != nil {
		if positions.IsNoSnapshot(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"source": "fix", "instruments": []any{}, "cash": []any{},
				"note": "нет снимка позиций — нажмите «Запросить по FIX»",
			})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapPositionsResponse(sn, items))
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
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	_, listed, _ := s.positions.ListFromLatest(r.Context(), req.Firm, "")
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
	if adapterID, items, ok := s.manager.LiveQuotes(); ok {
		items = filterQuotes(items, board, symbol)
		writeJSON(w, http.StatusOK, map[string]any{
			"source": "live",
			"live":   true,
			"snapshot": marketdata.Snapshot{
				AdapterID:   adapterID,
				Status:      "OK",
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
				"note": "нет котировок — нажмите «Запросить по FIX»",
			})
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, mapMarketDataResponse(sn, items))
}

func (s *Server) syncMarketDataFIX(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Symbol string `json:"symbol"`
		Board  string `json:"board"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	q := domain.MarketDataQuery{Symbol: strings.TrimSpace(req.Symbol), Board: strings.TrimSpace(req.Board)}
	if q.Symbol == "" {
		if list, err := s.refdata.ListActiveInstruments(r.Context()); err == nil {
			q.Instruments = list
		}
	}

	adapterID, reqID, items, err := s.manager.SubscribeMarketData(r.Context(), q)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.hub.Broadcast("marketdata.tick", map[string]any{"quotes": items})
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "adapter_id": adapterID, "md_req_id": reqID, "live": true,
		"received": len(items), "summary": marketdata.FormatReceived(len(items)),
		"result": map[string]any{"source": "live", "live": true, "quotes": items},
	})
}

func (s *Server) getAdminOrder(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	order, err := s.orderStore.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	events, _ := s.orderStore.ListEvents(r.Context(), id)
	trades, _ := s.orderStore.ListTradesByOrder(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"order":  orderDTO(order),
		"events": mapEvents(events),
		"trades": mapTrades(trades),
	})
}

func (s *Server) listAdapters(w http.ResponseWriter, r *http.Request) {
	list, err := s.manager.ListRuntime(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
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
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"audit": list})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.users.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": list})
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		writeErr(w, http.StatusUnauthorized, "missing token")
		return
	}
	if _, err := auth.ParseAdminJWT(s.cfg.JWTSecret, token); err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid token")
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.hub.Register(conn)
	defer s.hub.Unregister(conn)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
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

// OnExecution is wired from main to apply ERs and broadcast.
func (s *Server) OnExecution(report adapters.ExecutionReport) {
	ctx := context.Background()
	lastQty := parseDec(report.LastQty)
	lastPx := parseDec(report.LastPx)
	cumQty := parseDec(report.CumQty)
	leaves := parseDec(report.LeavesQty)
	if report.CancelReject {
		order, err := s.orderStore.ApplyCancelReject(ctx, report.ClOrdID, report.OrigClOrdID, report.OrderID, report.AdapterID,
			strings.TrimSpace(report.Text+" "+report.CxlRejReason), report.RawFIX)
		if err != nil {
			s.log.Error("apply cancel reject failed", "error", err, "cl_ord_id", report.ClOrdID, "orig_cl_ord_id", report.OrigClOrdID)
			return
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
		s.hub.Broadcast("order.updated", orderDTO(order))
		return
	}
	order, exec, err := s.orderStore.ApplyExecutionReport(ctx, report.ClOrdID, report.OrigClOrdID, report.ExecID, report.ExecType, report.OrdStatus,
		report.OrderID, lastQty, lastPx, cumQty, leaves, report.AdapterID, report.Text, report.OrdRejReason, report.RawFIX)
	if err != nil {
		s.log.Error("apply execution report failed", "error", err, "cl_ord_id", report.ClOrdID,
			"text", report.Text, "ord_rej_reason", report.OrdRejReason)
		return
	}
	if ad, ok := s.manager.Get(report.AdapterID); ok {
		if order.Status == domain.OrderFilled || order.Status == domain.OrderCancelled || order.Status == domain.OrderRejected {
			ad.IncActive(-1)
		}
	}
	s.hub.Broadcast("order.updated", orderDTO(order))
	if exec != nil {
		s.hub.Broadcast("trade.created", exec)
	}
	s.emitOrderLifecycle(order, report)
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

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "docs/openapi.yaml")
}

func (s *Server) handleBrokerOpenAPI(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "docs/broker-openapi.yaml")
}

func (s *Server) handleDocsAsset(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, r.URL.Path[1:]) // strip leading /
}

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><meta charset="utf-8"/><title>FIX Gateway API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/></head>
<body><div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:'/api/openapi.yaml', dom_id:'#swagger-ui'});</script>
</body></html>`))
}

func (s *Server) handleBrokerSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head>
<meta charset="utf-8"/>
<title>KSE Broker API</title>
<link rel="icon" type="image/png" href="/docs/kse-favicon.png"/>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/>
<style>
  body { margin: 0; background: #fafbfc; }
  .kse-header {
    background: #1a1a2e;
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
  <img src="/docs/kse-logo.png" alt="KSE"/>
  <div>
    <h1>Кыргыз Фондулук Биржасы — Broker API</h1>
    <p>Кыргызская Фондовая Биржа · Kyrgyz Stock Exchange</p>
  </div>
</div>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:'/api/broker-openapi.yaml', dom_id:'#swagger-ui', deepLinking:true, defaultModelsExpandDepth:1});</script>
</body></html>`))
}

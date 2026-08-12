package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/fix-gateway/backend/internal/adapters"
	"github.com/fix-gateway/backend/internal/admin"
	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/auth"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/config"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/metrics"
	"github.com/fix-gateway/backend/internal/orders"
	"github.com/fix-gateway/backend/internal/outbox"
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
) *Server {
	return &Server{
		cfg: cfg, log: log, pool: pool, brokers: brokerStore, orders: orderSvc, orderStore: orderStore,
		adapters: adapterStore, manager: manager, outbox: outboxStore, audit: auditStore, users: users, hub: hub,
		refdata: refdata.NewStore(pool),
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		authFailLimiter: ratelimit.NewSlidingWindow(time.Minute, cfg.AuthFailMaxPerMinute),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /ready", s.handleReady)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /api/openapi.yaml", s.handleOpenAPI)
	mux.HandleFunc("GET /swagger", s.handleSwaggerUI)

	mux.Handle("POST /api/v1/orders", s.brokerAuth(s.requirePermission(auth.PermOrdersCreate)(s.orderRateLimit(http.HandlerFunc(s.createOrder)))))
	mux.Handle("GET /api/v1/orders", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerOrders))))
	mux.Handle("GET /api/v1/orders/{id}", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.getBrokerOrder))))
	mux.Handle("POST /api/v1/orders/{id}/cancel", s.brokerAuth(s.requirePermission(auth.PermOrdersCancel)(http.HandlerFunc(s.cancelBrokerOrder))))
	mux.Handle("GET /api/v1/accounts", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listBrokerAccounts))))
	mux.Handle("GET /api/v1/firm", s.brokerAuth(http.HandlerFunc(s.getBrokerFirm)))
	mux.Handle("GET /api/v1/instruments", s.brokerAuth(s.requirePermission(auth.PermOrdersRead)(http.HandlerFunc(s.listInstruments))))
	mux.Handle("GET /api/v1/trades", s.brokerAuth(s.requirePermission(auth.PermTradesRead)(http.HandlerFunc(s.listTrades))))

	mux.HandleFunc("POST /api/v1/admin/login", s.adminLogin)
	mux.Handle("GET /api/v1/admin/dashboard", s.adminAuth(http.HandlerFunc(s.dashboard)))
	mux.Handle("POST /api/v1/admin/brokers", s.adminAuth(http.HandlerFunc(s.createBroker)))
	mux.Handle("GET /api/v1/admin/brokers", s.adminAuth(http.HandlerFunc(s.listBrokers)))
	mux.Handle("GET /api/v1/admin/brokers/{id}", s.adminAuth(http.HandlerFunc(s.getBroker)))
	mux.Handle("PATCH /api/v1/admin/brokers/{id}", s.adminAuth(http.HandlerFunc(s.patchBroker)))
	mux.Handle("POST /api/v1/admin/brokers/{id}/tokens", s.adminAuth(http.HandlerFunc(s.createToken)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/tokens", s.adminAuth(http.HandlerFunc(s.listTokens)))
	mux.Handle("GET /api/v1/admin/brokers/{id}/security", s.adminAuth(http.HandlerFunc(s.brokerSecurity)))
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
	mux.Handle("GET /api/v1/admin/trades", s.adminAuth(http.HandlerFunc(s.listAdminTrades)))
	mux.Handle("GET /api/v1/admin/adapters", s.adminAuth(http.HandlerFunc(s.listAdapters)))
	mux.Handle("POST /api/v1/admin/adapters", s.adminAuth(http.HandlerFunc(s.createAdapter)))
	mux.Handle("PATCH /api/v1/admin/adapters/{id}", s.adminAuth(http.HandlerFunc(s.patchAdapter)))
	mux.Handle("POST /api/v1/admin/adapters/{id}/enable", s.adminAuth(http.HandlerFunc(s.enableAdapter)))
	mux.Handle("POST /api/v1/admin/adapters/{id}/disable", s.adminAuth(http.HandlerFunc(s.disableAdapter)))
	mux.Handle("POST /api/v1/admin/adapters/{id}/restart", s.adminAuth(http.HandlerFunc(s.restartAdapter)))
	mux.Handle("GET /api/v1/admin/audit", s.adminAuth(http.HandlerFunc(s.listAudit)))
	mux.Handle("GET /api/v1/admin/users", s.adminAuth(http.HandlerFunc(s.listUsers)))
	mux.Handle("GET /api/v1/admin/instruments", s.adminAuth(http.HandlerFunc(s.listAdminInstruments)))
	mux.Handle("GET /api/v1/admin/firms", s.adminAuth(http.HandlerFunc(s.listAdminFirms)))
	mux.Handle("POST /api/v1/admin/instruments/sync", s.adminAuth(http.HandlerFunc(s.syncInstrumentsFIX)))
	mux.Handle("POST /api/v1/admin/quik/sync", s.adminAuth(http.HandlerFunc(s.syncInstrumentsFIX))) // alias
	mux.HandleFunc("GET /api/v1/admin/ws", s.handleWS)

	return s.requireHTTPS(s.cors(metrics.Middleware(mux)))
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
		writeErrDetails(w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	order, idempotent, err := s.orders.Create(r.Context(), b, clientIP(r), req, tok.QuikFirm)
	if err != nil {
		writeErrDetails(w, http.StatusBadRequest, err.Error(), orderFieldHints())
		return
	}
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
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
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
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	stats, _ := s.orderStore.StatsToday(r.Context())
	ads, _ := s.manager.ListRuntime(r.Context())
	brokersList, _ := s.brokers.ListBrokers(r.Context())
	var accounts int64
	_ = s.pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM broker_accounts WHERE status='ACTIVE'`).Scan(&accounts)
	dbOK := s.pool.Ping(r.Context()) == nil
	writeJSON(w, http.StatusOK, map[string]any{
		"system": map[string]any{
			"api": "ONLINE", "database": boolStatus(dbOK), "queue": boolStatus(dbOK),
		},
		"adapters":       ads,
		"stats":          stats,
		"brokers":        len(brokersList),
		"active_accounts": accounts,
		"adapters_available": s.manager.AvailableCount(),
	})
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
	writeJSON(w, http.StatusCreated, map[string]any{"broker": b})
}

func (s *Server) listBrokers(w http.ResponseWriter, r *http.Request) {
	list, err := s.brokers.ListBrokers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"brokers": list})
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
		RateLimitRPS: req.RateLimitRPS, RateLimitOrdersPerSec: req.RateLimitOrdersPerSec,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
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
		"quik_firm": t.QuikFirm,
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
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": t.ID, "name": t.Name, "token": created.Raw, "expires_at": t.ExpiresAt,
		"old_token_id": id, "old_token_status": "ACTIVE",
		"message": "Old token remains ACTIVE. Revoke it after the broker switches to the new token.",
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

func (s *Server) listAdminTrades(w http.ResponseWriter, r *http.Request) {
	list, err := s.orderStore.ListTrades(r.Context(), nil, 500)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"trades": mapTrades(list)})
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
	if err := s.manager.Enable(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "enabled"})
}

func (s *Server) disableAdapter(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.Disable(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
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
	order, exec, err := s.orderStore.ApplyExecutionReport(ctx, report.ClOrdID, report.ExecID, report.ExecType, report.OrdStatus,
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

func (s *Server) handleSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	_, _ = w.Write([]byte(`<!doctype html>
<html><head><title>FIX Gateway API</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/></head>
<body><div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>SwaggerUIBundle({url:'/api/openapi.yaml', dom_id:'#swagger-ui'});</script>
</body></html>`))
}

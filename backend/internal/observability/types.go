package observability

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	TypeAPI      = "API_REQUEST"
	TypeBusiness = "BUSINESS"
	TypeAuth     = "AUTH"
	TypeFIX      = "FIX"
	TypeAdapter  = "ADAPTER"
	TypeSystem   = "SYSTEM"
	TypeError    = "ERROR"
	TypeAudit    = "AUDIT"
)

const (
	SeverityDebug    = "DEBUG"
	SeverityInfo     = "INFO"
	SeverityWarning  = "WARNING"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"
)

const (
	ResultSuccess = "SUCCESS"
	ResultFailure = "FAILURE"
)

const (
	PerfFast     = "FAST"
	PerfNormal   = "NORMAL"
	PerfSlow     = "SLOW"
	PerfVerySlow = "VERY_SLOW"
	PerfTimeout  = "TIMEOUT"
)

// API subtypes
const (
	SubOrderCreate    = "ORDER_CREATE"
	SubOrderCancel    = "ORDER_CANCEL"
	SubOrderReplace   = "ORDER_REPLACE"
	SubOrderRead      = "ORDER_READ"
	SubOrdersList     = "ORDERS_LIST"
	SubTradeRead      = "TRADE_READ"
	SubTradesList     = "TRADES_LIST"
	SubPositionRead   = "POSITION_READ"
	SubPositionsList  = "POSITIONS_LIST"
	SubInstrumentRead = "INSTRUMENT_READ"
	SubAccountRead    = "ACCOUNT_READ"
	SubBrokerRequest  = "BROKER_REQUEST"
	SubTokenRequest   = "TOKEN_REQUEST"
	SubHealthRequest  = "HEALTH_REQUEST"
	SubAdminRequest   = "ADMIN_REQUEST"
	SubUnknown        = "UNKNOWN"
)

// Business
const (
	BizOrderCreated         = "ORDER_CREATED"
	BizOrderValidated       = "ORDER_VALIDATED"
	BizOrderRejected        = "ORDER_REJECTED"
	BizOrderQueued          = "ORDER_QUEUED"
	BizOrderRouted          = "ORDER_ROUTED"
	BizOrderAssigned        = "ORDER_ASSIGNED"
	BizOrderSent            = "ORDER_SENT"
	BizOrderAcknowledged    = "ORDER_ACKNOWLEDGED"
	BizOrderPartiallyFilled = "ORDER_PARTIALLY_FILLED"
	BizOrderFilled          = "ORDER_FILLED"
	BizOrderCancelRequested = "ORDER_CANCEL_REQUESTED"
	BizOrderCancelRejected  = "ORDER_CANCEL_REJECTED"
	BizOrderCancelled       = "ORDER_CANCELLED"
	BizOrderReplaced        = "ORDER_REPLACED"
	BizOrderFailed          = "ORDER_FAILED"
)

// Auth
const (
	AuthSuccess           = "AUTH_SUCCESS"
	AuthFailed            = "AUTH_FAILED"
	AuthTokenExpired      = "TOKEN_EXPIRED"
	AuthTokenRevoked      = "TOKEN_REVOKED"
	AuthTokenNotFound     = "TOKEN_NOT_FOUND"
	AuthPermissionDenied  = "TOKEN_PERMISSION_DENIED"
	AuthIPNotAllowed      = "IP_NOT_ALLOWED"
	AuthRateLimitExceeded = "RATE_LIMIT_EXCEEDED"
	AuthHMACFailed        = "HMAC_VALIDATION_FAILED"
	AuthNonceReplay       = "NONCE_REPLAY_DETECTED"
)

// Errors
const (
	ErrAuth       = "AUTH_ERROR"
	ErrValidation = "VALIDATION_ERROR"
	ErrDatabase   = "DATABASE_ERROR"
	ErrQueue      = "QUEUE_ERROR"
	ErrFIX        = "FIX_ERROR"
	ErrAdapter    = "ADAPTER_ERROR"
	ErrNetwork    = "NETWORK_ERROR"
	ErrTimeout    = "TIMEOUT"
	ErrRateLimit  = "RATE_LIMIT_ERROR"
	ErrSystem     = "SYSTEM_ERROR"
	ErrInternal   = "INTERNAL_ERROR"
)

// Critical subtypes
const (
	CritAllAdaptersOffline  = "ALL_ADAPTERS_OFFLINE"
	CritDatabaseUnavailable = "DATABASE_UNAVAILABLE"
	CritQueueUnavailable    = "QUEUE_UNAVAILABLE"
	CritFIXSessionDown      = "FIX_SESSION_DOWN"
	CritMultipleOrderFails  = "MULTIPLE_ORDER_FAILURES"
	CritHighErrorRate       = "HIGH_ERROR_RATE"
)

type Event struct {
	ID             uuid.UUID       `json:"id"`
	Timestamp      time.Time       `json:"timestamp"`
	Type           string          `json:"type"`
	Subtype        string          `json:"subtype"`
	Severity       string          `json:"severity"`
	RequestID      string          `json:"request_id,omitempty"`
	TraceID        string          `json:"trace_id,omitempty"`
	BrokerID       *int64          `json:"broker_id,omitempty"`
	BrokerCode     string          `json:"broker_code,omitempty"`
	BrokerName     string          `json:"broker_name,omitempty"`
	TokenID        *uuid.UUID      `json:"token_id,omitempty"`
	ClientIP       string          `json:"client_ip,omitempty"`
	ForwardedFor   string          `json:"forwarded_for,omitempty"`
	UserAgent      string          `json:"user_agent,omitempty"`
	HTTPMethod     string          `json:"http_method,omitempty"`
	Endpoint       string          `json:"endpoint,omitempty"`
	Route          string          `json:"route,omitempty"`
	QueryParams    string          `json:"query_params,omitempty"`
	Protocol       string          `json:"protocol,omitempty"`
	StatusCode     *int            `json:"status_code,omitempty"`
	ResponseTimeMs *int            `json:"response_time_ms,omitempty"`
	RequestSize    *int            `json:"request_size,omitempty"`
	ResponseSize   *int            `json:"response_size,omitempty"`
	PerfClass      string          `json:"perf_class,omitempty"`
	Result         string          `json:"result,omitempty"`
	OrderID        *uuid.UUID      `json:"order_id,omitempty"`
	ClientOrderID  string          `json:"client_order_id,omitempty"`
	AccountID      string          `json:"account_id,omitempty"`
	Instrument     string          `json:"instrument,omitempty"`
	Side           string          `json:"side,omitempty"`
	Quantity       string          `json:"quantity,omitempty"`
	Price          string          `json:"price,omitempty"`
	AdapterID      string          `json:"adapter_id,omitempty"`
	SessionID      string          `json:"session_id,omitempty"`
	FIXMsgType     string          `json:"fix_msg_type,omitempty"`
	FIXDirection   string          `json:"fix_direction,omitempty"`
	FIXSeqNum      *int            `json:"fix_seq_num,omitempty"`
	ErrorCode      string          `json:"error_code,omitempty"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	FailureStage   string          `json:"failure_stage,omitempty"`
	Message        string          `json:"message,omitempty"`
	ActorType      string          `json:"actor_type,omitempty"`
	ActorID        string          `json:"actor_id,omitempty"`
	TargetType     string          `json:"target_type,omitempty"`
	TargetID       string          `json:"target_id,omitempty"`
	Before         json.RawMessage `json:"before,omitempty"`
	After          json.RawMessage `json:"after,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

func (e *Event) Normalize() {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = e.Timestamp
	}
	e.ErrorMessage = RedactSecrets(e.ErrorMessage)
	e.Message = RedactSecrets(e.Message)
	if e.Severity == "" {
		e.Severity = InferSeverity(*e)
	}
	if e.Result == "" {
		if e.StatusCode != nil && *e.StatusCode >= 400 {
			e.Result = ResultFailure
		} else if e.Type == TypeError || e.Severity == SeverityError || e.Severity == SeverityCritical {
			e.Result = ResultFailure
		} else if e.Type == TypeAuth && e.Subtype != AuthSuccess {
			e.Result = ResultFailure
		} else {
			e.Result = ResultSuccess
		}
	}
}

func InferSeverity(e Event) string {
	switch e.Subtype {
	case CritAllAdaptersOffline, CritDatabaseUnavailable, CritQueueUnavailable, CritFIXSessionDown, CritMultipleOrderFails, CritHighErrorRate:
		return SeverityCritical
	}
	if e.Type == TypeError {
		if e.Subtype == ErrTimeout || e.Subtype == ErrFIX || e.Subtype == ErrAdapter || e.Subtype == ErrDatabase || e.Subtype == ErrQueue {
			return SeverityError
		}
		return SeverityError
	}
	if e.StatusCode != nil {
		if *e.StatusCode >= 500 {
			return SeverityError
		}
		if *e.StatusCode >= 400 {
			return SeverityWarning
		}
	}
	switch e.Subtype {
	case AuthFailed, AuthTokenExpired, AuthTokenNotFound, AuthIPNotAllowed, AuthHMACFailed, AuthNonceReplay, AuthRateLimitExceeded, AuthPermissionDenied:
		return SeverityWarning
	}
	return SeverityInfo
}

func PerfClass(ms int, fast, normal, slow int) string {
	if fast <= 0 {
		fast = 100
	}
	if normal <= 0 {
		normal = 500
	}
	if slow <= 0 {
		slow = 2000
	}
	switch {
	case ms < 0:
		return ""
	case ms < fast:
		return PerfFast
	case ms < normal:
		return PerfNormal
	case ms < slow:
		return PerfSlow
	default:
		return PerfVerySlow
	}
}

type Filter struct {
	From          time.Time
	To            time.Time
	BrokerID      *int64
	Type          string
	Types         []string
	Subtype       string
	Severity      string
	Status        string
	StatusCode    *int
	ClientIP      string
	RequestID     string
	TraceID       string
	OrderID       *uuid.UUID
	ClientOrderID string
	AdapterID     string
	Endpoint      string
	HTTPMethod    string
	ErrorCode     string
	Result        string
	CursorTS      time.Time
	CursorID      uuid.UUID
	Limit         int
}

type Notification struct {
	ID          uuid.UUID       `json:"id"`
	Fingerprint string          `json:"fingerprint"`
	Severity    string          `json:"severity"`
	EventType   string          `json:"event_type"`
	Subtype     string          `json:"subtype"`
	ErrorCode   string          `json:"error_code,omitempty"`
	BrokerID    *int64          `json:"broker_id,omitempty"`
	BrokerName  string          `json:"broker_name,omitempty"`
	Title       string          `json:"title"`
	Message     string          `json:"message"`
	Count       int             `json:"count"`
	WindowStart time.Time       `json:"window_start"`
	LastSeenAt  time.Time       `json:"last_seen_at"`
	ReadAt      *time.Time      `json:"read_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

type ExportJob struct {
	ID          uuid.UUID       `json:"id"`
	AdminUserID string          `json:"admin_user_id,omitempty"`
	Format      string          `json:"format"`
	Status      string          `json:"status"`
	Filter      json.RawMessage `json:"filter,omitempty"`
	FilePath    string          `json:"-"`
	RowCount    *int            `json:"row_count,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type Emitter interface {
	Emit(e Event)
}

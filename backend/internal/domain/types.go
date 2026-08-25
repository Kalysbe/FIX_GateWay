package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type BrokerStatus string

const (
	BrokerActive   BrokerStatus = "ACTIVE"
	BrokerBlocked  BrokerStatus = "BLOCKED"
	BrokerDisabled BrokerStatus = "DISABLED"
)

type TokenStatus string

const (
	TokenActive   TokenStatus = "ACTIVE"
	TokenRevoked  TokenStatus = "REVOKED"
	TokenExpired  TokenStatus = "EXPIRED"
	TokenDisabled TokenStatus = "DISABLED"
)

type OrderStatus string

const (
	OrderReceived        OrderStatus = "RECEIVED"
	OrderValidating      OrderStatus = "VALIDATING"
	OrderValidated       OrderStatus = "VALIDATED"
	OrderQueued          OrderStatus = "QUEUED"
	OrderRouting         OrderStatus = "ROUTING"
	OrderSent            OrderStatus = "SENT"
	OrderAcknowledged    OrderStatus = "ACKNOWLEDGED"
	OrderPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderFilled          OrderStatus = "FILLED"
	OrderCancelPending   OrderStatus = "CANCEL_PENDING"
	OrderCancelled       OrderStatus = "CANCELLED"
	OrderRejected        OrderStatus = "REJECTED"
	OrderExpired         OrderStatus = "EXPIRED"
	OrderFailed          OrderStatus = "FAILED"
)

type AdapterStatus string

const (
	AdapterOnline   AdapterStatus = "ONLINE"
	AdapterDegraded AdapterStatus = "DEGRADED"
	AdapterOffline  AdapterStatus = "OFFLINE"
	AdapterStarting AdapterStatus = "STARTING"
	AdapterStopping AdapterStatus = "STOPPING"
	AdapterUnknown  AdapterStatus = "UNKNOWN"
)

type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

type OrderType string

const (
	OrderTypeMarket OrderType = "MARKET"
	OrderTypeLimit  OrderType = "LIMIT"
)

type OutboxStatus string

const (
	OutboxPending    OutboxStatus = "PENDING"
	OutboxProcessing OutboxStatus = "PROCESSING"
	OutboxDone       OutboxStatus = "DONE"
	OutboxFailed     OutboxStatus = "FAILED"
)

type Broker struct {
	ID                    int64        `json:"id"`
	Code                  string       `json:"code"`
	Name                  string       `json:"name"`
	Description           string       `json:"description"`
	Status                BrokerStatus `json:"status"`
	IPWhitelistEnabled    bool         `json:"ip_whitelist_enabled"`
	RateLimitRPS          int          `json:"rate_limit_rps"`
	RateLimitOrdersPerSec int          `json:"rate_limit_orders_per_sec"`
	CreatedAt             time.Time    `json:"created_at"`
	UpdatedAt             time.Time    `json:"updated_at"`
}

type BrokerIPAllow struct {
	ID        int64     `json:"id"`
	BrokerID  int64     `json:"broker_id"`
	IPCIDR    string    `json:"ip_cidr"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type APIToken struct {
	ID          uuid.UUID   `json:"id"`
	BrokerID    int64       `json:"broker_id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	TokenHash   string      `json:"-"`
	TokenPrefix string      `json:"token_prefix"`
	Status      TokenStatus `json:"status"`
	Permissions []string    `json:"permissions"`
	QuikFirm    string      `json:"quik_firm"` // FIX tag 5018
	ExpiresAt   *time.Time  `json:"expires_at,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
	RevokedAt   *time.Time  `json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time  `json:"last_used_at,omitempty"`
	LastUsedIP  *string     `json:"last_used_ip,omitempty"`
	CreatedBy   *uuid.UUID  `json:"created_by,omitempty"`
	RevokedBy   *uuid.UUID  `json:"revoked_by,omitempty"`
	RotatedFrom *uuid.UUID  `json:"rotated_from,omitempty"`
}

type BrokerAccount struct {
	ID             int64     `json:"id"`
	BrokerID       int64     `json:"broker_id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	QuikFirm       string    `json:"quik_firm"`
	TradingAccount string    `json:"trading_account"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Instrument struct {
	ID             int64  `json:"id"`
	InternalSymbol string `json:"internal_symbol"`
	QuikSymbol     string `json:"quik_symbol"`
	Board          string `json:"board"`
	Currency       string `json:"currency"`
	SecurityType   string `json:"security_type"`
	Status         string `json:"status"`
}

type Adapter struct {
	ID                  string        `json:"id"`
	Name                string        `json:"name"`
	Type                string        `json:"type"`
	Host                string        `json:"host"`
	Port                int           `json:"port"`
	BeginString         string        `json:"begin_string"`
	SenderCompID        string        `json:"sender_comp_id"`
	TargetCompID        string        `json:"target_comp_id"`
	HeartbeatInterval   int           `json:"heartbeat_interval"`
	ResetOnLogon        bool          `json:"reset_on_logon"`
	UsernameEnv         string        `json:"-"`
	PasswordEnv         string        `json:"-"`
	Status              AdapterStatus `json:"status"`
	Priority            int           `json:"priority"`
	Weight              int           `json:"weight"`
	MaxConcurrentOrders int           `json:"max_concurrent_orders"`
	Enabled             bool          `json:"enabled"`
	ActiveOrders        int64         `json:"active_orders"`
	ProcessedOrders     int64         `json:"processed_orders"`
	ErrorCount          int64         `json:"error_count"`
	LastHeartbeatAt     *time.Time    `json:"last_heartbeat_at,omitempty"`
	StartedAt           *time.Time    `json:"started_at,omitempty"`
	CreatedAt           time.Time     `json:"created_at"`
	UpdatedAt           time.Time     `json:"updated_at"`
}

type Order struct {
	ID             uuid.UUID        `json:"id"`
	BrokerID       int64            `json:"broker_id"`
	AccountID      int64            `json:"account_id"`
	ClientOrderID  string           `json:"client_order_id"`
	InstrumentID   int64            `json:"instrument_id"`
	Side           Side             `json:"side"`
	OrderType      OrderType        `json:"order_type"`
	TimeInForce    string           `json:"time_in_force"`
	Quantity       decimal.Decimal  `json:"quantity"`
	Price          *decimal.Decimal `json:"price,omitempty"`
	FilledQuantity decimal.Decimal  `json:"filled_quantity"`
	AvgPrice       decimal.Decimal  `json:"avg_price"`
	Status         OrderStatus      `json:"status"`
	AdapterID      *string          `json:"adapter_id,omitempty"`
	FixClOrdID     *string          `json:"fix_cl_ord_id,omitempty"`
	QuikOrderID    *string          `json:"quik_order_id,omitempty"`
	RejectReason   *string          `json:"reject_reason,omitempty"`
	CreatedAt      time.Time        `json:"created_at"`
	UpdatedAt      time.Time        `json:"updated_at"`

	Symbol         string `json:"symbol,omitempty"`
	Board          string `json:"board,omitempty"`
	TradingAccount string `json:"trading_account,omitempty"`
	Currency       string `json:"currency,omitempty"`
	ClientCode     string `json:"client_code,omitempty"` // FIX tag 109
	QuikFirm       string `json:"quik_firm,omitempty"`   // FIX tag 5018
	FirmName       string `json:"firm_name,omitempty"`   // from quik_firms
	RequestID      string `json:"request_id,omitempty"`
	TraceID        string `json:"trace_id,omitempty"`
}

type OrderEvent struct {
	ID         int64     `json:"id"`
	OrderID    uuid.UUID `json:"order_id"`
	EventType  string    `json:"event_type"`
	FromStatus *string   `json:"from_status,omitempty"`
	ToStatus   *string   `json:"to_status,omitempty"`
	AdapterID  *string   `json:"adapter_id,omitempty"`
	Message    string    `json:"message"`
	Payload    []byte    `json:"payload,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type Execution struct {
	ID        uuid.UUID       `json:"id"`
	OrderID   uuid.UUID       `json:"order_id"`
	ExecID    string          `json:"exec_id"`
	LastQty   decimal.Decimal `json:"last_qty"`
	LastPx    decimal.Decimal `json:"last_px"`
	CumQty    decimal.Decimal `json:"cum_qty"`
	LeavesQty decimal.Decimal `json:"leaves_qty"`
	AdapterID *string         `json:"adapter_id,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Trade is an execution enriched with order/FIX context for list APIs.
type Trade struct {
	ID            uuid.UUID       `json:"id"`
	OrderID       uuid.UUID       `json:"order_id"`
	ExecID        string          `json:"exec_id"`
	LastQty       decimal.Decimal `json:"last_qty"`
	LastPx        decimal.Decimal `json:"last_px"`
	CumQty        decimal.Decimal `json:"cum_qty"`
	LeavesQty     decimal.Decimal `json:"leaves_qty"`
	AdapterID     *string         `json:"adapter_id,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	BrokerID      int64           `json:"broker_id"`
	ClientOrderID string          `json:"client_order_id"`
	Symbol        string          `json:"symbol"`
	Board         string          `json:"board"`
	Side          string          `json:"side"`
	ClientCode    string          `json:"client_code"`
	QuikFirm      string          `json:"quik_firm"`
	FirmName      string          `json:"firm_name"`
	FixClOrdID    *string         `json:"fix_cl_ord_id,omitempty"`
	QuikOrderID   *string         `json:"quik_order_id,omitempty"`
}

type OutboxEvent struct {
	ID            int64
	AggregateType string
	AggregateID   string
	EventType     string
	Payload       []byte
	Status        OutboxStatus
	Attempts      int
	AvailableAt   time.Time
	LockedBy      *string
	LockedAt      *time.Time
	LastError     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type User struct {
	ID           uuid.UUID `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	FullName     string    `json:"full_name"`
	RoleCode     string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type HealthDetails struct {
	ProcessAlive    bool
	TCPConnected    bool
	SessionLoggedOn bool
	QuikConnected   *bool
	Message         string
}

// PositionQuery filters a FIX Request For Positions (35=AN).
// ARQA §2.31 / HowTo: parties only when used — then exactly 2 (Client+Firm or Account+Firm).
// Empty parties = snapshot of all clients.
type PositionQuery struct {
	Firm                 string // PartyRole=13
	ClientCode           string // PartyRole=3 (spot cash/instrument)
	Account              string // PartyRole=38 on request (futures trading account)
	SubscriptionType     string // 263: 0=SNAPSHOT (default), 1=SNAPSHOT+UPDATES, 2=DISABLE
	ClearingBusinessDate string // metadata only — not sent on ARQA AN
}

// FIXPosition is one position block from Position Report (35=AP), ARQA §2.33.
// Spot money = PosType ML; spot instrument = DL; futures limit/pos = FL/FP.
type FIXPosition struct {
	AdapterID     string            `json:"adapter_id"`
	PosMaintRptID string            `json:"pos_maint_rpt_id"`
	PosReqID      string            `json:"pos_req_id"`
	ClearingDate  string            `json:"clearing_date"`
	Kind          string            `json:"kind"` // INSTRUMENT | CASH | FUTURES
	FirmID        string            `json:"firm_id"`
	ClientCode    string            `json:"client_code"`
	AccountCode   string            `json:"account_code"`
	Symbol        string            `json:"symbol"`
	SecurityID    string            `json:"security_id"`
	Board         string            `json:"board"`
	Currency      string            `json:"currency"`
	PosType       string            `json:"pos_type"`        // ML | DL | FL | FP
	UpdateAction  string            `json:"update_action"`   // 5024: 0 New / 1 Change / 2 Delete
	Keys          map[string]string `json:"keys,omitempty"`  // 5026/5027 PosKey*
	Items         map[string]string `json:"items,omitempty"` // 5029/5030 PosItem*
	LongQty       decimal.Decimal   `json:"long_qty"`
	ShortQty      decimal.Decimal   `json:"short_qty"`
	NetQty        decimal.Decimal   `json:"net_qty"`
	PosAmtType    string            `json:"pos_amt_type"`
	PosAmt        decimal.Decimal   `json:"pos_amt"`
	SettlPrice    decimal.Decimal   `json:"settl_price"`
	RawFIX        string            `json:"raw_fix,omitempty"`
}

type ExecutionReport struct {
	AdapterID    string
	ClOrdID      string
	OrigClOrdID  string // FIX tag 41 — original order id on cancel/replace reports
	OrderID      string
	ExecID       string
	ExecType     string
	OrdStatus    string
	LastQty      string
	LastPx       string
	CumQty       string
	LeavesQty    string
	Text         string // FIX tag 58
	OrdRejReason string // FIX tag 103
	CxlRejReason string // FIX tag 102 on 35=9
	CancelReject bool
	RawFIX       string // wire format with | separators (8=FIX.4.2|9=…|35=8|…)
}

type ExecutionHandler func(report ExecutionReport)

// MarketDataQuery filters a FIX Market Data Request (35=V), ARQA §2.19 / HowTo.
// Empty Symbol+Instruments = snapshot of all quotes the session is allowed to see.
type MarketDataQuery struct {
	Symbol           string        // SecurityID (48)
	Board            string        // ExDestination (100) class code
	Instruments      []SecurityRef // optional list for NoRelatedSym (146)
	SubscriptionType string        // 263: 0=SNAPSHOT, 1=SNAPSHOT+UPDATES, 2=DISABLE
}

type MarketDataHandler func(quotes []MarketQuote)

// MarketQuote is one row of QUIK «Текущие торги», built from Market Data Snapshot (35=W)
// / Incremental Refresh (35=X). Best bid/offer (top of book) plus last/OHLC/VWAP.
type MarketQuote struct {
	AdapterID         string            `json:"adapter_id"`
	MDReqID           string            `json:"md_req_id"`
	Symbol            string            `json:"symbol"`
	SecurityID        string            `json:"security_id"`
	Board             string            `json:"board"`
	Currency          string            `json:"currency"`
	Name              string            `json:"name,omitempty"`
	TradeDate         string            `json:"trade_date"`
	TradingStatus     string            `json:"trading_status"`      // 326
	TradingSessionID  string            `json:"trading_session_id"`  // 336
	TradingSessionSub string            `json:"trading_session_sub"` // 625
	Bid               string            `json:"bid"`                 // 269=0
	BidSize           string            `json:"bid_size"`
	Offer             string            `json:"offer"` // 269=1
	OfferSize         string            `json:"offer_size"`
	Last              string            `json:"last"` // 269=2
	LastSize          string            `json:"last_size"`
	LastTime          string            `json:"last_time"`
	Open              string            `json:"open"`  // 269=4
	High              string            `json:"high"`  // 269=7
	Low               string            `json:"low"`   // 269=8
	Close             string            `json:"close"` // 269=5
	WAPR              string            `json:"wapr"`  // 269=9
	Settle            string            `json:"settle"`
	CurrentPrice      string            `json:"current_price"`  // 269=o
	OfficialPrice     string            `json:"official_price"` // 269=O
	Volume            string            `json:"volume"`         // 387 TotalVolumeTraded
	Value             string            `json:"value"`          // 6143 TotalValueTraded
	Entries           map[string]string `json:"entries,omitempty"`
	RawFIX            string            `json:"raw_fix,omitempty"`
	UpdatedAt         time.Time         `json:"updated_at,omitempty"`
	DeltaVolume       string            `json:"-"` // last-trade qty in this FIX message, when 387 absent
	DeltaValue        string            `json:"-"` // last-trade notional, when 6143 absent
}

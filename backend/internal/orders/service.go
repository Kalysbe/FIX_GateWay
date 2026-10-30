package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/observability"
	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

type Service struct {
	store    *Store
	brokers  *brokers.Store
	audit    *audit.Store
	obs      observability.Emitter
	onQueued func()
	accounts TradingAccounts
}

// TradingAccounts is the QUIK Trdaccs source of the accounts a firm may trade.
type TradingAccounts interface {
	TradingAccount(ctx context.Context, firm, account string) (quikexport.Row, error)
}

// SetTradingAccounts must be called before serving requests; without it no
// account can be confirmed and orders are refused as unavailable.
func (s *Service) SetTradingAccounts(src TradingAccounts) { s.accounts = src }

var ErrPersistenceUnavailable = errors.New("order storage unavailable")

func NewService(store *Store, brokers *brokers.Store, auditStore *audit.Store) *Service {
	return &Service{store: store, brokers: brokers, audit: auditStore}
}

func (s *Service) SetObserver(obs observability.Emitter) { s.obs = obs }

// Configure before serving requests. Notifications never replace durable enqueueing.
func (s *Service) SetQueueNotifier(fn func()) { s.onQueued = fn }

// CreateOrderRequest uses the QUIK Orders column names. It has no order id: the
// gateway always generates it, so every request is a new order.
// Side, OrderType and TimeInForce are derived (Operation, Price present, DAY).
type CreateOrderRequest struct {
	Board       string           `json:"ClassCode"` // код класса → FIX tag 100 ExDestination
	Symbol      string           `json:"SecCode"`   // → FIX tag 48 SecurityID
	Operation   string           `json:"Operation"` // Купля / Продажа
	Price       *decimal.Decimal `json:"Price"`     // нет — рыночная заявка
	Quantity    decimal.Decimal  `json:"Qty"`
	AccountID   string           `json:"Account"`    // торговый счёт → FIX tag 1
	ClientCode  string           `json:"ClientCode"` // код клиента → FIX tag 109
	Side        domain.Side      `json:"-"`
	OrderType   domain.OrderType `json:"-"`
	TimeInForce string           `json:"-"`
}

// QUIK Orders.Operation values.
const (
	OperationBuy  = "Купля"
	OperationSell = "Продажа"
)

// deriveFIXFields fills the fields the QUIK-shaped request leaves implicit.
func (r *CreateOrderRequest) deriveFIXFields() {
	switch strings.TrimSpace(r.Operation) {
	case OperationBuy:
		r.Side = domain.SideBuy
	case OperationSell:
		r.Side = domain.SideSell
	}
	if r.OrderType == "" {
		r.OrderType = domain.OrderTypeMarket
		if r.Price != nil {
			r.OrderType = domain.OrderTypeLimit
		}
	}
	if r.TimeInForce == "" {
		r.TimeInForce = "DAY"
	}
}

// ValidateCreateRequest returns a field-specific error for bad order payloads.
func ValidateCreateRequest(req CreateOrderRequest) error {
	if req.AccountID == "" || req.AccountID == "string" {
		return fmt.Errorf("Account обязателен: торговый счёт из GET /api/v1/accounts (например 1-3101-006)")
	}
	if req.ClientCode == "" || req.ClientCode == "string" {
		return fmt.Errorf("ClientCode обязателен: код клиента")
	}
	if req.Symbol == "" || req.Symbol == "string" {
		return fmt.Errorf("SecCode обязателен: код инструмента")
	}
	if req.Board == "" || req.Board == "string" {
		return fmt.Errorf("ClassCode обязателен: код класса (например LIST_GBA)")
	}
	if req.Quantity.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("Qty должно быть > 0")
	}
	if req.Side != domain.SideBuy && req.Side != domain.SideSell {
		return fmt.Errorf("Operation: допустимо %q или %q", OperationBuy, OperationSell)
	}
	if req.OrderType != domain.OrderTypeMarket && req.OrderType != domain.OrderTypeLimit {
		return fmt.Errorf("order type: допустимо MARKET или LIMIT")
	}
	if req.OrderType == domain.OrderTypeLimit && (req.Price == nil || req.Price.LessThanOrEqual(decimal.Zero)) {
		return fmt.Errorf("Price должна быть > 0; для рыночной заявки не передавайте Price")
	}
	switch req.TimeInForce {
	case "", "DAY", "IOC", "FOK", "GTC":
	default:
		return fmt.Errorf("time in force: допустимо DAY, IOC, FOK, GTC")
	}
	return domain.CheckFIXTexts("Account", req.AccountID, "ClientCode", req.ClientCode,
		"SecCode", req.Symbol, "ClassCode", req.Board)
}

// Create stores a new order with a gateway-generated client_order_id
// (поручение пользователя 30.10.2026: без Idempotency-Key, каждый запрос — новая заявка).
func (s *Service) Create(ctx context.Context, broker domain.Broker, ip string, req CreateOrderRequest, quikFirm string) (domain.Order, error) {
	req.deriveFIXFields()
	if err := ValidateCreateRequest(req); err != nil {
		s.emit(ctx, observability.Event{
			Type: observability.TypeError, Subtype: observability.ErrValidation, BrokerID: &broker.ID,
			BrokerCode: broker.Code, BrokerName: broker.Name,
			ErrorCode: "VALIDATION_ERROR", ErrorMessage: err.Error(), FailureStage: "validation",
			Message: err.Error(), AccountID: req.AccountID, Instrument: req.Symbol, Side: string(req.Side),
		})
		return domain.Order{}, err
	}
	if strings.TrimSpace(quikFirm) == "" {
		err := fmt.Errorf("у токена не задана фирма; обратитесь к оператору шлюза")
		s.emit(ctx, observability.Event{
			Type: observability.TypeError, Subtype: observability.ErrValidation, BrokerID: &broker.ID,
			BrokerCode: broker.Code, BrokerName: broker.Name, ErrorCode: "MISSING_QUIK_FIRM",
			ErrorMessage: err.Error(), FailureStage: "validation", Message: err.Error(),
		})
		return domain.Order{}, err
	}
	if req.TimeInForce == "" || req.TimeInForce == "string" {
		req.TimeInForce = "DAY"
	}

	firm := strings.TrimSpace(quikFirm)
	if s.accounts == nil {
		return domain.Order{}, fmt.Errorf("%w: QUIK trading accounts source is not configured", ErrPersistenceUnavailable)
	}
	acc, err := s.accounts.TradingAccount(ctx, firm, req.AccountID)
	if err != nil {
		if !errors.Is(err, quikexport.ErrAccountNotFound) {
			return domain.Order{}, fmt.Errorf("%w: %w", ErrPersistenceUnavailable, err)
		}
		msg := fmt.Errorf("счёт %q не найден у фирмы %q; список счетов — GET /api/v1/accounts", req.AccountID, firm)
		s.emit(ctx, observability.Event{
			Type: observability.TypeError, Subtype: observability.ErrValidation, BrokerID: &broker.ID,
			BrokerCode: broker.Code, BrokerName: broker.Name,
			ErrorCode: "ACCOUNT_NOT_ALLOWED", ErrorMessage: msg.Error(), FailureStage: "validation",
			Message: msg.Error(), AccountID: req.AccountID, Instrument: req.Symbol, Side: string(req.Side),
		})
		return domain.Order{}, msg
	}
	// FIX tag 1 carries the account as QUIK stores it.
	fixAccount := strings.TrimSpace(acc.Text("TrdAccId"))

	instr, err := s.store.ResolveInstrument(ctx, broker.ID, req.Symbol, req.Board)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, ErrInstrumentNotFound) {
			return domain.Order{}, fmt.Errorf("%w: %w", ErrPersistenceUnavailable, err)
		}
		msg := fmt.Errorf("instrument %q (ClassCode %q) не найден: укажите SecCode и ClassCode из GET /api/v1/instruments", req.Symbol, req.Board)
		s.emit(ctx, observability.Event{
			Type: observability.TypeError, Subtype: observability.ErrValidation, BrokerID: &broker.ID,
			BrokerCode: broker.Code, BrokerName: broker.Name, Instrument: req.Symbol,
			ErrorCode: "INSTRUMENT_NOT_FOUND", ErrorMessage: msg.Error(), FailureStage: "validation", Message: msg.Error(),
		})
		return domain.Order{}, msg
	}

	order, _, err := s.store.CreateWithOutbox(ctx, CreateInput{
		BrokerID: broker.ID, FixAccount: fixAccount, ClientCode: req.ClientCode, QuikFirm: strings.TrimSpace(quikFirm),
		InstrumentID: instr.ID, Board: req.Board, Side: req.Side, OrderType: req.OrderType,
		TimeInForce: req.TimeInForce, Quantity: req.Quantity, Price: req.Price,
		RequestID: observability.RequestIDFrom(ctx), TraceID: observability.TraceIDFrom(ctx),
	})
	if err != nil {
		var risk *RiskRejected
		if errors.As(err, &risk) {
			s.emit(ctx, observability.Event{Type: observability.TypeError, Subtype: observability.ErrValidation, BrokerID: &broker.ID,
				ErrorCode: risk.Code, ErrorMessage: risk.Message, FailureStage: "risk", Message: risk.Message})
			return order, err
		}
		s.emit(ctx, observability.Event{
			Type: observability.TypeError, Subtype: observability.ErrDatabase, BrokerID: &broker.ID,
			BrokerCode: broker.Code, BrokerName: broker.Name,
			ErrorCode: "ORDER_PERSIST_FAILED", ErrorMessage: err.Error(), FailureStage: "order_service", Message: err.Error(),
		})
		return order, fmt.Errorf("%w: %w", ErrPersistenceUnavailable, err)
	}
	if s.onQueued != nil {
		s.onQueued()
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType: "broker", ActorID: fmt.Sprintf("%d", broker.ID), BrokerID: &broker.ID,
		Action: "ORDER_CREATE", EntityType: "order", EntityID: order.ID.String(),
		IP: ip, ClientOrderID: order.ClientOrderID, InternalOrderID: order.ID.String(),
		RequestPayload: req, Result: "ok",
	})
	{
		oid := order.ID
		base := observability.EventFromContext(ctx)
		base.BrokerID = &broker.ID
		base.BrokerCode = broker.Code
		base.BrokerName = broker.Name
		base.OrderID = &oid
		base.ClientOrderID = order.ClientOrderID
		base.AccountID = req.AccountID
		base.Instrument = order.Symbol
		base.Side = string(order.Side)
		base.Quantity = order.Quantity.String()
		if order.Price != nil {
			base.Price = order.Price.String()
		}
		for _, sub := range []string{observability.BizOrderCreated, observability.BizOrderValidated, observability.BizOrderQueued} {
			ev := base
			ev.Type = observability.TypeBusiness
			ev.Subtype = sub
			ev.Message = sub
			s.emit(ctx, ev)
		}
	}
	return order, nil
}

// AwaitingVenue reports an order that QUIK has not answered yet.
func AwaitingVenue(st domain.OrderStatus) bool {
	switch st {
	case domain.OrderReceived, domain.OrderValidating, domain.OrderValidated,
		domain.OrderQueued, domain.OrderRouting, domain.OrderSent:
		return true
	}
	return false
}

const venueAnswerPoll = 50 * time.Millisecond

// AwaitingCancel reports a cancel request that QUIK has not answered yet.
func AwaitingCancel(st domain.OrderStatus) bool { return st == domain.OrderCancelPending }

// AwaitVenueAnswer polls the order while pending(status) holds, i.e. until QUIK
// answers the new order or cancel request, or ctx ends. The outbox worker sends
// it; the DB stays authoritative, so the wait never changes the order.
func (s *Service) AwaitVenueAnswer(ctx context.Context, order domain.Order, pending func(domain.OrderStatus) bool) (domain.Order, error) {
	t := time.NewTicker(venueAnswerPoll)
	defer t.Stop()
	for pending(order.Status) {
		select {
		case <-ctx.Done():
			return order, nil
		case <-t.C:
		}
		next, err := s.store.Get(ctx, order.ID)
		if err != nil {
			if ctx.Err() != nil {
				return order, nil
			}
			return order, err
		}
		order = next
	}
	return order, nil
}

func (s *Service) GetForBroker(ctx context.Context, brokerID int64, id uuid.UUID) (domain.Order, error) {
	return s.store.GetForBroker(ctx, brokerID, id)
}

func (s *Service) ListForBroker(ctx context.Context, brokerID int64, limit int) ([]domain.Order, error) {
	return s.store.List(ctx, &brokerID, limit)
}

func (s *Service) Cancel(ctx context.Context, brokerID int64, id uuid.UUID, ip string) (domain.Order, error) {
	order, err := s.store.GetForBroker(ctx, brokerID, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			return order, fmt.Errorf("%w: %w", ErrPersistenceUnavailable, err)
		}
		return order, err
	}
	already, err := s.store.EnqueueCancel(ctx, order.ID)
	if err != nil {
		if !errors.Is(err, ErrInvalidCancel) {
			return order, fmt.Errorf("%w: %w", ErrPersistenceUnavailable, err)
		}
		s.emit(ctx, observability.Event{
			Type: observability.TypeError, Subtype: observability.ErrValidation, BrokerID: &brokerID,
			OrderID: &id, ClientOrderID: order.ClientOrderID, ErrorCode: "CANCEL_REJECTED",
			ErrorMessage: err.Error(), FailureStage: "order_service", Message: err.Error(),
		})
		return order, err
	}
	if already {
		return s.store.Get(ctx, id)
	}
	if s.onQueued != nil {
		s.onQueued()
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType: "broker", ActorID: fmt.Sprintf("%d", brokerID), BrokerID: &brokerID,
		Action: "ORDER_CANCEL", EntityType: "order", EntityID: id.String(),
		IP: ip, ClientOrderID: order.ClientOrderID, InternalOrderID: id.String(), Result: "ok",
	})
	oid := order.ID
	s.emit(ctx, observability.Event{
		Type: observability.TypeBusiness, Subtype: observability.BizOrderCancelRequested,
		BrokerID: &brokerID, OrderID: &oid, ClientOrderID: order.ClientOrderID,
		RequestID: firstNonEmpty(observability.RequestIDFrom(ctx), order.RequestID),
		TraceID:   firstNonEmpty(observability.TraceIDFrom(ctx), order.TraceID),
		AdapterID: deref(order.AdapterID), Message: "cancel requested",
	})
	return s.store.Get(ctx, id)
}

func (s *Service) Store() *Store { return s.store }

func (s *Service) emit(_ context.Context, e observability.Event) {
	if s.obs == nil {
		return
	}
	s.obs.Emit(e)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

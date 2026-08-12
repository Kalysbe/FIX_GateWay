package orders

import (
	"context"
	"fmt"
	"strings"

	"github.com/fix-gateway/backend/internal/audit"
	"github.com/fix-gateway/backend/internal/brokers"
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Service struct {
	store   *Store
	brokers *brokers.Store
	audit   *audit.Store
}

func NewService(store *Store, brokers *brokers.Store, auditStore *audit.Store) *Service {
	return &Service{store: store, brokers: brokers, audit: auditStore}
}

type CreateOrderRequest struct {
	AccountID     string           `json:"account_id"`  // торговый код → FIX tag 1
	ClientCode    string           `json:"client_code"` // произвольный код клиента → FIX tag 109
	ClientOrderID string           `json:"client_order_id"`
	Symbol        string           `json:"symbol"`
	Board         string           `json:"board"` // код класса → FIX tag 100 ExDestination
	Side          domain.Side      `json:"side"`
	OrderType     domain.OrderType `json:"order_type"`
	TimeInForce   string           `json:"time_in_force"`
	Quantity      decimal.Decimal  `json:"quantity"`
	Price         *decimal.Decimal `json:"price"`
}

// ValidateCreateRequest returns a field-specific error for bad order payloads.
func ValidateCreateRequest(req CreateOrderRequest) error {
	if req.AccountID == "" || req.AccountID == "string" {
		return fmt.Errorf("account_id обязателен: торговый код счёта брокера → FIX tag 1 (например 1-3101-006)")
	}
	if req.ClientCode == "" || req.ClientCode == "string" {
		return fmt.Errorf("client_code обязателен: произвольный код клиента → FIX tag 109 ClientID")
	}
	if req.ClientOrderID == "" || req.ClientOrderID == "string" {
		return fmt.Errorf("client_order_id обязателен (уникальный id заявки брокера, → FIX tag 11)")
	}
	if req.Symbol == "" || req.Symbol == "string" {
		return fmt.Errorf("symbol обязателен: инструмент (тикер/ISIN) → FIX tag 48 SecurityID")
	}
	if req.Board == "" || req.Board == "string" {
		return fmt.Errorf("board обязателен: код класса → FIX tag 100 ExDestination (например LISTEDSTOCKK, TQBR)")
	}
	if req.Quantity.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("quantity must be > 0")
	}
	if req.Side != domain.SideBuy && req.Side != domain.SideSell {
		return fmt.Errorf("side: допустимо BUY или SELL")
	}
	if req.OrderType != domain.OrderTypeMarket && req.OrderType != domain.OrderTypeLimit {
		return fmt.Errorf("order_type: допустимо MARKET или LIMIT")
	}
	if req.OrderType == domain.OrderTypeLimit && (req.Price == nil || req.Price.LessThanOrEqual(decimal.Zero)) {
		return fmt.Errorf("price обязателен для LIMIT")
	}
	switch req.TimeInForce {
	case "", "string", "DAY", "IOC", "FOK", "GTC":
	default:
		return fmt.Errorf("time_in_force: допустимо DAY, IOC, FOK, GTC (по умолчанию DAY)")
	}
	return nil
}

func (s *Service) Create(ctx context.Context, broker domain.Broker, ip string, req CreateOrderRequest, quikFirm string) (domain.Order, bool, error) {
	if err := ValidateCreateRequest(req); err != nil {
		return domain.Order{}, false, err
	}
	if strings.TrimSpace(quikFirm) == "" {
		return domain.Order{}, false, fmt.Errorf("на токене не задан quik_firm (код фирмы → FIX tag 5018); задайте при создании/обновлении токена")
	}
	if req.TimeInForce == "" || req.TimeInForce == "string" {
		req.TimeInForce = "DAY"
	}

	instr, err := s.store.ResolveInstrument(ctx, broker.ID, req.Symbol, req.Board)
	if err != nil {
		return domain.Order{}, false, fmt.Errorf("instrument %q не найден: добавьте в справочник instruments (symbol + board/код класса + currency) или укажите существующий символ из GET /api/v1/instruments", req.Symbol)
	}

	order, idempotent, err := s.store.CreateWithOutbox(ctx, CreateInput{
		BrokerID: broker.ID, FixAccount: req.AccountID, ClientCode: req.ClientCode, QuikFirm: strings.TrimSpace(quikFirm),
		ClientOrderID: req.ClientOrderID,
		InstrumentID: instr.ID, Board: req.Board, Side: req.Side, OrderType: req.OrderType,
		TimeInForce: req.TimeInForce, Quantity: req.Quantity, Price: req.Price,
	})
	if err != nil {
		return order, false, err
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType: "broker", ActorID: fmt.Sprintf("%d", broker.ID), BrokerID: &broker.ID,
		Action: "ORDER_CREATE", EntityType: "order", EntityID: order.ID.String(),
		IP: ip, ClientOrderID: req.ClientOrderID, InternalOrderID: order.ID.String(),
		RequestPayload: req, Result: "ok",
	})
	return order, idempotent, nil
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
		return order, err
	}
	if err := s.store.EnqueueCancel(ctx, order.ID); err != nil {
		return order, err
	}
	_ = s.audit.Write(ctx, audit.Entry{
		ActorType: "broker", ActorID: fmt.Sprintf("%d", brokerID), BrokerID: &brokerID,
		Action: "ORDER_CANCEL", EntityType: "order", EntityID: id.String(),
		IP: ip, ClientOrderID: order.ClientOrderID, InternalOrderID: id.String(), Result: "ok",
	})
	return s.store.Get(ctx, id)
}

func (s *Service) Store() *Store { return s.store }

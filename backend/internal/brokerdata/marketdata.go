package brokerdata

import (
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

// MarketQuote is the broker contract shared by REST and gRPC. Keep an explicit
// field list so internal adapter diagnostics never become public by accident.
type MarketQuote struct {
	Symbol            string             `json:"symbol"`
	SecurityID        string             `json:"security_id"`
	Board             string             `json:"board"`
	Currency          string             `json:"currency"`
	Name              string             `json:"name,omitempty"`
	TradeDate         string             `json:"trade_date"`
	TradingStatus     string             `json:"trading_status"`
	TradingSessionID  string             `json:"trading_session_id"`
	TradingSessionSub string             `json:"trading_session_sub"`
	Bid               string             `json:"bid"`
	BidSize           string             `json:"bid_size"`
	Offer             string             `json:"offer"`
	OfferSize         string             `json:"offer_size"`
	Bids              []domain.BookLevel `json:"bids,omitempty"`
	Offers            []domain.BookLevel `json:"offers,omitempty"`
	Last              string             `json:"last"`
	LastSize          string             `json:"last_size"`
	LastTime          string             `json:"last_time"`
	Open              string             `json:"open"`
	High              string             `json:"high"`
	Low               string             `json:"low"`
	Close             string             `json:"close"`
	WAPR              string             `json:"wapr"`
	Settle            string             `json:"settle"`
	CurrentPrice      string             `json:"current_price"`
	OfficialPrice     string             `json:"official_price"`
	Volume            string             `json:"volume"`
	Value             string             `json:"value"`
	UpdatedAt         time.Time          `json:"updated_at,omitempty"`
}

func Quote(q domain.MarketQuote) MarketQuote {
	return MarketQuote{
		Symbol: q.Symbol, SecurityID: q.SecurityID, Board: q.Board,
		Currency: q.Currency, Name: q.Name, TradeDate: q.TradeDate,
		TradingStatus: q.TradingStatus, TradingSessionID: q.TradingSessionID,
		TradingSessionSub: q.TradingSessionSub,
		Bid:               q.Bid, BidSize: q.BidSize, Offer: q.Offer, OfferSize: q.OfferSize,
		Bids: q.Bids, Offers: q.Offers, Last: q.Last, LastSize: q.LastSize,
		LastTime: q.LastTime, Open: q.Open, High: q.High, Low: q.Low,
		Close: q.Close, WAPR: q.WAPR, Settle: q.Settle,
		CurrentPrice: q.CurrentPrice, OfficialPrice: q.OfficialPrice,
		Volume: q.Volume, Value: q.Value, UpdatedAt: q.UpdatedAt,
	}
}

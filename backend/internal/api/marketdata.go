package api

import (
	"net/http"
	"strings"

	"github.com/fix-gateway/backend/internal/brokerdata"
	"github.com/fix-gateway/backend/internal/domain"
)

type marketDataReader interface {
	LiveQuotes() (string, []domain.MarketQuote, bool)
	MarketDataSubscription() domain.MarketDataSubscription
}

type brokerMarketDataResponse struct {
	Quotes []brokerdata.MarketQuote `json:"quotes"`
}

// Availability is used to choose the HTTP status, not exposed as quote data.
// An available feed with no matches is a successful empty selection.
func brokerMarketDataSnapshot(source marketDataReader, board, symbol string) (brokerMarketDataResponse, bool) {
	response := brokerMarketDataResponse{Quotes: []brokerdata.MarketQuote{}}
	if source == nil {
		return response, false
	}
	subscription := source.MarketDataSubscription()
	if subscription.Status != "ACTIVE" && subscription.Status != "PARTIAL" {
		return response, false
	}
	_, quotes, available := source.LiveQuotes()
	if !available {
		return response, false
	}
	for _, quote := range filterQuotes(quotes, board, symbol) {
		response.Quotes = append(response.Quotes, brokerdata.Quote(quote))
	}
	return response, true
}

func (s *Server) listBrokerMarketData(w http.ResponseWriter, r *http.Request) {
	var source marketDataReader
	if s.manager != nil {
		source = s.manager
	}
	response, available := brokerMarketDataSnapshot(source,
		strings.TrimSpace(r.URL.Query().Get("board")),
		strings.TrimSpace(r.URL.Query().Get("symbol")))
	if !available {
		writeErr(w, http.StatusServiceUnavailable, "market data unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

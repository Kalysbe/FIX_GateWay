package api

import (
	"net/http"
	"strings"

	"github.com/fix-gateway/backend/internal/domain"
)

type marketDataReader interface {
	LiveQuotes() (string, []domain.MarketQuote, bool)
	MarketDataSubscription() domain.MarketDataSubscription
}

type brokerMarketDataResponse struct {
	Source       string                        `json:"source"`
	Live         bool                          `json:"live"`
	Quotes       []domain.MarketQuote          `json:"quotes"`
	Subscription domain.MarketDataSubscription `json:"subscription"`
	Replace      bool                          `json:"replace"`
	Note         string                        `json:"note,omitempty"`
}

func brokerMarketDataSnapshot(source marketDataReader, board, symbol string) brokerMarketDataResponse {
	response := brokerMarketDataResponse{
		Source: "live", Quotes: []domain.MarketQuote{}, Replace: true,
		Subscription: domain.MarketDataSubscription{Status: "WAITING"},
	}
	if source != nil {
		response.Subscription = source.MarketDataSubscription()
		if _, quotes, available := source.LiveQuotes(); available {
			response.Live = response.Subscription.Status == "ACTIVE" || response.Subscription.Status == "PARTIAL"
			for _, quote := range filterQuotes(quotes, board, symbol) {
				// Match the gRPC quote DTO without exposing wire messages or
				// modifying the shared adapter snapshot.
				quote.RawFIX = ""
				response.Quotes = append(response.Quotes, quote)
			}
		}
	}
	if !response.Live {
		response.Note = "Шлюз автоматически получает рыночные данные по FIX и восстанавливает поток после разрыва связи."
	}
	return response
}

func (s *Server) listBrokerMarketData(w http.ResponseWriter, r *http.Request) {
	var source marketDataReader
	if s.manager != nil {
		source = s.manager
	}
	writeJSON(w, http.StatusOK, brokerMarketDataSnapshot(source,
		strings.TrimSpace(r.URL.Query().Get("board")),
		strings.TrimSpace(r.URL.Query().Get("symbol"))))
}

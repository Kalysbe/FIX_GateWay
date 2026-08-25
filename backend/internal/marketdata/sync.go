package marketdata

import (
	"context"
	"fmt"

	"github.com/fix-gateway/backend/internal/domain"
)

type FIXFetcher interface {
	FetchMarketData(ctx context.Context, q domain.MarketDataQuery) (adapterID, reqID string, items []domain.MarketQuote, err error)
}

type InstrumentSource interface {
	ListActiveInstruments(ctx context.Context) ([]domain.SecurityRef, error)
}

func (s *Store) SyncFromFIX(ctx context.Context, fetcher FIXFetcher, q domain.MarketDataQuery, inst InstrumentSource) (adapterID, reqID string, count int, sn Snapshot, err error) {
	adapterID, reqID, items, err := fetcher.FetchMarketData(ctx, q)
	needFallback := (err != nil || len(items) == 0) && q.Symbol == "" && len(q.Instruments) == 0 && inst != nil
	if needFallback {
		list, listErr := inst.ListActiveInstruments(ctx)
		if listErr == nil && len(list) > 0 {
			q.Instruments = list
			adapterID, reqID, items, err = fetcher.FetchMarketData(ctx, q)
		}
	}
	if err != nil {
		return "", "", 0, sn, err
	}
	snapID, err := s.BeginSnapshot(ctx, adapterID, reqID)
	if err != nil {
		return adapterID, reqID, 0, sn, err
	}
	if err := s.InsertQuotes(ctx, snapID, items); err != nil {
		_ = s.CompleteSnapshot(ctx, snapID, "ERROR", err.Error(), 0)
		return adapterID, reqID, 0, sn, err
	}
	if err := s.CompleteSnapshot(ctx, snapID, "OK", "", len(items)); err != nil {
		return adapterID, reqID, len(items), sn, err
	}
	sn, _, err = s.ListFromLatest(ctx, q.Board, q.Symbol)
	if err != nil {
		return adapterID, reqID, len(items), Snapshot{
			ID: snapID, AdapterID: adapterID, MDReqID: reqID,
			Status: "OK", ReportCount: len(items),
		}, nil
	}
	return adapterID, reqID, len(items), sn, nil
}

func FormatReceived(count int) string {
	return fmt.Sprintf("%d инструментов", count)
}

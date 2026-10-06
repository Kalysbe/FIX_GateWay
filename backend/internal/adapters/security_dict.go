package adapters

import (
	"context"
	"fmt"
	"strings"

	"github.com/fix-gateway/backend/internal/domain"
)

// SecurityDictionary is implemented by adapters that can fetch the instrument book via FIX.
type SecurityDictionary interface {
	FetchSecurities(ctx context.Context) ([]domain.SecurityRef, error)
}

func (m *Manager) FetchSecurities(ctx context.Context) (adapterID string, items []domain.SecurityRef, err error) {
	var firstErr error
	for _, ad := range m.snapshotAdapters() {
		if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
			continue
		}
		dict, ok := ad.(SecurityDictionary)
		if !ok {
			continue
		}
		items, err = dict.FetchSecurities(ctx)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.log.Warn("security definition fetch failed", "adapter_id", ad.ID(), "error", err)
			continue
		}
		return ad.ID(), items, nil
	}
	if firstErr != nil {
		return "", nil, firstErr
	}
	return "", nil, fmt.Errorf("no online FIX adapter available for Security Definition Request")
}

type securityByBoard interface {
	FetchSecuritiesByBoard(ctx context.Context, board string) ([]domain.SecurityRef, error)
}

func (m *Manager) FetchSecuritiesByBoard(ctx context.Context, board string) (adapterID string, items []domain.SecurityRef, err error) {
	board = strings.TrimSpace(board)
	if board == "" {
		return m.FetchSecurities(ctx)
	}
	var firstErr error
	for _, ad := range m.snapshotAdapters() {
		if ad.Status() != domain.AdapterOnline && ad.Status() != domain.AdapterDegraded {
			continue
		}
		src, ok := ad.(securityByBoard)
		if !ok {
			continue
		}
		items, err = src.FetchSecuritiesByBoard(ctx, board)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.log.Warn("security definition fetch failed", "adapter_id", ad.ID(), "board", board, "error", err)
			continue
		}
		return ad.ID(), items, nil
	}
	if firstErr != nil {
		return "", nil, firstErr
	}
	return "", nil, fmt.Errorf("no online FIX adapter available for Security Definition Request")
}

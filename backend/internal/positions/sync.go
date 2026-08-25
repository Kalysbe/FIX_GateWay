package positions

import (
	"context"
	"fmt"
	"time"

	"github.com/fix-gateway/backend/internal/domain"
)

// FIXFetcher pulls a position snapshot from a live FIX adapter.
type FIXFetcher interface {
	FetchPositions(ctx context.Context, q domain.PositionQuery) (adapterID, reqID string, items []domain.FIXPosition, err error)
}

// SyncFromFIX requests positions via FIX and persists a new OK snapshot.
func (s *Store) SyncFromFIX(ctx context.Context, fetcher FIXFetcher, q domain.PositionQuery) (adapterID, reqID string, count int, sn Snapshot, err error) {
	bizDate := q.ClearingBusinessDate
	if bizDate == "" {
		bizDate = time.Now().Format("20060102")
	}
	adapterID, reqID, items, err := fetcher.FetchPositions(ctx, q)
	if err != nil {
		return "", "", 0, sn, err
	}
	snapID, err := s.BeginSnapshot(ctx, adapterID, reqID, bizDate)
	if err != nil {
		return adapterID, reqID, 0, sn, err
	}
	if err := s.InsertPositions(ctx, snapID, items); err != nil {
		_ = s.CompleteSnapshot(ctx, snapID, "ERROR", err.Error(), 0)
		return adapterID, reqID, 0, sn, err
	}
	if err := s.CompleteSnapshot(ctx, snapID, "OK", "", len(items)); err != nil {
		return adapterID, reqID, len(items), sn, err
	}
	sn, _, err = s.ListFromLatest(ctx, q.Firm, "")
	if err != nil {
		// Snapshot was written; listing failure shouldn't fail the sync.
		return adapterID, reqID, len(items), Snapshot{
			ID: snapID, AdapterID: adapterID, PosReqID: reqID,
			ClearingDate: bizDate, Status: "OK", ReportCount: len(items),
		}, nil
	}
	return adapterID, reqID, len(items), sn, nil
}

// CountByKind returns ML/DL/other counts for UI copy.
func CountByKind(items []domain.FIXPosition) (cash, instruments, futures int) {
	for _, p := range items {
		switch p.Kind {
		case "CASH":
			cash++
		case "FUTURES":
			futures++
		default:
			instruments++
		}
	}
	return cash, instruments, futures
}

func FormatReceived(count, cash, instruments, futures int) string {
	parts := fmt.Sprintf("%d позиций", count)
	var bits []string
	if cash > 0 {
		bits = append(bits, fmt.Sprintf("%d ML", cash))
	}
	if instruments > 0 {
		bits = append(bits, fmt.Sprintf("%d DL", instruments))
	}
	if futures > 0 {
		bits = append(bits, fmt.Sprintf("%d FL/FP", futures))
	}
	if len(bits) == 0 {
		return parts
	}
	return fmt.Sprintf("%s (%s)", parts, joinComma(bits))
}

func joinComma(s []string) string {
	if len(s) == 0 {
		return ""
	}
	out := s[0]
	for i := 1; i < len(s); i++ {
		out += ", " + s[i]
	}
	return out
}

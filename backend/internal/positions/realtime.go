package positions

import (
	"context"

	"github.com/fix-gateway/backend/internal/domain"
)

// RealtimeSnapshot applies the same firm scope as the broker REST endpoint.
// Global snapshot metadata and raw FIX messages are not sent to broker streams.
func (s *Store) RealtimeSnapshot(ctx context.Context, firms []string) (any, error) {
	firms = NormalizeFirmIDs(firms...)
	cash := []domain.FIXPosition{}
	instruments := []domain.FIXPosition{}
	if len(firms) > 0 {
		_, items, err := s.ListMergedByFirms(ctx, firms, "")
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			item.RawFIX = ""
			if item.Kind == "CASH" {
				cash = append(cash, item)
			} else {
				instruments = append(instruments, item)
			}
		}
	}
	firm := ""
	if len(firms) > 0 {
		firm = firms[0]
	}
	return map[string]any{"source": "fix", "firm_id": firm, "cash": cash, "instruments": instruments}, nil
}

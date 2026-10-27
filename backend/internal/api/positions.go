package api

import (
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/positions"
)

func positionsResponseSource(sn positions.Snapshot, items []domain.FIXPosition) string {
	hasManual, hasFix := false, false
	for _, p := range items {
		if p.Source == positions.SourceManual {
			hasManual = true
		} else {
			hasFix = true
		}
	}
	switch {
	case hasManual && (hasFix || sn.ID != 0):
		return "mixed"
	case hasManual:
		return "manual"
	default:
		return "fix"
	}
}

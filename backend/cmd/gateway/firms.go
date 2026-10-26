package main

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/quikexport"
	"github.com/fix-gateway/backend/internal/refdata"
)

type firmSource interface {
	ListFirms(context.Context) ([]quikexport.Row, error)
}
type firmCache interface {
	ReplaceFirms(context.Context, []refdata.Firm) error
}

// syncFirmNames keeps the local firm name cache equal to QUIK "Firms". A failed
// read keeps the previous cache; it is never taken as an empty table.
func syncFirmNames(ctx context.Context, log *slog.Logger, src firmSource, cache firmCache, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		work, cancel := context.WithTimeout(ctx, 30*time.Second)
		rows, err := src.ListFirms(work)
		if err == nil {
			err = cache.ReplaceFirms(work, firmsFromRows(rows))
		}
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Warn("QUIK Firms sync failed; keeping previous firm names", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func firmsFromRows(rows []quikexport.Row) []refdata.Firm {
	out := make([]refdata.Firm, 0, len(rows))
	for _, r := range rows {
		f := refdata.Firm{
			FirmID: strings.TrimSpace(r.Text("FirmId")), Exchange: strings.TrimSpace(r.Text("Exchange")),
			FirmName: strings.TrimSpace(r.Text("FirmName")), Status: strings.TrimSpace(r.Text("Status")),
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(r.Text("StatusFlag")), 10, 32); err == nil {
			v := int32(n)
			f.StatusFlag = &v
		}
		out = append(out, f)
	}
	return out
}

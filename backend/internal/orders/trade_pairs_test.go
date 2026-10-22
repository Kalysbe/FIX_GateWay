package orders

import (
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

func TestTradePairExecID(t *testing.T) {
	for _, tc := range []struct{ name, execID, side, want string }{
		{"buy", "LIST_GBA-2sw-B-1-1-N", "BUY", "LIST_GBA-2sw-S-1-1-N"},
		{"sell", "LIST_GBA-2sw-S-1-1-N", "SELL", "LIST_GBA-2sw-B-1-1-N"},
		{"prefix", "MARKET-B-GBA-2sw-S-1-1-N", "SELL", "MARKET-B-GBA-2sw-B-1-1-N"},
		{"wrong side", "LIST_GBA-2sw-B-1-1-N", "SELL", ""},
		{"unknown side", "LIST_GBA-2sw-S-1-1-N", "", ""},
		{"opaque FIX ID", "exec-B-123", "BUY", ""},
		{"empty", "", "BUY", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := oppositeTradeExecID(domain.Trade{ExecID: tc.execID, Side: tc.side})
			if got != tc.want {
				t.Fatalf("counterpart = %q, want %q", got, tc.want)
			}
		})
	}
}

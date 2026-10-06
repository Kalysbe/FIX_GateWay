package orders

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestEffectiveFillQty(t *testing.T) {
	prior := decimal.NewFromInt(5)
	last := decimal.NewFromInt(3)
	if got := effectiveFillQty(prior, last, decimal.NewFromInt(8)); !got.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("use venue cumqty, got %s", got)
	}
	if got := effectiveFillQty(prior, last, decimal.Zero); !got.Equal(decimal.NewFromInt(8)) {
		t.Fatalf("missing cumqty should be prior+last, got %s", got)
	}
	if got := effectiveFillQty(prior, decimal.Zero, decimal.Zero); !got.Equal(prior) {
		t.Fatalf("ack without qty must keep prior fills, got %s", got)
	}
}

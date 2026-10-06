package api

import (
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/fix-gateway/backend/internal/positions"
	"github.com/shopspring/decimal"
)

func TestMapPositionsResponseSplitsAndSource(t *testing.T) {
	sn := positions.Snapshot{ID: 7, Status: "OK"}
	items := []domain.FIXPosition{
		{Kind: "CASH", FirmID: "2040", ClientCode: "a", NetQty: decimal.NewFromInt(1), Source: positions.SourceFIX},
		{Kind: "INSTRUMENT", FirmID: "2040", ClientCode: "a", Symbol: "SBER", Source: positions.SourceManual},
	}
	resp := mapPositionsResponse(sn, items)
	if resp["source"] != "mixed" {
		t.Fatalf("source %v", resp["source"])
	}
	cash, _ := resp["cash"].([]domain.FIXPosition)
	instr, _ := resp["instruments"].([]domain.FIXPosition)
	if len(cash) != 1 || len(instr) != 1 {
		t.Fatalf("split cash=%d instr=%d", len(cash), len(instr))
	}
	if _, ok := resp["snapshot"]; !ok {
		t.Fatal("snapshot omitted")
	}

	empty := mapPositionsResponse(positions.Snapshot{}, nil)
	if empty["source"] != "fix" {
		t.Fatalf("empty source %v", empty["source"])
	}
	if _, ok := empty["snapshot"]; ok {
		t.Fatal("empty snapshot should be omitted")
	}
}

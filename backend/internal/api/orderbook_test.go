package api

import (
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
)

func TestBookLevelsOrTop(t *testing.T) {
	got := bookLevelsOrTop(nil, "", "")
	if got == nil || len(got) != 0 {
		t.Fatalf("empty: %#v", got)
	}
	got = bookLevelsOrTop(nil, "99.5", "10")
	if len(got) != 1 || got[0].Price != "99.5" || got[0].Size != "10" {
		t.Fatalf("top: %#v", got)
	}
	got = bookLevelsOrTop([]domain.BookLevel{{Price: "99.5", Size: "3"}, {Price: "99.0", Size: "1"}}, "1", "1")
	if len(got) != 2 || got[0].Size != "3" {
		t.Fatalf("keep levels: %#v", got)
	}
}

package arqa

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/field"
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
	"golang.org/x/text/encoding/charmap"
)

// QUIK sends the instrument name (55) of a fill in Windows-1251. The report
// goes to the execution inbox as JSON, so its text must be UTF-8 there.
func TestExecutionReportDecodesWindows1251Text(t *testing.T) {
	name := "ГКВ-12 месячные(GD052270215)"
	legacy, err := charmap.Windows1251.NewEncoder().String(name)
	if err != nil {
		t.Fatal(err)
	}
	legacyText, _ := charmap.Windows1251.NewEncoder().String("Сделка")
	var got []domain.ExecutionReport
	app := newSessionApp("quik-fix-1", slog.New(slog.NewTextHandler(io.Discard, nil)), func(r domain.ExecutionReport) { got = append(got, r) })

	msg := quickfix.NewMessage()
	msg.Header.SetString(tag.MsgType, "8")
	msg.Body.Set(field.NewClOrdID("G1"))
	msg.Body.SetString(tag.ExecID, "LIST_GD-1H0-B-1-1-N")
	msg.Body.SetString(tag.ExecType, "2")
	msg.Body.SetString(tag.Symbol, legacy)
	msg.Body.SetString(tag.Text, legacyText)
	app.handleExecutionReport(msg)

	reject := quickfix.NewMessage()
	reject.Header.SetString(tag.MsgType, "9")
	reject.Body.Set(field.NewClOrdID("G2"))
	reject.Body.SetString(tag.Text, legacyText)
	app.handleOrderCancelReject(reject)

	if len(got) != 2 {
		t.Fatal(got)
	}
	for _, r := range got {
		if !utf8.ValidString(r.RawFIX) || !utf8.ValidString(r.Text) || r.Text != "Сделка" {
			t.Fatalf("report text not UTF-8: %q %q", r.Text, r.RawFIX)
		}
	}
	if !strings.Contains(got[0].RawFIX, "55="+name+"|") {
		t.Fatalf("instrument name lost: %q", got[0].RawFIX)
	}
}

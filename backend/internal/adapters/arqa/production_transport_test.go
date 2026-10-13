package arqa

import (
	"github.com/quickfixgo/quickfix"
	"github.com/quickfixgo/tag"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestLogonCredentialsAndRedaction(t *testing.T) {
	a := newSessionApp("test", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	a.username = "venue-user"
	a.password = "venue-secret"
	msg := quickfix.NewMessage()
	msg.Header.SetString(tag.MsgType, "A")
	a.ToAdmin(msg, quickfix.SessionID{})
	if value, _ := msg.Body.GetString(tag.Password); value != a.password {
		t.Fatal("password missing from Logon")
	}
	if value, _ := msg.Body.GetString(tag.Username); value != a.username {
		t.Fatal("username missing from Logon")
	}
	if text := sanitizeFIX(msg); strings.Contains(text, a.password) || strings.Contains(text, a.username) {
		t.Fatalf("credentials leaked: %s", text)
	}
	msg = quickfix.NewMessage()
	msg.Header.SetString(tag.MsgType, "0")
	a.ToAdmin(msg, quickfix.SessionID{})
	if msg.Body.Has(tag.Password) {
		t.Fatal("credentials added to heartbeat")
	}
}
func TestProductionRequiresVenueTransportEvidence(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("FIX_TRANSPORT_CONFIG_FILE", "")
	if _, err := transportSettings("adapter-1", false); err == nil {
		t.Fatal("production FIX accepted without venue configuration")
	}
	t.Setenv("VENUE_PASSWORD", "")
	if _, err := credential("VENUE_PASSWORD"); err == nil {
		t.Fatal("missing configured credential accepted")
	}
}

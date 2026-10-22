package arqa

import (
	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix"
	"log/slog"
	"testing"
)

func TestLogoutClearsOldMarketDataSession(t *testing.T) {
	app := newSessionApp("test", slog.Default(), nil)
	app.loggedOn.Store(true)
	app.mdBook["TEST|BOARD"] = domain.MarketQuote{Symbol: "TEST"}
	app.trackSub("old-subscription")
	app.OnLogout(quickfix.SessionID{})
	if app.isLoggedOn() || len(app.liveQuotes()) != 0 || len(app.takeSubscriptions()) != 0 {
		t.Fatal("old session state would suppress resubscription")
	}
}

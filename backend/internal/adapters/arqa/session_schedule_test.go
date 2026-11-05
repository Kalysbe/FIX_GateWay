package arqa

import (
	"io"
	"log/slog"
	"testing"

	"github.com/fix-gateway/backend/internal/domain"
	"github.com/quickfixgo/quickfix/config"
)

// The FIX session has no daily schedule: without StartTime/EndTime QuickFIX
// never logs out and resets the sequence numbers at 00:00 UTC.
func TestFIXSessionHasNoDailySchedule(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("FIX_TRANSPORT_CONFIG_FILE", "")
	for _, reset := range []bool{true, false} {
		a := New(domain.Adapter{ID: "quik-fix-2", Host: "127.0.0.1", Port: 15102, BeginString: "FIX.4.2", SenderCompID: "TEST", TargetCompID: "QUIK", ResetOnLogon: reset},
			slog.New(slog.NewTextHandler(io.Discard, nil)), nil, t.TempDir())
		settings, err := a.buildSettings()
		if err != nil {
			t.Fatal(err)
		}
		want := "N"
		if reset {
			want = "Y"
		}
		for _, s := range settings.SessionSettings() {
			for _, key := range []string{config.StartTime, config.EndTime} {
				if s.HasSetting(key) {
					t.Fatalf("reset=%v: %s set", reset, key)
				}
			}
			if got, _ := s.Setting(config.ResetOnLogon); got != want {
				t.Fatalf("ResetOnLogon=%s, want %s", got, want)
			}
		}
	}
}

package brokertime

import (
	"testing"
	"time"
)

func TestFormat(t *testing.T) {
	cases := []struct{ in, want string }{
		// QUIK dates are midnight.
		{`{"TradeDate":"2026-11-17T00:00:00","SettleDate":"2026-11-18T00:00:00"}`, `{"TradeDate":"17.11.2026","SettleDate":"18.11.2026"}`},
		// Moments; midnight in a date-time column stays a moment.
		{`{"OrderDateTime":"2026-11-17T15:54:45","ExpiryTime":"2026-11-17T00:00:00"}`, `{"OrderDateTime":"17.11.2026 15:54:45","ExpiryTime":"17.11.2026 00:00:00"}`},
		// Times of day carry a dummy date in QUIK Params.
		{`{"time":"1900-01-01T16:40:15","starttime":"1900-01-01T10:00:00"}`, `{"time":"16:40:15","starttime":"10:00:00"}`},
		// Gateway UTC timestamps are shown in KSE time.
		{`{"created_at":"2026-10-20T09:15:00Z","last_used_at":"2026-11-03T20:02:11.123456Z"}`, `{"created_at":"20.10.2026 15:15:00","last_used_at":"04.11.2026 02:02:11.123456"}`},
		// Values with the KSE zone (market trades) lose it and empty milliseconds;
		// other strings are left alone; nulls stay null.
		{`{"TradeDateTime":"2026-11-05T14:32:57.000+06:00","CanceledDateTime":"2026-11-05T14:32:57.250+06:00","WithdrawDateTime":null,"Expiry":"До отмены"}`, `{"TradeDateTime":"05.11.2026 14:32:57","CanceledDateTime":"05.11.2026 14:32:57.250","WithdrawDateTime":null,"Expiry":"До отмены"}`},
		// Inside the broker envelope, arrays and numbers keep their bytes.
		{`{"success":true,"data":[{"Price":94.99000000,"TradeDate":"2026-11-17T00:00:00"}],"has_more":false}`, `{"success":true,"data":[{"Price":94.99000000,"TradeDate":"17.11.2026"}],"has_more":false}`},
		// A timestamp-looking text inside another string is not a member.
		{`{"message":"see \"TradeDate\":\"2026-11-17T00:00:00\""}`, `{"message":"see \"TradeDate\":\"2026-11-17T00:00:00\""}`},
	}
	for _, c := range cases {
		if got := string(Format([]byte(c.in))); got != c.want {
			t.Errorf("Format(%s)\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

func TestParseInput(t *testing.T) {
	got, date, ok := ParseInput("17.11.2026")
	if !ok || !date || !got.Equal(time.Date(2026, 11, 17, 0, 0, 0, 0, Zone)) {
		t.Fatal(got, date, ok)
	}
	got, date, ok = ParseInput("17.11.2026 15:54:45")
	if !ok || date || !got.Equal(time.Date(2026, 11, 17, 9, 54, 45, 0, time.UTC)) {
		t.Fatal(got, date, ok)
	}
	if _, _, ok = ParseInput("2026-11-17"); ok {
		t.Fatal("ISO is not this layout")
	}
}

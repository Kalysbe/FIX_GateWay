package quikapi

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "http://192.168.0.89:5000", want: "http://192.168.0.89:5000"},
		{in: "http://192.168.0.89/", want: "http://192.168.0.89:5000"},
		{in: "http://192.168.0.89", want: "http://192.168.0.89:5000"},
		{in: "192.168.0.90:5000", want: "http://192.168.0.90:5000"},
		{in: "https://quik.local:5000/api", want: "https://quik.local:5000"},
		{in: "", wantErr: true},
		{in: "ftp://192.168.0.89:5000", wantErr: true},
		{in: "http://user:pass@192.168.0.89:5000", wantErr: true},
		{in: "http://169.254.169.254/", wantErr: true},
		{in: "http://0.0.0.0:5000", wantErr: true},
	}
	for _, tc := range cases {
		got, err := NormalizeBaseURL(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("%q: expected error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
		}
	}
}

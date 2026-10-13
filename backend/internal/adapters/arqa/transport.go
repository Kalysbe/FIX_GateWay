package arqa

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Transport configuration is operator-owned and mounted read-only. UAT evidence
// is a release gate, not something the application can generate for the venue.
type transportConfig struct {
	Dictionary  string `json:"dictionary"`
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"private_key"`
	CA          string `json:"ca"`
	ServerName  string `json:"server_name"`
	UATEvidence string `json:"uat_evidence"`
	StartTime   string `json:"start_time"`
	EndTime     string `json:"end_time"`
	TimeZone    string `json:"time_zone"`
}

func productionMode() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	return v == "production" || v == "prod"
}

func transportSettings(id string, reset bool) (string, error) {
	path := os.Getenv("FIX_TRANSPORT_CONFIG_FILE")
	if path == "" {
		if productionMode() {
			return "", errors.New("FIX_TRANSPORT_CONFIG_FILE and venue UAT evidence required")
		}
		return "", nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", errors.New("FIX transport configuration unavailable")
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	var configs map[string]transportConfig
	if err := decoder.Decode(&configs); err != nil {
		return "", errors.New("invalid FIX transport configuration")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return "", errors.New("trailing FIX transport configuration")
	}
	c, ok := configs[id]
	if !ok {
		return "", fmt.Errorf("FIX transport configuration missing adapter %s", id)
	}
	if c.Dictionary == "" || c.Certificate == "" || c.PrivateKey == "" || c.CA == "" || c.ServerName == "" {
		return "", errors.New("FIX dictionary and verified TLS certificate/key/CA/server_name required")
	}
	if productionMode() && (reset || c.UATEvidence == "") {
		return "", errors.New("production FIX requires persistent sequence numbers, ResetOnLogon=false and UAT evidence")
	}
	for _, p := range []string{c.Dictionary, c.Certificate, c.PrivateKey, c.CA, c.UATEvidence} {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			return "", errors.New("FIX transport file paths must be absolute")
		}
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return "", errors.New("FIX transport file unavailable or empty")
		}
	}
	if c.StartTime == "" {
		c.StartTime = "00:00:00"
	}
	if c.EndTime == "" {
		c.EndTime = "00:00:00"
	}
	if c.TimeZone == "" {
		c.TimeZone = "UTC"
	}
	for _, v := range []string{c.Dictionary, c.Certificate, c.PrivateKey, c.CA, c.ServerName, c.StartTime, c.EndTime, c.TimeZone} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return "", errors.New("invalid FIX setting")
		}
	}
	return fmt.Sprintf("\nUseDataDictionary=Y\nDataDictionary=%s\nValidateFieldsOutOfOrder=Y\nValidateUserDefinedFields=Y\nSocketCertificateFile=%s\nSocketPrivateKeyFile=%s\nSocketCAFile=%s\nSocketServerName=%s\nSocketInsecureSkipVerify=N\nSocketTimeout=10s\nStartTime=%s\nEndTime=%s\nTimeZone=%s\n", c.Dictionary, c.Certificate, c.PrivateKey, c.CA, c.ServerName, c.StartTime, c.EndTime, c.TimeZone), nil
}

func credential(name string) (string, error) {
	if name == "" {
		return "", nil
	}
	value := os.Getenv(name)
	if path := os.Getenv(name + "_FILE"); path != "" {
		if value != "" {
			return "", errors.New("FIX credential value and file both configured")
		}
		f, err := os.Open(path)
		if err != nil {
			return "", errors.New("FIX credential file unavailable")
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 8193))
		if err != nil || len(b) > 8192 {
			return "", errors.New("FIX credential file invalid")
		}
		value = strings.TrimRight(string(b), "\r\n")
	}
	if value == "" || strings.ContainsAny(value, "\r\n\x00\x01") {
		return "", errors.New("configured FIX credential unavailable or invalid")
	}
	return value, nil
}

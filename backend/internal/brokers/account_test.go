package brokers

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestNormalizeAccountInput(t *testing.T) {
	t.Parallel()
	code, name, firm, trading, err := normalizeAccountInput("", "", "  F  ", " 1-3101-2040 ")
	if err != nil {
		t.Fatal(err)
	}
	if trading != "1-3101-2040" || code != "1-3101-2040" || name != "1-3101-2040" || firm != "F" {
		t.Fatalf("got code=%q name=%q firm=%q trading=%q", code, name, firm, trading)
	}

	code, name, _, trading, err = normalizeAccountInput("ACC1", "Main", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if code != "ACC1" || trading != "ACC1" || name != "Main" {
		t.Fatalf("got code=%q name=%q trading=%q", code, name, trading)
	}

	_, _, _, _, err = normalizeAccountInput("  ", "", "", "")
	if !errors.Is(err, ErrAccountFields) {
		t.Fatalf("expected ErrAccountFields, got %v", err)
	}
}

func TestCreateAccountJSONTags(t *testing.T) {
	t.Parallel()
	body := `{"code":"c1","name":"n1","quik_firm":"FIRM","trading_account":"1-3101-2040"}`
	var req struct {
		Code           string `json:"code"`
		Name           string `json:"name"`
		QuikFirm       string `json:"quik_firm"`
		TradingAccount string `json:"trading_account"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatal(err)
	}
	if req.TradingAccount != "1-3101-2040" || req.QuikFirm != "FIRM" {
		t.Fatalf("json tags did not bind: %+v", req)
	}
}

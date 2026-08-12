package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}

func writeErrDetails(w http.ResponseWriter, status int, msg string, details any) {
	writeJSON(w, status, map[string]any{"error": msg, "details": details})
}

// decodeJSONBody decodes JSON and returns a human-readable field error when types mismatch.
func decodeJSONBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return fmt.Errorf("пустое тело запроса")
	}
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		return fmt.Errorf("синтаксис JSON некорректен (offset %d)", syn.Offset)
	case errors.As(err, &typ):
		field := typ.Field
		if field == "" {
			field = typ.Struct
		}
		return fmt.Errorf("%s: ожидается тип %s, получено %s. %s",
			field, typ.Type.String(), typ.Value, fieldHint(field))
	case strings.HasPrefix(err.Error(), "json: unknown field "):
		field := strings.Trim(strings.TrimPrefix(err.Error(), "json: unknown field "), `"`)
		return fmt.Errorf("неизвестное поле %q. %s", field, fieldHint(field))
	default:
		return fmt.Errorf("некорректный JSON: %v", err)
	}
}

func fieldHint(field string) string {
	f := strings.ToLower(field)
	switch {
	case strings.Contains(f, "account_id"):
		return `account_id — произвольный торговый код счёта брокера → FIX tag 1 (например "1-3101-006")`
	case strings.Contains(f, "quantity"), strings.Contains(f, "price"):
		return `quantity/price можно передавать числом или строкой, например 1 или "1"`
	case strings.Contains(f, "symbol"):
		return `инструмент пишите в symbol (тикер/ISIN)`
	case strings.Contains(f, "board"), strings.Contains(f, "class"):
		return `код класса пишите в board → FIX tag 100 ExDestination (например LISTEDSTOCKK)`
	case strings.Contains(f, "currency"), strings.Contains(f, "валют"):
		return `валюта не в теле заявки — задаётся в instruments.currency и уходит в FIX tag 15`
	default:
		return `см. схему CreateOrderRequest: symbol→инструмент; board→код класса (FIX 100); фирма→quik_firm (FIX 5018)`
	}
}

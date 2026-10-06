package api

import (
	"context"
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
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if w.Header().Get("Retry-After") == "" {
			w.Header().Set("Retry-After", "1")
		}
	}
	writeJSON(w, status, map[string]any{"error": msg})
}

func writeErrDetails(w http.ResponseWriter, status int, msg string, details any) {
	writeJSON(w, status, map[string]any{"error": msg, "details": details})
}

func (s *Server) writeInternal(w http.ResponseWriter, err error) {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		writeErr(w, http.StatusServiceUnavailable, "request interrupted; verify result and retry with the same client_order_id")
		return
	}
	if s != nil && s.log != nil && err != nil {
		s.log.Error("request failed", "error", err)
	}
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func (s *Server) writeUpstream(w http.ResponseWriter, err error) {
	if s != nil && s.log != nil && err != nil {
		s.log.Error("upstream failed", "error", err)
	}
	writeErr(w, http.StatusBadGateway, "upstream unavailable")
}

const maxJSONBody = 1 << 20

func (s *Server) limitBody(next http.Handler) http.Handler {
	n := s.maxBody
	if n <= 0 {
		n = maxJSONBody
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, n)
		}
		next.ServeHTTP(w, r)
	})
}

// decodeJSONBody decodes JSON and returns a human-readable field error when types mismatch.
func decodeJSONBody(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	err := dec.Decode(dst)
	if err == nil {
		return nil
	}
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return fmt.Errorf("тело запроса слишком большое")
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
	case strings.Contains(f, "amount"), strings.Contains(f, "currency"), strings.Contains(f, "валют"):
		return `для денег: currency (KGS/USD) и amount (CUR_BAL); опционально tag, open_bal, limit, locked`
	case strings.Contains(f, "quantity"), strings.Contains(f, "price"), strings.Contains(f, "long_qty"), strings.Contains(f, "short_qty"):
		return `quantity/price можно передавать числом или строкой, например 1 или "1"`
	case strings.Contains(f, "symbol"):
		return `инструмент пишите в symbol (тикер/ISIN)`
	case strings.Contains(f, "board"), strings.Contains(f, "class"):
		return `код класса пишите в board → FIX tag 100 ExDestination (например LISTEDSTOCKK)`
	case strings.Contains(f, "client_code"):
		return `client_code — код клиента (PartyRole 3)`
	case strings.Contains(f, "tag"):
		return `tag — ключ денежной позиции (например EQTV); если пусто, берётся currency`
	case strings.Contains(f, "firm"):
		return `firm_id — код фирмы / код брокера (например 2040)`
	case strings.Contains(f, "base_url"):
		return `base_url — адрес QUIK Module API, например http://192.168.0.89:5000`
	default:
		return `см. схему CreateOrderRequest: symbol→инструмент; board→код класса (FIX 100); фирма→quik_firm (FIX 5018)`
	}
}

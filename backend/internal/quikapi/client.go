package quikapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	maxQuotesBody = 8 << 20
	maxQuotes     = 10000
)

type Quote struct {
	Operation string `json:"operation"`
	OrderNum  string `json:"order_num"`
	ClassCode string `json:"class_code"`
	SecCode   string `json:"sec_code"`
	Price     string `json:"price"`
	Value     string `json:"value"`
	Qty       string `json:"qty"`
}

type QuotesResult struct {
	Source  string  `json:"source"`
	BaseURL string  `json:"base_url"`
	Count   int     `json:"count"`
	Quotes  []Quote `json:"quotes"`
}

type Client struct {
	store *Store
	http  *http.Client
}

func NewClient(store *Store, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return fmt.Errorf("too many redirects")
				}
				if len(via) > 0 && via[0].URL.Host != req.URL.Host {
					return fmt.Errorf("redirect to different host")
				}
				return nil
			},
		}
	}
	return &Client{store: store, http: httpClient}
}

func (c *Client) DefaultBaseURL() string {
	if c == nil || c.store == nil {
		return DefaultBaseURL
	}
	return c.store.DefaultBaseURL()
}

func (c *Client) Settings(ctx context.Context) (baseURL, defaultURL string, err error) {
	if c == nil || c.store == nil {
		return "", DefaultBaseURL, ErrNotConfigured
	}
	base, err := c.store.GetBaseURL(ctx)
	if err != nil {
		return "", c.store.DefaultBaseURL(), err
	}
	return base, c.store.DefaultBaseURL(), nil
}

func (c *Client) SetBaseURL(ctx context.Context, raw, updatedBy string) (string, error) {
	if c == nil || c.store == nil {
		return "", ErrNotConfigured
	}
	return c.store.SetBaseURL(ctx, raw, updatedBy)
}

func (c *Client) Quotes(ctx context.Context, q url.Values) (QuotesResult, error) {
	var out QuotesResult
	if c == nil || c.store == nil {
		return out, ErrNotConfigured
	}
	base, err := c.store.GetBaseURL(ctx)
	if err != nil {
		return out, err
	}
	if base == "" {
		return out, ErrNotConfigured
	}
	u, err := url.Parse(base)
	if err != nil {
		return out, ErrInvalidURL
	}
	u.Path = "/api/quotes"
	u.RawQuery = filterQuoteQuery(q).Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(io.LimitReader(res.Body, maxQuotesBody+1))
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if len(body) > maxQuotesBody {
		return out, fmt.Errorf("%w: response too large", ErrUpstream)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return out, fmt.Errorf("%w: %s", ErrUpstream, upstreamMessage(res.StatusCode, body))
	}

	quotes, err := parseQuotes(body)
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if len(quotes) > maxQuotes {
		quotes = quotes[:maxQuotes]
	}
	out.Source = "quik_api"
	out.BaseURL = base
	out.Quotes = quotes
	out.Count = len(quotes)
	return out, nil
}

var quoteQueryKeys = map[string]string{
	"operation":  "Operation",
	"ordernum":   "OrderNum",
	"order_num":  "OrderNum",
	"classcode":  "ClassCode",
	"class_code": "ClassCode",
	"seccode":    "SecCode",
	"sec_code":   "SecCode",
	"price":      "Price",
	"value":      "Value",
	"qty":        "Qty",
}

func filterQuoteQuery(in url.Values) url.Values {
	out := url.Values{}
	if in == nil {
		return out
	}
	for key, vals := range in {
		mapped, ok := quoteQueryKeys[strings.ToLower(strings.TrimSpace(key))]
		if !ok {
			continue
		}
		for _, v := range vals {
			v = strings.TrimSpace(v)
			if v == "" {
				continue
			}
			out.Add(mapped, v)
		}
	}
	return out
}

func parseQuotes(body []byte) ([]Quote, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	list, ok := raw.([]any)
	if !ok {
		if msg := mapMessage(raw); msg != "" {
			return nil, fmt.Errorf("%s", msg)
		}
		return nil, fmt.Errorf("expected JSON array")
	}
	out := make([]Quote, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, Quote{
			Operation: pick(m, "operation", "Operation"),
			OrderNum:  pick(m, "order_num", "OrderNum"),
			ClassCode: pick(m, "class_code", "ClassCode"),
			SecCode:   pick(m, "sec_code", "SecCode"),
			Price:     pick(m, "price", "Price"),
			Value:     pick(m, "value", "Value"),
			Qty:       pick(m, "qty", "Qty"),
		})
	}
	return out, nil
}

func pick(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := stringify(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return ""
		}
		return strings.Trim(string(b), `"`)
	}
}

func upstreamMessage(status int, body []byte) string {
	if msg := mapMessageBytes(body); msg != "" {
		return fmt.Sprintf("HTTP %d: %s", status, msg)
	}
	return fmt.Sprintf("HTTP %d", status)
}

func mapMessageBytes(body []byte) string {
	var raw any
	if err := json.Unmarshal(body, &raw); err != nil {
		return ""
	}
	return mapMessage(raw)
}

func mapMessage(raw any) string {
	m, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	for _, k := range []string{"message", "error", "Error"} {
		if s := stringify(m[k]); s != "" && s != "null" {
			return s
		}
	}
	return ""
}

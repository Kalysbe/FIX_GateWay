// Simple smoke load against a running gateway (not a full load test suite).
// Usage: go run ./scripts/load_smoke.go -url http://localhost:8080 -token <broker_token> -account 1-3101-006 -client-code 006 -n 100
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "gateway base url")
	token := flag.String("token", "", "broker API token")
	account := flag.String("account", "1-3101-006", "FIX account code (tag 1)")
	clientCode := flag.String("client-code", "", "required FIX client code (tag 109)")
	n := flag.Int("n", 100, "orders")
	workers := flag.Int("w", 10, "workers")
	flag.Parse()
	base, err := normalizeGatewayURL(*url)
	if err != nil || strings.TrimSpace(*token) == "" || strings.TrimSpace(*clientCode) == "" || strings.TrimSpace(*account) == "" || *n <= 0 || *workers <= 0 {
		fmt.Fprintln(os.Stderr, "valid http(s) -url, -token, -account, -client-code and positive -n/-w required")
		os.Exit(2)
	}
	runPrefix := "LOAD-" + uuid.NewString()

	var ok, fail atomic.Int64
	jobs := make(chan int, *workers)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 10 * time.Second}
			for i := range jobs {
				body, _ := json.Marshal(map[string]any{
					"account_id": *account, "client_code": *clientCode, "client_order_id": fmt.Sprintf("%s-%d", runPrefix, i),
					"symbol": "SBER", "board": "TQBR", "side": "BUY", "order_type": "LIMIT", "quantity": "1", "price": "100",
				})
				if err := createSmokeOrder(client, base, *token, body); err != nil {
					fail.Add(1)
				} else {
					ok.Add(1)
				}
			}
		}()
	}
	for i := 0; i < *n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	elapsed := time.Since(start).Seconds()
	fmt.Printf("ok=%d fail=%d elapsed=%.2fs rate=%.1f orders/sec\n", ok.Load(), fail.Load(), elapsed, float64(ok.Load())/elapsed)
	if fail.Load() != 0 {
		os.Exit(1)
	}
}

func normalizeGatewayURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("invalid gateway URL")
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func createSmokeOrder(client *http.Client, base, token string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, base+"/api/v1/orders", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("create order returned HTTP %d", resp.StatusCode)
	}
	return nil
}

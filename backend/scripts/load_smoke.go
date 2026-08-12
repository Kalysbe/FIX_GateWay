// Simple smoke load against a running gateway (not a full load test suite).
// Usage: go run ./scripts/load_smoke.go -url http://localhost:8080 -token <broker_token> -account 1-3101-006 -n 100
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	url := flag.String("url", "http://localhost:8080", "gateway base url")
	token := flag.String("token", "", "broker API token")
	account := flag.String("account", "1-3101-006", "FIX account code (tag 1)")
	n := flag.Int("n", 100, "orders")
	workers := flag.Int("w", 10, "workers")
	flag.Parse()
	if *token == "" {
		panic("token required")
	}

	var ok, fail atomic.Int64
	jobs := make(chan int, *n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := &http.Client{Timeout: 10 * time.Second}
			for i := range jobs {
				body, _ := json.Marshal(map[string]any{
					"account_id": *account, "client_order_id": fmt.Sprintf("LOAD-%d", i),
					"symbol": "SBER", "board": "TQBR", "side": "BUY", "order_type": "LIMIT", "quantity": "1", "price": "100",
				})
				req, _ := http.NewRequest(http.MethodPost, *url+"/api/v1/orders", bytes.NewReader(body))
				req.Header.Set("Authorization", "Bearer "+*token)
				req.Header.Set("Content-Type", "application/json")
				resp, err := client.Do(req)
				if err != nil || resp.StatusCode >= 300 {
					fail.Add(1)
				} else {
					ok.Add(1)
				}
				if resp != nil {
					resp.Body.Close()
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
}

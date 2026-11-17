package main

import (
	"flag"
	"log"
	"strings"
	"time"

	"github.com/fix-gateway/backend/internal/platform/devcert"
)

func main() {
	dir := flag.String("out", "certs", "development certificate directory")
	hosts := flag.String("dns", "localhost,gateway", "server DNS names")
	flag.Parse()
	result, err := devcert.Ensure(*dir, strings.Split(*hosts, ","))
	if err != nil {
		log.Fatal(err)
	}
	switch result {
	case devcert.Created:
		log.Print("development TLS/mTLS certificates created")
	case devcert.Replaced:
		log.Print("expired development PKI replaced with a new CA: give brokers that pin the CA the new ca.crt, then restart the gateway")
	default:
		log.Print("development TLS/mTLS certificates ready")
	}
	if notAfter, err := devcert.ServerExpiry(*dir); err == nil {
		log.Printf("server certificate valid until %s", notAfter.UTC().Format(time.RFC3339))
		if time.Until(notAfter) <= 30*24*time.Hour {
			log.Print("server certificate expires within 30 days; plan a new PKI and the ca.crt hand-over to brokers")
		}
	}
}

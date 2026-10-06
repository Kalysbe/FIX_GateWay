package main

import (
	"flag"
	"github.com/fix-gateway/backend/internal/platform/devcert"
	"log"
	"strings"
)

func main() {
	dir := flag.String("out", "certs", "development certificate directory")
	hosts := flag.String("dns", "localhost,gateway", "server DNS names")
	flag.Parse()
	if err := devcert.Generate(*dir, strings.Split(*hosts, ",")); err != nil {
		log.Fatal(err)
	}
	log.Print("development TLS/mTLS certificates ready")
}

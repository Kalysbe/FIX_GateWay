package grpcstream

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"google.golang.org/grpc/credentials"
	"os"
	"time"
)

func roots(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("invalid CA certificate")
	}
	return pool, nil
}
// CertificateExpiry is the NotAfter of the first certificate in a PEM file.
func CertificateExpiry(path string) (time.Time, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return time.Time{}, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return time.Time{}, fmt.Errorf("no certificate in %s", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

func ServerCredentials(cert, key, ca string, mtls bool) (credentials.TransportCredentials, error) {
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}}
	if mtls {
		pool, err := roots(ca)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return credentials.NewTLS(cfg), nil
}
func ClientCredentials(cert, key, ca, serverName string) (credentials.TransportCredentials, error) {
	pool, err := roots(ca)
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, ServerName: serverName}
	if cert != "" || key != "" {
		pair, err := tls.LoadX509KeyPair(cert, key)
		if err != nil {
			return nil, err
		}
		cfg.Certificates = []tls.Certificate{pair}
	}
	return credentials.NewTLS(cfg), nil
}

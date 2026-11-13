// Package devcert creates a private, local development PKI. Production must use
// operator-provisioned certificates. The CA private key is never written to disk.
package devcert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Lifetimes. Without the CA key a leaf cannot be renewed alone: renewal replaces
// the whole PKI and brokers that pin the CA need the new ca.crt. Long lifetimes
// keep that rare; the gateway warns 30 days before the server certificate ends.
const (
	CALifetime   = 3 * 365 * 24 * time.Hour
	LeafLifetime = 2 * 365 * 24 * time.Hour
)

var names = []string{"ca.crt", "server.crt", "server.key", "bridge.crt", "bridge.key"}

// Result says what Generate did with the directory.
type Result int

const (
	// Kept: a valid PKI was already there.
	Kept Result = iota
	// Created: the directory had no PKI.
	Created
	// Replaced: the PKI there had expired, so a new one with a new CA was written.
	Replaced
)

// Generate is Ensure without the report.
func Generate(dir string, hosts []string) error {
	_, err := Ensure(dir, hosts)
	return err
}

// Ensure keeps a valid PKI, creates one in an empty directory and replaces one
// that has expired: an expired certificate already breaks every TLS client, so a
// new CA cannot make things worse, while a gateway that cannot start does. A
// partial or otherwise invalid PKI is never touched.
func Ensure(dir string, hosts []string) (Result, error) {
	exists := 0
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			exists++
		} else if !os.IsNotExist(err) {
			return Kept, err
		}
	}
	if exists > 0 {
		if exists != len(names) {
			return Kept, fmt.Errorf("partial PKI exists; refusing to overwrite")
		}
		err := verify(dir, time.Now())
		if err == nil {
			return Kept, nil
		}
		var invalid x509.CertificateInvalidError
		if !errors.As(err, &invalid) || invalid.Reason != x509.Expired {
			return Kept, err
		}
		for _, name := range names {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return Kept, err
			}
		}
		if err := create(dir, hosts, time.Now()); err != nil {
			return Kept, err
		}
		return Replaced, nil
	}
	if err := create(dir, hosts, time.Now()); err != nil {
		return Kept, err
	}
	return Created, nil
}

// ServerExpiry is when the server certificate in dir expires.
func ServerExpiry(dir string) (time.Time, error) {
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"))
	if err != nil {
		return time.Time{}, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return time.Time{}, err
	}
	return leaf.NotAfter, nil
}

func verify(dir string, now time.Time) error {
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("invalid existing CA")
	}
	for _, name := range []string{"server", "bridge"} {
		pair, err := tls.LoadX509KeyPair(filepath.Join(dir, name+".crt"), filepath.Join(dir, name+".key"))
		if err != nil {
			return err
		}
		leaf, err := x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return err
		}
		usage := x509.ExtKeyUsageServerAuth
		if name == "bridge" {
			usage = x509.ExtKeyUsageClientAuth
		}
		if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{usage}, CurrentTime: now}); err != nil {
			return err
		}
	}
	return nil
}

func create(dir string, hosts []string, now time.Time) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial := func() *big.Int { n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)); return n }
	ca := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "FIX Gateway development CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(CALifetime), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		return err
	}
	write := func(name string, block *pem.Block) error {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		err = pem.Encode(f, block)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	if err := write("ca.crt", &pem.Block{Type: "CERTIFICATE", Bytes: caDER}); err != nil {
		return err
	}
	for _, name := range []string{"server", "bridge"} {
		pair, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return err
		}
		usage := x509.ExtKeyUsageServerAuth
		if name == "bridge" {
			usage = x509.ExtKeyUsageClientAuth
		}
		cert := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "fix-gateway-" + name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(LeafLifetime), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		if name == "server" {
			cert.DNSNames = hosts
			cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
		}
		der, err := x509.CreateCertificate(rand.Reader, cert, ca, &pair.PublicKey, key)
		if err != nil {
			return err
		}
		priv, err := x509.MarshalPKCS8PrivateKey(pair)
		if err != nil {
			return err
		}
		if err := write(name+".crt", &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			return err
		}
		if err := write(name+".key", &pem.Block{Type: "PRIVATE KEY", Bytes: priv}); err != nil {
			return err
		}
	}
	return nil
}

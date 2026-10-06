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
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

func Generate(dir string, hosts []string) error {
	names := []string{"ca.crt", "server.crt", "server.key", "bridge.crt", "bridge.key"}
	exists := 0
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			exists++
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if exists > 0 {
		if exists != len(names) {
			return fmt.Errorf("partial PKI exists; refusing to overwrite")
		}
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
			if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{usage}}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial := func() *big.Int { n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)); return n }
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "FIX Gateway development CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(90 * 24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
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
		cert := &x509.Certificate{SerialNumber: serial(), Subject: pkix.Name{CommonName: "fix-gateway-" + name}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * 24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}
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

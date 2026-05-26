// Command gencert produces a self-signed ECDSA certificate suitable for
// local HTTP/3 development. The cert is written to certs/cert.pem and the
// key to certs/key.pem unless overridden.
//
//	go run ./cmd/gencert -hosts localhost,127.0.0.1
//
// HTTP/3 requires TLS and browsers refuse to talk QUIC to an untrusted cert,
// so the printed CA file should be imported into the OS trust store, or
// Chrome started with --ignore-certificate-errors-spki-list (see README).
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	hosts := flag.String("hosts", "localhost,127.0.0.1,::1", "comma-separated hostnames and IPs in the cert SAN")
	out := flag.String("out", "certs", "output directory for cert.pem and key.pem")
	validFor := flag.Duration("valid-for", 365*24*time.Hour, "certificate validity duration")
	flag.Parse()

	if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatalf("gencert: mkdir %s: %v", *out, err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatalf("gencert: generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		log.Fatalf("gencert: serial: %v", err)
	}

	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "prifa dev", Organization: []string{"prifa"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(*validFor),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	for _, h := range strings.Split(*hosts, ",") {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		log.Fatalf("gencert: create certificate: %v", err)
	}

	certPath := filepath.Join(*out, "cert.pem")
	keyPath := filepath.Join(*out, "key.pem")

	certFile, err := os.OpenFile(certPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatalf("gencert: open %s: %v", certPath, err)
	}
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		log.Fatalf("gencert: encode cert: %v", err)
	}
	if err := certFile.Close(); err != nil {
		log.Fatalf("gencert: close cert: %v", err)
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		log.Fatalf("gencert: marshal key: %v", err)
	}
	keyFile, err := os.OpenFile(keyPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		log.Fatalf("gencert: open %s: %v", keyPath, err)
	}
	if err := pem.Encode(keyFile, &pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}); err != nil {
		log.Fatalf("gencert: encode key: %v", err)
	}
	if err := keyFile.Close(); err != nil {
		log.Fatalf("gencert: close key: %v", err)
	}

	fmt.Printf("wrote %s\nwrote %s\nhosts: %s\nvalid until: %s\n",
		certPath, keyPath, *hosts, template.NotAfter.Format(time.RFC3339))
}

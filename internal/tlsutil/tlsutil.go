// Package tlsutil builds the TLS configuration shared by the IMAP and SMTP
// listeners.
package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"sync"
	"time"
)

// reloader serves a certificate from disk and re-reads it when the file
// changes, so cert-manager renewals are picked up without a restart.
type reloader struct {
	certFile, keyFile string
	logger            *slog.Logger

	mu      sync.Mutex
	cert    *tls.Certificate
	modTime time.Time
}

func (r *reloader) get(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	info, err := os.Stat(r.certFile)
	if err != nil {
		if r.cert != nil {
			return r.cert, nil // keep serving the last good certificate
		}
		return nil, fmt.Errorf("stat certificate: %w", err)
	}
	if r.cert != nil && info.ModTime().Equal(r.modTime) {
		return r.cert, nil
	}

	cert, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		if r.cert != nil {
			r.logger.Error("TLS certificate reload failed, keeping the previous one", "error", err)
			return r.cert, nil
		}
		return nil, fmt.Errorf("load certificate: %w", err)
	}
	if r.cert != nil {
		r.logger.Info("TLS certificate reloaded")
	}
	r.cert, r.modTime = &cert, info.ModTime()
	return r.cert, nil
}

// Config returns a server TLS config. With certFile and keyFile empty it
// generates a self-signed certificate, which is only suitable for development.
func Config(certFile, keyFile string, logger *slog.Logger) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if certFile != "" {
		r := &reloader{certFile: certFile, keyFile: keyFile, logger: logger}
		if _, err := r.get(nil); err != nil { // fail fast on a bad certificate
			return nil, err
		}
		cfg.GetCertificate = r.get
		return cfg, nil
	}

	cert, err := selfSigned()
	if err != nil {
		return nil, err
	}
	logger.Warn("TLS_CERT_FILE/TLS_KEY_FILE not set, using a self-signed certificate")
	cfg.Certificates = []tls.Certificate{cert}
	return cfg, nil
}

func selfSigned() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("serial: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "mailcatcher"},
		DNSNames:     []string{"localhost", "mailcatcher"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// ABOUTME: Shared TLS configuration builder for API and gRPC servers.
// ABOUTME: Supports server-only TLS and mutual TLS (mTLS) with CA verification.

package dynupdate

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// buildTLSConfig creates a *tls.Config from the plugin's tlsConfig.
// When a CA is provided, mTLS with RequireAndVerifyClientCert is enabled.
func buildTLSConfig(cfg *tlsConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.cert, cfg.key)
	if err != nil {
		return nil, fmt.Errorf("loading TLS keypair: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.ca != "" {
		caPEM, err := os.ReadFile(cfg.ca)
		if err != nil {
			return nil, fmt.Errorf("reading CA file %s: %w", cfg.ca, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("CA file %s contains no valid certificates", cfg.ca)
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsCfg, nil
}

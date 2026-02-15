// ABOUTME: Tests for TLS configuration builder with cert generation helper.
// ABOUTME: Covers mTLS (with CA), server-only TLS, and invalid path handling.

package dynupdate

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	ctls "crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// testCerts holds paths to generated test certificates.
type testCerts struct {
	CACert     string
	ServerCert string
	ServerKey  string
	ClientCert string
	ClientKey  string
}

// generateTestCerts creates a CA, server, and client certificate for testing.
func generateTestCerts(t *testing.T) testCerts {
	t.Helper()
	dir := t.TempDir()

	// Generate CA key and cert
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test CA"},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}

	caCertDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating CA cert: %v", err)
	}
	caCert, err := x509.ParseCertificate(caCertDER)
	if err != nil {
		t.Fatalf("parsing CA cert: %v", err)
	}

	caCertPath := filepath.Join(dir, "ca.pem")
	writePEM(t, caCertPath, "CERTIFICATE", caCertDER)

	// Generate server cert signed by CA
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
	}
	serverCertDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating server cert: %v", err)
	}
	serverCertPath := filepath.Join(dir, "server.pem")
	writePEM(t, serverCertPath, "CERTIFICATE", serverCertDER)
	serverKeyPath := filepath.Join(dir, "server-key.pem")
	writeKeyPEM(t, serverKeyPath, serverKey)

	// Generate client cert signed by CA
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generating client key: %v", err)
	}
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "test-client"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientCertDER, err := x509.CreateCertificate(rand.Reader, clientTemplate, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("creating client cert: %v", err)
	}
	clientCertPath := filepath.Join(dir, "client.pem")
	writePEM(t, clientCertPath, "CERTIFICATE", clientCertDER)
	clientKeyPath := filepath.Join(dir, "client-key.pem")
	writeKeyPEM(t, clientKeyPath, clientKey)

	return testCerts{
		CACert:     caCertPath,
		ServerCert: serverCertPath,
		ServerKey:  serverKeyPath,
		ClientCert: clientCertPath,
		ClientKey:  clientKeyPath,
	}
}

func writePEM(t *testing.T, path, blockType string, data []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: data}); err != nil {
		t.Fatalf("encoding PEM %s: %v", path, err)
	}
}

func writeKeyPEM(t *testing.T, path string, key *ecdsa.PrivateKey) {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshalling EC key: %v", err)
	}
	writePEM(t, path, "EC PRIVATE KEY", der)
}

func TestBuildTLSConfig_WithCA_mTLS(t *testing.T) {
	t.Parallel()
	certs := generateTestCerts(t)

	cfg := &tlsConfig{
		cert: certs.ServerCert,
		key:  certs.ServerKey,
		ca:   certs.CACert,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() error: %v", err)
	}
	if tlsCfg.ClientAuth != ctls.RequireAndVerifyClientCert {
		t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", tlsCfg.ClientAuth)
	}
	if tlsCfg.MinVersion != ctls.VersionTLS12 {
		t.Errorf("MinVersion = %v, want TLS 1.2", tlsCfg.MinVersion)
	}
	if tlsCfg.ClientCAs == nil {
		t.Error("ClientCAs is nil, want CA pool")
	}
}

func TestBuildTLSConfig_WithoutCA_ServerOnly(t *testing.T) {
	t.Parallel()
	certs := generateTestCerts(t)

	cfg := &tlsConfig{
		cert: certs.ServerCert,
		key:  certs.ServerKey,
	}

	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		t.Fatalf("buildTLSConfig() error: %v", err)
	}
	if tlsCfg.ClientAuth != ctls.NoClientCert {
		t.Errorf("ClientAuth = %v, want NoClientCert", tlsCfg.ClientAuth)
	}
	if tlsCfg.ClientCAs != nil {
		t.Error("ClientCAs should be nil without CA")
	}
}

func TestBuildTLSConfig_InvalidPaths(t *testing.T) {
	t.Parallel()
	cfg := &tlsConfig{
		cert: "/nonexistent/cert.pem",
		key:  "/nonexistent/key.pem",
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("buildTLSConfig() expected error for invalid paths")
	}
}

func TestBuildTLSConfig_InvalidCA(t *testing.T) {
	t.Parallel()
	certs := generateTestCerts(t)

	// Use the server key as CA (invalid PEM for certs)
	cfg := &tlsConfig{
		cert: certs.ServerCert,
		key:  certs.ServerKey,
		ca:   certs.ServerKey, // not a valid cert PEM
	}

	_, err := buildTLSConfig(cfg)
	if err == nil {
		t.Fatal("buildTLSConfig() expected error for invalid CA")
	}
}

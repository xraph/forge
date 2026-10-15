package grpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xraph/forge"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	testgrpc "google.golang.org/grpc/interop/grpc_testing"
)

// writeTestCert writes a self-signed certificate for 127.0.0.1 and its key
// into dir, and returns their paths and the certificate.
func writeTestCert(t *testing.T, dir string) (certFile, keyFile string, cert *x509.Certificate) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "grpc-test"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	cert, err = x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	certFile = filepath.Join(dir, "server.crt")
	keyFile = filepath.Join(dir, "server.key")

	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}

	return certFile, keyFile, cert
}

func TestStartFailsWhenTLSFilesDontLoad(t *testing.T) {
	cfg := DefaultConfig()
	cfg.EnableTLS = true
	cfg.TLSCertFile = filepath.Join(t.TempDir(), "missing.crt")
	cfg.TLSKeyFile = filepath.Join(t.TempDir(), "missing.key")

	server := NewGRPCServer(cfg, forge.NewNoopLogger(), forge.NewNoOpMetrics())

	if err := server.Start(t.Context(), "127.0.0.1:0"); err == nil {
		_ = server.Stop(context.Background())

		t.Fatal("Start with unreadable TLS files succeeded, want an error")
	}

	if server.IsRunning() {
		t.Error("IsRunning() is true after Start failed to load TLS")
	}

	if server.GetServer() != nil {
		t.Error("GetServer() is not nil after Start failed to load TLS")
	}
}

func TestStartServesTLS(t *testing.T) {
	certFile, keyFile, cert := writeTestCert(t, t.TempDir())

	cfg := DefaultConfig()
	cfg.EnableTLS = true
	cfg.TLSCertFile = certFile
	cfg.TLSKeyFile = keyFile

	server := NewGRPCServer(cfg, forge.NewNoopLogger(), forge.NewNoOpMetrics())
	if err := server.RegisterService(&testgrpc.TestService_ServiceDesc, testService{}); err != nil {
		t.Fatal(err)
	}

	if err := server.Start(t.Context(), "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = server.Stop(context.Background()) })

	pool := x509.NewCertPool()
	pool.AddCert(cert)

	addr := server.(*grpcServer).listener.Addr().String()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	if err := emptyCall(t, conn); err != nil {
		t.Fatalf("EmptyCall over TLS: %v", err)
	}
}

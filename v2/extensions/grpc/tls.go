package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/xraph/forge/v2"
	"google.golang.org/grpc/credentials"
)

// loadTLSCredentials loads TLS credentials from files
func (s *grpcServer) loadTLSCredentials() (credentials.TransportCredentials, error) {
	// Load server certificate and key
	serverCert, err := tls.LoadX509KeyPair(s.config.TLSCertFile, s.config.TLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server cert/key: %w", err)
	}

	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.NoClientCert,
	}

	// If client authentication is required, load CA
	if s.config.ClientAuth {
		if s.config.TLSCAFile == "" {
			return nil, fmt.Errorf("client auth enabled but no CA file provided")
		}

		certPool := x509.NewCertPool()
		ca, err := os.ReadFile(s.config.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}

		if !certPool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("failed to append CA cert")
		}

		config.ClientAuth = tls.RequireAndVerifyClientCert
		config.ClientCAs = certPool

		s.logger.Info("grpc mTLS enabled with client verification",
			forge.F("ca_file", s.config.TLSCAFile),
		)
	}

	return credentials.NewTLS(config), nil
}



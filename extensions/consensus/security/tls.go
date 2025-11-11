package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// TLSConfig contains TLS configuration.
type TLSConfig struct {
	Enabled            bool
	CertFile           string
	KeyFile            string
	CAFile             string
	ClientAuthRequired bool
	InsecureSkipVerify bool
	MinVersion         uint16
	MaxVersion         uint16
	CipherSuites       []uint16
}

// TLSManager manages TLS configuration.
type TLSManager struct {
	config    TLSConfig
	logger    forge.Logger
	tlsConfig *tls.Config
}

// NewTLSManager creates a new TLS manager.
func NewTLSManager(config TLSConfig, logger forge.Logger) (*TLSManager, error) {
	tm := &TLSManager{
		config: config,
		logger: logger,
	}

	if config.Enabled {
		if err := tm.initialize(); err != nil {
			return nil, fmt.Errorf("failed to initialize TLS: %w", err)
		}
	}

	return tm, nil
}

// initialize initializes the TLS configuration.
func (tm *TLSManager) initialize() error {
	// Load server certificate and key
	cert, err := tls.LoadX509KeyPair(tm.config.CertFile, tm.config.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load certificate: %w", err)
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tm.config.MinVersion,
		MaxVersion:   tm.config.MaxVersion,
	}

	// Set default minimum version if not specified
	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}

	// Configure cipher suites if specified
	if len(tm.config.CipherSuites) > 0 {
		tlsConfig.CipherSuites = tm.config.CipherSuites
	} else {
		// Use secure defaults
		tlsConfig.CipherSuites = []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		}
	}

	// Load CA certificate for client authentication
	if tm.config.CAFile != "" {
		caCert, err := os.ReadFile(tm.config.CAFile)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return errors.New("failed to append CA certificate")
		}

		tlsConfig.ClientCAs = caCertPool

		if tm.config.ClientAuthRequired {
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert

			tm.logger.Info("mTLS enabled - client certificates required")
		} else {
			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven

			tm.logger.Info("TLS enabled - client certificates optional")
		}
	}

	// Configure for InsecureSkipVerify (not recommended for production)
	if tm.config.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true

		tm.logger.Warn("TLS certificate verification disabled - NOT RECOMMENDED FOR PRODUCTION")
	}

	tm.tlsConfig = tlsConfig

	tm.logger.Info("TLS initialized",
		forge.F("min_version", tm.getTLSVersionName(tlsConfig.MinVersion)),
		forge.F("max_version", tm.getTLSVersionName(tlsConfig.MaxVersion)),
		forge.F("cipher_suites", len(tlsConfig.CipherSuites)),
		forge.F("client_auth", tm.config.ClientAuthRequired),
	)

	return nil
}

// GetTLSConfig returns the TLS configuration.
func (tm *TLSManager) GetTLSConfig() *tls.Config {
	return tm.tlsConfig
}

// IsEnabled returns true if TLS is enabled.
func (tm *TLSManager) IsEnabled() bool {
	return tm.config.Enabled
}

// IsMTLSEnabled returns true if mTLS is enabled.
func (tm *TLSManager) IsMTLSEnabled() bool {
	return tm.config.Enabled && tm.config.ClientAuthRequired
}

// VerifyPeerCertificate verifies a peer certificate.
func (tm *TLSManager) VerifyPeerCertificate(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if !tm.config.Enabled {
		return nil
	}

	if len(verifiedChains) == 0 {
		return errors.New("no verified certificate chains")
	}

	if len(verifiedChains[0]) == 0 {
		return errors.New("empty certificate chain")
	}

	cert := verifiedChains[0][0]

	tm.logger.Debug("peer certificate verified",
		forge.F("subject", cert.Subject.String()),
		forge.F("issuer", cert.Issuer.String()),
		forge.F("expires", cert.NotAfter),
	)

	return nil
}

// getTLSVersionName returns the name of a TLS version.
func (tm *TLSManager) getTLSVersionName(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return "Unknown"
	}
}

// DefaultSecureTLSConfig returns a secure default TLS configuration.
func DefaultSecureTLSConfig() TLSConfig {
	return TLSConfig{
		Enabled:            true,
		ClientAuthRequired: true,
		InsecureSkipVerify: false,
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS13,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		},
	}
}

// CreateClientTLSConfig creates a TLS config for client connections.
func CreateClientTLSConfig(certFile, keyFile, caFile string, insecureSkipVerify bool) (*tls.Config, error) {
	// Load client certificate
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	// Load CA certificate
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("failed to append CA certificate")
	}

	return &tls.Config{
		Certificates:       []tls.Certificate{cert},
		RootCAs:            caCertPool,
		InsecureSkipVerify: insecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
	}, nil
}

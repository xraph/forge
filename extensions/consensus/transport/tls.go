package transport

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// TLSConfig contains TLS configuration for secure transport.
type TLSConfig struct {
	// Enable TLS
	Enabled bool

	// Certificate and key paths
	CertFile string
	KeyFile  string
	CAFile   string

	// mTLS configuration
	RequireClientCert bool
	ClientCAFile      string

	// TLS version
	MinVersion uint16
	MaxVersion uint16

	// Cipher suites
	CipherSuites []uint16

	// Server name for SNI
	ServerName string

	// Skip verification (for testing only)
	InsecureSkipVerify bool
}

// TLSManager manages TLS configuration and certificate loading.
type TLSManager struct {
	config TLSConfig
	logger forge.Logger

	// Loaded TLS config
	tlsConfig *tls.Config

	// Certificate reload
	certReloadInterval time.Duration
	lastReload         time.Time
}

// NewTLSManager creates a new TLS manager.
func NewTLSManager(config TLSConfig, logger forge.Logger) (*TLSManager, error) {
	tm := &TLSManager{
		config:             config,
		logger:             logger,
		certReloadInterval: 24 * time.Hour,
	}

	if config.Enabled {
		if err := tm.loadTLSConfig(); err != nil {
			return nil, err
		}
	}

	return tm, nil
}

// loadTLSConfig loads TLS configuration.
func (tm *TLSManager) loadTLSConfig() error {
	if !tm.config.Enabled {
		return nil
	}

	// Load certificate and key
	cert, err := tls.LoadX509KeyPair(tm.config.CertFile, tm.config.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to load certificate: %w", err)
	}

	// Create TLS config
	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		MinVersion:         tm.config.MinVersion,
		MaxVersion:         tm.config.MaxVersion,
		ServerName:         tm.config.ServerName,
		InsecureSkipVerify: tm.config.InsecureSkipVerify,
	}

	// Set default versions if not specified
	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}

	if tlsConfig.MaxVersion == 0 {
		tlsConfig.MaxVersion = tls.VersionTLS13
	}

	// Configure cipher suites if specified
	if len(tm.config.CipherSuites) > 0 {
		tlsConfig.CipherSuites = tm.config.CipherSuites
	} else {
		// Use secure default cipher suites
		tlsConfig.CipherSuites = []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		}
	}

	// Load CA certificate if provided
	if tm.config.CAFile != "" {
		caCert, err := os.ReadFile(tm.config.CAFile)
		if err != nil {
			return fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return errors.New("failed to parse CA certificate")
		}

		tlsConfig.RootCAs = caCertPool
	}

	// Configure mTLS if required
	if tm.config.RequireClientCert {
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert

		if tm.config.ClientCAFile != "" {
			clientCACert, err := os.ReadFile(tm.config.ClientCAFile)
			if err != nil {
				return fmt.Errorf("failed to read client CA certificate: %w", err)
			}

			clientCACertPool := x509.NewCertPool()
			if !clientCACertPool.AppendCertsFromPEM(clientCACert) {
				return errors.New("failed to parse client CA certificate")
			}

			tlsConfig.ClientCAs = clientCACertPool
		}
	}

	tm.tlsConfig = tlsConfig
	tm.lastReload = time.Now()

	tm.logger.Info("TLS configuration loaded",
		forge.F("min_version", tlsVersionString(tlsConfig.MinVersion)),
		forge.F("max_version", tlsVersionString(tlsConfig.MaxVersion)),
		forge.F("mtls", tm.config.RequireClientCert),
	)

	return nil
}

// GetTLSConfig returns the TLS configuration.
func (tm *TLSManager) GetTLSConfig() *tls.Config {
	if !tm.config.Enabled {
		return nil
	}

	// Clone to prevent modification
	if tm.tlsConfig != nil {
		return tm.tlsConfig.Clone()
	}

	return nil
}

// ReloadCertificates reloads certificates from disk.
func (tm *TLSManager) ReloadCertificates() error {
	if !tm.config.Enabled {
		return nil
	}

	tm.logger.Info("reloading TLS certificates")

	if err := tm.loadTLSConfig(); err != nil {
		tm.logger.Error("failed to reload certificates",
			forge.F("error", err),
		)

		return err
	}

	tm.logger.Info("TLS certificates reloaded successfully")

	return nil
}

// ShouldReload checks if certificates should be reloaded.
func (tm *TLSManager) ShouldReload() bool {
	if !tm.config.Enabled {
		return false
	}

	return time.Since(tm.lastReload) >= tm.certReloadInterval
}

// VerifyPeerCertificate verifies a peer certificate.
func (tm *TLSManager) VerifyPeerCertificate(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
	if !tm.config.Enabled || tm.config.InsecureSkipVerify {
		return nil
	}

	if len(rawCerts) == 0 {
		return errors.New("no peer certificates provided")
	}

	// Parse certificate
	cert, err := x509.ParseCertificate(rawCerts[0])
	if err != nil {
		return fmt.Errorf("failed to parse peer certificate: %w", err)
	}

	// Check expiration
	now := time.Now()
	if now.Before(cert.NotBefore) {
		return errors.New("certificate not yet valid")
	}

	if now.After(cert.NotAfter) {
		return errors.New("certificate has expired")
	}

	tm.logger.Debug("peer certificate verified",
		forge.F("subject", cert.Subject.CommonName),
		forge.F("issuer", cert.Issuer.CommonName),
		forge.F("expires", cert.NotAfter),
	)

	return nil
}

// tlsVersionString converts TLS version to string.
func tlsVersionString(version uint16) string {
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
		return fmt.Sprintf("Unknown (0x%04x)", version)
	}
}

// GetCertificateInfo returns information about the loaded certificate.
func (tm *TLSManager) GetCertificateInfo() map[string]any {
	if !tm.config.Enabled || tm.tlsConfig == nil || len(tm.tlsConfig.Certificates) == 0 {
		return map[string]any{
			"enabled": false,
		}
	}

	cert := tm.tlsConfig.Certificates[0]
	if cert.Leaf == nil && len(cert.Certificate) > 0 {
		// Parse leaf certificate
		var err error

		cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return map[string]any{
				"enabled": true,
				"error":   err.Error(),
			}
		}
	}

	if cert.Leaf != nil {
		return map[string]any{
			"enabled":    true,
			"subject":    cert.Leaf.Subject.CommonName,
			"issuer":     cert.Leaf.Issuer.CommonName,
			"not_before": cert.Leaf.NotBefore,
			"not_after":  cert.Leaf.NotAfter,
			"dns_names":  cert.Leaf.DNSNames,
			"mtls":       tm.config.RequireClientCert,
		}
	}

	return map[string]any{
		"enabled": true,
	}
}

// IsEnabled returns whether TLS is enabled.
func (tm *TLSManager) IsEnabled() bool {
	return tm.config.Enabled
}

// IsMTLSEnabled returns whether mTLS is enabled.
func (tm *TLSManager) IsMTLSEnabled() bool {
	return tm.config.Enabled && tm.config.RequireClientCert
}

// DefaultTLSConfig returns a secure default TLS configuration.
func DefaultTLSConfig() TLSConfig {
	return TLSConfig{
		Enabled:            true,
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS13,
		RequireClientCert:  false,
		InsecureSkipVerify: false,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_CHACHA20_POLY1305_SHA256,
		},
	}
}

// MTLSConfig returns a default mTLS configuration.
func MTLSConfig(certFile, keyFile, caFile string) TLSConfig {
	config := DefaultTLSConfig()
	config.CertFile = certFile
	config.KeyFile = keyFile
	config.CAFile = caFile
	config.ClientCAFile = caFile
	config.RequireClientCert = true

	return config
}

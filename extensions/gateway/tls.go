package gateway

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
)

// TLSManager manages TLS configurations for upstream connections.
// It supports:
//   - Global TLS configuration for all upstream connections
//   - Per-target TLS overrides (specific CA certs, client certs for mTLS)
//   - Automatic certificate reloading on a configurable interval
//   - TLS version and cipher suite configuration
//   - InsecureSkipVerify for development/testing
type TLSManager struct {
	config     TLSConfig
	logger     forge.Logger
	globalTLS  atomic.Pointer[tls.Config]
	targetTLS  sync.Map // map[string]*tls.Config (targetID -> per-target TLS)
	stopCh     chan struct{}
	lastReload time.Time
}

// NewTLSManager creates a new TLS manager.
func NewTLSManager(config TLSConfig, logger forge.Logger) *TLSManager {
	tm := &TLSManager{
		config: config,
		logger: logger,
		stopCh: make(chan struct{}),
	}

	// Build initial global TLS config
	globalCfg := tm.buildGlobalTLSConfig()
	tm.globalTLS.Store(globalCfg)
	tm.lastReload = time.Now()

	return tm
}

// GlobalTLSConfig returns the current global TLS configuration.
// This is safe for concurrent use.
func (tm *TLSManager) GlobalTLSConfig() *tls.Config {
	return tm.globalTLS.Load()
}

// TargetTLSConfig returns a TLS configuration for a specific target.
// If the target has per-target TLS overrides, those are used.
// Otherwise, the global TLS config is returned.
func (tm *TLSManager) TargetTLSConfig(target *Target) *tls.Config {
	if target.TLS == nil || !target.TLS.Enabled {
		return tm.GlobalTLSConfig()
	}

	// Check cache first
	if cached, ok := tm.targetTLS.Load(target.ID); ok {
		return cached.(*tls.Config)
	}

	// Build per-target TLS config
	cfg := tm.buildTargetTLSConfig(target.TLS)
	tm.targetTLS.Store(target.ID, cfg)

	return cfg
}

// TransportForTarget returns an http.Transport configured with the appropriate
// TLS settings for the given target.
func (tm *TLSManager) TransportForTarget(baseTransport *http.Transport, target *Target) *http.Transport {
	tlsCfg := tm.TargetTLSConfig(target)

	if tlsCfg == baseTransport.TLSClientConfig {
		return baseTransport
	}

	// Clone the base transport with target-specific TLS
	clone := baseTransport.Clone()
	clone.TLSClientConfig = tlsCfg

	return clone
}

// Start begins the certificate reload loop if configured.
func (tm *TLSManager) Start() {
	if !tm.config.Enabled || tm.config.ReloadInterval <= 0 {
		return
	}

	go tm.reloadLoop()

	tm.logger.Info("TLS certificate reload enabled",
		forge.F("interval", tm.config.ReloadInterval),
	)
}

// Stop stops the certificate reload loop.
func (tm *TLSManager) Stop() {
	close(tm.stopCh)
}

// Reload forces an immediate certificate reload.
func (tm *TLSManager) Reload() error {
	newCfg := tm.buildGlobalTLSConfig()
	tm.globalTLS.Store(newCfg)

	// Clear target TLS cache to force rebuild
	tm.targetTLS.Range(func(key, _ any) bool {
		tm.targetTLS.Delete(key)
		return true
	})

	tm.lastReload = time.Now()

	tm.logger.Info("TLS certificates reloaded")

	return nil
}

// LastReload returns the time of the last certificate reload.
func (tm *TLSManager) LastReload() time.Time {
	return tm.lastReload
}

// reloadLoop periodically reloads certificates.
func (tm *TLSManager) reloadLoop() {
	ticker := time.NewTicker(tm.config.ReloadInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.stopCh:
			return
		case <-ticker.C:
			if err := tm.Reload(); err != nil {
				tm.logger.Error("failed to reload TLS certificates",
					forge.F("error", err),
				)
			}
		}
	}
}

// buildGlobalTLSConfig creates a tls.Config from the global configuration.
func (tm *TLSManager) buildGlobalTLSConfig() *tls.Config {
	cfg := &tls.Config{
		MinVersion:         tlsVersionFromString(tm.config.MinVersion),
		InsecureSkipVerify: tm.config.InsecureSkipVerify, //nolint:gosec // user-configured
	}

	// Load CA certificate for server verification
	if tm.config.CACertFile != "" {
		caCert, err := os.ReadFile(tm.config.CACertFile)
		if err != nil {
			tm.logger.Error("failed to read CA cert file",
				forge.F("file", tm.config.CACertFile),
				forge.F("error", err),
			)
		} else {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				cfg.RootCAs = caCertPool
			} else {
				tm.logger.Warn("failed to parse CA cert", forge.F("file", tm.config.CACertFile))
			}
		}
	}

	// Load client certificate for mTLS
	if tm.config.ClientCertFile != "" && tm.config.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(tm.config.ClientCertFile, tm.config.ClientKeyFile)
		if err != nil {
			tm.logger.Error("failed to load client certificate",
				forge.F("cert_file", tm.config.ClientCertFile),
				forge.F("key_file", tm.config.ClientKeyFile),
				forge.F("error", err),
			)
		} else {
			cfg.Certificates = []tls.Certificate{cert}
		}
	}

	// Configure cipher suites
	if len(tm.config.CipherSuites) > 0 {
		suites := parseCipherSuites(tm.config.CipherSuites)
		if len(suites) > 0 {
			cfg.CipherSuites = suites
		}
	}

	return cfg
}

// buildTargetTLSConfig creates a tls.Config for a specific target.
func (tm *TLSManager) buildTargetTLSConfig(targetTLS *TargetTLSConfig) *tls.Config {
	// Start with the global config as a base
	base := tm.GlobalTLSConfig()
	cfg := base.Clone()

	cfg.InsecureSkipVerify = targetTLS.InsecureSkipVerify //nolint:gosec // user-configured

	// Override server name if specified
	if targetTLS.ServerName != "" {
		cfg.ServerName = targetTLS.ServerName
	}

	// Override CA cert if specified
	if targetTLS.CACertFile != "" {
		caCert, err := os.ReadFile(targetTLS.CACertFile)
		if err != nil {
			tm.logger.Error("failed to read target CA cert file",
				forge.F("file", targetTLS.CACertFile),
				forge.F("error", err),
			)
		} else {
			caCertPool := x509.NewCertPool()
			if caCertPool.AppendCertsFromPEM(caCert) {
				cfg.RootCAs = caCertPool
			}
		}
	}

	// Override client cert for per-target mTLS
	if targetTLS.ClientCertFile != "" && targetTLS.ClientKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(targetTLS.ClientCertFile, targetTLS.ClientKeyFile)
		if err != nil {
			tm.logger.Error("failed to load target client certificate",
				forge.F("cert_file", targetTLS.ClientCertFile),
				forge.F("key_file", targetTLS.ClientKeyFile),
				forge.F("error", err),
			)
		} else {
			cfg.Certificates = []tls.Certificate{cert}
		}
	}

	return cfg
}

// tlsVersionFromString converts a version string to a tls version constant.
func tlsVersionFromString(version string) uint16 {
	switch version {
	case "1.0":
		return tls.VersionTLS10 //nolint:staticcheck // support legacy
	case "1.1":
		return tls.VersionTLS11 //nolint:staticcheck // support legacy
	case "1.2":
		return tls.VersionTLS12
	case "1.3":
		return tls.VersionTLS13
	default:
		return tls.VersionTLS12
	}
}

// parseCipherSuites converts cipher suite names to TLS cipher suite IDs.
func parseCipherSuites(names []string) []uint16 {
	// Build a lookup map from the standard cipher suites
	available := make(map[string]uint16)
	for _, suite := range tls.CipherSuites() {
		available[suite.Name] = suite.ID
	}

	// Also include insecure suites for completeness
	for _, suite := range tls.InsecureCipherSuites() {
		available[suite.Name] = suite.ID
	}

	result := make([]uint16, 0, len(names))

	for _, name := range names {
		if id, ok := available[name]; ok {
			result = append(result, id)
		}
	}

	return result
}

// ValidateTLSConfig checks that the TLS configuration is valid.
func ValidateTLSConfig(config TLSConfig) error {
	if !config.Enabled {
		return nil
	}

	// Validate CA cert file if specified
	if config.CACertFile != "" {
		if _, err := os.Stat(config.CACertFile); err != nil {
			return fmt.Errorf("CA cert file not found: %s: %w", config.CACertFile, err)
		}
	}

	// Validate client cert/key pair (both must be present for mTLS)
	if config.ClientCertFile != "" || config.ClientKeyFile != "" {
		if config.ClientCertFile == "" || config.ClientKeyFile == "" {
			return fmt.Errorf("both client cert and key must be provided for mTLS")
		}

		if _, err := os.Stat(config.ClientCertFile); err != nil {
			return fmt.Errorf("client cert file not found: %s: %w", config.ClientCertFile, err)
		}

		if _, err := os.Stat(config.ClientKeyFile); err != nil {
			return fmt.Errorf("client key file not found: %s: %w", config.ClientKeyFile, err)
		}

		// Verify the pair loads correctly
		if _, err := tls.LoadX509KeyPair(config.ClientCertFile, config.ClientKeyFile); err != nil {
			return fmt.Errorf("invalid client certificate pair: %w", err)
		}
	}

	// Validate min version
	switch config.MinVersion {
	case "", "1.0", "1.1", "1.2", "1.3":
		// valid
	default:
		return fmt.Errorf("invalid TLS min version: %s (expected 1.0, 1.1, 1.2, or 1.3)", config.MinVersion)
	}

	return nil
}

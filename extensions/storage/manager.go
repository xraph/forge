package storage

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// StorageManager manages multiple storage backends.
type StorageManager struct {
	config         Config
	backends       map[string]Storage
	defaultBackend Storage
	logger         forge.Logger
	metrics        forge.Metrics
	healthChecker  *HealthChecker
	mu             sync.RWMutex
}

// NewStorageManager creates a new storage manager.
func NewStorageManager(config Config, logger forge.Logger, metrics forge.Metrics) *StorageManager {
	return &StorageManager{
		config:   config,
		backends: make(map[string]Storage),
		logger:   logger,
		metrics:  metrics,
	}
}

// Name returns the service name for Vessel's lifecycle management.
// Implements di.Service interface.
func (m *StorageManager) Name() string {
	return "storage-manager"
}

// Start initializes all storage backends.
func (m *StorageManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure resilience config has sane defaults
	if m.config.Resilience.OperationTimeout == 0 {
		m.config.Resilience = DefaultResilienceConfig()
	}

	// Initialize backends
	for name, backendConfig := range m.config.Backends {
		var (
			backend Storage
			err     error
		)

		switch backendConfig.Type {
		case BackendTypeLocal:
			// Use enhanced backend if configured
			if m.config.UseEnhancedBackend {
				backend, err = NewEnhancedLocalBackend(backendConfig.Config, m.logger, m.metrics)
			} else {
				backend, err = NewLocalBackend(backendConfig.Config, m.logger, m.metrics)
			}
		case BackendTypeS3:
			backend, err = NewS3Backend(backendConfig.Config, m.logger, m.metrics)
		case BackendTypeGCS:
			// TODO: Implement GCS backend
			return errors.New("GCS backend not yet implemented")
		case BackendTypeAzure:
			// TODO: Implement Azure backend
			return errors.New("Azure backend not yet implemented")
		default:
			return fmt.Errorf("unknown backend type: %s", backendConfig.Type)
		}

		if err != nil {
			return fmt.Errorf("failed to create backend %s: %w", name, err)
		}

		// Wrap with resilience layer
		resilientBackend := NewResilientStorage(backend, m.config.Resilience, m.logger, m.metrics)

		m.backends[name] = resilientBackend
		m.logger.Info("storage backend initialized",
			forge.F("name", name),
			forge.F("type", backendConfig.Type),
			forge.F("resilience_enabled", m.config.Resilience.CircuitBreakerEnabled),
		)
	}

	// Set default backend
	if m.config.Default != "" {
		backend, exists := m.backends[m.config.Default]
		if !exists {
			return fmt.Errorf("default backend %s not found", m.config.Default)
		}

		m.defaultBackend = backend
	}

	// Initialize health checker
	m.healthChecker = NewHealthChecker(m.backends, m.logger, m.metrics, DefaultHealthCheckConfig())

	return nil
}

// Stop closes all storage backends.
func (m *StorageManager) Stop(ctx context.Context) error {
	// Local backend doesn't need cleanup
	return nil
}

// Health checks the health of all backends.
func (m *StorageManager) Health(ctx context.Context) error {
	if m.healthChecker == nil {
		return errors.New("health checker not initialized")
	}

	health, err := m.healthChecker.CheckHealth(ctx, m.config.Default, false)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if !health.Healthy {
		return fmt.Errorf("storage backend unhealthy: %d/%d backends healthy",
			health.HealthyCount, health.BackendCount)
	}

	return nil
}

// HealthDetailed returns detailed health information.
func (m *StorageManager) HealthDetailed(ctx context.Context, checkAll bool) (*OverallHealth, error) {
	if m.healthChecker == nil {
		return nil, errors.New("health checker not initialized")
	}

	return m.healthChecker.CheckHealth(ctx, m.config.Default, checkAll)
}

// BackendHealth returns health of a specific backend.
func (m *StorageManager) BackendHealth(ctx context.Context, name string) (*BackendHealth, error) {
	if m.healthChecker == nil {
		return nil, errors.New("health checker not initialized")
	}

	return m.healthChecker.GetBackendHealth(ctx, name)
}

// HealthCheckAll checks the health of all backends and returns a map of backend names to their health status.
func (m *StorageManager) HealthCheckAll(ctx context.Context) map[string]*BackendHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*BackendHealth)

	if m.healthChecker == nil {
		for name := range m.backends {
			result[name] = &BackendHealth{
				Name:    name,
				Healthy: false,
				Error:   "health checker not initialized",
			}
		}

		return result
	}

	for name := range m.backends {
		health, err := m.healthChecker.GetBackendHealth(ctx, name)
		if err != nil {
			result[name] = &BackendHealth{
				Name:    name,
				Healthy: false,
				Error:   err.Error(),
			}

			continue
		}

		result[name] = health
	}

	return result
}

// Backend returns a specific backend.
func (m *StorageManager) Backend(name string) Storage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	backend, exists := m.backends[name]
	if !exists {
		return nil
	}

	return backend
}

// DefaultBackend returns the default backend.
// Returns error if no default backend is configured.
func (m *StorageManager) DefaultBackend() (Storage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.defaultBackend == nil {
		return nil, errors.New("no default backend configured")
	}

	return m.defaultBackend, nil
}

// Upload uploads to the default backend.
func (m *StorageManager) Upload(ctx context.Context, key string, data io.Reader, opts ...UploadOption) error {
	backend, err := m.DefaultBackend()
	if err != nil {
		return err
	}

	return backend.Upload(ctx, key, data, opts...)
}

// Download downloads from the default backend.
func (m *StorageManager) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	backend, err := m.DefaultBackend()
	if err != nil {
		return nil, err
	}

	return backend.Download(ctx, key)
}

// Delete deletes from the default backend.
func (m *StorageManager) Delete(ctx context.Context, key string) error {
	backend, err := m.DefaultBackend()
	if err != nil {
		return err
	}

	return backend.Delete(ctx, key)
}

// List lists from the default backend.
func (m *StorageManager) List(ctx context.Context, prefix string, opts ...ListOption) ([]Object, error) {
	backend, err := m.DefaultBackend()
	if err != nil {
		return nil, err
	}

	return backend.List(ctx, prefix, opts...)
}

// Metadata gets metadata from the default backend.
func (m *StorageManager) Metadata(ctx context.Context, key string) (*ObjectMetadata, error) {
	backend, err := m.DefaultBackend()
	if err != nil {
		return nil, err
	}

	return backend.Metadata(ctx, key)
}

// Exists checks existence in the default backend.
func (m *StorageManager) Exists(ctx context.Context, key string) (bool, error) {
	backend, err := m.DefaultBackend()
	if err != nil {
		return false, err
	}

	return backend.Exists(ctx, key)
}

// Copy copies in the default backend.
func (m *StorageManager) Copy(ctx context.Context, srcKey, dstKey string) error {
	backend, err := m.DefaultBackend()
	if err != nil {
		return err
	}

	return backend.Copy(ctx, srcKey, dstKey)
}

// Move moves in the default backend.
func (m *StorageManager) Move(ctx context.Context, srcKey, dstKey string) error {
	backend, err := m.DefaultBackend()
	if err != nil {
		return err
	}

	return backend.Move(ctx, srcKey, dstKey)
}

// PresignUpload generates a presigned upload URL for the default backend.
func (m *StorageManager) PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if !m.config.EnablePresignedURLs {
		return "", ErrPresignNotSupported
	}

	backend, err := m.DefaultBackend()
	if err != nil {
		return "", err
	}

	return backend.PresignUpload(ctx, key, expiry)
}

// PresignDownload generates a presigned download URL for the default backend.
func (m *StorageManager) PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error) {
	if !m.config.EnablePresignedURLs {
		return "", ErrPresignNotSupported
	}

	backend, err := m.DefaultBackend()
	if err != nil {
		return "", err
	}

	return backend.PresignDownload(ctx, key, expiry)
}

// GetURL returns the URL for an object (CDN or direct).
func (m *StorageManager) GetURL(ctx context.Context, key string) string {
	if m.config.EnableCDN && m.config.CDNBaseURL != "" {
		return fmt.Sprintf("%s/%s", m.config.CDNBaseURL, key)
	}

	// Try to get a presigned download URL
	if m.config.EnablePresignedURLs {
		url, err := m.PresignDownload(ctx, key, m.config.PresignExpiry)
		if err == nil {
			return url
		}

		// Log at debug level as this is not critical
		m.logger.Debug("failed to generate presigned URL",
			forge.F("key", key),
			forge.F("error", err.Error()),
		)
	}

	return ""
}

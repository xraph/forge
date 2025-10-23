package extensions

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ExtensionManager manages the loading and lifecycle of extensions
type ExtensionManager struct {
	extensions   map[string]Extension
	extensionsMu sync.RWMutex
	config       ExtensionConfig
	logger       common.Logger
	metrics      common.Metrics
	started      bool
	shutdownC    chan struct{}
	wg           sync.WaitGroup
}

// ExtensionConfig contains configuration for the extension manager
type ExtensionConfig struct {
	AutoLoad            bool           `yaml:"auto_load" default:"true"`
	LoadTimeout         time.Duration  `yaml:"load_timeout" default:"30s"`
	UnloadTimeout       time.Duration  `yaml:"unload_timeout" default:"10s"`
	HealthCheckInterval time.Duration  `yaml:"health_check_interval" default:"30s"`
	Logger              common.Logger  `yaml:"-"`
	Metrics             common.Metrics `yaml:"-"`
}

// Extension represents a loadable extension
type Extension interface {
	// Metadata
	Name() string
	Version() string
	Description() string
	Dependencies() []string

	// Lifecycle
	Initialize(ctx context.Context, config ExtensionConfig) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	HealthCheck(ctx context.Context) error

	// Capabilities
	GetCapabilities() []string
	IsCapabilitySupported(capability string) bool

	// Configuration
	GetConfig() interface{}
	UpdateConfig(config interface{}) error
}

// ExtensionInfo contains metadata about an extension
type ExtensionInfo struct {
	Name         string          `json:"name"`
	Version      string          `json:"version"`
	Description  string          `json:"description"`
	Dependencies []string        `json:"dependencies"`
	Capabilities []string        `json:"capabilities"`
	Status       ExtensionStatus `json:"status"`
	StartedAt    time.Time       `json:"started_at"`
	LastHealth   time.Time       `json:"last_health"`
	Config       interface{}     `json:"config"`
}

// ExtensionStatus represents the status of an extension
type ExtensionStatus string

const (
	ExtensionStatusUnknown   ExtensionStatus = "unknown"
	ExtensionStatusLoading   ExtensionStatus = "loading"
	ExtensionStatusLoaded    ExtensionStatus = "loaded"
	ExtensionStatusStarting  ExtensionStatus = "starting"
	ExtensionStatusRunning   ExtensionStatus = "running"
	ExtensionStatusStopping  ExtensionStatus = "stopping"
	ExtensionStatusStopped   ExtensionStatus = "stopped"
	ExtensionStatusError     ExtensionStatus = "error"
	ExtensionStatusUnhealthy ExtensionStatus = "unhealthy"
)

// NewExtensionManager creates a new extension manager
func NewExtensionManager(config ExtensionConfig) *ExtensionManager {
	return &ExtensionManager{
		extensions: make(map[string]Extension),
		config:     config,
		logger:     config.Logger,
		metrics:    config.Metrics,
		shutdownC:  make(chan struct{}),
	}
}

// RegisterExtension registers a new extension
func (em *ExtensionManager) RegisterExtension(extension Extension) error {
	em.extensionsMu.Lock()
	defer em.extensionsMu.Unlock()

	name := extension.Name()
	if _, exists := em.extensions[name]; exists {
		return fmt.Errorf("extension %s already registered", name)
	}

	em.extensions[name] = extension

	if em.logger != nil {
		em.logger.Info("extension registered",
			logger.String("name", name),
			logger.String("version", extension.Version()),
			logger.Strings("dependencies", extension.Dependencies()),
		)
	}

	return nil
}

// UnregisterExtension unregisters an extension
func (em *ExtensionManager) UnregisterExtension(name string) error {
	em.extensionsMu.Lock()
	defer em.extensionsMu.Unlock()

	extension, exists := em.extensions[name]
	if !exists {
		return fmt.Errorf("extension %s not found", name)
	}

	// Stop the extension if it's running
	if em.started {
		ctx, cancel := context.WithTimeout(context.Background(), em.config.UnloadTimeout)
		defer cancel()

		if err := extension.Stop(ctx); err != nil {
			em.logger.Warn("failed to stop extension during unregistration",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	delete(em.extensions, name)

	if em.logger != nil {
		em.logger.Info("extension unregistered", logger.String("name", name))
	}

	return nil
}

// LoadExtension loads an extension by name
func (em *ExtensionManager) LoadExtension(ctx context.Context, name string) error {
	em.extensionsMu.RLock()
	extension, exists := em.extensions[name]
	em.extensionsMu.RUnlock()

	if !exists {
		return fmt.Errorf("extension %s not found", name)
	}

	// Check dependencies
	if err := em.checkDependencies(extension); err != nil {
		return fmt.Errorf("dependency check failed for extension %s: %w", name, err)
	}

	// Initialize the extension
	if err := extension.Initialize(ctx, ExtensionConfig{
		Logger:  em.logger,
		Metrics: em.metrics,
	}); err != nil {
		return fmt.Errorf("failed to initialize extension %s: %w", name, err)
	}

	if em.logger != nil {
		em.logger.Info("extension loaded",
			logger.String("name", name),
			logger.String("version", extension.Version()),
		)
	}

	return nil
}

// StartExtension starts an extension
func (em *ExtensionManager) StartExtension(ctx context.Context, name string) error {
	em.extensionsMu.RLock()
	extension, exists := em.extensions[name]
	em.extensionsMu.RUnlock()

	if !exists {
		return fmt.Errorf("extension %s not found", name)
	}

	if err := extension.Start(ctx); err != nil {
		return fmt.Errorf("failed to start extension %s: %w", name, err)
	}

	if em.logger != nil {
		em.logger.Info("extension started", logger.String("name", name))
	}

	return nil
}

// StopExtension stops an extension
func (em *ExtensionManager) StopExtension(ctx context.Context, name string) error {
	em.extensionsMu.RLock()
	extension, exists := em.extensions[name]
	em.extensionsMu.RUnlock()

	if !exists {
		return fmt.Errorf("extension %s not found", name)
	}

	if err := extension.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop extension %s: %w", name, err)
	}

	if em.logger != nil {
		em.logger.Info("extension stopped", logger.String("name", name))
	}

	return nil
}

// GetExtension returns an extension by name
func (em *ExtensionManager) GetExtension(name string) (Extension, error) {
	em.extensionsMu.RLock()
	defer em.extensionsMu.RUnlock()

	extension, exists := em.extensions[name]
	if !exists {
		return nil, fmt.Errorf("extension %s not found", name)
	}

	return extension, nil
}

// ListExtensions returns all registered extensions
func (em *ExtensionManager) ListExtensions() []ExtensionInfo {
	em.extensionsMu.RLock()
	defer em.extensionsMu.RUnlock()

	extensions := make([]ExtensionInfo, 0, len(em.extensions))
	for name, extension := range em.extensions {
		extensions = append(extensions, ExtensionInfo{
			Name:         name,
			Version:      extension.Version(),
			Description:  extension.Description(),
			Dependencies: extension.Dependencies(),
			Capabilities: extension.GetCapabilities(),
			Status:       ExtensionStatusLoaded, // TODO: Track actual status
			Config:       extension.GetConfig(),
		})
	}

	return extensions
}

// Start starts all registered extensions
func (em *ExtensionManager) Start(ctx context.Context) error {
	em.extensionsMu.Lock()
	if em.started {
		em.extensionsMu.Unlock()
		return fmt.Errorf("extension manager already started")
	}
	em.started = true
	em.extensionsMu.Unlock()

	// Load all extensions if auto-load is enabled
	if em.config.AutoLoad {
		em.extensionsMu.RLock()
		extensionNames := make([]string, 0, len(em.extensions))
		for name := range em.extensions {
			extensionNames = append(extensionNames, name)
		}
		em.extensionsMu.RUnlock()

		for _, name := range extensionNames {
			if err := em.LoadExtension(ctx, name); err != nil {
				em.logger.Error("failed to load extension",
					logger.String("name", name),
					logger.Error(err),
				)
				continue
			}
		}
	}

	// Start all loaded extensions
	em.extensionsMu.RLock()
	extensions := make(map[string]Extension)
	for name, extension := range em.extensions {
		extensions[name] = extension
	}
	em.extensionsMu.RUnlock()

	for name, extension := range extensions {
		if err := extension.Start(ctx); err != nil {
			em.logger.Error("failed to start extension",
				logger.String("name", name),
				logger.Error(err),
			)
			continue
		}
	}

	// Start health check routine
	em.wg.Add(1)
	go em.healthCheckRoutine()

	if em.logger != nil {
		em.logger.Info("extension manager started")
	}

	return nil
}

// Stop stops all extensions
func (em *ExtensionManager) Stop(ctx context.Context) error {
	em.extensionsMu.Lock()
	defer em.extensionsMu.Unlock()

	if !em.started {
		return nil
	}

	em.started = false
	close(em.shutdownC)

	// Stop all extensions
	for name, extension := range em.extensions {
		if err := extension.Stop(ctx); err != nil {
			em.logger.Warn("failed to stop extension",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	// Wait for health check routine to finish
	em.wg.Wait()

	if em.logger != nil {
		em.logger.Info("extension manager stopped")
	}

	return nil
}

// HealthCheck performs health check on all extensions
func (em *ExtensionManager) HealthCheck(ctx context.Context) error {
	em.extensionsMu.RLock()
	defer em.extensionsMu.RUnlock()

	if !em.started {
		return fmt.Errorf("extension manager not started")
	}

	var errors []error
	for name, extension := range em.extensions {
		if err := extension.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("extension %s: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("health check failed: %v", errors)
	}

	return nil
}

// Helper methods

func (em *ExtensionManager) checkDependencies(extension Extension) error {
	dependencies := extension.Dependencies()

	for _, dep := range dependencies {
		_, exists := em.extensions[dep]

		if !exists {
			return fmt.Errorf("dependency %s not found", dep)
		}
	}

	return nil
}

func (em *ExtensionManager) healthCheckRoutine() {
	defer em.wg.Done()

	// Use default interval if not set
	interval := em.config.HealthCheckInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-em.shutdownC:
			return
		case <-ticker.C:
			em.performHealthChecks()
		}
	}
}

func (em *ExtensionManager) performHealthChecks() {
	em.extensionsMu.RLock()
	extensions := make([]Extension, 0, len(em.extensions))
	for _, ext := range em.extensions {
		extensions = append(extensions, ext)
	}
	em.extensionsMu.RUnlock()

	for _, extension := range extensions {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := extension.HealthCheck(ctx); err != nil {
			em.logger.Warn("extension health check failed",
				logger.String("name", extension.Name()),
				logger.Error(err),
			)
		}
		cancel()
	}
}

package extensions

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ExtensionRegistry manages extension discovery and dynamic loading
type ExtensionRegistry struct {
	managers   map[string]*ExtensionManager
	managersMu sync.RWMutex
	config     RegistryConfig
	logger     common.Logger
	metrics    common.Metrics
	discovery  ExtensionDiscovery
	started    bool
	shutdownC  chan struct{}
	wg         sync.WaitGroup
}

// RegistryConfig contains configuration for the extension registry
type RegistryConfig struct {
	DiscoveryInterval time.Duration  `yaml:"discovery_interval" default:"60s"`
	AutoLoad          bool           `yaml:"auto_load" default:"true"`
	LoadTimeout       time.Duration  `yaml:"load_timeout" default:"30s"`
	Logger            common.Logger  `yaml:"-"`
	Metrics           common.Metrics `yaml:"-"`
}

// ExtensionDiscovery handles discovery of available extensions
type ExtensionDiscovery interface {
	Discover(ctx context.Context) ([]ExtensionDescriptor, error)
	GetDescriptor(name string) (ExtensionDescriptor, error)
	Watch(ctx context.Context, callback func(ExtensionDescriptor)) error
}

// ExtensionDescriptor describes a discoverable extension
type ExtensionDescriptor struct {
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	Path         string                 `json:"path"`
	Type         string                 `json:"type"`
	Dependencies []string               `json:"dependencies"`
	Capabilities []string               `json:"capabilities"`
	Config       map[string]interface{} `json:"config"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// NewExtensionRegistry creates a new extension registry
func NewExtensionRegistry(config RegistryConfig) *ExtensionRegistry {
	return &ExtensionRegistry{
		managers:  make(map[string]*ExtensionManager),
		config:    config,
		logger:    config.Logger,
		metrics:   config.Metrics,
		shutdownC: make(chan struct{}),
	}
}

// SetDiscovery sets the extension discovery mechanism
func (er *ExtensionRegistry) SetDiscovery(discovery ExtensionDiscovery) {
	er.discovery = discovery
}

// RegisterManager registers an extension manager
func (er *ExtensionRegistry) RegisterManager(name string, manager *ExtensionManager) error {
	er.managersMu.Lock()
	defer er.managersMu.Unlock()

	if _, exists := er.managers[name]; exists {
		return fmt.Errorf("extension manager %s already registered", name)
	}

	er.managers[name] = manager

	if er.logger != nil {
		er.logger.Info("extension manager registered", logger.String("name", name))
	}

	return nil
}

// UnregisterManager unregisters an extension manager
func (er *ExtensionRegistry) UnregisterManager(name string) error {
	er.managersMu.Lock()
	defer er.managersMu.Unlock()

	manager, exists := er.managers[name]
	if !exists {
		return fmt.Errorf("extension manager %s not found", name)
	}

	// Stop the manager if it's running
	if er.started {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := manager.Stop(ctx); err != nil {
			er.logger.Warn("failed to stop extension manager during unregistration",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	delete(er.managers, name)

	if er.logger != nil {
		er.logger.Info("extension manager unregistered", logger.String("name", name))
	}

	return nil
}

// GetManager returns an extension manager by name
func (er *ExtensionRegistry) GetManager(name string) (*ExtensionManager, error) {
	er.managersMu.RLock()
	defer er.managersMu.RUnlock()

	manager, exists := er.managers[name]
	if !exists {
		return nil, fmt.Errorf("extension manager %s not found", name)
	}

	return manager, nil
}

// ListManagers returns all registered extension managers
func (er *ExtensionRegistry) ListManagers() []string {
	er.managersMu.RLock()
	defer er.managersMu.RUnlock()

	managers := make([]string, 0, len(er.managers))
	for name := range er.managers {
		managers = append(managers, name)
	}

	return managers
}

// DiscoverExtensions discovers available extensions
func (er *ExtensionRegistry) DiscoverExtensions(ctx context.Context) ([]ExtensionDescriptor, error) {
	if er.discovery == nil {
		return nil, fmt.Errorf("extension discovery not configured")
	}

	return er.discovery.Discover(ctx)
}

// LoadExtension loads an extension by descriptor
func (er *ExtensionRegistry) LoadExtension(ctx context.Context, descriptor ExtensionDescriptor) error {
	// Find appropriate manager for the extension type
	manager, err := er.findManagerForType(descriptor.Type)
	if err != nil {
		return fmt.Errorf("no manager found for extension type %s: %w", descriptor.Type, err)
	}

	// Create extension instance from descriptor
	extension, err := er.createExtensionFromDescriptor(descriptor)
	if err != nil {
		return fmt.Errorf("failed to create extension from descriptor: %w", err)
	}

	// Register the extension with the manager
	if err := manager.RegisterExtension(extension); err != nil {
		return fmt.Errorf("failed to register extension: %w", err)
	}

	// Load the extension if auto-load is enabled
	if er.config.AutoLoad {
		if err := manager.LoadExtension(ctx, descriptor.Name); err != nil {
			return fmt.Errorf("failed to load extension: %w", err)
		}
	}

	if er.logger != nil {
		er.logger.Info("extension loaded from discovery",
			logger.String("name", descriptor.Name),
			logger.String("version", descriptor.Version),
			logger.String("type", descriptor.Type),
		)
	}

	return nil
}

// Start starts the extension registry
func (er *ExtensionRegistry) Start(ctx context.Context) error {
	er.managersMu.Lock()
	defer er.managersMu.Unlock()

	if er.started {
		return fmt.Errorf("extension registry already started")
	}

	er.started = true

	// Start all registered managers
	for name, manager := range er.managers {
		if err := manager.Start(ctx); err != nil {
			er.logger.Error("failed to start extension manager",
				logger.String("name", name),
				logger.Error(err),
			)
			continue
		}
	}

	// Start discovery routine if discovery is configured
	if er.discovery != nil {
		er.wg.Add(1)
		go er.discoveryRoutine()
	}

	if er.logger != nil {
		er.logger.Info("extension registry started")
	}

	return nil
}

// Stop stops the extension registry
func (er *ExtensionRegistry) Stop(ctx context.Context) error {
	er.managersMu.Lock()
	defer er.managersMu.Unlock()

	if !er.started {
		return nil
	}

	er.started = false
	close(er.shutdownC)

	// Stop all managers
	for name, manager := range er.managers {
		if err := manager.Stop(ctx); err != nil {
			er.logger.Warn("failed to stop extension manager",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	// Wait for discovery routine to finish
	er.wg.Wait()

	if er.logger != nil {
		er.logger.Info("extension registry stopped")
	}

	return nil
}

// HealthCheck performs health check on all managers
func (er *ExtensionRegistry) HealthCheck(ctx context.Context) error {
	er.managersMu.RLock()
	defer er.managersMu.RUnlock()

	var errors []error
	for name, manager := range er.managers {
		if err := manager.HealthCheck(ctx); err != nil {
			errors = append(errors, fmt.Errorf("manager %s: %w", name, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("health check failed: %v", errors)
	}

	return nil
}

// Helper methods

func (er *ExtensionRegistry) findManagerForType(extensionType string) (*ExtensionManager, error) {
	er.managersMu.RLock()
	defer er.managersMu.RUnlock()

	// For now, use the first available manager
	// In a more sophisticated implementation, this would be based on extension type
	for _, manager := range er.managers {
		return manager, nil
	}

	return nil, fmt.Errorf("no extension managers available")
}

func (er *ExtensionRegistry) createExtensionFromDescriptor(descriptor ExtensionDescriptor) (Extension, error) {
	// This is a placeholder implementation
	// In a real implementation, this would dynamically load the extension
	// based on the descriptor's path and type

	return &DynamicExtension{
		descriptor: descriptor,
		logger:     er.logger,
		metrics:    er.metrics,
	}, nil
}

func (er *ExtensionRegistry) discoveryRoutine() {
	defer er.wg.Done()

	ticker := time.NewTicker(er.config.DiscoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-er.shutdownC:
			return
		case <-ticker.C:
			er.performDiscovery()
		}
	}
}

func (er *ExtensionRegistry) performDiscovery() {
	ctx, cancel := context.WithTimeout(context.Background(), er.config.LoadTimeout)
	defer cancel()

	descriptors, err := er.discovery.Discover(ctx)
	if err != nil {
		er.logger.Error("extension discovery failed", logger.Error(err))
		return
	}

	for _, descriptor := range descriptors {
		// Check if extension is already loaded
		er.managersMu.RLock()
		alreadyLoaded := false
		for _, manager := range er.managers {
			if _, err := manager.GetExtension(descriptor.Name); err == nil {
				alreadyLoaded = true
				break
			}
		}
		er.managersMu.RUnlock()

		if !alreadyLoaded {
			if err := er.LoadExtension(ctx, descriptor); err != nil {
				er.logger.Error("failed to load discovered extension",
					logger.String("name", descriptor.Name),
					logger.Error(err),
				)
			}
		}
	}
}

// DynamicExtension is a placeholder implementation for dynamically loaded extensions
type DynamicExtension struct {
	descriptor ExtensionDescriptor
	logger     common.Logger
	metrics    common.Metrics
	status     ExtensionStatus
	startedAt  time.Time
}

func (de *DynamicExtension) Name() string {
	return de.descriptor.Name
}

func (de *DynamicExtension) Version() string {
	return de.descriptor.Version
}

func (de *DynamicExtension) Description() string {
	return de.descriptor.Description
}

func (de *DynamicExtension) Dependencies() []string {
	return de.descriptor.Dependencies
}

func (de *DynamicExtension) Initialize(ctx context.Context, config ExtensionConfig) error {
	de.status = ExtensionStatusLoaded
	return nil
}

func (de *DynamicExtension) Start(ctx context.Context) error {
	de.status = ExtensionStatusRunning
	de.startedAt = time.Now()
	return nil
}

func (de *DynamicExtension) Stop(ctx context.Context) error {
	de.status = ExtensionStatusStopped
	return nil
}

func (de *DynamicExtension) HealthCheck(ctx context.Context) error {
	if de.status != ExtensionStatusRunning {
		return fmt.Errorf("extension not running")
	}
	return nil
}

func (de *DynamicExtension) GetCapabilities() []string {
	return de.descriptor.Capabilities
}

func (de *DynamicExtension) IsCapabilitySupported(capability string) bool {
	for _, cap := range de.descriptor.Capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

func (de *DynamicExtension) GetConfig() interface{} {
	return de.descriptor.Config
}

func (de *DynamicExtension) UpdateConfig(config interface{}) error {
	// Update the descriptor's config
	if configMap, ok := config.(map[string]interface{}); ok {
		de.descriptor.Config = configMap
	}
	return nil
}

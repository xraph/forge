package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/logger"
)

// manager implements the Manager interface
type manager struct {
	mu        sync.RWMutex
	plugins   map[string]Plugin
	config    ManagerConfig
	container core.Container
	loader    Loader
	registry  Registry
	eventBus  EventBus
	sandbox   Sandbox
	logger    logger.Logger

	// Plugin state
	enabled     map[string]bool
	initialized map[string]bool
	started     map[string]bool
	status      map[string]PluginStatus
	lastError   map[string]error

	// Event callbacks
	onEnabledCallbacks  []func(Plugin)
	onDisabledCallbacks []func(Plugin)
	onErrorCallbacks    []func(Plugin, error)

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

// NewManager creates a new plugin manager
func NewManager(config ManagerConfig) Manager {
	ctx, cancel := context.WithCancel(context.Background())

	mgr := &manager{
		plugins:     make(map[string]Plugin),
		config:      config,
		enabled:     make(map[string]bool),
		initialized: make(map[string]bool),
		started:     make(map[string]bool),
		status:      make(map[string]PluginStatus),
		lastError:   make(map[string]error),
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger.GetGlobalLogger().Named("plugin-manager"),
	}

	// Initialize components
	mgr.loader = NewLoader(config.Loader)
	mgr.registry = NewRegistry(config.Registry)
	mgr.eventBus = NewEventBus()

	if config.EnableSandbox {
		mgr.sandbox = NewSandbox()
	}

	return mgr
}

// Register registers a plugin
func (m *manager) Register(plugin Plugin) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := plugin.Name()
	if name == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}

	// Check if plugin already exists
	if _, exists := m.plugins[name]; exists {
		return fmt.Errorf("%w: %s", ErrPluginExists, name)
	}

	// Validate plugin
	if err := m.validatePlugin(plugin); err != nil {
		return fmt.Errorf("plugin validation failed: %w", err)
	}

	// Check dependencies
	if err := m.checkDependencies(plugin); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}

	// Register plugin
	m.plugins[name] = plugin
	m.enabled[name] = false
	m.initialized[name] = false
	m.started[name] = false
	m.status[name] = PluginStatus{
		Enabled:     false,
		Initialized: false,
		Started:     false,
		Health:      string(HealthStatusUnknown),
	}

	m.logger.Info("Plugin registered successfully",
		logger.String("name", name),
		logger.String("version", plugin.Version()),
		logger.String("author", plugin.Author()),
	)

	// Publish event
	m.publishEvent(Event{
		Type:      "plugin.registered",
		Plugin:    name,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"version": plugin.Version(),
			"author":  plugin.Author(),
		},
	})

	return nil
}

// Unregister unregisters a plugin
func (m *manager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	// Stop and cleanup plugin if running
	if m.started[name] {
		if err := m.stopPlugin(plugin); err != nil {
			m.logger.Warn("Error stopping plugin during unregister",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	// Remove from all maps
	delete(m.plugins, name)
	delete(m.enabled, name)
	delete(m.initialized, name)
	delete(m.started, name)
	delete(m.status, name)
	delete(m.lastError, name)

	m.logger.Info("Plugin unregistered",
		logger.String("name", name),
	)

	// Publish event
	m.publishEvent(Event{
		Type:      "plugin.unregistered",
		Plugin:    name,
		Timestamp: time.Now(),
	})

	return nil
}

// Enable enables a plugin
func (m *manager) Enable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	if m.enabled[name] {
		return nil // Already enabled
	}

	// Check dependencies again
	if err := m.checkDependencies(plugin); err != nil {
		return fmt.Errorf("dependency check failed: %w", err)
	}

	// Initialize if needed
	if !m.initialized[name] && m.container != nil {
		if err := m.initializePlugin(plugin); err != nil {
			return fmt.Errorf("failed to initialize plugin: %w", err)
		}
	}

	m.enabled[name] = true
	m.updatePluginStatus(name)

	m.logger.Info("Plugin enabled",
		logger.String("name", name),
	)

	// Notify callbacks
	for _, callback := range m.onEnabledCallbacks {
		go callback(plugin)
	}

	// Publish event
	m.publishEvent(Event{
		Type:      "plugin.enabled",
		Plugin:    name,
		Timestamp: time.Now(),
	})

	return nil
}

// Disable disables a plugin
func (m *manager) Disable(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	if !m.enabled[name] {
		return nil // Already disabled
	}

	// Stop plugin if running
	if m.started[name] {
		if err := m.stopPlugin(plugin); err != nil {
			m.logger.Warn("Error stopping plugin during disable",
				logger.String("name", name),
				logger.Error(err),
			)
		}
	}

	m.enabled[name] = false
	m.updatePluginStatus(name)

	m.logger.Info("Plugin disabled",
		logger.String("name", name),
	)

	// Notify callbacks
	for _, callback := range m.onDisabledCallbacks {
		go callback(plugin)
	}

	// Publish event
	m.publishEvent(Event{
		Type:      "plugin.disabled",
		Plugin:    name,
		Timestamp: time.Now(),
	})

	return nil
}

// Reload reloads a plugin
func (m *manager) Reload(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	wasEnabled := m.enabled[name]
	wasStarted := m.started[name]

	// Stop and disable
	if wasStarted {
		if err := m.stopPlugin(plugin); err != nil {
			return fmt.Errorf("failed to stop plugin for reload: %w", err)
		}
	}

	// Re-initialize
	if m.initialized[name] {
		m.initialized[name] = false
		if err := m.initializePlugin(plugin); err != nil {
			return fmt.Errorf("failed to re-initialize plugin: %w", err)
		}
	}

	// Restore previous state
	if wasEnabled {
		m.enabled[name] = true
	}
	if wasStarted {
		if err := m.startPlugin(plugin); err != nil {
			return fmt.Errorf("failed to restart plugin: %w", err)
		}
	}

	m.updatePluginStatus(name)

	m.logger.Info("Plugin reloaded",
		logger.String("name", name),
	)

	// Publish event
	m.publishEvent(Event{
		Type:      "plugin.reloaded",
		Plugin:    name,
		Timestamp: time.Now(),
	})

	return nil
}

// LoadFromDirectory loads plugins from a directory
func (m *manager) LoadFromDirectory(directory string) error {
	if directory == "" {
		return fmt.Errorf("directory path cannot be empty")
	}

	if _, err := os.Stat(directory); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", directory)
	}

	var loadErrors []error

	err := filepath.Walk(directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check supported formats
		ext := filepath.Ext(path)
		supported := false
		for _, format := range m.loader.SupportedFormats() {
			if ext == format {
				supported = true
				break
			}
		}

		if !supported {
			return nil
		}

		// Load plugin
		plugin, err := m.loader.LoadPlugin(path)
		if err != nil {
			m.logger.Warn("Failed to load plugin",
				logger.String("path", path),
				logger.Error(err),
			)
			loadErrors = append(loadErrors, fmt.Errorf("failed to load %s: %w", path, err))
			return nil
		}

		// Register plugin
		if err := m.Register(plugin); err != nil {
			m.logger.Warn("Failed to register plugin",
				logger.String("path", path),
				logger.String("name", plugin.Name()),
				logger.Error(err),
			)
			loadErrors = append(loadErrors, fmt.Errorf("failed to register %s: %w", plugin.Name(), err))
			return nil
		}

		// Auto-enable if configured
		if m.config.AutoLoad {
			if err := m.Enable(plugin.Name()); err != nil {
				m.logger.Warn("Failed to enable plugin",
					logger.String("name", plugin.Name()),
					logger.Error(err),
				)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("error walking directory: %w", err)
	}

	if len(loadErrors) > 0 {
		return fmt.Errorf("failed to load %d plugins: %v", len(loadErrors), loadErrors)
	}

	m.logger.Info("Successfully loaded plugins from directory",
		logger.String("directory", directory),
	)

	return nil
}

// LoadFromFile loads a plugin from a file
func (m *manager) LoadFromFile(path string) error {
	plugin, err := m.loader.LoadPlugin(path)
	if err != nil {
		return fmt.Errorf("failed to load plugin from %s: %w", path, err)
	}

	if err := m.Register(plugin); err != nil {
		return fmt.Errorf("failed to register plugin: %w", err)
	}

	if m.config.AutoLoad {
		if err := m.Enable(plugin.Name()); err != nil {
			m.logger.Warn("Failed to auto-enable plugin",
				logger.String("name", plugin.Name()),
				logger.Error(err),
			)
		}
	}

	return nil
}

// Get gets a plugin by name
func (m *manager) Get(name string) (Plugin, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	return plugin, nil
}

// List returns all plugins
func (m *manager) List() []Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugins := make([]Plugin, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		plugins = append(plugins, plugin)
	}

	// Sort by name for consistent ordering
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Name() < plugins[j].Name()
	})

	return plugins
}

// ListEnabled returns all enabled plugins
func (m *manager) ListEnabled() []Plugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var plugins []Plugin
	for name, plugin := range m.plugins {
		if m.enabled[name] {
			plugins = append(plugins, plugin)
		}
	}

	// Sort by name for consistent ordering
	sort.Slice(plugins, func(i, j int) bool {
		return plugins[i].Name() < plugins[j].Name()
	})

	return plugins
}

// InitializeAll initializes all enabled plugins
func (m *manager) InitializeAll(container core.Container) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.container = container

	var initErrors []error

	// Sort plugins by dependencies
	sortedPlugins := m.sortPluginsByDependencies()

	for _, name := range sortedPlugins {
		plugin := m.plugins[name]
		if !m.enabled[name] {
			continue
		}

		if err := m.initializePlugin(plugin); err != nil {
			m.logger.Error("Failed to initialize plugin",
				logger.String("name", name),
				logger.Error(err),
			)
			initErrors = append(initErrors, fmt.Errorf("failed to initialize %s: %w", name, err))
			continue
		}
	}

	if len(initErrors) > 0 {
		return fmt.Errorf("failed to initialize %d plugins: %v", len(initErrors), initErrors)
	}

	m.logger.Info("All enabled plugins initialized successfully")
	return nil
}

// StartAll starts all initialized plugins
func (m *manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var startErrors []error

	// Sort plugins by dependencies
	sortedPlugins := m.sortPluginsByDependencies()

	for _, name := range sortedPlugins {
		plugin := m.plugins[name]
		if !m.enabled[name] || !m.initialized[name] {
			continue
		}

		if err := m.startPlugin(plugin); err != nil {
			m.logger.Error("Failed to start plugin",
				logger.String("name", name),
				logger.Error(err),
			)
			startErrors = append(startErrors, fmt.Errorf("failed to start %s: %w", name, err))
			continue
		}
	}

	if len(startErrors) > 0 {
		return fmt.Errorf("failed to start %d plugins: %v", len(startErrors), startErrors)
	}

	m.logger.Info("All enabled plugins started successfully")
	return nil
}

// StopAll stops all running plugins
func (m *manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var stopErrors []error

	// Stop in reverse dependency order
	sortedPlugins := m.sortPluginsByDependencies()
	for i := len(sortedPlugins) - 1; i >= 0; i-- {
		name := sortedPlugins[i]
		plugin := m.plugins[name]

		if !m.started[name] {
			continue
		}

		if err := m.stopPlugin(plugin); err != nil {
			m.logger.Error("Failed to stop plugin",
				logger.String("name", name),
				logger.Error(err),
			)
			stopErrors = append(stopErrors, fmt.Errorf("failed to stop %s: %w", name, err))
			continue
		}
	}

	if len(stopErrors) > 0 {
		return fmt.Errorf("failed to stop %d plugins: %v", len(stopErrors), stopErrors)
	}

	m.logger.Info("All plugins stopped successfully")
	return nil
}

// Status returns the status of all plugins
func (m *manager) Status() map[string]PluginStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]PluginStatus)
	for name, s := range m.status {
		status[name] = s
	}

	return status
}

// Health checks the health of all plugins
func (m *manager) Health(ctx context.Context) map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]error)

	for name, plugin := range m.plugins {
		if !m.enabled[name] || !m.started[name] {
			continue
		}

		// Check plugin health
		if healthChecker, ok := plugin.(interface{ Health(context.Context) error }); ok {
			health[name] = healthChecker.Health(ctx)
		} else {
			health[name] = nil // No health check available, assume healthy
		}
	}

	return health
}

// Configuration management

// SetConfig sets configuration for a plugin
func (m *manager) SetConfig(name string, config map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return fmt.Errorf("%w: %s", ErrPluginNotFound, name)
	}

	// Validate configuration
	if err := plugin.ValidateConfig(config); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Store configuration (implementation would persist this)
	// For now, just update the plugin status
	m.updatePluginStatus(name)

	m.logger.Info("Plugin configuration updated",
		logger.String("name", name),
	)

	return nil
}

// GetConfig gets configuration for a plugin
func (m *manager) GetConfig(name string) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	plugin, exists := m.plugins[name]
	if !exists {
		return nil
	}

	// Return default config for now
	return plugin.DefaultConfig()
}

// Event handling

// OnPluginEnabled registers a callback for plugin enabled events
func (m *manager) OnPluginEnabled(callback func(Plugin)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEnabledCallbacks = append(m.onEnabledCallbacks, callback)
}

// OnPluginDisabled registers a callback for plugin disabled events
func (m *manager) OnPluginDisabled(callback func(Plugin)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onDisabledCallbacks = append(m.onDisabledCallbacks, callback)
}

// OnPluginError registers a callback for plugin error events
func (m *manager) OnPluginError(callback func(Plugin, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onErrorCallbacks = append(m.onErrorCallbacks, callback)
}

// Private helper methods

func (m *manager) validatePlugin(plugin Plugin) error {
	if plugin.Name() == "" {
		return fmt.Errorf("plugin name cannot be empty")
	}

	if plugin.Version() == "" {
		return fmt.Errorf("plugin version cannot be empty")
	}

	// Use loader for validation if available
	if m.loader != nil {
		return m.loader.ValidatePlugin(plugin)
	}

	return nil
}

func (m *manager) checkDependencies(plugin Plugin) error {
	dependencies := plugin.Dependencies()

	for _, dep := range dependencies {
		if dep.Optional {
			continue
		}

		// Check if dependency is available
		if _, exists := m.plugins[dep.Name]; !exists {
			return fmt.Errorf("%w: %s", ErrDependencyMissing, dep.Name)
		}

		// Check if dependency is enabled
		if !m.enabled[dep.Name] {
			return fmt.Errorf("dependency not enabled: %s", dep.Name)
		}
	}

	return nil
}

func (m *manager) initializePlugin(plugin Plugin) error {
	start := time.Now()

	var err error
	if m.sandbox != nil {
		err = m.sandbox.Execute(plugin, func() error {
			return plugin.Initialize(m.container)
		})
	} else {
		err = plugin.Initialize(m.container)
	}

	if err != nil {
		m.lastError[plugin.Name()] = err
		m.notifyError(plugin, err)
		return err
	}

	m.initialized[plugin.Name()] = true
	m.updatePluginStatus(plugin.Name())

	loadTime := time.Since(start)
	m.logger.Info("Plugin initialized",
		logger.String("name", plugin.Name()),
		logger.Duration("load_time", loadTime),
	)

	return nil
}

func (m *manager) startPlugin(plugin Plugin) error {
	start := time.Now()

	var err error
	if m.sandbox != nil {
		err = m.sandbox.Execute(plugin, func() error {
			return plugin.Start(m.ctx)
		})
	} else {
		err = plugin.Start(m.ctx)
	}

	if err != nil {
		m.lastError[plugin.Name()] = err
		m.notifyError(plugin, err)
		return err
	}

	m.started[plugin.Name()] = true
	m.updatePluginStatus(plugin.Name())

	startTime := time.Since(start)
	m.logger.Info("Plugin started",
		logger.String("name", plugin.Name()),
		logger.Duration("start_time", startTime),
	)

	return nil
}

func (m *manager) stopPlugin(plugin Plugin) error {
	var err error
	if m.sandbox != nil {
		err = m.sandbox.Execute(plugin, func() error {
			return plugin.Stop(m.ctx)
		})
	} else {
		err = plugin.Stop(m.ctx)
	}

	if err != nil {
		m.lastError[plugin.Name()] = err
		m.notifyError(plugin, err)
		return err
	}

	m.started[plugin.Name()] = false
	m.updatePluginStatus(plugin.Name())

	m.logger.Info("Plugin stopped",
		logger.String("name", plugin.Name()),
	)

	return nil
}

func (m *manager) updatePluginStatus(name string) {
	plugin := m.plugins[name]

	status := PluginStatus{
		Enabled:     m.enabled[name],
		Initialized: m.initialized[name],
		Started:     m.started[name],
		Health:      string(HealthStatusUnknown),
	}

	if err := m.lastError[name]; err != nil {
		status.Error = err.Error()
		status.Health = string(HealthStatusDown)
	} else if status.Started {
		status.Health = string(HealthStatusUp)
	}

	// Add metadata from plugin
	if plugin.IsEnabled() != status.Enabled {
		// Plugin state mismatch, update accordingly
	}

	m.status[name] = status
}

func (m *manager) sortPluginsByDependencies() []string {
	// Simple topological sort for plugin dependencies
	var sorted []string
	visited := make(map[string]bool)
	visiting := make(map[string]bool)

	var visit func(name string) error
	visit = func(name string) error {
		if visiting[name] {
			return fmt.Errorf("circular dependency detected: %s", name)
		}
		if visited[name] {
			return nil
		}

		visiting[name] = true

		plugin, exists := m.plugins[name]
		if !exists {
			return nil
		}

		// Visit dependencies first
		for _, dep := range plugin.Dependencies() {
			if err := visit(dep.Name); err != nil {
				return err
			}
		}

		visiting[name] = false
		visited[name] = true
		sorted = append(sorted, name)

		return nil
	}

	// Visit all plugins
	for name := range m.plugins {
		if err := visit(name); err != nil {
			m.logger.Warn("Error sorting plugins by dependencies",
				logger.Error(err),
			)
			// Fallback to simple ordering
			sorted = []string{}
			for name := range m.plugins {
				sorted = append(sorted, name)
			}
			break
		}
	}

	return sorted
}

func (m *manager) publishEvent(event Event) {
	if m.eventBus != nil {
		go func() {
			if err := m.eventBus.PublishAsync(m.ctx, event); err != nil {
				m.logger.Warn("Failed to publish plugin event",
					logger.String("type", event.Type),
					logger.String("plugin", event.Plugin),
					logger.Error(err),
				)
			}
		}()
	}
}

func (m *manager) notifyError(plugin Plugin, err error) {
	for _, callback := range m.onErrorCallbacks {
		go callback(plugin, err)
	}

	m.publishEvent(Event{
		Type:      "plugin.error",
		Plugin:    plugin.Name(),
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"error": err.Error(),
		},
	})
}

// HealthStatusLevel represents health status levels (defined in observability interfaces)
type HealthStatusLevel string

const (
	HealthStatusUp      HealthStatusLevel = "UP"
	HealthStatusDown    HealthStatusLevel = "DOWN"
	HealthStatusWarning HealthStatusLevel = "WARNING"
	HealthStatusUnknown HealthStatusLevel = "UNKNOWN"
)

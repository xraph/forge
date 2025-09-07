package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/plugins/hooks"
	"github.com/xraph/forge/pkg/plugins/loader"
	"github.com/xraph/forge/pkg/plugins/security"
	"github.com/xraph/forge/pkg/plugins/store"
)

// PluginManager manages plugins for the framework
type PluginManager interface {
	common.Service // Integrates with Phase 1 service lifecycle
	LoadPlugin(ctx context.Context, source PluginSource) (Plugin, error)
	UnloadPlugin(ctx context.Context, pluginID string) error
	GetPlugin(pluginID string) (Plugin, error)
	GetPlugins() []Plugin
	GetPluginStore() store.PluginStore
	InstallPlugin(ctx context.Context, source PluginSource) error
	UpdatePlugin(ctx context.Context, pluginID string) error
	EnablePlugin(ctx context.Context, pluginID string) error
	DisablePlugin(ctx context.Context, pluginID string) error
	ValidatePlugin(ctx context.Context, plugin Plugin) error
	GetStats() PluginManagerStats
}

// PluginManagerImpl implements the PluginManager interface
type PluginManagerImpl struct {
	plugins     map[string]*PluginEntry
	pluginStore store.PluginStore
	loader      loader.PluginLoader
	registry    PluginRegistry
	sandbox     security.PluginSandbox
	hookSystem  hooks.HookSystem
	security    security.PermissionManager
	validator   *store.PluginValidator
	container   common.Container
	logger      common.Logger
	metrics     common.Metrics
	config      common.ConfigManager
	mu          sync.RWMutex
	started     bool
	startTime   time.Time
	stats       PluginManagerStats
}

// PluginEntry represents a plugin entry in the manager
type PluginEntry struct {
	Plugin       Plugin
	State        PluginState
	LoadedAt     time.Time
	StartedAt    time.Time
	Config       interface{}
	Dependencies []string
	Dependents   []string
	Metrics      PluginMetrics
	LastError    error
	HealthScore  float64
	Sandbox      security.PluginSandbox
	Context      *PluginContext
	mu           sync.RWMutex
}

// PluginManagerStats contains statistics about the plugin manager
type PluginManagerStats struct {
	TotalPlugins       int                 `json:"total_plugins"`
	LoadedPlugins      int                 `json:"loaded_plugins"`
	ActivePlugins      int                 `json:"active_plugins"`
	FailedPlugins      int                 `json:"failed_plugins"`
	PluginsByType      map[PluginType]int  `json:"plugins_by_type"`
	PluginsByState     map[PluginState]int `json:"plugins_by_state"`
	TotalMemoryUsage   int64               `json:"total_memory_usage"`
	AverageCPUUsage    float64             `json:"average_cpu_usage"`
	AverageHealthScore float64             `json:"average_health_score"`
	LastUpdated        time.Time           `json:"last_updated"`
	Uptime             time.Duration       `json:"uptime"`
}

// PluginSource represents the source of a plugin
type PluginSource struct {
	Type     PluginSourceType       `json:"type"`
	Location string                 `json:"location"`
	Version  string                 `json:"version"`
	Config   map[string]interface{} `json:"config"`
	Metadata map[string]interface{} `json:"metadata"`
}

// PluginSourceType defines the type of plugin source
type PluginSourceType string

const (
	PluginSourceTypeFile        PluginSourceType = "file"
	PluginSourceTypeURL         PluginSourceType = "url"
	PluginSourceTypeMarketplace PluginSourceType = "marketplace"
	PluginSourceTypeGit         PluginSourceType = "git"
	PluginSourceTypeRegistry    PluginSourceType = "registry"
)

// NewPluginManager creates a new plugin manager
func NewPluginManager(container common.Container, logger common.Logger, metrics common.Metrics, config common.ConfigManager) PluginManager {
	pm := &PluginManagerImpl{
		plugins:   make(map[string]*PluginEntry),
		container: container,
		logger:    logger,
		metrics:   metrics,
		config:    config,
		stats:     PluginManagerStats{},
	}

	// Initialize components
	pm.pluginStore = store.NewPluginStore(logger, metrics)
	pm.loader = loader.NewPluginLoader(logger, metrics)
	pm.registry = store.NewPluginRegistry(logger, metrics)
	pm.sandbox = security.NewPluginSandbox(logger, metrics)
	pm.hookSystem = hooks.NewHookSystem(logger, metrics)
	pm.security = security.NewPermissionManager(logger, metrics)
	pm.validator = store.NewPluginValidator(logger, metrics)

	return pm
}

// Name returns the service name
func (pm *PluginManagerImpl) Name() string {
	return "plugin-manager"
}

// Dependencies returns the service dependencies
func (pm *PluginManagerImpl) Dependencies() []string {
	return []string{"config-manager", "logger", "metrics"}
}

// OnStart starts the plugin manager
func (pm *PluginManagerImpl) OnStart(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.started {
		return common.ErrServiceAlreadyExists("plugin-manager")
	}

	pm.startTime = time.Now()

	// Initialize plugin store
	if err := pm.pluginStore.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize plugin store: %w", err)
	}

	// Initialize security manager
	if err := pm.security.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize security manager: %w", err)
	}

	// Initialize hook system
	if err := pm.hookSystem.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize hook system: %w", err)
	}

	// Load auto-load plugins from configuration
	if err := pm.loadAutoLoadPlugins(ctx); err != nil {
		pm.logger.Warn("failed to load auto-load plugins", logger.Error(err))
	}

	pm.started = true

	pm.logger.Info("plugin manager started",
		logger.Int("total_plugins", len(pm.plugins)),
		logger.Duration("startup_time", time.Since(pm.startTime)),
	)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.plugins.manager_started").Inc()
	}

	return nil
}

// OnStop stops the plugin manager
func (pm *PluginManagerImpl) OnStop(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.started {
		return common.ErrServiceNotFound("plugin-manager")
	}

	// Stop all plugins in reverse dependency order
	if err := pm.stopAllPlugins(ctx); err != nil {
		pm.logger.Error("failed to stop all plugins", logger.Error(err))
	}

	// Stop hook system
	if err := pm.hookSystem.Stop(ctx); err != nil {
		pm.logger.Error("failed to stop hook system", logger.Error(err))
	}

	// Stop security manager
	if err := pm.security.Stop(ctx); err != nil {
		pm.logger.Error("failed to stop security manager", logger.Error(err))
	}

	// Stop plugin store
	if err := pm.pluginStore.Stop(ctx); err != nil {
		pm.logger.Error("failed to stop plugin store", logger.Error(err))
	}

	pm.started = false

	pm.logger.Info("plugin manager stopped")

	if pm.metrics != nil {
		pm.metrics.Counter("forge.plugins.manager_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs a health check
func (pm *PluginManagerImpl) OnHealthCheck(ctx context.Context) error {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if !pm.started {
		return common.ErrHealthCheckFailed("plugin-manager", fmt.Errorf("plugin manager not started"))
	}

	// Check plugin health
	failedPlugins := 0
	for _, entry := range pm.plugins {
		if entry.State == PluginStateError {
			failedPlugins++
		}
	}

	if failedPlugins > 0 {
		return common.ErrHealthCheckFailed("plugin-manager", fmt.Errorf("%d plugins in error state", failedPlugins))
	}

	return nil
}

// LoadPlugin loads a plugin from a source
func (pm *PluginManagerImpl) LoadPlugin(ctx context.Context, source PluginSource) (Plugin, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.started {
		return nil, common.ErrServiceNotFound("plugin-manager")
	}

	// Load the plugin using the loader
	plugin, err := pm.loader.LoadPlugin(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin: %w", err)
	}

	// Validate the plugin
	if err := pm.validator.ValidatePlugin(ctx, plugin); err != nil {
		return nil, fmt.Errorf("plugin validation failed: %w", err)
	}

	// Check if plugin already exists
	if _, exists := pm.plugins[plugin.ID()]; exists {
		return nil, common.ErrServiceAlreadyExists(plugin.ID())
	}

	// Validate dependencies
	if err := pm.validateDependencies(ctx, plugin); err != nil {
		return nil, fmt.Errorf("dependency validation failed: %w", err)
	}

	// Create sandbox for plugin
	sandbox, err := pm.sandbox.CreateSandbox(ctx, SandboxConfig{
		PluginID:         plugin.ID(),
		Isolated:         true,
		MaxMemory:        1024 * 1024 * 1024, // 1GB
		MaxCPU:           0.5,                // 50%
		NetworkAccess:    NetworkPolicy{Allowed: true},
		FileSystemAccess: FileSystemPolicy{ReadOnly: true},
		Timeout:          30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create sandbox: %w", err)
	}

	// Create plugin context
	pluginCtx := NewPluginContext(ctx, plugin, pm.container)

	// Create plugin entry
	entry := &PluginEntry{
		Plugin:       plugin,
		State:        PluginStateLoaded,
		LoadedAt:     time.Now(),
		Dependencies: pm.extractDependencies(plugin),
		Metrics:      PluginMetrics{},
		HealthScore:  1.0,
		Sandbox:      sandbox,
		Context:      pluginCtx,
	}

	// Initialize plugin in sandbox
	if err := pm.initializePlugin(ctx, entry); err != nil {
		sandbox.Destroy(ctx)
		return nil, fmt.Errorf("failed to initialize plugin: %w", err)
	}

	// Register plugin
	pm.plugins[plugin.ID()] = entry
	pm.registry.Register(plugin)

	// Update dependents
	pm.updateDependents(plugin.ID())

	// Update stats
	pm.updateStats()

	pm.logger.Info("plugin loaded",
		logger.String("plugin_id", plugin.ID()),
		logger.String("plugin_name", plugin.Name()),
		logger.String("plugin_version", plugin.Version()),
		logger.String("plugin_type", string(plugin.Type())),
	)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.plugins.loaded").Inc()
		pm.metrics.Gauge("forge.plugins.total").Set(float64(len(pm.plugins)))
	}

	return plugin, nil
}

// UnloadPlugin unloads a plugin
func (pm *PluginManagerImpl) UnloadPlugin(ctx context.Context, pluginID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry, exists := pm.plugins[pluginID]
	if !exists {
		return common.ErrServiceNotFound(pluginID)
	}

	// Check if plugin has dependents
	if len(entry.Dependents) > 0 {
		return fmt.Errorf("cannot unload plugin %s: has dependents %v", pluginID, entry.Dependents)
	}

	// Stop plugin if running
	if entry.State == PluginStateStarted {
		if err := pm.stopPlugin(ctx, entry); err != nil {
			pm.logger.Error("failed to stop plugin during unload", logger.Error(err))
		}
	}

	// Cleanup plugin
	if err := entry.Plugin.Cleanup(ctx); err != nil {
		pm.logger.Error("failed to cleanup plugin", logger.Error(err))
	}

	// Destroy sandbox
	if err := entry.Sandbox.Destroy(ctx); err != nil {
		pm.logger.Error("failed to destroy sandbox", logger.Error(err))
	}

	// Unregister plugin
	pm.registry.Unregister(pluginID)

	// Remove from plugins map
	delete(pm.plugins, pluginID)

	// Update dependents
	pm.removeDependents(pluginID)

	// Update stats
	pm.updateStats()

	pm.logger.Info("plugin unloaded",
		logger.String("plugin_id", pluginID),
	)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.plugins.unloaded").Inc()
		pm.metrics.Gauge("forge.plugins.total").Set(float64(len(pm.plugins)))
	}

	return nil
}

// GetPlugin returns a plugin by ID
func (pm *PluginManagerImpl) GetPlugin(pluginID string) (Plugin, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entry, exists := pm.plugins[pluginID]
	if !exists {
		return nil, common.ErrServiceNotFound(pluginID)
	}

	return entry.Plugin, nil
}

// GetPlugins returns all plugins
func (pm *PluginManagerImpl) GetPlugins() []Plugin {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	plugins := make([]Plugin, 0, len(pm.plugins))
	for _, entry := range pm.plugins {
		plugins = append(plugins, entry.Plugin)
	}

	return plugins
}

// GetPluginStore returns the plugin store
func (pm *PluginManagerImpl) GetPluginStore() PluginStore {
	return pm.pluginStore
}

// InstallPlugin installs a plugin from a source
func (pm *PluginManagerImpl) InstallPlugin(ctx context.Context, source PluginSource) error {
	// Download plugin package
	pkg, err := pm.pluginStore.Download(ctx, source.Location, source.Version)
	if err != nil {
		return fmt.Errorf("failed to download plugin: %w", err)
	}

	// Validate package integrity
	if err := pm.validatePackage(pkg); err != nil {
		return fmt.Errorf("package validation failed: %w", err)
	}

	// Security scan
	if err := pm.security.ScanPackage(ctx, pkg); err != nil {
		return fmt.Errorf("security scan failed: %w", err)
	}

	// Install plugin
	if err := pm.installPackage(ctx, pkg); err != nil {
		return fmt.Errorf("failed to install plugin: %w", err)
	}

	// Load plugin
	loadSource := PluginSource{
		Type:     PluginSourceTypeFile,
		Location: pm.getPluginPath(pkg.Info.ID),
		Version:  pkg.Info.Version,
		Config:   source.Config,
	}

	_, err = pm.LoadPlugin(ctx, loadSource)
	if err != nil {
		return fmt.Errorf("failed to load installed plugin: %w", err)
	}

	pm.logger.Info("plugin installed",
		logger.String("plugin_id", pkg.Info.ID),
		logger.String("plugin_name", pkg.Info.Name),
		logger.String("plugin_version", pkg.Info.Version),
	)

	return nil
}

// UpdatePlugin updates a plugin to a new version
func (pm *PluginManagerImpl) UpdatePlugin(ctx context.Context, pluginID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry, exists := pm.plugins[pluginID]
	if !exists {
		return common.ErrServiceNotFound(pluginID)
	}

	// Check for updates
	latestVersion, err := pm.pluginStore.GetLatestVersion(ctx, pluginID)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	if latestVersion == entry.Plugin.Version() {
		return nil // Already up to date
	}

	// Download new version
	pkg, err := pm.pluginStore.Download(ctx, pluginID, latestVersion)
	if err != nil {
		return fmt.Errorf("failed to download update: %w", err)
	}

	// Validate package
	if err := pm.validatePackage(pkg); err != nil {
		return fmt.Errorf("package validation failed: %w", err)
	}

	// Stop current plugin
	if err := pm.stopPlugin(ctx, entry); err != nil {
		return fmt.Errorf("failed to stop plugin for update: %w", err)
	}

	// Backup current plugin
	backupPath := pm.getPluginBackupPath(pluginID)
	if err := pm.backupPlugin(ctx, entry, backupPath); err != nil {
		pm.logger.Warn("failed to backup plugin", logger.Error(err))
	}

	// Install new version
	if err := pm.installPackage(ctx, pkg); err != nil {
		// Restore backup
		if restoreErr := pm.restorePlugin(ctx, entry, backupPath); restoreErr != nil {
			pm.logger.Error("failed to restore plugin backup", logger.Error(restoreErr))
		}
		return fmt.Errorf("failed to install update: %w", err)
	}

	// Reload plugin
	newSource := PluginSource{
		Type:     PluginSourceTypeFile,
		Location: pm.getPluginPath(pluginID),
		Version:  latestVersion,
		Config:   entry.Config,
	}

	// Unload old plugin
	pm.unloadPluginUnsafe(ctx, entry)

	// Load new plugin
	newPlugin, err := pm.LoadPlugin(ctx, newSource)
	if err != nil {
		// Restore backup
		if restoreErr := pm.restorePlugin(ctx, entry, backupPath); restoreErr != nil {
			pm.logger.Error("failed to restore plugin backup", logger.Error(restoreErr))
		}
		return fmt.Errorf("failed to load updated plugin: %w", err)
	}

	// Start new plugin
	if err := pm.startPlugin(ctx, pm.plugins[newPlugin.ID()]); err != nil {
		pm.logger.Error("failed to start updated plugin", logger.Error(err))
	}

	pm.logger.Info("plugin updated",
		logger.String("plugin_id", pluginID),
		logger.String("old_version", entry.Plugin.Version()),
		logger.String("new_version", latestVersion),
	)

	return nil
}

// EnablePlugin enables a plugin
func (pm *PluginManagerImpl) EnablePlugin(ctx context.Context, pluginID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry, exists := pm.plugins[pluginID]
	if !exists {
		return common.ErrServiceNotFound(pluginID)
	}

	if entry.State == PluginStateStarted {
		return nil // Already enabled
	}

	return pm.startPlugin(ctx, entry)
}

// DisablePlugin disables a plugin
func (pm *PluginManagerImpl) DisablePlugin(ctx context.Context, pluginID string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	entry, exists := pm.plugins[pluginID]
	if !exists {
		return common.ErrServiceNotFound(pluginID)
	}

	if entry.State != PluginStateStarted {
		return nil // Already disabled
	}

	return pm.stopPlugin(ctx, entry)
}

// ValidatePlugin validates a plugin
func (pm *PluginManagerImpl) ValidatePlugin(ctx context.Context, plugin Plugin) error {
	return pm.validator.ValidatePlugin(ctx, plugin)
}

// GetStats returns plugin manager statistics
func (pm *PluginManagerImpl) GetStats() PluginManagerStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pm.updateStats()
	return pm.stats
}

// Helper methods

func (pm *PluginManagerImpl) initializePlugin(ctx context.Context, entry *PluginEntry) error {
	// Initialize plugin in sandbox
	err := entry.Sandbox.Execute(ctx, entry.Plugin, PluginOperation{
		Type:       OperationTypeLoad,
		Target:     entry.Plugin.ID(),
		Parameters: map[string]interface{}{"container": pm.container},
		Timeout:    30 * time.Second,
	})
	if err != nil {
		return err
	}

	// Initialize plugin
	if err := entry.Plugin.Initialize(ctx, pm.container); err != nil {
		return err
	}

	entry.State = PluginStateInitialized
	return nil
}

func (pm *PluginManagerImpl) startPlugin(ctx context.Context, entry *PluginEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State == PluginStateStarted {
		return nil
	}

	// Start plugin
	if err := entry.Plugin.Start(ctx); err != nil {
		entry.State = PluginStateError
		entry.LastError = err
		return err
	}

	entry.State = PluginStateStarted
	entry.StartedAt = time.Now()

	pm.logger.Info("plugin started",
		logger.String("plugin_id", entry.Plugin.ID()),
	)

	return nil
}

func (pm *PluginManagerImpl) stopPlugin(ctx context.Context, entry *PluginEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State != PluginStateStarted {
		return nil
	}

	// Stop plugin
	if err := entry.Plugin.Stop(ctx); err != nil {
		entry.State = PluginStateError
		entry.LastError = err
		return err
	}

	entry.State = PluginStateStopped

	pm.logger.Info("plugin stopped",
		logger.String("plugin_id", entry.Plugin.ID()),
	)

	return nil
}

func (pm *PluginManagerImpl) stopAllPlugins(ctx context.Context) error {
	// Calculate stop order (reverse dependency order)
	stopOrder := pm.calculateStopOrder()

	for _, pluginID := range stopOrder {
		if entry, exists := pm.plugins[pluginID]; exists {
			if err := pm.stopPlugin(ctx, entry); err != nil {
				pm.logger.Error("failed to stop plugin",
					logger.String("plugin_id", pluginID),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (pm *PluginManagerImpl) calculateStopOrder() []string {
	// Simple reverse dependency order calculation
	// In a real implementation, this would use a proper topological sort
	var order []string
	visited := make(map[string]bool)

	var visit func(string)
	visit = func(pluginID string) {
		if visited[pluginID] {
			return
		}
		visited[pluginID] = true

		if entry, exists := pm.plugins[pluginID]; exists {
			for _, dep := range entry.Dependencies {
				visit(dep)
			}
		}

		order = append(order, pluginID)
	}

	for pluginID := range pm.plugins {
		visit(pluginID)
	}

	// Reverse order for stopping
	for i, j := 0, len(order)-1; i < j; i, j = i+1, j-1 {
		order[i], order[j] = order[j], order[i]
	}

	return order
}

func (pm *PluginManagerImpl) validateDependencies(ctx context.Context, plugin Plugin) error {
	for _, dep := range plugin.Dependencies() {
		if dep.Required {
			if dep.Type == "plugin" {
				if _, exists := pm.plugins[dep.Name]; !exists {
					return fmt.Errorf("required plugin dependency not found: %s", dep.Name)
				}
			} else if dep.Type == "service" {
				if !pm.container.HasNamed(dep.Name) {
					return fmt.Errorf("required service dependency not found: %s", dep.Name)
				}
			}
		}
	}
	return nil
}

func (pm *PluginManagerImpl) extractDependencies(plugin Plugin) []string {
	var deps []string
	for _, dep := range plugin.Dependencies() {
		if dep.Type == "plugin" {
			deps = append(deps, dep.Name)
		}
	}
	return deps
}

func (pm *PluginManagerImpl) updateDependents(pluginID string) {
	for _, entry := range pm.plugins {
		for _, dep := range entry.Dependencies {
			if dep == pluginID {
				entry.Dependents = append(entry.Dependents, pluginID)
			}
		}
	}
}

func (pm *PluginManagerImpl) removeDependents(pluginID string) {
	for _, entry := range pm.plugins {
		for i, dep := range entry.Dependents {
			if dep == pluginID {
				entry.Dependents = append(entry.Dependents[:i], entry.Dependents[i+1:]...)
				break
			}
		}
	}
}

func (pm *PluginManagerImpl) validatePackage(pkg PluginPackage) error {
	// Calculate checksum
	hasher := sha256.New()
	hasher.Write(pkg.Binary)
	checksum := hex.EncodeToString(hasher.Sum(nil))

	if checksum != pkg.Checksum {
		return fmt.Errorf("package checksum mismatch: expected %s, got %s", pkg.Checksum, checksum)
	}

	return nil
}

func (pm *PluginManagerImpl) installPackage(ctx context.Context, pkg PluginPackage) error {
	// This would implement actual plugin installation logic
	// For now, just a placeholder
	return nil
}

func (pm *PluginManagerImpl) getPluginPath(pluginID string) string {
	return fmt.Sprintf("plugins/%s", pluginID)
}

func (pm *PluginManagerImpl) getPluginBackupPath(pluginID string) string {
	return fmt.Sprintf("plugins/backup/%s", pluginID)
}

func (pm *PluginManagerImpl) backupPlugin(ctx context.Context, entry *PluginEntry, backupPath string) error {
	// Implement plugin backup logic
	return nil
}

func (pm *PluginManagerImpl) restorePlugin(ctx context.Context, entry *PluginEntry, backupPath string) error {
	// Implement plugin restore logic
	return nil
}

func (pm *PluginManagerImpl) unloadPluginUnsafe(ctx context.Context, entry *PluginEntry) {
	// Cleanup plugin without dependency checks
	entry.Plugin.Cleanup(ctx)
	entry.Sandbox.Destroy(ctx)
	delete(pm.plugins, entry.Plugin.ID())
}

func (pm *PluginManagerImpl) loadAutoLoadPlugins(ctx context.Context) error {
	// Load plugins marked for auto-loading from configuration
	autoLoadPlugins := pm.config.GetString("plugins.auto_load")
	if autoLoadPlugins == "" {
		return nil
	}

	// Parse auto-load configuration and load plugins
	// This would be implemented based on configuration format
	return nil
}

func (pm *PluginManagerImpl) updateStats() {
	pm.stats.TotalPlugins = len(pm.plugins)
	pm.stats.LastUpdated = time.Now()
	pm.stats.Uptime = time.Since(pm.startTime)

	// Reset counters
	pm.stats.LoadedPlugins = 0
	pm.stats.ActivePlugins = 0
	pm.stats.FailedPlugins = 0
	pm.stats.PluginsByType = make(map[PluginType]int)
	pm.stats.PluginsByState = make(map[PluginState]int)
	pm.stats.TotalMemoryUsage = 0
	totalCPU := 0.0
	totalHealth := 0.0
	count := 0

	for _, entry := range pm.plugins {
		pm.stats.PluginsByType[entry.Plugin.Type()]++
		pm.stats.PluginsByState[entry.State]++

		if entry.State == PluginStateLoaded || entry.State == PluginStateInitialized {
			pm.stats.LoadedPlugins++
		}
		if entry.State == PluginStateStarted {
			pm.stats.ActivePlugins++
		}
		if entry.State == PluginStateError {
			pm.stats.FailedPlugins++
		}

		pm.stats.TotalMemoryUsage += entry.Metrics.MemoryUsage
		totalCPU += entry.Metrics.CPUUsage
		totalHealth += entry.HealthScore
		count++
	}

	if count > 0 {
		pm.stats.AverageCPUUsage = totalCPU / float64(count)
		pm.stats.AverageHealthScore = totalHealth / float64(count)
	}
}

package plugins

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// PluginService implements the common.Service interface for plugin management
type PluginService struct {
	manager   PluginManager
	container common.Container
	logger    common.Logger
	metrics   common.Metrics
	config    common.ConfigManager
	startTime time.Time
	started   bool
}

// PluginServiceConfig contains configuration for the plugin service
type PluginServiceConfig struct {
	AutoLoadPlugins     []string          `yaml:"auto_load_plugins" json:"auto_load_plugins"`
	PluginDirectory     string            `yaml:"plugin_directory" json:"plugin_directory"`
	SandboxEnabled      bool              `yaml:"sandbox_enabled" json:"sandbox_enabled"`
	SecurityEnabled     bool              `yaml:"security_enabled" json:"security_enabled"`
	MarketplaceURL      string            `yaml:"marketplace_url" json:"marketplace_url"`
	MaxPlugins          int               `yaml:"max_plugins" json:"max_plugins"`
	MaxMemoryPerPlugin  int64             `yaml:"max_memory_per_plugin" json:"max_memory_per_plugin"`
	UpdateCheckInterval time.Duration     `yaml:"update_check_interval" json:"update_check_interval"`
	HealthCheckInterval time.Duration     `yaml:"health_check_interval" json:"health_check_interval"`
	Marketplace         MarketplaceConfig `yaml:"marketplace" json:"marketplace"`
	Security            SecurityConfig    `yaml:"security" json:"security"`
}

// MarketplaceConfig contains marketplace configuration
type MarketplaceConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	URL        string `yaml:"url" json:"url"`
	APIKey     string `yaml:"api_key" json:"api_key"`
	CacheDir   string `yaml:"cache_dir" json:"cache_dir"`
	AutoUpdate bool   `yaml:"auto_update" json:"auto_update"`
}

// SecurityConfig contains security configuration
type SecurityConfig struct {
	Enabled             bool     `yaml:"enabled" json:"enabled"`
	AllowedSources      []string `yaml:"allowed_sources" json:"allowed_sources"`
	BlockedSources      []string `yaml:"blocked_sources" json:"blocked_sources"`
	RequireSignature    bool     `yaml:"require_signature" json:"require_signature"`
	TrustedPublishers   []string `yaml:"trusted_publishers" json:"trusted_publishers"`
	MaxSandboxMemory    int64    `yaml:"max_sandbox_memory" json:"max_sandbox_memory"`
	MaxSandboxCPU       float64  `yaml:"max_sandbox_cpu" json:"max_sandbox_cpu"`
	NetworkIsolation    bool     `yaml:"network_isolation" json:"network_isolation"`
	FileSystemIsolation bool     `yaml:"filesystem_isolation" json:"filesystem_isolation"`
}

// NewPluginService creates a new plugin service
func NewPluginService(container common.Container, logger common.Logger, metrics common.Metrics, config common.ConfigManager) *PluginService {
	manager := NewPluginManager(container, logger, metrics, config)

	return &PluginService{
		manager:   manager,
		container: container,
		logger:    logger,
		metrics:   metrics,
		config:    config,
	}
}

// Name returns the service name
func (ps *PluginService) Name() string {
	return "plugin-service"
}

// Dependencies returns the service dependencies
func (ps *PluginService) Dependencies() []string {
	return []string{
		"config-manager",
		"logger",
		"metrics",
	}
}

// OnStart starts the plugin service
func (ps *PluginService) OnStart(ctx context.Context) error {
	if ps.started {
		return common.ErrServiceAlreadyExists(ps.Name())
	}

	ps.startTime = time.Now()

	// Load configuration
	config, err := ps.loadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load plugin service configuration: %w", err)
	}

	// Configure plugin manager
	if err := ps.configureManager(ctx, config); err != nil {
		return fmt.Errorf("failed to configure plugin manager: %w", err)
	}

	// Start plugin manager
	if err := ps.manager.OnStart(ctx); err != nil {
		return fmt.Errorf("failed to start plugin manager: %w", err)
	}

	// Load auto-load plugins
	if err := ps.loadAutoLoadPlugins(ctx, config); err != nil {
		ps.logger.Warn("failed to load auto-load plugins", logger.Error(err))
	}

	// Start background tasks
	go ps.startBackgroundTasks(ctx)

	ps.started = true

	ps.logger.Info("plugin service started",
		logger.Duration("startup_time", time.Since(ps.startTime)),
		logger.Int("auto_load_plugins", len(config.AutoLoadPlugins)),
		logger.Bool("sandbox_enabled", config.SandboxEnabled),
		logger.Bool("security_enabled", config.SecurityEnabled),
	)

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.service_started").Inc()
	}

	return nil
}

// OnStop stops the plugin service
func (ps *PluginService) OnStop(ctx context.Context) error {
	if !ps.started {
		return common.ErrServiceNotFound(ps.Name())
	}

	// Stop plugin manager
	if err := ps.manager.OnStop(ctx); err != nil {
		ps.logger.Error("failed to stop plugin manager", logger.Error(err))
	}

	ps.started = false

	ps.logger.Info("plugin service stopped",
		logger.Duration("uptime", time.Since(ps.startTime)),
	)

	if ps.metrics != nil {
		ps.metrics.Counter("forge.plugins.service_stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs a health check
func (ps *PluginService) OnHealthCheck(ctx context.Context) error {
	if !ps.started {
		return common.ErrHealthCheckFailed(ps.Name(), fmt.Errorf("plugin service not started"))
	}

	// Check plugin manager health
	if err := ps.manager.OnHealthCheck(ctx); err != nil {
		return common.ErrHealthCheckFailed(ps.Name(), fmt.Errorf("plugin manager health check failed: %w", err))
	}

	// Check system resources
	stats := ps.manager.GetStats()
	if stats.FailedPlugins > 0 {
		return common.ErrHealthCheckFailed(ps.Name(), fmt.Errorf("%d plugins in failed state", stats.FailedPlugins))
	}

	// Check average health score
	if stats.AverageHealthScore < 0.7 {
		return common.ErrHealthCheckFailed(ps.Name(), fmt.Errorf("average plugin health score is low: %.2f", stats.AverageHealthScore))
	}

	return nil
}

// GetManager returns the plugin manager
func (ps *PluginService) GetManager() PluginManager {
	return ps.manager
}

// GetStats returns plugin service statistics
func (ps *PluginService) GetStats() PluginServiceStats {
	managerStats := ps.manager.GetStats()

	return PluginServiceStats{
		ServiceName:         ps.Name(),
		Started:             ps.started,
		StartTime:           ps.startTime,
		Uptime:              time.Since(ps.startTime),
		PluginManagerStats:  managerStats,
		ConfigurationLoaded: ps.config != nil,
		LastHealthCheck:     time.Now(),
	}
}

// PluginServiceStats contains plugin service statistics
type PluginServiceStats struct {
	ServiceName         string             `json:"service_name"`
	Started             bool               `json:"started"`
	StartTime           time.Time          `json:"start_time"`
	Uptime              time.Duration      `json:"uptime"`
	PluginManagerStats  PluginManagerStats `json:"plugin_manager_stats"`
	ConfigurationLoaded bool               `json:"configuration_loaded"`
	LastHealthCheck     time.Time          `json:"last_health_check"`
}

// RegisterPluginAPI registers plugin management API endpoints
func (ps *PluginService) RegisterPluginAPI(router common.Router) error {
	// Plugin management endpoints
	router.GET("/plugins", ps.handleListPlugins)
	router.GET("/plugins/:id", ps.handleGetPlugin)
	router.POST("/plugins/install", ps.handleInstallPlugin)
	router.DELETE("/plugins/:id", ps.handleUninstallPlugin)
	router.POST("/plugins/:id/enable", ps.handleEnablePlugin)
	router.POST("/plugins/:id/disable", ps.handleDisablePlugin)
	router.POST("/plugins/:id/update", ps.handleUpdatePlugin)
	router.GET("/plugins/:id/health", ps.handlePluginHealth)
	router.GET("/plugins/:id/metrics", ps.handlePluginMetrics)
	router.GET("/plugins/:id/config", ps.handleGetPluginConfig)
	router.PUT("/plugins/:id/config", ps.handleUpdatePluginConfig)

	// Plugin store endpoints
	router.GET("/plugins/store/search", ps.handleSearchPlugins)
	router.GET("/plugins/store/:id", ps.handleGetPluginInfo)
	router.GET("/plugins/store/:id/versions", ps.handleGetPluginVersions)

	// Plugin manager endpoints
	router.GET("/plugins/manager/stats", ps.handleGetManagerStats)
	router.GET("/plugins/manager/health", ps.handleGetManagerHealth)
	router.POST("/plugins/manager/reload", ps.handleReloadManager)

	ps.logger.Info("plugin API endpoints registered")
	return nil
}

// API Handler methods

func (ps *PluginService) handleListPlugins(ctx common.Context) (interface{}, error) {
	plugins := ps.manager.GetPlugins()

	response := make([]map[string]interface{}, len(plugins))
	for i, plugin := range plugins {
		response[i] = map[string]interface{}{
			"id":          plugin.ID(),
			"name":        plugin.Name(),
			"version":     plugin.Version(),
			"description": plugin.Description(),
			"author":      plugin.Author(),
			"type":        plugin.Type(),
			"metrics":     plugin.GetMetrics(),
		}
	}

	return response, nil
}

func (ps *PluginService) handleGetPlugin(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	plugin, err := ps.manager.GetPlugin(pluginID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":           plugin.ID(),
		"name":         plugin.Name(),
		"version":      plugin.Version(),
		"description":  plugin.Description(),
		"author":       plugin.Author(),
		"license":      plugin.License(),
		"type":         plugin.Type(),
		"capabilities": plugin.Capabilities(),
		"dependencies": plugin.Dependencies(),
		"metrics":      plugin.GetMetrics(),
		"config":       plugin.GetConfig(),
	}, nil
}

func (ps *PluginService) handleInstallPlugin(ctx common.Context) (interface{}, error) {
	var req struct {
		Source   string                 `json:"source"`
		Version  string                 `json:"version"`
		Config   map[string]interface{} `json:"config"`
		AutoLoad bool                   `json:"auto_load"`
	}

	if err := ctx.BindJSON(&req); err != nil {
		return nil, fmt.Errorf("invalid request body: %w", err)
	}

	source := PluginSource{
		Type:     PluginSourceTypeMarketplace,
		Location: req.Source,
		Version:  req.Version,
		Config:   req.Config,
	}

	if err := ps.manager.InstallPlugin(ctx, source); err != nil {
		return nil, fmt.Errorf("failed to install plugin: %w", err)
	}

	return map[string]interface{}{
		"message": "Plugin installed successfully",
		"source":  req.Source,
		"version": req.Version,
	}, nil
}

func (ps *PluginService) handleUninstallPlugin(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	if err := ps.manager.UnloadPlugin(ctx, pluginID); err != nil {
		return nil, fmt.Errorf("failed to uninstall plugin: %w", err)
	}

	return map[string]interface{}{
		"message":   "Plugin uninstalled successfully",
		"plugin_id": pluginID,
	}, nil
}

func (ps *PluginService) handleEnablePlugin(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	if err := ps.manager.EnablePlugin(ctx, pluginID); err != nil {
		return nil, fmt.Errorf("failed to enable plugin: %w", err)
	}

	return map[string]interface{}{
		"message":   "Plugin enabled successfully",
		"plugin_id": pluginID,
	}, nil
}

func (ps *PluginService) handleDisablePlugin(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	if err := ps.manager.DisablePlugin(ctx, pluginID); err != nil {
		return nil, fmt.Errorf("failed to disable plugin: %w", err)
	}

	return map[string]interface{}{
		"message":   "Plugin disabled successfully",
		"plugin_id": pluginID,
	}, nil
}

func (ps *PluginService) handleUpdatePlugin(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	if err := ps.manager.UpdatePlugin(ctx, pluginID); err != nil {
		return nil, fmt.Errorf("failed to update plugin: %w", err)
	}

	return map[string]interface{}{
		"message":   "Plugin updated successfully",
		"plugin_id": pluginID,
	}, nil
}

func (ps *PluginService) handlePluginHealth(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	plugin, err := ps.manager.GetPlugin(pluginID)
	if err != nil {
		return nil, err
	}

	if err := plugin.HealthCheck(ctx); err != nil {
		return map[string]interface{}{
			"healthy": false,
			"error":   err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"healthy": true,
		"metrics": plugin.GetMetrics(),
	}, nil
}

func (ps *PluginService) handlePluginMetrics(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	plugin, err := ps.manager.GetPlugin(pluginID)
	if err != nil {
		return nil, err
	}

	return plugin.GetMetrics(), nil
}

func (ps *PluginService) handleGetPluginConfig(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	plugin, err := ps.manager.GetPlugin(pluginID)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"config": plugin.GetConfig(),
		"schema": plugin.ConfigSchema(),
	}, nil
}

func (ps *PluginService) handleUpdatePluginConfig(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	plugin, err := ps.manager.GetPlugin(pluginID)
	if err != nil {
		return nil, err
	}

	var config map[string]interface{}
	if err := ctx.BindJSON(&config); err != nil {
		return nil, fmt.Errorf("invalid config JSON: %w", err)
	}

	if err := plugin.Configure(config); err != nil {
		return nil, fmt.Errorf("failed to update plugin config: %w", err)
	}

	return map[string]interface{}{
		"message": "Plugin configuration updated successfully",
		"config":  config,
	}, nil
}

func (ps *PluginService) handleSearchPlugins(ctx common.Context) (interface{}, error) {
	query := ctx.QueryParam("q")
	pluginType := ctx.QueryParam("type")
	category := ctx.QueryParam("category")

	searchQuery := PluginQuery{
		Name:     query,
		Category: category,
		Limit:    50,
	}

	if pluginType != "" {
		searchQuery.Type = PluginType(pluginType)
	}

	results, err := ps.manager.GetPluginStore().Search(ctx, searchQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to search plugins: %w", err)
	}

	return results, nil
}

func (ps *PluginService) handleGetPluginInfo(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	info, err := ps.manager.GetPluginStore().Get(ctx, pluginID)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin info: %w", err)
	}

	return info, nil
}

func (ps *PluginService) handleGetPluginVersions(ctx common.Context) (interface{}, error) {
	pluginID := ctx.PathParam("id")
	if pluginID == "" {
		return nil, fmt.Errorf("plugin ID is required")
	}

	versions, err := ps.manager.GetPluginStore().GetVersions(ctx, pluginID)
	if err != nil {
		return nil, fmt.Errorf("failed to get plugin versions: %w", err)
	}

	return versions, nil
}

func (ps *PluginService) handleGetManagerStats(ctx common.Context) (interface{}, error) {
	return ps.manager.GetStats(), nil
}

func (ps *PluginService) handleGetManagerHealth(ctx common.Context) (interface{}, error) {
	if err := ps.manager.OnHealthCheck(ctx); err != nil {
		return map[string]interface{}{
			"healthy": false,
			"error":   err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"healthy": true,
		"stats":   ps.manager.GetStats(),
	}, nil
}

func (ps *PluginService) handleReloadManager(ctx common.Context) (interface{}, error) {
	// Reload plugin manager configuration
	config, err := ps.loadConfiguration()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	if err := ps.configureManager(ctx, config); err != nil {
		return nil, fmt.Errorf("failed to reconfigure manager: %w", err)
	}

	return map[string]interface{}{
		"message": "Plugin manager reloaded successfully",
	}, nil
}

// Helper methods

func (ps *PluginService) loadConfiguration() (*PluginServiceConfig, error) {
	config := &PluginServiceConfig{
		AutoLoadPlugins:     []string{},
		PluginDirectory:     "plugins",
		SandboxEnabled:      true,
		SecurityEnabled:     true,
		MarketplaceURL:      "https://plugins.forge.dev",
		MaxPlugins:          100,
		MaxMemoryPerPlugin:  1024 * 1024 * 1024, // 1GB
		UpdateCheckInterval: 24 * time.Hour,
		HealthCheckInterval: 5 * time.Minute,
	}

	if err := ps.config.Bind("plugins", config); err != nil {
		return nil, fmt.Errorf("failed to bind plugin configuration: %w", err)
	}

	return config, nil
}

func (ps *PluginService) configureManager(ctx context.Context, config *PluginServiceConfig) error {
	// Configure plugin manager based on configuration
	// This would involve setting up the sandbox, security, and other components
	return nil
}

func (ps *PluginService) loadAutoLoadPlugins(ctx context.Context, config *PluginServiceConfig) error {
	for _, pluginSpec := range config.AutoLoadPlugins {
		source := PluginSource{
			Type:     PluginSourceTypeFile,
			Location: pluginSpec,
		}

		if _, err := ps.manager.LoadPlugin(ctx, source); err != nil {
			ps.logger.Error("failed to load auto-load plugin",
				logger.String("plugin", pluginSpec),
				logger.Error(err),
			)
		}
	}

	return nil
}

func (ps *PluginService) startBackgroundTasks(ctx context.Context) {
	config, err := ps.loadConfiguration()
	if err != nil {
		ps.logger.Error("failed to load configuration for background tasks", logger.Error(err))
		return
	}

	// Start update check task
	if config.UpdateCheckInterval > 0 {
		go ps.updateCheckTask(ctx, config.UpdateCheckInterval)
	}

	// Start health check task
	if config.HealthCheckInterval > 0 {
		go ps.healthCheckTask(ctx, config.HealthCheckInterval)
	}
}

func (ps *PluginService) updateCheckTask(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ps.checkForUpdates(ctx)
		}
	}
}

func (ps *PluginService) healthCheckTask(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ps.performHealthChecks(ctx)
		}
	}
}

func (ps *PluginService) checkForUpdates(ctx context.Context) {
	plugins := ps.manager.GetPlugins()

	for _, plugin := range plugins {
		if err := ps.manager.UpdatePlugin(ctx, plugin.ID()); err != nil {
			ps.logger.Debug("no updates available for plugin",
				logger.String("plugin_id", plugin.ID()),
			)
		}
	}
}

func (ps *PluginService) performHealthChecks(ctx context.Context) {
	plugins := ps.manager.GetPlugins()

	for _, plugin := range plugins {
		if err := plugin.HealthCheck(ctx); err != nil {
			ps.logger.Warn("plugin health check failed",
				logger.String("plugin_id", plugin.ID()),
				logger.Error(err),
			)

			if ps.metrics != nil {
				ps.metrics.Counter("forge.plugins.health_check_failed",
					"plugin_id", plugin.ID(),
					"plugin_type", string(plugin.Type()),
				).Inc()
			}
		}
	}
}

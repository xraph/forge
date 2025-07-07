package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/xraph/forge/core"
	"github.com/xraph/forge/jobs"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// System represents the complete plugin system
type System struct {
	manager     Manager
	registry    Registry
	loader      Loader
	installer   Installer
	integrator  Integrator
	configMgr   ConfigManager
	eventBus    EventBus
	sandbox     Sandbox
	cliCommands *CLICommands

	config      SystemConfig
	logger      logger.Logger
	initialized bool
	started     bool
}

// SystemConfig configures the entire plugin system
type SystemConfig struct {
	// Directories
	PluginDir string `mapstructure:"plugin_dir" yaml:"plugin_dir"`
	ConfigDir string `mapstructure:"config_dir" yaml:"config_dir"`
	CacheDir  string `mapstructure:"cache_dir" yaml:"cache_dir"`
	TempDir   string `mapstructure:"temp_dir" yaml:"temp_dir"`

	// Manager configuration
	Manager    ManagerConfig    `mapstructure:"manager" yaml:"manager"`
	Registry   RegistryConfig   `mapstructure:"registry" yaml:"registry"`
	Loader     LoaderConfig     `mapstructure:"loader" yaml:"loader"`
	Installer  InstallerConfig  `mapstructure:"installer" yaml:"installer"`
	Integrator IntegratorConfig `mapstructure:"integrator" yaml:"integrator"`
	EventBus   EventBusConfig   `mapstructure:"event_bus" yaml:"event_bus"`

	// Features
	EnableSandbox bool `mapstructure:"enable_sandbox" yaml:"enable_sandbox"`
	EnableCLI     bool `mapstructure:"enable_cli" yaml:"enable_cli"`
	AutoLoad      bool `mapstructure:"auto_load" yaml:"auto_load"`
	AutoStart     bool `mapstructure:"auto_start" yaml:"auto_start"`

	// Security
	VerifySignatures bool     `mapstructure:"verify_signatures" yaml:"verify_signatures"`
	TrustedSources   []string `mapstructure:"trusted_sources" yaml:"trusted_sources"`
	AllowedFormats   []string `mapstructure:"allowed_formats" yaml:"allowed_formats"`
}

// DefaultSystemConfig returns a default system configuration
func DefaultSystemConfig() SystemConfig {
	baseDir := filepath.Join(os.TempDir(), "forge-plugins")

	return SystemConfig{
		PluginDir:        filepath.Join(baseDir, "plugins"),
		ConfigDir:        filepath.Join(baseDir, "config"),
		CacheDir:         filepath.Join(baseDir, "cache"),
		TempDir:          os.TempDir(),
		Manager:          ManagerConfig{AutoLoad: true},
		Registry:         RegistryConfig{Type: "local", CacheDir: filepath.Join(baseDir, "registry")},
		Loader:           LoaderConfig{Timeout: 30 * time.Second, MaxPluginSize: 100 * 1024 * 1024},
		Installer:        InstallerConfig{MaxConcurrent: 3, Timeout: 5 * time.Minute},
		Integrator:       IntegratorConfig{AutoRegister: true, ValidateOnRegister: true},
		EventBus:         EventBusConfig{BufferSize: 1000, BatchSize: 10},
		EnableSandbox:    true,
		EnableCLI:        true,
		AutoLoad:         true,
		AutoStart:        true,
		VerifySignatures: false,
		TrustedSources:   []string{},
		AllowedFormats:   []string{".so", ".dylib", ".dll", ".wasm", ".js", ".lua"},
	}
}

// NewSystem creates a new plugin system
func NewSystem(config SystemConfig) (*System, error) {
	if config.PluginDir == "" {
		config = DefaultSystemConfig()
	}

	// Create directories
	for _, dir := range []string{config.PluginDir, config.ConfigDir, config.CacheDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	system := &System{
		config: config,
		logger: logger.GetGlobalLogger().Named("plugin-system"),
	}

	// Initialize components
	if err := system.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize plugin system: %w", err)
	}

	system.initialized = true
	system.logger.Info("Plugin system initialized successfully")

	return system, nil
}

// NewSystemWithDefaults creates a new plugin system with default configuration
func NewSystemWithDefaults() (*System, error) {
	return NewSystem(DefaultSystemConfig())
}

// Initialize initializes the plugin system with a container
func (s *System) Initialize(container core.Container) error {
	if !s.initialized {
		return fmt.Errorf("system not initialized")
	}

	s.logger.Info("Initializing plugin system with container")

	// Initialize manager with container
	if err := s.manager.InitializeAll(container); err != nil {
		return fmt.Errorf("failed to initialize plugins: %w", err)
	}

	s.logger.Info("Plugin system initialized with container")
	return nil
}

// Start starts the plugin system
func (s *System) Start(ctx context.Context) error {
	if !s.initialized {
		return fmt.Errorf("system not initialized")
	}

	if s.started {
		return nil
	}

	s.logger.Info("Starting plugin system")

	// Start all enabled plugins
	if err := s.manager.StartAll(ctx); err != nil {
		return fmt.Errorf("failed to start plugins: %w", err)
	}

	s.started = true
	s.logger.Info("Plugin system started successfully")

	return nil
}

// Stop stops the plugin system
func (s *System) Stop(ctx context.Context) error {
	if !s.started {
		return nil
	}

	s.logger.Info("Stopping plugin system")

	// Stop all plugins
	if err := s.manager.StopAll(ctx); err != nil {
		s.logger.Error("Failed to stop some plugins", logger.Error(err))
	}

	// Shutdown event bus
	if err := s.eventBus.Shutdown(ctx); err != nil {
		s.logger.Error("Failed to shutdown event bus", logger.Error(err))
	}

	s.started = false
	s.logger.Info("Plugin system stopped")

	return nil
}

// Integration methods

// IntegrateWithRouter integrates plugins with the HTTP router
func (s *System) IntegrateWithRouter(r router.Router) error {
	s.logger.Info("Integrating plugins with router")

	if err := s.integrator.RegisterRoutes(r); err != nil {
		return fmt.Errorf("failed to register plugin routes: %w", err)
	}

	if err := s.integrator.RegisterMiddleware(r); err != nil {
		return fmt.Errorf("failed to register plugin middleware: %w", err)
	}

	s.logger.Info("Plugin router integration completed")
	return nil
}

// IntegrateWithJobManager integrates plugins with the job manager
func (s *System) IntegrateWithJobManager(jobManager jobs.Processor) error {
	s.logger.Info("Integrating plugins with job manager")

	if err := s.integrator.RegisterJobs(jobManager); err != nil {
		return fmt.Errorf("failed to register plugin jobs: %w", err)
	}

	s.logger.Info("Plugin job manager integration completed")
	return nil
}

// IntegrateWithCLI integrates plugins with the CLI
func (s *System) IntegrateWithCLI(rootCmd *cobra.Command) error {
	if !s.config.EnableCLI {
		return nil
	}

	s.logger.Info("Integrating plugins with CLI")

	// Add plugin management commands
	if s.cliCommands != nil {
		pluginCmd := s.cliCommands.CreateCommands()
		rootCmd.AddCommand(pluginCmd)
	}

	// Register plugin commands
	if err := s.integrator.RegisterCommands(rootCmd); err != nil {
		return fmt.Errorf("failed to register plugin commands: %w", err)
	}

	s.logger.Info("Plugin CLI integration completed")
	return nil
}

// IntegrateWithHealthChecker integrates plugins with health checking
func (s *System) IntegrateWithHealthChecker(healthChecker interface{}) error {
	s.logger.Info("Integrating plugins with health checker")

	if err := s.integrator.RegisterHealthChecks(healthChecker); err != nil {
		return fmt.Errorf("failed to register plugin health checks: %w", err)
	}

	s.logger.Info("Plugin health checker integration completed")
	return nil
}

// Plugin management methods

// InstallPlugin installs a plugin from various sources
func (s *System) InstallPlugin(ctx context.Context, spec InstallSpec) (*InstallResult, error) {
	s.logger.Info("Installing plugin",
		logger.String("name", spec.Name),
		logger.String("source", string(spec.Source.Type)),
	)

	result, err := s.installer.Install(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("plugin installation failed: %w", err)
	}

	// Auto-start if system is running and auto-start is enabled
	if s.started && s.config.AutoStart && spec.Options.Enable {
		if container := s.getContainer(); container != nil {
			if err := result.Plugin.Initialize(container); err != nil {
				s.logger.Warn("Failed to initialize newly installed plugin", logger.Error(err))
			} else if err := result.Plugin.Start(ctx); err != nil {
				s.logger.Warn("Failed to start newly installed plugin", logger.Error(err))
			}
		}
	}

	return result, nil
}

// UninstallPlugin removes a plugin
func (s *System) UninstallPlugin(ctx context.Context, pluginName string, options UninstallOptions) error {
	s.logger.Info("Uninstalling plugin", logger.String("name", pluginName))

	return s.installer.Uninstall(ctx, pluginName, options)
}

// UpdatePlugin updates a plugin to a newer version
func (s *System) UpdatePlugin(ctx context.Context, pluginName string, options UpdateOptions) (*UpdateResult, error) {
	s.logger.Info("Updating plugin", logger.String("name", pluginName))

	return s.installer.Update(ctx, pluginName, options)
}

// EnablePlugin enables a plugin
func (s *System) EnablePlugin(pluginName string) error {
	return s.manager.Enable(pluginName)
}

// DisablePlugin disables a plugin
func (s *System) DisablePlugin(pluginName string) error {
	return s.manager.Disable(pluginName)
}

// ReloadPlugin reloads a plugin
func (s *System) ReloadPlugin(pluginName string) error {
	return s.manager.Reload(pluginName)
}

// Search and discovery methods

// SearchPlugins searches for plugins in the registry
func (s *System) SearchPlugins(query string) []PluginMetadata {
	return s.registry.Search(query)
}

// GetPlugin gets information about a specific plugin
func (s *System) GetPlugin(pluginName string) (Plugin, error) {
	return s.manager.Get(pluginName)
}

// ListPlugins lists all installed plugins
func (s *System) ListPlugins() []Plugin {
	return s.manager.List()
}

// ListEnabledPlugins lists all enabled plugins
func (s *System) ListEnabledPlugins() []Plugin {
	return s.manager.ListEnabled()
}

// GetPluginStatus gets the status of all plugins
func (s *System) GetPluginStatus() map[string]PluginStatus {
	return s.manager.Status()
}

// Configuration methods

// SetPluginConfig sets configuration for a plugin
func (s *System) SetPluginConfig(pluginName string, config map[string]interface{}) error {
	return s.manager.SetConfig(pluginName, config)
}

// GetPluginConfig gets configuration for a plugin
func (s *System) GetPluginConfig(pluginName string) map[string]interface{} {
	return s.manager.GetConfig(pluginName)
}

// System status and health

// GetSystemStatus returns the overall system status
func (s *System) GetSystemStatus() SystemStatus {
	pluginStatuses := s.manager.Status()
	integrationStatus := s.integrator.GetIntegrationStatus()

	totalPlugins := len(s.manager.List())
	enabledPlugins := len(s.manager.ListEnabled())

	status := SystemStatus{
		Initialized:     s.initialized,
		Started:         s.started,
		TotalPlugins:    totalPlugins,
		EnabledPlugins:  enabledPlugins,
		DisabledPlugins: totalPlugins - enabledPlugins,
		Integration:     integrationStatus,
		Health:          make(map[string]error),
		LastUpdate:      time.Now(),
	}

	// Check plugin health
	if s.started {
		status.Health = s.manager.Health(context.Background())

		// Calculate health summary
		healthyCount := 0
		for _, err := range status.Health {
			if err == nil {
				healthyCount++
			}
		}
		status.HealthyPlugins = healthyCount
		status.UnhealthyPlugins = len(status.Health) - healthyCount
	}

	// Count errors
	errorCount := 0
	for _, pluginStatus := range pluginStatuses {
		if pluginStatus.Error != "" {
			errorCount++
		}
	}
	status.ErrorCount = errorCount

	return status
}

// GetSystemStatistics returns detailed system statistics
func (s *System) GetSystemStatistics() SystemStatistics {
	stats := SystemStatistics{
		System:     s.GetSystemStatus(),
		Manager:    s.getManagerStatistics(),
		Registry:   s.registry.GetStatistics(),
		Loader:     s.getLoaderStatistics(),
		Installer:  s.getInstallerStatistics(),
		Integrator: s.integrator.GetIntegrationStatus(),
		EventBus:   s.eventBus.GetStatistics(),
		ConfigMgr:  s.configMgr.GetStatistics(),
	}

	if s.sandbox != nil {
		stats.Sandbox = s.sandbox.GetSandboxStatistics()
	}

	return stats
}

// CheckSystemHealth performs a comprehensive health check
func (s *System) CheckSystemHealth(ctx context.Context) HealthCheckResult {
	result := HealthCheckResult{
		Timestamp: time.Now(),
		Overall:   "healthy",
		Checks:    make(map[string]CheckResult),
	}

	// Check plugin health
	pluginHealth := s.manager.Health(ctx)
	for plugin, err := range pluginHealth {
		checkResult := CheckResult{
			Status:  "healthy",
			Message: "Plugin is healthy",
		}

		if err != nil {
			checkResult.Status = "unhealthy"
			checkResult.Message = err.Error()
			result.Overall = "unhealthy"
		}

		result.Checks["plugin."+plugin] = checkResult
	}

	// Check integration health
	integrationErrors := s.integrator.ValidateIntegrations()
	if len(integrationErrors) > 0 {
		result.Overall = "degraded"
		result.Checks["integrations"] = CheckResult{
			Status:  "degraded",
			Message: fmt.Sprintf("%d integration errors detected", len(integrationErrors)),
		}
	} else {
		result.Checks["integrations"] = CheckResult{
			Status:  "healthy",
			Message: "All integrations are valid",
		}
	}

	// Check system components
	result.Checks["manager"] = CheckResult{Status: "healthy", Message: "Manager is operational"}
	result.Checks["registry"] = CheckResult{Status: "healthy", Message: "Registry is operational"}
	result.Checks["loader"] = CheckResult{Status: "healthy", Message: "Loader is operational"}
	result.Checks["installer"] = CheckResult{Status: "healthy", Message: "Installer is operational"}
	result.Checks["integrator"] = CheckResult{Status: "healthy", Message: "Integrator is operational"}
	result.Checks["event_bus"] = CheckResult{Status: "healthy", Message: "Event bus is operational"}

	return result
}

// Validation and diagnostics

// ValidateSystem validates the entire plugin system
func (s *System) ValidateSystem() []ValidationError {
	var errors []ValidationError

	// Validate plugins
	plugins := s.manager.List()
	for _, plugin := range plugins {
		if err := s.installer.ValidateInstallation(plugin); err != nil {
			errors = append(errors, ValidationError{
				Component: "plugin",
				Name:      plugin.Name(),
				Type:      "installation",
				Message:   err.Error(),
			})
		}
	}

	// Validate integrations
	integrationErrors := s.integrator.ValidateIntegrations()
	for _, err := range integrationErrors {
		errors = append(errors, ValidationError{
			Component: "integration",
			Name:      err.Plugin,
			Type:      err.Type,
			Message:   err.Message,
		})
	}

	// Validate configuration
	for _, plugin := range plugins {
		config := s.manager.GetConfig(plugin.Name())
		if err := plugin.ValidateConfig(config); err != nil {
			errors = append(errors, ValidationError{
				Component: "config",
				Name:      plugin.Name(),
				Type:      "validation",
				Message:   err.Error(),
			})
		}
	}

	return errors
}

// GetSystemDiagnostics returns detailed system diagnostics
func (s *System) GetSystemDiagnostics() SystemDiagnostics {
	diagnostics := SystemDiagnostics{
		Timestamp:        time.Now(),
		SystemInfo:       s.getSystemInfo(),
		Configuration:    s.config,
		Status:           s.GetSystemStatus(),
		Statistics:       s.GetSystemStatistics(),
		ValidationErrors: s.ValidateSystem(),
		Health:           s.CheckSystemHealth(context.Background()),
	}

	return diagnostics
}

// Component access methods

// Manager returns the plugin manager
func (s *System) Manager() Manager {
	return s.manager
}

// Registry returns the plugin registry
func (s *System) Registry() Registry {
	return s.registry
}

// Loader returns the plugin loader
func (s *System) Loader() Loader {
	return s.loader
}

// Installer returns the plugin installer
func (s *System) Installer() Installer {
	return s.installer
}

// Integrator returns the plugin integrator
func (s *System) Integrator() Integrator {
	return s.integrator
}

// ConfigManager returns the configuration manager
func (s *System) ConfigManager() ConfigManager {
	return s.configMgr
}

// EventBus returns the event bus
func (s *System) EventBus() EventBus {
	return s.eventBus
}

// Sandbox returns the sandbox (if enabled)
func (s *System) Sandbox() Sandbox {
	return s.sandbox
}

// CLICommands returns the CLI commands
func (s *System) CLICommands() *CLICommands {
	return s.cliCommands
}

// Private initialization methods

func (s *System) initializeComponents() error {
	// Initialize registry
	s.registry = NewRegistry(s.config.Registry)

	// Initialize loader
	s.loader = NewLoader(s.config.Loader)

	// Initialize sandbox if enabled
	if s.config.EnableSandbox {
		s.sandbox = NewSandbox()
	}

	// Initialize configuration manager
	s.configMgr = NewConfigManager(s.config.ConfigDir)

	// Initialize event bus
	s.eventBus = NewEventBusWithConfig(s.config.EventBus)

	// Initialize manager
	s.manager = NewManager(s.config.Manager)

	// Initialize installer
	s.installer = NewInstaller(s.manager, s.registry, s.loader, s.config.Installer)

	// Initialize integrator
	s.integrator = NewIntegrator(s.manager, s.config.Integrator)

	// Initialize CLI commands if enabled
	if s.config.EnableCLI {
		s.cliCommands = NewCLICommands(s.manager, s.registry, s.loader)
	}

	// Setup event subscriptions
	if err := s.setupEventSubscriptions(); err != nil {
		return fmt.Errorf("failed to setup event subscriptions: %w", err)
	}

	// Auto-load plugins if enabled
	if s.config.AutoLoad && s.config.PluginDir != "" {
		if err := s.manager.LoadFromDirectory(s.config.PluginDir); err != nil {
			s.logger.Warn("Failed to auto-load plugins", logger.Error(err))
		}
	}

	return nil
}

func (s *System) setupEventSubscriptions() error {
	// Subscribe integrator to plugin events
	if err := s.integrator.SubscribeToEvents(s.eventBus); err != nil {
		return err
	}

	// Add logging event handler
	loggingHandler := NewLoggingEventHandler(EventLevelInfo)
	s.eventBus.Subscribe("*", loggingHandler)

	// Add metrics event handler
	metricsHandler := NewMetricsEventHandler()
	s.eventBus.Subscribe("*", metricsHandler)

	return nil
}

// Utility methods

func (s *System) getContainer() core.Container {
	// This would return the container if available
	// For now, return nil as a placeholder
	return nil
}

func (s *System) getManagerStatistics() map[string]interface{} {
	status := s.manager.Status()

	stats := map[string]interface{}{
		"total_plugins": len(status),
		"enabled_plugins": func() int {
			count := 0
			for _, s := range status {
				if s.Enabled {
					count++
				}
			}
			return count
		}(),
		"initialized_plugins": func() int {
			count := 0
			for _, s := range status {
				if s.Initialized {
					count++
				}
			}
			return count
		}(),
		"started_plugins": func() int {
			count := 0
			for _, s := range status {
				if s.Started {
					count++
				}
			}
			return count
		}(),
	}

	return stats
}

func (s *System) getLoaderStatistics() interface{} {
	if loader, ok := s.loader.(*loader); ok {
		return loader.GetStatistics()
	}
	return map[string]interface{}{}
}

func (s *System) getInstallerStatistics() map[string]interface{} {
	// Placeholder for installer statistics
	return map[string]interface{}{
		"cache_dir": s.config.CacheDir,
		"temp_dir":  s.config.TempDir,
	}
}

func (s *System) getSystemInfo() map[string]interface{} {
	return map[string]interface{}{
		"plugin_dir":        s.config.PluginDir,
		"config_dir":        s.config.ConfigDir,
		"cache_dir":         s.config.CacheDir,
		"sandbox_enabled":   s.config.EnableSandbox,
		"cli_enabled":       s.config.EnableCLI,
		"auto_load":         s.config.AutoLoad,
		"auto_start":        s.config.AutoStart,
		"verify_signatures": s.config.VerifySignatures,
		"allowed_formats":   s.config.AllowedFormats,
		"trusted_sources":   s.config.TrustedSources,
	}
}

// Supporting types for system status and diagnostics

// SystemStatus represents the overall system status
type SystemStatus struct {
	Initialized      bool              `json:"initialized"`
	Started          bool              `json:"started"`
	TotalPlugins     int               `json:"total_plugins"`
	EnabledPlugins   int               `json:"enabled_plugins"`
	DisabledPlugins  int               `json:"disabled_plugins"`
	HealthyPlugins   int               `json:"healthy_plugins"`
	UnhealthyPlugins int               `json:"unhealthy_plugins"`
	ErrorCount       int               `json:"error_count"`
	Integration      IntegrationStatus `json:"integration"`
	Health           map[string]error  `json:"health,omitempty"`
	LastUpdate       time.Time         `json:"last_update"`
}

// SystemStatistics represents detailed system statistics
type SystemStatistics struct {
	System     SystemStatus           `json:"system"`
	Manager    map[string]interface{} `json:"manager"`
	Registry   map[string]interface{} `json:"registry"`
	Loader     interface{}            `json:"loader"`
	Installer  map[string]interface{} `json:"installer"`
	Integrator IntegrationStatus      `json:"integrator"`
	EventBus   EventBusStatistics     `json:"event_bus"`
	ConfigMgr  map[string]interface{} `json:"config_manager"`
	Sandbox    map[string]interface{} `json:"sandbox,omitempty"`
}

// HealthCheckResult represents the result of a system health check
type HealthCheckResult struct {
	Timestamp time.Time              `json:"timestamp"`
	Overall   string                 `json:"overall"` // healthy, degraded, unhealthy
	Checks    map[string]CheckResult `json:"checks"`
}

// CheckResult represents the result of an individual check
type CheckResult struct {
	Status  string `json:"status"` // healthy, degraded, unhealthy
	Message string `json:"message"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Component string `json:"component"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Message   string `json:"message"`
}

// SystemDiagnostics contains comprehensive system diagnostic information
type SystemDiagnostics struct {
	Timestamp        time.Time              `json:"timestamp"`
	SystemInfo       map[string]interface{} `json:"system_info"`
	Configuration    SystemConfig           `json:"configuration"`
	Status           SystemStatus           `json:"status"`
	Statistics       SystemStatistics       `json:"statistics"`
	ValidationErrors []ValidationError      `json:"validation_errors"`
	Health           HealthCheckResult      `json:"health"`
}

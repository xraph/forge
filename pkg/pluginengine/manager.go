package pluginengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/middleware"
	common2 "github.com/xraph/forge/pkg/pluginengine/common"
	"github.com/xraph/forge/pkg/pluginengine/hooks"
	"github.com/xraph/forge/pkg/pluginengine/loader"
	"github.com/xraph/forge/pkg/pluginengine/security"
	"github.com/xraph/forge/pkg/pluginengine/store"
)

// PluginManager manages plugins for the framework
type PluginManager interface {
	common.Service

	// AddPlugin registers a new plugin with the plugin manager, ensuring it adheres to compatibility and dependencies.
	AddPlugin(ctx context.Context, plugin Plugin) error

	// LoadPlugin loads a plugin from the given source and initializes it in the system.
	LoadPlugin(ctx context.Context, source PluginSource) (Plugin, error)

	// UnloadPlugin removes a loaded plugin by its ID and releases associated resources. Returns an error if the operation fails.
	UnloadPlugin(ctx context.Context, pluginID string) error

	// GetPlugin retrieves a plugin by its unique ID and returns it along with any error encountered during retrieval.
	GetPlugin(pluginID string) (Plugin, error)

	// GetPlugins returns a list of all currently managed plugins.
	GetPlugins() []Plugin

	// GetPluginStore retrieves the PluginStore instance for managing and discovering plugins.
	GetPluginStore() store.PluginStore

	// InstallPlugin installs a plugin from the specified source into the system and prepares it for use.
	InstallPlugin(ctx context.Context, source PluginSource) error

	// UpdatePlugin updates the specified plugin by its ID. It ensures the plugin is up-to-date with the latest version.
	UpdatePlugin(ctx context.Context, pluginID string) error

	// EnablePlugin activates a plugin by its ID, making it operational within the framework.
	EnablePlugin(ctx context.Context, pluginID string) error

	// DisablePlugin disables the plugin identified by the given pluginID, effectively stopping its operations.
	DisablePlugin(ctx context.Context, pluginID string) error

	// ValidatePlugin validates the provided plugin to ensure it meets the necessary requirements and conventions.
	ValidatePlugin(ctx context.Context, plugin Plugin) error

	// GetStats retrieves statistical data about the current state and performance of the plugin manager.
	GetStats() PluginManagerStats

	// ExecutePluginOperation executes a specific operation on a plugin identified by pluginID using the provided context.
	ExecutePluginOperation(ctx context.Context, pluginID string, operation PluginOperation) (interface{}, error)

	// GetSecurityReport retrieves the security report for a specified plugin by its pluginID.
	GetSecurityReport(pluginID string) (SecurityReport, error)

	// UpdateSecurityPolicies updates the security policies applied to the plugins managed by the plugin manager.
	UpdateSecurityPolicies(ctx context.Context) error

	// SetRouter sets the HTTP router to enable registration of routes and middleware required by plugins or the service.
	SetRouter(router common.Router)

	// SetMiddlewareManager sets the middleware manager instance to manage middleware operations for the plugin manager.
	SetMiddlewareManager(mw *middleware.Manager)
}

// PluginManagerImpl implements the PluginManager interface
type PluginManagerImpl struct {
	plugins     map[string]*PluginEntry
	pluginStore store.PluginStore
	loader      loader.PluginLoader
	registry    PluginRegistry
	hookSystem  hooks.HookSystem
	validator   *store.PluginValidator
	config      PluginManagerConfig

	// Security components
	securityManager *security.SecurityManager

	// Core dependencies
	container     common.Container
	logger        common.Logger
	metrics       common.Metrics
	configManager common.ConfigManager

	// State
	mu        sync.RWMutex
	started   bool
	startTime time.Time
	stats     PluginManagerStats

	// Component management
	router            common.Router       // Router instance for registering routes
	appContainer      common.Container    // Application container for services
	middlewareManager *middleware.Manager // Middleware manager

	// Track registered components per plugin for cleanup
	pluginRoutes     map[string][]string // pluginID -> routeIDs
	pluginServices   map[string][]string // pluginID -> serviceNames
	pluginMiddleware map[string][]string // pluginID -> middlewareNames
}

// PluginEntry represents a plugin entry in the manager
type PluginEntry struct {
	Plugin       Plugin
	State        PluginState
	LoadedAt     time.Time
	StartedAt    time.Time
	Config       map[string]interface{}
	Dependencies []string
	Dependents   []string
	Metrics      PluginMetrics
	LastError    error
	HealthScore  float64
	Context      *PluginContext

	// Security components
	Sandbox         security.PluginSandbox
	SecureEnv       *security.SecurePluginEnvironment
	IsolatedProcess security.IsolatedProcess
	SecurityReport  SecurityReport
	Permissions     []security.Permission
	Roles           []security.Role

	mu sync.RWMutex
}

// SecurityReport contains security information for a plugin
type SecurityReport struct {
	PluginID         string                     `json:"plugin_id"`
	SecurityScore    float64                    `json:"security_score"`
	RiskLevel        security.RiskLevel         `json:"risk_level"`
	LastScanTime     time.Time                  `json:"last_scan_time"`
	ScanResult       security.ScanResult        `json:"scan_result"`
	ComplianceReport security.ComplianceReport  `json:"compliance_report"`
	Violations       []security.PolicyViolation `json:"violations"`
	IsolationMetrics security.SandboxMetrics    `json:"isolation_metrics"`
	PermissionAudit  []security.AuditEntry      `json:"permission_audit"`
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
type PluginSource = common2.PluginEngineSource

// PluginSourceType defines the type of plugin source
type PluginSourceType = common2.PluginEngineSourceType

const (
	PluginSourceTypeFile        = common2.PluginEngineSourceTypeFile
	PluginSourceTypeURL         = common2.PluginEngineSourceTypeURL
	PluginSourceTypeMarketplace = common2.PluginEngineSourceTypeMarketplace
	PluginSourceTypeGit         = common2.PluginEngineSourceTypeGit
	PluginSourceTypeRegistry    = common2.PluginEngineSourceTypeRegistry
)

// NewPluginManager creates a new plugin manager with enhanced security
func NewPluginManager(container common.Container, log common.Logger, metrics common.Metrics, config common.ConfigManager) (PluginManager, error) {
	pluginConfig := PluginManagerConfig{}
	err := config.BindWithDefault("plugins", &pluginConfig, DefaultManagerConfig())
	if err != nil {
		return nil, err
	}

	pm := &PluginManagerImpl{
		plugins:       make(map[string]*PluginEntry),
		container:     container,
		logger:        log,
		metrics:       metrics,
		configManager: config,
		stats:         PluginManagerStats{},
		config:        pluginConfig,

		// Initialize tracking maps
		pluginRoutes:     make(map[string][]string),
		pluginServices:   make(map[string][]string),
		pluginMiddleware: make(map[string][]string),
	}

	// Initialize core components
	pm.pluginStore = store.NewPluginStore(pluginConfig.StoreConfig, log, metrics)
	pm.loader = loader.NewPluginLoader(log, metrics)
	pm.registry = pm.pluginStore.Registry()
	pm.hookSystem = hooks.NewHookSystem(log, metrics)
	pm.validator = pm.pluginStore.Validator()

	// Resolve router from container if available
	if routerInstance, err := container.ResolveNamed("router"); err == nil {
		if router, ok := routerInstance.(common.Router); ok {
			pm.router = router
			log.Info("router resolved for plugin manager")
		} else {
			log.Warn("resolved router instance is not of correct type")
		}
	} else {
		log.Warn("router not found in container, plugin routes will not be registered")
	}

	// Resolve middleware manager from container if available
	if middlewareInstance, err := container.ResolveNamed("middleware-manager"); err == nil {
		if mwManager, ok := middlewareInstance.(*middleware.Manager); ok {
			pm.middlewareManager = mwManager
			log.Info("middleware manager resolved for plugin manager")
		}
	}

	// Resolve application container if different from main container
	if appContainerInstance, err := container.ResolveNamed("app-container"); err == nil {
		if appContainer, ok := appContainerInstance.(common.Container); ok {
			pm.appContainer = appContainer
		}
	} else {
		// Use main container as fallback
		pm.appContainer = container
	}

	// Initialize security components
	pm.initializeSecurityComponents(log, metrics)

	return pm, nil
}

// SetRouter allows setting the router after plugin manager creation
func (pm *PluginManagerImpl) SetRouter(router common.Router) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.router = router
	pm.logger.Info("router set for plugin manager")
}

// SetMiddlewareManager allows setting the middleware manager after creation
func (pm *PluginManagerImpl) SetMiddlewareManager(mw *middleware.Manager) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.middlewareManager = mw
	pm.logger.Info("middleware manager set for plugin manager")
}

func (pm *PluginManagerImpl) initializeSecurityComponents(log common.Logger, metrics common.Metrics) {
	if !pm.config.EnableSecurity {
		pm.logger.Warn("plugin security is disabled")
		return
	}

	// Create security managers
	pm.securityManager = security.NewSecurityManager(pm.config.SecurityConfig, pm.logger, pm.metrics)

	pm.logger.Info("security components initialized",
		logger.Bool("sandbox_enabled", true),
		logger.Bool("permissions_enabled", true),
		logger.Bool("scanning_enabled", true),
		logger.Bool("policies_enabled", true),
		logger.Bool("isolation_enabled", true),
	)
}

// OnStart starts the plugin manager
func (pm *PluginManagerImpl) Start(ctx context.Context) error {
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

	// Initialize hook system
	if err := pm.hookSystem.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize hook system: %w", err)
	}

	// Initialize security components
	if pm.config.EnableSecurity {
		if err := pm.securityManager.Initialize(ctx); err != nil {
			return fmt.Errorf("failed to initialize security manager: %w", err)
		}
	}

	// Load auto-load plugins from configuration
	if err := pm.loadAutoLoadPlugins(ctx); err != nil {
		pm.logger.Warn("failed to load auto-load plugins", logger.Error(err))
	}

	pm.started = true

	pm.logger.Info("plugin manager started",
		logger.Int("total_plugins", len(pm.plugins)),
		logger.Bool("security_enabled", pm.config.EnableSecurity),
		logger.Duration("startup_time", time.Since(pm.startTime)),
	)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.plugins.manager_started").Inc()
	}

	return nil
}

// Name returns the service name
func (pm *PluginManagerImpl) Name() string {
	return common.PluginManagerKey
}

// Dependencies returns the service dependencies
func (pm *PluginManagerImpl) Dependencies() []string {
	return []string{common.ConfigKey, common.LoggerKey, common.MetricsKey}
}

// OnStop stops the plugin manager
func (pm *PluginManagerImpl) Stop(ctx context.Context) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.started {
		return common.ErrServiceNotStarted("plugin-manager", nil)
	}

	// Collect shutdown errors
	var shutdownErrors []error

	// Stop all plugins in reverse dependency order
	if err := pm.stopAllPlugins(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop all plugins: %w", err))
	}

	// Stop hook system
	if err := pm.hookSystem.Stop(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop hook system: %w", err))
	}

	// Stop plugin store
	if err := pm.pluginStore.Stop(ctx); err != nil {
		shutdownErrors = append(shutdownErrors, fmt.Errorf("failed to stop plugin store: %w", err))
	}

	// Return combined shutdown errors if any occurred
	if len(shutdownErrors) > 0 {
		return fmt.Errorf("plugin manager shutdown completed with errors: %v", shutdownErrors)
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

	// Check plugin health based on error rates (pattern from router plugin manager)
	failedPlugins := 0
	for pluginID, entry := range pm.plugins {
		entry.mu.RLock()

		if entry.State == PluginStateError {
			failedPlugins++
		} else if entry.State == PluginStateStarted {
			// Check for high error rates (30% threshold like router plugin manager)
			if entry.Metrics.CallCount > 0 {
				errorRate := float64(entry.Metrics.ErrorCount) / float64(entry.Metrics.CallCount)
				if errorRate > 0.3 {
					entry.mu.RUnlock()
					return common.ErrHealthCheckFailed("plugin-manager",
						fmt.Errorf("plugin %s has high error rate: %.2f%%", pluginID, errorRate*100))
				}
			}

			// Check health score
			if entry.HealthScore < 0.5 {
				entry.mu.RUnlock()
				return common.ErrHealthCheckFailed("plugin-manager",
					fmt.Errorf("plugin %s has low health score: %.2f", pluginID, entry.HealthScore))
			}
		}

		entry.mu.RUnlock()
	}

	if failedPlugins > 0 {
		return common.ErrHealthCheckFailed("plugin-manager",
			fmt.Errorf("%d plugins in error state", failedPlugins))
	}

	return nil
}

// LoadPlugin loads a plugin with full security integration
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

	// // Validate the plugin
	// if err := pm.validator.ValidatePackage(ctx, plugin); err != nil {
	// 	return nil, fmt.Errorf("plugin validation failed: %w", err)
	// }

	// Check if plugin already exists
	if _, exists := pm.plugins[plugin.ID()]; exists {
		return nil, common.ErrServiceAlreadyExists(plugin.ID())
	}

	// Security scanning
	var scanResult security.ScanResult
	if pm.config.EnableSecurity {
		// pkg := common2.PluginEnginePackage{
		// 	Info:   plugin.GetConfig(),
		// 	Binary: []byte{}, // Would be actual binary data
		// 	Config: []byte{}, // Would be actual config data
		// }
		//
		// scanResult, err = pm.securityManager.Scanner().ScanPackage(ctx, pkg)
		// if err != nil {
		// 	return nil, fmt.Errorf("security scan failed: %w", err)
		// }
		//
		// // Check if plugin passes security requirements
		// if scanResult.RiskLevel == security.RiskLevelCritical {
		// 	return nil, fmt.Errorf("plugin security scan failed: critical risk level")
		// }
	}

	// Validate dependencies
	if err := pm.validateDependencies(ctx, plugin); err != nil {
		return nil, fmt.Errorf("dependency validation failed: %w", err)
	}

	// Create secure environment for plugin
	securityConfig := security.PluginSecurityConfig{
		// NetworkAccess:               pm.shouldAllowNetworkAccess(pkg.Info),
		// ReadOnlyFileSystem:          pm.shouldUseReadOnlyFS(pkg.Info),
		// RequiresElevatedPermissions: pm.requiresElevatedPermissions(pkg.Info),
	}

	secureEnv, err := pm.securityManager.CreateSecurePluginEnvironment(ctx, plugin.ID(), securityConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create secure environment: %w", err)
	}

	// Create plugin entry
	entry := &PluginEntry{
		Plugin:       plugin,
		State:        PluginStateLoaded,
		LoadedAt:     time.Now(),
		Dependencies: pm.extractDependencies(plugin),
		Metrics:      PluginMetrics{},
		HealthScore:  1.0,
		SecurityReport: SecurityReport{
			PluginID:      plugin.ID(),
			LastScanTime:  time.Now(),
			ScanResult:    scanResult,
			SecurityScore: pm.calculateSecurityScore(scanResult),
			RiskLevel:     scanResult.RiskLevel,
		},
		SecureEnv: secureEnv,
		Sandbox:   secureEnv.Sandbox,
	}

	// Create plugin context
	entry.Context = NewPluginContext(ctx, plugin, pm.container)

	// Setup security for plugin
	if pm.config.EnableSecurity {
		if err := pm.setupPluginSecurity(ctx, entry); err != nil {
			return nil, fmt.Errorf("failed to setup plugin security: %w", err)
		}
	}

	// Initialize plugin
	if err := pm.initializePlugin(ctx, entry); err != nil {
		if entry.Sandbox != nil {
			entry.Sandbox.Destroy(ctx)
		}
		if entry.IsolatedProcess != nil {
			entry.IsolatedProcess.Stop(ctx)
		}
		return nil, fmt.Errorf("failed to initialize plugin: %w", err)
	}

	// Register plugin
	pm.plugins[plugin.ID()] = entry
	pm.registry.Register(plugin)

	// Update dependents
	pm.updateDependents(plugin.ID())

	// Update stats
	pm.updateStats()

	// Start monitoring plugin security
	if err := pm.securityManager.MonitorPluginSecurity(ctx, plugin.ID()); err != nil {
		pm.logger.Warn("failed to start security monitoring",
			logger.String("plugin_id", plugin.ID()),
			logger.Error(err),
		)
	}

	pm.logger.Info("plugin loaded with security",
		logger.String("plugin_id", plugin.ID()),
		logger.String("plugin_name", plugin.Name()),
		logger.String("plugin_version", plugin.Version()),
		logger.String("plugin_type", string(plugin.Type())),
		logger.String("risk_level", string(entry.SecurityReport.RiskLevel)),
		logger.Float64("security_score", entry.SecurityReport.SecurityScore),
		logger.Int("vulnerabilities", len(scanResult.Vulnerabilities)),
	)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.plugins.loaded").Inc()
		pm.metrics.Counter("forge.plugins.security_scanned").Inc()
		pm.metrics.Gauge("forge.plugins.total").Set(float64(len(pm.plugins)))
	}

	return plugin, nil
}

// AddPlugin adds a pre-instantiated plugin to the manager
func (pm *PluginManagerImpl) AddPlugin(ctx context.Context, plugin Plugin) error {
	// Only lock for the initial validation and state check
	pm.mu.Lock()
	if !pm.started {
		// pm.mu.Unlock()
		// return common.ErrServiceNotFound("plugin-manager")
	}

	pluginID := plugin.ID()
	pluginName := plugin.Name()

	// Check if plugin already exists
	if _, exists := pm.plugins[pluginID]; exists {
		pm.mu.Unlock()
		return common.ErrServiceAlreadyExists(pluginID)
	}
	pm.mu.Unlock()

	// Validate plugin WITHOUT holding the lock
	if err := pm.ValidatePlugin(ctx, plugin); err != nil {
		return fmt.Errorf("plugin validation failed: %w", err)
	}

	// Validate dependencies WITHOUT holding the lock
	if err := pm.validateDependencies(ctx, plugin); err != nil {
		return fmt.Errorf("dependency validation failed: %w", err)
	}

	// Security scanning and environment setup WITHOUT holding the lock
	var scanResult security.ScanResult
	var secureEnv *security.SecurePluginEnvironment
	if pm.config.EnableSecurity {
		securityConfig := security.PluginSecurityConfig{}
		var err error
		secureEnv, err = pm.securityManager.CreateSecurePluginEnvironment(ctx, pluginID, securityConfig)
		if err != nil {
			return fmt.Errorf("failed to create secure environment: %w", err)
		}

		scanResult = security.ScanResult{
			PluginID:        pluginID,
			RiskLevel:       security.RiskLevelLow,
			Vulnerabilities: []security.Vulnerability{},
			Warnings:        []security.SecurityWarning{},
			Metadata:        make(map[string]interface{}),
		}
	}

	// Create plugin entry WITHOUT holding the lock
	entry := &PluginEntry{
		Plugin:       plugin,
		State:        PluginStateLoaded,
		LoadedAt:     time.Now(),
		Dependencies: pm.extractDependencies(plugin),
		Metrics:      PluginMetrics{},
		HealthScore:  1.0,
		SecurityReport: SecurityReport{
			PluginID:      pluginID,
			LastScanTime:  time.Now(),
			ScanResult:    scanResult,
			SecurityScore: pm.calculateSecurityScore(scanResult),
			RiskLevel:     scanResult.RiskLevel,
		},
		SecureEnv: secureEnv,
	}

	if secureEnv != nil {
		entry.Sandbox = secureEnv.Sandbox
	}

	// Create plugin context
	entry.Context = NewPluginContext(ctx, plugin, pm.container)

	// Setup security for plugin if enabled WITHOUT holding the lock
	if pm.config.EnableSecurity {
		if err := pm.setupPluginSecurity(ctx, entry); err != nil {
			if entry.Sandbox != nil {
				entry.Sandbox.Destroy(ctx)
			}
			if entry.IsolatedProcess != nil {
				entry.IsolatedProcess.Stop(ctx)
			}
			return fmt.Errorf("failed to setup plugin security: %w", err)
		}
	}

	// Initialize plugin WITHOUT holding the lock
	if err := pm.initializePlugin(ctx, entry); err != nil {
		if entry.Sandbox != nil {
			entry.Sandbox.Destroy(ctx)
		}
		if entry.IsolatedProcess != nil {
			entry.IsolatedProcess.Stop(ctx)
		}
		return fmt.Errorf("failed to initialize plugin: %w", err)
	}

	// Register plugin components WITHOUT holding the main lock
	if err := pm.registerPluginComponents(ctx, plugin); err != nil {
		// Cleanup on failure
		if entry.Sandbox != nil {
			entry.Sandbox.Destroy(ctx)
		}
		if entry.IsolatedProcess != nil {
			entry.IsolatedProcess.Stop(ctx)
		}
		return fmt.Errorf("failed to register plugin components: %w", err)
	}

	// Only lock when actually updating the plugin registry
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check plugin doesn't exist (race condition protection)
	if _, exists := pm.plugins[pluginID]; exists {
		return common.ErrServiceAlreadyExists(pluginID)
	}

	// Register plugin
	pm.plugins[pluginID] = entry
	pm.registry.Register(plugin)

	// Update dependents
	pm.updateDependents(pluginID)

	// Update stats
	pm.updateStats()

	// Start monitoring plugin security if enabled
	if pm.config.EnableSecurity {
		if err := pm.securityManager.MonitorPluginSecurity(ctx, pluginID); err != nil {
			pm.logger.Warn("failed to start security monitoring",
				logger.String("plugin_id", pluginID),
				logger.Error(err),
			)
		}
	}

	pm.logger.Info("plugin added dynamically",
		logger.String("plugin_id", pluginID),
		logger.String("plugin_name", pluginName),
		logger.String("plugin_version", plugin.Version()),
		logger.String("plugin_type", string(plugin.Type())),
		logger.String("plugin_description", plugin.Description()),
		logger.String("risk_level", string(entry.SecurityReport.RiskLevel)),
		logger.Float64("security_score", entry.SecurityReport.SecurityScore),
		logger.String("dependencies", fmt.Sprintf("%v", pm.extractDependencyNames(plugin))),
	)

	if pm.metrics != nil {
		pm.metrics.Counter("forge.plugins.added").Inc()
		pm.metrics.Counter("forge.plugins.security_scanned").Inc()
		pm.metrics.Gauge("forge.plugins.total").Set(float64(len(pm.plugins)))
	}

	return nil
}

// Helper method to extract dependency names for logging
func (pm *PluginManagerImpl) extractDependencyNames(plugin Plugin) []string {
	var names []string
	for _, dep := range plugin.Dependencies() {
		names = append(names, fmt.Sprintf("%s@%s", dep.Name, dep.Version))
	}
	return names
}

// AddPluginWithConfig adds a plugin with custom configuration
func (pm *PluginManagerImpl) AddPluginWithConfig(ctx context.Context, plugin Plugin, config interface{}) error {
	// Configure the plugin before adding it
	if err := plugin.Configure(config); err != nil {
		return fmt.Errorf("failed to configure plugin: %w", err)
	}

	return pm.AddPlugin(ctx, plugin)
}

// AddPluginAndStart adds a plugin and immediately starts it if the manager is running
func (pm *PluginManagerImpl) AddPluginAndStart(ctx context.Context, plugin Plugin) error {
	if err := pm.AddPlugin(ctx, plugin); err != nil {
		return err
	}

	// If plugin manager is started, start the plugin too
	if pm.started {
		entry, exists := pm.plugins[plugin.ID()]
		if !exists {
			return fmt.Errorf("plugin was added but not found in registry")
		}

		if err := pm.startPlugin(ctx, entry); err != nil {
			pm.logger.Error("failed to start newly added plugin",
				logger.String("plugin_id", plugin.ID()),
				logger.Error(err),
			)
			return fmt.Errorf("plugin added but failed to start: %w", err)
		}

		pm.logger.Info("plugin added and started",
			logger.String("plugin_id", plugin.ID()),
			logger.String("plugin_name", plugin.Name()),
		)
	}

	return nil
}

func (pm *PluginManagerImpl) setupPluginSecurity(ctx context.Context, entry *PluginEntry) error {
	// pluginID := entry.Plugin.ID()

	// // Create sandbox with security configuration
	// sandboxConfig := pm.createSandboxConfig(entry.Plugin)
	// sandbox, err := pm.sandboxManager.CreateSandbox(ctx, sandboxConfig)
	// if err != nil {
	// 	return fmt.Errorf("failed to create sandbox: %w", err)
	// }
	// entry.Sandbox = sandbox
	//
	// // Create isolated process for the plugin
	// isolationConfig := pm.createIsolationConfig(entry.Plugin)
	// isolatedProcess, err := pm.isolationManager.CreateIsolatedProcess(ctx, isolationConfig)
	// if err != nil {
	// 	return fmt.Errorf("failed to create isolated process: %w", err)
	// }
	// entry.IsolatedProcess = isolatedProcess
	//
	// // Assign default role based on plugin type
	// defaultRole := pm.getDefaultRoleForPlugin(entry.Plugin)
	// if err := pm.permissionManager.AssignRole(ctx, pluginID, defaultRole); err != nil {
	// 	return fmt.Errorf("failed to assign default role: %w", err)
	// }

	// Grant additional permissions based on plugin requirements
	if err := pm.grantPluginPermissions(ctx, entry); err != nil {
		return fmt.Errorf("failed to grant plugin permissions: %w", err)
	}

	return nil
}

func (pm *PluginManagerImpl) createSandboxConfig(plugin Plugin) security.SandboxConfig {
	config := security.SandboxConfig{
		PluginID:         plugin.ID(),
		Isolated:         true,
		MaxMemory:        pm.configManager.GetInt64("plugins.security.max_memory", 1024*1024*1024), // 1GB default
		MaxCPU:           pm.configManager.GetFloat64("plugins.security.max_cpu", 0.5),             // 50% default
		MaxDiskSpace:     pm.configManager.GetInt64("plugins.security.max_disk", 100*1024*1024),    // 100MB default
		Timeout:          pm.configManager.GetDuration("plugins.security.timeout", 30*time.Second),
		WorkingDirectory: fmt.Sprintf("/tmp/forge/plugins/%s", plugin.ID()),
		NetworkAccess: security.NetworkPolicy{
			Allowed:   pm.configManager.GetBool("plugins.security.network_allowed", true),
			MaxConns:  pm.configManager.GetInt("plugins.security.max_connections", 10),
			Bandwidth: pm.configManager.GetInt64("plugins.security.max_bandwidth", 1024*1024), // 1MB/s
		},
		FileSystemAccess: security.FileSystemPolicy{
			ReadOnly:     pm.configManager.GetBool("plugins.security.readonly_fs", false),
			MaxFileSize:  pm.configManager.GetInt64("plugins.security.max_file_size", 10*1024*1024), // 10MB
			MaxFiles:     pm.configManager.GetInt("plugins.security.max_files", 100),
			NoDevAccess:  true,
			NoProcAccess: true,
			NoSysAccess:  true,
		},
		Environment: map[string]string{
			"FORGE_PLUGIN_ID":      plugin.ID(),
			"FORGE_PLUGIN_NAME":    plugin.Name(),
			"FORGE_PLUGIN_VERSION": plugin.Version(),
			"FORGE_SECURITY_MODE":  "strict",
		},
	}

	// Customize based on plugin type
	switch plugin.Type() {
	case common2.PluginEngineTypeService:
		config.NetworkAccess.Allowed = true
		config.MaxMemory = config.MaxMemory * 2 // Services get more memory
	case common2.PluginEngineTypeMiddleware:
		config.NetworkAccess.Allowed = true
		config.FileSystemAccess.ReadOnly = false // Middleware might need to write
	case common2.PluginEngineTypeHandler:
		config.NetworkAccess.Allowed = true
	case common2.PluginEngineTypeFilter:
		config.MaxCPU = 0.25 // Filters get less CPU
	}

	return config
}

func (pm *PluginManagerImpl) createIsolationConfig(plugin Plugin) security.IsolationConfig {
	return security.IsolationConfig{
		ProcessID:  plugin.ID(),
		Command:    "/usr/bin/forge-plugin-runner",
		Args:       []string{"--plugin-id", plugin.ID()},
		WorkingDir: fmt.Sprintf("/tmp/forge/plugins/%s", plugin.ID()),
		Environment: map[string]string{
			"FORGE_PLUGIN_ID": plugin.ID(),
			"PATH":            "/usr/bin:/bin",
		},
		Privileges: security.PrivilegeConfig{
			DropCapabilities: []string{"CAP_SYS_ADMIN", "CAP_NET_ADMIN", "CAP_SYS_PTRACE"},
			NoNewPrivileges:  true,
			Seccomp:          true,
		},
		Resources: security.ResourceLimits{
			Memory: security.MemoryLimits{
				Limit: pm.configManager.GetInt64("plugins.security.max_memory", 1024*1024*1024),
			},
			CPU: security.CPULimits{
				Cores: pm.configManager.GetFloat64("plugins.security.max_cpu", 0.5),
			},
		},
		Namespaces: security.NamespaceConfig{
			PID:     true,
			Network: false, // Controlled by sandbox
			Mount:   true,
			UTS:     true,
			IPC:     true,
			User:    true,
		},
		Timeout: pm.configManager.GetDuration("plugins.security.timeout", 30*time.Second),
	}
}

func (pm *PluginManagerImpl) getDefaultRoleForPlugin(plugin Plugin) string {
	switch plugin.Type() {
	case common2.PluginEngineTypeService, common2.PluginEngineTypeMiddleware:
		return "plugin-advanced"
	default:
		return "plugin-basic"
	}
}

func (pm *PluginManagerImpl) grantPluginPermissions(ctx context.Context, entry *PluginEntry) error {
	// pluginID := entry.Plugin.ID()
	//
	// // Grant permissions based on plugin requirements
	// for _, requirement := range entry.Plugin.Requirements() {
	// 	permission := security.Permission{
	// 		Name:      fmt.Sprintf("%s-%s", requirement.Type, requirement.Name),
	// 		Resource:  requirement.Resource,
	// 		Action:    requirement.Action,
	// 		GrantedAt: time.Now(),
	// 		GrantedBy: "plugin-manager",
	// 		Scope: security.PermissionScope{
	// 			Type:      security.ScopeTypeResource,
	// 			Resources: []string{requirement.Resource},
	// 		},
	// 	}
	//
	// 	if err := pm.permissionManager.GrantPermission(ctx, pluginID, permission); err != nil {
	// 		return fmt.Errorf("failed to grant permission %s: %w", permission.Name, err)
	// 	}
	//
	// 	entry.Permissions = append(entry.Permissions, permission)
	// }

	return nil
}

// ExecutePluginOperation executes a plugin operation with security checks
func (pm *PluginManagerImpl) ExecutePluginOperation(ctx context.Context, pluginID string, operation PluginOperation) (interface{}, error) {
	pm.mu.RLock()
	entry, exists := pm.plugins[pluginID]
	pm.mu.RUnlock()

	if !exists {
		return nil, common.ErrServiceNotFound(pluginID)
	}

	if !pm.config.EnableSecurity {
		// Direct execution without security
		return pm.executePluginOperationDirect(ctx, entry, operation)
	}

	// Security enforcement
	policyOperation := security.PolicyOperation{
		Type:       security.OperationType(operation.Type),
		PluginID:   pluginID,
		Resource:   operation.Target,
		Action:     string(operation.Type),
		Parameters: operation.Parameters,
		// Context:    operation.Context,
		Timestamp: time.Now(),
		// RequestID:  operation.ID,
	}

	// Enforce security policies
	if err := pm.securityManager.PolicyEnforcer().EnforcePolicy(ctx, pluginID, policyOperation); err != nil {
		pm.logger.Warn("policy enforcement failed",
			logger.String("plugin_id", pluginID),
			logger.String("operation", string(operation.Type)),
			logger.Error(err),
		)
		return nil, fmt.Errorf("security policy violation: %w", err)
	}

	// Check permissions
	if err := pm.securityManager.PermissionManager().CheckPermission(ctx, pluginID, operation.Target, string(operation.Type)); err != nil {
		return nil, fmt.Errorf("permission denied: %w", err)
	}

	// Execute in sandbox
	result, err := entry.Sandbox.Execute(ctx, entry.Plugin, operation)
	if err != nil {
		return nil, fmt.Errorf("sandbox execution failed: %w", err)
	}

	// Update metrics
	entry.mu.Lock()
	entry.Metrics.CallCount++
	entry.Metrics.LastExecuted = time.Now()
	entry.mu.Unlock()

	pm.logger.Debug("plugin operation executed securely",
		logger.String("plugin_id", pluginID),
		logger.String("operation", string(operation.Type)),
		logger.String("target", operation.Target),
	)

	return result, nil
}

func (pm *PluginManagerImpl) executePluginOperationDirect(ctx context.Context, entry *PluginEntry, operation PluginOperation) (interface{}, error) {
	// Direct execution without security (for development/testing)
	switch operation.Type {
	case common2.OperationTypeLoad:
		return nil, entry.Plugin.Initialize(ctx, pm.container)
	case common2.OperationTypeStart:
		return nil, entry.Plugin.OnStart(ctx)
	case common2.OperationTypeStop:
		return nil, entry.Plugin.OnStop(ctx)
	default:
		return nil, fmt.Errorf("unsupported operation type: %s", operation.Type)
	}
}

// GetSecurityReport returns a security report for a plugin
func (pm *PluginManagerImpl) GetSecurityReport(pluginID string) (SecurityReport, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	entry, exists := pm.plugins[pluginID]
	if !exists {
		return SecurityReport{}, common.ErrServiceNotFound(pluginID)
	}

	// Update security report with latest data
	if pm.config.EnableSecurity {
		pm.updateSecurityReport(entry)
	}

	return entry.SecurityReport, nil
}

func (pm *PluginManagerImpl) updateSecurityReport(entry *PluginEntry) {
	pluginID := entry.Plugin.ID()

	// Get latest compliance report
	if complianceReport, err := pm.securityManager.PolicyEnforcer().CheckCompliance(context.Background(), pluginID); err == nil {
		entry.SecurityReport.ComplianceReport = complianceReport
	}

	// Get permission audit log
	if auditLog, err := pm.securityManager.PermissionManager().GetAuditLog(context.Background(), pluginID); err == nil {
		entry.SecurityReport.PermissionAudit = auditLog
	}

	// Get policy violations
	if violations, err := pm.securityManager.PolicyEnforcer().GetViolations(context.Background(), pluginID); err == nil {
		entry.SecurityReport.Violations = violations
	}

	// Get isolation metrics
	if entry.Sandbox != nil {
		if metrics, err := entry.Sandbox.Monitor(context.Background(), entry.Plugin); err == nil {
			entry.SecurityReport.IsolationMetrics = metrics
		}
	}

	// Update security score
	entry.SecurityReport.SecurityScore = pm.calculateSecurityScore(entry.SecurityReport.ScanResult)
}

func (pm *PluginManagerImpl) calculateSecurityScore(scanResult security.ScanResult) float64 {
	baseScore := 100.0

	// Deduct points for vulnerabilities
	for _, vuln := range scanResult.Vulnerabilities {
		switch vuln.Severity {
		case security.SeverityCritical:
			baseScore -= 25.0
		case security.SeverityHigh:
			baseScore -= 15.0
		case security.SeverityMedium:
			baseScore -= 10.0
		case security.SeverityLow:
			baseScore -= 5.0
		}
	}

	// Deduct points for warnings
	for _, warning := range scanResult.Warnings {
		switch warning.Severity {
		case security.SeverityCritical:
			baseScore -= 10.0
		case security.SeverityHigh:
			baseScore -= 5.0
		case security.SeverityMedium:
			baseScore -= 3.0
		case security.SeverityLow:
			baseScore -= 1.0
		}
	}

	if baseScore < 0 {
		baseScore = 0
	}

	return baseScore
}

// UpdateSecurityPolicies updates security policies
func (pm *PluginManagerImpl) UpdateSecurityPolicies(ctx context.Context) error {
	if !pm.config.EnableSecurity {
		return nil
	}

	// Update vulnerability database
	if err := pm.securityManager.Scanner().UpdateVulnerabilityDatabase(ctx); err != nil {
		return fmt.Errorf("failed to update vulnerability database: %w", err)
	}

	// // Reload security policies
	// if err := pm.loadSecurityPolicies(ctx); err != nil {
	// 	return fmt.Errorf("failed to reload security policies: %w", err)
	// }

	pm.logger.Info("security policies updated")
	return nil
}

// Helper methods for initialization and cleanup
func (pm *PluginManagerImpl) initializePlugin(ctx context.Context, entry *PluginEntry) error {
	if pm.config.EnableSecurity && entry.Sandbox != nil {
		// Initialize plugin in sandbox
		operation := PluginOperation{
			Type:       common2.OperationTypeLoad,
			Target:     entry.Plugin.ID(),
			Parameters: map[string]interface{}{"container": pm.container},
			Timeout:    30 * time.Second,
		}

		_, err := entry.Sandbox.Execute(ctx, entry.Plugin, operation)
		if err != nil {
			return err
		}
	} else {
		// Direct initialization
		if err := entry.Plugin.Initialize(ctx, pm.container); err != nil {
			return err
		}
	}

	entry.State = PluginStateInitialized
	return nil
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

	// Unregister all plugin components BEFORE plugin cleanup
	pm.unregisterPluginComponents(ctx, pluginID)

	// Cleanup plugin
	if err := entry.Plugin.Cleanup(ctx); err != nil {
		pm.logger.Error("failed to cleanup plugin", logger.Error(err))
	}

	// Destroy sandbox
	if entry.Sandbox != nil {
		if err := entry.Sandbox.Destroy(ctx); err != nil {
			pm.logger.Error("failed to destroy sandbox", logger.Error(err))
		}
	}

	// Unregister from registry
	pm.registry.Unregister(pluginID)

	// Remove from plugins map
	delete(pm.plugins, pluginID)

	// Update dependents
	pm.removeDependents(pluginID)

	// Update stats
	pm.updateStats()

	pm.logger.Info("plugin unloaded and components cleaned up",
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
func (pm *PluginManagerImpl) GetPluginStore() store.PluginStore {
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
	if _, err := pm.securityManager.Scanner().ScanPackage(ctx, pkg); err != nil {
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
	return nil
	// return pm.validator.ValidatePlugin(ctx, plugin)
}

// GetStats returns plugin manager statistics
func (pm *PluginManagerImpl) GetStats() PluginManagerStats {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	pm.updateStats()
	return pm.stats
}

func (pm *PluginManagerImpl) startPlugin(ctx context.Context, entry *PluginEntry) error {
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if entry.State == PluginStateStarted {
		return nil
	}

	// Start plugin
	if err := entry.Plugin.OnStart(ctx); err != nil {
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
	if err := entry.Plugin.OnStop(ctx); err != nil {
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
	autoLoadPlugins := pm.configManager.GetString("plugins.auto_load")
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

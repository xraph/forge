package forge

import (
	"context"
	"os"
	"time"
)

// App represents a Forge application with lifecycle management.
type App interface {
	// Core components
	Container() Container
	Router() Router
	Config() ConfigManager
	Logger() Logger
	Metrics() Metrics
	HealthManager() HealthManager
	LifecycleManager() LifecycleManager

	// Lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Run() error // Blocks until shutdown signal

	// Registration
	RegisterService(name string, factory Factory, opts ...RegisterOption) error
	RegisterController(controller Controller) error
	RegisterExtension(ext Extension) error

	// Lifecycle hooks - convenience methods
	RegisterHook(phase LifecyclePhase, hook LifecycleHook, opts LifecycleHookOptions) error
	RegisterHookFn(phase LifecyclePhase, name string, hook LifecycleHook) error

	// Information
	Name() string
	Version() string
	Environment() string
	StartTime() time.Time
	Uptime() time.Duration

	// Extensions
	Extensions() []Extension
	GetExtension(name string) (Extension, error)
}

// AppConfig configures the application.
type AppConfig struct {
	// Basic info
	Name        string
	Version     string
	Description string
	Environment string // "development", "staging", "production"

	// Components
	ConfigManager ConfigManager
	Logger        Logger
	Metrics       Metrics

	// Router options
	RouterOptions []RouterOption

	// Observability
	MetricsConfig MetricsConfig
	HealthConfig  HealthConfig

	ErrorHandler ErrorHandler

	// Server
	HTTPAddress string        // Default: ":8080"
	HTTPTimeout time.Duration // Default: 30s

	// Shutdown
	ShutdownTimeout time.Duration // Default: 30s
	ShutdownSignals []os.Signal   // Default: SIGINT, SIGTERM

	// Extensions
	Extensions []Extension // Extensions to register with the app

	// Config Auto-Discovery
	// If ConfigManager is not provided, these options control auto-discovery
	EnableConfigAutoDiscovery bool     // Enable automatic config file discovery (default: true)
	ConfigSearchPaths         []string // Paths to search for config files (default: current directory)
	ConfigBaseNames           []string // Base config file names (default: ["config.yaml", "config.yml"])
	ConfigLocalNames          []string // Local config file names (default: ["config.local.yaml", "config.local.yml"])
	EnableAppScopedConfig     bool     // Enable app-scoped config extraction for monorepos (default: true)

	// Environment Variable Config Sources
	// These options control how environment variables are loaded as config sources
	EnableEnvConfig  bool   // Enable loading config from environment variables (default: true)
	EnvPrefix        string // Prefix for environment variables (default: app name uppercase, e.g., "MYAPP_")
	EnvSeparator     string // Separator for nested keys in env vars (default: "_")
	EnvOverridesFile bool   // Whether env vars override file config values (default: true)
}

// DefaultAppConfig returns a default application configuration.
func DefaultAppConfig() AppConfig {
	// Get proper default health config with all fields set
	healthConfig := DefaultHealthConfig()
	healthConfig.Intervals.Check = 30 * time.Second
	healthConfig.Intervals.Report = 60 * time.Second
	healthConfig.Features.AutoDiscovery = true
	healthConfig.Performance.MaxConcurrentChecks = 10
	healthConfig.Performance.DefaultTimeout = 5 * time.Second
	healthConfig.Features.Aggregation = true
	healthConfig.Performance.HistorySize = 100

	return AppConfig{
		Name:                      "forge-app",
		Version:                   "1.0.0",
		Description:               "Forge Application",
		Environment:               "development",
		HTTPAddress:               ":8080",
		HTTPTimeout:               30 * time.Second,
		ShutdownTimeout:           30 * time.Second,
		ShutdownSignals:           nil, // Will use default SIGINT, SIGTERM
		RouterOptions:             nil,
		MetricsConfig:             DefaultMetricsConfig(),
		HealthConfig:              healthConfig,
		EnableConfigAutoDiscovery: true,
		EnableAppScopedConfig:     true,
		ConfigBaseNames:           []string{"config.yaml", "config.yml"},
		ConfigLocalNames:          []string{"config.local.yaml", "config.local.yml"},
		// Environment variable source defaults
		EnableEnvConfig:  true, // Enabled by default
		EnvSeparator:     "_",  // Standard separator
		EnvOverridesFile: true, // Env takes precedence over files by default
	}
}

// AppOption is a functional option for AppConfig.
type AppOption func(*AppConfig)

// WithConfig replaces the entire config.
func WithConfig(config AppConfig) AppOption {
	return func(c *AppConfig) {
		*c = config
	}
}

// WithAppName sets the application name.
func WithAppName(name string) AppOption {
	return func(c *AppConfig) {
		c.Name = name
	}
}

// WithAppVersion sets the application version.
func WithAppVersion(version string) AppOption {
	return func(c *AppConfig) {
		c.Version = version
	}
}

// WithAppDescription sets the application description.
func WithAppDescription(description string) AppOption {
	return func(c *AppConfig) {
		c.Description = description
	}
}

// WithAppEnvironment sets the application environment.
func WithAppEnvironment(environment string) AppOption {
	return func(c *AppConfig) {
		c.Environment = environment
	}
}

// WithAppConfigManager sets the config manager.
func WithAppConfigManager(configManager ConfigManager) AppOption {
	return func(c *AppConfig) {
		c.ConfigManager = configManager
	}
}

// WithAppMetrics sets the metrics provider.
func WithAppMetrics(metrics Metrics) AppOption {
	return func(c *AppConfig) {
		c.Metrics = metrics
	}
}

// WithAppLogger sets the logger.
func WithAppLogger(logger Logger) AppOption {
	return func(c *AppConfig) {
		c.Logger = logger
	}
}

// WithAppRouterOptions sets the router options.
func WithAppRouterOptions(opts ...RouterOption) AppOption {
	return func(c *AppConfig) {
		c.RouterOptions = opts
	}
}

// WithAppMetricsConfig sets the metrics configuration.
func WithAppMetricsConfig(config MetricsConfig) AppOption {
	return func(c *AppConfig) {
		c.MetricsConfig = config
	}
}

// WithAppHealthConfig sets the health configuration.
func WithAppHealthConfig(config HealthConfig) AppOption {
	return func(c *AppConfig) {
		c.HealthConfig = config
	}
}

// WithAppErrorHandler sets the error handler.
func WithAppErrorHandler(handler ErrorHandler) AppOption {
	return func(c *AppConfig) {
		c.ErrorHandler = handler
	}
}

// WithHTTPAddress sets the HTTP address.
func WithHTTPAddress(address string) AppOption {
	return func(c *AppConfig) {
		c.HTTPAddress = address
	}
}

// WithHTTPTimeout sets the HTTP timeout.
func WithHTTPTimeout(timeout time.Duration) AppOption {
	return func(c *AppConfig) {
		c.HTTPTimeout = timeout
	}
}

// WithShutdownTimeout sets the shutdown timeout.
func WithShutdownTimeout(timeout time.Duration) AppOption {
	return func(c *AppConfig) {
		c.ShutdownTimeout = timeout
	}
}

// WithShutdownSignals sets the shutdown signals.
func WithShutdownSignals(signals ...os.Signal) AppOption {
	return func(c *AppConfig) {
		c.ShutdownSignals = signals
	}
}

// WithExtensions sets the extensions.
func WithExtensions(extensions ...Extension) AppOption {
	return func(c *AppConfig) {
		c.Extensions = extensions
	}
}

// WithEnableConfigAutoDiscovery enables or disables config auto-discovery.
func WithEnableConfigAutoDiscovery(enabled bool) AppOption {
	return func(c *AppConfig) {
		c.EnableConfigAutoDiscovery = enabled
	}
}

// WithConfigSearchPaths sets the config search paths.
func WithConfigSearchPaths(paths ...string) AppOption {
	return func(c *AppConfig) {
		c.ConfigSearchPaths = paths
	}
}

// WithConfigBaseNames sets the config base names.
func WithConfigBaseNames(names ...string) AppOption {
	return func(c *AppConfig) {
		c.ConfigBaseNames = names
	}
}

// WithConfigLocalNames sets the config local names.
func WithConfigLocalNames(names ...string) AppOption {
	return func(c *AppConfig) {
		c.ConfigLocalNames = names
	}
}

// WithEnableAppScopedConfig enables or disables app-scoped config.
func WithEnableAppScopedConfig(enabled bool) AppOption {
	return func(c *AppConfig) {
		c.EnableAppScopedConfig = enabled
	}
}

// WithEnableEnvConfig enables or disables environment variable config source.
func WithEnableEnvConfig(enabled bool) AppOption {
	return func(c *AppConfig) {
		c.EnableEnvConfig = enabled
	}
}

// WithEnvPrefix sets the prefix for environment variables.
// If not set, defaults to the app name in uppercase with trailing underscore.
func WithEnvPrefix(prefix string) AppOption {
	return func(c *AppConfig) {
		c.EnvPrefix = prefix
	}
}

// WithEnvSeparator sets the separator for nested keys in environment variables.
// Default is "_".
func WithEnvSeparator(separator string) AppOption {
	return func(c *AppConfig) {
		c.EnvSeparator = separator
	}
}

// WithEnvOverridesFile controls whether environment variables override file config values.
// Default is true (env vars take precedence over file config).
func WithEnvOverridesFile(override bool) AppOption {
	return func(c *AppConfig) {
		c.EnvOverridesFile = override
	}
}

// NewApp creates a new Forge application.
func NewApp(config AppConfig) App {
	return newApp(config)
}

// NewWithConfig creates a new Forge application with a complete config.
func NewWithConfig(config AppConfig) App {
	return newApp(config)
}

// New creates a new Forge application with variadic options.
func New(opts ...AppOption) App {
	config := DefaultAppConfig()
	for _, opt := range opts {
		opt(&config)
	}

	return newApp(config)
}

// AppInfo represents application information returned by /_/info endpoint.
type AppInfo struct {
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Description string          `json:"description"`
	Environment string          `json:"environment"`
	StartTime   time.Time       `json:"start_time"`
	Uptime      time.Duration   `json:"uptime"`
	GoVersion   string          `json:"go_version"`
	Services    []string        `json:"services"`
	Routes      int             `json:"routes"`
	Extensions  []ExtensionInfo `json:"extensions,omitempty"`
}

package forge

import (
	"context"
	"net/http"
	"os"
	"strings"
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

	// Configuration queries
	MigrationsDisabled() bool

	// SetMigrationsDisabled overrides the DisableMigrations config flag at
	// runtime. CLI commands call this before app.Start() to prevent the
	// PhaseAfterRegister forward-migration hook from running when rolling back
	// or inspecting status.
	SetMigrationsDisabled(v bool)

	// CentralMigrationsEnabled reports whether the single-pass migration
	// lifecycle is enabled.
	CentralMigrationsEnabled() bool

	// CentralMigrator resolves the CentralMigrator registered in the DI
	// container (ok=true) or returns nil, false when nothing has been
	// contributed (e.g. no grove extension or CentralMigrations is off).
	CentralMigrator() (CentralMigrator, bool)
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
	MetricsConfig       MetricsConfig
	HealthConfig        HealthConfig
	TracingConfig       TracingConfig
	MetricsExportConfig MetricsExportConfig
	LogsExportConfig    LogsExportConfig
	ProfilingConfig     ProfilingConfig

	// Profiling
	EnablePprof bool   // Enable pprof profiling endpoints at /_/debug/pprof (default: false)
	PprofPrefix string // URL prefix for pprof endpoints (default: "/_/debug/pprof")

	// PprofGuard authorizes access to the pprof endpoints. It is called before
	// every pprof request; return false to deny (the caller gets a 404, so the
	// endpoints are not discoverable).
	//
	// Strongly recommended whenever EnablePprof is on outside a local dev loop.
	// The endpoints expose heap and goroutine contents, allow a caller to pin a
	// CPU for an arbitrary duration, and /cmdline discloses the process argv —
	// which commonly carries credentials passed as flags.
	//
	// Nil means no authorization check.
	PprofGuard func(*http.Request) bool

	ErrorHandler ErrorHandler

	// Server
	HTTPAddress string        // Default: ":8080"
	HTTPTimeout time.Duration // Default: 30s

	// MaxRequestBodySize caps request bodies in bytes. Bodies larger than this
	// are rejected instead of being buffered, so a single request cannot drive
	// unbounded allocation. Default: 10 MiB (router.DefaultMaxRequestBodySize).
	// Negative disables the limit; override per route with WithMaxBodySize.
	MaxRequestBodySize int64

	// WebSocketOrigins is the Origin allow-list for WebSocket upgrades. Empty
	// means same-origin only — browsers send cookies with upgrades but do not
	// apply CORS to them, so an open upgrade endpoint is hijackable by any site
	// the user visits. Entries may be "https://app.example.com", "app.example.com",
	// "*.example.com", or "*" to allow any origin.
	WebSocketOrigins []string

	// HTTPWriteTimeout bounds how long the server spends writing one response.
	// A client that requests a large body and never reads it otherwise pins a
	// handler goroutine and a connection forever. Responses that stream (a
	// WebSocket or other Upgrade, an SSE request, gRPC, or any route the router
	// classifies as a stream) are exempt, so long-lived connections keep
	// working. Zero means the default of 60s; negative disables the deadline.
	HTTPWriteTimeout time.Duration

	// Shutdown
	ShutdownTimeout time.Duration // HTTP drain budget. Default: 30s
	ShutdownSignals []os.Signal   // Default: SIGINT, SIGTERM

	// StopTimeout bounds the second half of a graceful shutdown: lifecycle
	// hooks, extension stops and the DI container. It is separate from
	// ShutdownTimeout, which the HTTP drain spends, so a slow drain cannot
	// leave the extensions with no time at all. A step that overruns is
	// abandoned and logged instead of blocking shutdown forever. Zero means
	// the same value as ShutdownTimeout.
	StopTimeout time.Duration

	// ObservabilityGuard authorizes access to the detailed observability
	// endpoints: the per-check detail of /_/health, /_/info and /_/metrics.
	// Return false to deny. A denied /_/health caller still gets the overall
	// status and the status of each check, without messages or details, and a
	// denied /_/info or /_/metrics caller gets a 404. /_/health/live and
	// /_/health/ready never consult the guard, so orchestrator probes keep
	// working without credentials.
	//
	// Nil means /_/metrics and an enabled /_/info are open, and /_/health
	// shows details only when Environment is explicitly a development one
	// (development, dev, local or test).
	ObservabilityGuard func(*http.Request) bool

	// EnableInfoEndpoint registers /_/info, which lists the services,
	// extensions, versions and Go version the app runs. Default: false.
	EnableInfoEndpoint bool

	// MetricsScrapeCacheTTL is how long a rendered /_/metrics response is
	// reused. Concurrent scrapes inside the window share one render, so a
	// burst of scrapers costs one gather and one encode. Zero means the
	// default of 1s; negative disables the cache.
	MetricsScrapeCacheTTL time.Duration

	// ExposeInternalErrors decides whether 5xx responses carry the internal
	// error text. Nil means it follows Environment: only an explicitly set
	// development, dev, local or test environment exposes it. An unset or
	// unrecognised environment, including staging and prod, redacts.
	ExposeInternalErrors *bool

	// Health
	// HealthGracePeriod is how long after startup /_/health answers 200 with
	// status "starting" whatever the checks say, so a platform that uses it as
	// a liveness probe does not kill an app whose dependencies are still
	// warming up. Readiness (/_/health/ready) ignores it and reports the real
	// result, so traffic is not routed to an instance that cannot serve it.
	// Default: 60s
	HealthGracePeriod time.Duration

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

	// Database / Migration
	DisableMigrations bool // When true, skip auto-migrations on serve (default: false). Also settable via .forge.yaml database.disable_migrations.

	// CentralMigrations runs all MigratableExtension migrations as a single
	// ordered pass (Register-all -> migrate -> Start-all) instead of letting
	// each extension migrate independently. Default: false. Also settable via
	// .forge.yaml database.central_migrations.
	CentralMigrations bool

	// ParallelExtensionStartup starts extensions that do not depend on each
	// other at the same time instead of one after another. Extensions are
	// grouped into dependency levels; a level starts only once every level
	// below it has finished, so an extension still sees its dependencies fully
	// registered and started.
	//
	// Default: false. Serial startup is the historical behaviour, and an
	// extension that depends on another without declaring it gets away with it
	// as long as the topological order happens to favour it. Turning this on
	// surfaces that as a startup race rather than hiding it, so switch it on
	// once and watch a few boots before relying on it.
	ParallelExtensionStartup bool

	// metricsConfigSet records a WithAppMetricsConfig call, so a config equal
	// to the defaults still counts as set in code.
	metricsConfigSet bool

	// environmentDefaulted marks the "development" that DefaultAppConfig puts
	// in Environment as a placeholder rather than a choice. Security defaults
	// treat it as unset; see environmentIsDevelopment.
	environmentDefaulted bool
}

// DefaultAppConfig returns a default application configuration.
func DefaultAppConfig() AppConfig {
	// Get proper default health config with all fields set
	healthConfig := DefaultHealthConfig()
	healthConfig.Intervals.Check = 30 * time.Second
	healthConfig.Intervals.Report = 60 * time.Second
	healthConfig.Features.AutoDiscovery = false
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
		HealthGracePeriod:         60 * time.Second,
		EnableConfigAutoDiscovery: true,
		EnableAppScopedConfig:     true,
		ConfigBaseNames:           []string{"config.yaml", "config.yml"},
		ConfigLocalNames:          []string{"config.local.yaml", "config.local.yml"},
		// Environment variable source defaults
		EnableEnvConfig:  true, // Enabled by default
		EnvSeparator:     "_",  // Standard separator
		EnvOverridesFile: true, // Env takes precedence over files by default

		environmentDefaulted: true,
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
		c.environmentDefaulted = false
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

// WithAppMetricsConfig sets the metrics configuration. Values set here win
// over the metrics block in the forge config file. Without it the defaults
// apply and the file may change any of them.
func WithAppMetricsConfig(config MetricsConfig) AppOption {
	return func(c *AppConfig) {
		c.MetricsConfig = config
		c.metricsConfigSet = true
	}
}

// WithAppTracingConfig sets the tracing configuration. Values set here win
// over the forge config file and OTEL_* environment variables.
func WithAppTracingConfig(config TracingConfig) AppOption {
	return func(c *AppConfig) {
		c.TracingConfig = config
	}
}

// WithAppProfilingConfig sets the profiling configuration. Values set here win
// over the forge config file and PYROSCOPE_* environment variables.
func WithAppProfilingConfig(config ProfilingConfig) AppOption {
	return func(c *AppConfig) {
		c.ProfilingConfig = config
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

// WithDisableMigrations disables auto-migrations on serve.
// This can also be set via .forge.yaml under database.disable_migrations.
func WithDisableMigrations() AppOption {
	return func(c *AppConfig) { c.DisableMigrations = true }
}

// WithCentralMigrations enables the single-pass, dependency-ordered migration
// lifecycle (Register-all -> migrate -> Start-all).
func WithCentralMigrations() AppOption {
	return func(c *AppConfig) { c.CentralMigrations = true }
}

// WithParallelExtensionStartup starts independent extensions concurrently.
// See AppConfig.ParallelExtensionStartup for what that does and does not
// guarantee.
func WithParallelExtensionStartup() AppOption {
	return func(c *AppConfig) { c.ParallelExtensionStartup = true }
}

// WithPprof enables pprof profiling endpoints.
// Endpoints are registered at /_/debug/pprof by default.
// Only enable in development or staging — never in production.
func WithPprof() AppOption {
	return func(c *AppConfig) { c.EnablePprof = true }
}

// WithPprofPrefix sets a custom URL prefix for pprof endpoints.
// Implies WithPprof(). Default is "/_/debug/pprof".
func WithPprofPrefix(prefix string) AppOption {
	return func(c *AppConfig) {
		c.EnablePprof = true
		c.PprofPrefix = prefix
	}
}

// WithPprofGuard restricts access to the pprof endpoints. The guard runs before
// every pprof request; returning false yields a 404 so the endpoints stay
// undiscoverable. Implies WithPprof().
//
// Use this whenever profiling is enabled on anything reachable beyond localhost:
// the endpoints dump heap and goroutine state, let a caller pin a CPU for an
// arbitrary duration, and /cmdline reveals the process argv.
//
//	forge.WithPprofGuard(func(r *http.Request) bool {
//	    return subtle.ConstantTimeCompare(
//	        []byte(r.Header.Get("X-Debug-Token")), []byte(token)) == 1
//	})
func WithPprofGuard(guard func(*http.Request) bool) AppOption {
	return func(c *AppConfig) {
		c.EnablePprof = true
		c.PprofGuard = guard
	}
}

// WithMaxRequestBodySize caps request bodies app-wide, in bytes. Defaults to
// 10 MiB; pass a negative value to disable the limit. Override per route with
// WithMaxBodySize.
func WithMaxRequestBodySize(bytes int64) AppOption {
	return func(c *AppConfig) { c.MaxRequestBodySize = bytes }
}

// WithWebSocketOrigins sets the Origin allow-list for WebSocket upgrades.
// Without it only same-origin upgrades are accepted — see AppConfig.WebSocketOrigins.
func WithWebSocketOrigins(origins ...string) AppOption {
	return func(c *AppConfig) {
		c.WebSocketOrigins = append(c.WebSocketOrigins, origins...)
	}
}

// WithHTTPWriteTimeout bounds how long the server spends writing one
// non-streaming response. See AppConfig.HTTPWriteTimeout. Pass a negative
// value to disable it.
func WithHTTPWriteTimeout(timeout time.Duration) AppOption {
	return func(c *AppConfig) { c.HTTPWriteTimeout = timeout }
}

// WithStopTimeout sets the budget for stopping hooks, extensions and the DI
// container during shutdown. See AppConfig.StopTimeout.
func WithStopTimeout(timeout time.Duration) AppOption {
	return func(c *AppConfig) { c.StopTimeout = timeout }
}

// WithObservabilityGuard restricts the detailed observability endpoints. The
// guard runs on every request to /_/health, /_/info and /_/metrics; see
// AppConfig.ObservabilityGuard for what a denied caller sees.
//
//	forge.WithObservabilityGuard(func(r *http.Request) bool {
//	    return subtle.ConstantTimeCompare(
//	        []byte(r.Header.Get("X-Ops-Token")), []byte(token)) == 1
//	})
func WithObservabilityGuard(guard func(*http.Request) bool) AppOption {
	return func(c *AppConfig) { c.ObservabilityGuard = guard }
}

// WithInfoEndpoint registers /_/info. It is off by default because it lists
// the app's services, extensions and versions to whoever asks; pair it with
// WithObservabilityGuard anywhere beyond localhost.
func WithInfoEndpoint() AppOption {
	return func(c *AppConfig) { c.EnableInfoEndpoint = true }
}

// WithMetricsScrapeCacheTTL sets how long a rendered /_/metrics response is
// reused. See AppConfig.MetricsScrapeCacheTTL.
func WithMetricsScrapeCacheTTL(ttl time.Duration) AppOption {
	return func(c *AppConfig) { c.MetricsScrapeCacheTTL = ttl }
}

// WithExposeInternalErrors overrides whether 5xx responses carry the internal
// error text, whatever the environment. See AppConfig.ExposeInternalErrors.
func WithExposeInternalErrors(expose bool) AppOption {
	return func(c *AppConfig) { c.ExposeInternalErrors = &expose }
}

// developmentEnvironments are the Environment values that turn on the
// debugging defaults. Compared case-insensitively.
var developmentEnvironments = map[string]bool{
	"development": true,
	"dev":         true,
	"local":       true,
	"test":        true,
}

// environmentIsDevelopment reports whether config names a development
// environment on purpose. An empty Environment is unset, and so is the
// "development" placeholder DefaultAppConfig fills in when nothing replaced
// it: an app deployed without setting its environment must get the
// production-safe behaviour, not the debugging one.
func environmentIsDevelopment(config AppConfig) bool {
	env := strings.ToLower(strings.TrimSpace(config.Environment))
	if env == "" || (config.environmentDefaulted && env == "development") {
		return false
	}

	return developmentEnvironments[env]
}

// resolveExposeInternalErrors applies an explicit ExposeInternalErrors first
// and otherwise exposes error text only in an explicit development
// environment.
func resolveExposeInternalErrors(config AppConfig, devEnvironment bool) bool {
	if config.ExposeInternalErrors != nil {
		return *config.ExposeInternalErrors
	}

	return devEnvironment
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

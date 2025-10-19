package common

import (
	"context"
	"time"
)

// PluginContext extends context.Context and provides plugin dependencies
type PluginContext interface {
	context.Context

	// Container returns the dependency injection container
	Container() Container

	// Router returns the HTTP router
	Router() Router

	// ConfigManager returns the configuration manager
	ConfigManager() ConfigManager
}

// pluginContext is the concrete implementation of PluginContext
type pluginContext struct {
	context.Context
	container     Container
	router        Router
	configManager ConfigManager
}

// NewPluginContext creates a new PluginContext
func NewPluginContext(ctx context.Context, container Container, router Router, configManager ConfigManager) PluginContext {
	return &pluginContext{
		Context:       ctx,
		container:     container,
		router:        router,
		configManager: configManager,
	}
}

func (pc *pluginContext) Container() Container {
	return pc.container
}

func (pc *pluginContext) Router() Router {
	return pc.router
}

func (pc *pluginContext) ConfigManager() ConfigManager {
	return pc.configManager
}

// Plugin defines a simplified interface for framework plugins
type Plugin interface {

	// Name returns the name of the plugin as a string.
	Name() string

	// Version returns the version of the plugin as a string.
	Version() string

	// Start is invoked when the plugin is being started. It initializes the plugin using the provided PluginContext.
	Start(ctx PluginContext) error

	// Stop is invoked to stop the plugin. It performs cleanup tasks and releases resources before the plugin shuts down.
	Stop(ctx context.Context) error

	// Middleware returns a slice of middleware components to be registered with the router.
	Middleware() []any

	// Routes registers the HTTP routes for the plugin using the provided Router instance and returns an error if it fails.
	Routes(r Router) error

	// Services returns a list of services defined by the plugin for registration into the application container.
	Services() []ServiceDefinition

	// Controllers returns a list of HTTP controllers associated with the plugin.
	Controllers() []Controller

	// HealthCheck verifies the operational status of a plugin and returns an error if issues are detected.
	HealthCheck(ctx context.Context) error
}

// PluginEngine defines the interface for building extensible, pluggable components in an application.
// It includes lifecycle methods, type identification, capabilities, dependencies, extensions, configuration, health checks, and metrics management.
// This is the complex plugin interface used by the pluginengine package.
type PluginEngine interface {
	// ID returns the unique identifier as a string. It is typically used to retrieve or represent the object's identity.
	ID() string

	// Name returns the name as a string.
	Name() string

	// Version returns the current version of the application or module as a string.
	Version() string

	// Description returns a string providing a textual description or details about an entity or object.
	Description() string

	// Author returns the name of the author or entity responsible for creating the plugin.
	Author() string

	// License returns the license type or information as a string. It is used for retrieving the licensing details of a resource.
	License() string

	// Initialize sets up necessary configurations and dependencies using the provided context and container.
	Initialize(ctx context.Context, container Container) error

	// OnStart initializes and begins execution of the process, using the provided context to manage its lifecycle.
	OnStart(ctx context.Context) error

	// OnStop gracefully halts the operation, utilizing the provided context for cancellation and timeout management.
	OnStop(ctx context.Context) error

	// Cleanup performs necessary cleanup operations such as freeing resources or closing connections after the plugin is stopped.
	Cleanup(ctx context.Context) error

	// Type returns the PluginType of the plugin, which specifies the category or kind of the plugin being used.
	Type() PluginType

	// Capabilities returns a list of PluginCapability, indicating the features or functionalities provided by the plugin.
	Capabilities() []PluginCapability

	// Dependencies retrieves a list of PluginDependency instances that represent the dependencies required by the plugin.
	Dependencies() []PluginDependency

	// Middleware returns a list of middleware definitions to be used by the plugin, including priority and dependencies.
	Middleware() []any

	// ConfigureRoutes returns a list of route definitions that the plugin provides, enabling HTTP routing for specific functionalities.
	ConfigureRoutes(router Router) error

	// Services returns a slice of common.ServiceDefinition, representing the services provided by this plugin.
	Services() []ServiceDefinition

	// Controllers returns a slice of Controller instances that this plugin provides.
	Controllers() []Controller

	// Commands returns a list of CLI commands provided by the plugin, including metadata, flags, and execution logic.
	Commands() []CLICommand

	// Hooks retrieves a slice of Hook objects that can be used to implement custom behaviors during the execution process.
	Hooks() []Hook

	// ConfigSchema returns the configuration schema for the plugin, defining expected structure, properties, and validation.
	ConfigSchema() ConfigSchema

	// Configure applies the provided configuration object to the plugin.
	// Returns an error if the configuration is invalid or cannot be applied.
	Configure(config interface{}) error

	// GetConfig retrieves the current configuration associated with the plugin. Returns an interface containing the config data.
	GetConfig() interface{}

	// HealthCheck verifies the health status of the plugin and returns an error if it detects any issues or failures.
	HealthCheck(ctx context.Context) error

	// GetMetrics retrieves the performance metrics and statistics collected from the plugin during its lifecycle and operations.
	GetMetrics() PluginMetrics
}

// PluginType represents the type or category of a plugin within the system, enabling classification and functionality grouping.
type PluginType string

// PluginTypeMiddleware represents plugins used as HTTP middleware.
// PluginTypeDatabase represents plugins used as database adapters.
// PluginTypeAuth represents plugins used for authentication purposes.
// PluginTypeCache represents plugins used as cache backends.
// PluginTypeStorage represents plugins used as storage backends.
// PluginTypeMessaging represents plugins used as message brokers.
// PluginTypeMonitoring represents plugins used for system or application monitoring.
// PluginTypeAI represents plugins used as AI agents.
// PluginTypeSecurity represents plugins used for security purposes.
// PluginTypeIntegration represents plugins used for third-party integrations.
// PluginTypeUtility represents utility plugins that provide additional tools or helpers.
// PluginTypeExtension represents framework extension plugins.
const (
	PluginTypeMiddleware  PluginType = "middleware" // HTTP middleware plugins
	PluginTypeService     PluginType = "service"
	PluginTypeHandler     PluginType = "handler"
	PluginTypeFilter      PluginType = "filter"
	PluginTypeDatabase    PluginType = "database"    // Database adapter plugins
	PluginTypeAuth        PluginType = "auth"        // Authentication plugins
	PluginTypeCache       PluginType = "cache"       // Cache backend plugins
	PluginTypeStorage     PluginType = "storage"     // Storage backend plugins
	PluginTypeMessaging   PluginType = "messaging"   // Message broker plugins
	PluginTypeMonitoring  PluginType = "monitoring"  // Monitoring plugins
	PluginTypeAI          PluginType = "ai"          // AI agent plugins
	PluginTypeSecurity    PluginType = "security"    // Security plugins
	PluginTypeIntegration PluginType = "integration" // Third-party integration plugins
	PluginTypeUtility     PluginType = "utility"     // Utility plugins
	PluginTypeExtension   PluginType = "extension"   // Framework extensions
)

// PluginCapability represents the set of features or functionalities that a plugin provides.
type PluginCapability struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Interface   string                 `json:"interface"`
	Methods     []string               `json:"methods"`
	Metadata    map[string]interface{} `json:"metadata"`
	Controllers []string               `json:"controllers,omitempty"` // Controller names provided
	Streaming   []string               `json:"streaming,omitempty"`   // Streaming protocols supported
}

// PluginDependency represents a specific dependency required by a plugin.
// It includes the name, version, type, requirement status, and version constraints for the dependency.
type PluginDependency struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Type       string `json:"type"` // plugin, service, package
	Required   bool   `json:"required"`
	Constraint string `json:"constraint"` // version constraint
}

// PluginMetrics represents metrics and performance statistics collected from a plugin's lifecycle and operations.
type PluginMetrics struct {
	CallCount      int64         `json:"call_count"`
	RouteCount     int64         `json:"route_count"`
	ErrorCount     int64         `json:"error_count"`
	AverageLatency time.Duration `json:"average_latency"`
	LastExecuted   time.Time     `json:"last_executed"`
	MemoryUsage    int64         `json:"memory_usage"`
	CPUUsage       float64       `json:"cpu_usage"`
	HealthScore    float64       `json:"health_score"`
	Uptime         time.Duration `json:"uptime"`
}

// CLICommand represents a command-line interface command with metadata, flags, and execution logic.
type CLICommand struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Usage       string     `json:"usage"`
	Flags       []CLIFlag  `json:"flags"`
	Handler     CLIHandler `json:"-"`
	Category    string     `json:"category"`
	Hidden      bool       `json:"hidden"`
	Aliases     []string   `json:"aliases"`
	Examples    []string   `json:"examples"`
}

// CLIFlag represents a command-line flag with its attributes, including name, description, type, and default value.
// Name is the full name of the flag as it will appear in the CLI.
// ShortName is the abbreviated single-character name of the flag.
// Description provides information about the flag's purpose or usage.
// Required indicates whether the flag must be supplied by the user.
// Default defines the fallback value for the flag if not explicitly set.
// Type specifies the expected data type of the flag's value (e.g., string, int).
type CLIFlag struct {
	Name        string      `json:"name"`
	ShortName   string      `json:"short_name"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default"`
	Type        string      `json:"type"`
}

// CLIHandler defines a function type for handling CLI commands with context, arguments, and flag parameters.
type CLIHandler func(ctx context.Context, args []string, flags map[string]interface{}) error

// ConfigSchema defines the structure for plugin configuration schemas, including metadata, properties, and validation rules.
type ConfigSchema struct {
	Version    string                    `json:"version"`
	Type       string                    `json:"type"`
	Title      string                    `json:"title"`
	Properties map[string]ConfigProperty `json:"properties"`
	Required   []string                  `json:"required"`
	Examples   []interface{}             `json:"examples"`
}

// ConfigProperty represents a configuration schema property with various validation and metadata attributes.
type ConfigProperty struct {
	Type        string                    `json:"type"`
	Description string                    `json:"description"`
	Default     interface{}               `json:"default"`
	Enum        []interface{}             `json:"enum,omitempty"`
	Format      string                    `json:"format,omitempty"`
	Minimum     *float64                  `json:"minimum,omitempty"`
	Maximum     *float64                  `json:"maximum,omitempty"`
	MinLength   *int                      `json:"minLength,omitempty"`
	MaxLength   *int                      `json:"maxLength,omitempty"`
	Pattern     string                    `json:"pattern,omitempty"`
	Items       *ConfigProperty           `json:"items,omitempty"`
	Properties  map[string]ConfigProperty `json:"properties,omitempty"`
	Required    []string                  `json:"required,omitempty"`
}

// Hook defines an interface for implementing extensible and customizable hooks in a system.
// Name returns the unique name of the hook.
// Type returns the type of the hook, used to categorize its functionality.
// Priority returns the execution priority of the hook (lower value indicates higher priority).
// Execute is invoked when the hook is triggered, processing the given context and data and returning a result or error.
type Hook interface {
	Name() string
	Type() HookType
	Priority() int
	Execute(ctx context.Context, data HookData) (HookResult, error)
}

// HookType represents the type of a hook, defining the stage at which the hook is executed in the application lifecycle.
type HookType string

// HookTypePreRequest represents the hook executed before request processing.
// HookTypePostRequest represents the hook executed after request processing.
// HookTypePreMiddleware represents the hook executed before middleware execution.
// HookTypePostMiddleware represents the hook executed after middleware execution.
// HookTypePreRoute represents the hook executed before route handling.
// HookTypePostRoute represents the hook executed after route handling.
// HookTypeServiceStart represents the hook executed during service startup.
// HookTypeServiceStop represents the hook executed during service shutdown.
// HookTypeError represents the hook executed for error handling.
// HookTypeHealthCheck represents the hook executed for health checking.
// HookTypeMetrics represents the hook executed for metrics collection.
const (
	HookTypePreRequest     HookType = "pre_request"     // Before request processing
	HookTypePostRequest    HookType = "post_request"    // After request processing
	HookTypePreMiddleware  HookType = "pre_middleware"  // Before middleware execution
	HookTypePostMiddleware HookType = "post_middleware" // After middleware execution
	HookTypePreRoute       HookType = "pre_route"       // Before route handling
	HookTypePostRoute      HookType = "post_route"      // After route handling
	HookTypeServiceStart   HookType = "service_start"   // Service startup
	HookTypeServiceStop    HookType = "service_stop"    // Service shutdown
	HookTypeError          HookType = "error"           // Error handling
	HookTypeHealthCheck    HookType = "health_check"    // Health check
	HookTypeMetrics        HookType = "metrics"         // Metrics collection
)

// HookData represents the structure that encapsulates hook event details and necessary execution context.
// It includes the hook type, associated data, context, metadata, and a timestamp of when the hook was triggered.
type HookData struct {
	Type      HookType               `json:"type"`
	Context   context.Context        `json:"-"`
	Data      interface{}            `json:"data"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
}

// HookResult represents the outcome of a hook execution.
// Continue indicates whether the execution should proceed to subsequent hooks.
// Data holds the result or payload returned by the hook.
// Error contains any error that occurred during the hook's execution.
// Metadata provides additional contextual information about the hook result.
type HookResult struct {
	Continue bool                   `json:"continue"`
	Data     interface{}            `json:"data"`
	Error    error                  `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata"`
}

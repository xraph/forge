package common

import (
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// Plugin defines the interface for building extensible, pluggable components in an application.
// It includes lifecycle methods, type identification, capabilities, dependencies, extensions, configuration, health checks, and metrics management.
type PluginEngine = common.PluginEngine

// PluginType represents the type or category of a plugin within the system, enabling classification and functionality grouping.
type PluginType = common.PluginType

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
	PluginTypeMiddleware  = common.PluginTypeMiddleware
	PluginTypeService     = common.PluginTypeService
	PluginTypeHandler     = common.PluginTypeHandler
	PluginTypeFilter      = common.PluginTypeFilter
	PluginTypeDatabase    = common.PluginTypeDatabase
	PluginTypeAuth        = common.PluginTypeAuth
	PluginTypeCache       = common.PluginTypeCache
	PluginTypeStorage     = common.PluginTypeStorage
	PluginTypeMessaging   = common.PluginTypeMessaging
	PluginTypeMonitoring  = common.PluginTypeMonitoring
	PluginTypeAI          = common.PluginTypeAI
	PluginTypeSecurity    = common.PluginTypeSecurity
	PluginTypeIntegration = common.PluginTypeIntegration
	PluginTypeUtility     = common.PluginTypeUtility
	PluginTypeExtension   = common.PluginTypeExtension
)

// PluginCapability represents the set of features or functionalities that a plugin provides.
type PluginCapability = common.PluginCapability

// PluginDependency represents a specific dependency required by a plugin.
// It includes the name, version, type, requirement status, and version constraints for the dependency.
type PluginDependency = common.PluginDependency

// PluginMetrics represents metrics and performance statistics collected from a plugin's lifecycle and operations.
type PluginMetrics = common.PluginMetrics

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

// CLICommand represents a command-line interface command with metadata, flags, and execution logic.
type CLICommand = common.CLICommand

// CLIFlag represents a command-line flag with its attributes, including name, description, type, and default value.
// Name is the full name of the flag as it will appear in the CLI.
// ShortName is the abbreviated single-character name of the flag.
// Description provides information about the flag's purpose or usage.
// Required indicates whether the flag must be supplied by the user.
// Default defines the fallback value for the flag if not explicitly set.
// Type specifies the expected data type of the flag's value (e.g., string, int).
type CLIFlag = common.CLIFlag

// CLIHandler defines a function type for handling CLI commands with context, arguments, and flag parameters.
type CLIHandler = common.CLIHandler

// ConfigSchema defines the structure for plugin configuration schemas, including metadata, properties, and validation rules.
type ConfigSchema = common.ConfigSchema

// ConfigProperty represents a configuration schema property with various validation and metadata attributes.
type ConfigProperty = common.ConfigProperty

// Hook defines an interface for implementing extensible and customizable hooks in a system.
// Name returns the unique name of the hook.
// Type returns the type of the hook, used to categorize its functionality.
// Priority returns the execution priority of the hook (lower value indicates higher priority).
// Execute is invoked when the hook is triggered, processing the given context and data and returning a result or error.
type Hook = common.Hook

// HookType represents the type of a hook, defining the stage at which the hook is executed in the application lifecycle.
type HookType = common.HookType

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
	HookTypePreRequest     = common.HookTypePreRequest
	HookTypePostRequest    = common.HookTypePostRequest
	HookTypePreMiddleware  = common.HookTypePreMiddleware
	HookTypePostMiddleware = common.HookTypePostMiddleware
	HookTypePreRoute       = common.HookTypePreRoute
	HookTypePostRoute      = common.HookTypePostRoute
	HookTypeServiceStart   = common.HookTypeServiceStart
	HookTypeServiceStop    = common.HookTypeServiceStop
	HookTypeError          = common.HookTypeError
	HookTypeHealthCheck    = common.HookTypeHealthCheck
	HookTypeMetrics        = common.HookTypeMetrics
)

// HookData represents the structure that encapsulates hook event details and necessary execution context.
// It includes the hook type, associated data, context, metadata, and a timestamp of when the hook was triggered.
type HookData = common.HookData

// HookResult represents the outcome of a hook execution.
// Continue indicates whether the execution should proceed to subsequent hooks.
// Data holds the result or payload returned by the hook.
// Error contains any error that occurred during the hook's execution.
// Metadata provides additional contextual information about the hook result.
type HookResult = common.HookResult

// PluginInfo represents metadata and configuration details of a plugin.
type PluginInfo struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Version       string                 `json:"version"`
	Description   string                 `json:"description"`
	Author        string                 `json:"author"`
	License       string                 `json:"license"`
	Homepage      string                 `json:"homepage"`
	Repository    string                 `json:"repository"`
	Tags          []string               `json:"tags"`
	Category      string                 `json:"category"`
	Type          PluginType             `json:"type"`
	Rating        float64                `json:"rating"`
	Downloads     int64                  `json:"downloads"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Capabilities  []PluginCapability     `json:"capabilities"`
	Dependencies  []PluginDependency     `json:"dependencies"`
	Compatibility []string               `json:"compatibility"`
	Screenshots   []string               `json:"screenshots"`
	Documentation string                 `json:"documentation"`
	ConfigSchema  ConfigSchema           `json:"config_schema"`
	Metadata      map[string]interface{} `json:"metadata"`
}

// PluginPackage represents a plugin package containing metadata, binaries, configuration, documentation, and assets.
type PluginPackage struct {
	Info     PluginInfo        `json:"info"`
	Binary   []byte            `json:"binary"`
	Config   []byte            `json:"config"`
	Docs     []byte            `json:"docs"`
	Assets   map[string][]byte `json:"assets"`
	Checksum string            `json:"checksum"`
}

// PluginStats represents the statistics and metadata of a plugin within the system.
type PluginStats struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Version       string                 `json:"version"`
	Type          PluginType             `json:"type"`
	State         PluginState            `json:"state"`
	LoadedAt      time.Time              `json:"loaded_at"`
	StartedAt     time.Time              `json:"started_at"`
	Uptime        time.Duration          `json:"uptime"`
	Metrics       PluginMetrics          `json:"metrics"`
	Dependencies  []PluginDependency     `json:"dependencies"`
	Capabilities  []PluginCapability     `json:"capabilities"`
	MemoryUsage   int64                  `json:"memory_usage"`
	CPUUsage      float64                `json:"cpu_usage"`
	LastError     string                 `json:"last_error,omitempty"`
	HealthScore   float64                `json:"health_score"`
	Configuration map[string]interface{} `json:"configuration"`
}

// PluginState defines the lifecycle state of a plugin, represented as a string.
type PluginState string

// PluginStateUnloaded indicates the plugin is not loaded into the system.
// PluginStateLoaded indicates the plugin is loaded into the system but not initialized.
// PluginStateInitialized indicates the plugin is initialized and ready to be started.
// PluginStateStarted indicates the plugin is actively running or in use.
// PluginStateStopped indicates the plugin is stopped but still available in the system.
// PluginStateError indicates the plugin encountered an error state.
const (
	PluginStateUnloaded    PluginState = "unloaded"
	PluginStateLoaded      PluginState = "loaded"
	PluginStateInitialized PluginState = "initialized"
	PluginStateStarted     PluginState = "started"
	PluginStateStopped     PluginState = "stopped"
	PluginStateError       PluginState = "error"
)

// PluginOperation defines the structure for a plugin operation, including its type, target, parameters, timeout, and retries.
type PluginOperation struct {
	Type       OperationType          `json:"type"`
	Target     string                 `json:"target"`
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    time.Duration          `json:"timeout"`
	Retries    int                    `json:"retries"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// OperationType represents a string-based enumeration of various operational actions or commands.
type OperationType string

// OperationTypeLoad represents the "load" operation type.
// OperationTypeUnload represents the "unload" operation type.
// OperationTypeStart represents the "start" operation type.
// OperationTypeStop represents the "stop" operation type.
// OperationTypeConfigure represents the "configure" operation type.
// OperationTypeExecute represents the "execute" operation type.
// OperationTypeQuery represents the "query" operation type.
const (
	OperationTypeLoad      OperationType = "load"
	OperationTypeUnload    OperationType = "unload"
	OperationTypeStart     OperationType = "start"
	OperationTypeStop      OperationType = "stop"
	OperationTypeConfigure OperationType = "configure"
	OperationTypeExecute   OperationType = "execute"
	OperationTypeQuery     OperationType = "query"
)

// PluginRegistry defines an interface for managing plugins including registration, retrieval, listing, and statistics.
type PluginRegistry interface {
	Register(plugin PluginEngine) error
	Unregister(pluginID string) error
	Get(pluginID string) (PluginEngine, error)
	List() []PluginEngine
	ListByType(pluginType PluginType) []PluginEngine
	Search(query string) []PluginEngine
	GetStats() RegistryStats
}

// RegistryStats provides a summary of the plugin registry including counts, states, memory, CPU usage, and update timestamp.
type RegistryStats struct {
	TotalPlugins     int                 `json:"total_plugins"`
	PluginsByType    map[PluginType]int  `json:"plugins_by_type"`
	PluginsByState   map[PluginState]int `json:"plugins_by_state"`
	LoadedPlugins    int                 `json:"loaded_plugins"`
	ActivePlugins    int                 `json:"active_plugins"`
	FailedPlugins    int                 `json:"failed_plugins"`
	TotalMemoryUsage int64               `json:"total_memory_usage"`
	AverageCPUUsage  float64             `json:"average_cpu_usage"`
	LastUpdated      time.Time           `json:"last_updated"`
}

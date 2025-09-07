package plugins

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// Plugin defines the interface for framework plugins
type Plugin interface {
	// Basic plugin information
	ID() string
	Name() string
	Version() string
	Description() string
	Author() string
	License() string

	// Plugin lifecycle
	Initialize(ctx context.Context, container common.Container) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Cleanup(ctx context.Context) error

	// Plugin capabilities
	Type() PluginType
	Capabilities() []PluginCapability
	Dependencies() []PluginDependency

	// Plugin extensions
	Middleware() []common.MiddlewareDefinition
	Routes() []common.RouteDefinition
	Services() []common.ServiceDefinition
	Commands() []CLICommand
	Hooks() []Hook

	// Plugin configuration
	ConfigSchema() ConfigSchema
	Configure(config interface{}) error
	GetConfig() interface{}

	// Plugin health and metrics
	HealthCheck(ctx context.Context) error
	GetMetrics() PluginMetrics
}

// PluginType defines the type of plugin
type PluginType string

const (
	PluginTypeMiddleware  PluginType = "middleware"  // HTTP middleware plugins
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

// PluginCapability describes what a plugin can do
type PluginCapability struct {
	Name        string                 `json:"name"`
	Version     string                 `json:"version"`
	Description string                 `json:"description"`
	Interface   string                 `json:"interface"`
	Methods     []string               `json:"methods"`
	Metadata    map[string]interface{} `json:"metadata"`
}

// PluginDependency describes a plugin dependency
type PluginDependency struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Type       string `json:"type"` // plugin, service, package
	Required   bool   `json:"required"`
	Constraint string `json:"constraint"` // version constraint
}

// PluginMetrics contains plugin performance metrics
type PluginMetrics struct {
	CallCount      int64         `json:"call_count"`
	ErrorCount     int64         `json:"error_count"`
	AverageLatency time.Duration `json:"average_latency"`
	LastExecuted   time.Time     `json:"last_executed"`
	MemoryUsage    int64         `json:"memory_usage"`
	CPUUsage       float64       `json:"cpu_usage"`
	HealthScore    float64       `json:"health_score"`
	Uptime         time.Duration `json:"uptime"`
}

// CLICommand represents a CLI command provided by a plugin
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

// CLIFlag represents a command line flag
type CLIFlag struct {
	Name        string      `json:"name"`
	ShortName   string      `json:"short_name"`
	Description string      `json:"description"`
	Required    bool        `json:"required"`
	Default     interface{} `json:"default"`
	Type        string      `json:"type"`
}

// CLIHandler defines the signature for CLI command handlers
type CLIHandler func(ctx context.Context, args []string, flags map[string]interface{}) error

// ConfigSchema defines the configuration schema for a plugin
type ConfigSchema struct {
	Version    string                    `json:"version"`
	Type       string                    `json:"type"`
	Title      string                    `json:"title"`
	Properties map[string]ConfigProperty `json:"properties"`
	Required   []string                  `json:"required"`
	Examples   []interface{}             `json:"examples"`
}

// ConfigProperty defines a configuration property
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

// Hook defines framework extension points
type Hook interface {
	Name() string
	Type() HookType
	Priority() int
	Execute(ctx context.Context, data HookData) (HookResult, error)
}

// HookType defines the type of hook
type HookType string

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

// HookData contains data passed to hooks
type HookData struct {
	Type      HookType               `json:"type"`
	Context   context.Context        `json:"-"`
	Data      interface{}            `json:"data"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
}

// HookResult contains the result of hook execution
type HookResult struct {
	Continue bool                   `json:"continue"`
	Data     interface{}            `json:"data"`
	Error    error                  `json:"error,omitempty"`
	Metadata map[string]interface{} `json:"metadata"`
}

// BasePlugin provides a basic implementation of the Plugin interface
type BasePlugin struct {
	id           string
	name         string
	version      string
	description  string
	author       string
	license      string
	pluginType   PluginType
	capabilities []PluginCapability
	dependencies []PluginDependency
	config       interface{}
	configSchema ConfigSchema
	metrics      PluginMetrics
	startTime    time.Time
	container    common.Container
}

// NewBasePlugin creates a new base plugin
func NewBasePlugin(info PluginInfo) *BasePlugin {
	return &BasePlugin{
		id:           info.ID,
		name:         info.Name,
		version:      info.Version,
		description:  info.Description,
		author:       info.Author,
		license:      info.License,
		pluginType:   info.Type,
		capabilities: info.Capabilities,
		dependencies: info.Dependencies,
		configSchema: info.ConfigSchema,
		metrics:      PluginMetrics{},
	}
}

// ID returns the plugin ID
func (p *BasePlugin) ID() string {
	return p.id
}

// Name returns the plugin name
func (p *BasePlugin) Name() string {
	return p.name
}

// Version returns the plugin version
func (p *BasePlugin) Version() string {
	return p.version
}

// Description returns the plugin description
func (p *BasePlugin) Description() string {
	return p.description
}

// Author returns the plugin author
func (p *BasePlugin) Author() string {
	return p.author
}

// License returns the plugin license
func (p *BasePlugin) License() string {
	return p.license
}

// Type returns the plugin type
func (p *BasePlugin) Type() PluginType {
	return p.pluginType
}

// Capabilities returns the plugin capabilities
func (p *BasePlugin) Capabilities() []PluginCapability {
	return p.capabilities
}

// Dependencies returns the plugin dependencies
func (p *BasePlugin) Dependencies() []PluginDependency {
	return p.dependencies
}

// Initialize initializes the plugin
func (p *BasePlugin) Initialize(ctx context.Context, container common.Container) error {
	p.container = container
	p.startTime = time.Now()
	return nil
}

// Start starts the plugin
func (p *BasePlugin) Start(ctx context.Context) error {
	return nil
}

// Stop stops the plugin
func (p *BasePlugin) Stop(ctx context.Context) error {
	return nil
}

// Cleanup cleans up plugin resources
func (p *BasePlugin) Cleanup(ctx context.Context) error {
	return nil
}

// Middleware returns middleware definitions (empty by default)
func (p *BasePlugin) Middleware() []common.MiddlewareDefinition {
	return []common.MiddlewareDefinition{}
}

// Routes returns route definitions (empty by default)
func (p *BasePlugin) Routes() []common.RouteDefinition {
	return []common.RouteDefinition{}
}

// Services returns service definitions (empty by default)
func (p *BasePlugin) Services() []common.ServiceDefinition {
	return []common.ServiceDefinition{}
}

// Commands returns CLI commands (empty by default)
func (p *BasePlugin) Commands() []CLICommand {
	return []CLICommand{}
}

// Hooks returns hooks (empty by default)
func (p *BasePlugin) Hooks() []Hook {
	return []Hook{}
}

// ConfigSchema returns the configuration schema
func (p *BasePlugin) ConfigSchema() ConfigSchema {
	return p.configSchema
}

// Configure configures the plugin
func (p *BasePlugin) Configure(config interface{}) error {
	p.config = config
	return nil
}

// GetConfig returns the plugin configuration
func (p *BasePlugin) GetConfig() interface{} {
	return p.config
}

// HealthCheck performs a health check
func (p *BasePlugin) HealthCheck(ctx context.Context) error {
	return nil
}

// GetMetrics returns plugin metrics
func (p *BasePlugin) GetMetrics() PluginMetrics {
	p.metrics.Uptime = time.Since(p.startTime)
	return p.metrics
}

// UpdateMetrics updates plugin metrics
func (p *BasePlugin) UpdateMetrics(callCount, errorCount int64, latency time.Duration) {
	p.metrics.CallCount = callCount
	p.metrics.ErrorCount = errorCount
	p.metrics.AverageLatency = latency
	p.metrics.LastExecuted = time.Now()
}

// PluginInfo contains metadata about a plugin
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

// PluginPackage represents a plugin package
type PluginPackage struct {
	Info     PluginInfo        `json:"info"`
	Binary   []byte            `json:"binary"`
	Config   []byte            `json:"config"`
	Docs     []byte            `json:"docs"`
	Assets   map[string][]byte `json:"assets"`
	Checksum string            `json:"checksum"`
}

// PluginStats contains plugin statistics
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

// PluginState represents the state of a plugin
type PluginState string

const (
	PluginStateUnloaded    PluginState = "unloaded"
	PluginStateLoaded      PluginState = "loaded"
	PluginStateInitialized PluginState = "initialized"
	PluginStateStarted     PluginState = "started"
	PluginStateStopped     PluginState = "stopped"
	PluginStateError       PluginState = "error"
)

// PluginOperation represents an operation that can be performed on a plugin
type PluginOperation struct {
	Type       OperationType          `json:"type"`
	Target     string                 `json:"target"`
	Parameters map[string]interface{} `json:"parameters"`
	Timeout    time.Duration          `json:"timeout"`
	Retries    int                    `json:"retries"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// OperationType defines the type of operation
type OperationType string

const (
	OperationTypeLoad      OperationType = "load"
	OperationTypeUnload    OperationType = "unload"
	OperationTypeStart     OperationType = "start"
	OperationTypeStop      OperationType = "stop"
	OperationTypeConfigure OperationType = "configure"
	OperationTypeExecute   OperationType = "execute"
	OperationTypeQuery     OperationType = "query"
)

// PluginRegistry defines the interface for plugin registry
type PluginRegistry interface {
	Register(plugin Plugin) error
	Unregister(pluginID string) error
	Get(pluginID string) (Plugin, error)
	List() []Plugin
	ListByType(pluginType PluginType) []Plugin
	Search(query string) []Plugin
	GetStats() RegistryStats
}

// RegistryStats contains plugin registry statistics
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

// PluginContext provides context for plugin operations
type PluginContext struct {
	context.Context
	Plugin    Plugin
	Container common.Container
	Logger    common.Logger
	Metrics   common.Metrics
	Config    common.ConfigManager
	Metadata  map[string]interface{}
}

// NewPluginContext creates a new plugin context
func NewPluginContext(ctx context.Context, plugin Plugin, container common.Container) *PluginContext {
	logger, _ := container.Resolve((*common.Logger)(nil))
	metrics, _ := container.Resolve((*common.Metrics)(nil))
	config, _ := container.Resolve((*common.ConfigManager)(nil))

	return &PluginContext{
		Context:   ctx,
		Plugin:    plugin,
		Container: container,
		Logger:    logger.(common.Logger),
		Metrics:   metrics.(common.Metrics),
		Config:    config.(common.ConfigManager),
		Metadata:  make(map[string]interface{}),
	}
}

// WithMetadata adds metadata to the plugin context
func (pc *PluginContext) WithMetadata(key string, value interface{}) *PluginContext {
	pc.Metadata[key] = value
	return pc
}

// GetMetadata retrieves metadata from the plugin context
func (pc *PluginContext) GetMetadata(key string) (interface{}, bool) {
	value, exists := pc.Metadata[key]
	return value, exists
}

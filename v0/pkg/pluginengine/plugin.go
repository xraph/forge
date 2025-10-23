package pluginengine

import (
	"context"
	"time"

	"github.com/xraph/forge/pkg/common"
	common2 "github.com/xraph/forge/pkg/pluginengine/common"
)

// Plugin defines the interface for framework plugins
type PluginEngine = common2.PluginEngine

// PluginType defines the type of plugin
type PluginType = common2.PluginType

const (
	PluginTypeMiddleware  = common2.PluginTypeMiddleware
	PluginTypeDatabase    = common2.PluginTypeDatabase
	PluginTypeAuth        = common2.PluginTypeAuth
	PluginTypeCache       = common2.PluginTypeCache
	PluginTypeStorage     = common2.PluginTypeStorage
	PluginTypeMessaging   = common2.PluginTypeMessaging
	PluginTypeMonitoring  = common2.PluginTypeMonitoring
	PluginTypeAI          = common2.PluginTypeAI
	PluginTypeSecurity    = common2.PluginTypeSecurity
	PluginTypeIntegration = common2.PluginTypeIntegration
	PluginTypeUtility     = common2.PluginTypeUtility
	PluginTypeExtension   = common2.PluginTypeExtension
)

// PluginCapability describes what a plugin can do
type PluginCapability = common2.PluginCapability

// PluginDependency describes a plugin dependency
type PluginDependency = common2.PluginDependency

// PluginMetrics contains plugin performance metrics
type PluginMetrics = common2.PluginMetrics

// CLICommand represents a CLI command provided by a plugin
type CLICommand = common2.CLICommand

// CLIFlag represents a command line flag
type CLIFlag = common2.CLIFlag

// CLIHandler defines the signature for CLI command handlers
type CLIHandler = common2.CLIHandler

// ConfigSchema defines the configuration schema for a plugin
type ConfigSchema = common2.ConfigSchema

// ConfigProperty defines a configuration property
type ConfigProperty = common2.ConfigProperty

// Hook defines framework extension points
type Hook interface {
	Name() string
	Type() HookType
	Priority() int
	Execute(ctx context.Context, data HookData) (HookResult, error)
}

// HookType defines the type of hook
type HookType = common2.HookType

const (
	HookTypePreRequest     = common2.HookTypePreRequest
	HookTypePostRequest    = common2.HookTypePostRoute
	HookTypePreMiddleware  = common2.HookTypePreMiddleware
	HookTypePostMiddleware = common2.HookTypePostMiddleware
	HookTypePreRoute       = common2.HookTypePreRoute
	HookTypePostRoute      = common2.HookTypePostRoute
	HookTypeServiceStart   = common2.HookTypeServiceStart
	HookTypeServiceStop    = common2.HookTypeServiceStop
	HookTypeError          = common2.HookTypeError
	HookTypeHealthCheck    = common2.HookTypeHealthCheck
	HookTypeMetrics        = common2.HookTypeMetrics
)

// HookData contains data passed to hooks
type HookData = common2.HookData

// HookResult contains the result of hook execution
type HookResult = common2.HookResult

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

// ConfigureRoutes returns route definitions (empty by default)
func (p *BasePlugin) ConfigureRoutes(router common.Router) error {
	return nil
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
type PluginInfo = common2.PluginInfo

// PluginPackage represents a plugin package
type PluginPackage = common2.PluginPackage

// PluginStats contains plugin statistics
type PluginStats = common2.PluginStats

// PluginState represents the state of a plugin
type PluginState = common2.PluginState

const (
	PluginStateUnloaded    = common2.PluginStateUnloaded
	PluginStateLoaded      = common2.PluginStateLoaded
	PluginStateInitialized = common2.PluginStateInitialized
	PluginStateStarted     = common2.PluginStateStarted
	PluginStateStopped     = common2.PluginStateStopped
	PluginStateError       = common2.PluginStateError
)

// PluginOperation represents an operation that can be performed on a plugin
type PluginOperation = common2.PluginOperation

// OperationType defines the type of operation
type OperationType = common2.OperationType

const (
	OperationTypeLoad      = common2.OperationTypeLoad
	OperationTypeUnload    = common2.OperationTypeUnload
	OperationTypeStart     = common2.OperationTypeStart
	OperationTypeStop      = common2.OperationTypeStop
	OperationTypeConfigure = common2.OperationTypeConfigure
	OperationTypeExecute   = common2.OperationTypeExecute
	OperationTypeQuery     = common2.OperationTypeQuery
)

// PluginRegistry defines the interface for plugin registry
type PluginRegistry = common2.PluginRegistry
type Plugin = common.PluginEngine

// RegistryStats contains plugin registry statistics
type RegistryStats = common2.RegistryStats

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

package plugins

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/core"
	"github.com/xraph/forge/jobs"
)

// Plugin represents the main plugin interface
type Plugin interface {
	// Plugin metadata
	Name() string
	Version() string
	Description() string
	Author() string

	// Plugin lifecycle
	Initialize(container core.Container) error
	Start(ctx context.Context) error
	Stop(ctx context.Context) error

	// Plugin capabilities
	Routes() []RouteDefinition
	Jobs() []JobDefinition
	Middleware() []MiddlewareDefinition
	Commands() []CommandDefinition
	HealthChecks() []HealthCheckDefinition

	// Plugin configuration
	ConfigSchema() map[string]interface{}
	DefaultConfig() map[string]interface{}
	ValidateConfig(config map[string]interface{}) error

	// Plugin dependencies
	Dependencies() []Dependency

	// Plugin state
	IsEnabled() bool
	IsInitialized() bool
	Status() PluginStatus
}

// Manager manages plugin lifecycle and registration
type Manager interface {
	// Plugin registration
	Register(plugin Plugin) error
	Unregister(name string) error

	// Plugin management
	Enable(name string) error
	Disable(name string) error
	Reload(name string) error

	// Plugin discovery
	LoadFromDirectory(directory string) error
	LoadFromFile(path string) error

	// Plugin access
	Get(name string) (Plugin, error)
	List() []Plugin
	ListEnabled() []Plugin

	// Plugin lifecycle
	InitializeAll(container core.Container) error
	StartAll(ctx context.Context) error
	StopAll(ctx context.Context) error

	// Plugin status
	Status() map[string]PluginStatus
	Health(ctx context.Context) map[string]error

	// Configuration
	SetConfig(name string, config map[string]interface{}) error
	GetConfig(name string) map[string]interface{}

	// Events
	OnPluginEnabled(callback func(Plugin))
	OnPluginDisabled(callback func(Plugin))
	OnPluginError(callback func(Plugin, error))
}

// Registry provides plugin discovery and metadata
type Registry interface {
	// Plugin registry
	Register(metadata PluginMetadata) error
	Unregister(name string) error

	// Plugin search
	Search(query string) []PluginMetadata
	List() []PluginMetadata
	Get(name string) (*PluginMetadata, error)

	// Plugin versions
	ListVersions(name string) []string
	GetLatestVersion(name string) (string, error)

	// Plugin categories
	ListCategories() []string
	GetByCategory(category string) []PluginMetadata

	// Plugin popularity
	GetPopular(limit int) []PluginMetadata
	GetRecommended() []PluginMetadata

	GetStatistics() map[string]interface{}
}

// Loader handles plugin loading and compilation
type Loader interface {
	// Plugin loading
	LoadPlugin(path string) (Plugin, error)
	LoadFromBytes(data []byte, metadata PluginMetadata) (Plugin, error)
	UnloadPlugin(name string) error

	// Plugin compilation (for compiled plugins)
	CompilePlugin(source string, metadata PluginMetadata) ([]byte, error)

	// Plugin validation
	ValidatePlugin(plugin Plugin) error
	ValidateSource(source string) error

	// Plugin formats
	SupportedFormats() []string

	// Security
	SandboxPlugin(plugin Plugin) (Plugin, error)
	ValidateSignature(data []byte, signature string) error
}

// Plugin definition types

// RouteDefinition defines a plugin route
type RouteDefinition struct {
	Method      string                 `json:"method"`
	Pattern     string                 `json:"pattern"`
	Handler     http.HandlerFunc       `json:"-"`
	Name        string                 `json:"name,omitempty"`
	Description string                 `json:"description,omitempty"`
	Middleware  []string               `json:"middleware,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`

	// Security
	RequireAuth   bool     `json:"require_auth,omitempty"`
	RequireRoles  []string `json:"require_roles,omitempty"`
	RequireScopes []string `json:"require_scopes,omitempty"`

	// Rate limiting
	RateLimit *RouteRateLimit `json:"rate_limit,omitempty"`

	// OpenAPI documentation
	OpenAPI *OpenAPIDefinition `json:"openapi,omitempty"`
}

// JobDefinition defines a plugin job
type JobDefinition struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Handler     jobs.JobHandlerFunc    `json:"-"`
	Description string                 `json:"description,omitempty"`
	Schedule    string                 `json:"schedule,omitempty"` // Cron expression
	Queue       string                 `json:"queue,omitempty"`
	Priority    int                    `json:"priority,omitempty"`
	MaxRetries  int                    `json:"max_retries,omitempty"`
	Timeout     time.Duration          `json:"timeout,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// MiddlewareDefinition defines a plugin middleware
type MiddlewareDefinition struct {
	Name        string                          `json:"name"`
	Handler     func(http.Handler) http.Handler `json:"-"`
	Description string                          `json:"description,omitempty"`
	Priority    int                             `json:"priority,omitempty"`
	Config      map[string]interface{}          `json:"config,omitempty"`
	ApplyTo     []string                        `json:"apply_to,omitempty"` // Route patterns
}

// CommandDefinition defines a plugin CLI command
type CommandDefinition struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Usage       string              `json:"usage,omitempty"`
	Handler     CommandHandler      `json:"-"`
	Flags       []FlagDefinition    `json:"flags,omitempty"`
	Subcommands []CommandDefinition `json:"subcommands,omitempty"`
	Aliases     []string            `json:"aliases,omitempty"`
	Hidden      bool                `json:"hidden,omitempty"`
}

// HealthCheckDefinition defines a plugin health check
type HealthCheckDefinition struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Handler     HealthCheckHandler `json:"-"`
	Interval    time.Duration      `json:"interval,omitempty"`
	Timeout     time.Duration      `json:"timeout,omitempty"`
	Critical    bool               `json:"critical,omitempty"`
	Tags        []string           `json:"tags,omitempty"`
}

// Supporting types

// PluginMetadata represents plugin metadata
type PluginMetadata struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	Email       string `json:"email,omitempty"`
	Website     string `json:"website,omitempty"`
	License     string `json:"license,omitempty"`

	// Classification
	Category string   `json:"category"`
	Tags     []string `json:"tags,omitempty"`
	Keywords []string `json:"keywords,omitempty"`

	// Compatibility
	ForgeVersion string   `json:"forge_version"`
	GoVersion    string   `json:"go_version,omitempty"`
	Platform     []string `json:"platform,omitempty"`     // linux, darwin, windows
	Architecture []string `json:"architecture,omitempty"` // amd64, arm64

	// Dependencies
	Dependencies []Dependency `json:"dependencies,omitempty"`

	// Configuration
	ConfigSchema  map[string]interface{} `json:"config_schema,omitempty"`
	DefaultConfig map[string]interface{} `json:"default_config,omitempty"`

	// Security
	Signature   string       `json:"signature,omitempty"`
	Checksum    string       `json:"checksum,omitempty"`
	Permissions []Permission `json:"permissions,omitempty"`

	// Lifecycle
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Downloads int64     `json:"downloads,omitempty"`

	// Repository
	Repository    string `json:"repository,omitempty"`
	Homepage      string `json:"homepage,omitempty"`
	Documentation string `json:"documentation,omitempty"`
	BugReports    string `json:"bug_reports,omitempty"`
}

// Dependency represents a plugin dependency
type Dependency struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Optional    bool   `json:"optional,omitempty"`
	Condition   string `json:"condition,omitempty"` // When this dependency is needed
	Description string `json:"description,omitempty"`
}

// Permission represents a plugin permission
type Permission struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
	Scope       string `json:"scope,omitempty"`
}

// PluginStatus represents plugin status
type PluginStatus struct {
	Enabled     bool                   `json:"enabled"`
	Initialized bool                   `json:"initialized"`
	Started     bool                   `json:"started"`
	Error       string                 `json:"error,omitempty"`
	Health      string                 `json:"health"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`

	// Performance
	LoadTime  time.Duration `json:"load_time"`
	StartTime time.Duration `json:"start_time"`

	// Statistics
	RequestCount int64     `json:"request_count,omitempty"`
	ErrorCount   int64     `json:"error_count,omitempty"`
	LastUsed     time.Time `json:"last_used,omitempty"`
	LastError    time.Time `json:"last_error,omitempty"`
}

// Handler types

// CommandHandler handles plugin CLI commands
type CommandHandler interface {
	Execute(ctx context.Context, args []string) error
}

// HealthCheckHandler handles plugin health checks
type HealthCheckHandler interface {
	Check(ctx context.Context) error
}

// Configuration types

// FlagDefinition defines a CLI flag
type FlagDefinition struct {
	Name        string      `json:"name"`
	Short       string      `json:"short,omitempty"`
	Description string      `json:"description"`
	Type        string      `json:"type"` // string, int, bool, duration
	Default     interface{} `json:"default,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Hidden      bool        `json:"hidden,omitempty"`
}

// RouteRateLimit defines rate limiting for a route
type RouteRateLimit struct {
	RequestsPerSecond float64 `json:"requests_per_second"`
	BurstSize         int     `json:"burst_size"`
	KeyFunc           string  `json:"key_func,omitempty"` // ip, user, custom
}

// OpenAPIDefinition defines OpenAPI documentation for a route
type OpenAPIDefinition struct {
	Summary     string                        `json:"summary,omitempty"`
	Description string                        `json:"description,omitempty"`
	Tags        []string                      `json:"tags,omitempty"`
	Parameters  []ParameterDefinition         `json:"parameters,omitempty"`
	Responses   map[string]ResponseDefinition `json:"responses,omitempty"`
	Security    []SecurityDefinition          `json:"security,omitempty"`
}

// ParameterDefinition defines an OpenAPI parameter
type ParameterDefinition struct {
	Name        string      `json:"name"`
	In          string      `json:"in"` // query, path, header, cookie
	Description string      `json:"description,omitempty"`
	Required    bool        `json:"required,omitempty"`
	Type        string      `json:"type"`
	Format      string      `json:"format,omitempty"`
	Example     interface{} `json:"example,omitempty"`
}

// ResponseDefinition defines an OpenAPI response
type ResponseDefinition struct {
	Description string                      `json:"description"`
	Schema      map[string]interface{}      `json:"schema,omitempty"`
	Headers     map[string]HeaderDefinition `json:"headers,omitempty"`
	Examples    map[string]interface{}      `json:"examples,omitempty"`
}

// HeaderDefinition defines an OpenAPI header
type HeaderDefinition struct {
	Description string `json:"description,omitempty"`
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
}

// SecurityDefinition defines OpenAPI security
type SecurityDefinition struct {
	Type   string   `json:"type"` // apiKey, oauth2, http
	Name   string   `json:"name,omitempty"`
	In     string   `json:"in,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

// AuthPlugin represents an authentication plugin
type AuthPlugin interface {
	Plugin
	Authenticate(ctx context.Context, credentials interface{}) (interface{}, error)
	Authorize(ctx context.Context, user interface{}, resource string, action string) error
}

// StoragePlugin represents a storage plugin
type StoragePlugin interface {
	Plugin
	CreateConnection(config map[string]interface{}) (interface{}, error)
	ValidateConnection(conn interface{}) error
}

// NotificationPlugin represents a notification plugin
type NotificationPlugin interface {
	Plugin
	Send(ctx context.Context, notification Notification) error
	SupportsChannel(channel string) bool
}

// ScheduleDefinition defines a scheduled job
type ScheduleDefinition struct {
	Name        string                 `json:"name"`
	Schedule    string                 `json:"schedule"` // Cron expression
	Job         JobDefinition          `json:"job"`
	Enabled     bool                   `json:"enabled"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Notification represents a notification message
type Notification struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	Channel    string                 `json:"channel"`
	Recipients []string               `json:"recipients"`
	Subject    string                 `json:"subject,omitempty"`
	Message    string                 `json:"message"`
	Data       map[string]interface{} `json:"data,omitempty"`
	Priority   int                    `json:"priority,omitempty"`
	Timestamp  time.Time              `json:"timestamp"`
}

// Plugin events

// Event represents a plugin event
type Event struct {
	Type      string                 `json:"type"`
	Plugin    string                 `json:"plugin"`
	Data      map[string]interface{} `json:"data,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// EventHandler handles plugin events
type EventHandler interface {
	Handle(ctx context.Context, event Event) error
}

// EventBus manages plugin events
type EventBus interface {
	Subscribe(eventType string, handler EventHandler) error
	Unsubscribe(eventType string, handler EventHandler) error
	Publish(ctx context.Context, event Event) error
	PublishAsync(ctx context.Context, event Event) error
	Shutdown(ctx context.Context) error
	GetStatistics() EventBusStatistics
}

// Configuration management

// ConfigManager manages plugin configurations
type ConfigManager interface {
	Get(plugin string) (map[string]interface{}, error)
	Set(plugin string, config map[string]interface{}) error
	Update(plugin string, updates map[string]interface{}) error
	Delete(plugin string) error

	// Schema validation
	ValidateConfig(plugin string, config map[string]interface{}) error
	GetSchema(plugin string) (map[string]interface{}, error)

	// Defaults
	GetDefaults(plugin string) (map[string]interface{}, error)
	ApplyDefaults(plugin string, config map[string]interface{}) map[string]interface{}

	// Environment overrides
	ApplyEnvironmentOverrides(plugin string, config map[string]interface{}) map[string]interface{}

	// Change notifications
	OnConfigChange(plugin string, callback func(map[string]interface{})) error

	GetStatistics() map[string]interface{}
}

// Security and sandboxing

// Sandbox provides security sandboxing for plugins
type Sandbox interface {
	// Execution sandbox
	Execute(plugin Plugin, operation func() error) error

	// Resource limits
	SetMemoryLimit(plugin Plugin, limit int64) error
	SetCPULimit(plugin Plugin, limit float64) error
	SetNetworkAccess(plugin Plugin, allowed bool) error
	SetFileSystemAccess(plugin Plugin, paths []string) error

	// Permission checking
	CheckPermission(plugin Plugin, permission string) error
	GrantPermission(plugin Plugin, permission string) error
	RevokePermission(plugin Plugin, permission string) error

	// Monitoring
	GetResourceUsage(plugin Plugin) ResourceUsage
	GetViolations(plugin Plugin) []SecurityViolation

	GetSandboxStatistics() map[string]interface{}
}

// ResourceUsage represents plugin resource usage
type ResourceUsage struct {
	MemoryUsage int64         `json:"memory_usage"`
	CPUUsage    float64       `json:"cpu_usage"`
	NetworkIO   int64         `json:"network_io"`
	DiskIO      int64         `json:"disk_io"`
	Goroutines  int           `json:"goroutines"`
	Duration    time.Duration `json:"duration"`
}

// SecurityViolation represents a security violation
type SecurityViolation struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Timestamp   time.Time `json:"timestamp"`
	Severity    string    `json:"severity"`
	Action      string    `json:"action"` // block, warn, log
}

// Error types
var (
	ErrPluginNotFound      = fmt.Errorf("plugin not found")
	ErrPluginExists        = fmt.Errorf("plugin already exists")
	ErrPluginNotEnabled    = fmt.Errorf("plugin not enabled")
	ErrPluginNotLoaded     = fmt.Errorf("plugin not loaded")
	ErrInvalidPlugin       = fmt.Errorf("invalid plugin")
	ErrDependencyMissing   = fmt.Errorf("dependency missing")
	ErrPermissionDenied    = fmt.Errorf("permission denied")
	ErrConfigInvalid       = fmt.Errorf("config invalid")
	ErrVersionIncompatible = fmt.Errorf("version incompatible")
	ErrSecurityViolation   = fmt.Errorf("security violation")
)

// Configuration for plugin system components

// ManagerConfig represents plugin manager configuration
type ManagerConfig struct {
	PluginDirectory string              `mapstructure:"plugin_directory" yaml:"plugin_directory"`
	ConfigDirectory string              `mapstructure:"config_directory" yaml:"config_directory"`
	AutoLoad        bool                `mapstructure:"auto_load" yaml:"auto_load"`
	EnableSandbox   bool                `mapstructure:"enable_sandbox" yaml:"enable_sandbox"`
	Security        core.SecurityConfig `mapstructure:"security" yaml:"security"`
	Registry        RegistryConfig      `mapstructure:"registry" yaml:"registry"`
	Loader          LoaderConfig        `mapstructure:"loader" yaml:"loader"`
}

// RegistryConfig represents plugin registry configuration
type RegistryConfig struct {
	Type     string            `mapstructure:"type" yaml:"type"` // local, remote, hybrid
	URL      string            `mapstructure:"url" yaml:"url"`
	CacheDir string            `mapstructure:"cache_dir" yaml:"cache_dir"`
	TTL      time.Duration     `mapstructure:"ttl" yaml:"ttl"`
	Auth     map[string]string `mapstructure:"auth" yaml:"auth"`
}

// LoaderConfig represents plugin loader configuration
type LoaderConfig struct {
	Timeout         time.Duration `mapstructure:"timeout" yaml:"timeout"`
	MaxPluginSize   int64         `mapstructure:"max_plugin_size" yaml:"max_plugin_size"`
	AllowedFormats  []string      `mapstructure:"allowed_formats" yaml:"allowed_formats"`
	VerifySignature bool          `mapstructure:"verify_signature" yaml:"verify_signature"`
	PublicKey       string        `mapstructure:"public_key" yaml:"public_key"`
}

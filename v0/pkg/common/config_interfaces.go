package common

import (
	"context"
	"reflect"
	"time"
)

// =============================================================================
// SECRETS MANAGEMENT
// =============================================================================

// SecretsManager manages secrets for configuration
type SecretsManager interface {
	// GetSecret retrieves a secret by key
	GetSecret(ctx context.Context, key string) (string, error)

	// SetSecret stores a secret
	SetSecret(ctx context.Context, key, value string) error

	// DeleteSecret removes a secret
	DeleteSecret(ctx context.Context, key string) error

	// ListSecrets returns all secret keys
	ListSecrets(ctx context.Context) ([]string, error)

	// RotateSecret rotates a secret with a new value
	RotateSecret(ctx context.Context, key, newValue string) error

	// RegisterProvider registers a secrets provider
	RegisterProvider(name string, provider SecretProvider) error

	// GetProvider returns a secrets provider by name
	GetProvider(name string) (SecretProvider, error)

	// RefreshSecrets refreshes all cached secrets
	RefreshSecrets(ctx context.Context) error

	// Start starts the secrets manager
	Start(ctx context.Context) error

	// Stop stops the secrets manager
	Stop(ctx context.Context) error

	// HealthCheck performs a health check
	HealthCheck(ctx context.Context) error
}

// SecretProvider defines an interface for different secret backends
type SecretProvider interface {
	// Name returns the provider name
	Name() string

	// GetSecret retrieves a secret
	GetSecret(ctx context.Context, key string) (string, error)

	// SetSecret stores a secret
	SetSecret(ctx context.Context, key, value string) error

	// DeleteSecret removes a secret
	DeleteSecret(ctx context.Context, key string) error

	// ListSecrets returns all secret keys
	ListSecrets(ctx context.Context) ([]string, error)

	// HealthCheck performs a health check
	HealthCheck(ctx context.Context) error

	// SupportsRotation returns true if the provider supports secret rotation
	SupportsRotation() bool

	// SupportsCaching returns true if the provider supports caching
	SupportsCaching() bool

	// Initialize initializes the provider
	Initialize(ctx context.Context, config map[string]interface{}) error

	// Close closes the provider
	Close(ctx context.Context) error
}

// =============================================================================
// CONFIGURATION MANAGEMENT
// =============================================================================

// ConfigManager defines the comprehensive interface for configuration management
type ConfigManager interface {
	// Lifecycle
	Name() string
	SecretsManager() SecretsManager

	// Loading and management
	LoadFrom(sources ...ConfigSource) error
	Watch(ctx context.Context) error
	Reload() error
	ReloadContext(ctx context.Context) error
	Validate() error
	Stop() error

	// Basic getters with optional variadic defaults
	Get(key string) interface{}
	GetString(key string, defaultValue ...string) string
	GetInt(key string, defaultValue ...int) int
	GetInt8(key string, defaultValue ...int8) int8
	GetInt16(key string, defaultValue ...int16) int16
	GetInt32(key string, defaultValue ...int32) int32
	GetInt64(key string, defaultValue ...int64) int64
	GetUint(key string, defaultValue ...uint) uint
	GetUint8(key string, defaultValue ...uint8) uint8
	GetUint16(key string, defaultValue ...uint16) uint16
	GetUint32(key string, defaultValue ...uint32) uint32
	GetUint64(key string, defaultValue ...uint64) uint64
	GetFloat32(key string, defaultValue ...float32) float32
	GetFloat64(key string, defaultValue ...float64) float64
	GetBool(key string, defaultValue ...bool) bool
	GetDuration(key string, defaultValue ...time.Duration) time.Duration
	GetTime(key string, defaultValue ...time.Time) time.Time
	GetSizeInBytes(key string, defaultValue ...uint64) uint64

	// Collection getters with optional variadic defaults
	GetStringSlice(key string, defaultValue ...[]string) []string
	GetIntSlice(key string, defaultValue ...[]int) []int
	GetInt64Slice(key string, defaultValue ...[]int64) []int64
	GetFloat64Slice(key string, defaultValue ...[]float64) []float64
	GetBoolSlice(key string, defaultValue ...[]bool) []bool
	GetStringMap(key string, defaultValue ...map[string]string) map[string]string
	GetStringMapString(key string, defaultValue ...map[string]string) map[string]string
	GetStringMapStringSlice(key string, defaultValue ...map[string][]string) map[string][]string

	// Advanced getters with functional options
	GetWithOptions(key string, opts ...GetOption) (interface{}, error)
	GetStringWithOptions(key string, opts ...GetOption) (string, error)
	GetIntWithOptions(key string, opts ...GetOption) (int, error)
	GetBoolWithOptions(key string, opts ...GetOption) (bool, error)
	GetDurationWithOptions(key string, opts ...GetOption) (time.Duration, error)

	// Configuration modification
	Set(key string, value interface{})

	// Binding methods
	Bind(key string, target interface{}) error
	BindWithDefault(key string, target interface{}, defaultValue interface{}) error
	BindWithOptions(key string, target interface{}, options BindOptions) error

	// Watching and callbacks
	WatchWithCallback(key string, callback func(string, interface{}))
	WatchChanges(callback func(ConfigChange))

	// Metadata and introspection
	GetSourceMetadata() map[string]*SourceMetadata
	GetKeys() []string
	GetSection(key string) map[string]interface{}
	HasKey(key string) bool
	IsSet(key string) bool
	Size() int

	// Structure operations
	Sub(key string) ConfigManager
	MergeWith(other ConfigManager) error
	Clone() ConfigManager
	GetAllSettings() map[string]interface{}

	// Utility methods
	Reset()
	ExpandEnvVars() error
	SafeGet(key string, expectedType reflect.Type) (interface{}, error)

	// Compatibility aliases
	GetBytesSize(key string, defaultValue ...uint64) uint64
	InConfig(key string) bool
	UnmarshalKey(key string, rawVal interface{}) error
	Unmarshal(rawVal interface{}) error
	AllKeys() []string
	AllSettings() map[string]interface{}
	ReadInConfig() error
	SetConfigType(configType string)
	SetConfigFile(filePath string) error
	ConfigFileUsed() string
	WatchConfig() error
	OnConfigChange(callback func(ConfigChange))
}

// GetOption defines functional options for advanced get operations
type GetOption func(*GetOptions)

// GetOptions contains options for advanced get operations
type GetOptions struct {
	Default    interface{}
	Required   bool
	Validator  func(interface{}) error
	Transform  func(interface{}) interface{}
	OnMissing  func(string) interface{}
	AllowEmpty bool
	CacheKey   string
}

// ConfigSource represents a source of configuration data
type ConfigSource interface {
	// Name returns the unique name of the configuration source
	Name() string

	// Priority returns the priority of this source (higher = more important)
	Priority() int

	// Load loads configuration data from the source
	Load(ctx context.Context) (map[string]interface{}, error)

	// Watch starts watching for configuration changes
	Watch(ctx context.Context, callback func(map[string]interface{})) error

	// StopWatch stops watching for configuration changes
	StopWatch() error

	// Reload forces a reload of the configuration source
	Reload(ctx context.Context) error

	// IsWatchable returns true if the source supports watching for changes
	IsWatchable() bool

	// SupportsSecrets returns true if the source supports secret management
	SupportsSecrets() bool

	// GetSecret retrieves a secret value from the source
	GetSecret(ctx context.Context, key string) (string, error)

	// IsAvailable checks if the source is available
	IsAvailable(ctx context.Context) bool

	// GetName returns the name of the source (alias for Name)
	GetName() string

	// GetType returns the type of the source
	GetType() string
}

// SourceMetadata contains metadata about a configuration source
type SourceMetadata struct {
	Name         string                 `json:"name"`
	Priority     int                    `json:"priority"`
	Type         string                 `json:"type"`
	LastLoaded   time.Time              `json:"last_loaded"`
	LastModified time.Time              `json:"last_modified"`
	IsWatching   bool                   `json:"is_watching"`
	KeyCount     int                    `json:"key_count"`
	ErrorCount   int                    `json:"error_count"`
	LastError    string                 `json:"last_error,omitempty"`
	Properties   map[string]interface{} `json:"properties"`
}

// ConfigChange represents a configuration change event
type ConfigChange struct {
	Source    string      `json:"source"`
	Type      ChangeType  `json:"type"`
	Key       string      `json:"key"`
	OldValue  interface{} `json:"old_value,omitempty"`
	NewValue  interface{} `json:"new_value,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// ChangeType represents the type of configuration change
type ChangeType string

const (
	ChangeTypeSet    ChangeType = "set"
	ChangeTypeUpdate ChangeType = "update"
	ChangeTypeDelete ChangeType = "delete"
	ChangeTypeReload ChangeType = "reload"
)

// BindOptions provides flexible options for binding configuration to structs
type BindOptions struct {
	// DefaultValue to use when the key is not found or is nil
	DefaultValue interface{}

	// UseDefaults when true, uses struct field default values for missing keys
	UseDefaults bool

	// IgnoreCase when true, performs case-insensitive key matching
	IgnoreCase bool

	// TagName specifies which struct tag to use for field mapping (default: "yaml")
	TagName string

	// Required specifies field names that must be present in the configuration
	Required []string

	// ErrorOnMissing when true, returns error if any field cannot be bound
	ErrorOnMissing bool

	// DeepMerge when true, performs deep merging for nested structs
	DeepMerge bool
}

// DefaultBindOptions returns default bind options
func DefaultBindOptions() BindOptions {
	return BindOptions{
		UseDefaults:    true,
		IgnoreCase:     false,
		TagName:        "yaml",
		Required:       []string{},
		ErrorOnMissing: false,
		DeepMerge:      true,
	}
}

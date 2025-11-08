package core

import (
	"context"
	"reflect"
	"time"
)

// =============================================================================
// CONFIGURATION MANAGEMENT
// =============================================================================

// ConfigManager defines the comprehensive interface for configuration management.
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
	Get(key string) any
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
	GetWithOptions(key string, opts ...GetOption) (any, error)
	GetStringWithOptions(key string, opts ...GetOption) (string, error)
	GetIntWithOptions(key string, opts ...GetOption) (int, error)
	GetBoolWithOptions(key string, opts ...GetOption) (bool, error)
	GetDurationWithOptions(key string, opts ...GetOption) (time.Duration, error)

	// Configuration modification
	Set(key string, value any)

	// Binding methods
	Bind(key string, target any) error
	BindWithDefault(key string, target any, defaultValue any) error
	BindWithOptions(key string, target any, options BindOptions) error

	// Watching and callbacks
	WatchWithCallback(key string, callback func(string, any))
	WatchChanges(callback func(ConfigChange))

	// Metadata and introspection
	GetSourceMetadata() map[string]*SourceMetadata
	GetKeys() []string
	GetSection(key string) map[string]any
	HasKey(key string) bool
	IsSet(key string) bool
	Size() int

	// Structure operations
	Sub(key string) ConfigManager
	MergeWith(other ConfigManager) error
	Clone() ConfigManager
	GetAllSettings() map[string]any

	// Utility methods
	Reset()
	ExpandEnvVars() error
	SafeGet(key string, expectedType reflect.Type) (any, error)

	// Compatibility aliases
	GetBytesSize(key string, defaultValue ...uint64) uint64
	InConfig(key string) bool
	UnmarshalKey(key string, rawVal any) error
	Unmarshal(rawVal any) error
	AllKeys() []string
	AllSettings() map[string]any
	ReadInConfig() error
	SetConfigType(configType string)
	SetConfigFile(filePath string) error
	ConfigFileUsed() string
	WatchConfig() error
	OnConfigChange(callback func(ConfigChange))
}

// GetOption defines functional options for advanced get operations.
type GetOption func(*GetOptions)

// GetOptions contains options for advanced get operations.
type GetOptions struct {
	Default    any
	Required   bool
	Validator  func(any) error
	Transform  func(any) any
	OnMissing  func(string) any
	AllowEmpty bool
	CacheKey   string
}

package forge

import (
	"fmt"
	"reflect"
	"time"

	"github.com/xraph/confy"
	"github.com/xraph/confy/sources"
	"github.com/xraph/forge/config"
)

// =============================================================================
// CONFIGURATION CONSTANTS
// =============================================================================

// ConfigKey is the service key for configuration manager
// Use config.ManagerKey or shared.ConfigKey for consistency.
const ConfigKey = "config"

// =============================================================================
// RE-EXPORT CONFIG TYPES
// =============================================================================

// ConfigManager is the configuration manager interface.
type ConfigManager = confy.Confy

// Configuration Sources.
type (
	ConfigSource        = confy.ConfigSource
	ConfigSourceOptions = confy.ConfigSourceOptions
	ConfigSourceFactory = confy.ConfigSourceFactory
	SourceConfig        = confy.SourceConfig
	SourceMetadata      = confy.SourceMetadata
	SourceRegistry      = confy.SourceRegistry
	SourceEvent         = confy.SourceEvent
	SourceEventHandler  = confy.SourceEventHandler
	WatchContext        = confy.WatchContext
)

// Configuration Options.
type (
	GetOption   = confy.GetOption
	GetOptions  = confy.GetOptions
	BindOptions = confy.BindOptions
)

// Configuration Changes.
type (
	ConfigChange = confy.ConfigChange
	ChangeType   = confy.ChangeType
)

// Constants.
const (
	ChangeTypeSet    = confy.ChangeTypeSet
	ChangeTypeUpdate = confy.ChangeTypeUpdate
	ChangeTypeDelete = confy.ChangeTypeDelete
	ChangeTypeReload = confy.ChangeTypeReload
)

// Validation.
type (
	ValidationOptions = confy.ValidationOptions
	ValidationRule    = confy.ValidationRule
	ValidationConfig  = confy.ValidationConfig
	ValidationMode    = confy.ValidationMode
	Validator         = confy.Validator
)

const (
	ValidationModePermissive = confy.ValidationModePermissive
	ValidationModeStrict     = confy.ValidationModeStrict
	ValidationModeLoose      = confy.ValidationModeLoose
)

// Secrets.
type (
	SecretsManager = confy.SecretsManager
	SecretProvider = confy.SecretProvider
	SecretsConfig  = confy.SecretsConfig
)

// Manager Configuration.
type (
	ConfigConfig = confy.Config
)

// Environment Variable Source Types.
type (
	EnvSourceOptions = sources.EnvSourceOptions
	EnvSourceConfig  = sources.EnvSourceConfig
)

// Auto-Discovery Types.
type (
	AutoDiscoveryConfig = confy.AutoDiscoveryConfig
	AutoDiscoveryResult = confy.AutoDiscoveryResult
)

// Watcher.
type (
	Watcher       = confy.Watcher
	WatcherConfig = confy.WatcherConfig
)

// =============================================================================
// RE-EXPORT CONFIG CONSTRUCTORS
// =============================================================================

var (
	NewManager        = confy.New
	NewSourceRegistry = confy.NewSourceRegistry
	NewValidator      = confy.NewValidator
	NewWatcher        = confy.NewWatcher
	NewSecretsManager = confy.NewSecretsManager

	// NewEnvSource creates an environment variable source.
	NewEnvSource = sources.NewEnvSource

	// DiscoverAndLoadConfigs discovers and loads configuration files automatically.
	DiscoverAndLoadConfigs = confy.DiscoverAndLoadConfigs
	// DefaultAutoDiscoveryConfig returns the default auto-discovery configuration.
	DefaultAutoDiscoveryConfig = confy.DefaultAutoDiscoveryConfig
)

// =============================================================================
// RE-EXPORT CONFIG OPTIONS (Functional Options)
// =============================================================================

var (
	WithDefault   = confy.WithDefault
	WithRequired  = confy.WithRequired
	WithValidator = confy.WithValidator
	WithTransform = confy.WithTransform
	WithOnMissing = confy.WithOnMissing
	AllowEmpty    = confy.AllowEmpty
	WithCacheKey  = confy.WithCacheKey
)

// =============================================================================
// DEFAULT BIND OPTIONS
// =============================================================================

// DefaultBindOptions returns default bind options.
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

// =============================================================================
// ERROR HANDLER FOR CONFIG
// =============================================================================

// ConfigErrorHandler defines how errors should be handled in the config system.
type ConfigErrorHandler interface {
	// HandleError handles an error
	HandleError(ctx Context, err error) error

	// ShouldRetry determines if an operation should be retried
	ShouldRetry(err error) bool

	// GetRetryDelay returns the delay before retrying
	GetRetryDelay(attempt int, err error) time.Duration
}

// DefaultConfigErrorHandler is the default error handler for config.
type DefaultConfigErrorHandler struct {
	logger Logger
}

// NewDefaultConfigErrorHandler creates a new default config error handler.
func NewDefaultConfigErrorHandler(logger Logger) *DefaultConfigErrorHandler {
	return &DefaultConfigErrorHandler{
		logger: logger,
	}
}

// HandleError handles an error.
func (h *DefaultConfigErrorHandler) HandleError(ctx Context, err error) error {
	if h.logger != nil {
		h.logger.Error("config error occurred",
			Error(err),
		)
	}

	return err
}

// ShouldRetry determines if an operation should be retried.
func (h *DefaultConfigErrorHandler) ShouldRetry(err error) bool {
	return false // Config errors generally shouldn't auto-retry
}

// GetRetryDelay returns the delay before retrying.
func (h *DefaultConfigErrorHandler) GetRetryDelay(attempt int, err error) time.Duration {
	return time.Duration(100*attempt*attempt) * time.Millisecond
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// SafeGet returns a value with type checking.
func SafeGet[T any](cm ConfigManager, key string) (T, error) {
	var zero T

	value := cm.Get(key)
	if value == nil {
		return zero, fmt.Errorf("config not found: %s", key)
	}

	result, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("config type mismatch for key %s: expected %v, got %v", key, reflect.TypeOf(zero), reflect.TypeOf(value))
	}

	return result, nil
}

// MustGet returns a value or panics if not found.
func MustGet[T any](cm ConfigManager, key string) T {
	value, err := SafeGet[T](cm, key)
	if err != nil {
		panic(err)
	}

	return value
}

// GetConfigManager resolves the config manager from the container
// Returns the config manager instance and an error if resolution fails.
func GetConfigManager(c Container) (ConfigManager, error) {
	return config.GetConfigManager(c)
}

// GetConfy resolves the confy from the container
// Returns the confy instance and an error if resolution fails.
func GetConfy(c Container) (confy.Confy, error) {
	return config.GetConfy(c)
}

package forge

import (
	"fmt"
	"reflect"
	"time"

	"github.com/xraph/forge/v2/internal/config"
	configcore "github.com/xraph/forge/v2/internal/config/core"
)

// =============================================================================
// CONFIGURATION CONSTANTS
// =============================================================================

// ConfigKey is the service key for configuration manager
const ConfigKey = "config"

// =============================================================================
// RE-EXPORT CONFIG TYPES
// =============================================================================

// Configuration Manager
type ConfigManager = configcore.ConfigManager

// Configuration Sources
type (
	ConfigSource        = config.ConfigSource
	ConfigSourceOptions = config.ConfigSourceOptions
	ConfigSourceFactory = config.ConfigSourceFactory
	SourceConfig        = config.SourceConfig
	SourceMetadata      = config.SourceMetadata
	SourceRegistry      = config.SourceRegistry
	SourceEvent         = config.SourceEvent
	SourceEventHandler  = config.SourceEventHandler
	WatchContext        = config.WatchContext
)

// Configuration Options
type (
	GetOption   = configcore.GetOption
	GetOptions  = configcore.GetOptions
	BindOptions = configcore.BindOptions
)

// Configuration Changes
type (
	ConfigChange = config.ConfigChange
	ChangeType   = config.ChangeType
)

// Constants
const (
	ChangeTypeSet    = config.ChangeTypeSet
	ChangeTypeUpdate = config.ChangeTypeUpdate
	ChangeTypeDelete = config.ChangeTypeDelete
	ChangeTypeReload = config.ChangeTypeReload
)

// Validation
type (
	ValidationOptions = config.ValidationOptions
	ValidationRule    = config.ValidationRule
	ValidationConfig  = config.ValidationConfig
	ValidationMode    = config.ValidationMode
	Validator         = config.Validator
)

const (
	ValidationModePermissive = config.ValidationModePermissive
	ValidationModeStrict     = config.ValidationModeStrict
	ValidationModeLoose      = config.ValidationModeLoose
)

// Secrets
type (
	SecretsManager = config.SecretsManager
	SecretProvider = config.SecretProvider
	SecretsConfig  = config.SecretsConfig
)

// Manager Configuration
type (
	ManagerConfig = config.ManagerConfig
)

// Watcher
type (
	Watcher       = config.Watcher
	WatcherConfig = config.WatcherConfig
)

// =============================================================================
// RE-EXPORT CONFIG CONSTRUCTORS
// =============================================================================

var (
	NewManager        = config.NewManager
	NewSourceRegistry = config.NewSourceRegistry
	NewValidator      = config.NewValidator
	NewWatcher        = config.NewWatcher
	NewSecretsManager = config.NewSecretsManager
)

// =============================================================================
// RE-EXPORT CONFIG OPTIONS (Functional Options)
// =============================================================================

var (
	WithDefault   = config.WithDefault
	WithRequired  = config.WithRequired
	WithValidator = config.WithValidator
	WithTransform = config.WithTransform
	WithOnMissing = config.WithOnMissing
	AllowEmpty    = config.AllowEmpty
	WithCacheKey  = config.WithCacheKey
)

// =============================================================================
// DEFAULT BIND OPTIONS
// =============================================================================

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

// =============================================================================
// ERROR HANDLER FOR CONFIG
// =============================================================================

// ConfigErrorHandler defines how errors should be handled in the config system
type ConfigErrorHandler interface {
	// HandleError handles an error
	HandleError(ctx Context, err error) error

	// ShouldRetry determines if an operation should be retried
	ShouldRetry(err error) bool

	// GetRetryDelay returns the delay before retrying
	GetRetryDelay(attempt int, err error) time.Duration
}

// DefaultConfigErrorHandler is the default error handler for config
type DefaultConfigErrorHandler struct {
	logger Logger
}

// NewDefaultConfigErrorHandler creates a new default config error handler
func NewDefaultConfigErrorHandler(logger Logger) *DefaultConfigErrorHandler {
	return &DefaultConfigErrorHandler{
		logger: logger,
	}
}

// HandleError handles an error
func (h *DefaultConfigErrorHandler) HandleError(ctx Context, err error) error {
	if h.logger != nil {
		h.logger.Error("config error occurred",
			Error(err),
		)
	}
	return err
}

// ShouldRetry determines if an operation should be retried
func (h *DefaultConfigErrorHandler) ShouldRetry(err error) bool {
	return false // Config errors generally shouldn't auto-retry
}

// GetRetryDelay returns the delay before retrying
func (h *DefaultConfigErrorHandler) GetRetryDelay(attempt int, err error) time.Duration {
	return time.Duration(100*attempt*attempt) * time.Millisecond
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// SafeGet returns a value with type checking
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

// MustGet returns a value or panics if not found
func MustGet[T any](cm ConfigManager, key string) T {
	value, err := SafeGet[T](cm, key)
	if err != nil {
		panic(err)
	}
	return value
}

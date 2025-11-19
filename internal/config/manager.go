package config

//nolint:gosec // G115: All integer type conversions are intentional and value-controlled
// This file contains helper methods for type-safe configuration value retrieval.
// The integer conversions are safe because values come from application configuration sources.

import (
	"context"
	"fmt"
	"maps"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	configcore "github.com/xraph/forge/internal/config/core"
	configformats "github.com/xraph/forge/internal/config/formats"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// ManagerKey is the DI key for the config manager service.
const ManagerKey = shared.ConfigKey

// =============================================================================
// MANAGER IMPLEMENTATION
// =============================================================================

// Manager implements an enhanced configuration manager that extends ConfigManager.
type Manager struct {
	sources         []ConfigSource
	registry        SourceRegistry
	loader          *configformats.Loader
	validator       *Validator
	watcher         *Watcher
	data            map[string]any
	watchCallbacks  map[string][]func(string, any)
	changeCallbacks []func(ConfigChange)
	mu              sync.RWMutex
	watchCtx        context.Context
	watchCancel     context.CancelFunc
	started         bool
	logger          logger.Logger
	metrics         shared.Metrics
	errorHandler    shared.ErrorHandler
	secretsManager  configcore.SecretsManager
}

// ManagerConfig contains configuration for the config manager.
type ManagerConfig struct {
	DefaultSources  []SourceConfig      `json:"default_sources"   yaml:"default_sources"`
	WatchInterval   time.Duration       `json:"watch_interval"    yaml:"watch_interval"`
	ValidationMode  ValidationMode      `json:"validation_mode"   yaml:"validation_mode"`
	SecretsEnabled  bool                `json:"secrets_enabled"   yaml:"secrets_enabled"`
	CacheEnabled    bool                `json:"cache_enabled"     yaml:"cache_enabled"`
	ReloadOnChange  bool                `json:"reload_on_change"  yaml:"reload_on_change"`
	ErrorRetryCount int                 `json:"error_retry_count" yaml:"error_retry_count"`
	ErrorRetryDelay time.Duration       `json:"error_retry_delay" yaml:"error_retry_delay"`
	MetricsEnabled  bool                `json:"metrics_enabled"   yaml:"metrics_enabled"`
	Logger          logger.Logger       `json:"-"                 yaml:"-"`
	Metrics         shared.Metrics      `json:"-"                 yaml:"-"`
	ErrorHandler    shared.ErrorHandler `json:"-"                 yaml:"-"`
}

// NewManager creates a new enhanced configuration manager.
func NewManager(config ManagerConfig) ConfigManager {
	if config.WatchInterval == 0 {
		config.WatchInterval = 30 * time.Second
	}

	if config.ErrorRetryCount == 0 {
		config.ErrorRetryCount = 3
	}

	if config.ErrorRetryDelay == 0 {
		config.ErrorRetryDelay = 5 * time.Second
	}

	manager := &Manager{
		sources:         make([]ConfigSource, 0),
		data:            make(map[string]any),
		watchCallbacks:  make(map[string][]func(string, any)),
		changeCallbacks: make([]func(ConfigChange), 0),
		logger:          config.Logger,
		metrics:         config.Metrics,
		errorHandler:    config.ErrorHandler,
	}

	manager.registry = NewSourceRegistry(manager.logger)
	manager.loader = configformats.NewLoader(configformats.LoaderConfig{
		Logger:       manager.logger,
		Metrics:      manager.metrics,
		ErrorHandler: manager.errorHandler,
		RetryCount:   config.ErrorRetryCount,
		RetryDelay:   config.ErrorRetryDelay,
	})
	manager.validator = NewValidator(ValidatorConfig{
		Mode:         config.ValidationMode,
		Logger:       manager.logger,
		ErrorHandler: manager.errorHandler,
	})
	manager.watcher = NewWatcher(WatcherConfig{
		Interval:     config.WatchInterval,
		Logger:       manager.logger,
		Metrics:      manager.metrics,
		ErrorHandler: manager.errorHandler,
	})

	if config.SecretsEnabled {
		manager.secretsManager = NewSecretsManager(SecretsConfig{
			Logger:       manager.logger,
			ErrorHandler: manager.errorHandler,
		})
	}

	return manager
}

func (m *Manager) Name() string {
	return ManagerKey
}

func (m *Manager) SecretsManager() SecretsManager {
	return m.secretsManager
}

// =============================================================================
// SIMPLE API - VARIADIC DEFAULTS
// =============================================================================

// Get returns a configuration value.
func (m *Manager) Get(key string) any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getValue(key)
}

// GetString returns a string value with optional default.
func (m *Manager) GetString(key string, defaultValue ...string) string {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return ""
	}

	return m.convertToString(value)
}

// GetInt returns an int value with optional default.
func (m *Manager) GetInt(key string, defaultValue ...int) int {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case uint:
		return int(v)
	case uint8:
		return int(v)
	case uint16:
		return int(v)
	case uint32:
		return int(v)
	case uint64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetInt8 returns an int8 value with optional default.
func (m *Manager) GetInt8(key string, defaultValue ...int8) int8 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case int8:
		return v
	case int:
		if v >= -128 && v <= 127 {
			return int8(v)
		}
	case int16:
		if v >= -128 && v <= 127 {
			return int8(v)
		}
	case int32:
		if v >= -128 && v <= 127 {
			return int8(v)
		}
	case int64:
		if v >= -128 && v <= 127 {
			return int8(v)
		}
	case uint8:
		if v <= 127 {
			return int8(v)
		}
	case float32:
		if v >= -128 && v <= 127 && v == float32(int8(v)) {
			return int8(v)
		}
	case float64:
		if v >= -128 && v <= 127 && v == float64(int8(v)) {
			return int8(v)
		}
	case string:
		if i, err := strconv.ParseInt(v, 10, 8); err == nil {
			return int8(i)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetInt16 returns an int16 value with optional default.
func (m *Manager) GetInt16(key string, defaultValue ...int16) int16 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case int16:
		return v
	case int:
		if v >= -32768 && v <= 32767 {
			return int16(v)
		}
	case int8:
		return int16(v)
	case int32:
		if v >= -32768 && v <= 32767 {
			return int16(v)
		}
	case int64:
		if v >= -32768 && v <= 32767 {
			return int16(v)
		}
	case uint16:
		if v <= 32767 {
			return int16(v)
		}
	case float32:
		if v >= -32768 && v <= 32767 && v == float32(int16(v)) {
			return int16(v)
		}
	case float64:
		if v >= -32768 && v <= 32767 && v == float64(int16(v)) {
			return int16(v)
		}
	case string:
		if i, err := strconv.ParseInt(v, 10, 16); err == nil {
			return int16(i)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetInt32 returns an int32 value with optional default.
func (m *Manager) GetInt32(key string, defaultValue ...int32) int32 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case int32:
		return v
	case int:
		return int32(v)
	case int8:
		return int32(v)
	case int16:
		return int32(v)
	case int64:
		return int32(v)
	case uint32:
		return int32(v)
	case float32:
		return int32(v)
	case float64:
		return int32(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 32); err == nil {
			return int32(i)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetInt64 returns an int64 value with optional default.
func (m *Manager) GetInt64(key string, defaultValue ...int64) int64 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case uint64:
		return int64(v)
	case float32:
		return int64(v)
	case float64:
		return int64(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 64); err == nil {
			return i
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetUint returns a uint value with optional default.
func (m *Manager) GetUint(key string, defaultValue ...uint) uint {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case uint:
		return v
	case uint8:
		return uint(v)
	case uint16:
		return uint(v)
	case uint32:
		return uint(v)
	case uint64:
		return uint(v)
	case int:
		if v >= 0 {
			return uint(v)
		}
	case int64:
		if v >= 0 {
			return uint(v)
		}
	case float64:
		if v >= 0 {
			return uint(v)
		}
	case string:
		if i, err := strconv.ParseUint(v, 10, 0); err == nil {
			return uint(i)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetUint8 returns a uint8 value with optional default.
func (m *Manager) GetUint8(key string, defaultValue ...uint8) uint8 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case uint8:
		return v
	case uint:
		if v <= 255 {
			return uint8(v)
		}
	case uint16:
		if v <= 255 {
			return uint8(v)
		}
	case uint32:
		if v <= 255 {
			return uint8(v)
		}
	case uint64:
		if v <= 255 {
			return uint8(v)
		}
	case int:
		if v >= 0 && v <= 255 {
			return uint8(v)
		}
	case float64:
		if v >= 0 && v <= 255 && v == float64(uint8(v)) {
			return uint8(v)
		}
	case string:
		if i, err := strconv.ParseUint(v, 10, 8); err == nil {
			return uint8(i)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetUint16 returns a uint16 value with optional default.
func (m *Manager) GetUint16(key string, defaultValue ...uint16) uint16 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case uint16:
		return v
	case uint:
		return uint16(v)
	case uint8:
		return uint16(v)
	case uint32:
		return uint16(v)
	case uint64:
		return uint16(v)
	case int:
		if v >= 0 {
			return uint16(v)
		}
	case float64:
		if v >= 0 {
			return uint16(v)
		}
	case string:
		if i, err := strconv.ParseUint(v, 10, 16); err == nil {
			return uint16(i)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetUint32 returns a uint32 value with optional default.
func (m *Manager) GetUint32(key string, defaultValue ...uint32) uint32 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case uint32:
		return v
	case uint:
		return uint32(v)
	case uint8:
		return uint32(v)
	case uint16:
		return uint32(v)
	case uint64:
		return uint32(v)
	case int:
		if v >= 0 {
			return uint32(v)
		}
	case float64:
		if v >= 0 {
			return uint32(v)
		}
	case string:
		if i, err := strconv.ParseUint(v, 10, 32); err == nil {
			return uint32(i)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetUint64 returns a uint64 value with optional default.
func (m *Manager) GetUint64(key string, defaultValue ...uint64) uint64 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case uint64:
		return v
	case uint:
		return uint64(v)
	case uint8:
		return uint64(v)
	case uint16:
		return uint64(v)
	case uint32:
		return uint64(v)
	case int:
		if v >= 0 {
			return uint64(v)
		}
	case int64:
		if v >= 0 {
			return uint64(v)
		}
	case float64:
		if v >= 0 {
			return uint64(v)
		}
	case string:
		if i, err := strconv.ParseUint(v, 10, 64); err == nil {
			return i
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetFloat32 returns a float32 value with optional default.
func (m *Manager) GetFloat32(key string, defaultValue ...float32) float32 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case float32:
		return v
	case float64:
		return float32(v)
	case int:
		return float32(v)
	case int64:
		return float32(v)
	case uint:
		return float32(v)
	case uint64:
		return float32(v)
	case string:
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			return float32(f)
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetFloat64 returns a float64 value with optional default.
func (m *Manager) GetFloat64(key string, defaultValue ...float64) float64 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint64:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetBool returns a bool value with optional default.
func (m *Manager) GetBool(key string, defaultValue ...bool) bool {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return false
	}

	switch v := value.(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	case int:
		return v != 0
	case float64:
		return v != 0
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return false
}

// GetDuration returns a duration value with optional default.
func (m *Manager) GetDuration(key string, defaultValue ...time.Duration) time.Duration {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case time.Duration:
		return v
	case string:
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	case int:
		return time.Duration(v) * time.Second
	case float64:
		return time.Duration(v) * time.Second
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetTime returns a time value with optional default.
func (m *Manager) GetTime(key string, defaultValue ...time.Time) time.Time {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return time.Time{}
	}

	switch v := value.(type) {
	case time.Time:
		return v
	case string:
		formats := []string{
			time.RFC3339,
			time.RFC3339Nano,
			"2006-01-02 15:04:05",
			"2006-01-02T15:04:05",
			"2006-01-02",
		}
		for _, format := range formats {
			if t, err := time.Parse(format, v); err == nil {
				return t
			}
		}
	case int64:
		return time.Unix(v, 0)
	case float64:
		return time.Unix(int64(v), int64((v-float64(int64(v)))*1e9))
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return time.Time{}
}

// GetSizeInBytes returns size in bytes with optional default.
func (m *Manager) GetSizeInBytes(key string, defaultValue ...uint64) uint64 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return 0
	}

	switch v := value.(type) {
	case uint64:
		return v
	case uint:
		return uint64(v)
	case int:
		if v >= 0 {
			return uint64(v)
		}
	case int64:
		if v >= 0 {
			return uint64(v)
		}
	case string:
		return m.parseSizeInBytes(v)
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return 0
}

// GetStringSlice returns a string slice with optional default.
func (m *Manager) GetStringSlice(key string, defaultValue ...[]string) []string {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return nil
	}

	switch v := value.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}

		return result
	case string:
		if strings.Contains(v, ",") {
			parts := strings.Split(v, ",")

			result := make([]string, len(parts))
			for i, part := range parts {
				result[i] = strings.TrimSpace(part)
			}

			return result
		}

		return []string{v}
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// GetIntSlice returns an int slice with optional default.
func (m *Manager) GetIntSlice(key string, defaultValue ...[]int) []int {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return nil
	}

	switch v := value.(type) {
	case []int:
		return v
	case []any:
		result := make([]int, 0, len(v))
		for _, item := range v {
			switch i := item.(type) {
			case int:
				result = append(result, i)
			case float64:
				result = append(result, int(i))
			case string:
				if num, err := strconv.Atoi(i); err == nil {
					result = append(result, num)
				}
			}
		}

		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// GetInt64Slice returns an int64 slice with optional default.
func (m *Manager) GetInt64Slice(key string, defaultValue ...[]int64) []int64 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return nil
	}

	switch v := value.(type) {
	case []int64:
		return v
	case []any:
		result := make([]int64, 0, len(v))
		for _, item := range v {
			switch i := item.(type) {
			case int64:
				result = append(result, i)
			case int:
				result = append(result, int64(i))
			case float64:
				result = append(result, int64(i))
			case string:
				if num, err := strconv.ParseInt(i, 10, 64); err == nil {
					result = append(result, num)
				}
			}
		}

		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// GetFloat64Slice returns a float64 slice with optional default.
func (m *Manager) GetFloat64Slice(key string, defaultValue ...[]float64) []float64 {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return nil
	}

	switch v := value.(type) {
	case []float64:
		return v
	case []any:
		result := make([]float64, 0, len(v))
		for _, item := range v {
			switch f := item.(type) {
			case float64:
				result = append(result, f)
			case float32:
				result = append(result, float64(f))
			case int:
				result = append(result, float64(f))
			case string:
				if num, err := strconv.ParseFloat(f, 64); err == nil {
					result = append(result, num)
				}
			}
		}

		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// GetBoolSlice returns a bool slice with optional default.
func (m *Manager) GetBoolSlice(key string, defaultValue ...[]bool) []bool {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return nil
	}

	switch v := value.(type) {
	case []bool:
		return v
	case []any:
		result := make([]bool, 0, len(v))
		for _, item := range v {
			switch b := item.(type) {
			case bool:
				result = append(result, b)
			case string:
				if val, err := strconv.ParseBool(b); err == nil {
					result = append(result, val)
				}
			case int:
				result = append(result, b != 0)
			}
		}

		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// GetStringMap returns a string map with optional default.
func (m *Manager) GetStringMap(key string, defaultValue ...map[string]string) map[string]string {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return nil
	}

	switch v := value.(type) {
	case map[string]string:
		return v
	case map[string]any:
		result := make(map[string]string)
		for k, val := range v {
			result[k] = fmt.Sprintf("%v", val)
		}

		return result
	case map[any]any:
		result := make(map[string]string)
		for k, val := range v {
			result[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", val)
		}

		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// GetStringMapString is an alias for GetStringMap.
func (m *Manager) GetStringMapString(key string, defaultValue ...map[string]string) map[string]string {
	return m.GetStringMap(key, defaultValue...)
}

// GetStringMapStringSlice returns a map of string slices with optional default.
func (m *Manager) GetStringMapStringSlice(key string, defaultValue ...map[string][]string) map[string][]string {
	value := m.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}

		return nil
	}

	switch v := value.(type) {
	case map[string][]string:
		return v
	case map[string]any:
		result := make(map[string][]string)

		for k, val := range v {
			switch slice := val.(type) {
			case []string:
				result[k] = slice
			case []any:
				strSlice := make([]string, len(slice))
				for i, item := range slice {
					strSlice[i] = fmt.Sprintf("%v", item)
				}

				result[k] = strSlice
			case string:
				result[k] = []string{slice}
			}
		}

		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}

	return nil
}

// =============================================================================
// ADVANCED API - FUNCTIONAL OPTIONS
// =============================================================================

// GetWithOptions returns a value with advanced options.
func (m *Manager) GetWithOptions(key string, opts ...configcore.GetOption) (any, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := m.Get(key)

	// Handle missing key
	if value == nil {
		if options.Required {
			return nil, ErrConfigError(fmt.Sprintf("required key '%s' not found", key), nil)
		}

		if options.OnMissing != nil {
			value = options.OnMissing(key)
		} else if options.Default != nil {
			return options.Default, nil
		} else {
			return nil, nil
		}
	}

	// Transform
	if options.Transform != nil {
		value = options.Transform(value)
	}

	// Validate
	if options.Validator != nil {
		if err := options.Validator(value); err != nil {
			return nil, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return value, nil
}

// GetStringWithOptions returns a string with advanced options.
func (m *Manager) GetStringWithOptions(key string, opts ...configcore.GetOption) (string, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := m.Get(key)

	// Handle missing key
	if value == nil {
		if options.Required {
			return "", ErrConfigError(fmt.Sprintf("required key '%s' not found", key), nil)
		}

		if options.OnMissing != nil {
			value = options.OnMissing(key)
		} else if options.Default != nil {
			value = options.Default
		} else {
			return "", nil
		}
	}

	// Transform
	if options.Transform != nil {
		value = options.Transform(value)
	}

	// Convert to string
	result := m.convertToString(value)

	// Check empty
	if !options.AllowEmpty && result == "" {
		if options.Required {
			return "", ErrConfigError(fmt.Sprintf("key '%s' is empty", key), nil)
		}

		if options.Default != nil {
			if defaultStr, ok := options.Default.(string); ok {
				result = defaultStr
			}
		}
	}

	// Validate
	if options.Validator != nil {
		if err := options.Validator(result); err != nil {
			return "", ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return result, nil
}

// GetIntWithOptions returns an int with advanced options.
func (m *Manager) GetIntWithOptions(key string, opts ...configcore.GetOption) (int, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := m.Get(key)

	// Handle missing key
	if value == nil {
		if options.Required {
			return 0, ErrConfigError(fmt.Sprintf("required key '%s' not found", key), nil)
		}

		if options.OnMissing != nil {
			value = options.OnMissing(key)
		} else if options.Default != nil {
			if defaultInt, ok := options.Default.(int); ok {
				return defaultInt, nil
			}
		} else {
			return 0, nil
		}
	}

	// Transform
	if options.Transform != nil {
		value = options.Transform(value)
	}

	// Convert to int
	result, err := m.convertToInt(value)
	if err != nil {
		if options.Default != nil {
			if defaultInt, ok := options.Default.(int); ok {
				result = int64(defaultInt)
			}
		} else {
			return 0, ErrConfigError(fmt.Sprintf("failed to convert key '%s' to int", key), err)
		}
	}

	// Validate
	if options.Validator != nil {
		if err := options.Validator(int(result)); err != nil {
			return 0, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return int(result), nil
}

// GetBoolWithOptions returns a bool with advanced options.
func (m *Manager) GetBoolWithOptions(key string, opts ...configcore.GetOption) (bool, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := m.Get(key)

	// Handle missing key
	if value == nil {
		if options.Required {
			return false, ErrConfigError(fmt.Sprintf("required key '%s' not found", key), nil)
		}

		if options.OnMissing != nil {
			value = options.OnMissing(key)
		} else if options.Default != nil {
			if defaultBool, ok := options.Default.(bool); ok {
				return defaultBool, nil
			}
		} else {
			return false, nil
		}
	}

	// Transform
	if options.Transform != nil {
		value = options.Transform(value)
	}

	// Convert to bool
	result, err := m.convertToBool(value)
	if err != nil {
		if options.Default != nil {
			if defaultBool, ok := options.Default.(bool); ok {
				result = defaultBool
			}
		} else {
			return false, ErrConfigError(fmt.Sprintf("failed to convert key '%s' to bool", key), err)
		}
	}

	// Validate
	if options.Validator != nil {
		if err := options.Validator(result); err != nil {
			return false, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return result, nil
}

// GetDurationWithOptions returns a duration with advanced options.
func (m *Manager) GetDurationWithOptions(key string, opts ...configcore.GetOption) (time.Duration, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := m.Get(key)

	// Handle missing key
	if value == nil {
		if options.Required {
			return 0, ErrConfigError(fmt.Sprintf("required key '%s' not found", key), nil)
		}

		if options.OnMissing != nil {
			value = options.OnMissing(key)
		} else if options.Default != nil {
			if defaultDur, ok := options.Default.(time.Duration); ok {
				return defaultDur, nil
			}
		} else {
			return 0, nil
		}
	}

	// Transform
	if options.Transform != nil {
		value = options.Transform(value)
	}

	// Convert to duration
	var result time.Duration

	switch v := value.(type) {
	case time.Duration:
		result = v
	case string:
		var err error

		result, err = time.ParseDuration(v)
		if err != nil {
			if options.Default != nil {
				if defaultDur, ok := options.Default.(time.Duration); ok {
					result = defaultDur
				}
			} else {
				return 0, ErrConfigError(fmt.Sprintf("failed to parse duration for key '%s'", key), err)
			}
		}
	case int, int64:
		if intVal, err := m.convertToInt(v); err == nil {
			result = time.Duration(intVal) * time.Second
		}
	default:
		if options.Default != nil {
			if defaultDur, ok := options.Default.(time.Duration); ok {
				result = defaultDur
			}
		}
	}

	// Validate
	if options.Validator != nil {
		if err := options.Validator(result); err != nil {
			return 0, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return result, nil
}

// =============================================================================
// LIFECYCLE METHODS (continuing from previous implementation)
// =============================================================================

// LoadFrom loads configuration from multiple sources.
func (m *Manager) LoadFrom(sources ...ConfigSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.logger != nil {
		m.logger.Info("loading configuration from sources",
			logger.Int("source_count", len(sources)),
		)
	}

	for _, source := range sources {
		if err := m.registry.RegisterSource(source); err != nil {
			return ErrConfigError("failed to register source "+source.Name(), err)
		}

		m.sources = append(m.sources, source)
	}

	if err := m.loadAllSources(context.Background()); err != nil {
		return err
	}

	if err := m.validator.ValidateAll(m.data); err != nil {
		return ErrConfigError("configuration validation failed", err)
	}

	if m.metrics != nil {
		m.metrics.Counter("config.sources_loaded").Add(float64(len(sources)))
		m.metrics.Gauge("config.active_sources").Set(float64(len(m.sources)))
		m.metrics.Gauge("config.keys_count").Set(float64(len(m.data)))
	}

	return nil
}

// Watch starts watching for configuration changes.
func (m *Manager) Watch(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return ErrLifecycleError("watch", errors.New("configuration manager already watching"))
	}

	m.watchCtx, m.watchCancel = context.WithCancel(ctx)

	for _, source := range m.sources {
		if source.IsWatchable() {
			if err := m.watcher.WatchSource(m.watchCtx, source, m.handleConfigChange); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to start watching source",
						logger.String("source", source.Name()),
						logger.Error(err),
					)
				}
			}
		}
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("configuration manager started watching")
	}

	if m.metrics != nil {
		m.metrics.Counter("config.watch_started").Inc()
	}

	return nil
}

// Reload forces a reload of all configuration sources.
func (m *Manager) Reload() error {
	return m.ReloadContext(context.Background())
}

// ReloadContext forces a reload with context.
func (m *Manager) ReloadContext(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.logger != nil {
		m.logger.Info("reloading configuration from all sources")
	}

	startTime := time.Now()

	if err := m.loadAllSources(ctx); err != nil {
		return err
	}

	if err := m.validator.ValidateAll(m.data); err != nil {
		return ErrConfigError("configuration validation failed after reload", err)
	}

	m.notifyWatchCallbacks()

	if m.metrics != nil {
		m.metrics.Counter("config.reloads").Inc()
		m.metrics.Histogram("config.reload_duration").Observe(time.Since(startTime).Seconds())
	}

	return nil
}

// Validate validates the current configuration.
func (m *Manager) Validate() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.validator.ValidateAll(m.data)
}

// Set sets a configuration value.
func (m *Manager) Set(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldValue := m.getValue(key)
	m.setValue(key, value)

	change := ConfigChange{
		Source:    "manager",
		Type:      ChangeTypeSet,
		Key:       key,
		OldValue:  oldValue,
		NewValue:  value,
		Timestamp: time.Now(),
	}
	m.notifyChangeCallbacks(change)
	m.notifyWatchCallbacks()
}

// =============================================================================
// BINDING METHODS
// =============================================================================

// Bind binds configuration to a struct.
func (m *Manager) Bind(key string, target any) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var data any
	if key == "" {
		data = m.data
	} else {
		data = m.getValue(key)
	}

	if data == nil {
		return ErrConfigError(fmt.Sprintf("no configuration found for key '%s'", key), nil)
	}

	return m.bindValue(data, target)
}

// BindWithDefault binds with a default value.
func (m *Manager) BindWithDefault(key string, target any, defaultValue any) error {
	return m.BindWithOptions(key, target, configcore.BindOptions{
		DefaultValue:   defaultValue,
		UseDefaults:    true,
		TagName:        "yaml",
		DeepMerge:      true,
		ErrorOnMissing: false,
	})
}

// BindWithOptions binds with flexible options.
func (m *Manager) BindWithOptions(key string, target any, options configcore.BindOptions) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var data any
	if key == "" {
		data = m.data
	} else {
		data = m.getValue(key)
	}

	// Convert struct defaultValue to map if needed (before checking if data is nil)
	if options.DefaultValue != nil {
		defaultVal := reflect.ValueOf(options.DefaultValue)
		if defaultVal.Kind() == reflect.Struct || (defaultVal.Kind() == reflect.Ptr && defaultVal.Elem().Kind() == reflect.Struct) {
			if converted, err := m.structToMap(options.DefaultValue, options.TagName); err == nil {
				// Replace DefaultValue with converted map for proper deep merge
				options.DefaultValue = converted
			} else {
				return ErrConfigError(fmt.Sprintf("failed to convert struct defaultValue: %v", err), nil)
			}
		}
	}

	if data == nil {
		if options.DefaultValue != nil {
			data = options.DefaultValue
		} else if options.UseDefaults {
			data = make(map[string]any)
		} else {
			if options.ErrorOnMissing {
				return ErrConfigError(fmt.Sprintf("no configuration found for key '%s'", key), nil)
			}

			data = make(map[string]any)
		}
	}

	return m.bindValueWithOptions(data, target, options)
}

// =============================================================================
// WATCH AND CHANGE CALLBACKS
// =============================================================================

// WatchWithCallback registers a callback for key changes.
func (m *Manager) WatchWithCallback(key string, callback func(string, any)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.watchCallbacks[key] == nil {
		m.watchCallbacks[key] = make([]func(string, any), 0)
	}

	m.watchCallbacks[key] = append(m.watchCallbacks[key], callback)
}

// WatchChanges registers a callback for all changes.
func (m *Manager) WatchChanges(callback func(ConfigChange)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.changeCallbacks = append(m.changeCallbacks, callback)
}

// =============================================================================
// METADATA AND INTROSPECTION
// =============================================================================

// GetSourceMetadata returns metadata for all sources.
func (m *Manager) GetSourceMetadata() map[string]*SourceMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.registry.GetAllMetadata()
}

// GetKeys returns all configuration keys.
func (m *Manager) GetKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getAllKeys(m.data, "")
}

// GetSection returns a configuration section.
func (m *Manager) GetSection(key string) map[string]any {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	if section, ok := value.(map[string]any); ok {
		return section
	}

	return nil
}

// HasKey checks if a key exists.
func (m *Manager) HasKey(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getValue(key) != nil
}

// IsSet checks if a key is set and not empty.
func (m *Manager) IsSet(key string) bool {
	value := m.Get(key)
	if value == nil {
		return false
	}

	switch v := value.(type) {
	case string:
		return v != ""
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// Size returns the number of keys.
func (m *Manager) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.getAllKeys(m.data, ""))
}

// =============================================================================
// STRUCTURE OPERATIONS
// =============================================================================

// Sub returns a sub-configuration manager.
func (m *Manager) Sub(key string) ConfigManager {
	subData := m.GetSection(key)
	if subData == nil {
		subData = make(map[string]any)
	}

	subManager := &Manager{
		data:            subData,
		watchCallbacks:  make(map[string][]func(string, any)),
		changeCallbacks: make([]func(ConfigChange), 0),
		logger:          m.logger,
		metrics:         m.metrics,
		errorHandler:    m.errorHandler,
	}

	subManager.registry = NewSourceRegistry(subManager.logger)
	subManager.validator = NewValidator(ValidatorConfig{
		Mode:         ValidationModePermissive,
		Logger:       subManager.logger,
		ErrorHandler: subManager.errorHandler,
	})

	return subManager
}

// MergeWith merges another config manager.
func (m *Manager) MergeWith(other ConfigManager) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if otherManager, ok := other.(*Manager); ok {
		otherManager.mu.RLock()
		defer otherManager.mu.RUnlock()

		m.mergeData(m.data, otherManager.data)

		return nil
	}

	return errors.New("merge not supported for this ConfigManager implementation")
}

// Clone creates a deep copy.
func (m *Manager) Clone() ConfigManager {
	m.mu.RLock()
	defer m.mu.RUnlock()

	clonedData := m.deepCopyMap(m.data)

	cloned := &Manager{
		data:            clonedData,
		watchCallbacks:  make(map[string][]func(string, any)),
		changeCallbacks: make([]func(ConfigChange), 0),
		logger:          m.logger,
		metrics:         m.metrics,
		errorHandler:    m.errorHandler,
	}

	cloned.registry = NewSourceRegistry(cloned.logger)
	cloned.validator = NewValidator(ValidatorConfig{
		Mode:         ValidationModePermissive,
		Logger:       cloned.logger,
		ErrorHandler: cloned.errorHandler,
	})

	return cloned
}

// GetAllSettings returns all settings.
func (m *Manager) GetAllSettings() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.deepCopyMap(m.data)
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Reset clears all configuration.
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string]any)
	m.watchCallbacks = make(map[string][]func(string, any))
	m.changeCallbacks = make([]func(ConfigChange), 0)

	if m.logger != nil {
		m.logger.Info("configuration manager reset")
	}

	if m.metrics != nil {
		m.metrics.Counter("config.reset").Inc()
		m.metrics.Gauge("config.keys_count").Set(0)
	}
}

// ExpandEnvVars expands environment variables.
func (m *Manager) ExpandEnvVars() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.expandEnvInMap(m.data)

	return nil
}

// SafeGet returns a value with type checking.
func (m *Manager) SafeGet(key string, expectedType reflect.Type) (any, error) {
	value := m.Get(key)
	if value == nil {
		return nil, fmt.Errorf("key '%s' not found", key)
	}

	valueType := reflect.TypeOf(value)
	if valueType != expectedType {
		return nil, fmt.Errorf("key '%s' expected type %v, got %v", key, expectedType, valueType)
	}

	return value, nil
}

// Stop stops the configuration manager.
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	if m.watchCancel != nil {
		m.watchCancel()
	}

	for _, source := range m.sources {
		if err := source.StopWatch(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to stop watching source",
					logger.String("source", source.Name()),
					logger.Error(err),
				)
			}
		}
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("configuration manager stopped")
	}

	if m.metrics != nil {
		m.metrics.Counter("config.watch_stopped").Inc()
	}

	return nil
}

// =============================================================================
// COMPATIBILITY ALIASES
// =============================================================================

// GetBytesSize is an alias for GetSizeInBytes.
func (m *Manager) GetBytesSize(key string, defaultValue ...uint64) uint64 {
	return m.GetSizeInBytes(key, defaultValue...)
}

// InConfig is an alias for HasKey.
func (m *Manager) InConfig(key string) bool {
	return m.HasKey(key)
}

// UnmarshalKey is an alias for Bind.
func (m *Manager) UnmarshalKey(key string, rawVal any) error {
	return m.Bind(key, rawVal)
}

// Unmarshal unmarshals entire configuration.
func (m *Manager) Unmarshal(rawVal any) error {
	return m.Bind("", rawVal)
}

// AllKeys is an alias for GetKeys.
func (m *Manager) AllKeys() []string {
	return m.GetKeys()
}

// AllSettings is an alias for GetAllSettings.
func (m *Manager) AllSettings() map[string]any {
	return m.GetAllSettings()
}

// ReadInConfig reads configuration.
func (m *Manager) ReadInConfig() error {
	return m.ReloadContext(context.Background())
}

// SetConfigType sets the configuration type.
func (m *Manager) SetConfigType(configType string) {
	// Placeholder for loader configuration
}

// SetConfigFile sets the configuration file.
func (m *Manager) SetConfigFile(filePath string) error {
	if m.logger != nil {
		m.logger.Info("configuration file path set",
			logger.String("file_path", filePath),
		)
	}

	return nil
}

// ConfigFileUsed returns the config file path.
func (m *Manager) ConfigFileUsed() string {
	sources := m.registry.GetSources()
	for _, source := range sources {
		if fileSource, ok := source.(interface {
			FilePath() string
		}); ok {
			return fileSource.FilePath()
		}
	}

	return ""
}

// WatchConfig is an alias for Watch.
func (m *Manager) WatchConfig() error {
	return m.Watch(context.Background())
}

// OnConfigChange is an alias for WatchChanges.
func (m *Manager) OnConfigChange(callback func(ConfigChange)) {
	m.WatchChanges(callback)
}

// =============================================================================
// INTERNAL HELPER METHODS
// =============================================================================

func (m *Manager) loadAllSources(ctx context.Context) error {
	mergedData := make(map[string]any)

	sources := m.registry.GetSources()

	// Sort sources by priority (lower number = lower priority, loaded first)
	// This ensures higher priority sources override lower priority ones
	type prioritySource struct {
		priority int
		source   ConfigSource
	}

	prioritySources := make([]prioritySource, 0, len(sources))
	for _, source := range sources {
		prioritySources = append(prioritySources, prioritySource{
			priority: source.Priority(),
			source:   source,
		})
	}

	// Sort by priority (ascending)
	for i := 0; i < len(prioritySources); i++ {
		for j := i + 1; j < len(prioritySources); j++ {
			if prioritySources[i].priority > prioritySources[j].priority {
				prioritySources[i], prioritySources[j] = prioritySources[j], prioritySources[i]
			}
		}
	}

	// Load sources in priority order (lower priority first, so higher priority can override)
	for _, ps := range prioritySources {
		data, err := m.loader.LoadSource(ctx, ps.source)
		if err != nil {
			if m.errorHandler != nil {
				// nolint:gosec // G104: Error handler intentionally discards return value
				m.errorHandler.HandleError(nil, err)
			}

			return ErrConfigError("failed to load source "+ps.source.Name(), err)
		}

		m.mergeData(mergedData, data)
	}

	m.data = mergedData

	return nil
}

func (m *Manager) handleConfigChange(source string, data map[string]any) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.logger != nil {
		m.logger.Info("configuration change detected",
			logger.String("source", source),
			logger.Int("keys", len(data)),
		)
	}

	oldData := make(map[string]any)
	maps.Copy(oldData, m.data)

	m.mergeData(m.data, data)

	if err := m.validator.ValidateAll(m.data); err != nil {
		if m.logger != nil {
			m.logger.Error("configuration validation failed after change",
				logger.String("source", source),
				logger.Error(err),
			)
		}

		if m.validator.IsStrictMode() {
			m.data = oldData

			return
		}
	}

	change := ConfigChange{
		Source:    source,
		Type:      ChangeTypeUpdate,
		Timestamp: time.Now(),
	}
	m.notifyChangeCallbacks(change)
	m.notifyWatchCallbacks()

	if m.metrics != nil {
		m.metrics.Counter("config.changes_applied").Inc()
	}
}

func (m *Manager) getValue(key string) any {
	keys := strings.Split(key, ".")
	current := any(m.data)

	for _, k := range keys {
		if current == nil {
			return nil
		}

		switch v := current.(type) {
		case map[string]any:
			current = v[k]
		case map[any]any:
			current = v[k]
		default:
			return nil
		}
	}

	return current
}

func (m *Manager) setValue(key string, value any) {
	keys := strings.Split(key, ".")
	current := m.data

	for i, k := range keys {
		if i == len(keys)-1 {
			current[k] = value
		} else {
			if current[k] == nil {
				current[k] = make(map[string]any)
			}

			if next, ok := current[k].(map[string]any); ok {
				current = next
			} else {
				current[k] = make(map[string]any)
				current = current[k].(map[string]any)
			}
		}
	}
}

func (m *Manager) mergeData(target, source map[string]any) {
	for key, value := range source {
		if existingValue, exists := target[key]; exists {
			if existingMap, ok := existingValue.(map[string]any); ok {
				if sourceMap, ok := value.(map[string]any); ok {
					m.mergeData(existingMap, sourceMap)

					continue
				}
			}
		}

		target[key] = value
	}
}

func (m *Manager) convertToString(value any) string {
	if value == nil {
		return ""
	}

	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (m *Manager) convertToInt(value any) (int64, error) {
	switch v := value.(type) {
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float32:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to int", value)
	}
}

func (m *Manager) convertToUint(value any) (uint64, error) {
	switch v := value.(type) {
	case uint:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case int:
		return uint64(v), nil
	case float64:
		return uint64(v), nil
	case string:
		return strconv.ParseUint(v, 10, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to uint", value)
	}
}

func (m *Manager) convertToFloat(value any) (float64, error) {
	switch v := value.(type) {
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("cannot convert %T to float", value)
	}
}

func (m *Manager) convertToBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		return strconv.ParseBool(v)
	case int:
		return v != 0, nil
	case float64:
		return v != 0, nil
	default:
		return false, fmt.Errorf("cannot convert %T to bool", value)
	}
}

func (m *Manager) parseSizeInBytes(s string) uint64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}

	units := map[string]uint64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
		"PB": 1024 * 1024 * 1024 * 1024 * 1024,
		"K":  1000,
		"M":  1000 * 1000,
		"G":  1000 * 1000 * 1000,
		"T":  1000 * 1000 * 1000 * 1000,
		"P":  1000 * 1000 * 1000 * 1000 * 1000,
	}

	for unit, multiplier := range units {
		if before, ok := strings.CutSuffix(s, unit); ok {
			numberStr := before
			if number, err := strconv.ParseFloat(numberStr, 64); err == nil {
				return uint64(number * float64(multiplier))
			}
		}
	}

	if number, err := strconv.ParseUint(s, 10, 64); err == nil {
		return number
	}

	return 0
}

// structToMap converts a struct to map[string]any using struct tags
// Supports yaml tags (preferred) and json tags as fallback, with optional custom tagName
func (m *Manager) structToMap(v any, tagName string) (map[string]any, error) {
	val := reflect.ValueOf(v)

	// Handle pointer to struct
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, fmt.Errorf("cannot convert nil pointer to map")
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("value must be a struct, got %s", val.Kind())
	}

	result := make(map[string]any)
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		fieldVal := val.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get field name from tags (yaml takes precedence over json)
		fieldName := field.Name

		// Try yaml tag first
		if yamlTag := field.Tag.Get("yaml"); yamlTag != "" {
			if idx := strings.Index(yamlTag, ","); idx != -1 {
				fieldName = yamlTag[:idx]
			} else {
				fieldName = yamlTag
			}
			if fieldName == "-" {
				continue
			}
		} else if jsonTag := field.Tag.Get("json"); jsonTag != "" {
			// Fallback to json tag
			if idx := strings.Index(jsonTag, ","); idx != -1 {
				fieldName = jsonTag[:idx]
			} else {
				fieldName = jsonTag
			}
			if fieldName == "-" {
				continue
			}
		}

		// If using custom tagName from options (not yaml/json), respect it
		if tagName != "" && tagName != "yaml" && tagName != "json" {
			if customTag := field.Tag.Get(tagName); customTag != "" {
				if idx := strings.Index(customTag, ","); idx != -1 {
					fieldName = customTag[:idx]
				} else {
					fieldName = customTag
				}
				if fieldName == "-" {
					continue
				}
			}
		}

		// Handle nested structs recursively
		if fieldVal.Kind() == reflect.Struct {
			nested, err := m.structToMap(fieldVal.Interface(), tagName)
			if err == nil {
				result[fieldName] = nested
				continue
			}
		}

		// Set the value
		result[fieldName] = fieldVal.Interface()
	}

	return result, nil
}

func (m *Manager) bindValue(value any, target any) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return ErrConfigError("target must be a pointer", nil)
	}

	targetElem := targetValue.Elem()
	sourceValue := reflect.ValueOf(value)

	// Handle simple type binding (string, int, etc.)
	if targetElem.Kind() != reflect.Struct {
		if sourceValue.Type().AssignableTo(targetElem.Type()) {
			targetElem.Set(sourceValue)

			return nil
		}
		// Try to convert if possible
		if sourceValue.Type().ConvertibleTo(targetElem.Type()) {
			targetElem.Set(sourceValue.Convert(targetElem.Type()))

			return nil
		}

		return ErrConfigError(fmt.Sprintf("cannot convert %s to %s", sourceValue.Type(), targetElem.Type()), nil)
	}

	// Handle struct binding
	if sourceValue.Kind() == reflect.Map {
		return m.bindMapToStruct(sourceValue, targetElem)
	}

	return ErrConfigError("unsupported value type for binding", nil)
}

func (m *Manager) bindMapToStruct(mapValue reflect.Value, structValue reflect.Value) error {
	structType := structValue.Type()

	for i := 0; i < structValue.NumField(); i++ {
		field := structValue.Field(i)
		fieldType := structType.Field(i)

		if !field.CanSet() {
			continue
		}

		fieldName := m.getFieldName(fieldType)
		if fieldName == "" {
			continue
		}

		mapKey := reflect.ValueOf(fieldName)
		mapVal := mapValue.MapIndex(mapKey)

		if !mapVal.IsValid() {
			continue
		}

		if err := m.setFieldValue(field, mapVal); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) getFieldName(field reflect.StructField) string {
	if tag := field.Tag.Get("yaml"); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}

	if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}

	if tag := field.Tag.Get("config"); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}

	return field.Name
}

func (m *Manager) setFieldValue(field reflect.Value, value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}

	valueInterface := value.Interface()

	switch field.Kind() {
	case reflect.String:
		field.SetString(m.convertToString(valueInterface))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if intVal, err := m.convertToInt(valueInterface); err == nil {
			field.SetInt(intVal)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if uintVal, err := m.convertToUint(valueInterface); err == nil {
			field.SetUint(uintVal)
		}
	case reflect.Float32, reflect.Float64:
		if floatVal, err := m.convertToFloat(valueInterface); err == nil {
			field.SetFloat(floatVal)
		}
	case reflect.Bool:
		if boolVal, err := m.convertToBool(valueInterface); err == nil {
			field.SetBool(boolVal)
		}
	case reflect.Slice:
		if slice, ok := valueInterface.([]any); ok {
			return m.setSliceValue(field, slice)
		}
	case reflect.Map:
		if mapVal, ok := valueInterface.(map[string]any); ok {
			return m.setMapValue(field, mapVal)
		}
	case reflect.Struct:
		if mapVal, ok := valueInterface.(map[string]any); ok {
			return m.bindMapToStruct(reflect.ValueOf(mapVal), field)
		}
	case reflect.Ptr:
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}

		return m.setFieldValue(field.Elem(), value)
	}

	return nil
}

func (m *Manager) setSliceValue(field reflect.Value, slice []any) error {
	sliceValue := reflect.MakeSlice(field.Type(), len(slice), len(slice))
	for i, item := range slice {
		if err := m.setFieldValue(sliceValue.Index(i), reflect.ValueOf(item)); err != nil {
			return err
		}
	}

	field.Set(sliceValue)

	return nil
}

func (m *Manager) setMapValue(field reflect.Value, mapData map[string]any) error {
	mapValue := reflect.MakeMap(field.Type())
	mapValueType := field.Type().Elem()

	for key, value := range mapData {
		keyValue := reflect.ValueOf(key)

		// Convert value to the correct type for the map's value type
		var convertedValue reflect.Value

		// Check if the map value type is a struct
		if mapValueType.Kind() == reflect.Struct {
			// Create a new instance of the struct type
			structInstance := reflect.New(mapValueType).Elem()

			// If the value is a map[string]interface{}, bind it to the struct
			if valueMap, ok := value.(map[string]any); ok {
				if err := m.bindMapToStruct(reflect.ValueOf(valueMap), structInstance); err != nil {
					return fmt.Errorf("failed to bind map value for key '%s': %w", key, err)
				}

				convertedValue = structInstance
			} else {
				// Direct assignment if types match
				convertedValue = reflect.ValueOf(value)
			}
		} else if mapValueType.Kind() == reflect.Ptr && mapValueType.Elem().Kind() == reflect.Struct {
			// Handle pointer to struct
			structInstance := reflect.New(mapValueType.Elem())

			if valueMap, ok := value.(map[string]any); ok {
				if err := m.bindMapToStruct(reflect.ValueOf(valueMap), structInstance.Elem()); err != nil {
					return fmt.Errorf("failed to bind map value for key '%s': %w", key, err)
				}

				convertedValue = structInstance
			} else {
				convertedValue = reflect.ValueOf(value)
			}
		} else {
			// For primitive types or interfaces, try direct conversion
			convertedValue = reflect.ValueOf(value)

			// If types don't match, try to convert
			if convertedValue.Type() != mapValueType {
				// Try type conversion if possible
				if convertedValue.Type().ConvertibleTo(mapValueType) {
					convertedValue = convertedValue.Convert(mapValueType)
				} else {
					// If it's still a map[string]interface{} and we need a different type,
					// we need to recursively bind it
					if _, ok := value.(map[string]any); ok {
						newValue := reflect.New(mapValueType).Elem()
						if err := m.setFieldValue(newValue, convertedValue); err != nil {
							return fmt.Errorf("failed to convert map value for key '%s': %w", key, err)
						}

						convertedValue = newValue
					}
				}
			}
		}

		mapValue.SetMapIndex(keyValue, convertedValue)
	}

	field.Set(mapValue)

	return nil
}

func (m *Manager) bindValueWithOptions(value any, target any, options configcore.BindOptions) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return ErrConfigError("target must be a pointer", nil)
	}

	targetElem := targetValue.Elem()

	// Handle primitive target types
	if targetElem.Kind() != reflect.Struct {
		sourceValue := reflect.ValueOf(value)
		if sourceValue.Type().AssignableTo(targetElem.Type()) {
			targetElem.Set(sourceValue)
			return nil
		}
		if sourceValue.Type().ConvertibleTo(targetElem.Type()) {
			targetElem.Set(sourceValue.Convert(targetElem.Type()))
			return nil
		}
		return ErrConfigError(fmt.Sprintf("cannot convert %s to %s", sourceValue.Type(), targetElem.Type()), nil)
	}

	// Handle struct target (existing logic continues...)
	targetStruct := targetElem

	// Apply struct tag defaults (lowest precedence)
	if err := m.applyStructDefaults(targetStruct); err != nil {
		return err
	}

	// Apply passed default value (medium precedence)
	if options.DefaultValue != nil {
		if defaultMap, ok := options.DefaultValue.(map[string]any); ok {
			if options.DeepMerge {
				value = m.deepMergeValues(defaultMap, value)
			}
		}
	}

	// Apply config file values (highest precedence)
	sourceValue := reflect.ValueOf(value)
	if sourceValue.Kind() == reflect.Map {
		return m.bindMapToStructWithOptions(sourceValue, targetStruct, options)
	}

	return ErrConfigError("unsupported value type for binding", nil)
}

func (m *Manager) bindMapToStructWithOptions(mapValue reflect.Value, structValue reflect.Value, options configcore.BindOptions) error {
	structType := structValue.Type()

	// Track required fields
	requiredFields := make(map[string]bool)
	for _, field := range options.Required {
		requiredFields[field] = false
	}

	for i := 0; i < structValue.NumField(); i++ {
		field := structValue.Field(i)
		fieldType := structType.Field(i)

		if !field.CanSet() {
			continue
		}

		// Get field name from tags
		fieldName := m.getFieldNameWithOptions(fieldType, options)
		if fieldName == "" {
			continue
		}

		// Mark required field as potentially found
		if _, isRequired := requiredFields[fieldName]; isRequired {
			requiredFields[fieldName] = true
		}

		// Get value from config map
		var mapVal reflect.Value
		if options.IgnoreCase {
			mapVal = m.findMapValueIgnoreCase(mapValue, fieldName)
		} else {
			mapKey := reflect.ValueOf(fieldName)
			mapVal = mapValue.MapIndex(mapKey)
		}
		// Handle missing values with proper precedence
		if !mapVal.IsValid() {
			// Check required fields
			if _, isRequired := requiredFields[fieldName]; isRequired {
				if options.ErrorOnMissing {
					return ErrConfigError(fmt.Sprintf("required field '%s' not found", fieldName), nil)
				}
			}

			// Field not in config, keep existing value (could be from struct tag default or passed default)
			if options.UseDefaults {
				continue
			}

			continue
		}

		// Set field value with deep merge support
		if err := m.setFieldValueWithDeepMerge(field, mapVal, fieldType, options); err != nil {
			return err
		}
	}

	// Validate all required fields were found
	for fieldName, found := range requiredFields {
		if !found && options.ErrorOnMissing {
			return ErrConfigError(fmt.Sprintf("required field '%s' not found in configuration", fieldName), nil)
		}
	}

	return nil
}

func (m *Manager) getFieldNameWithOptions(field reflect.StructField, options configcore.BindOptions) string {
	tagName := options.TagName
	if tagName == "" {
		tagName = "yaml"
	}

	if tag := field.Tag.Get(tagName); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}

	if tagName != "yaml" {
		if tag := field.Tag.Get("yaml"); tag != "" && tag != "-" {
			return strings.Split(tag, ",")[0]
		}
	}

	if tagName != "json" {
		if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
			return strings.Split(tag, ",")[0]
		}
	}

	return field.Name
}

func (m *Manager) findMapValueIgnoreCase(mapValue reflect.Value, fieldName string) reflect.Value {
	fieldNameLower := strings.ToLower(fieldName)

	for _, key := range mapValue.MapKeys() {
		if keyStr, ok := key.Interface().(string); ok {
			if strings.ToLower(keyStr) == fieldNameLower {
				return mapValue.MapIndex(key)
			}
		}
	}

	return reflect.Value{}
}

// deepMergeValues deeply merges two values with proper precedence
// configValue (from file) takes precedence over defaultValue.
func (m *Manager) deepMergeValues(defaultValue, configValue any) any {
	// If config value is nil, use default
	if configValue == nil {
		return defaultValue
	}

	// If default is nil, use config
	if defaultValue == nil {
		return configValue
	}

	// Both are maps - deep merge
	defaultMap, defaultIsMap := defaultValue.(map[string]any)
	configMap, configIsMap := configValue.(map[string]any)

	if defaultIsMap && configIsMap {
		merged := make(map[string]any)

		// Start with all default keys
		maps.Copy(merged, defaultMap)

		// Override/merge with config values
		for k, configVal := range configMap {
			if defaultVal, exists := merged[k]; exists {
				// Recursively merge nested maps
				merged[k] = m.deepMergeValues(defaultVal, configVal)
			} else {
				merged[k] = configVal
			}
		}

		return merged
	}

	// For non-map values, config takes precedence
	return configValue
}

// applyStructDefaults applies default values from struct tags.
func (m *Manager) applyStructDefaults(structValue reflect.Value) error {
	structType := structValue.Type()

	for i := 0; i < structValue.NumField(); i++ {
		field := structValue.Field(i)
		fieldType := structType.Field(i)

		if !field.CanSet() {
			continue
		}

		// Check for default tag
		defaultTag := fieldType.Tag.Get("default")
		if defaultTag == "" || defaultTag == "-" {
			// Recursively apply defaults to nested structs
			if field.Kind() == reflect.Struct {
				if err := m.applyStructDefaults(field); err != nil {
					return err
				}
			}

			continue
		}

		// Only apply default if field is zero value
		if !field.IsZero() {
			continue
		}
		// Parse and set default value based on field type
		if err := m.setDefaultValue(field, defaultTag, fieldType); err != nil {
			return ErrConfigError(
				fmt.Sprintf("failed to set default for field '%s'", fieldType.Name),
				err,
			)
		}
	}

	return nil
}

// setDefaultValue sets a field value from a default tag string.
func (m *Manager) setDefaultValue(field reflect.Value, defaultTag string, fieldType reflect.StructField) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(defaultTag)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// Check if it's a duration
		if field.Type() == reflect.TypeFor[time.Duration]() {
			if d, err := time.ParseDuration(defaultTag); err == nil {
				field.SetInt(int64(d))
			} else {
				return fmt.Errorf("invalid duration default: %s", defaultTag)
			}
		} else {
			if intVal, err := strconv.ParseInt(defaultTag, 10, 64); err == nil {
				field.SetInt(intVal)
			} else {
				return fmt.Errorf("invalid int default: %s", defaultTag)
			}
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if uintVal, err := strconv.ParseUint(defaultTag, 10, 64); err == nil {
			field.SetUint(uintVal)
		} else {
			return fmt.Errorf("invalid uint default: %s", defaultTag)
		}

	case reflect.Float32, reflect.Float64:
		if floatVal, err := strconv.ParseFloat(defaultTag, 64); err == nil {
			field.SetFloat(floatVal)
		} else {
			return fmt.Errorf("invalid float default: %s", defaultTag)
		}

	case reflect.Bool:
		if boolVal, err := strconv.ParseBool(defaultTag); err == nil {
			field.SetBool(boolVal)
		} else {
			return fmt.Errorf("invalid bool default: %s", defaultTag)
		}

	case reflect.Slice:
		// Handle slice defaults (comma-separated)
		if field.Type().Elem().Kind() == reflect.String {
			values := strings.Split(defaultTag, ",")

			slice := reflect.MakeSlice(field.Type(), len(values), len(values))
			for i, val := range values {
				slice.Index(i).SetString(strings.TrimSpace(val))
			}

			field.Set(slice)
		} else {
			return errors.New("slice defaults only supported for []string")
		}

	case reflect.Struct:
		// Handle time.Time
		if field.Type() == reflect.TypeFor[time.Time]() {
			formats := []string{
				time.RFC3339,
				time.RFC3339Nano,
				"2006-01-02 15:04:05",
				"2006-01-02T15:04:05",
				"2006-01-02",
			}
			for _, format := range formats {
				if t, err := time.Parse(format, defaultTag); err == nil {
					field.Set(reflect.ValueOf(t))

					return nil
				}
			}

			return fmt.Errorf("invalid time default: %s", defaultTag)
		}

	default:
		return fmt.Errorf("unsupported default type: %v", field.Kind())
	}

	return nil
}

func (m *Manager) setFieldValueWithOptions(field reflect.Value, value reflect.Value, options configcore.BindOptions) error {
	if !value.IsValid() {
		return nil
	}

	valueInterface := value.Interface()

	switch field.Kind() {
	case reflect.String:
		field.SetString(m.convertToString(valueInterface))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if intVal, err := m.convertToInt(valueInterface); err == nil {
			field.SetInt(intVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if uintVal, err := m.convertToUint(valueInterface); err == nil {
			field.SetUint(uintVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}
	case reflect.Float32, reflect.Float64:
		if floatVal, err := m.convertToFloat(valueInterface); err == nil {
			field.SetFloat(floatVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}
	case reflect.Bool:
		if boolVal, err := m.convertToBool(valueInterface); err == nil {
			field.SetBool(boolVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}
	case reflect.Slice:
		if slice, ok := valueInterface.([]any); ok {
			return m.setSliceValue(field, slice)
		}
	case reflect.Map:
		if mapVal, ok := valueInterface.(map[string]any); ok {
			return m.setMapValue(field, mapVal)
		}
	case reflect.Struct:
		if mapVal, ok := valueInterface.(map[string]any); ok {
			if options.DeepMerge && !field.IsZero() {
				return m.mergeStructValue(field, mapVal, options)
			}

			return m.bindMapToStructWithOptions(reflect.ValueOf(mapVal), field, options)
		}
	case reflect.Ptr:
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}

		return m.setFieldValueWithOptions(field.Elem(), value, options)
	}

	return nil
}

// setFieldValueWithDeepMerge sets field with deep merge support for nested structs.
func (m *Manager) setFieldValueWithDeepMerge(field reflect.Value, value reflect.Value, fieldType reflect.StructField, options configcore.BindOptions) error {
	if !value.IsValid() {
		return nil
	}

	valueInterface := value.Interface()

	switch field.Kind() {
	case reflect.String:
		field.SetString(m.convertToString(valueInterface))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if intVal, err := m.convertToInt(valueInterface); err == nil {
			field.SetInt(intVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if uintVal, err := m.convertToUint(valueInterface); err == nil {
			field.SetUint(uintVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}

	case reflect.Float32, reflect.Float64:
		if floatVal, err := m.convertToFloat(valueInterface); err == nil {
			field.SetFloat(floatVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}

	case reflect.Bool:
		if boolVal, err := m.convertToBool(valueInterface); err == nil {
			field.SetBool(boolVal)
		} else if !options.ErrorOnMissing {
			return nil
		} else {
			return err
		}

	case reflect.Slice:
		if slice, ok := valueInterface.([]any); ok {
			return m.setSliceValue(field, slice)
		}

	case reflect.Map:
		if mapVal, ok := valueInterface.(map[string]any); ok {
			if options.DeepMerge && !field.IsZero() {
				// Deep merge with existing map
				return m.mergeMapValue(field, mapVal, options)
			}

			return m.setMapValue(field, mapVal)
		}

	case reflect.Struct:
		if mapVal, ok := valueInterface.(map[string]any); ok {
			if options.DeepMerge && !field.IsZero() {
				// Deep merge with existing struct
				return m.mergeStructValue(field, mapVal, options)
			}

			return m.bindMapToStructWithOptions(reflect.ValueOf(mapVal), field, options)
		}

	case reflect.Ptr:
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}

		return m.setFieldValueWithDeepMerge(field.Elem(), value, fieldType, options)
	}

	return nil
}

// mergeMapValue deeply merges a map into an existing field.
func (m *Manager) mergeMapValue(field reflect.Value, newData map[string]any, options configcore.BindOptions) error {
	if field.IsNil() {
		return m.setMapValue(field, newData)
	}

	mapValueType := field.Type().Elem()

	// Create merged map
	merged := reflect.MakeMap(field.Type())

	// Copy existing values
	for _, key := range field.MapKeys() {
		merged.SetMapIndex(key, field.MapIndex(key))
	}

	// Merge new values
	for key, value := range newData {
		keyValue := reflect.ValueOf(key)

		// Convert value to correct type
		var convertedValue reflect.Value

		if mapValueType.Kind() == reflect.Struct {
			// Create new struct instance
			structInstance := reflect.New(mapValueType).Elem()

			if valueMap, ok := value.(map[string]any); ok {
				// Check if we should deep merge with existing
				if existingValue := field.MapIndex(keyValue); existingValue.IsValid() && options.DeepMerge {
					// Deep merge existing struct with new data
					if err := m.mergeStructValue(existingValue, valueMap, options); err != nil {
						return err
					}

					merged.SetMapIndex(keyValue, existingValue)

					continue
				} else {
					// Bind new struct
					if err := m.bindMapToStructWithOptions(reflect.ValueOf(valueMap), structInstance, options); err != nil {
						return fmt.Errorf("failed to bind map value for key '%v': %w", key, err)
					}

					convertedValue = structInstance
				}
			} else {
				convertedValue = reflect.ValueOf(value)
			}
		} else {
			convertedValue = reflect.ValueOf(value)

			// Convert if types don't match
			if convertedValue.Type() != mapValueType {
				if convertedValue.Type().ConvertibleTo(mapValueType) {
					convertedValue = convertedValue.Convert(mapValueType)
				}
			}
		}

		merged.SetMapIndex(keyValue, convertedValue)
	}

	field.Set(merged)

	return nil
}

func (m *Manager) mergeStructValue(structField reflect.Value, mapData map[string]any, options configcore.BindOptions) error {
	// Extract current struct values to map
	currentData := make(map[string]any)
	structType := structField.Type()

	for i := 0; i < structField.NumField(); i++ {
		field := structField.Field(i)
		fieldType := structType.Field(i)

		if !field.CanInterface() {
			continue
		}

		fieldName := m.getFieldNameWithOptions(fieldType, options)
		if fieldName != "" && !field.IsZero() {
			currentData[fieldName] = field.Interface()
		}
	}

	// Deep merge current with new data (new data takes precedence)
	mergedData := m.deepMergeValues(currentData, mapData)

	// Bind merged data back to struct
	if mergedMap, ok := mergedData.(map[string]any); ok {
		return m.bindMapToStructWithOptions(reflect.ValueOf(mergedMap), structField, options)
	}

	return nil
}

func (m *Manager) getAllKeys(data any, prefix string) []string {
	var keys []string

	if mapData, ok := data.(map[string]any); ok {
		for key, value := range mapData {
			fullKey := key
			if prefix != "" {
				fullKey = prefix + "." + key
			}

			keys = append(keys, fullKey)
			nestedKeys := m.getAllKeys(value, fullKey)
			keys = append(keys, nestedKeys...)
		}
	}

	return keys
}

func (m *Manager) deepCopyMap(original map[string]any) map[string]any {
	copy := make(map[string]any)

	for key, value := range original {
		switch v := value.(type) {
		case map[string]any:
			copy[key] = m.deepCopyMap(v)
		case []any:
			copy[key] = m.deepCopySlice(v)
		default:
			copy[key] = v
		}
	}

	return copy
}

func (m *Manager) deepCopySlice(original []any) []any {
	copy := make([]any, len(original))

	for i, value := range original {
		switch v := value.(type) {
		case map[string]any:
			copy[i] = m.deepCopyMap(v)
		case []any:
			copy[i] = m.deepCopySlice(v)
		default:
			copy[i] = v
		}
	}

	return copy
}

func (m *Manager) expandEnvInMap(data map[string]any) {
	for key, value := range data {
		switch v := value.(type) {
		case string:
			data[key] = m.expandEnvInString(v)
		case map[string]any:
			m.expandEnvInMap(v)
		case []any:
			m.expandEnvInSlice(v)
		}
	}
}

func (m *Manager) expandEnvInSlice(slice []any) {
	for i, value := range slice {
		switch v := value.(type) {
		case string:
			slice[i] = m.expandEnvInString(v)
		case map[string]any:
			m.expandEnvInMap(v)
		case []any:
			m.expandEnvInSlice(v)
		}
	}
}

func (m *Manager) expandEnvInString(s string) string {
	return os.Expand(s, os.Getenv)
}

func (m *Manager) notifyWatchCallbacks() {
	for key, callbacks := range m.watchCallbacks {
		value := m.getValue(key)
		for _, callback := range callbacks {
			go callback(key, value)
		}
	}
}

func (m *Manager) notifyChangeCallbacks(change ConfigChange) {
	for _, callback := range m.changeCallbacks {
		go callback(change)
	}
}

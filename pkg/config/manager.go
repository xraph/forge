package config

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	core2 "github.com/xraph/forge/pkg/config/formats"
	"github.com/xraph/forge/pkg/logger"
)

const ManagerKey = common.ConfigKey

// Manager implements an enhanced configuration manager that extends common.ConfigManager
type Manager struct {
	sources         []ConfigSource
	registry        SourceRegistry
	loader          *core2.Loader
	validator       *Validator
	watcher         *Watcher
	data            map[string]interface{}
	watchCallbacks  map[string][]func(string, interface{})
	changeCallbacks []func(ConfigChange)
	mu              sync.RWMutex
	watchCtx        context.Context
	watchCancel     context.CancelFunc
	started         bool
	logger          common.Logger
	metrics         common.Metrics
	errorHandler    common.ErrorHandler
	secretsManager  SecretsManager
}

// ManagerConfig contains configuration for the config manager
type ManagerConfig struct {
	DefaultSources  []SourceConfig      `yaml:"default_sources" json:"default_sources"`
	WatchInterval   time.Duration       `yaml:"watch_interval" json:"watch_interval"`
	ValidationMode  ValidationMode      `yaml:"validation_mode" json:"validation_mode"`
	SecretsEnabled  bool                `yaml:"secrets_enabled" json:"secrets_enabled"`
	CacheEnabled    bool                `yaml:"cache_enabled" json:"cache_enabled"`
	ReloadOnChange  bool                `yaml:"reload_on_change" json:"reload_on_change"`
	ErrorRetryCount int                 `yaml:"error_retry_count" json:"error_retry_count"`
	ErrorRetryDelay time.Duration       `yaml:"error_retry_delay" json:"error_retry_delay"`
	MetricsEnabled  bool                `yaml:"metrics_enabled" json:"metrics_enabled"`
	Logger          common.Logger       `yaml:"-" json:"-"`
	Metrics         common.Metrics      `yaml:"-" json:"-"`
	ErrorHandler    common.ErrorHandler `yaml:"-" json:"-"`
}

// ValidationMode defines how validation should be performed
type ValidationMode string

const (
	ValidationModeStrict     ValidationMode = "strict"     // Fail on validation errors
	ValidationModePermissive ValidationMode = "permissive" // Log validation errors but continue
	ValidationModeDisabled   ValidationMode = "disabled"   // No validation
)

// NewManager creates a new enhanced configuration manager
func NewManager(config ManagerConfig) common.ConfigManager {
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
		data:            make(map[string]interface{}),
		watchCallbacks:  make(map[string][]func(string, interface{})),
		changeCallbacks: make([]func(ConfigChange), 0),
		logger:          config.Logger,
		metrics:         config.Metrics,
		errorHandler:    config.ErrorHandler,
	}

	// Initialize components
	manager.registry = NewSourceRegistry(manager.logger)
	manager.loader = core2.NewLoader(core2.LoaderConfig{
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
	return common.ConfigKey
}

// LoadFrom loads configuration from multiple sources
func (m *Manager) LoadFrom(sources ...ConfigSource) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.logger != nil {
		m.logger.Info("loading configuration from sources",
			logger.Int("source_count", len(sources)),
		)
	}

	// Register sources
	for _, source := range sources {
		if err := m.registry.RegisterSource(source); err != nil {
			return common.ErrConfigError(fmt.Sprintf("failed to register source %s", source.Name()), err)
		}
		m.sources = append(m.sources, source)
	}

	// Load configuration from all sources
	if err := m.loadAllSources(context.Background()); err != nil {
		return err
	}

	// Validate configuration if enabled
	if err := m.validator.ValidateAll(m.data); err != nil {
		return common.ErrConfigError("configuration validation failed", err)
	}

	// Record metrics
	if m.metrics != nil {
		m.metrics.Counter("forge.config.sources_loaded").Add(float64(len(sources)))
		m.metrics.Gauge("forge.config.active_sources").Set(float64(len(m.sources)))
		m.metrics.Gauge("forge.config.keys_count").Set(float64(len(m.data)))
	}

	return nil
}

// Watch starts watching for configuration changes
func (m *Manager) Watch(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return common.ErrLifecycleError("watch", fmt.Errorf("configuration manager already watching"))
	}

	m.watchCtx, m.watchCancel = context.WithCancel(ctx)

	// OnStart watching all sources
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
		m.metrics.Counter("forge.config.watch_started").Inc()
	}

	return nil
}

// Reload forces a reload of all configuration sources
func (m *Manager) Reload() error {
	return m.ReloadContext(context.Background())
}

// ReloadContext forces a reload of all configuration sources with context
func (m *Manager) ReloadContext(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.logger != nil {
		m.logger.Info("reloading configuration from all sources")
	}

	startTime := time.Now()

	// Reload all sources
	if err := m.loadAllSources(ctx); err != nil {
		return err
	}

	// Validate configuration
	if err := m.validator.ValidateAll(m.data); err != nil {
		return common.ErrConfigError("configuration validation failed after reload", err)
	}

	// Notify all watch callbacks
	m.notifyWatchCallbacks()

	// Record metrics
	if m.metrics != nil {
		m.metrics.Counter("forge.config.reloads").Inc()
		m.metrics.Histogram("forge.config.reload_duration").Observe(time.Since(startTime).Seconds())
	}

	return nil
}

// Validate validates the current configuration
func (m *Manager) Validate() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.validator.ValidateAll(m.data)
}

// Get returns a configuration value
func (m *Manager) Get(key string) interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getValue(key)
}

// GetString returns a configuration value as string
func (m *Manager) GetString(key string) string {
	value := m.Get(key)
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

// GetInt returns a configuration value as integer
func (m *Manager) GetInt(key string) int {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetInt8 returns a configuration value as int8
func (m *Manager) GetInt8(key string) int8 {
	value := m.Get(key)
	if value == nil {
		return 0
	}

	switch v := value.(type) {
	case int8:
		return v
	case int:
		return int8(v)
	case int16:
		return int8(v)
	case int32:
		return int8(v)
	case int64:
		return int8(v)
	case uint8:
		return int8(v)
	case float32:
		return int8(v)
	case float64:
		return int8(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 8); err == nil {
			return int8(i)
		}
	}

	return 0
}

// GetInt16 returns a configuration value as int16
func (m *Manager) GetInt16(key string) int16 {
	value := m.Get(key)
	if value == nil {
		return 0
	}

	switch v := value.(type) {
	case int16:
		return v
	case int:
		return int16(v)
	case int8:
		return int16(v)
	case int32:
		return int16(v)
	case int64:
		return int16(v)
	case uint16:
		return int16(v)
	case float32:
		return int16(v)
	case float64:
		return int16(v)
	case string:
		if i, err := strconv.ParseInt(v, 10, 16); err == nil {
			return int16(i)
		}
	}

	return 0
}

// GetInt32 returns a configuration value as int32
func (m *Manager) GetInt32(key string) int32 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetInt64 returns a configuration value as int64
func (m *Manager) GetInt64(key string) int64 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetUint returns a configuration value as uint
func (m *Manager) GetUint(key string) uint {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetUint8 returns a configuration value as uint8
func (m *Manager) GetUint8(key string) uint8 {
	value := m.Get(key)
	if value == nil {
		return 0
	}

	switch v := value.(type) {
	case uint8:
		return v
	case uint:
		return uint8(v)
	case uint16:
		return uint8(v)
	case uint32:
		return uint8(v)
	case uint64:
		return uint8(v)
	case int:
		if v >= 0 {
			return uint8(v)
		}
	case float64:
		if v >= 0 {
			return uint8(v)
		}
	case string:
		if i, err := strconv.ParseUint(v, 10, 8); err == nil {
			return uint8(i)
		}
	}

	return 0
}

// GetUint16 returns a configuration value as uint16
func (m *Manager) GetUint16(key string) uint16 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetUint32 returns a configuration value as uint32
func (m *Manager) GetUint32(key string) uint32 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetUint64 returns a configuration value as uint64
func (m *Manager) GetUint64(key string) uint64 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetFloat32 returns a configuration value as float32
func (m *Manager) GetFloat32(key string) float32 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetFloat64 returns a configuration value as float64
func (m *Manager) GetFloat64(key string) float64 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetBool returns a configuration value as boolean
func (m *Manager) GetBool(key string) bool {
	value := m.Get(key)
	if value == nil {
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

	return false
}

// GetDuration returns a configuration value as duration
func (m *Manager) GetDuration(key string) time.Duration {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// GetTime returns a configuration value as time.Time
func (m *Manager) GetTime(key string) time.Time {
	value := m.Get(key)
	if value == nil {
		return time.Time{}
	}

	switch v := value.(type) {
	case time.Time:
		return v
	case string:
		// Try common time formats
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
		// Assume Unix timestamp
		return time.Unix(v, 0)
	case float64:
		// Assume Unix timestamp with fractional seconds
		return time.Unix(int64(v), int64((v-float64(int64(v)))*1e9))
	}

	return time.Time{}
}

// GetStringSlice returns a configuration value as []string
func (m *Manager) GetStringSlice(key string) []string {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []string:
		return v
	case []interface{}:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = fmt.Sprintf("%v", item)
		}
		return result
	case string:
		// Handle comma-separated values
		if strings.Contains(v, ",") {
			return strings.Split(v, ",")
		}
		return []string{v}
	}

	return nil
}

// GetIntSlice returns a configuration value as []int
func (m *Manager) GetIntSlice(key string) []int {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []int:
		return v
	case []interface{}:
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

	return nil
}

// GetInt64Slice returns a configuration value as []int64
func (m *Manager) GetInt64Slice(key string) []int64 {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []int64:
		return v
	case []interface{}:
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

	return nil
}

// GetFloat64Slice returns a configuration value as []float64
func (m *Manager) GetFloat64Slice(key string) []float64 {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []float64:
		return v
	case []interface{}:
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

	return nil
}

// GetBoolSlice returns a configuration value as []bool
func (m *Manager) GetBoolSlice(key string) []bool {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case []bool:
		return v
	case []interface{}:
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

	return nil
}

// GetStringMap returns a configuration value as map[string]string
func (m *Manager) GetStringMap(key string) map[string]string {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string]string:
		return v
	case map[string]interface{}:
		result := make(map[string]string)
		for k, val := range v {
			result[k] = fmt.Sprintf("%v", val)
		}
		return result
	case map[interface{}]interface{}:
		result := make(map[string]string)
		for k, val := range v {
			result[fmt.Sprintf("%v", k)] = fmt.Sprintf("%v", val)
		}
		return result
	}

	return nil
}

// GetStringMapString returns a configuration value as map[string]string (alias for GetStringMap)
func (m *Manager) GetStringMapString(key string) map[string]string {
	return m.GetStringMap(key)
}

// GetStringMapStringSlice returns a configuration value as map[string][]string
func (m *Manager) GetStringMapStringSlice(key string) map[string][]string {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	switch v := value.(type) {
	case map[string][]string:
		return v
	case map[string]interface{}:
		result := make(map[string][]string)
		for k, val := range v {
			switch slice := val.(type) {
			case []string:
				result[k] = slice
			case []interface{}:
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

	return nil
}

// GetSizeInBytes returns a configuration value as bytes (supports units like KB, MB, GB)
func (m *Manager) GetSizeInBytes(key string) uint64 {
	value := m.Get(key)
	if value == nil {
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

	return 0
}

// parseSizeInBytes parses size strings like "10MB", "1GB", etc.
func (m *Manager) parseSizeInBytes(s string) uint64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}

	// Define unit multipliers
	units := map[string]uint64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
		"PB": 1024 * 1024 * 1024 * 1024 * 1024,
		// Also support SI units
		"K": 1000,
		"M": 1000 * 1000,
		"G": 1000 * 1000 * 1000,
		"T": 1000 * 1000 * 1000 * 1000,
		"P": 1000 * 1000 * 1000 * 1000 * 1000,
	}

	// Try to find a unit suffix
	for unit, multiplier := range units {
		if strings.HasSuffix(s, unit) {
			numberStr := strings.TrimSuffix(s, unit)
			if number, err := strconv.ParseFloat(numberStr, 64); err == nil {
				return uint64(number * float64(multiplier))
			}
		}
	}

	// No unit found, try to parse as plain number
	if number, err := strconv.ParseUint(s, 10, 64); err == nil {
		return number
	}

	return 0
}

// WithDefault methods for all types
func (m *Manager) GetWithDefault(key string, val any) interface{} {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return value
}

func (m *Manager) GetStringWithDefault(key string, val string) string {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetString(key)
}

func (m *Manager) GetIntWithDefault(key string, val int) int {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetInt(key)
}

func (m *Manager) GetInt8WithDefault(key string, val int8) int8 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetInt8(key)
}

func (m *Manager) GetInt16WithDefault(key string, val int16) int16 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetInt16(key)
}

func (m *Manager) GetInt32WithDefault(key string, val int32) int32 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetInt32(key)
}

func (m *Manager) GetInt64WithDefault(key string, val int64) int64 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetInt64(key)
}

func (m *Manager) GetUintWithDefault(key string, val uint) uint {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetUint(key)
}

func (m *Manager) GetUint8WithDefault(key string, val uint8) uint8 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetUint8(key)
}

func (m *Manager) GetUint16WithDefault(key string, val uint16) uint16 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetUint16(key)
}

func (m *Manager) GetUint32WithDefault(key string, val uint32) uint32 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetUint32(key)
}

func (m *Manager) GetUint64WithDefault(key string, val uint64) uint64 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetUint64(key)
}

func (m *Manager) GetFloat32WithDefault(key string, val float32) float32 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetFloat32(key)
}

func (m *Manager) GetFloat64WithDefault(key string, val float64) float64 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetFloat64(key)
}

func (m *Manager) GetBoolWithDefault(key string, val bool) bool {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetBool(key)
}

func (m *Manager) GetDurationWithDefault(key string, val time.Duration) time.Duration {
	value := m.Get(key)
	if value == nil {
		return val
	}
	return m.GetDuration(key)
}

func (m *Manager) GetTimeWithDefault(key string, val time.Time) time.Time {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetTime(key)
	if result.IsZero() {
		return val
	}
	return result
}

func (m *Manager) GetStringSliceWithDefault(key string, val []string) []string {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetStringSlice(key)
	if result == nil {
		return val
	}
	return result
}

func (m *Manager) GetIntSliceWithDefault(key string, val []int) []int {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetIntSlice(key)
	if result == nil {
		return val
	}
	return result
}

func (m *Manager) GetInt64SliceWithDefault(key string, val []int64) []int64 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetInt64Slice(key)
	if result == nil {
		return val
	}
	return result
}

func (m *Manager) GetFloat64SliceWithDefault(key string, val []float64) []float64 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetFloat64Slice(key)
	if result == nil {
		return val
	}
	return result
}

func (m *Manager) GetBoolSliceWithDefault(key string, val []bool) []bool {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetBoolSlice(key)
	if result == nil {
		return val
	}
	return result
}

func (m *Manager) GetStringMapWithDefault(key string, val map[string]string) map[string]string {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetStringMap(key)
	if result == nil {
		return val
	}
	return result
}

func (m *Manager) GetStringMapStringSliceWithDefault(key string, val map[string][]string) map[string][]string {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetStringMapStringSlice(key)
	if result == nil {
		return val
	}
	return result
}

func (m *Manager) GetSizeInBytesWithDefault(key string, val uint64) uint64 {
	value := m.Get(key)
	if value == nil {
		return val
	}
	result := m.GetSizeInBytes(key)
	if result == 0 {
		return val
	}
	return result
}

// Set sets a configuration value
func (m *Manager) Set(key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	oldValue := m.getValue(key)
	m.setValue(key, value)

	// Notify change callbacks
	change := ConfigChange{
		Source:    "manager",
		Type:      ChangeTypeSet,
		Key:       key,
		OldValue:  oldValue,
		NewValue:  value,
		Timestamp: time.Now(),
	}
	m.notifyChangeCallbacks(change)
}

// Bind binds configuration to a struct
func (m *Manager) Bind(key string, target interface{}) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var data interface{}
	if key == "" {
		data = m.data
	} else {
		data = m.getValue(key)
	}

	if data == nil {
		return common.ErrConfigError(fmt.Sprintf("no configuration found for key '%s'", key), nil)
	}

	return m.bindValue(data, target)
}

// BindWithDefault binds configuration to a struct with a default value
func (m *Manager) BindWithDefault(key string, target interface{}, defaultValue interface{}) error {
	return m.BindWithOptions(key, target, common.BindOptions{
		DefaultValue:   defaultValue,
		UseDefaults:    true,
		TagName:        "yaml",
		DeepMerge:      true,
		ErrorOnMissing: false,
	})
}

// BindWithOptions binds configuration to a struct with flexible options
func (m *Manager) BindWithOptions(key string, target interface{}, options common.BindOptions) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var data interface{}
	if key == "" {
		data = m.data
	} else {
		data = m.getValue(key)
	}

	// Use default value if data is nil and default is provided
	if data == nil {
		if options.DefaultValue != nil {
			data = options.DefaultValue
		} else if options.UseDefaults {
			// If no data found and UseDefaults is true, bind with empty data
			// This allows struct default values to be used
			data = make(map[string]interface{})
		} else {
			if options.ErrorOnMissing {
				return common.ErrConfigError(fmt.Sprintf("no configuration found for key '%s'", key), nil)
			}
			// Create empty map to allow partial binding
			data = make(map[string]interface{})
		}
	}

	return m.bindValueWithOptions(data, target, options)
}

// WatchWithCallback registers a callback for configuration changes
func (m *Manager) WatchWithCallback(key string, callback func(string, interface{})) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.watchCallbacks[key] == nil {
		m.watchCallbacks[key] = make([]func(string, interface{}), 0)
	}
	m.watchCallbacks[key] = append(m.watchCallbacks[key], callback)
}

// WatchChanges registers a callback for all configuration changes
func (m *Manager) WatchChanges(callback func(ConfigChange)) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.changeCallbacks = append(m.changeCallbacks, callback)
}

// GetSourceMetadata returns metadata for all configuration sources
func (m *Manager) GetSourceMetadata() map[string]*SourceMetadata {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.registry.GetAllMetadata()
}

// GetKeys returns all configuration keys
func (m *Manager) GetKeys() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getAllKeys(m.data, "")
}

// GetSection returns a configuration section as a map
func (m *Manager) GetSection(key string) map[string]interface{} {
	value := m.Get(key)
	if value == nil {
		return nil
	}

	if section, ok := value.(map[string]interface{}); ok {
		return section
	}

	return nil
}

// HasKey checks if a configuration key exists
func (m *Manager) HasKey(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.getValue(key) != nil
}

// IsSet checks if a configuration key is set (not nil and not empty)
func (m *Manager) IsSet(key string) bool {
	value := m.Get(key)
	if value == nil {
		return false
	}

	switch v := value.(type) {
	case string:
		return v != ""
	case []interface{}:
		return len(v) > 0
	case map[string]interface{}:
		return len(v) > 0
	default:
		return true
	}
}

// Sub returns a new configuration manager for a subsection
func (m *Manager) Sub(key string) common.ConfigManager {
	subData := m.GetSection(key)
	if subData == nil {
		subData = make(map[string]interface{})
	}

	subManager := &Manager{
		data:            subData,
		watchCallbacks:  make(map[string][]func(string, interface{})),
		changeCallbacks: make([]func(ConfigChange), 0),
		logger:          m.logger,
		metrics:         m.metrics,
		errorHandler:    m.errorHandler,
	}

	// Initialize components for sub-manager
	subManager.registry = NewSourceRegistry(subManager.logger)
	subManager.validator = NewValidator(ValidatorConfig{
		Mode:         ValidationModePermissive, // Sub-configs are usually more permissive
		Logger:       subManager.logger,
		ErrorHandler: subManager.errorHandler,
	})

	return subManager
}

// MergeWith merges another configuration manager's data into this one
func (m *Manager) MergeWith(other common.ConfigManager) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If other is also a Manager, we can access its data directly
	if otherManager, ok := other.(*Manager); ok {
		otherManager.mu.RLock()
		defer otherManager.mu.RUnlock()
		m.mergeData(m.data, otherManager.data)
	} else {
		// For other implementations, we need to get all keys
		return fmt.Errorf("merge not supported for this ConfigManager implementation")
	}

	return nil
}

// Clone creates a deep copy of the configuration manager
func (m *Manager) Clone() common.ConfigManager {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Deep copy the data
	clonedData := m.deepCopyMap(m.data)

	cloned := &Manager{
		data:            clonedData,
		watchCallbacks:  make(map[string][]func(string, interface{})),
		changeCallbacks: make([]func(ConfigChange), 0),
		logger:          m.logger,
		metrics:         m.metrics,
		errorHandler:    m.errorHandler,
	}

	// Initialize components
	cloned.registry = NewSourceRegistry(cloned.logger)
	cloned.validator = NewValidator(ValidatorConfig{
		Mode:         ValidationModePermissive,
		Logger:       cloned.logger,
		ErrorHandler: cloned.errorHandler,
	})

	return cloned
}

// GetAllSettings returns all configuration as a map
func (m *Manager) GetAllSettings() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.deepCopyMap(m.data)
}

// Size returns the number of configuration keys
func (m *Manager) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.getAllKeys(m.data, ""))
}

// Utility and compatibility methods

// InConfig checks if a key path exists in configuration (alias for HasKey)
func (m *Manager) InConfig(key string) bool {
	return m.HasKey(key)
}

// UnmarshalKey unmarshals a configuration key to a struct (alias for Bind)
func (m *Manager) UnmarshalKey(key string, rawVal interface{}) error {
	return m.Bind(key, rawVal)
}

// Unmarshal unmarshals the entire configuration to a struct
func (m *Manager) Unmarshal(rawVal interface{}) error {
	return m.Bind("", rawVal)
}

// SafeGet returns a value with type checking and error
func (m *Manager) SafeGet(key string, expectedType reflect.Type) (interface{}, error) {
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

// GetBytesSize is an alias for GetSizeInBytes
func (m *Manager) GetBytesSize(key string) uint64 {
	return m.GetSizeInBytes(key)
}

// GetBytesSizeWithDefault is an alias for GetSizeInBytesWithDefault
func (m *Manager) GetBytesSizeWithDefault(key string, defaultValue uint64) uint64 {
	return m.GetSizeInBytesWithDefault(key, defaultValue)
}

// ReadInConfig reads configuration from a file (if file source is available)
func (m *Manager) ReadInConfig() error {
	return m.ReloadContext(context.Background())
}

// SetConfigType sets the type of configuration file (used by loaders)
func (m *Manager) SetConfigType(configType string) {
	if m.loader != nil {
		// m.loader.SetDefaultFormat(configType)
	}
}

// SetConfigFile sets the configuration file path (would be used with file sources)
func (m *Manager) SetConfigFile(filePath string) error {
	if m.logger != nil {
		m.logger.Info("configuration file path set",
			logger.String("file_path", filePath),
		)
	}
	return nil
}

// AllKeys returns all configuration keys
func (m *Manager) AllKeys() []string {
	return m.GetKeys()
}

// AllSettings returns all configuration settings
func (m *Manager) AllSettings() map[string]interface{} {
	return m.GetAllSettings()
}

// ConfigFileUsed returns the path to the configuration file being used
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

// WatchConfig starts watching for configuration file changes
func (m *Manager) WatchConfig() error {
	return m.Watch(context.Background())
}

// OnConfigChange registers a callback for configuration changes (alias for WatchChanges)
func (m *Manager) OnConfigChange(callback func(ConfigChange)) {
	m.WatchChanges(callback)
}

// Reset clears all configuration data
func (m *Manager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string]interface{})
	m.watchCallbacks = make(map[string][]func(string, interface{}))
	m.changeCallbacks = make([]func(ConfigChange), 0)

	if m.logger != nil {
		m.logger.Info("configuration manager reset")
	}

	if m.metrics != nil {
		m.metrics.Counter("forge.config.reset").Inc()
		m.metrics.Gauge("forge.config.keys_count").Set(0)
	}
}

// ExpandEnvVars expands environment variables in string values
func (m *Manager) ExpandEnvVars() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.expandEnvInMap(m.data)
	return nil
}

// Stop stops the configuration manager
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	if m.watchCancel != nil {
		m.watchCancel()
	}

	// OnStop all sources
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
		m.metrics.Counter("forge.config.watch_stopped").Inc()
	}

	return nil
}

// Internal helper methods

func (m *Manager) loadAllSources(ctx context.Context) error {
	mergedData := make(map[string]interface{})

	// Load from sources in priority order
	sources := m.registry.GetSources()
	for _, source := range sources {
		data, err := m.loader.LoadSource(ctx, source)
		if err != nil {
			if m.errorHandler != nil {
				m.errorHandler.HandleError(nil, err)
			}
			return common.ErrConfigError(fmt.Sprintf("failed to load source %s", source.Name()), err)
		}

		// Merge data with priority
		m.mergeData(mergedData, data)
	}

	m.data = mergedData
	return nil
}

func (m *Manager) handleConfigChange(source string, data map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.logger != nil {
		m.logger.Info("configuration change detected",
			logger.String("source", source),
			logger.Int("keys", len(data)),
		)
	}

	// Update configuration data
	oldData := make(map[string]interface{})
	for k, v := range m.data {
		oldData[k] = v
	}

	m.mergeData(m.data, data)

	// Validate if enabled
	if err := m.validator.ValidateAll(m.data); err != nil {
		if m.logger != nil {
			m.logger.Error("configuration validation failed after change",
				logger.String("source", source),
				logger.Error(err),
			)
		}
		// Revert changes if validation fails in strict mode
		if m.validator.IsStrictMode() {
			m.data = oldData
			return
		}
	}

	// Notify callbacks
	change := ConfigChange{
		Source:    source,
		Type:      ChangeTypeUpdate,
		Timestamp: time.Now(),
	}
	m.notifyChangeCallbacks(change)
	m.notifyWatchCallbacks()

	if m.metrics != nil {
		m.metrics.Counter("forge.config.changes_applied").Inc()
	}
}

func (m *Manager) getValue(key string) interface{} {
	keys := strings.Split(key, ".")
	current := interface{}(m.data)

	for _, k := range keys {
		if current == nil {
			return nil
		}

		switch v := current.(type) {
		case map[string]interface{}:
			current = v[k]
		case map[interface{}]interface{}:
			current = v[k]
		default:
			return nil
		}
	}

	return current
}

func (m *Manager) setValue(key string, value interface{}) {
	keys := strings.Split(key, ".")
	current := m.data

	for i, k := range keys {
		if i == len(keys)-1 {
			current[k] = value
		} else {
			if current[k] == nil {
				current[k] = make(map[string]interface{})
			}
			if next, ok := current[k].(map[string]interface{}); ok {
				current = next
			} else {
				// Create new map if type mismatch
				current[k] = make(map[string]interface{})
				current = current[k].(map[string]interface{})
			}
		}
	}
}

func (m *Manager) mergeData(target, source map[string]interface{}) {
	for key, value := range source {
		if existingValue, exists := target[key]; exists {
			// If both are maps, merge recursively
			if existingMap, ok := existingValue.(map[string]interface{}); ok {
				if sourceMap, ok := value.(map[string]interface{}); ok {
					m.mergeData(existingMap, sourceMap)
					continue
				}
			}
		}
		target[key] = value
	}
}

func (m *Manager) bindValue(value interface{}, target interface{}) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return common.ErrConfigError("target must be a pointer to struct", nil)
	}

	targetStruct := targetValue.Elem()
	sourceValue := reflect.ValueOf(value)

	if sourceValue.Kind() == reflect.Map {
		return m.bindMapToStruct(sourceValue, targetStruct)
	}

	return common.ErrConfigError("unsupported value type for binding", nil)
}

func (m *Manager) bindMapToStruct(mapValue reflect.Value, structValue reflect.Value) error {
	structType := structValue.Type()

	for i := 0; i < structValue.NumField(); i++ {
		field := structValue.Field(i)
		fieldType := structType.Field(i)

		if !field.CanSet() {
			continue
		}

		// Get field name from tags or use field name
		fieldName := m.getFieldName(fieldType)
		if fieldName == "" {
			continue
		}

		// Get value from map
		mapKey := reflect.ValueOf(fieldName)
		mapVal := mapValue.MapIndex(mapKey)

		if !mapVal.IsValid() {
			continue
		}

		// Set field value
		if err := m.setFieldValue(field, mapVal); err != nil {
			return err
		}
	}

	return nil
}

func (m *Manager) getFieldName(field reflect.StructField) string {
	// Check yaml tag first
	if tag := field.Tag.Get("yaml"); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}
	// Check json tag
	if tag := field.Tag.Get("json"); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}
	// Check config tag
	if tag := field.Tag.Get("config"); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}
	// Use field name
	return field.Name
}

func (m *Manager) setFieldValue(field reflect.Value, value reflect.Value) error {
	if !value.IsValid() {
		return nil
	}

	valueInterface := value.Interface()

	// Handle different field types
	switch field.Kind() {
	case reflect.String:
		if str, ok := valueInterface.(string); ok {
			field.SetString(str)
		} else {
			field.SetString(fmt.Sprintf("%v", valueInterface))
		}
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
		if slice, ok := valueInterface.([]interface{}); ok {
			return m.setSliceValue(field, slice)
		}
	case reflect.Map:
		if mapVal, ok := valueInterface.(map[string]interface{}); ok {
			return m.setMapValue(field, mapVal)
		}
	case reflect.Struct:
		if mapVal, ok := valueInterface.(map[string]interface{}); ok {
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

// Helper methods for type conversion
func (m *Manager) convertToInt(value interface{}) (int64, error) {
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

func (m *Manager) convertToUint(value interface{}) (uint64, error) {
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

func (m *Manager) convertToFloat(value interface{}) (float64, error) {
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

func (m *Manager) convertToBool(value interface{}) (bool, error) {
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

func (m *Manager) setSliceValue(field reflect.Value, slice []interface{}) error {
	sliceValue := reflect.MakeSlice(field.Type(), len(slice), len(slice))
	for i, item := range slice {
		if err := m.setFieldValue(sliceValue.Index(i), reflect.ValueOf(item)); err != nil {
			return err
		}
	}
	field.Set(sliceValue)
	return nil
}

func (m *Manager) setMapValue(field reflect.Value, mapData map[string]interface{}) error {
	mapValue := reflect.MakeMap(field.Type())
	for key, value := range mapData {
		keyValue := reflect.ValueOf(key)
		valueValue := reflect.ValueOf(value)
		mapValue.SetMapIndex(keyValue, valueValue)
	}
	field.Set(mapValue)
	return nil
}

func (m *Manager) getAllKeys(data interface{}, prefix string) []string {
	var keys []string

	if mapData, ok := data.(map[string]interface{}); ok {
		for key, value := range mapData {
			fullKey := key
			if prefix != "" {
				fullKey = prefix + "." + key
			}

			keys = append(keys, fullKey)

			// Recursively get nested keys
			nestedKeys := m.getAllKeys(value, fullKey)
			keys = append(keys, nestedKeys...)
		}
	}

	return keys
}

func (m *Manager) deepCopyMap(original map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{})

	for key, value := range original {
		switch v := value.(type) {
		case map[string]interface{}:
			copy[key] = m.deepCopyMap(v)
		case []interface{}:
			copy[key] = m.deepCopySlice(v)
		default:
			copy[key] = v
		}
	}

	return copy
}

func (m *Manager) deepCopySlice(original []interface{}) []interface{} {
	copy := make([]interface{}, len(original))

	for i, value := range original {
		switch v := value.(type) {
		case map[string]interface{}:
			copy[i] = m.deepCopyMap(v)
		case []interface{}:
			copy[i] = m.deepCopySlice(v)
		default:
			copy[i] = v
		}
	}

	return copy
}

func (m *Manager) expandEnvInMap(data map[string]interface{}) {
	for key, value := range data {
		switch v := value.(type) {
		case string:
			data[key] = m.expandEnvInString(v)
		case map[string]interface{}:
			m.expandEnvInMap(v)
		case []interface{}:
			m.expandEnvInSlice(v)
		}
	}
}

func (m *Manager) expandEnvInSlice(slice []interface{}) {
	for i, value := range slice {
		switch v := value.(type) {
		case string:
			slice[i] = m.expandEnvInString(v)
		case map[string]interface{}:
			m.expandEnvInMap(v)
		case []interface{}:
			m.expandEnvInSlice(v)
		}
	}
}

func (m *Manager) expandEnvInString(s string) string {
	// Simple implementation - can be enhanced with more sophisticated expansion
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

// bindValueWithOptions binds a value to target with options
func (m *Manager) bindValueWithOptions(value interface{}, target interface{}, options common.BindOptions) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return common.ErrConfigError("target must be a pointer to struct", nil)
	}

	targetStruct := targetValue.Elem()
	sourceValue := reflect.ValueOf(value)

	if sourceValue.Kind() == reflect.Map {
		return m.bindMapToStructWithOptions(sourceValue, targetStruct, options)
	}

	return common.ErrConfigError("unsupported value type for binding", nil)
}

// bindMapToStructWithOptions binds a map to struct with options
func (m *Manager) bindMapToStructWithOptions(mapValue reflect.Value, structValue reflect.Value, options common.BindOptions) error {
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

		// Get field name from tags or use field name
		fieldName := m.getFieldNameWithOptions(fieldType, options)
		if fieldName == "" {
			continue
		}

		// Mark required field as found
		if _, isRequired := requiredFields[fieldName]; isRequired {
			requiredFields[fieldName] = true
		}

		// Get value from map with case sensitivity handling
		var mapVal reflect.Value
		if options.IgnoreCase {
			mapVal = m.findMapValueIgnoreCase(mapValue, fieldName)
		} else {
			mapKey := reflect.ValueOf(fieldName)
			mapVal = mapValue.MapIndex(mapKey)
		}

		if !mapVal.IsValid() {
			// Handle missing field
			if options.UseDefaults {
				// Skip - let struct keep its default value
				continue
			}
			if options.ErrorOnMissing {
				return common.ErrConfigError(fmt.Sprintf("required field '%s' not found in configuration", fieldName), nil)
			}
			continue
		}

		// Set field value with deep merge support
		if err := m.setFieldValueWithOptions(field, mapVal, options); err != nil {
			return err
		}
	}

	// Check if all required fields were found
	for fieldName, found := range requiredFields {
		if !found {
			return common.ErrConfigError(fmt.Sprintf("required field '%s' not found in configuration", fieldName), nil)
		}
	}

	return nil
}

// getFieldNameWithOptions gets field name considering options
func (m *Manager) getFieldNameWithOptions(field reflect.StructField, options common.BindOptions) string {
	tagName := options.TagName
	if tagName == "" {
		tagName = "yaml"
	}

	// Check specified tag first
	if tag := field.Tag.Get(tagName); tag != "" && tag != "-" {
		return strings.Split(tag, ",")[0]
	}

	// Fallback to other common tags
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
	if tagName != "config" {
		if tag := field.Tag.Get("config"); tag != "" && tag != "-" {
			return strings.Split(tag, ",")[0]
		}
	}

	// Use field name
	return field.Name
}

// findMapValueIgnoreCase finds a map value ignoring case
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

// setFieldValueWithOptions sets field value considering options
func (m *Manager) setFieldValueWithOptions(field reflect.Value, value reflect.Value, options common.BindOptions) error {
	if !value.IsValid() {
		return nil
	}

	valueInterface := value.Interface()

	// Handle different field types
	switch field.Kind() {
	case reflect.String:
		if str, ok := valueInterface.(string); ok {
			field.SetString(str)
		} else {
			field.SetString(fmt.Sprintf("%v", valueInterface))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if intVal, err := m.convertToInt(valueInterface); err == nil {
			field.SetInt(intVal)
		} else if !options.ErrorOnMissing {
			// Keep default value
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
		if slice, ok := valueInterface.([]interface{}); ok {
			return m.setSliceValueWithOptions(field, slice, options)
		}
	case reflect.Map:
		if mapVal, ok := valueInterface.(map[string]interface{}); ok {
			return m.setMapValueWithOptions(field, mapVal, options)
		}
	case reflect.Struct:
		if mapVal, ok := valueInterface.(map[string]interface{}); ok {
			if options.DeepMerge && !field.IsZero() {
				// For deep merge, we need to merge with existing struct values
				return m.mergeStructValue(field, mapVal, options)
			} else {
				return m.bindMapToStructWithOptions(reflect.ValueOf(mapVal), field, options)
			}
		}
	case reflect.Ptr:
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return m.setFieldValueWithOptions(field.Elem(), value, options)
	}

	return nil
}

// mergeStructValue merges map data with existing struct values
func (m *Manager) mergeStructValue(structField reflect.Value, mapData map[string]interface{}, options common.BindOptions) error {
	// Create a temporary map with current struct values
	currentData := make(map[string]interface{})

	// Extract current values from struct
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

	// Merge with new data
	mergedData := make(map[string]interface{})
	for k, v := range currentData {
		mergedData[k] = v
	}
	for k, v := range mapData {
		if existingValue, exists := mergedData[k]; exists {
			// Deep merge nested maps
			if existingMap, ok := existingValue.(map[string]interface{}); ok {
				if newMap, ok := v.(map[string]interface{}); ok {
					merged := make(map[string]interface{})
					for mk, mv := range existingMap {
						merged[mk] = mv
					}
					for mk, mv := range newMap {
						merged[mk] = mv
					}
					mergedData[k] = merged
					continue
				}
			}
		}
		mergedData[k] = v
	}

	// Bind merged data to struct
	return m.bindMapToStructWithOptions(reflect.ValueOf(mergedData), structField, options)
}

// setSliceValueWithOptions sets slice value with options
func (m *Manager) setSliceValueWithOptions(field reflect.Value, slice []interface{}, options common.BindOptions) error {
	sliceValue := reflect.MakeSlice(field.Type(), len(slice), len(slice))
	for i, item := range slice {
		if err := m.setFieldValueWithOptions(sliceValue.Index(i), reflect.ValueOf(item), options); err != nil {
			return err
		}
	}
	field.Set(sliceValue)
	return nil
}

// setMapValueWithOptions sets map value with options
func (m *Manager) setMapValueWithOptions(field reflect.Value, mapData map[string]interface{}, options common.BindOptions) error {
	mapValue := reflect.MakeMap(field.Type())
	for key, value := range mapData {
		keyValue := reflect.ValueOf(key)
		valueValue := reflect.ValueOf(value)
		mapValue.SetMapIndex(keyValue, valueValue)
	}
	field.Set(mapValue)
	return nil
}

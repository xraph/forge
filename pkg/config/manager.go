package config

import (
	"context"
	"fmt"
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
	// factory         ConfigSourceFactory
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
	// manager.factory = NewSourceFactory()

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

	// Start watching all sources
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
	case int32:
		return int(v)
	case int64:
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

	// Stop all sources
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

// loadAllSources loads configuration from all registered sources
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

// handleConfigChange handles configuration changes from sources
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

// getValue gets a value using dot notation (e.g., "database.host")
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

// setValue sets a value using dot notation
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

// mergeData merges source data into target with priority
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

// bindValue binds a value to a target struct using reflection
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

// bindMapToStruct binds a map to a struct
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

// getFieldName gets field name from struct tags
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

// setFieldValue sets a field value with type conversion
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

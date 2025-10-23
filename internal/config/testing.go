package config

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	configcore "github.com/xraph/forge/internal/config/core"
)

// TestConfigManager is a lightweight, in-memory implementation of ConfigManager
// optimized for testing scenarios. It provides realistic behavior without
// external dependencies like files, databases, or network services.
type TestConfigManager struct {
	data            map[string]interface{}
	watchCallbacks  map[string][]func(string, interface{})
	changeCallbacks []func(ConfigChange)
	mu              sync.RWMutex
	name            string
	secretsManager  SecretsManager
}

// NewTestConfigManager creates a new test configuration manager
func NewTestConfigManager() ConfigManager {
	return &TestConfigManager{
		data:            make(map[string]interface{}),
		watchCallbacks:  make(map[string][]func(string, interface{})),
		changeCallbacks: make([]func(ConfigChange), 0),
		name:            "test-config-manager",
		secretsManager:  NewMockSecretsManager(),
	}
}

// NewTestConfigManagerWithData creates a test config manager with initial data
func NewTestConfigManagerWithData(data map[string]interface{}) ConfigManager {
	manager := &TestConfigManager{
		data:            make(map[string]interface{}),
		watchCallbacks:  make(map[string][]func(string, interface{})),
		changeCallbacks: make([]func(ConfigChange), 0),
		name:            "test-config-manager",
		secretsManager:  NewMockSecretsManager(),
	}

	// Deep copy the provided data
	manager.data = manager.deepCopyMap(data)
	return manager
}

// TestConfigBuilder provides a fluent interface for building test configurations
type TestConfigBuilder struct {
	data map[string]interface{}
}

// NewTestConfigBuilder creates a new builder for test configurations
func NewTestConfigBuilder() *TestConfigBuilder {
	return &TestConfigBuilder{
		data: make(map[string]interface{}),
	}
}

// Set sets a key-value pair
func (b *TestConfigBuilder) Set(key string, value interface{}) *TestConfigBuilder {
	b.setValue(key, value)
	return b
}

// SetSection sets a configuration section
func (b *TestConfigBuilder) SetSection(key string, section map[string]interface{}) *TestConfigBuilder {
	b.setValue(key, section)
	return b
}

// SetDefaults sets multiple default values
func (b *TestConfigBuilder) SetDefaults(defaults map[string]interface{}) *TestConfigBuilder {
	for k, v := range defaults {
		b.setValue(k, v)
	}
	return b
}

// Build creates a TestConfigManager with the configured data
func (b *TestConfigBuilder) Build() ConfigManager {
	return NewTestConfigManagerWithData(b.data)
}

func (b *TestConfigBuilder) setValue(key string, value interface{}) {
	keys := strings.Split(key, ".")
	current := b.data

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
				current[k] = make(map[string]interface{})
				current = current[k].(map[string]interface{})
			}
		}
	}
}

// =============================================================================
// CORE INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the manager name
func (t *TestConfigManager) Name() string {
	return t.name
}

// SecretsManager returns the secrets manager
func (t *TestConfigManager) SecretsManager() SecretsManager {
	return t.secretsManager
}

// LoadFrom simulates loading from sources
func (t *TestConfigManager) LoadFrom(sources ...ConfigSource) error {
	// Test implementation doesn't need to load from external sources
	return nil
}

// Watch simulates watching for changes
func (t *TestConfigManager) Watch(ctx context.Context) error {
	go func() {
		<-ctx.Done()
	}()
	return nil
}

// Reload simulates reloading configuration
func (t *TestConfigManager) Reload() error {
	return t.ReloadContext(context.Background())
}

// ReloadContext simulates reload with context
func (t *TestConfigManager) ReloadContext(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.notifyWatchCallbacks()
	t.notifyChangeCallbacks(ConfigChange{
		Source:    "test",
		Type:      ChangeTypeReload,
		Timestamp: time.Now(),
	})
	return nil
}

// Validate always validates successfully in tests
func (t *TestConfigManager) Validate() error {
	return nil
}

// Stop stops the manager
func (t *TestConfigManager) Stop() error {
	return nil
}

// =============================================================================
// BASIC GETTERS WITH VARIADIC DEFAULTS
// =============================================================================

// Get returns a configuration value
func (t *TestConfigManager) Get(key string) interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.getValue(key)
}

// GetString returns a string value with optional default
func (t *TestConfigManager) GetString(key string, defaultValue ...string) string {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return fmt.Sprintf("%v", value)
}

// GetInt returns an int value with optional default
func (t *TestConfigManager) GetInt(key string, defaultValue ...int) int {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return t.convertToInt(value)
}

// GetInt8 returns an int8 value with optional default
func (t *TestConfigManager) GetInt8(key string, defaultValue ...int8) int8 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return int8(t.convertToInt(value))
}

// GetInt16 returns an int16 value with optional default
func (t *TestConfigManager) GetInt16(key string, defaultValue ...int16) int16 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return int16(t.convertToInt(value))
}

// GetInt32 returns an int32 value with optional default
func (t *TestConfigManager) GetInt32(key string, defaultValue ...int32) int32 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return int32(t.convertToInt(value))
}

// GetInt64 returns an int64 value with optional default
func (t *TestConfigManager) GetInt64(key string, defaultValue ...int64) int64 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return int64(t.convertToInt(value))
}

// GetUint returns a uint value with optional default
func (t *TestConfigManager) GetUint(key string, defaultValue ...uint) uint {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return uint(t.convertToInt(value))
}

// GetUint8 returns a uint8 value with optional default
func (t *TestConfigManager) GetUint8(key string, defaultValue ...uint8) uint8 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return uint8(t.convertToInt(value))
}

// GetUint16 returns a uint16 value with optional default
func (t *TestConfigManager) GetUint16(key string, defaultValue ...uint16) uint16 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return uint16(t.convertToInt(value))
}

// GetUint32 returns a uint32 value with optional default
func (t *TestConfigManager) GetUint32(key string, defaultValue ...uint32) uint32 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return uint32(t.convertToInt(value))
}

// GetUint64 returns a uint64 value with optional default
func (t *TestConfigManager) GetUint64(key string, defaultValue ...uint64) uint64 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return uint64(t.convertToInt(value))
}

// GetFloat32 returns a float32 value with optional default
func (t *TestConfigManager) GetFloat32(key string, defaultValue ...float32) float32 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return float32(t.convertToFloat64(value))
}

// GetFloat64 returns a float64 value with optional default
func (t *TestConfigManager) GetFloat64(key string, defaultValue ...float64) float64 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return 0
	}
	return t.convertToFloat64(value)
}

// GetBool returns a bool value with optional default
func (t *TestConfigManager) GetBool(key string, defaultValue ...bool) bool {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return false
	}
	return t.convertToBool(value)
}

// GetDuration returns a duration value with optional default
func (t *TestConfigManager) GetDuration(key string, defaultValue ...time.Duration) time.Duration {
	value := t.Get(key)
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

// GetTime returns a time value with optional default
func (t *TestConfigManager) GetTime(key string, defaultValue ...time.Time) time.Time {
	value := t.Get(key)
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
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	case int64:
		return time.Unix(v, 0)
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return time.Time{}
}

// GetSizeInBytes returns size in bytes with optional default
func (t *TestConfigManager) GetSizeInBytes(key string, defaultValue ...uint64) uint64 {
	value := t.Get(key)
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
	case string:
		return t.parseSizeInBytes(v)
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return 0
}

// =============================================================================
// COLLECTION GETTERS WITH VARIADIC DEFAULTS
// =============================================================================

// GetStringSlice returns a string slice with optional default
func (t *TestConfigManager) GetStringSlice(key string, defaultValue ...[]string) []string {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
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
		return strings.Split(v, ",")
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// GetIntSlice returns an int slice with optional default
func (t *TestConfigManager) GetIntSlice(key string, defaultValue ...[]int) []int {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return nil
	}

	switch v := value.(type) {
	case []int:
		return v
	case []interface{}:
		result := make([]int, 0, len(v))
		for _, item := range v {
			if i := t.convertToInt(item); i != 0 || fmt.Sprintf("%v", item) == "0" {
				result = append(result, i)
			}
		}
		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// GetInt64Slice returns an int64 slice with optional default
func (t *TestConfigManager) GetInt64Slice(key string, defaultValue ...[]int64) []int64 {
	ints := t.GetIntSlice(key)
	if ints == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return nil
	}
	result := make([]int64, len(ints))
	for i, v := range ints {
		result[i] = int64(v)
	}
	return result
}

// GetFloat64Slice returns a float64 slice with optional default
func (t *TestConfigManager) GetFloat64Slice(key string, defaultValue ...[]float64) []float64 {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return nil
	}

	switch v := value.(type) {
	case []float64:
		return v
	case []interface{}:
		result := make([]float64, 0, len(v))
		for _, item := range v {
			result = append(result, t.convertToFloat64(item))
		}
		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// GetBoolSlice returns a bool slice with optional default
func (t *TestConfigManager) GetBoolSlice(key string, defaultValue ...[]bool) []bool {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return nil
	}

	switch v := value.(type) {
	case []bool:
		return v
	case []interface{}:
		result := make([]bool, 0, len(v))
		for _, item := range v {
			result = append(result, t.convertToBool(item))
		}
		return result
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// GetStringMap returns a string map with optional default
func (t *TestConfigManager) GetStringMap(key string, defaultValue ...map[string]string) map[string]string {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
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
	}

	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return nil
}

// GetStringMapString is an alias for GetStringMap
func (t *TestConfigManager) GetStringMapString(key string, defaultValue ...map[string]string) map[string]string {
	return t.GetStringMap(key, defaultValue...)
}

// GetStringMapStringSlice returns a map of string slices with optional default
func (t *TestConfigManager) GetStringMapStringSlice(key string, defaultValue ...map[string][]string) map[string][]string {
	value := t.Get(key)
	if value == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return nil
	}

	switch v := value.(type) {
	case map[string][]string:
		return v
	case map[string]interface{}:
		result := make(map[string][]string)
		for k, val := range v {
			if slice, ok := val.([]interface{}); ok {
				strSlice := make([]string, len(slice))
				for i, item := range slice {
					strSlice[i] = fmt.Sprintf("%v", item)
				}
				result[k] = strSlice
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
// ADVANCED GETTERS WITH FUNCTIONAL OPTIONS
// =============================================================================

// GetWithOptions returns a value with advanced options
func (t *TestConfigManager) GetWithOptions(key string, opts ...configcore.GetOption) (interface{}, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := t.Get(key)

	if value == nil {
		if options.Required {
			return nil, ErrConfigError(fmt.Sprintf("required key '%s' not found", key), nil)
		}
		if options.OnMissing != nil {
			value = options.OnMissing(key)
		} else if options.Default != nil {
			return options.Default, nil
		}
		return nil, nil
	}

	if options.Transform != nil {
		value = options.Transform(value)
	}

	if options.Validator != nil {
		if err := options.Validator(value); err != nil {
			return nil, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return value, nil
}

// GetStringWithOptions returns a string with advanced options
func (t *TestConfigManager) GetStringWithOptions(key string, opts ...configcore.GetOption) (string, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := t.Get(key)

	if value == nil {
		if options.Required {
			return "", ErrConfigError(fmt.Sprintf("required key '%s' not found", key), nil)
		}
		if options.OnMissing != nil {
			value = options.OnMissing(key)
		} else if options.Default != nil {
			if defaultStr, ok := options.Default.(string); ok {
				return defaultStr, nil
			}
		}
		return "", nil
	}

	if options.Transform != nil {
		value = options.Transform(value)
	}

	result := fmt.Sprintf("%v", value)

	if !options.AllowEmpty && result == "" {
		if options.Required {
			return "", ErrConfigError(fmt.Sprintf("key '%s' is empty", key), nil)
		}
		if options.Default != nil {
			if defaultStr, ok := options.Default.(string); ok {
				return defaultStr, nil
			}
		}
	}

	if options.Validator != nil {
		if err := options.Validator(result); err != nil {
			return "", ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return result, nil
}

// GetIntWithOptions returns an int with advanced options
func (t *TestConfigManager) GetIntWithOptions(key string, opts ...configcore.GetOption) (int, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := t.Get(key)

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
		}
		return 0, nil
	}

	if options.Transform != nil {
		value = options.Transform(value)
	}

	result := t.convertToInt(value)

	if options.Validator != nil {
		if err := options.Validator(result); err != nil {
			return 0, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return result, nil
}

// GetBoolWithOptions returns a bool with advanced options
func (t *TestConfigManager) GetBoolWithOptions(key string, opts ...configcore.GetOption) (bool, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := t.Get(key)

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
		}
		return false, nil
	}

	if options.Transform != nil {
		value = options.Transform(value)
	}

	result := t.convertToBool(value)

	if options.Validator != nil {
		if err := options.Validator(result); err != nil {
			return false, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return result, nil
}

// GetDurationWithOptions returns a duration with advanced options
func (t *TestConfigManager) GetDurationWithOptions(key string, opts ...configcore.GetOption) (time.Duration, error) {
	options := &configcore.GetOptions{}
	for _, opt := range opts {
		opt(options)
	}

	value := t.Get(key)

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
		}
		return 0, nil
	}

	if options.Transform != nil {
		value = options.Transform(value)
	}

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
		result = time.Duration(t.convertToInt(v)) * time.Second
	default:
		if options.Default != nil {
			if defaultDur, ok := options.Default.(time.Duration); ok {
				result = defaultDur
			}
		}
	}

	if options.Validator != nil {
		if err := options.Validator(result); err != nil {
			return 0, ErrConfigError(fmt.Sprintf("validation failed for key '%s'", key), err)
		}
	}

	return result, nil
}

// =============================================================================
// CONFIGURATION MODIFICATION
// =============================================================================

// Set sets a configuration value
func (t *TestConfigManager) Set(key string, value interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldValue := t.getValue(key)
	t.setValue(key, value)

	change := ConfigChange{
		Source:    "test",
		Type:      ChangeTypeSet,
		Key:       key,
		OldValue:  oldValue,
		NewValue:  value,
		Timestamp: time.Now(),
	}
	t.notifyChangeCallbacks(change)
}

// =============================================================================
// BINDING METHODS
// =============================================================================

// Bind binds configuration to a struct (simplified for testing)
func (t *TestConfigManager) Bind(key string, target interface{}) error {
	return t.BindWithOptions(key, target, configcore.DefaultBindOptions())
}

// BindWithDefault binds with a default value
func (t *TestConfigManager) BindWithDefault(key string, target interface{}, defaultValue interface{}) error {
	options := configcore.DefaultBindOptions()
	options.DefaultValue = defaultValue
	return t.BindWithOptions(key, target, options)
}

// BindWithOptions binds with flexible options
func (t *TestConfigManager) BindWithOptions(key string, target interface{}, options configcore.BindOptions) error {
	var data interface{}
	if key == "" {
		data = t.data
	} else {
		data = t.getValue(key)
	}

	if data == nil && options.DefaultValue != nil {
		data = options.DefaultValue
	}

	if data == nil {
		if options.ErrorOnMissing {
			return fmt.Errorf("no configuration found for key '%s'", key)
		}
		return nil
	}

	return t.bindValue(data, target)
}

// =============================================================================
// WATCH METHODS
// =============================================================================

// WatchWithCallback registers a callback for key changes
func (t *TestConfigManager) WatchWithCallback(key string, callback func(string, interface{})) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.watchCallbacks[key] == nil {
		t.watchCallbacks[key] = make([]func(string, interface{}), 0)
	}
	t.watchCallbacks[key] = append(t.watchCallbacks[key], callback)
}

// WatchChanges registers a callback for all changes
func (t *TestConfigManager) WatchChanges(callback func(ConfigChange)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.changeCallbacks = append(t.changeCallbacks, callback)
}

// =============================================================================
// METADATA AND INTROSPECTION
// =============================================================================

// GetSourceMetadata returns metadata for all sources
func (t *TestConfigManager) GetSourceMetadata() map[string]*SourceMetadata {
	return map[string]*SourceMetadata{
		"test": {
			Name:         "test",
			Priority:     1,
			Type:         "memory",
			LastLoaded:   time.Now(),
			LastModified: time.Now(),
			IsWatching:   false,
			KeyCount:     len(t.GetKeys()),
			ErrorCount:   0,
		},
	}
}

// GetKeys returns all configuration keys
func (t *TestConfigManager) GetKeys() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.getAllKeys(t.data, "")
}

// GetSection returns a configuration section
func (t *TestConfigManager) GetSection(key string) map[string]interface{} {
	value := t.Get(key)
	if value == nil {
		return nil
	}

	if section, ok := value.(map[string]interface{}); ok {
		return section
	}
	return nil
}

// HasKey checks if a key exists
func (t *TestConfigManager) HasKey(key string) bool {
	return t.Get(key) != nil
}

// IsSet checks if a key is set and not empty
func (t *TestConfigManager) IsSet(key string) bool {
	value := t.Get(key)
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

// Size returns the number of keys
func (t *TestConfigManager) Size() int {
	return len(t.GetKeys())
}

// =============================================================================
// STRUCTURE OPERATIONS
// =============================================================================

// Sub returns a sub-configuration manager
func (t *TestConfigManager) Sub(key string) ConfigManager {
	subData := t.GetSection(key)
	if subData == nil {
		subData = make(map[string]interface{})
	}
	return NewTestConfigManagerWithData(subData)
}

// MergeWith merges another config manager
func (t *TestConfigManager) MergeWith(other ConfigManager) error {
	// Try to get all settings - if the implementation has GetAllSettings, use it
	// Otherwise, we can't merge (this is a limitation of the interface)
	var otherData map[string]interface{}
	if mgr, ok := other.(*Manager); ok {
		otherData = mgr.GetAllSettings()
	} else if testMgr, ok := other.(*TestConfigManager); ok {
		otherData = testMgr.GetAllSettings()
	} else {
		return fmt.Errorf("cannot merge: GetAllSettings not available")
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	t.mergeData(t.data, otherData)
	return nil
}

// Clone creates a deep copy
func (t *TestConfigManager) Clone() ConfigManager {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return NewTestConfigManagerWithData(t.data)
}

// GetAllSettings returns all settings
func (t *TestConfigManager) GetAllSettings() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.deepCopyMap(t.data)
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// Reset clears all configuration
func (t *TestConfigManager) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = make(map[string]interface{})
	t.watchCallbacks = make(map[string][]func(string, interface{}))
	t.changeCallbacks = make([]func(ConfigChange), 0)
}

// ExpandEnvVars expands environment variables (no-op for testing)
func (t *TestConfigManager) ExpandEnvVars() error {
	return nil
}

// SafeGet returns a value with type checking
func (t *TestConfigManager) SafeGet(key string, expectedType reflect.Type) (interface{}, error) {
	value := t.Get(key)
	if value == nil {
		return nil, fmt.Errorf("key '%s' not found", key)
	}

	valueType := reflect.TypeOf(value)
	if valueType != expectedType {
		return nil, fmt.Errorf("key '%s' expected type %v, got %v", key, expectedType, valueType)
	}

	return value, nil
}

// =============================================================================
// COMPATIBILITY ALIASES
// =============================================================================

// GetBytesSize is an alias for GetSizeInBytes
func (t *TestConfigManager) GetBytesSize(key string, defaultValue ...uint64) uint64 {
	return t.GetSizeInBytes(key, defaultValue...)
}

// InConfig is an alias for HasKey
func (t *TestConfigManager) InConfig(key string) bool {
	return t.HasKey(key)
}

// UnmarshalKey is an alias for Bind
func (t *TestConfigManager) UnmarshalKey(key string, rawVal interface{}) error {
	return t.Bind(key, rawVal)
}

// Unmarshal unmarshals entire configuration
func (t *TestConfigManager) Unmarshal(rawVal interface{}) error {
	return t.Bind("", rawVal)
}

// AllKeys is an alias for GetKeys
func (t *TestConfigManager) AllKeys() []string {
	return t.GetKeys()
}

// AllSettings is an alias for GetAllSettings
func (t *TestConfigManager) AllSettings() map[string]interface{} {
	return t.GetAllSettings()
}

// ReadInConfig reads configuration
func (t *TestConfigManager) ReadInConfig() error {
	return t.ReloadContext(context.Background())
}

// SetConfigType sets the configuration type (no-op for testing)
func (t *TestConfigManager) SetConfigType(configType string) {
	// No-op for testing
}

// SetConfigFile sets the configuration file (no-op for testing)
func (t *TestConfigManager) SetConfigFile(filePath string) error {
	return nil
}

// ConfigFileUsed returns the config file path
func (t *TestConfigManager) ConfigFileUsed() string {
	return "test://memory"
}

// WatchConfig is an alias for Watch
func (t *TestConfigManager) WatchConfig() error {
	return t.Watch(context.Background())
}

// OnConfigChange is an alias for WatchChanges
func (t *TestConfigManager) OnConfigChange(callback func(ConfigChange)) {
	t.WatchChanges(callback)
}

// =============================================================================
// HELPER METHODS
// =============================================================================

func (t *TestConfigManager) getValue(key string) interface{} {
	keys := strings.Split(key, ".")
	current := interface{}(t.data)

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

func (t *TestConfigManager) setValue(key string, value interface{}) {
	keys := strings.Split(key, ".")
	current := t.data

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
				current[k] = make(map[string]interface{})
				current = current[k].(map[string]interface{})
			}
		}
	}
}

func (t *TestConfigManager) convertToInt(value interface{}) int {
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
	case bool:
		if v {
			return 1
		}
		return 0
	}
	return 0
}

func (t *TestConfigManager) convertToFloat64(value interface{}) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 0
}

func (t *TestConfigManager) convertToBool(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		return v != ""
	case int:
		return v != 0
	case float64:
		return v != 0
	}
	return false
}

func (t *TestConfigManager) parseSizeInBytes(s string) uint64 {
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
	}

	for unit, multiplier := range units {
		if strings.HasSuffix(s, unit) {
			numberStr := strings.TrimSuffix(s, unit)
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

func (t *TestConfigManager) bindValue(value interface{}, target interface{}) error {
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr || targetValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("target must be a pointer to struct")
	}

	if mapData, ok := value.(map[string]interface{}); ok {
		return t.bindMapToStruct(mapData, targetValue.Elem())
	}

	if targetValue.Elem().CanSet() {
		sourceValue := reflect.ValueOf(value)
		if sourceValue.Type().ConvertibleTo(targetValue.Elem().Type()) {
			targetValue.Elem().Set(sourceValue.Convert(targetValue.Elem().Type()))
		}
	}

	return nil
}

func (t *TestConfigManager) bindMapToStruct(mapData map[string]interface{}, structValue reflect.Value) error {
	structType := structValue.Type()

	for i := 0; i < structValue.NumField(); i++ {
		field := structValue.Field(i)
		fieldType := structType.Field(i)

		if !field.CanSet() {
			continue
		}

		fieldName := t.getFieldName(fieldType)
		if fieldName == "" {
			continue
		}

		if mapValue, exists := mapData[fieldName]; exists {
			if err := t.setFieldValue(field, mapValue); err != nil {
				return err
			}
		}
	}

	return nil
}

func (t *TestConfigManager) getFieldName(field reflect.StructField) string {
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

func (t *TestConfigManager) setFieldValue(field reflect.Value, value interface{}) error {
	if value == nil {
		return nil
	}

	sourceValue := reflect.ValueOf(value)

	switch field.Kind() {
	case reflect.String:
		field.SetString(fmt.Sprintf("%v", value))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		field.SetInt(int64(t.convertToInt(value)))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		field.SetUint(uint64(t.convertToInt(value)))
	case reflect.Float32, reflect.Float64:
		field.SetFloat(t.convertToFloat64(value))
	case reflect.Bool:
		field.SetBool(t.convertToBool(value))
	case reflect.Slice:
		if slice, ok := value.([]interface{}); ok {
			return t.setSliceValue(field, slice)
		}
	case reflect.Map:
		if mapVal, ok := value.(map[string]interface{}); ok {
			return t.setMapValue(field, mapVal)
		}
	case reflect.Struct:
		if mapVal, ok := value.(map[string]interface{}); ok {
			return t.bindMapToStruct(mapVal, field)
		}
	case reflect.Ptr:
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
		return t.setFieldValue(field.Elem(), value)
	default:
		if sourceValue.Type().ConvertibleTo(field.Type()) {
			field.Set(sourceValue.Convert(field.Type()))
		}
	}

	return nil
}

func (t *TestConfigManager) setSliceValue(field reflect.Value, slice []interface{}) error {
	sliceValue := reflect.MakeSlice(field.Type(), len(slice), len(slice))
	for i, item := range slice {
		if err := t.setFieldValue(sliceValue.Index(i), item); err != nil {
			return err
		}
	}
	field.Set(sliceValue)
	return nil
}

func (t *TestConfigManager) setMapValue(field reflect.Value, mapData map[string]interface{}) error {
	mapValue := reflect.MakeMap(field.Type())
	for key, value := range mapData {
		keyValue := reflect.ValueOf(key)
		valueValue := reflect.ValueOf(value)
		mapValue.SetMapIndex(keyValue, valueValue)
	}
	field.Set(mapValue)
	return nil
}

func (t *TestConfigManager) getAllKeys(data interface{}, prefix string) []string {
	var keys []string

	if mapData, ok := data.(map[string]interface{}); ok {
		for key, value := range mapData {
			fullKey := key
			if prefix != "" {
				fullKey = prefix + "." + key
			}

			keys = append(keys, fullKey)
			nestedKeys := t.getAllKeys(value, fullKey)
			keys = append(keys, nestedKeys...)
		}
	}

	return keys
}

func (t *TestConfigManager) deepCopyMap(original map[string]interface{}) map[string]interface{} {
	copy := make(map[string]interface{})

	for key, value := range original {
		switch v := value.(type) {
		case map[string]interface{}:
			copy[key] = t.deepCopyMap(v)
		case []interface{}:
			copy[key] = t.deepCopySlice(v)
		default:
			copy[key] = v
		}
	}

	return copy
}

func (t *TestConfigManager) deepCopySlice(original []interface{}) []interface{} {
	copy := make([]interface{}, len(original))

	for i, value := range original {
		switch v := value.(type) {
		case map[string]interface{}:
			copy[i] = t.deepCopyMap(v)
		case []interface{}:
			copy[i] = t.deepCopySlice(v)
		default:
			copy[i] = v
		}
	}

	return copy
}

func (t *TestConfigManager) mergeData(target, source map[string]interface{}) {
	for key, value := range source {
		if existingValue, exists := target[key]; exists {
			if existingMap, ok := existingValue.(map[string]interface{}); ok {
				if sourceMap, ok := value.(map[string]interface{}); ok {
					t.mergeData(existingMap, sourceMap)
					continue
				}
			}
		}
		target[key] = value
	}
}

func (t *TestConfigManager) notifyWatchCallbacks() {
	for key, callbacks := range t.watchCallbacks {
		value := t.getValue(key)
		for _, callback := range callbacks {
			go callback(key, value)
		}
	}
}

func (t *TestConfigManager) notifyChangeCallbacks(change ConfigChange) {
	for _, callback := range t.changeCallbacks {
		go callback(change)
	}
}

// =============================================================================
// TEST HELPERS
// =============================================================================

// SimulateConfigChange simulates a configuration change for testing watchers
func (t *TestConfigManager) SimulateConfigChange(key string, newValue interface{}) {
	oldValue := t.Get(key)
	t.Set(key, newValue)

	change := ConfigChange{
		Source:    "test-simulation",
		Type:      ChangeTypeUpdate,
		Key:       key,
		OldValue:  oldValue,
		NewValue:  newValue,
		Timestamp: time.Now(),
	}

	go func() {
		for _, callback := range t.changeCallbacks {
			callback(change)
		}
	}()
}

// TestConfigAssertions provides assertion helpers for testing configuration
type TestConfigAssertions struct {
	manager *TestConfigManager
}

// NewTestConfigAssertions creates assertion helpers for the test config manager
func NewTestConfigAssertions(manager ConfigManager) *TestConfigAssertions {
	if testManager, ok := manager.(*TestConfigManager); ok {
		return &TestConfigAssertions{manager: testManager}
	}
	panic("provided manager is not a TestConfigManager")
}

// AssertKeyExists checks that a key exists
func (a *TestConfigAssertions) AssertKeyExists(key string) bool {
	return a.manager.HasKey(key)
}

// AssertKeyEquals checks that a key has the expected value
func (a *TestConfigAssertions) AssertKeyEquals(key string, expected interface{}) bool {
	actual := a.manager.Get(key)
	return reflect.DeepEqual(actual, expected)
}

// AssertStringEquals checks that a key has the expected string value
func (a *TestConfigAssertions) AssertStringEquals(key string, expected string) bool {
	actual := a.manager.GetString(key)
	return actual == expected
}

// AssertIntEquals checks that a key has the expected int value
func (a *TestConfigAssertions) AssertIntEquals(key string, expected int) bool {
	actual := a.manager.GetInt(key)
	return actual == expected
}

// =============================================================================
// MOCK SECRETS MANAGER
// =============================================================================

// MockSecretsManager is a comprehensive mock implementation of SecretsManager
// optimized for testing with call tracking, assertions, and behavior simulation
type MockSecretsManager struct {
	secrets   map[string]string
	providers map[string]SecretProvider
	started   bool
	mu        sync.RWMutex

	// Call tracking for test assertions
	getCalls     []SecretCall
	setCalls     []SecretCall
	deleteCalls  []SecretCall
	listCalls    []ListCall
	rotateCalls  []RotateCall
	refreshCalls []RefreshCall
	startCalls   []LifecycleCall
	stopCalls    []LifecycleCall
	healthCalls  []HealthCall

	// Behavior simulation
	getError          error
	setError          error
	deleteError       error
	listError         error
	rotateError       error
	refreshError      error
	startError        error
	stopError         error
	healthError       error
	latencySimulation time.Duration

	// Metrics
	totalCalls   int64
	errorCount   int64
	lastAccessed time.Time
}

// SecretCall represents a tracked secret operation call
type SecretCall struct {
	Key       string
	Value     string
	Timestamp time.Time
	Error     error
}

// ListCall represents a tracked list operation
type ListCall struct {
	Result    []string
	Timestamp time.Time
	Error     error
}

// RotateCall represents a tracked rotate operation
type RotateCall struct {
	Key       string
	OldValue  string
	NewValue  string
	Timestamp time.Time
	Error     error
}

// RefreshCall represents a tracked refresh operation
type RefreshCall struct {
	Timestamp time.Time
	Error     error
}

// LifecycleCall represents a tracked lifecycle operation
type LifecycleCall struct {
	Timestamp time.Time
	Error     error
}

// HealthCall represents a tracked health check
type HealthCall struct {
	Timestamp time.Time
	Error     error
	Healthy   bool
}

// NewMockSecretsManager creates a new mock secrets manager with default settings
func NewMockSecretsManager() *MockSecretsManager {
	return &MockSecretsManager{
		secrets:      make(map[string]string),
		providers:    make(map[string]SecretProvider),
		started:      false,
		getCalls:     make([]SecretCall, 0),
		setCalls:     make([]SecretCall, 0),
		deleteCalls:  make([]SecretCall, 0),
		listCalls:    make([]ListCall, 0),
		rotateCalls:  make([]RotateCall, 0),
		refreshCalls: make([]RefreshCall, 0),
		startCalls:   make([]LifecycleCall, 0),
		stopCalls:    make([]LifecycleCall, 0),
		healthCalls:  make([]HealthCall, 0),
	}
}

// NewMockSecretsManagerWithSecrets creates a mock with initial secrets
func NewMockSecretsManagerWithSecrets(secrets map[string]string) *MockSecretsManager {
	manager := NewMockSecretsManager()
	manager.secrets = make(map[string]string)
	for k, v := range secrets {
		manager.secrets[k] = v
	}
	return manager
}

// =============================================================================
// CORE INTERFACE IMPLEMENTATION
// =============================================================================

// GetSecret retrieves a secret by key
func (m *MockSecretsManager) GetSecret(ctx context.Context, key string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Simulate latency if configured
	if m.latencySimulation > 0 {
		time.Sleep(m.latencySimulation)
	}

	// Return configured error if set
	if m.getError != nil {
		m.errorCount++
		call := SecretCall{
			Key:       key,
			Timestamp: time.Now(),
			Error:     m.getError,
		}
		m.getCalls = append(m.getCalls, call)
		return "", m.getError
	}

	// Check if started
	if !m.started {
		err := fmt.Errorf("secrets manager not started")
		m.errorCount++
		call := SecretCall{
			Key:       key,
			Timestamp: time.Now(),
			Error:     err,
		}
		m.getCalls = append(m.getCalls, call)
		return "", err
	}

	// Get secret
	value, exists := m.secrets[key]
	if !exists {
		err := fmt.Errorf("secret '%s' not found", key)
		m.errorCount++
		call := SecretCall{
			Key:       key,
			Timestamp: time.Now(),
			Error:     err,
		}
		m.getCalls = append(m.getCalls, call)
		return "", err
	}

	// Track successful call
	call := SecretCall{
		Key:       key,
		Value:     value,
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.getCalls = append(m.getCalls, call)

	return value, nil
}

// SetSecret stores a secret
func (m *MockSecretsManager) SetSecret(ctx context.Context, key, value string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Simulate latency if configured
	if m.latencySimulation > 0 {
		time.Sleep(m.latencySimulation)
	}

	// Return configured error if set
	if m.setError != nil {
		m.errorCount++
		call := SecretCall{
			Key:       key,
			Value:     value,
			Timestamp: time.Now(),
			Error:     m.setError,
		}
		m.setCalls = append(m.setCalls, call)
		return m.setError
	}

	// Check if started
	if !m.started {
		err := fmt.Errorf("secrets manager not started")
		m.errorCount++
		call := SecretCall{
			Key:       key,
			Value:     value,
			Timestamp: time.Now(),
			Error:     err,
		}
		m.setCalls = append(m.setCalls, call)
		return err
	}

	// Store secret
	m.secrets[key] = value

	// Track successful call
	call := SecretCall{
		Key:       key,
		Value:     value,
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.setCalls = append(m.setCalls, call)

	return nil
}

// DeleteSecret removes a secret
func (m *MockSecretsManager) DeleteSecret(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Simulate latency if configured
	if m.latencySimulation > 0 {
		time.Sleep(m.latencySimulation)
	}

	// Return configured error if set
	if m.deleteError != nil {
		m.errorCount++
		call := SecretCall{
			Key:       key,
			Timestamp: time.Now(),
			Error:     m.deleteError,
		}
		m.deleteCalls = append(m.deleteCalls, call)
		return m.deleteError
	}

	// Check if started
	if !m.started {
		err := fmt.Errorf("secrets manager not started")
		m.errorCount++
		call := SecretCall{
			Key:       key,
			Timestamp: time.Now(),
			Error:     err,
		}
		m.deleteCalls = append(m.deleteCalls, call)
		return err
	}

	// Delete secret
	delete(m.secrets, key)

	// Track successful call
	call := SecretCall{
		Key:       key,
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.deleteCalls = append(m.deleteCalls, call)

	return nil
}

// ListSecrets returns all secret keys
func (m *MockSecretsManager) ListSecrets(ctx context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Simulate latency if configured
	if m.latencySimulation > 0 {
		time.Sleep(m.latencySimulation)
	}

	// Return configured error if set
	if m.listError != nil {
		m.errorCount++
		call := ListCall{
			Timestamp: time.Now(),
			Error:     m.listError,
		}
		m.listCalls = append(m.listCalls, call)
		return nil, m.listError
	}

	// Check if started
	if !m.started {
		err := fmt.Errorf("secrets manager not started")
		m.errorCount++
		call := ListCall{
			Timestamp: time.Now(),
			Error:     err,
		}
		m.listCalls = append(m.listCalls, call)
		return nil, err
	}

	// List secrets
	keys := make([]string, 0, len(m.secrets))
	for k := range m.secrets {
		keys = append(keys, k)
	}

	// Track successful call
	call := ListCall{
		Result:    keys,
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.listCalls = append(m.listCalls, call)

	return keys, nil
}

// RotateSecret rotates a secret with a new value
func (m *MockSecretsManager) RotateSecret(ctx context.Context, key, newValue string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Simulate latency if configured
	if m.latencySimulation > 0 {
		time.Sleep(m.latencySimulation)
	}

	// Return configured error if set
	if m.rotateError != nil {
		m.errorCount++
		call := RotateCall{
			Key:       key,
			NewValue:  newValue,
			Timestamp: time.Now(),
			Error:     m.rotateError,
		}
		m.rotateCalls = append(m.rotateCalls, call)
		return m.rotateError
	}

	// Check if started
	if !m.started {
		err := fmt.Errorf("secrets manager not started")
		m.errorCount++
		call := RotateCall{
			Key:       key,
			NewValue:  newValue,
			Timestamp: time.Now(),
			Error:     err,
		}
		m.rotateCalls = append(m.rotateCalls, call)
		return err
	}

	// Get old value
	oldValue, exists := m.secrets[key]
	if !exists {
		err := fmt.Errorf("secret '%s' not found", key)
		m.errorCount++
		call := RotateCall{
			Key:       key,
			NewValue:  newValue,
			Timestamp: time.Now(),
			Error:     err,
		}
		m.rotateCalls = append(m.rotateCalls, call)
		return err
	}

	// Rotate secret
	m.secrets[key] = newValue

	// Track successful call
	call := RotateCall{
		Key:       key,
		OldValue:  oldValue,
		NewValue:  newValue,
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.rotateCalls = append(m.rotateCalls, call)

	return nil
}

// RegisterProvider registers a secrets provider
func (m *MockSecretsManager) RegisterProvider(name string, provider SecretProvider) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.providers[name] = provider
	return nil
}

// GetProvider returns a secrets provider by name
func (m *MockSecretsManager) GetProvider(name string) (SecretProvider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	provider, exists := m.providers[name]
	if !exists {
		return nil, fmt.Errorf("provider '%s' not found", name)
	}

	return provider, nil
}

// RefreshSecrets refreshes all cached secrets
func (m *MockSecretsManager) RefreshSecrets(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Simulate latency if configured
	if m.latencySimulation > 0 {
		time.Sleep(m.latencySimulation)
	}

	// Return configured error if set
	if m.refreshError != nil {
		m.errorCount++
		call := RefreshCall{
			Timestamp: time.Now(),
			Error:     m.refreshError,
		}
		m.refreshCalls = append(m.refreshCalls, call)
		return m.refreshError
	}

	// Check if started
	if !m.started {
		err := fmt.Errorf("secrets manager not started")
		m.errorCount++
		call := RefreshCall{
			Timestamp: time.Now(),
			Error:     err,
		}
		m.refreshCalls = append(m.refreshCalls, call)
		return err
	}

	// Track successful call
	call := RefreshCall{
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.refreshCalls = append(m.refreshCalls, call)

	return nil
}

// Start starts the secrets manager
func (m *MockSecretsManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Return configured error if set
	if m.startError != nil {
		m.errorCount++
		call := LifecycleCall{
			Timestamp: time.Now(),
			Error:     m.startError,
		}
		m.startCalls = append(m.startCalls, call)
		return m.startError
	}

	if m.started {
		err := fmt.Errorf("secrets manager already started")
		call := LifecycleCall{
			Timestamp: time.Now(),
			Error:     err,
		}
		m.startCalls = append(m.startCalls, call)
		return err
	}

	m.started = true

	// Track successful call
	call := LifecycleCall{
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.startCalls = append(m.startCalls, call)

	return nil
}

// Stop stops the secrets manager
func (m *MockSecretsManager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Return configured error if set
	if m.stopError != nil {
		m.errorCount++
		call := LifecycleCall{
			Timestamp: time.Now(),
			Error:     m.stopError,
		}
		m.stopCalls = append(m.stopCalls, call)
		return m.stopError
	}

	if !m.started {
		// Not an error for stop to be called when already stopped
		call := LifecycleCall{
			Timestamp: time.Now(),
			Error:     nil,
		}
		m.stopCalls = append(m.stopCalls, call)
		return nil
	}

	m.started = false

	// Track successful call
	call := LifecycleCall{
		Timestamp: time.Now(),
		Error:     nil,
	}
	m.stopCalls = append(m.stopCalls, call)

	return nil
}

// HealthCheck performs a health check
func (m *MockSecretsManager) HealthCheck(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCalls++
	m.lastAccessed = time.Now()

	// Return configured error if set
	if m.healthError != nil {
		m.errorCount++
		call := HealthCall{
			Timestamp: time.Now(),
			Error:     m.healthError,
			Healthy:   false,
		}
		m.healthCalls = append(m.healthCalls, call)
		return m.healthError
	}

	if !m.started {
		err := fmt.Errorf("secrets manager not started")
		m.errorCount++
		call := HealthCall{
			Timestamp: time.Now(),
			Error:     err,
			Healthy:   false,
		}
		m.healthCalls = append(m.healthCalls, call)
		return err
	}

	// Track successful call
	call := HealthCall{
		Timestamp: time.Now(),
		Error:     nil,
		Healthy:   true,
	}
	m.healthCalls = append(m.healthCalls, call)

	return nil
}

// =============================================================================
// BEHAVIOR CONFIGURATION
// =============================================================================

// SetGetError configures GetSecret to return an error
func (m *MockSecretsManager) SetGetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getError = err
}

// SetSetError configures SetSecret to return an error
func (m *MockSecretsManager) SetSetError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setError = err
}

// SetDeleteError configures DeleteSecret to return an error
func (m *MockSecretsManager) SetDeleteError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteError = err
}

// SetListError configures ListSecrets to return an error
func (m *MockSecretsManager) SetListError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listError = err
}

// SetRotateError configures RotateSecret to return an error
func (m *MockSecretsManager) SetRotateError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rotateError = err
}

// SetRefreshError configures RefreshSecrets to return an error
func (m *MockSecretsManager) SetRefreshError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refreshError = err
}

// SetStartError configures Start to return an error
func (m *MockSecretsManager) SetStartError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startError = err
}

// SetStopError configures Stop to return an error
func (m *MockSecretsManager) SetStopError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopError = err
}

// SetHealthError configures HealthCheck to return an error
func (m *MockSecretsManager) SetHealthError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthError = err
}

// SetLatencySimulation configures artificial latency for all operations
func (m *MockSecretsManager) SetLatencySimulation(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencySimulation = latency
}

// ClearErrors clears all configured errors
func (m *MockSecretsManager) ClearErrors() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getError = nil
	m.setError = nil
	m.deleteError = nil
	m.listError = nil
	m.rotateError = nil
	m.refreshError = nil
	m.startError = nil
	m.stopError = nil
	m.healthError = nil
}

// =============================================================================
// CALL TRACKING & ASSERTIONS
// =============================================================================

// GetCallCount returns the number of GetSecret calls
func (m *MockSecretsManager) GetCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.getCalls)
}

// SetCallCount returns the number of SetSecret calls
func (m *MockSecretsManager) SetCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.setCalls)
}

// DeleteCallCount returns the number of DeleteSecret calls
func (m *MockSecretsManager) DeleteCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.deleteCalls)
}

// ListCallCount returns the number of ListSecrets calls
func (m *MockSecretsManager) ListCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.listCalls)
}

// RotateCallCount returns the number of RotateSecret calls
func (m *MockSecretsManager) RotateCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.rotateCalls)
}

// RefreshCallCount returns the number of RefreshSecrets calls
func (m *MockSecretsManager) RefreshCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.refreshCalls)
}

// StartCallCount returns the number of Start calls
func (m *MockSecretsManager) StartCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.startCalls)
}

// StopCallCount returns the number of Stop calls
func (m *MockSecretsManager) StopCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.stopCalls)
}

// HealthCallCount returns the number of HealthCheck calls
func (m *MockSecretsManager) HealthCallCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.healthCalls)
}

// TotalCallCount returns the total number of calls
func (m *MockSecretsManager) TotalCallCount() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalCalls
}

// ErrorCount returns the total number of errors
func (m *MockSecretsManager) ErrorCount() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.errorCount
}

// GetCalls returns all GetSecret calls
func (m *MockSecretsManager) GetCalls() []SecretCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]SecretCall, len(m.getCalls))
	copy(calls, m.getCalls)
	return calls
}

// SetCalls returns all SetSecret calls
func (m *MockSecretsManager) SetCalls() []SecretCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]SecretCall, len(m.setCalls))
	copy(calls, m.setCalls)
	return calls
}

// DeleteCalls returns all DeleteSecret calls
func (m *MockSecretsManager) DeleteCalls() []SecretCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]SecretCall, len(m.deleteCalls))
	copy(calls, m.deleteCalls)
	return calls
}

// ListCalls returns all ListSecrets calls
func (m *MockSecretsManager) ListCalls() []ListCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]ListCall, len(m.listCalls))
	copy(calls, m.listCalls)
	return calls
}

// RotateCalls returns all RotateSecret calls
func (m *MockSecretsManager) RotateCalls() []RotateCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]RotateCall, len(m.rotateCalls))
	copy(calls, m.rotateCalls)
	return calls
}

// RefreshCalls returns all RefreshSecrets calls
func (m *MockSecretsManager) RefreshCalls() []RefreshCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]RefreshCall, len(m.refreshCalls))
	copy(calls, m.refreshCalls)
	return calls
}

// StartCalls returns all Start calls
func (m *MockSecretsManager) StartCalls() []LifecycleCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]LifecycleCall, len(m.startCalls))
	copy(calls, m.startCalls)
	return calls
}

// StopCalls returns all Stop calls
func (m *MockSecretsManager) StopCalls() []LifecycleCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]LifecycleCall, len(m.stopCalls))
	copy(calls, m.stopCalls)
	return calls
}

// HealthCalls returns all HealthCheck calls
func (m *MockSecretsManager) HealthCalls() []HealthCall {
	m.mu.RLock()
	defer m.mu.RUnlock()
	calls := make([]HealthCall, len(m.healthCalls))
	copy(calls, m.healthCalls)
	return calls
}

// WasGetCalledWith checks if GetSecret was called with a specific key
func (m *MockSecretsManager) WasGetCalledWith(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, call := range m.getCalls {
		if call.Key == key {
			return true
		}
	}
	return false
}

// WasSetCalledWith checks if SetSecret was called with a specific key and value
func (m *MockSecretsManager) WasSetCalledWith(key, value string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, call := range m.setCalls {
		if call.Key == key && call.Value == value {
			return true
		}
	}
	return false
}

// WasDeleteCalledWith checks if DeleteSecret was called with a specific key
func (m *MockSecretsManager) WasDeleteCalledWith(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, call := range m.deleteCalls {
		if call.Key == key {
			return true
		}
	}
	return false
}

// WasRotateCalledWith checks if RotateSecret was called with a specific key
func (m *MockSecretsManager) WasRotateCalledWith(key string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, call := range m.rotateCalls {
		if call.Key == key {
			return true
		}
	}
	return false
}

// IsStarted returns whether the manager is started
func (m *MockSecretsManager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// SecretCount returns the number of secrets stored
func (m *MockSecretsManager) SecretCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.secrets)
}

// ProviderCount returns the number of registered providers
func (m *MockSecretsManager) ProviderCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.providers)
}

// LastAccessed returns the timestamp of the last operation
func (m *MockSecretsManager) LastAccessed() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastAccessed
}

// =============================================================================
// RESET & CLEANUP
// =============================================================================

// Reset resets the mock to its initial state
func (m *MockSecretsManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.secrets = make(map[string]string)
	m.providers = make(map[string]SecretProvider)
	m.started = false

	m.getCalls = make([]SecretCall, 0)
	m.setCalls = make([]SecretCall, 0)
	m.deleteCalls = make([]SecretCall, 0)
	m.listCalls = make([]ListCall, 0)
	m.rotateCalls = make([]RotateCall, 0)
	m.refreshCalls = make([]RefreshCall, 0)
	m.startCalls = make([]LifecycleCall, 0)
	m.stopCalls = make([]LifecycleCall, 0)
	m.healthCalls = make([]HealthCall, 0)

	m.getError = nil
	m.setError = nil
	m.deleteError = nil
	m.listError = nil
	m.rotateError = nil
	m.refreshError = nil
	m.startError = nil
	m.stopError = nil
	m.healthError = nil
	m.latencySimulation = 0

	m.totalCalls = 0
	m.errorCount = 0
	m.lastAccessed = time.Time{}
}

// ResetCallTracking resets only the call tracking (keeps secrets and state)
func (m *MockSecretsManager) ResetCallTracking() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.getCalls = make([]SecretCall, 0)
	m.setCalls = make([]SecretCall, 0)
	m.deleteCalls = make([]SecretCall, 0)
	m.listCalls = make([]ListCall, 0)
	m.rotateCalls = make([]RotateCall, 0)
	m.refreshCalls = make([]RefreshCall, 0)
	m.startCalls = make([]LifecycleCall, 0)
	m.stopCalls = make([]LifecycleCall, 0)
	m.healthCalls = make([]HealthCall, 0)

	m.totalCalls = 0
	m.errorCount = 0
}

// =============================================================================
// TEST HELPERS
// =============================================================================

// MockSecretsBuilder provides fluent interface for building test scenarios
type MockSecretsBuilder struct {
	manager *MockSecretsManager
}

// NewMockSecretsBuilder creates a new builder
func NewMockSecretsBuilder() *MockSecretsBuilder {
	return &MockSecretsBuilder{
		manager: NewMockSecretsManager(),
	}
}

// WithSecret adds a secret to the mock
func (b *MockSecretsBuilder) WithSecret(key, value string) *MockSecretsBuilder {
	b.manager.secrets[key] = value
	return b
}

// WithSecrets adds multiple secrets
func (b *MockSecretsBuilder) WithSecrets(secrets map[string]string) *MockSecretsBuilder {
	for k, v := range secrets {
		b.manager.secrets[k] = v
	}
	return b
}

// WithProvider adds a provider
func (b *MockSecretsBuilder) WithProvider(name string, provider SecretProvider) *MockSecretsBuilder {
	b.manager.providers[name] = provider
	return b
}

// WithGetError configures GetSecret error
func (b *MockSecretsBuilder) WithGetError(err error) *MockSecretsBuilder {
	b.manager.getError = err
	return b
}

// WithSetError configures SetSecret error
func (b *MockSecretsBuilder) WithSetError(err error) *MockSecretsBuilder {
	b.manager.setError = err
	return b
}

// WithStarted sets the started state
func (b *MockSecretsBuilder) WithStarted(started bool) *MockSecretsBuilder {
	b.manager.started = started
	return b
}

// WithLatency configures latency simulation
func (b *MockSecretsBuilder) WithLatency(latency time.Duration) *MockSecretsBuilder {
	b.manager.latencySimulation = latency
	return b
}

// Build returns the configured mock
func (b *MockSecretsBuilder) Build() *MockSecretsManager {
	return b.manager
}

// =============================================================================
// STATISTICS & DEBUGGING
// =============================================================================

// Stats contains statistics about the mock's usage
type MockStats struct {
	SecretCount   int
	ProviderCount int
	TotalCalls    int64
	ErrorCount    int64
	Started       bool
	LastAccessed  time.Time

	GetCalls     int
	SetCalls     int
	DeleteCalls  int
	ListCalls    int
	RotateCalls  int
	RefreshCalls int
	StartCalls   int
	StopCalls    int
	HealthCalls  int
}

// GetStats returns usage statistics
func (m *MockSecretsManager) GetStats() MockStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MockStats{
		SecretCount:   len(m.secrets),
		ProviderCount: len(m.providers),
		TotalCalls:    m.totalCalls,
		ErrorCount:    m.errorCount,
		Started:       m.started,
		LastAccessed:  m.lastAccessed,
		GetCalls:      len(m.getCalls),
		SetCalls:      len(m.setCalls),
		DeleteCalls:   len(m.deleteCalls),
		ListCalls:     len(m.listCalls),
		RotateCalls:   len(m.rotateCalls),
		RefreshCalls:  len(m.refreshCalls),
		StartCalls:    len(m.startCalls),
		StopCalls:     len(m.stopCalls),
		HealthCalls:   len(m.healthCalls),
	}
}

// PrintStats prints statistics for debugging
func (m *MockSecretsManager) PrintStats() {
	stats := m.GetStats()
	fmt.Printf("=== MockSecretsManager Statistics ===\n")
	fmt.Printf("Secrets:       %d\n", stats.SecretCount)
	fmt.Printf("Providers:     %d\n", stats.ProviderCount)
	fmt.Printf("Started:       %v\n", stats.Started)
	fmt.Printf("Total Calls:   %d\n", stats.TotalCalls)
	fmt.Printf("Errors:        %d\n", stats.ErrorCount)
	fmt.Printf("Get Calls:     %d\n", stats.GetCalls)
	fmt.Printf("Set Calls:     %d\n", stats.SetCalls)
	fmt.Printf("Delete Calls:  %d\n", stats.DeleteCalls)
	fmt.Printf("List Calls:    %d\n", stats.ListCalls)
	fmt.Printf("Rotate Calls:  %d\n", stats.RotateCalls)
	fmt.Printf("Refresh Calls: %d\n", stats.RefreshCalls)
	fmt.Printf("Start Calls:   %d\n", stats.StartCalls)
	fmt.Printf("Stop Calls:    %d\n", stats.StopCalls)
	fmt.Printf("Health Calls:  %d\n", stats.HealthCalls)
	if !stats.LastAccessed.IsZero() {
		fmt.Printf("Last Accessed: %s\n", stats.LastAccessed.Format(time.RFC3339))
	}
	fmt.Printf("=====================================\n")
}

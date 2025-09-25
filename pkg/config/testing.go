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
)

// TestConfigManager is a lightweight, in-memory implementation of ConfigManager
// optimized for testing scenarios. It provides realistic behavior without
// external dependencies like files, databases, or network services.
type TestConfigManager struct {
	data            map[string]interface{}
	watchCallbacks  map[string][]func(string, interface{})
	changeCallbacks []func(common.ConfigChange)
	mu              sync.RWMutex
	name            string
}

// NewTestConfigManager creates a new test configuration manager
func NewTestConfigManager() common.ConfigManager {
	return &TestConfigManager{
		data:            make(map[string]interface{}),
		watchCallbacks:  make(map[string][]func(string, interface{})),
		changeCallbacks: make([]func(common.ConfigChange), 0),
		name:            "test-config-manager",
	}
}

// NewTestConfigManagerWithData creates a test config manager with initial data
func NewTestConfigManagerWithData(data map[string]interface{}) common.ConfigManager {
	manager := &TestConfigManager{
		data:            make(map[string]interface{}),
		watchCallbacks:  make(map[string][]func(string, interface{})),
		changeCallbacks: make([]func(common.ConfigChange), 0),
		name:            "test-config-manager",
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
func (b *TestConfigBuilder) Build() common.ConfigManager {
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

// Core interface implementation
func (t *TestConfigManager) Name() string {
	return t.name
}

func (t *TestConfigManager) LoadFrom(sources ...common.ConfigSource) error {
	// Test implementation doesn't need to load from external sources
	// Instead, it can simulate loading by merging data
	return nil
}

func (t *TestConfigManager) Watch(ctx context.Context) error {
	// Test implementation can simulate watching
	go func() {
		<-ctx.Done()
		// Cleanup watch operations
	}()
	return nil
}

func (t *TestConfigManager) Reload() error {
	return t.ReloadContext(context.Background())
}

func (t *TestConfigManager) ReloadContext(ctx context.Context) error {
	// Simulate reload by notifying watchers
	t.mu.Lock()
	defer t.mu.Unlock()

	t.notifyWatchCallbacks()
	t.notifyChangeCallbacks(common.ConfigChange{
		Source:    "test",
		Type:      common.ChangeTypeReload,
		Timestamp: time.Now(),
	})
	return nil
}

func (t *TestConfigManager) Validate() error {
	// Test implementation always validates successfully
	return nil
}

func (t *TestConfigManager) Stop() error {
	return nil
}

// Basic getters
func (t *TestConfigManager) Get(key string) interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.getValue(key)
}

func (t *TestConfigManager) GetString(key string) string {
	value := t.Get(key)
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

func (t *TestConfigManager) GetInt(key string) int {
	value := t.Get(key)
	if value == nil {
		return 0
	}
	return t.convertToInt(value)
}

func (t *TestConfigManager) GetInt8(key string) int8 {
	return int8(t.GetInt(key))
}

func (t *TestConfigManager) GetInt16(key string) int16 {
	return int16(t.GetInt(key))
}

func (t *TestConfigManager) GetInt32(key string) int32 {
	return int32(t.GetInt(key))
}

func (t *TestConfigManager) GetInt64(key string) int64 {
	return int64(t.GetInt(key))
}

func (t *TestConfigManager) GetUint(key string) uint {
	return uint(t.GetInt(key))
}

func (t *TestConfigManager) GetUint8(key string) uint8 {
	return uint8(t.GetInt(key))
}

func (t *TestConfigManager) GetUint16(key string) uint16 {
	return uint16(t.GetInt(key))
}

func (t *TestConfigManager) GetUint32(key string) uint32 {
	return uint32(t.GetInt(key))
}

func (t *TestConfigManager) GetUint64(key string) uint64 {
	return uint64(t.GetInt(key))
}

func (t *TestConfigManager) GetFloat32(key string) float32 {
	return float32(t.GetFloat64(key))
}

func (t *TestConfigManager) GetFloat64(key string) float64 {
	value := t.Get(key)
	if value == nil {
		return 0
	}
	return t.convertToFloat64(value)
}

func (t *TestConfigManager) GetBool(key string) bool {
	value := t.Get(key)
	if value == nil {
		return false
	}
	return t.convertToBool(value)
}

func (t *TestConfigManager) GetDuration(key string) time.Duration {
	value := t.Get(key)
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

func (t *TestConfigManager) GetTime(key string) time.Time {
	value := t.Get(key)
	if value == nil {
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
	return time.Time{}
}

func (t *TestConfigManager) GetSizeInBytes(key string) uint64 {
	value := t.Get(key)
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
	case string:
		return t.parseSizeInBytes(v)
	}
	return 0
}

// Collection getters
func (t *TestConfigManager) GetStringSlice(key string) []string {
	value := t.Get(key)
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
		return strings.Split(v, ",")
	}
	return nil
}

func (t *TestConfigManager) GetIntSlice(key string) []int {
	value := t.Get(key)
	if value == nil {
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
	return nil
}

func (t *TestConfigManager) GetInt64Slice(key string) []int64 {
	ints := t.GetIntSlice(key)
	if ints == nil {
		return nil
	}
	result := make([]int64, len(ints))
	for i, v := range ints {
		result[i] = int64(v)
	}
	return result
}

func (t *TestConfigManager) GetFloat64Slice(key string) []float64 {
	value := t.Get(key)
	if value == nil {
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
	return nil
}

func (t *TestConfigManager) GetBoolSlice(key string) []bool {
	value := t.Get(key)
	if value == nil {
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
	return nil
}

func (t *TestConfigManager) GetStringMap(key string) map[string]string {
	value := t.Get(key)
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
	}
	return nil
}

func (t *TestConfigManager) GetStringMapString(key string) map[string]string {
	return t.GetStringMap(key)
}

func (t *TestConfigManager) GetStringMapStringSlice(key string) map[string][]string {
	value := t.Get(key)
	if value == nil {
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
	return nil
}

// Default value getters
func (t *TestConfigManager) GetWithDefault(key string, val any) interface{} {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return value
}

func (t *TestConfigManager) GetStringWithDefault(key string, val string) string {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetString(key)
}

func (t *TestConfigManager) GetIntWithDefault(key string, val int) int {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetInt(key)
}

func (t *TestConfigManager) GetInt8WithDefault(key string, val int8) int8 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetInt8(key)
}

func (t *TestConfigManager) GetInt16WithDefault(key string, val int16) int16 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetInt16(key)
}

func (t *TestConfigManager) GetInt32WithDefault(key string, val int32) int32 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetInt32(key)
}

func (t *TestConfigManager) GetInt64WithDefault(key string, val int64) int64 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetInt64(key)
}

func (t *TestConfigManager) GetUintWithDefault(key string, val uint) uint {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetUint(key)
}

func (t *TestConfigManager) GetUint8WithDefault(key string, val uint8) uint8 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetUint8(key)
}

func (t *TestConfigManager) GetUint16WithDefault(key string, val uint16) uint16 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetUint16(key)
}

func (t *TestConfigManager) GetUint32WithDefault(key string, val uint32) uint32 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetUint32(key)
}

func (t *TestConfigManager) GetUint64WithDefault(key string, val uint64) uint64 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetUint64(key)
}

func (t *TestConfigManager) GetFloat32WithDefault(key string, val float32) float32 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetFloat32(key)
}

func (t *TestConfigManager) GetFloat64WithDefault(key string, val float64) float64 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetFloat64(key)
}

func (t *TestConfigManager) GetBoolWithDefault(key string, val bool) bool {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetBool(key)
}

func (t *TestConfigManager) GetDurationWithDefault(key string, val time.Duration) time.Duration {
	value := t.Get(key)
	if value == nil {
		return val
	}
	return t.GetDuration(key)
}

func (t *TestConfigManager) GetTimeWithDefault(key string, val time.Time) time.Time {
	value := t.Get(key)
	if value == nil {
		return val
	}
	result := t.GetTime(key)
	if result.IsZero() {
		return val
	}
	return result
}

func (t *TestConfigManager) GetSizeInBytesWithDefault(key string, val uint64) uint64 {
	value := t.Get(key)
	if value == nil {
		return val
	}
	result := t.GetSizeInBytes(key)
	if result == 0 {
		return val
	}
	return result
}

// Collection getters with defaults
func (t *TestConfigManager) GetStringSliceWithDefault(key string, val []string) []string {
	result := t.GetStringSlice(key)
	if result == nil {
		return val
	}
	return result
}

func (t *TestConfigManager) GetIntSliceWithDefault(key string, val []int) []int {
	result := t.GetIntSlice(key)
	if result == nil {
		return val
	}
	return result
}

func (t *TestConfigManager) GetInt64SliceWithDefault(key string, val []int64) []int64 {
	result := t.GetInt64Slice(key)
	if result == nil {
		return val
	}
	return result
}

func (t *TestConfigManager) GetFloat64SliceWithDefault(key string, val []float64) []float64 {
	result := t.GetFloat64Slice(key)
	if result == nil {
		return val
	}
	return result
}

func (t *TestConfigManager) GetBoolSliceWithDefault(key string, val []bool) []bool {
	result := t.GetBoolSlice(key)
	if result == nil {
		return val
	}
	return result
}

func (t *TestConfigManager) GetStringMapWithDefault(key string, val map[string]string) map[string]string {
	result := t.GetStringMap(key)
	if result == nil {
		return val
	}
	return result
}

func (t *TestConfigManager) GetStringMapStringSliceWithDefault(key string, val map[string][]string) map[string][]string {
	result := t.GetStringMapStringSlice(key)
	if result == nil {
		return val
	}
	return result
}

// Configuration modification
func (t *TestConfigManager) Set(key string, value interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()

	oldValue := t.getValue(key)
	t.setValue(key, value)

	// Notify callbacks
	change := common.ConfigChange{
		Source:    "test",
		Type:      common.ChangeTypeSet,
		Key:       key,
		OldValue:  oldValue,
		NewValue:  value,
		Timestamp: time.Now(),
	}
	t.notifyChangeCallbacks(change)
}

// Binding methods - simplified for testing
func (t *TestConfigManager) Bind(key string, target interface{}) error {
	return t.BindWithOptions(key, target, common.DefaultBindOptions())
}

func (t *TestConfigManager) BindWithDefault(key string, target interface{}, defaultValue interface{}) error {
	options := common.DefaultBindOptions()
	options.DefaultValue = defaultValue
	return t.BindWithOptions(key, target, options)
}

func (t *TestConfigManager) BindWithOptions(key string, target interface{}, options common.BindOptions) error {
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

// Watch methods
func (t *TestConfigManager) WatchWithCallback(key string, callback func(string, interface{})) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.watchCallbacks[key] == nil {
		t.watchCallbacks[key] = make([]func(string, interface{}), 0)
	}
	t.watchCallbacks[key] = append(t.watchCallbacks[key], callback)
}

func (t *TestConfigManager) WatchChanges(callback func(common.ConfigChange)) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.changeCallbacks = append(t.changeCallbacks, callback)
}

// Metadata and introspection
func (t *TestConfigManager) GetSourceMetadata() map[string]*common.SourceMetadata {
	return map[string]*common.SourceMetadata{
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

func (t *TestConfigManager) GetKeys() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.getAllKeys(t.data, "")
}

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

func (t *TestConfigManager) HasKey(key string) bool {
	return t.Get(key) != nil
}

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

func (t *TestConfigManager) Size() int {
	return len(t.GetKeys())
}

// Structure operations
func (t *TestConfigManager) Sub(key string) common.ConfigManager {
	subData := t.GetSection(key)
	if subData == nil {
		subData = make(map[string]interface{})
	}
	return NewTestConfigManagerWithData(subData)
}

func (t *TestConfigManager) MergeWith(other common.ConfigManager) error {
	otherData := other.GetAllSettings()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mergeData(t.data, otherData)
	return nil
}

func (t *TestConfigManager) Clone() common.ConfigManager {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return NewTestConfigManagerWithData(t.data)
}

func (t *TestConfigManager) GetAllSettings() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.deepCopyMap(t.data)
}

// Utility methods
func (t *TestConfigManager) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.data = make(map[string]interface{})
	t.watchCallbacks = make(map[string][]func(string, interface{}))
	t.changeCallbacks = make([]func(common.ConfigChange), 0)
}

func (t *TestConfigManager) ExpandEnvVars() error {
	// Test implementation doesn't need to expand env vars
	return nil
}

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

// Compatibility aliases
func (t *TestConfigManager) GetBytesSize(key string) uint64 {
	return t.GetSizeInBytes(key)
}

func (t *TestConfigManager) GetBytesSizeWithDefault(key string, defaultValue uint64) uint64 {
	return t.GetSizeInBytesWithDefault(key, defaultValue)
}

func (t *TestConfigManager) InConfig(key string) bool {
	return t.HasKey(key)
}

func (t *TestConfigManager) UnmarshalKey(key string, rawVal interface{}) error {
	return t.Bind(key, rawVal)
}

func (t *TestConfigManager) Unmarshal(rawVal interface{}) error {
	return t.Bind("", rawVal)
}

func (t *TestConfigManager) AllKeys() []string {
	return t.GetKeys()
}

func (t *TestConfigManager) AllSettings() map[string]interface{} {
	return t.GetAllSettings()
}

func (t *TestConfigManager) ReadInConfig() error {
	return t.ReloadContext(context.Background())
}

func (t *TestConfigManager) SetConfigType(configType string) {
	// Test implementation ignores config type
}

func (t *TestConfigManager) SetConfigFile(filePath string) error {
	// Test implementation ignores file paths
	return nil
}

func (t *TestConfigManager) ConfigFileUsed() string {
	return "test://memory"
}

func (t *TestConfigManager) WatchConfig() error {
	return t.Watch(context.Background())
}

func (t *TestConfigManager) OnConfigChange(callback func(common.ConfigChange)) {
	t.WatchChanges(callback)
}

// Helper methods
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

	// Simplified binding - convert to JSON and back for easy binding
	// This is acceptable for test scenarios
	if mapData, ok := value.(map[string]interface{}); ok {
		return t.bindMapToStruct(mapData, targetValue.Elem())
	}

	// Direct assignment for simple types
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

		// Get field name from tags
		fieldName := t.getFieldName(fieldType)
		if fieldName == "" {
			continue
		}

		// Get value from map
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

	// Handle different field types
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
		// Try direct assignment
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

			// Recursively get nested keys
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

func (t *TestConfigManager) notifyChangeCallbacks(change common.ConfigChange) {
	for _, callback := range t.changeCallbacks {
		go callback(change)
	}
}

// TestConfigAssertions provides assertion helpers for testing configuration
type TestConfigAssertions struct {
	manager *TestConfigManager
}

// NewTestConfigAssertions creates assertion helpers for the test config manager
func NewTestConfigAssertions(manager common.ConfigManager) *TestConfigAssertions {
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

// SimulateConfigChange simulates a configuration change for testing watchers
func (t *TestConfigManager) SimulateConfigChange(key string, newValue interface{}) {
	oldValue := t.Get(key)
	t.Set(key, newValue)

	// This will trigger the normal change notification process
	change := common.ConfigChange{
		Source:    "test-simulation",
		Type:      common.ChangeTypeUpdate,
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

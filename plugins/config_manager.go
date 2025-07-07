package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
)

// configManager implements the ConfigManager interface
type configManager struct {
	mu        sync.RWMutex
	logger    logger.Logger
	configDir string
	configs   map[string]map[string]interface{}
	schemas   map[string]map[string]interface{}
	defaults  map[string]map[string]interface{}
	watchers  map[string][]func(map[string]interface{})
	envPrefix string
	autoSave  bool
	validator ConfigValidator
}

// ConfigValidator validates configuration values
type ConfigValidator interface {
	Validate(schema map[string]interface{}, config map[string]interface{}) error
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(configDir string) ConfigManager {
	cm := &configManager{
		logger:    logger.GetGlobalLogger().Named("plugin-config"),
		configDir: configDir,
		configs:   make(map[string]map[string]interface{}),
		schemas:   make(map[string]map[string]interface{}),
		defaults:  make(map[string]map[string]interface{}),
		watchers:  make(map[string][]func(map[string]interface{})),
		envPrefix: "FORGE_PLUGIN_",
		autoSave:  true,
		validator: NewJSONSchemaValidator(),
	}

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		cm.logger.Error("Failed to create config directory",
			logger.String("dir", configDir),
			logger.Error(err),
		)
	}

	// Load existing configurations
	cm.loadExistingConfigs()

	return cm
}

// Get retrieves configuration for a plugin
func (cm *configManager) Get(plugin string) (map[string]interface{}, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	config, exists := cm.configs[plugin]
	if !exists {
		// Return defaults if no custom config exists
		if defaults, hasDefaults := cm.defaults[plugin]; hasDefaults {
			result := make(map[string]interface{})
			for k, v := range defaults {
				result[k] = v
			}
			return result, nil
		}
		return make(map[string]interface{}), nil
	}

	// Create a deep copy to prevent external modification
	result := make(map[string]interface{})
	for k, v := range config {
		result[k] = deepCopyValue(v)
	}

	return result, nil
}

// Set sets configuration for a plugin
func (cm *configManager) Set(plugin string, config map[string]interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Validate configuration
	if err := cm.validateConfigUnsafe(plugin, config); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Apply environment overrides
	config = cm.applyEnvironmentOverridesUnsafe(plugin, config)

	// Apply defaults
	config = cm.applyDefaultsUnsafe(plugin, config)

	// Store configuration
	cm.configs[plugin] = config

	// Save to file if auto-save is enabled
	if cm.autoSave {
		if err := cm.saveConfigToFile(plugin, config); err != nil {
			cm.logger.Error("Failed to save configuration",
				logger.String("plugin", plugin),
				logger.Error(err),
			)
		}
	}

	// Notify watchers
	cm.notifyWatchersUnsafe(plugin, config)

	cm.logger.Info("Configuration updated",
		logger.String("plugin", plugin),
	)

	return nil
}

// Update updates specific fields in plugin configuration
func (cm *configManager) Update(plugin string, updates map[string]interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Get current configuration
	current := cm.configs[plugin]
	if current == nil {
		current = make(map[string]interface{})
	}

	// Apply updates
	updated := make(map[string]interface{})
	for k, v := range current {
		updated[k] = v
	}
	for k, v := range updates {
		updated[k] = v
	}

	// Validate updated configuration
	if err := cm.validateConfigUnsafe(plugin, updated); err != nil {
		return fmt.Errorf("updated configuration validation failed: %w", err)
	}

	// Apply environment overrides
	updated = cm.applyEnvironmentOverridesUnsafe(plugin, updated)

	// Apply defaults for missing keys
	updated = cm.applyDefaultsUnsafe(plugin, updated)

	// Store updated configuration
	cm.configs[plugin] = updated

	// Save to file if auto-save is enabled
	if cm.autoSave {
		if err := cm.saveConfigToFile(plugin, updated); err != nil {
			cm.logger.Error("Failed to save updated configuration",
				logger.String("plugin", plugin),
				logger.Error(err),
			)
		}
	}

	// Notify watchers
	cm.notifyWatchersUnsafe(plugin, updated)

	cm.logger.Info("Configuration updated",
		logger.String("plugin", plugin),
		logger.Int("updated_fields", len(updates)),
	)

	return nil
}

// Delete removes configuration for a plugin
func (cm *configManager) Delete(plugin string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Remove from memory
	delete(cm.configs, plugin)
	delete(cm.schemas, plugin)
	delete(cm.defaults, plugin)
	delete(cm.watchers, plugin)

	// Remove configuration file
	configPath := cm.getConfigPath(plugin)
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		cm.logger.Error("Failed to remove configuration file",
			logger.String("plugin", plugin),
			logger.String("path", configPath),
			logger.Error(err),
		)
		return err
	}

	cm.logger.Info("Configuration deleted",
		logger.String("plugin", plugin),
	)

	return nil
}

// ValidateConfig validates configuration against schema
func (cm *configManager) ValidateConfig(plugin string, config map[string]interface{}) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.validateConfigUnsafe(plugin, config)
}

// GetSchema returns the configuration schema for a plugin
func (cm *configManager) GetSchema(plugin string) (map[string]interface{}, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	schema, exists := cm.schemas[plugin]
	if !exists {
		return nil, fmt.Errorf("no schema found for plugin: %s", plugin)
	}

	// Return a copy
	result := make(map[string]interface{})
	for k, v := range schema {
		result[k] = deepCopyValue(v)
	}

	return result, nil
}

// GetDefaults returns default configuration for a plugin
func (cm *configManager) GetDefaults(plugin string) (map[string]interface{}, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	defaults, exists := cm.defaults[plugin]
	if !exists {
		return make(map[string]interface{}), nil
	}

	// Return a copy
	result := make(map[string]interface{})
	for k, v := range defaults {
		result[k] = deepCopyValue(v)
	}

	return result, nil
}

// ApplyDefaults applies default values to configuration
func (cm *configManager) ApplyDefaults(plugin string, config map[string]interface{}) map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.applyDefaultsUnsafe(plugin, config)
}

// ApplyEnvironmentOverrides applies environment variable overrides
func (cm *configManager) ApplyEnvironmentOverrides(plugin string, config map[string]interface{}) map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.applyEnvironmentOverridesUnsafe(plugin, config)
}

// OnConfigChange registers a callback for configuration changes
func (cm *configManager) OnConfigChange(plugin string, callback func(map[string]interface{})) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.watchers[plugin] = append(cm.watchers[plugin], callback)

	cm.logger.Debug("Configuration watcher registered",
		logger.String("plugin", plugin),
	)

	return nil
}

// Private helper methods

func (cm *configManager) validateConfigUnsafe(plugin string, config map[string]interface{}) error {
	schema, exists := cm.schemas[plugin]
	if !exists {
		// No schema defined, allow any configuration
		return nil
	}

	if cm.validator != nil {
		return cm.validator.Validate(schema, config)
	}

	// Basic validation without schema validator
	return cm.basicValidation(schema, config)
}

func (cm *configManager) basicValidation(schema, config map[string]interface{}) error {
	for key, schemaValue := range schema {
		configValue, exists := config[key]

		// Check required fields
		if schemaMap, ok := schemaValue.(map[string]interface{}); ok {
			if required, hasRequired := schemaMap["required"]; hasRequired && required.(bool) {
				if !exists {
					return fmt.Errorf("required field missing: %s", key)
				}
			}

			// Type checking
			if expectedType, hasType := schemaMap["type"]; hasType && exists {
				if !cm.isValidType(configValue, expectedType.(string)) {
					return fmt.Errorf("invalid type for field %s: expected %s", key, expectedType)
				}
			}

			// Enum validation
			if enum, hasEnum := schemaMap["enum"]; hasEnum && exists {
				if !cm.isValidEnum(configValue, enum) {
					return fmt.Errorf("invalid value for field %s: must be one of %v", key, enum)
				}
			}

			// Range validation
			if minimum, hasMin := schemaMap["minimum"]; hasMin && exists {
				if !cm.isAboveMinimum(configValue, minimum) {
					return fmt.Errorf("value for field %s is below minimum: %v", key, minimum)
				}
			}

			if maximum, hasMax := schemaMap["maximum"]; hasMax && exists {
				if !cm.isBelowMaximum(configValue, maximum) {
					return fmt.Errorf("value for field %s is above maximum: %v", key, maximum)
				}
			}
		}
	}

	return nil
}

func (cm *configManager) isValidType(value interface{}, expectedType string) bool {
	switch expectedType {
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		switch value.(type) {
		case int, int32, int64, float64:
			return true
		}
		return false
	case "number":
		switch value.(type) {
		case int, int32, int64, float32, float64:
			return true
		}
		return false
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "array":
		return reflect.TypeOf(value).Kind() == reflect.Slice
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	}
	return true
}

func (cm *configManager) isValidEnum(value interface{}, enum interface{}) bool {
	enumSlice, ok := enum.([]interface{})
	if !ok {
		return false
	}

	for _, allowed := range enumSlice {
		if value == allowed {
			return true
		}
	}
	return false
}

func (cm *configManager) isAboveMinimum(value interface{}, minimum interface{}) bool {
	switch v := value.(type) {
	case int:
		if min, ok := minimum.(float64); ok {
			return float64(v) >= min
		}
	case int64:
		if min, ok := minimum.(float64); ok {
			return float64(v) >= min
		}
	case float64:
		if min, ok := minimum.(float64); ok {
			return v >= min
		}
	}
	return true
}

func (cm *configManager) isBelowMaximum(value interface{}, maximum interface{}) bool {
	switch v := value.(type) {
	case int:
		if max, ok := maximum.(float64); ok {
			return float64(v) <= max
		}
	case int64:
		if max, ok := maximum.(float64); ok {
			return float64(v) <= max
		}
	case float64:
		if max, ok := maximum.(float64); ok {
			return v <= max
		}
	}
	return true
}

func (cm *configManager) applyDefaultsUnsafe(plugin string, config map[string]interface{}) map[string]interface{} {
	defaults, exists := cm.defaults[plugin]
	if !exists {
		return config
	}

	result := make(map[string]interface{})

	// Apply defaults first
	for k, v := range defaults {
		result[k] = deepCopyValue(v)
	}

	// Override with actual config values
	for k, v := range config {
		result[k] = deepCopyValue(v)
	}

	return result
}

func (cm *configManager) applyEnvironmentOverridesUnsafe(plugin string, config map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range config {
		result[k] = v
	}

	// Look for environment variables with plugin-specific prefix
	prefix := fmt.Sprintf("%s%s_", cm.envPrefix, strings.ToUpper(plugin))

	for key := range config {
		envKey := prefix + strings.ToUpper(key)
		if envValue := os.Getenv(envKey); envValue != "" {
			// Try to convert environment value to appropriate type
			result[key] = cm.convertEnvValue(envValue, config[key])
		}
	}

	return result
}

func (cm *configManager) convertEnvValue(envValue string, originalValue interface{}) interface{} {
	switch originalValue.(type) {
	case bool:
		if val, err := strconv.ParseBool(envValue); err == nil {
			return val
		}
	case int:
		if val, err := strconv.Atoi(envValue); err == nil {
			return val
		}
	case int64:
		if val, err := strconv.ParseInt(envValue, 10, 64); err == nil {
			return val
		}
	case float64:
		if val, err := strconv.ParseFloat(envValue, 64); err == nil {
			return val
		}
	case time.Duration:
		if val, err := time.ParseDuration(envValue); err == nil {
			return val
		}
	}

	// Default to string
	return envValue
}

func (cm *configManager) notifyWatchersUnsafe(plugin string, config map[string]interface{}) {
	watchers, exists := cm.watchers[plugin]
	if !exists {
		return
	}

	// Create a copy for watchers
	configCopy := make(map[string]interface{})
	for k, v := range config {
		configCopy[k] = deepCopyValue(v)
	}

	// Notify all watchers asynchronously
	for _, watcher := range watchers {
		go func(w func(map[string]interface{})) {
			defer func() {
				if r := recover(); r != nil {
					cm.logger.Error("Configuration watcher panicked",
						logger.String("plugin", plugin),
						logger.Any("panic", r),
					)
				}
			}()
			w(configCopy)
		}(watcher)
	}
}

func (cm *configManager) loadExistingConfigs() {
	if cm.configDir == "" {
		return
	}

	entries, err := os.ReadDir(cm.configDir)
	if err != nil {
		cm.logger.Error("Failed to read config directory",
			logger.String("dir", cm.configDir),
			logger.Error(err),
		)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		pluginName := strings.TrimSuffix(entry.Name(), ".json")
		configPath := filepath.Join(cm.configDir, entry.Name())

		if err := cm.loadConfigFromFile(pluginName, configPath); err != nil {
			cm.logger.Error("Failed to load configuration",
				logger.String("plugin", pluginName),
				logger.String("path", configPath),
				logger.Error(err),
			)
		}
	}

	cm.logger.Info("Loaded existing configurations",
		logger.Int("count", len(cm.configs)),
	)
}

func (cm *configManager) loadConfigFromFile(plugin, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	cm.configs[plugin] = config
	return nil
}

func (cm *configManager) saveConfigToFile(plugin string, config map[string]interface{}) error {
	if cm.configDir == "" {
		return nil
	}

	configPath := cm.getConfigPath(plugin)
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func (cm *configManager) getConfigPath(plugin string) string {
	return filepath.Join(cm.configDir, plugin+".json")
}

// Public utility methods

// SetSchema sets the configuration schema for a plugin
func (cm *configManager) SetSchema(plugin string, schema map[string]interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.schemas[plugin] = schema

	cm.logger.Debug("Configuration schema set",
		logger.String("plugin", plugin),
	)

	return nil
}

// SetDefaults sets default configuration for a plugin
func (cm *configManager) SetDefaults(plugin string, defaults map[string]interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.defaults[plugin] = defaults

	cm.logger.Debug("Default configuration set",
		logger.String("plugin", plugin),
	)

	return nil
}

// SetAutoSave enables or disables automatic saving
func (cm *configManager) SetAutoSave(enabled bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.autoSave = enabled

	cm.logger.Info("Auto-save configuration changed",
		logger.Bool("enabled", enabled),
	)
}

// SaveAll saves all configurations to files
func (cm *configManager) SaveAll() error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var errors []error

	for plugin, config := range cm.configs {
		if err := cm.saveConfigToFile(plugin, config); err != nil {
			errors = append(errors, fmt.Errorf("failed to save %s: %w", plugin, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to save %d configurations: %v", len(errors), errors)
	}

	cm.logger.Info("All configurations saved")
	return nil
}

// GetStatistics returns configuration manager statistics
func (cm *configManager) GetStatistics() map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	totalWatchers := 0
	for _, watchers := range cm.watchers {
		totalWatchers += len(watchers)
	}

	return map[string]interface{}{
		"total_configs":  len(cm.configs),
		"total_schemas":  len(cm.schemas),
		"total_defaults": len(cm.defaults),
		"total_watchers": totalWatchers,
		"config_dir":     cm.configDir,
		"auto_save":      cm.autoSave,
		"env_prefix":     cm.envPrefix,
	}
}

// Utility functions

func deepCopyValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		result := make(map[string]interface{})
		for k, val := range v {
			result[k] = deepCopyValue(val)
		}
		return result
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, val := range v {
			result[i] = deepCopyValue(val)
		}
		return result
	default:
		return v
	}
}

// JSON Schema Validator implementation
type jsonSchemaValidator struct {
	logger logger.Logger
}

func NewJSONSchemaValidator() ConfigValidator {
	return &jsonSchemaValidator{
		logger: logger.GetGlobalLogger().Named("json-schema-validator"),
	}
}

func (v *jsonSchemaValidator) Validate(schema map[string]interface{}, config map[string]interface{}) error {
	// This is a simplified implementation
	// In a real implementation, you would use a proper JSON schema validation library
	v.logger.Debug("Validating configuration against schema")

	// For now, just check required fields
	for key, schemaValue := range schema {
		if schemaMap, ok := schemaValue.(map[string]interface{}); ok {
			if required, hasRequired := schemaMap["required"]; hasRequired && required.(bool) {
				if _, exists := config[key]; !exists {
					return fmt.Errorf("required field missing: %s", key)
				}
			}
		}
	}

	return nil
}

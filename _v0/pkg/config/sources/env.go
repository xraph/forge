package sources

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	configcore "github.com/xraph/forge/v0/pkg/config/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// EnvSource represents an environment variable configuration source
type EnvSource struct {
	name          string
	prefix        string
	priority      int
	separator     string
	keyMapping    map[string]string
	valueMapping  map[string]func(string) interface{}
	watching      bool
	lastEnv       map[string]string
	watchTicker   *time.Ticker
	watchStop     chan struct{}
	watchCallback func(map[string]interface{})
	mu            sync.RWMutex
	logger        common.Logger
	errorHandler  common.ErrorHandler
	options       EnvSourceOptions
}

// EnvSourceOptions contains options for environment variable sources
type EnvSourceOptions struct {
	Name           string
	Prefix         string
	Priority       int
	Separator      string
	WatchEnabled   bool
	WatchInterval  time.Duration
	CaseSensitive  bool
	IgnoreEmpty    bool
	TypeConversion bool
	KeyTransform   KeyTransformFunc
	ValueTransform ValueTransformFunc
	KeyMapping     map[string]string
	ValueMapping   map[string]func(string) interface{}
	RequiredVars   []string
	SecretVars     []string
	Logger         common.Logger
	ErrorHandler   common.ErrorHandler
}

// EnvSourceConfig contains configuration for creating environment variable sources
type EnvSourceConfig struct {
	Prefix         string            `yaml:"prefix" json:"prefix"`
	Priority       int               `yaml:"priority" json:"priority"`
	Separator      string            `yaml:"separator" json:"separator"`
	WatchEnabled   bool              `yaml:"watch_enabled" json:"watch_enabled"`
	WatchInterval  time.Duration     `yaml:"watch_interval" json:"watch_interval"`
	CaseSensitive  bool              `yaml:"case_sensitive" json:"case_sensitive"`
	IgnoreEmpty    bool              `yaml:"ignore_empty" json:"ignore_empty"`
	TypeConversion bool              `yaml:"type_conversion" json:"type_conversion"`
	RequiredVars   []string          `yaml:"required_vars" json:"required_vars"`
	SecretVars     []string          `yaml:"secret_vars" json:"secret_vars"`
	KeyMapping     map[string]string `yaml:"key_mapping" json:"key_mapping"`
}

// KeyTransformFunc transforms environment variable keys
type KeyTransformFunc func(string) string

// ValueTransformFunc transforms environment variable values
type ValueTransformFunc func(string, interface{}) interface{}

// NewEnvSource creates a new environment variable configuration source
func NewEnvSource(prefix string, options EnvSourceOptions) (configcore.ConfigSource, error) {
	if options.Separator == "" {
		options.Separator = "_"
	}

	if options.WatchInterval == 0 {
		options.WatchInterval = 30 * time.Second
	}

	name := options.Name
	if name == "" {
		if prefix != "" {
			name = fmt.Sprintf("env:%s", prefix)
		} else {
			name = "env"
		}
	}

	source := &EnvSource{
		name:         name,
		prefix:       prefix,
		priority:     options.Priority,
		separator:    options.Separator,
		keyMapping:   options.KeyMapping,
		valueMapping: options.ValueMapping,
		logger:       options.Logger,
		errorHandler: options.ErrorHandler,
		options:      options,
		lastEnv:      make(map[string]string),
	}

	// Initialize key mapping if not provided
	if source.keyMapping == nil {
		source.keyMapping = make(map[string]string)
	}

	// Initialize value mapping if not provided
	if source.valueMapping == nil {
		source.valueMapping = make(map[string]func(string) interface{})
	}

	// Set up default value transformations if type conversion is enabled
	if options.TypeConversion {
		source.setupDefaultValueTransforms()
	}

	return source, nil
}

// Name returns the source name
func (es *EnvSource) Name() string {
	return es.name
}

// GetName returns the source name (alias for Name)
func (es *EnvSource) GetName() string {
	return es.name
}

// GetType returns the source type
func (es *EnvSource) GetType() string {
	return "environment"
}

// IsAvailable checks if the source is available
func (es *EnvSource) IsAvailable(ctx context.Context) bool {
	// Environment variables are always available
	return true
}

// Priority returns the source priority
func (es *EnvSource) Priority() int {
	return es.priority
}

// Load loads configuration from environment variables
func (es *EnvSource) Load(ctx context.Context) (map[string]interface{}, error) {
	if es.logger != nil {
		es.logger.Debug("loading configuration from environment variables",
			logger.String("prefix", es.prefix),
			logger.String("separator", es.separator),
		)
	}

	config := make(map[string]interface{})
	envVars := os.Environ()

	// Process each environment variable
	for _, env := range envVars {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key, value := parts[0], parts[1]

		// Check if this variable matches our prefix
		if !es.matchesPrefix(key) {
			continue
		}

		// Skip empty values if configured to do so
		if es.options.IgnoreEmpty && value == "" {
			continue
		}

		// Transform the key
		configKey := es.transformKey(key)
		if configKey == "" {
			continue
		}

		// Transform the value
		configValue := es.transformValue(key, value)

		// Set the configuration value
		es.setNestedValue(config, configKey, configValue)
	}

	// Check required variables
	if err := es.checkRequiredVars(config); err != nil {
		return nil, err
	}

	// Update last environment state for watching
	es.updateLastEnvState()

	if es.logger != nil {
		es.logger.Info("configuration loaded from environment variables",
			logger.String("prefix", es.prefix),
			logger.Int("keys", len(config)),
		)
	}

	return config, nil
}

// Watch starts watching environment variables for changes
func (es *EnvSource) Watch(ctx context.Context, callback func(map[string]interface{})) error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.watching {
		return common.ErrConfigError("already watching environment variables", nil)
	}

	if !es.IsWatchable() {
		return common.ErrConfigError("environment variable watching is not enabled", nil)
	}

	es.watchCallback = callback
	es.watchStop = make(chan struct{})
	es.watchTicker = time.NewTicker(es.options.WatchInterval)
	es.watching = true

	// Start watching goroutine
	go es.watchLoop(ctx)

	if es.logger != nil {
		es.logger.Info("started watching environment variables",
			logger.String("prefix", es.prefix),
			logger.Duration("interval", es.options.WatchInterval),
		)
	}

	return nil
}

// StopWatch stops watching environment variables
func (es *EnvSource) StopWatch() error {
	es.mu.Lock()
	defer es.mu.Unlock()

	if !es.watching {
		return nil
	}

	if es.watchTicker != nil {
		es.watchTicker.Stop()
		es.watchTicker = nil
	}

	if es.watchStop != nil {
		close(es.watchStop)
		es.watchStop = nil
	}

	es.watching = false
	es.watchCallback = nil

	if es.logger != nil {
		es.logger.Info("stopped watching environment variables",
			logger.String("prefix", es.prefix),
		)
	}

	return nil
}

// Reload forces a reload of environment variables
func (es *EnvSource) Reload(ctx context.Context) error {
	if es.logger != nil {
		es.logger.Info("reloading environment variables",
			logger.String("prefix", es.prefix),
		)
	}

	// Just load again - the environment state will be updated
	_, err := es.Load(ctx)
	return err
}

// IsWatchable returns true if environment watching is enabled
func (es *EnvSource) IsWatchable() bool {
	return es.options.WatchEnabled
}

// SupportsSecrets returns true if secret variables are configured
func (es *EnvSource) SupportsSecrets() bool {
	return len(es.options.SecretVars) > 0
}

// GetSecret retrieves a secret environment variable
func (es *EnvSource) GetSecret(ctx context.Context, key string) (string, error) {
	// Check if the key is in the list of secret variables
	for _, secretVar := range es.options.SecretVars {
		if secretVar == key {
			value := os.Getenv(key)
			if value == "" {
				return "", common.ErrConfigError(fmt.Sprintf("secret environment variable not found: %s", key), nil)
			}
			return value, nil
		}
	}

	return "", common.ErrConfigError(fmt.Sprintf("key %s is not configured as a secret variable", key), nil)
}

// matchesPrefix checks if an environment variable key matches our prefix
func (es *EnvSource) matchesPrefix(key string) bool {
	if es.prefix == "" {
		return true // No prefix means all variables
	}

	if es.options.CaseSensitive {
		return strings.HasPrefix(key, es.prefix)
	}

	return strings.HasPrefix(strings.ToUpper(key), strings.ToUpper(es.prefix))
}

// transformKey transforms an environment variable key to a configuration key
func (es *EnvSource) transformKey(envKey string) string {
	// Check for explicit key mapping first
	if mappedKey, exists := es.keyMapping[envKey]; exists {
		return mappedKey
	}

	// Remove prefix if present
	key := envKey
	if es.prefix != "" {
		if es.options.CaseSensitive {
			if strings.HasPrefix(key, es.prefix) {
				key = key[len(es.prefix):]
			}
		} else {
			upperKey := strings.ToUpper(key)
			upperPrefix := strings.ToUpper(es.prefix)
			if strings.HasPrefix(upperKey, upperPrefix) {
				key = key[len(es.prefix):]
			}
		}
	}

	// Remove leading separator
	if strings.HasPrefix(key, es.separator) {
		key = key[len(es.separator):]
	}

	// Apply key transformation if configured
	if es.options.KeyTransform != nil {
		key = es.options.KeyTransform(key)
	} else {
		// Default transformation: convert to lowercase and replace separators with dots
		key = strings.ToLower(key)
		key = strings.ReplaceAll(key, es.separator, ".")
	}

	return key
}

// transformValue transforms an environment variable value
func (es *EnvSource) transformValue(envKey, envValue string) interface{} {
	// Check for explicit value mapping first
	if transform, exists := es.valueMapping[envKey]; exists {
		return transform(envValue)
	}

	// Apply value transformation if configured
	if es.options.ValueTransform != nil {
		return es.options.ValueTransform(envKey, envValue)
	}

	// Default transformation (type conversion if enabled)
	if es.options.TypeConversion {
		return es.convertValue(envValue)
	}

	return envValue
}

// convertValue attempts to convert a string value to an appropriate type
func (es *EnvSource) convertValue(value string) interface{} {
	// Try boolean first
	if boolVal, err := strconv.ParseBool(value); err == nil {
		return boolVal
	}

	// Try integer
	if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
		// Return int if it fits, otherwise int64
		const maxInt = int(^uint(0) >> 1)
		const minInt = -maxInt - 1

		if intVal >= int64(minInt) && intVal <= int64(maxInt) {
			return int(intVal)
		}
		return intVal
	}

	// Try float
	if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
		return floatVal
	}

	// Try comma-separated list
	if strings.Contains(value, ",") {
		parts := strings.Split(value, ",")
		result := make([]string, len(parts))
		for i, part := range parts {
			result[i] = strings.TrimSpace(part)
		}
		return result
	}

	// Return as string
	return value
}

// setNestedValue sets a nested configuration value using dot notation
func (es *EnvSource) setNestedValue(config map[string]interface{}, key string, value interface{}) {
	keys := strings.Split(key, ".")
	current := config

	for i, k := range keys {
		if i == len(keys)-1 {
			// Last key - set the value
			current[k] = value
		} else {
			// Intermediate key - ensure map exists
			if _, exists := current[k]; !exists {
				current[k] = make(map[string]interface{})
			}

			if nextMap, ok := current[k].(map[string]interface{}); ok {
				current = nextMap
			} else {
				// Type conflict - create new map
				current[k] = make(map[string]interface{})
				current = current[k].(map[string]interface{})
			}
		}
	}
}

// checkRequiredVars checks that all required environment variables are present
func (es *EnvSource) checkRequiredVars(config map[string]interface{}) error {
	for _, required := range es.options.RequiredVars {
		if !es.hasNestedKey(config, required) {
			return common.ErrConfigError(fmt.Sprintf("required environment variable missing: %s", required), nil)
		}
	}
	return nil
}

// hasNestedKey checks if a nested key exists in the configuration
func (es *EnvSource) hasNestedKey(config map[string]interface{}, key string) bool {
	keys := strings.Split(key, ".")
	current := interface{}(config)

	for _, k := range keys {
		if currentMap, ok := current.(map[string]interface{}); ok {
			if value, exists := currentMap[k]; exists {
				current = value
			} else {
				return false
			}
		} else {
			return false
		}
	}

	return true
}

// updateLastEnvState updates the last known environment state for change detection
func (es *EnvSource) updateLastEnvState() {
	es.mu.Lock()
	defer es.mu.Unlock()

	es.lastEnv = make(map[string]string)
	envVars := os.Environ()

	for _, env := range envVars {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			key, value := parts[0], parts[1]
			if es.matchesPrefix(key) {
				es.lastEnv[key] = value
			}
		}
	}
}

// watchLoop is the main watching loop for environment variables
func (es *EnvSource) watchLoop(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			if es.logger != nil {
				es.logger.Error("panic in environment watch loop",
					logger.String("prefix", es.prefix),
					logger.Any("panic", r),
				)
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-es.watchStop:
			return
		case <-es.watchTicker.C:
			es.checkForChanges(ctx)
		}
	}
}

// checkForChanges checks for changes in environment variables
func (es *EnvSource) checkForChanges(ctx context.Context) {
	// Get current environment state
	currentEnv := make(map[string]string)
	envVars := os.Environ()

	for _, env := range envVars {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			key, value := parts[0], parts[1]
			if es.matchesPrefix(key) {
				currentEnv[key] = value
			}
		}
	}

	// Compare with last known state
	es.mu.RLock()
	lastEnv := make(map[string]string)
	for k, v := range es.lastEnv {
		lastEnv[k] = v
	}
	es.mu.RUnlock()

	// Check for changes
	hasChanges := false

	// Check for new or modified variables
	for key, value := range currentEnv {
		if lastValue, exists := lastEnv[key]; !exists || lastValue != value {
			hasChanges = true
			break
		}
	}

	// Check for deleted variables
	if !hasChanges {
		for key := range lastEnv {
			if _, exists := currentEnv[key]; !exists {
				hasChanges = true
				break
			}
		}
	}

	if hasChanges {
		if es.logger != nil {
			es.logger.Info("environment variable changes detected",
				logger.String("prefix", es.prefix),
			)
		}

		// Load new configuration
		if data, err := es.Load(ctx); err == nil {
			if es.watchCallback != nil {
				es.watchCallback(data)
			}
		} else {
			es.handleWatchError(err)
		}
	}
}

// handleWatchError handles errors during watching
func (es *EnvSource) handleWatchError(err error) {
	if es.logger != nil {
		es.logger.Error("environment variable watch error",
			logger.String("prefix", es.prefix),
			logger.Error(err),
		)
	}

	if es.errorHandler != nil {
		es.errorHandler.HandleError(nil, common.ErrConfigError(fmt.Sprintf("environment watch error for prefix %s", es.prefix), err))
	}
}

// setupDefaultValueTransforms sets up default value transformation functions
func (es *EnvSource) setupDefaultValueTransforms() {
	// Add common transformations for specific variable patterns

	// Transform duration strings
	es.addValueTransform("*TIMEOUT*", func(value string) interface{} {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
		return value
	})

	// Transform size strings (e.g., "1MB", "1GB")
	es.addValueTransform("*SIZE*", func(value string) interface{} {
		return es.parseSize(value)
	})

	// Transform URL lists
	es.addValueTransform("*URLS*", func(value string) interface{} {
		if strings.Contains(value, ",") {
			urls := strings.Split(value, ",")
			result := make([]string, len(urls))
			for i, url := range urls {
				result[i] = strings.TrimSpace(url)
			}
			return result
		}
		return value
	})
}

// addValueTransform adds a value transformation for keys matching a pattern
func (es *EnvSource) addValueTransform(pattern string, transform func(string) interface{}) {
	// This is a simplified pattern matching - in production you might want regex
	for envKey := range es.valueMapping {
		if es.matchesPattern(envKey, pattern) {
			es.valueMapping[envKey] = transform
		}
	}
}

// matchesPattern checks if a key matches a simple pattern (with * wildcards)
func (es *EnvSource) matchesPattern(key, pattern string) bool {
	if pattern == "*" {
		return true
	}

	// Simple wildcard matching
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.Contains(strings.ToUpper(key), strings.ToUpper(parts[1]))
		}
	}

	return strings.ToUpper(key) == strings.ToUpper(pattern)
}

// parseSize parses size strings like "1MB", "1GB", etc.
func (es *EnvSource) parseSize(value string) interface{} {
	value = strings.ToUpper(strings.TrimSpace(value))

	suffixes := map[string]int64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}

	for suffix, multiplier := range suffixes {
		if strings.HasSuffix(value, suffix) {
			numStr := strings.TrimSuffix(value, suffix)
			if num, err := strconv.ParseInt(numStr, 10, 64); err == nil {
				return num * multiplier
			}
		}
	}

	return value
}

// GetPrefix returns the environment variable prefix
func (es *EnvSource) GetPrefix() string {
	return es.prefix
}

// GetSeparator returns the key separator
func (es *EnvSource) GetSeparator() string {
	return es.separator
}

// IsWatching returns true if the source is watching for changes
func (es *EnvSource) IsWatching() bool {
	es.mu.RLock()
	defer es.mu.RUnlock()
	return es.watching
}

// GetKeyMapping returns the key mapping configuration
func (es *EnvSource) GetKeyMapping() map[string]string {
	es.mu.RLock()
	defer es.mu.RUnlock()

	mapping := make(map[string]string)
	for k, v := range es.keyMapping {
		mapping[k] = v
	}
	return mapping
}

// EnvSourceFactory creates environment variable sources
type EnvSourceFactory struct {
	logger       common.Logger
	errorHandler common.ErrorHandler
}

// NewEnvSourceFactory creates a new environment variable source factory
func NewEnvSourceFactory(logger common.Logger, errorHandler common.ErrorHandler) *EnvSourceFactory {
	return &EnvSourceFactory{
		logger:       logger,
		errorHandler: errorHandler,
	}
}

// CreateFromConfig creates an environment variable source from configuration
func (factory *EnvSourceFactory) CreateFromConfig(config EnvSourceConfig) (configcore.ConfigSource, error) {
	options := EnvSourceOptions{
		Name:           fmt.Sprintf("env:%s", config.Prefix),
		Prefix:         config.Prefix,
		Priority:       config.Priority,
		Separator:      config.Separator,
		WatchEnabled:   config.WatchEnabled,
		WatchInterval:  config.WatchInterval,
		CaseSensitive:  config.CaseSensitive,
		IgnoreEmpty:    config.IgnoreEmpty,
		TypeConversion: config.TypeConversion,
		RequiredVars:   config.RequiredVars,
		SecretVars:     config.SecretVars,
		KeyMapping:     config.KeyMapping,
		Logger:         factory.logger,
		ErrorHandler:   factory.errorHandler,
	}

	return NewEnvSource(config.Prefix, options)
}

// CreateWithDefaults creates an environment variable source with default settings
func (factory *EnvSourceFactory) CreateWithDefaults(prefix string) (configcore.ConfigSource, error) {
	options := EnvSourceOptions{
		Prefix:         prefix,
		Priority:       100, // Lower priority than files
		Separator:      "_",
		WatchEnabled:   true,
		WatchInterval:  30 * time.Second,
		CaseSensitive:  false,
		IgnoreEmpty:    true,
		TypeConversion: true,
		Logger:         factory.logger,
		ErrorHandler:   factory.errorHandler,
	}

	return NewEnvSource(prefix, options)
}

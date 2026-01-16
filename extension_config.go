package forge

import (
	"fmt"
	"reflect"

	"github.com/xraph/forge/errors"
)

// ExtensionConfigLoader provides helper methods for loading extension configuration
// from ConfigManager with fallback to programmatic config.
//
// Extensions can use LoadConfig to:
//  1. Try loading from "extensions.{name}" key
//  2. Fallback to "{name}" key (legacy/v1 compatibility)
//  3. Use provided defaults if config not found
//  4. Optionally fail if config is required but not found
type ExtensionConfigLoader struct {
	app    App
	logger Logger
}

// NewExtensionConfigLoader creates a new config loader.
func NewExtensionConfigLoader(app App, logger Logger) *ExtensionConfigLoader {
	return &ExtensionConfigLoader{
		app:    app,
		logger: logger,
	}
}

// LoadConfig loads configuration for an extension from ConfigManager.
//
// It tries the following keys in order:
//  1. "extensions.{key}" - Namespaced pattern (preferred)
//  2. "{key}" - Top-level pattern (legacy/v1 compatibility)
//
// Parameters:
//   - key: The config key (e.g., "cache", "mcp")
//   - target: Pointer to config struct to populate
//   - programmaticConfig: Config provided programmatically (may be partially filled)
//   - defaults: Default config to use if nothing found
//   - requireConfig: If true, returns error when config not found; if false, uses defaults
//
// Returns error only if requireConfig=true and config not found, or if binding fails.
func (l *ExtensionConfigLoader) LoadConfig(
	key string,
	target any,
	programmaticConfig any,
	defaults any,
	requireConfig bool,
) error {
	// Validate inputs
	if target == nil {
		return errors.New("target cannot be nil")
	}

	targetVal := reflect.ValueOf(target)
	if targetVal.Kind() != reflect.Ptr {
		return errors.New("target must be a pointer")
	}

	// Try to get ConfigManager from app
	configManager, err := l.getConfigManager()
	if err != nil {
		// No config manager available
		if requireConfig {
			return fmt.Errorf("config required but no ConfigManager available: %w", err)
		}

		// Use programmatic config or defaults
		if programmaticConfig != nil && !isZeroValue(programmaticConfig) {
			return l.copyConfig(programmaticConfig, target)
		}

		if defaults != nil {
			return l.copyConfig(defaults, target)
		}

		if l.logger != nil {
			l.logger.Debug("no config manager available, using defaults",
				F("key", key),
			)
		}

		return nil
	}

	// Try namespaced key first: "extensions.{key}"
	namespacedKey := "extensions." + key

	found, err := l.tryBindConfig(configManager, namespacedKey, target)
	if err != nil {
		return fmt.Errorf("failed to bind config from %s: %w", namespacedKey, err)
	}

	if found {
		if l.logger != nil {
			l.logger.Debug("loaded config from ConfigManager",
				F("key", namespacedKey),
			)
		}

		// Merge with programmatic config (programmatic takes precedence)
		if programmaticConfig != nil && !isZeroValue(programmaticConfig) {
			if err := l.mergeConfig(programmaticConfig, target); err != nil {
				return fmt.Errorf("failed to merge programmatic config: %w", err)
			}
		}

		return nil
	}

	// Try top-level key: "{key}"
	found, err = l.tryBindConfig(configManager, key, target)
	if err != nil {
		return fmt.Errorf("failed to bind config from %s: %w", key, err)
	}

	if found {
		if l.logger != nil {
			l.logger.Debug("loaded config from ConfigManager (legacy key)",
				F("key", key),
			)
		}

		// Merge with programmatic config (programmatic takes precedence)
		if programmaticConfig != nil && !isZeroValue(programmaticConfig) {
			if err := l.mergeConfig(programmaticConfig, target); err != nil {
				return fmt.Errorf("failed to merge programmatic config: %w", err)
			}
		}

		return nil
	}

	// Config not found in ConfigManager
	if requireConfig {
		return fmt.Errorf("config required but not found (tried: %s, %s)", namespacedKey, key)
	}

	// Use programmatic config or defaults
	if programmaticConfig != nil && !isZeroValue(programmaticConfig) {
		if err := l.copyConfig(programmaticConfig, target); err != nil {
			return err
		}

		if l.logger != nil {
			l.logger.Debug("using programmatic config",
				F("key", key),
			)
		}

		return nil
	}

	if defaults != nil {
		if err := l.copyConfig(defaults, target); err != nil {
			return err
		}

		if l.logger != nil {
			l.logger.Debug("using default config",
				F("key", key),
			)
		}

		return nil
	}

	if l.logger != nil {
		l.logger.Warn("no config found, target unchanged",
			F("key", key),
		)
	}

	return nil
}

// getConfigManager tries to get ConfigManager from the app's DI container.
func (l *ExtensionConfigLoader) getConfigManager() (ConfigManager, error) {
	container := l.app.Container()
	if container == nil {
		return nil, errors.New("no container available")
	}

	cm, err := Resolve[ConfigManager](container, ConfigKey)
	if err != nil {
		return nil, fmt.Errorf("ConfigManager not registered: %w", err)
	}

	return cm, nil
}

// tryBindConfig attempts to bind configuration from a specific key
// Returns (found, error).
func (l *ExtensionConfigLoader) tryBindConfig(cm ConfigManager, key string, target any) (bool, error) {
	// Check if key exists
	if !cm.IsSet(key) {
		return false, nil
	}

	// Try to bind
	if err := cm.Bind(key, target); err != nil {
		return false, err
	}

	return true, nil
}

// copyConfig copies config from source to target.
func (l *ExtensionConfigLoader) copyConfig(source, target any) error {
	sourceVal := reflect.ValueOf(source)
	targetVal := reflect.ValueOf(target)

	// Dereference pointers
	if sourceVal.Kind() == reflect.Ptr {
		sourceVal = sourceVal.Elem()
	}

	if targetVal.Kind() == reflect.Ptr {
		targetVal = targetVal.Elem()
	}

	if !targetVal.CanSet() {
		return errors.New("target is not settable")
	}

	// Simple copy for same types
	if sourceVal.Type() == targetVal.Type() {
		targetVal.Set(sourceVal)

		return nil
	}

	return fmt.Errorf("type mismatch: source=%v, target=%v", sourceVal.Type(), targetVal.Type())
}

// mergeConfig merges non-zero fields from source into target.
func (l *ExtensionConfigLoader) mergeConfig(source, target any) error {
	sourceVal := reflect.ValueOf(source)
	targetVal := reflect.ValueOf(target)

	// Dereference pointers
	if sourceVal.Kind() == reflect.Ptr {
		sourceVal = sourceVal.Elem()
	}

	if targetVal.Kind() == reflect.Ptr {
		targetVal = targetVal.Elem()
	}

	if !targetVal.CanSet() {
		return errors.New("target is not settable")
	}

	if sourceVal.Type() != targetVal.Type() {
		return fmt.Errorf("type mismatch: source=%v, target=%v", sourceVal.Type(), targetVal.Type())
	}

	// Merge fields
	for i := range sourceVal.NumField() {
		sourceField := sourceVal.Field(i)
		targetField := targetVal.Field(i)

		// Skip unexported fields
		if !targetField.CanSet() {
			continue
		}

		// Only override if source field is non-zero
		if !isZeroValue(sourceField.Interface()) {
			targetField.Set(sourceField)
		}
	}

	return nil
}

// isZeroValue checks if a value is the zero value for its type.
func isZeroValue(v any) bool {
	if v == nil {
		return true
	}

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return true
		}

		val = val.Elem()
	}

	return val.IsZero()
}

package forge

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/xraph/confy"
	"github.com/xraph/vessel"

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
// When the key is found, the result is defaults, then the file, then the
// fields of programmaticConfig that differ from defaults. So a value
// DefaultConfig() put into programmaticConfig doesn't hide the file, and an
// option like WithLimit(70) or WithEnabled(false) still beats it. Pass the
// same defaults the constructor started from: a field that differs from them
// wins, zero or not. An entirely zero programmaticConfig is ignored.
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
	if targetVal.Kind() != reflect.Pointer {
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

		// Options set in code win over the file; defaults don't.
		if programmaticConfig != nil && !isZeroValue(programmaticConfig) {
			if err := l.mergeConfig(programmaticConfig, defaults, target); err != nil {
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

		// Options set in code win over the file; defaults don't.
		if programmaticConfig != nil && !isZeroValue(programmaticConfig) {
			if err := l.mergeConfig(programmaticConfig, defaults, target); err != nil {
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

	cm, err := vessel.Inject[ConfigManager](container)
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

	// A section written with Go field names where the struct now has tags
	// (mcp, cache and security used to have none) still applies, with a
	// warning that names the new key.
	if section, ok := cm.Get(key).(map[string]any); ok {
		renamed, legacy := renameLegacyKeys(section, reflect.TypeOf(target), key+".")
		if len(legacy) > 0 {
			for _, k := range legacy {
				if l.logger != nil {
					l.logger.Warn("config key is deprecated, use the new name",
						F("key", k.old),
						F("use", k.new),
					)
				}
			}

			scratch := confy.New()
			scratch.Set("section", renamed)

			if err := scratch.Bind("section", target); err != nil {
				return false, err
			}

			return true, nil
		}
	}

	// Try to bind
	if err := cm.Bind(key, target); err != nil {
		return false, err
	}

	return true, nil
}

// legacyKey is a config key written as a Go field name for a field whose tag
// names a different key.
type legacyKey struct {
	old, new string
}

// renameLegacyKeys returns a copy of section with every key that matches a
// field's Go name moved to the key confy binds that field from, recursing
// into nested structs. When both spellings are present the tag's key wins.
// prefix is prepended to the reported key paths. It returns nil for legacy
// when section has no old keys, and never modifies section.
func renameLegacyKeys(section map[string]any, t reflect.Type, prefix string) (map[string]any, []legacyKey) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return section, nil
	}

	// A key that is some field's bound name is never treated as an old one.
	bound := make(map[string]bool, t.NumField())

	for f := range t.Fields() {
		if f.IsExported() {
			bound[configKeyName(f)] = true
		}
	}

	var (
		out    map[string]any
		legacy []legacyKey
	)

	clone := func() {
		if out == nil {
			out = maps.Clone(section)
		}
	}

	for f := range t.Fields() {
		if !f.IsExported() {
			continue
		}

		name := configKeyName(f)

		if name != f.Name && !bound[f.Name] {
			if v, ok := section[f.Name]; ok {
				clone()

				if _, both := section[name]; !both {
					out[name] = v
				}

				delete(out, f.Name)

				legacy = append(legacy, legacyKey{old: prefix + f.Name, new: prefix + name})
			}
		}

		current := section
		if out != nil {
			current = out
		}

		sub, ok := current[name].(map[string]any)
		if !ok {
			continue
		}

		renamedSub, subLegacy := renameLegacyKeys(sub, f.Type, prefix+name+".")
		if len(subLegacy) == 0 {
			continue
		}

		clone()

		out[name] = renamedSub

		legacy = append(legacy, subLegacy...)
	}

	if out == nil {
		return section, nil
	}

	return out, legacy
}

// configKeyName is the key confy binds a struct field from: the yaml tag,
// then the json tag, then the config tag, then the Go field name.
func configKeyName(f reflect.StructField) string {
	for _, tag := range []string{"yaml", "json", "config"} {
		if v := f.Tag.Get(tag); v != "" && v != "-" {
			return strings.Split(v, ",")[0]
		}
	}

	return f.Name
}

// copyConfig copies config from source to target.
func (l *ExtensionConfigLoader) copyConfig(source, target any) error {
	sourceVal := reflect.ValueOf(source)
	targetVal := reflect.ValueOf(target)

	// Dereference pointers
	if sourceVal.Kind() == reflect.Pointer {
		sourceVal = sourceVal.Elem()
	}

	if targetVal.Kind() == reflect.Pointer {
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

// mergeConfig copies the fields of source that were set in code onto target,
// which already holds the file config. A field counts as set when it differs
// from the same field in defaults, so a value that DefaultConfig() put there
// doesn't hide the file's value, and an option that sets a field to its zero
// value over a non-zero default still wins. Nested structs are compared field
// by field. With nil defaults every non-zero field counts.
//
// A field set in code to exactly its default value can't be told apart from
// one left alone, so the file wins for that field.
func (l *ExtensionConfigLoader) mergeConfig(source, defaults, target any) error {
	sourceVal := reflect.ValueOf(source)
	targetVal := reflect.ValueOf(target)

	// Dereference pointers
	if sourceVal.Kind() == reflect.Pointer {
		sourceVal = sourceVal.Elem()
	}

	if targetVal.Kind() == reflect.Pointer {
		targetVal = targetVal.Elem()
	}

	if !targetVal.CanSet() {
		return errors.New("target is not settable")
	}

	if sourceVal.Type() != targetVal.Type() {
		return fmt.Errorf("type mismatch: source=%v, target=%v", sourceVal.Type(), targetVal.Type())
	}

	var defaultsVal reflect.Value

	if defaults != nil {
		defaultsVal = reflect.ValueOf(defaults)
		if defaultsVal.Kind() == reflect.Pointer {
			defaultsVal = defaultsVal.Elem()
		}

		if defaultsVal.Type() != targetVal.Type() {
			return fmt.Errorf("type mismatch: defaults=%v, target=%v", defaultsVal.Type(), targetVal.Type())
		}
	}

	mergeSetFields(sourceVal, defaultsVal, targetVal)

	return nil
}

// mergeSetFields walks the exported fields of a struct and copies each one
// that source set onto target. A field counts as set when it differs from the
// same field in defaults, zero values included, so WithEnabled(false) over a
// true default beats the file. defaults may be the invalid Value, in which
// case every non-zero source field is copied.
func mergeSetFields(source, defaults, target reflect.Value) {
	for i := range source.NumField() {
		sourceField := source.Field(i)
		targetField := target.Field(i)

		// Skip unexported fields
		if !targetField.CanSet() {
			continue
		}

		if !defaults.IsValid() {
			if !sourceField.IsZero() {
				targetField.Set(sourceField)
			}

			continue
		}

		defaultField := defaults.Field(i)
		if reflect.DeepEqual(sourceField.Interface(), defaultField.Interface()) {
			continue
		}

		if sourceField.Kind() == reflect.Struct && hasExportedField(sourceField.Type()) {
			mergeSetFields(sourceField, defaultField, targetField)

			continue
		}

		targetField.Set(sourceField)
	}
}

// hasExportedField reports whether a struct type has a field mergeSetFields
// can set. Structs without one, such as time.Time, are copied whole.
func hasExportedField(t reflect.Type) bool {
	for f := range t.Fields() {
		if f.IsExported() {
			return true
		}
	}

	return false
}

// isZeroValue checks if a value is the zero value for its type.
func isZeroValue(v any) bool {
	if v == nil {
		return true
	}

	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return true
		}

		val = val.Elem()
	}

	return val.IsZero()
}

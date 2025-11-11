package config

import (
	errors2 "errors"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// ValidationMode defines the validation mode.
type ValidationMode string

const (
	ValidationModeStrict     ValidationMode = "strict"
	ValidationModeLoose      ValidationMode = "loose"
	ValidationModePermissive ValidationMode = "permissive"
	ValidationModeDisabled   ValidationMode = "disabled"
)

// Validator handles configuration validation.
type Validator struct {
	mode         ValidationMode
	rules        map[string][]ValidationRule
	schemas      map[string]ValidationSchema
	constraints  map[string][]ConstraintRule
	logger       logger.Logger
	errorHandler shared.ErrorHandler
	mu           sync.RWMutex
}

// ValidatorConfig contains configuration for the validator.
type ValidatorConfig struct {
	Mode           ValidationMode
	DefaultRules   []ValidationRule
	DefaultSchemas []ValidationSchema
	Logger         logger.Logger
	ErrorHandler   shared.ErrorHandler
	StrictMode     bool
	ValidateOnLoad bool
	ValidateOnSet  bool
}

// ValidationSchema defines a schema for validating configuration sections.
type ValidationSchema struct {
	Name        string                    `json:"name"`
	Path        string                    `json:"path"`
	Required    []string                  `json:"required"`
	Properties  map[string]PropertySchema `json:"properties"`
	Constraints []ConstraintRule          `json:"constraints"`
	Custom      []ValidationRule          `json:"-"`
}

// PropertySchema defines validation for a specific property.
type PropertySchema struct {
	Type        PropertyType              `json:"type"`
	Required    bool                      `json:"required"`
	Default     any                       `json:"default,omitempty"`
	Description string                    `json:"description,omitempty"`
	Format      string                    `json:"format,omitempty"`
	Pattern     string                    `json:"pattern,omitempty"`
	Enum        []any                     `json:"enum,omitempty"`
	Minimum     *float64                  `json:"minimum,omitempty"`
	Maximum     *float64                  `json:"maximum,omitempty"`
	MinLength   *int                      `json:"min_length,omitempty"`
	MaxLength   *int                      `json:"max_length,omitempty"`
	Items       *PropertySchema           `json:"items,omitempty"`
	Properties  map[string]PropertySchema `json:"properties,omitempty"`
	Constraints []ConstraintRule          `json:"constraints,omitempty"`
}

// PropertyType represents the type of a configuration property.
type PropertyType string

const (
	PropertyTypeString  PropertyType = "string"
	PropertyTypeInteger PropertyType = "integer"
	PropertyTypeNumber  PropertyType = "number"
	PropertyTypeBoolean PropertyType = "boolean"
	PropertyTypeArray   PropertyType = "array"
	PropertyTypeObject  PropertyType = "object"
	PropertyTypeAny     PropertyType = "any"
)

// ConstraintRule defines a constraint that must be satisfied.
type ConstraintRule interface {
	Name() string
	Description() string
	Validate(key string, value any, config map[string]any) error
	AppliesTo(key string) bool
}

// ValidationResult contains the result of validation.
type ValidationResult struct {
	Valid    bool                `json:"valid"`
	Errors   []ValidationError   `json:"errors,omitempty"`
	Warnings []ValidationWarning `json:"warnings,omitempty"`
	Schema   string              `json:"schema,omitempty"`
}

// Re-export error types from internal/errors.
type ValidationError = errors.ValidationError
type Severity = errors.Severity

const (
	SeverityError   = errors.SeverityError
	SeverityWarning = errors.SeverityWarning
	SeverityInfo    = errors.SeverityInfo
)

// ValidationWarning represents a validation warning.
type ValidationWarning struct {
	Key        string `json:"key"`
	Value      any    `json:"value,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// NewValidator creates a new configuration validator.
func NewValidator(config ValidatorConfig) *Validator {
	if config.Mode == "" {
		config.Mode = ValidationModePermissive
	}

	validator := &Validator{
		mode:         config.Mode,
		rules:        make(map[string][]ValidationRule),
		schemas:      make(map[string]ValidationSchema),
		constraints:  make(map[string][]ConstraintRule),
		logger:       config.Logger,
		errorHandler: config.ErrorHandler,
	}

	// Register default rules
	validator.registerDefaultRules()

	// Register default schemas
	validator.registerDefaultSchemas()

	// Add custom rules if provided
	for _, rule := range config.DefaultRules {
		validator.AddRule("*", rule)
	}

	// Add custom schemas if provided
	for _, schema := range config.DefaultSchemas {
		validator.RegisterSchema(schema)
	}

	return validator
}

// ValidateAll validates the entire configuration.
func (v *Validator) ValidateAll(config map[string]any) error {
	if v.mode == ValidationModeDisabled {
		return nil
	}

	result := v.ValidateConfig(config)

	if !result.Valid {
		if v.mode == ValidationModeStrict {
			return v.createValidationError(result.Errors)
		} else {
			// Log warnings in permissive mode
			v.logValidationIssues(result)
		}
	}

	return nil
}

// ValidateConfig validates a configuration map.
func (v *Validator) ValidateConfig(config map[string]any) ValidationResult {
	v.mu.RLock()
	defer v.mu.RUnlock()

	result := ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Validate against registered schemas
	for _, schema := range v.schemas {
		schemaResult := v.validateSchema(config, schema)
		result.Errors = append(result.Errors, schemaResult.Errors...)

		result.Warnings = append(result.Warnings, schemaResult.Warnings...)
		if !schemaResult.Valid {
			result.Valid = false
		}
	}

	// Validate using registered rules
	for key, value := range config {
		keyResult := v.validateKey(key, value, config)
		result.Errors = append(result.Errors, keyResult.Errors...)

		result.Warnings = append(result.Warnings, keyResult.Warnings...)
		if !keyResult.Valid {
			result.Valid = false
		}
	}

	return result
}

// ValidateKey validates a specific configuration key.
func (v *Validator) ValidateKey(key string, value any, config map[string]any) error {
	if v.mode == ValidationModeDisabled {
		return nil
	}

	result := v.validateKey(key, value, config)

	if !result.Valid && v.mode == ValidationModeStrict {
		return v.createValidationError(result.Errors)
	}

	return nil
}

// validateKey validates a specific key-value pair.
func (v *Validator) validateKey(key string, value any, config map[string]any) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Apply rules for this key
	rules := v.getRulesForKey(key)
	for _, rule := range rules {
		if rule.AppliesTo(key) {
			if err := rule.Validate(key, value); err != nil {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      key,
					Value:    value,
					Rule:     rule.Name(),
					Message:  err.Error(),
					Severity: SeverityError,
				})
			}
		}
	}

	// Apply constraints for this key
	constraints := v.getConstraintsForKey(key)
	for _, constraint := range constraints {
		if constraint.AppliesTo(key) {
			if err := constraint.Validate(key, value, config); err != nil {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      key,
					Value:    value,
					Rule:     constraint.Name(),
					Message:  err.Error(),
					Severity: SeverityError,
				})
			}
		}
	}

	return result
}

// validateSchema validates configuration against a schema.
func (v *Validator) validateSchema(config map[string]any, schema ValidationSchema) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
		Schema:   schema.Name,
	}

	// Get the configuration section for this schema
	var sectionData any
	if schema.Path == "" {
		sectionData = config
	} else {
		sectionData = v.getNestedValue(config, schema.Path)
	}

	if sectionData == nil {
		// Check if any required properties are missing
		if len(schema.Required) > 0 {
			for _, required := range schema.Required {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      fmt.Sprintf("%s.%s", schema.Path, required),
					Rule:     "required",
					Message:  fmt.Sprintf("required property '%s' is missing", required),
					Severity: SeverityError,
				})
			}
		}

		return result
	}

	sectionMap, ok := sectionData.(map[string]any)
	if !ok {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Key:      schema.Path,
			Value:    sectionData,
			Rule:     "type",
			Message:  "expected object type",
			Severity: SeverityError,
		})

		return result
	}

	// Validate required properties
	for _, required := range schema.Required {
		if _, exists := sectionMap[required]; !exists {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Key:      fmt.Sprintf("%s.%s", schema.Path, required),
				Rule:     "required",
				Message:  fmt.Sprintf("required property '%s' is missing", required),
				Severity: SeverityError,
			})
		}
	}

	// Validate properties against their schemas
	for propName, propSchema := range schema.Properties {
		if value, exists := sectionMap[propName]; exists {
			propResult := v.validateProperty(fmt.Sprintf("%s.%s", schema.Path, propName), value, propSchema)
			result.Errors = append(result.Errors, propResult.Errors...)

			result.Warnings = append(result.Warnings, propResult.Warnings...)
			if !propResult.Valid {
				result.Valid = false
			}
		} else if propSchema.Required {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Key:      fmt.Sprintf("%s.%s", schema.Path, propName),
				Rule:     "required",
				Message:  fmt.Sprintf("required property '%s' is missing", propName),
				Severity: SeverityError,
			})
		}
	}

	// Apply schema-level constraints
	for _, constraint := range schema.Constraints {
		if err := constraint.Validate(schema.Path, sectionData, config); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Key:      schema.Path,
				Value:    sectionData,
				Rule:     constraint.Name(),
				Message:  err.Error(),
				Severity: SeverityError,
			})
		}
	}

	return result
}

// validateProperty validates a property against its schema.
func (v *Validator) validateProperty(key string, value any, schema PropertySchema) ValidationResult {
	result := ValidationResult{
		Valid:    true,
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	// Validate type
	if !v.validateType(value, schema.Type) {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Key:      key,
			Value:    value,
			Rule:     "type",
			Message:  fmt.Sprintf("expected type %s, got %T", schema.Type, value),
			Severity: SeverityError,
		})

		return result // Don't continue if type is wrong
	}

	// Validate format
	if schema.Format != "" {
		if err := v.validateFormat(value, schema.Format); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Key:      key,
				Value:    value,
				Rule:     "format",
				Message:  err.Error(),
				Severity: SeverityError,
			})
		}
	}

	// Validate pattern (for strings)
	if schema.Pattern != "" && schema.Type == PropertyTypeString {
		if str, ok := value.(string); ok {
			if matched, _ := regexp.MatchString(schema.Pattern, str); !matched {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      key,
					Value:    value,
					Rule:     "pattern",
					Message:  "value does not match pattern: " + schema.Pattern,
					Severity: SeverityError,
				})
			}
		}
	}

	// Validate enum
	if len(schema.Enum) > 0 {
		if !v.isInEnum(value, schema.Enum) {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Key:      key,
				Value:    value,
				Rule:     "enum",
				Message:  fmt.Sprintf("value must be one of: %v", schema.Enum),
				Severity: SeverityError,
			})
		}
	}

	// Validate numeric constraints
	if schema.Type == PropertyTypeNumber || schema.Type == PropertyTypeInteger {
		if num, ok := v.toFloat64(value); ok {
			if schema.Minimum != nil && num < *schema.Minimum {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      key,
					Value:    value,
					Rule:     "minimum",
					Message:  fmt.Sprintf("value %v is less than minimum %v", num, *schema.Minimum),
					Severity: SeverityError,
				})
			}

			if schema.Maximum != nil && num > *schema.Maximum {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      key,
					Value:    value,
					Rule:     "maximum",
					Message:  fmt.Sprintf("value %v is greater than maximum %v", num, *schema.Maximum),
					Severity: SeverityError,
				})
			}
		}
	}

	// Validate string length
	if schema.Type == PropertyTypeString {
		if str, ok := value.(string); ok {
			length := len(str)
			if schema.MinLength != nil && length < *schema.MinLength {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      key,
					Value:    value,
					Rule:     "min_length",
					Message:  fmt.Sprintf("string length %d is less than minimum %d", length, *schema.MinLength),
					Severity: SeverityError,
				})
			}

			if schema.MaxLength != nil && length > *schema.MaxLength {
				result.Valid = false
				result.Errors = append(result.Errors, ValidationError{
					Key:      key,
					Value:    value,
					Rule:     "max_length",
					Message:  fmt.Sprintf("string length %d is greater than maximum %d", length, *schema.MaxLength),
					Severity: SeverityError,
				})
			}
		}
	}

	// Validate array items
	if schema.Type == PropertyTypeArray && schema.Items != nil {
		if arr, ok := value.([]any); ok {
			for i, item := range arr {
				itemKey := fmt.Sprintf("%s[%d]", key, i)
				itemResult := v.validateProperty(itemKey, item, *schema.Items)
				result.Errors = append(result.Errors, itemResult.Errors...)

				result.Warnings = append(result.Warnings, itemResult.Warnings...)
				if !itemResult.Valid {
					result.Valid = false
				}
			}
		}
	}

	// Validate object properties
	if schema.Type == PropertyTypeObject && len(schema.Properties) > 0 {
		if obj, ok := value.(map[string]any); ok {
			for propName, propSchema := range schema.Properties {
				if propValue, exists := obj[propName]; exists {
					propKey := fmt.Sprintf("%s.%s", key, propName)
					propResult := v.validateProperty(propKey, propValue, propSchema)
					result.Errors = append(result.Errors, propResult.Errors...)

					result.Warnings = append(result.Warnings, propResult.Warnings...)
					if !propResult.Valid {
						result.Valid = false
					}
				} else if propSchema.Required {
					result.Valid = false
					result.Errors = append(result.Errors, ValidationError{
						Key:      fmt.Sprintf("%s.%s", key, propName),
						Rule:     "required",
						Message:  fmt.Sprintf("required property '%s' is missing", propName),
						Severity: SeverityError,
					})
				}
			}
		}
	}

	// Apply property-level constraints
	for _, constraint := range schema.Constraints {
		if err := constraint.Validate(key, value, nil); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Key:      key,
				Value:    value,
				Rule:     constraint.Name(),
				Message:  err.Error(),
				Severity: SeverityError,
			})
		}
	}

	return result
}

// AddRule adds a validation rule for a specific key pattern.
func (v *Validator) AddRule(keyPattern string, rule ValidationRule) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.rules[keyPattern] == nil {
		v.rules[keyPattern] = make([]ValidationRule, 0)
	}

	v.rules[keyPattern] = append(v.rules[keyPattern], rule)

	if v.logger != nil {
		v.logger.Debug("validation rule added",
			logger.String("pattern", keyPattern),
			logger.String("rule", rule.Name()),
		)
	}
}

// AddConstraint adds a constraint rule for a specific key pattern.
func (v *Validator) AddConstraint(keyPattern string, constraint ConstraintRule) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.constraints[keyPattern] == nil {
		v.constraints[keyPattern] = make([]ConstraintRule, 0)
	}

	v.constraints[keyPattern] = append(v.constraints[keyPattern], constraint)

	if v.logger != nil {
		v.logger.Debug("validation constraint added",
			logger.String("pattern", keyPattern),
			logger.String("constraint", constraint.Name()),
		)
	}
}

// RegisterSchema registers a validation schema.
func (v *Validator) RegisterSchema(schema ValidationSchema) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.schemas[schema.Name] = schema

	if v.logger != nil {
		v.logger.Debug("validation schema registered",
			logger.String("schema", schema.Name),
			logger.String("path", schema.Path),
		)
	}
}

// IsStrictMode returns true if the validator is in strict mode.
func (v *Validator) IsStrictMode() bool {
	return v.mode == ValidationModeStrict
}

// SetMode sets the validation mode.
func (v *Validator) SetMode(mode ValidationMode) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.mode = mode
}

// Helper methods

func (v *Validator) getRulesForKey(key string) []ValidationRule {
	var rules []ValidationRule

	for pattern, patternRules := range v.rules {
		if v.matchesPattern(key, pattern) {
			rules = append(rules, patternRules...)
		}
	}

	return rules
}

func (v *Validator) getConstraintsForKey(key string) []ConstraintRule {
	var constraints []ConstraintRule

	for pattern, patternConstraints := range v.constraints {
		if v.matchesPattern(key, pattern) {
			constraints = append(constraints, patternConstraints...)
		}
	}

	return constraints
}

func (v *Validator) matchesPattern(key, pattern string) bool {
	if pattern == "*" {
		return true
	}

	// Simple glob-style matching
	matched, _ := regexp.MatchString(strings.ReplaceAll(pattern, "*", ".*"), key)

	return matched
}

func (v *Validator) getNestedValue(config map[string]any, path string) any {
	if path == "" {
		return config
	}

	keys := strings.Split(path, ".")
	current := any(config)

	for _, key := range keys {
		if current == nil {
			return nil
		}

		if m, ok := current.(map[string]any); ok {
			current = m[key]
		} else {
			return nil
		}
	}

	return current
}

func (v *Validator) validateType(value any, expectedType PropertyType) bool {
	switch expectedType {
	case PropertyTypeString:
		_, ok := value.(string)

		return ok
	case PropertyTypeInteger:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			return true
		}

		return false
	case PropertyTypeNumber:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return true
		}

		return false
	case PropertyTypeBoolean:
		_, ok := value.(bool)

		return ok
	case PropertyTypeArray:
		_, ok := value.([]any)

		return ok
	case PropertyTypeObject:
		_, ok := value.(map[string]any)

		return ok
	case PropertyTypeAny:
		return true
	}

	return false
}

func (v *Validator) validateFormat(value any, format string) error {
	str, ok := value.(string)
	if !ok {
		return errors.New("format validation only applies to strings")
	}

	switch format {
	case "email":
		emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
		if !emailRegex.MatchString(str) {
			return errors.New("invalid email format")
		}
	case "uri", "url":
		// Simple URL validation
		if !strings.HasPrefix(str, "http://") && !strings.HasPrefix(str, "https://") {
			return errors.New("invalid URL format")
		}
	case "date":
		if _, err := time.Parse("2006-01-02", str); err != nil {
			return errors.New("invalid date format, expected YYYY-MM-DD")
		}
	case "time":
		if _, err := time.Parse("15:04:05", str); err != nil {
			return errors.New("invalid time format, expected HH:MM:SS")
		}
	case "datetime":
		if _, err := time.Parse(time.RFC3339, str); err != nil {
			return errors.New("invalid datetime format, expected RFC3339")
		}
	case "duration":
		if _, err := time.ParseDuration(str); err != nil {
			return errors.New("invalid duration format")
		}
	}

	return nil
}

func (v *Validator) isInEnum(value any, enum []any) bool {
	for _, item := range enum {
		if reflect.DeepEqual(value, item) {
			return true
		}
	}

	return false
}

func (v *Validator) toFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f, true
		}
	}

	return 0, false
}

func (v *Validator) createValidationError(errors []ValidationError) error {
	if len(errors) == 0 {
		return nil
	}

	var messages []string
	for _, err := range errors {
		messages = append(messages, fmt.Sprintf("%s: %s", err.Key, err.Message))
	}

	return ErrValidationError("configuration", errors2.New(strings.Join(messages, "; ")))
}

func (v *Validator) logValidationIssues(result ValidationResult) {
	if v.logger == nil {
		return
	}

	for _, err := range result.Errors {
		v.logger.Error("configuration validation error",
			logger.String("key", err.Key),
			logger.String("rule", err.Rule),
			logger.String("message", err.Message),
		)
	}

	for _, warning := range result.Warnings {
		v.logger.Warn("configuration validation warning",
			logger.String("key", warning.Key),
			logger.String("message", warning.Message),
		)
	}
}

func (v *Validator) registerDefaultRules() {
	// Register common validation rules
	v.AddRule("*.port", &PortRule{})
	v.AddRule("*.email", &EmailRule{})
	v.AddRule("*.url", &URLRule{})
	v.AddRule("*.duration", &DurationRule{})
}

func (v *Validator) registerDefaultSchemas() {
	// Register common configuration schemas
	// These would be implemented as needed
}

// Built-in validation rules

type PortRule struct{}

func (r *PortRule) Name() string              { return "port" }
func (r *PortRule) AppliesTo(key string) bool { return strings.Contains(key, "port") }
func (r *PortRule) Validate(key string, value any) error {
	if port, ok := value.(int); ok {
		if port < 1 || port > 65535 {
			return errors.New("port must be between 1 and 65535")
		}
	}

	return nil
}

type EmailRule struct{}

func (r *EmailRule) Name() string              { return "email" }
func (r *EmailRule) AppliesTo(key string) bool { return strings.Contains(key, "email") }
func (r *EmailRule) Validate(key string, value any) error {
	if email, ok := value.(string); ok {
		emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
		if !emailRegex.MatchString(email) {
			return errors.New("invalid email format")
		}
	}

	return nil
}

type URLRule struct{}

func (r *URLRule) Name() string              { return "url" }
func (r *URLRule) AppliesTo(key string) bool { return strings.Contains(key, "url") }
func (r *URLRule) Validate(key string, value any) error {
	if url, ok := value.(string); ok {
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			return errors.New("URL must start with http:// or https://")
		}
	}

	return nil
}

type DurationRule struct{}

func (r *DurationRule) Name() string { return "duration" }
func (r *DurationRule) AppliesTo(key string) bool {
	return strings.Contains(key, "duration") || strings.Contains(key, "timeout")
}
func (r *DurationRule) Validate(key string, value any) error {
	if duration, ok := value.(string); ok {
		if _, err := time.ParseDuration(duration); err != nil {
			return fmt.Errorf("invalid duration format: %s", err.Error())
		}
	}

	return nil
}

package schema

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	eventscore "github.com/xraph/forge/v0/pkg/events/core"
	"github.com/xraph/forge/v0/pkg/logger"
)

// EventSchemaValidator validates events against registered schemas
type EventSchemaValidator struct {
	registry   *SchemaRegistry
	validators map[string]Validator
	config     *ValidationConfig
	logger     common.Logger
	metrics    common.Metrics
	mu         sync.RWMutex
}

// ValidationConfig defines configuration for event validation
type ValidationConfig struct {
	StrictMode       bool     `yaml:"strict_mode" json:"strict_mode"`
	RequiredFields   []string `yaml:"required_fields" json:"required_fields"`
	AllowExtraFields bool     `yaml:"allow_extra_fields" json:"allow_extra_fields"`
	ValidateMetadata bool     `yaml:"validate_metadata" json:"validate_metadata"`
	MaxDepth         int      `yaml:"max_depth" json:"max_depth"`
	MaxSize          int      `yaml:"max_size" json:"max_size"`
}

// Validator defines the interface for validating data
type Validator interface {
	// Validate validates data against the schema
	Validate(data interface{}) error

	// GetSchema returns the schema
	GetSchema() *Schema

	// GetType returns the validator type
	GetType() string
}

// Schema represents an event schema
type Schema struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Version     int                    `json:"version"`
	Type        string                 `json:"type"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Properties  map[string]*Property   `json:"properties"`
	Required    []string               `json:"required"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

// Property represents a schema property
type Property struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Format      string                 `json:"format,omitempty"`
	Pattern     string                 `json:"pattern,omitempty"`
	Enum        []interface{}          `json:"enum,omitempty"`
	Minimum     *float64               `json:"minimum,omitempty"`
	Maximum     *float64               `json:"maximum,omitempty"`
	MinLength   *int                   `json:"min_length,omitempty"`
	MaxLength   *int                   `json:"max_length,omitempty"`
	Items       *Property              `json:"items,omitempty"`
	Properties  map[string]*Property   `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
	Default     interface{}            `json:"default,omitempty"`
	Examples    []interface{}          `json:"examples,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// ValidationError represents a validation error
type ValidationError struct {
	Field    string      `json:"field"`
	Value    interface{} `json:"value"`
	Expected string      `json:"expected"`
	Message  string      `json:"message"`
	Code     string      `json:"code"`
}

// Error implements error interface
func (ve *ValidationError) Error() string {
	return fmt.Sprintf("validation error in field '%s': %s", ve.Field, ve.Message)
}

// ValidationResult contains validation results
type ValidationResult struct {
	Valid   bool               `json:"valid"`
	Errors  []*ValidationError `json:"errors"`
	Schema  *Schema            `json:"schema"`
	EventID string             `json:"event_id"`
}

// NewEventSchemaValidator creates a new event schema validator
func NewEventSchemaValidator(registry *SchemaRegistry, config *ValidationConfig, logger common.Logger, metrics common.Metrics) *EventSchemaValidator {
	if config == nil {
		config = DefaultValidationConfig()
	}

	return &EventSchemaValidator{
		registry:   registry,
		validators: make(map[string]Validator),
		config:     config,
		logger:     logger,
		metrics:    metrics,
	}
}

// DefaultValidationConfig returns default validation configuration
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		StrictMode:       false,
		RequiredFields:   []string{"id", "type", "aggregate_id", "timestamp"},
		AllowExtraFields: true,
		ValidateMetadata: false,
		MaxDepth:         10,
		MaxSize:          1024 * 1024, // 1MB
	}
}

// ValidateEvent validates an event against its schema
func (esv *EventSchemaValidator) ValidateEvent(event *eventscore.Event) *ValidationResult {
	start := time.Now()

	result := &ValidationResult{
		Valid:   true,
		Errors:  make([]*ValidationError, 0),
		EventID: event.ID,
	}

	// Get schema for event type
	schema, err := esv.registry.GetSchema(event.Type, event.Version)
	if err != nil {
		if !esv.config.StrictMode {
			// In non-strict mode, allow events without schemas
			if esv.logger != nil {
				esv.logger.Debug("no schema found for event type",
					logger.String("event_type", event.Type),
					logger.Int("version", event.Version),
				)
			}
			return result
		}

		result.Valid = false
		result.Errors = append(result.Errors, &ValidationError{
			Field:    "type",
			Value:    event.Type,
			Expected: "registered schema",
			Message:  fmt.Sprintf("no schema found for event type '%s' version %d", event.Type, event.Version),
			Code:     "SCHEMA_NOT_FOUND",
		})
		return result
	}

	result.Schema = schema

	// Validate required fields
	if err := esv.validateRequiredFields(event); err != nil {
		result.Valid = false
		if validationErr, ok := err.(*ValidationError); ok {
			result.Errors = append(result.Errors, validationErr)
		} else {
			result.Errors = append(result.Errors, &ValidationError{
				Field:   "event",
				Message: err.Error(),
				Code:    "REQUIRED_FIELD_MISSING",
			})
		}
	}

	// Get validator for this schema
	validator, err := esv.getValidator(schema)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, &ValidationError{
			Field:   "schema",
			Message: fmt.Sprintf("failed to get validator: %v", err),
			Code:    "VALIDATOR_ERROR",
		})
		return result
	}

	// Validate event data
	if err := validator.Validate(event.Data); err != nil {
		result.Valid = false
		if validationErr, ok := err.(*ValidationError); ok {
			result.Errors = append(result.Errors, validationErr)
		} else {
			result.Errors = append(result.Errors, &ValidationError{
				Field:   "data",
				Message: err.Error(),
				Code:    "DATA_VALIDATION_FAILED",
			})
		}
	}

	// Validate metadata if enabled
	if esv.config.ValidateMetadata && event.Metadata != nil {
		if err := esv.validateMetadata(event.Metadata, schema); err != nil {
			result.Valid = false
			if validationErr, ok := err.(*ValidationError); ok {
				result.Errors = append(result.Errors, validationErr)
			} else {
				result.Errors = append(result.Errors, &ValidationError{
					Field:   "metadata",
					Message: err.Error(),
					Code:    "METADATA_VALIDATION_FAILED",
				})
			}
		}
	}

	// Record metrics
	duration := time.Since(start)
	if esv.metrics != nil {
		esv.metrics.Histogram("forge.events.validation_duration", "event_type", event.Type).Observe(duration.Seconds())
		esv.metrics.Counter("forge.events.validations_total", "event_type", event.Type).Inc()

		if result.Valid {
			esv.metrics.Counter("forge.events.validations_success", "event_type", event.Type).Inc()
		} else {
			esv.metrics.Counter("forge.events.validations_failed", "event_type", event.Type).Inc()
			esv.metrics.Counter("forge.events.validation_errors", "event_type", event.Type).Add(float64(len(result.Errors)))
		}
	}

	if esv.logger != nil {
		if result.Valid {
			esv.logger.Debug("event validation successful",
				logger.String("event_id", event.ID),
				logger.String("event_type", event.Type),
				logger.Duration("duration", duration),
			)
		} else {
			esv.logger.Warn("event validation failed",
				logger.String("event_id", event.ID),
				logger.String("event_type", event.Type),
				logger.Int("error_count", len(result.Errors)),
				logger.Duration("duration", duration),
			)
		}
	}

	return result
}

// validateRequiredFields validates that required fields are present
func (esv *EventSchemaValidator) validateRequiredFields(event *eventscore.Event) error {
	for _, field := range esv.config.RequiredFields {
		switch field {
		case "id":
			if event.ID == "" {
				return &ValidationError{
					Field:    "id",
					Value:    event.ID,
					Expected: "non-empty string",
					Message:  "event ID is required",
					Code:     "REQUIRED_FIELD_MISSING",
				}
			}
		case "type":
			if event.Type == "" {
				return &ValidationError{
					Field:    "type",
					Value:    event.Type,
					Expected: "non-empty string",
					Message:  "event type is required",
					Code:     "REQUIRED_FIELD_MISSING",
				}
			}
		case "aggregate_id":
			if event.AggregateID == "" {
				return &ValidationError{
					Field:    "aggregate_id",
					Value:    event.AggregateID,
					Expected: "non-empty string",
					Message:  "aggregate ID is required",
					Code:     "REQUIRED_FIELD_MISSING",
				}
			}
		case "timestamp":
			if event.Timestamp.IsZero() {
				return &ValidationError{
					Field:    "timestamp",
					Value:    event.Timestamp,
					Expected: "valid timestamp",
					Message:  "timestamp is required",
					Code:     "REQUIRED_FIELD_MISSING",
				}
			}
		case "data":
			if event.Data == nil {
				return &ValidationError{
					Field:    "data",
					Value:    event.Data,
					Expected: "non-nil value",
					Message:  "event data is required",
					Code:     "REQUIRED_FIELD_MISSING",
				}
			}
		}
	}

	return nil
}

// validateMetadata validates event metadata
func (esv *EventSchemaValidator) validateMetadata(metadata map[string]interface{}, schema *Schema) error {
	// Basic metadata validation
	if len(metadata) == 0 {
		return nil
	}

	// Check for required metadata fields if defined in schema
	if schema.Metadata != nil {
		if requiredMetadata, ok := schema.Metadata["required"].([]interface{}); ok {
			for _, required := range requiredMetadata {
				if requiredStr, ok := required.(string); ok {
					if _, exists := metadata[requiredStr]; !exists {
						return &ValidationError{
							Field:    fmt.Sprintf("metadata.%s", requiredStr),
							Expected: "value",
							Message:  fmt.Sprintf("required metadata field '%s' is missing", requiredStr),
							Code:     "REQUIRED_METADATA_MISSING",
						}
					}
				}
			}
		}
	}

	return nil
}

// getValidator gets or creates a validator for a schema
func (esv *EventSchemaValidator) getValidator(schema *Schema) (Validator, error) {
	esv.mu.RLock()
	validator, exists := esv.validators[schema.ID]
	esv.mu.RUnlock()

	if exists {
		return validator, nil
	}

	esv.mu.Lock()
	defer esv.mu.Unlock()

	// Double-check after acquiring write lock
	if validator, exists := esv.validators[schema.ID]; exists {
		return validator, nil
	}

	// Create new validator based on schema type
	var newValidator Validator
	var err error

	switch schema.Type {
	case "json-schema":
		newValidator, err = NewJSONSchemaValidator(schema)
	case "struct":
		newValidator, err = NewStructValidator(schema)
	case "regex":
		newValidator, err = NewRegexValidator(schema)
	default:
		return nil, fmt.Errorf("unsupported schema type: %s", schema.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create validator: %w", err)
	}

	esv.validators[schema.ID] = newValidator
	return newValidator, nil
}

// AddValidator adds a custom validator
func (esv *EventSchemaValidator) AddValidator(schemaID string, validator Validator) {
	esv.mu.Lock()
	defer esv.mu.Unlock()
	esv.validators[schemaID] = validator
}

// RemoveValidator removes a validator
func (esv *EventSchemaValidator) RemoveValidator(schemaID string) {
	esv.mu.Lock()
	defer esv.mu.Unlock()
	delete(esv.validators, schemaID)
}

// GetStats returns validation statistics
func (esv *EventSchemaValidator) GetStats() map[string]interface{} {
	esv.mu.RLock()
	defer esv.mu.RUnlock()

	return map[string]interface{}{
		"validators_count": len(esv.validators),
		"strict_mode":      esv.config.StrictMode,
		"required_fields":  esv.config.RequiredFields,
		"max_depth":        esv.config.MaxDepth,
		"max_size":         esv.config.MaxSize,
	}
}

// JSONSchemaValidator validates against JSON Schema
type JSONSchemaValidator struct {
	schema *Schema
	config *ValidationConfig
}

// NewJSONSchemaValidator creates a new JSON schema validator
func NewJSONSchemaValidator(schema *Schema) (*JSONSchemaValidator, error) {
	return &JSONSchemaValidator{
		schema: schema,
		config: DefaultValidationConfig(),
	}, nil
}

// Validate implements Validator
func (jsv *JSONSchemaValidator) Validate(data interface{}) error {
	return jsv.validateValue(data, jsv.schema.Properties, "", 0)
}

// validateValue validates a value against properties
func (jsv *JSONSchemaValidator) validateValue(value interface{}, properties map[string]*Property, path string, depth int) error {
	if depth > jsv.config.MaxDepth {
		return &ValidationError{
			Field:   path,
			Message: fmt.Sprintf("maximum validation depth (%d) exceeded", jsv.config.MaxDepth),
			Code:    "MAX_DEPTH_EXCEEDED",
		}
	}

	// Convert value to map for validation
	dataMap, ok := value.(map[string]interface{})
	if !ok {
		// Try to convert via JSON marshaling/unmarshaling
		jsonData, err := json.Marshal(value)
		if err != nil {
			return &ValidationError{
				Field:   path,
				Value:   value,
				Message: "failed to convert data to map",
				Code:    "TYPE_CONVERSION_FAILED",
			}
		}

		if err := json.Unmarshal(jsonData, &dataMap); err != nil {
			return &ValidationError{
				Field:   path,
				Value:   value,
				Message: "failed to unmarshal data to map",
				Code:    "TYPE_CONVERSION_FAILED",
			}
		}
	}

	// Validate required properties
	for _, required := range jsv.schema.Required {
		if _, exists := dataMap[required]; !exists {
			return &ValidationError{
				Field:    fmt.Sprintf("%s.%s", path, required),
				Expected: "value",
				Message:  fmt.Sprintf("required property '%s' is missing", required),
				Code:     "REQUIRED_PROPERTY_MISSING",
			}
		}
	}

	// Validate each property
	for propName, propValue := range dataMap {
		propPath := propName
		if path != "" {
			propPath = fmt.Sprintf("%s.%s", path, propName)
		}

		property, exists := properties[propName]
		if !exists {
			if !jsv.config.AllowExtraFields {
				return &ValidationError{
					Field:   propPath,
					Value:   propValue,
					Message: fmt.Sprintf("unexpected property '%s'", propName),
					Code:    "UNEXPECTED_PROPERTY",
				}
			}
			continue
		}

		if err := jsv.validateProperty(propValue, property, propPath, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// validateProperty validates a single property
func (jsv *JSONSchemaValidator) validateProperty(value interface{}, property *Property, path string, depth int) error {
	// Validate type
	if err := jsv.validateType(value, property.Type, path); err != nil {
		return err
	}

	// Validate enum
	if len(property.Enum) > 0 {
		found := false
		for _, enumValue := range property.Enum {
			if value == enumValue {
				found = true
				break
			}
		}
		if !found {
			return &ValidationError{
				Field:    path,
				Value:    value,
				Expected: fmt.Sprintf("one of %v", property.Enum),
				Message:  fmt.Sprintf("value must be one of %v", property.Enum),
				Code:     "ENUM_VALIDATION_FAILED",
			}
		}
	}

	// Validate string properties
	if property.Type == "string" {
		if str, ok := value.(string); ok {
			if property.MinLength != nil && len(str) < *property.MinLength {
				return &ValidationError{
					Field:    path,
					Value:    value,
					Expected: fmt.Sprintf("minimum length %d", *property.MinLength),
					Message:  fmt.Sprintf("string length %d is less than minimum %d", len(str), *property.MinLength),
					Code:     "MIN_LENGTH_VALIDATION_FAILED",
				}
			}
			if property.MaxLength != nil && len(str) > *property.MaxLength {
				return &ValidationError{
					Field:    path,
					Value:    value,
					Expected: fmt.Sprintf("maximum length %d", *property.MaxLength),
					Message:  fmt.Sprintf("string length %d exceeds maximum %d", len(str), *property.MaxLength),
					Code:     "MAX_LENGTH_VALIDATION_FAILED",
				}
			}
			if property.Pattern != "" {
				// Pattern validation would go here
			}
		}
	}

	// Validate number properties
	if property.Type == "number" || property.Type == "integer" {
		if num, ok := value.(float64); ok {
			if property.Minimum != nil && num < *property.Minimum {
				return &ValidationError{
					Field:    path,
					Value:    value,
					Expected: fmt.Sprintf("minimum %f", *property.Minimum),
					Message:  fmt.Sprintf("value %f is less than minimum %f", num, *property.Minimum),
					Code:     "MIN_VALUE_VALIDATION_FAILED",
				}
			}
			if property.Maximum != nil && num > *property.Maximum {
				return &ValidationError{
					Field:    path,
					Value:    value,
					Expected: fmt.Sprintf("maximum %f", *property.Maximum),
					Message:  fmt.Sprintf("value %f exceeds maximum %f", num, *property.Maximum),
					Code:     "MAX_VALUE_VALIDATION_FAILED",
				}
			}
		}
	}

	// Validate array properties
	if property.Type == "array" {
		if arr, ok := value.([]interface{}); ok {
			if property.Items != nil {
				for i, item := range arr {
					itemPath := fmt.Sprintf("%s[%d]", path, i)
					if err := jsv.validateProperty(item, property.Items, itemPath, depth+1); err != nil {
						return err
					}
				}
			}
		}
	}

	// Validate object properties
	if property.Type == "object" && property.Properties != nil {
		return jsv.validateValue(value, property.Properties, path, depth+1)
	}

	return nil
}

// validateType validates the type of a value
func (jsv *JSONSchemaValidator) validateType(value interface{}, expectedType string, path string) error {
	actualType := jsv.getJSONType(value)

	if actualType != expectedType {
		return &ValidationError{
			Field:    path,
			Value:    value,
			Expected: expectedType,
			Message:  fmt.Sprintf("expected type %s, got %s", expectedType, actualType),
			Code:     "TYPE_VALIDATION_FAILED",
		}
	}

	return nil
}

// getJSONType returns the JSON type of a value
func (jsv *JSONSchemaValidator) getJSONType(value interface{}) string {
	if value == nil {
		return "null"
	}

	switch value.(type) {
	case bool:
		return "boolean"
	case float64, int, int32, int64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		return "unknown"
	}
}

// GetSchema implements Validator
func (jsv *JSONSchemaValidator) GetSchema() *Schema {
	return jsv.schema
}

// GetType implements Validator
func (jsv *JSONSchemaValidator) GetType() string {
	return "json-schema"
}

// StructValidator validates against Go struct definitions
type StructValidator struct {
	schema     *Schema
	structType reflect.Type
}

// NewStructValidator creates a new struct validator
func NewStructValidator(schema *Schema) (*StructValidator, error) {
	return &StructValidator{
		schema: schema,
	}, nil
}

// Validate implements Validator
func (sv *StructValidator) Validate(data interface{}) error {
	// This would implement struct-based validation
	// For now, return nil (not implemented)
	return nil
}

// GetSchema implements Validator
func (sv *StructValidator) GetSchema() *Schema {
	return sv.schema
}

// GetType implements Validator
func (sv *StructValidator) GetType() string {
	return "struct"
}

// RegexValidator validates using regular expressions
type RegexValidator struct {
	schema *Schema
}

// NewRegexValidator creates a new regex validator
func NewRegexValidator(schema *Schema) (*RegexValidator, error) {
	return &RegexValidator{
		schema: schema,
	}, nil
}

// Validate implements Validator
func (rv *RegexValidator) Validate(data interface{}) error {
	// This would implement regex-based validation
	// For now, return nil (not implemented)
	return nil
}

// GetSchema implements Validator
func (rv *RegexValidator) GetSchema() *Schema {
	return rv.schema
}

// GetType implements Validator
func (rv *RegexValidator) GetType() string {
	return "regex"
}

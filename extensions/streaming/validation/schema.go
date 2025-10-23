package validation

import (
	"context"
	"encoding/json"
	"fmt"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// Schema represents a JSON schema for validation.
type Schema struct {
	Type       string            `json:"type"`
	Properties map[string]Schema `json:"properties,omitempty"`
	Required   []string          `json:"required,omitempty"`
	MinLength  *int              `json:"minLength,omitempty"`
	MaxLength  *int              `json:"maxLength,omitempty"`
	Pattern    string            `json:"pattern,omitempty"`
	Enum       []any             `json:"enum,omitempty"`
}

// SchemaValidator validates messages against JSON schemas.
type SchemaValidator struct {
	schemas map[string]*Schema // messageType -> schema
}

// NewSchemaValidator creates a schema validator.
func NewSchemaValidator(schemas map[string]*Schema) *SchemaValidator {
	return &SchemaValidator{
		schemas: schemas,
	}
}

// Validate validates message against schema.
func (sv *SchemaValidator) Validate(ctx context.Context, msg *streaming.Message, sender streaming.EnhancedConnection) error {
	schema, ok := sv.schemas[msg.Type]
	if !ok {
		// No schema defined for this type, skip validation
		return nil
	}

	// Validate data against schema
	if err := sv.validateAgainstSchema(msg.Data, schema); err != nil {
		return fmt.Errorf("schema validation failed for type '%s': %w", msg.Type, err)
	}

	return nil
}

// ValidateContent validates content structure.
func (sv *SchemaValidator) ValidateContent(content any) error {
	if content == nil {
		return NewValidationError("content", "content cannot be nil", "CONTENT_REQUIRED")
	}
	return nil
}

// ValidateMetadata validates metadata structure.
func (sv *SchemaValidator) ValidateMetadata(metadata map[string]any) error {
	// Basic metadata validation
	return nil
}

// validateAgainstSchema validates data against a schema.
func (sv *SchemaValidator) validateAgainstSchema(data any, schema *Schema) error {
	switch schema.Type {
	case "object":
		return sv.validateObject(data, schema)
	case "string":
		return sv.validateString(data, schema)
	case "number":
		return sv.validateNumber(data, schema)
	case "boolean":
		return sv.validateBoolean(data, schema)
	case "array":
		return sv.validateArray(data, schema)
	default:
		return nil
	}
}

func (sv *SchemaValidator) validateObject(data any, schema *Schema) error {
	obj, ok := data.(map[string]any)
	if !ok {
		// Try to convert from JSON
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return NewValidationError("", "expected object", "TYPE_MISMATCH")
		}
		if err := json.Unmarshal(jsonBytes, &obj); err != nil {
			return NewValidationError("", "expected object", "TYPE_MISMATCH")
		}
	}

	// Check required fields
	for _, required := range schema.Required {
		if _, ok := obj[required]; !ok {
			return NewValidationError(required, "required field missing", "REQUIRED_FIELD")
		}
	}

	// Validate properties
	for key, propSchema := range schema.Properties {
		if val, ok := obj[key]; ok {
			if err := sv.validateAgainstSchema(val, &propSchema); err != nil {
				return err
			}
		}
	}

	return nil
}

func (sv *SchemaValidator) validateString(data any, schema *Schema) error {
	str, ok := data.(string)
	if !ok {
		return NewValidationError("", "expected string", "TYPE_MISMATCH")
	}

	if schema.MinLength != nil && len(str) < *schema.MinLength {
		return NewValidationError("", fmt.Sprintf("string too short (min: %d)", *schema.MinLength), "LENGTH_MIN")
	}

	if schema.MaxLength != nil && len(str) > *schema.MaxLength {
		return NewValidationError("", fmt.Sprintf("string too long (max: %d)", *schema.MaxLength), "LENGTH_MAX")
	}

	// Check enum
	if len(schema.Enum) > 0 {
		valid := false
		for _, allowed := range schema.Enum {
			if str == allowed {
				valid = true
				break
			}
		}
		if !valid {
			return NewValidationError("", "value not in allowed list", "ENUM_MISMATCH")
		}
	}

	return nil
}

func (sv *SchemaValidator) validateNumber(data any, schema *Schema) error {
	switch data.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return nil
	default:
		return NewValidationError("", "expected number", "TYPE_MISMATCH")
	}
}

func (sv *SchemaValidator) validateBoolean(data any, schema *Schema) error {
	if _, ok := data.(bool); !ok {
		return NewValidationError("", "expected boolean", "TYPE_MISMATCH")
	}
	return nil
}

func (sv *SchemaValidator) validateArray(data any, schema *Schema) error {
	if _, ok := data.([]any); !ok {
		return NewValidationError("", "expected array", "TYPE_MISMATCH")
	}
	return nil
}

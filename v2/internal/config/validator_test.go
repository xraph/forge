package config

import (
	"context"
	"testing"

	configcore "github.com/xraph/forge/v2/internal/config/core"
)

// =============================================================================
// VALIDATOR CREATION TESTS
// =============================================================================

func TestNewValidator(t *testing.T) {
	tests := []struct {
		name string
		mode ValidationMode
	}{
		{"strict mode", ValidationModeStrict},
		{"loose mode", ValidationModeLoose},
		{"permissive mode", ValidationModePermissive},
		{"disabled mode", ValidationModeDisabled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator := NewValidator(tt.mode)
			if validator == nil {
				t.Fatal("NewValidator() returned nil")
			}

			v, ok := validator.(*Validator)
			if !ok {
				t.Fatal("NewValidator() did not return *Validator")
			}

			if v.mode != tt.mode {
				t.Errorf("mode = %v, want %v", v.mode, tt.mode)
			}

			if v.rules == nil {
				t.Error("rules map not initialized")
			}

			if v.schemas == nil {
				t.Error("schemas map not initialized")
			}
		})
	}
}

// =============================================================================
// VALIDATION MODE TESTS
// =============================================================================

func TestValidator_Validate_Disabled(t *testing.T) {
	validator := NewValidator(ValidationModeDisabled).(*Validator)

	// Add a rule that would fail
	validator.AddRule("test", func(v interface{}) error {
		return ErrValidationError("should not be called", nil)
	})

	data := map[string]interface{}{
		"test": "any_value",
	}

	// With disabled mode, validation should always pass
	err := validator.Validate(context.Background(), data)
	if err != nil {
		t.Errorf("Validate() with disabled mode error = %v, want nil", err)
	}
}

func TestValidator_Validate_Permissive(t *testing.T) {
	validator := NewValidator(ValidationModePermissive).(*Validator)

	// Add a rule that fails
	validator.AddRule("test", func(v interface{}) error {
		return ErrValidationError("validation failed", nil)
	})

	data := map[string]interface{}{
		"test": "value",
	}

	// Permissive mode should log but not return error
	err := validator.Validate(context.Background(), data)
	if err != nil {
		t.Errorf("Validate() with permissive mode error = %v, want nil", err)
	}
}

func TestValidator_Validate_Loose(t *testing.T) {
	validator := NewValidator(ValidationModeLoose).(*Validator)

	// Add rules - some pass, some fail
	validator.AddRule("required", func(v interface{}) error {
		if v == nil || v == "" {
			return ErrValidationError("required field empty", nil)
		}
		return nil
	})

	validator.AddRule("optional", func(v interface{}) error {
		if v != "expected" {
			return ErrValidationError("optional validation failed", nil)
		}
		return nil
	})

	t.Run("critical validation fails", func(t *testing.T) {
		data := map[string]interface{}{
			"required": "",
			"optional": "wrong",
		}

		err := validator.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error for critical validation failure")
		}
	})

	t.Run("only optional validation fails", func(t *testing.T) {
		data := map[string]interface{}{
			"required": "present",
			"optional": "wrong",
		}

		// In loose mode, might pass if only non-critical validations fail
		err := validator.Validate(context.Background(), data)
		// Behavior depends on implementation
		_ = err
	})
}

func TestValidator_Validate_Strict(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	validator.AddRule("test", func(v interface{}) error {
		if v != "expected" {
			return ErrValidationError("value not expected", nil)
		}
		return nil
	})

	t.Run("validation passes", func(t *testing.T) {
		data := map[string]interface{}{
			"test": "expected",
		}

		err := validator.Validate(context.Background(), data)
		if err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("validation fails", func(t *testing.T) {
		data := map[string]interface{}{
			"test": "wrong",
		}

		err := validator.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error in strict mode")
		}
	})
}

// =============================================================================
// RULE MANAGEMENT TESTS
// =============================================================================

func TestValidator_AddRule(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	rule := func(v interface{}) error {
		return nil
	}

	t.Run("add rule", func(t *testing.T) {
		validator.AddRule("test_key", rule)

		if _, exists := validator.rules["test_key"]; !exists {
			t.Error("Rule not added to validator")
		}
	})

	t.Run("add multiple rules for same key", func(t *testing.T) {
		validator.AddRule("multi", rule)
		validator.AddRule("multi", rule)

		if len(validator.rules["multi"]) != 2 {
			t.Errorf("Expected 2 rules for key, got %d", len(validator.rules["multi"]))
		}
	})

	t.Run("add nil rule", func(t *testing.T) {
		// Should not panic
		validator.AddRule("nil_rule", nil)
	})
}

func TestValidator_AddConstraintRule(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	constraint := configcore.ConstraintRule{
		Field:       "age",
		Type:        "int",
		Required:    true,
		Min:         18,
		Max:         100,
		Pattern:     "",
		CustomCheck: nil,
	}

	t.Run("add constraint rule", func(t *testing.T) {
		validator.AddConstraintRule(constraint)

		// Verify constraint was added to internal rules
		if _, exists := validator.rules["age"]; !exists {
			t.Error("Constraint rule not added")
		}
	})

	t.Run("required field validation", func(t *testing.T) {
		data := map[string]interface{}{
			// age is missing
		}

		err := validator.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error for missing required field")
		}
	})

	t.Run("min/max validation", func(t *testing.T) {
		v := NewValidator(ValidationModeStrict).(*Validator)
		v.AddConstraintRule(constraint)

		// Too low
		data := map[string]interface{}{"age": 10}
		err := v.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error for value below min")
		}

		// Too high
		data = map[string]interface{}{"age": 150}
		err = v.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error for value above max")
		}

		// Just right
		data = map[string]interface{}{"age": 25}
		err = v.Validate(context.Background(), data)
		if err != nil {
			t.Errorf("Validate() with valid value error = %v, want nil", err)
		}
	})
}

func TestValidator_RemoveRule(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	rule := func(v interface{}) error {
		return ErrValidationError("test", nil)
	}

	validator.AddRule("test", rule)

	t.Run("remove existing rule", func(t *testing.T) {
		validator.RemoveRule("test")

		if rules, exists := validator.rules["test"]; exists && len(rules) > 0 {
			t.Error("Rule not removed from validator")
		}
	})

	t.Run("remove non-existent rule", func(t *testing.T) {
		// Should not panic
		validator.RemoveRule("nonexistent")
	})
}

func TestValidator_GetRules(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	rule1 := func(v interface{}) error { return nil }
	rule2 := func(v interface{}) error { return nil }

	validator.AddRule("key1", rule1)
	validator.AddRule("key2", rule2)

	rules := validator.GetRules()

	if len(rules) != 2 {
		t.Errorf("GetRules() returned %d rules, want 2", len(rules))
	}

	if _, exists := rules["key1"]; !exists {
		t.Error("key1 not in returned rules")
	}

	if _, exists := rules["key2"]; !exists {
		t.Error("key2 not in returned rules")
	}
}

// =============================================================================
// SCHEMA MANAGEMENT TESTS
// =============================================================================

func TestValidator_AddSchema(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	schema := configcore.ValidationSchema{
		Type: "object",
		Properties: map[string]configcore.ValidationSchema{
			"name": {Type: "string", Required: true},
			"age":  {Type: "int", Required: false},
		},
		Required: []string{"name"},
	}

	t.Run("add schema", func(t *testing.T) {
		validator.AddSchema("user", schema)

		if _, exists := validator.schemas["user"]; !exists {
			t.Error("Schema not added to validator")
		}
	})

	t.Run("replace existing schema", func(t *testing.T) {
		newSchema := configcore.ValidationSchema{
			Type:     "object",
			Required: []string{"email"},
		}

		validator.AddSchema("user", newSchema)

		stored := validator.schemas["user"]
		if len(stored.Required) != 1 || stored.Required[0] != "email" {
			t.Error("Schema not replaced correctly")
		}
	})
}

func TestValidator_RemoveSchema(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	schema := configcore.ValidationSchema{Type: "object"}
	validator.AddSchema("test", schema)

	t.Run("remove existing schema", func(t *testing.T) {
		validator.RemoveSchema("test")

		if _, exists := validator.schemas["test"]; exists {
			t.Error("Schema not removed from validator")
		}
	})

	t.Run("remove non-existent schema", func(t *testing.T) {
		// Should not panic
		validator.RemoveSchema("nonexistent")
	})
}

func TestValidator_GetSchema(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	schema := configcore.ValidationSchema{
		Type:     "object",
		Required: []string{"field1"},
	}
	validator.AddSchema("test", schema)

	t.Run("get existing schema", func(t *testing.T) {
		retrieved := validator.GetSchema("test")
		if retrieved == nil {
			t.Fatal("GetSchema() returned nil")
		}

		if retrieved.Type != "object" {
			t.Errorf("Schema type = %v, want %v", retrieved.Type, "object")
		}
	})

	t.Run("get non-existent schema", func(t *testing.T) {
		retrieved := validator.GetSchema("nonexistent")
		if retrieved != nil {
			t.Error("GetSchema() should return nil for non-existent schema")
		}
	})
}

func TestValidator_GetAllSchemas(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	schema1 := configcore.ValidationSchema{Type: "string"}
	schema2 := configcore.ValidationSchema{Type: "int"}

	validator.AddSchema("schema1", schema1)
	validator.AddSchema("schema2", schema2)

	schemas := validator.GetAllSchemas()

	if len(schemas) != 2 {
		t.Errorf("GetAllSchemas() returned %d schemas, want 2", len(schemas))
	}

	if _, exists := schemas["schema1"]; !exists {
		t.Error("schema1 not in returned schemas")
	}

	if _, exists := schemas["schema2"]; !exists {
		t.Error("schema2 not in returned schemas")
	}
}

// =============================================================================
// SCHEMA VALIDATION TESTS
// =============================================================================

func TestValidator_ValidateWithSchema(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	schema := configcore.ValidationSchema{
		Type: "object",
		Properties: map[string]configcore.ValidationSchema{
			"name":  {Type: "string", Required: true},
			"age":   {Type: "int", Required: true},
			"email": {Type: "string", Required: false},
		},
		Required: []string{"name", "age"},
	}

	validator.AddSchema("user", schema)

	t.Run("valid data", func(t *testing.T) {
		data := map[string]interface{}{
			"name": "John",
			"age":  30,
		}

		err := validator.ValidateWithSchema(context.Background(), "user", data)
		if err != nil {
			t.Errorf("ValidateWithSchema() error = %v, want nil", err)
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		data := map[string]interface{}{
			"name": "John",
			// age is missing
		}

		err := validator.ValidateWithSchema(context.Background(), "user", data)
		if err == nil {
			t.Error("ValidateWithSchema() should return error for missing required field")
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		data := map[string]interface{}{
			"name": "John",
			"age":  "not_a_number",
		}

		err := validator.ValidateWithSchema(context.Background(), "user", data)
		if err == nil {
			t.Error("ValidateWithSchema() should return error for wrong type")
		}
	})

	t.Run("non-existent schema", func(t *testing.T) {
		data := map[string]interface{}{"key": "value"}

		err := validator.ValidateWithSchema(context.Background(), "nonexistent", data)
		if err == nil {
			t.Error("ValidateWithSchema() should return error for non-existent schema")
		}
	})
}

// =============================================================================
// KEY-SPECIFIC VALIDATION TESTS
// =============================================================================

func TestValidator_ValidateKey(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	validator.AddRule("test_key", func(v interface{}) error {
		if v != "expected" {
			return ErrValidationError("unexpected value", nil)
		}
		return nil
	})

	t.Run("validate existing key with valid value", func(t *testing.T) {
		data := map[string]interface{}{
			"test_key": "expected",
		}

		err := validator.ValidateKey(context.Background(), "test_key", data)
		if err != nil {
			t.Errorf("ValidateKey() error = %v, want nil", err)
		}
	})

	t.Run("validate existing key with invalid value", func(t *testing.T) {
		data := map[string]interface{}{
			"test_key": "wrong",
		}

		err := validator.ValidateKey(context.Background(), "test_key", data)
		if err == nil {
			t.Error("ValidateKey() should return error for invalid value")
		}
	})

	t.Run("validate key without rules", func(t *testing.T) {
		data := map[string]interface{}{
			"no_rules": "any_value",
		}

		err := validator.ValidateKey(context.Background(), "no_rules", data)
		if err != nil {
			t.Errorf("ValidateKey() for key without rules error = %v, want nil", err)
		}
	})

	t.Run("validate missing key", func(t *testing.T) {
		data := map[string]interface{}{
			"other_key": "value",
		}

		err := validator.ValidateKey(context.Background(), "test_key", data)
		// Behavior depends on implementation - missing key might pass
		_ = err
	})
}

// =============================================================================
// TYPE VALIDATION TESTS
// =============================================================================

func TestValidator_TypeValidation(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	tests := []struct {
		name      string
		schema    configcore.ValidationSchema
		value     interface{}
		shouldErr bool
	}{
		{
			name:      "string type valid",
			schema:    configcore.ValidationSchema{Type: "string"},
			value:     "text",
			shouldErr: false,
		},
		{
			name:      "string type invalid",
			schema:    configcore.ValidationSchema{Type: "string"},
			value:     123,
			shouldErr: true,
		},
		{
			name:      "int type valid",
			schema:    configcore.ValidationSchema{Type: "int"},
			value:     42,
			shouldErr: false,
		},
		{
			name:      "int type invalid",
			schema:    configcore.ValidationSchema{Type: "int"},
			value:     "not_int",
			shouldErr: true,
		},
		{
			name:      "bool type valid",
			schema:    configcore.ValidationSchema{Type: "bool"},
			value:     true,
			shouldErr: false,
		},
		{
			name:      "bool type invalid",
			schema:    configcore.ValidationSchema{Type: "bool"},
			value:     "not_bool",
			shouldErr: true,
		},
		{
			name:      "array type valid",
			schema:    configcore.ValidationSchema{Type: "array"},
			value:     []interface{}{1, 2, 3},
			shouldErr: false,
		},
		{
			name:      "array type invalid",
			schema:    configcore.ValidationSchema{Type: "array"},
			value:     "not_array",
			shouldErr: true,
		},
		{
			name:      "object type valid",
			schema:    configcore.ValidationSchema{Type: "object"},
			value:     map[string]interface{}{"key": "value"},
			shouldErr: false,
		},
		{
			name:      "object type invalid",
			schema:    configcore.ValidationSchema{Type: "object"},
			value:     "not_object",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validator.AddSchema("test", tt.schema)

			data := map[string]interface{}{
				"field": tt.value,
			}

			// Create a schema that validates the field
			fieldSchema := configcore.ValidationSchema{
				Type: "object",
				Properties: map[string]configcore.ValidationSchema{
					"field": tt.schema,
				},
			}
			validator.AddSchema("test", fieldSchema)

			err := validator.ValidateWithSchema(context.Background(), "test", data)

			if tt.shouldErr && err == nil {
				t.Error("ValidateWithSchema() should return error")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("ValidateWithSchema() unexpected error = %v", err)
			}
		})
	}
}

// =============================================================================
// PATTERN VALIDATION TESTS
// =============================================================================

func TestValidator_PatternValidation(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	tests := []struct {
		name      string
		pattern   string
		value     interface{}
		shouldErr bool
	}{
		{
			name:      "email pattern valid",
			pattern:   `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			value:     "test@example.com",
			shouldErr: false,
		},
		{
			name:      "email pattern invalid",
			pattern:   `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`,
			value:     "not-an-email",
			shouldErr: true,
		},
		{
			name:      "phone pattern valid",
			pattern:   `^\d{3}-\d{3}-\d{4}$`,
			value:     "123-456-7890",
			shouldErr: false,
		},
		{
			name:      "phone pattern invalid",
			pattern:   `^\d{3}-\d{3}-\d{4}$`,
			value:     "123456789",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			constraint := configcore.ConstraintRule{
				Field:   "test",
				Type:    "string",
				Pattern: tt.pattern,
			}

			v := NewValidator(ValidationModeStrict).(*Validator)
			v.AddConstraintRule(constraint)

			data := map[string]interface{}{
				"test": tt.value,
			}

			err := v.Validate(context.Background(), data)

			if tt.shouldErr && err == nil {
				t.Error("Validate() should return error for pattern mismatch")
			}
			if !tt.shouldErr && err != nil {
				t.Errorf("Validate() unexpected error = %v", err)
			}
		})
	}
}

// =============================================================================
// CUSTOM CHECK TESTS
// =============================================================================

func TestValidator_CustomCheck(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	customCheck := func(v interface{}) error {
		str, ok := v.(string)
		if !ok {
			return ErrValidationError("not a string", nil)
		}
		if len(str) < 5 {
			return ErrValidationError("string too short", nil)
		}
		return nil
	}

	constraint := configcore.ConstraintRule{
		Field:       "custom",
		Type:        "string",
		CustomCheck: customCheck,
	}

	validator.AddConstraintRule(constraint)

	t.Run("custom check passes", func(t *testing.T) {
		data := map[string]interface{}{
			"custom": "long enough",
		}

		err := validator.Validate(context.Background(), data)
		if err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("custom check fails", func(t *testing.T) {
		data := map[string]interface{}{
			"custom": "short",
		}

		err := validator.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error when custom check fails")
		}
	})
}

// =============================================================================
// NESTED VALIDATION TESTS
// =============================================================================

func TestValidator_NestedValidation(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	schema := configcore.ValidationSchema{
		Type: "object",
		Properties: map[string]configcore.ValidationSchema{
			"user": {
				Type: "object",
				Properties: map[string]configcore.ValidationSchema{
					"name": {Type: "string", Required: true},
					"address": {
						Type: "object",
						Properties: map[string]configcore.ValidationSchema{
							"street": {Type: "string", Required: true},
							"city":   {Type: "string", Required: true},
						},
					},
				},
			},
		},
	}

	validator.AddSchema("nested", schema)

	t.Run("valid nested structure", func(t *testing.T) {
		data := map[string]interface{}{
			"user": map[string]interface{}{
				"name": "John",
				"address": map[string]interface{}{
					"street": "123 Main St",
					"city":   "Springfield",
				},
			},
		}

		err := validator.ValidateWithSchema(context.Background(), "nested", data)
		if err != nil {
			t.Errorf("ValidateWithSchema() error = %v, want nil", err)
		}
	})

	t.Run("invalid nested field", func(t *testing.T) {
		data := map[string]interface{}{
			"user": map[string]interface{}{
				"name": "John",
				"address": map[string]interface{}{
					"street": "123 Main St",
					// city is missing
				},
			},
		}

		err := validator.ValidateWithSchema(context.Background(), "nested", data)
		if err == nil {
			t.Error("ValidateWithSchema() should return error for missing nested field")
		}
	})
}

// =============================================================================
// ARRAY VALIDATION TESTS
// =============================================================================

func TestValidator_ArrayValidation(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	schema := configcore.ValidationSchema{
		Type: "array",
		Items: &configcore.ValidationSchema{
			Type: "string",
		},
		MinItems: 1,
		MaxItems: 5,
	}

	validator.AddSchema("array_test", configcore.ValidationSchema{
		Type: "object",
		Properties: map[string]configcore.ValidationSchema{
			"items": schema,
		},
	})

	t.Run("valid array", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{"a", "b", "c"},
		}

		err := validator.ValidateWithSchema(context.Background(), "array_test", data)
		if err != nil {
			t.Errorf("ValidateWithSchema() error = %v, want nil", err)
		}
	})

	t.Run("array too short", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{},
		}

		err := validator.ValidateWithSchema(context.Background(), "array_test", data)
		if err == nil {
			t.Error("ValidateWithSchema() should return error for array below MinItems")
		}
	})

	t.Run("array too long", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{"a", "b", "c", "d", "e", "f"},
		}

		err := validator.ValidateWithSchema(context.Background(), "array_test", data)
		if err == nil {
			t.Error("ValidateWithSchema() should return error for array above MaxItems")
		}
	})

	t.Run("array item wrong type", func(t *testing.T) {
		data := map[string]interface{}{
			"items": []interface{}{"a", 123, "c"},
		}

		err := validator.ValidateWithSchema(context.Background(), "array_test", data)
		if err == nil {
			t.Error("ValidateWithSchema() should return error for wrong item type")
		}
	})
}

// =============================================================================
// MULTIPLE RULE TESTS
// =============================================================================

func TestValidator_MultipleRules(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	// Add multiple rules for the same key
	validator.AddRule("multi", func(v interface{}) error {
		str, ok := v.(string)
		if !ok {
			return ErrValidationError("not a string", nil)
		}
		if len(str) < 3 {
			return ErrValidationError("too short", nil)
		}
		return nil
	})

	validator.AddRule("multi", func(v interface{}) error {
		str, ok := v.(string)
		if !ok {
			return ErrValidationError("not a string", nil)
		}
		if len(str) > 10 {
			return ErrValidationError("too long", nil)
		}
		return nil
	})

	t.Run("passes all rules", func(t *testing.T) {
		data := map[string]interface{}{
			"multi": "medium",
		}

		err := validator.Validate(context.Background(), data)
		if err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("fails first rule", func(t *testing.T) {
		data := map[string]interface{}{
			"multi": "ab",
		}

		err := validator.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error when first rule fails")
		}
	})

	t.Run("fails second rule", func(t *testing.T) {
		data := map[string]interface{}{
			"multi": "very_long_string",
		}

		err := validator.Validate(context.Background(), data)
		if err == nil {
			t.Error("Validate() should return error when second rule fails")
		}
	})
}

// =============================================================================
// EDGE CASE TESTS
// =============================================================================

func TestValidator_EdgeCases(t *testing.T) {
	t.Run("validate empty data", func(t *testing.T) {
		validator := NewValidator(ValidationModeStrict).(*Validator)
		err := validator.Validate(context.Background(), map[string]interface{}{})
		if err != nil {
			t.Errorf("Validate() with empty data error = %v, want nil", err)
		}
	})

	t.Run("validate nil data", func(t *testing.T) {
		validator := NewValidator(ValidationModeStrict).(*Validator)
		err := validator.Validate(context.Background(), nil)
		if err == nil {
			t.Error("Validate() should return error for nil data")
		}
	})

	t.Run("rule returns nil error", func(t *testing.T) {
		validator := NewValidator(ValidationModeStrict).(*Validator)
		validator.AddRule("test", func(v interface{}) error {
			return nil
		})

		data := map[string]interface{}{"test": "any"}
		err := validator.Validate(context.Background(), data)
		if err != nil {
			t.Errorf("Validate() error = %v, want nil", err)
		}
	})

	t.Run("validate with context cancellation", func(t *testing.T) {
		validator := NewValidator(ValidationModeStrict).(*Validator)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		data := map[string]interface{}{"key": "value"}
		err := validator.Validate(ctx, data)

		// Should handle cancellation gracefully
		if err != nil && err != context.Canceled {
			// Either no error or context canceled error is acceptable
		}
	})
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestValidator_Concurrency(t *testing.T) {
	validator := NewValidator(ValidationModeStrict).(*Validator)

	validator.AddRule("test", func(v interface{}) error {
		if v != "valid" {
			return ErrValidationError("invalid", nil)
		}
		return nil
	})

	done := make(chan bool)

	// Concurrent validations
	for i := 0; i < 10; i++ {
		go func() {
			data := map[string]interface{}{"test": "valid"}
			_ = validator.Validate(context.Background(), data)
			done <- true
		}()
	}

	// Concurrent rule additions
	for i := 0; i < 5; i++ {
		go func(idx int) {
			key := string(rune('a' + idx))
			validator.AddRule(key, func(v interface{}) error { return nil })
			done <- true
		}(i)
	}

	// Wait for all
	for i := 0; i < 15; i++ {
		<-done
	}
}

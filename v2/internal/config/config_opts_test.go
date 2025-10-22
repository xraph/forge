package config

import (
	"testing"

	configcore "github.com/xraph/forge/v2/internal/config/core"
)

// =============================================================================
// WITH DEFAULT TESTS
// =============================================================================

func TestWithDefault(t *testing.T) {
	tests := []struct {
		name         string
		defaultValue interface{}
	}{
		{"string default", "default_value"},
		{"int default", 42},
		{"bool default", true},
		{"nil default", nil},
		{"map default", map[string]interface{}{"key": "value"}},
		{"slice default", []interface{}{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := WithDefault(tt.defaultValue)
			if opt == nil {
				t.Fatal("WithDefault() returned nil")
			}

			opts := &configcore.GetOptions{}
			opt(opts)

			if opts.DefaultValue != tt.defaultValue {
				t.Errorf("DefaultValue = %v, want %v", opts.DefaultValue, tt.defaultValue)
			}

			if !opts.HasDefault {
				t.Error("HasDefault should be true")
			}
		})
	}
}

// =============================================================================
// WITH REQUIRED TESTS
// =============================================================================

func TestWithRequired(t *testing.T) {
	opt := WithRequired()
	if opt == nil {
		t.Fatal("WithRequired() returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	if !opts.Required {
		t.Error("Required should be true")
	}
}

// =============================================================================
// WITH VALIDATOR TESTS
// =============================================================================

func TestWithValidator(t *testing.T) {
	validator := func(v interface{}) error {
		if v == nil {
			return ErrValidationError("nil value", nil)
		}
		return nil
	}

	opt := WithValidator(validator)
	if opt == nil {
		t.Fatal("WithValidator() returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	if opts.Validator == nil {
		t.Fatal("Validator should not be nil")
	}

	// Test that validator works
	err := opts.Validator("test")
	if err != nil {
		t.Errorf("Validator() error = %v, want nil", err)
	}

	err = opts.Validator(nil)
	if err == nil {
		t.Error("Validator() should return error for nil value")
	}
}

func TestWithValidator_Nil(t *testing.T) {
	opt := WithValidator(nil)
	if opt == nil {
		t.Fatal("WithValidator(nil) returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	// Should handle nil validator gracefully
	if opts.Validator != nil {
		// Implementation may or may not set nil validator
	}
}

// =============================================================================
// WITH TRANSFORM TESTS
// =============================================================================

func TestWithTransform(t *testing.T) {
	transform := func(v interface{}) interface{} {
		if str, ok := v.(string); ok {
			return str + "_transformed"
		}
		return v
	}

	opt := WithTransform(transform)
	if opt == nil {
		t.Fatal("WithTransform() returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	if opts.Transform == nil {
		t.Fatal("Transform should not be nil")
	}

	// Test that transform works
	result := opts.Transform("test")
	if result != "test_transformed" {
		t.Errorf("Transform() = %v, want %v", result, "test_transformed")
	}

	// Test with non-string
	result = opts.Transform(42)
	if result != 42 {
		t.Errorf("Transform() = %v, want %v", result, 42)
	}
}

func TestWithTransform_Nil(t *testing.T) {
	opt := WithTransform(nil)
	if opt == nil {
		t.Fatal("WithTransform(nil) returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	// Should handle nil transform gracefully
	if opts.Transform != nil {
		// Implementation may or may not set nil transform
	}
}

// =============================================================================
// WITH ON MISSING TESTS
// =============================================================================

func TestWithOnMissing(t *testing.T) {
	onMissing := func(key string) interface{} {
		return "fallback_" + key
	}

	opt := WithOnMissing(onMissing)
	if opt == nil {
		t.Fatal("WithOnMissing() returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	if opts.OnMissing == nil {
		t.Fatal("OnMissing should not be nil")
	}

	// Test that onMissing works
	result := opts.OnMissing("test_key")
	if result != "fallback_test_key" {
		t.Errorf("OnMissing() = %v, want %v", result, "fallback_test_key")
	}
}

func TestWithOnMissing_Nil(t *testing.T) {
	opt := WithOnMissing(nil)
	if opt == nil {
		t.Fatal("WithOnMissing(nil) returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	// Should handle nil onMissing gracefully
	if opts.OnMissing != nil {
		// Implementation may or may not set nil onMissing
	}
}

// =============================================================================
// ALLOW EMPTY TESTS
// =============================================================================

func TestAllowEmpty(t *testing.T) {
	opt := AllowEmpty()
	if opt == nil {
		t.Fatal("AllowEmpty() returned nil")
	}

	opts := &configcore.GetOptions{}
	opt(opts)

	if !opts.AllowEmpty {
		t.Error("AllowEmpty should be true")
	}
}

// =============================================================================
// WITH CACHE KEY TESTS
// =============================================================================

func TestWithCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		cacheKey string
	}{
		{"simple key", "cache_key"},
		{"dotted key", "cache.key.path"},
		{"empty key", ""},
		{"key with special chars", "cache-key_123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opt := WithCacheKey(tt.cacheKey)
			if opt == nil {
				t.Fatal("WithCacheKey() returned nil")
			}

			opts := &configcore.GetOptions{}
			opt(opts)

			if opts.CacheKey != tt.cacheKey {
				t.Errorf("CacheKey = %v, want %v", opts.CacheKey, tt.cacheKey)
			}
		})
	}
}

// =============================================================================
// MULTIPLE OPTIONS TESTS
// =============================================================================

func TestMultipleOptions(t *testing.T) {
	t.Run("combine default and required", func(t *testing.T) {
		opts := &configcore.GetOptions{}

		WithDefault("default_value")(opts)
		WithRequired()(opts)

		if !opts.HasDefault {
			t.Error("HasDefault should be true")
		}
		if opts.DefaultValue != "default_value" {
			t.Errorf("DefaultValue = %v, want %v", opts.DefaultValue, "default_value")
		}
		if !opts.Required {
			t.Error("Required should be true")
		}
	})

	t.Run("combine validator and transform", func(t *testing.T) {
		validator := func(v interface{}) error {
			if v == nil {
				return ErrValidationError("nil", nil)
			}
			return nil
		}

		transform := func(v interface{}) interface{} {
			return "transformed"
		}

		opts := &configcore.GetOptions{}

		WithValidator(validator)(opts)
		WithTransform(transform)(opts)

		if opts.Validator == nil {
			t.Error("Validator should not be nil")
		}
		if opts.Transform == nil {
			t.Error("Transform should not be nil")
		}

		// Test both work
		err := opts.Validator("test")
		if err != nil {
			t.Errorf("Validator() error = %v", err)
		}

		result := opts.Transform("input")
		if result != "transformed" {
			t.Errorf("Transform() = %v, want transformed", result)
		}
	})

	t.Run("all options combined", func(t *testing.T) {
		opts := &configcore.GetOptions{}

		WithDefault("default")(opts)
		WithRequired()(opts)
		WithValidator(func(v interface{}) error { return nil })(opts)
		WithTransform(func(v interface{}) interface{} { return v })(opts)
		WithOnMissing(func(key string) interface{} { return "missing" })(opts)
		AllowEmpty()(opts)
		WithCacheKey("cache")(opts)

		// Verify all options are set
		if !opts.HasDefault {
			t.Error("HasDefault should be true")
		}
		if !opts.Required {
			t.Error("Required should be true")
		}
		if opts.Validator == nil {
			t.Error("Validator should not be nil")
		}
		if opts.Transform == nil {
			t.Error("Transform should not be nil")
		}
		if opts.OnMissing == nil {
			t.Error("OnMissing should not be nil")
		}
		if !opts.AllowEmpty {
			t.Error("AllowEmpty should be true")
		}
		if opts.CacheKey != "cache" {
			t.Errorf("CacheKey = %v, want cache", opts.CacheKey)
		}
	})
}

// =============================================================================
// OPTION PRECEDENCE TESTS
// =============================================================================

func TestOptionPrecedence(t *testing.T) {
	t.Run("last default wins", func(t *testing.T) {
		opts := &configcore.GetOptions{}

		WithDefault("first")(opts)
		WithDefault("second")(opts)
		WithDefault("third")(opts)

		if opts.DefaultValue != "third" {
			t.Errorf("DefaultValue = %v, want third", opts.DefaultValue)
		}
	})

	t.Run("last cache key wins", func(t *testing.T) {
		opts := &configcore.GetOptions{}

		WithCacheKey("first")(opts)
		WithCacheKey("second")(opts)

		if opts.CacheKey != "second" {
			t.Errorf("CacheKey = %v, want second", opts.CacheKey)
		}
	})

	t.Run("required set multiple times", func(t *testing.T) {
		opts := &configcore.GetOptions{}

		WithRequired()(opts)
		WithRequired()(opts)

		if !opts.Required {
			t.Error("Required should be true")
		}
	})
}

// =============================================================================
// VALIDATOR FUNCTION TESTS
// =============================================================================

func TestValidatorFunctions(t *testing.T) {
	t.Run("type validator", func(t *testing.T) {
		validator := func(v interface{}) error {
			if _, ok := v.(string); !ok {
				return ErrValidationError("not a string", nil)
			}
			return nil
		}

		opt := WithValidator(validator)
		opts := &configcore.GetOptions{}
		opt(opts)

		// Valid case
		err := opts.Validator("test string")
		if err != nil {
			t.Errorf("Validator() error = %v, want nil", err)
		}

		// Invalid case
		err = opts.Validator(123)
		if err == nil {
			t.Error("Validator() should return error for non-string")
		}
	})

	t.Run("range validator", func(t *testing.T) {
		validator := func(v interface{}) error {
			num, ok := v.(int)
			if !ok {
				return ErrValidationError("not an int", nil)
			}
			if num < 0 || num > 100 {
				return ErrValidationError("out of range", nil)
			}
			return nil
		}

		opt := WithValidator(validator)
		opts := &configcore.GetOptions{}
		opt(opts)

		// Valid cases
		for _, val := range []int{0, 50, 100} {
			err := opts.Validator(val)
			if err != nil {
				t.Errorf("Validator(%d) error = %v, want nil", val, err)
			}
		}

		// Invalid cases
		for _, val := range []int{-1, 101} {
			err := opts.Validator(val)
			if err == nil {
				t.Errorf("Validator(%d) should return error", val)
			}
		}
	})

	t.Run("pattern validator", func(t *testing.T) {
		validator := func(v interface{}) error {
			str, ok := v.(string)
			if !ok {
				return ErrValidationError("not a string", nil)
			}
			if len(str) < 3 {
				return ErrValidationError("too short", nil)
			}
			return nil
		}

		opt := WithValidator(validator)
		opts := &configcore.GetOptions{}
		opt(opts)

		err := opts.Validator("ab")
		if err == nil {
			t.Error("Validator() should return error for short string")
		}

		err = opts.Validator("abc")
		if err != nil {
			t.Errorf("Validator() error = %v, want nil", err)
		}
	})
}

// =============================================================================
// TRANSFORM FUNCTION TESTS
// =============================================================================

func TestTransformFunctions(t *testing.T) {
	t.Run("uppercase transform", func(t *testing.T) {
		transform := func(v interface{}) interface{} {
			if str, ok := v.(string); ok {
				return str + "_UPPER"
			}
			return v
		}

		opt := WithTransform(transform)
		opts := &configcore.GetOptions{}
		opt(opts)

		result := opts.Transform("test")
		if result != "test_UPPER" {
			t.Errorf("Transform() = %v, want test_UPPER", result)
		}

		// Non-string passthrough
		result = opts.Transform(42)
		if result != 42 {
			t.Errorf("Transform() = %v, want 42", result)
		}
	})

	t.Run("type conversion transform", func(t *testing.T) {
		transform := func(v interface{}) interface{} {
			if str, ok := v.(string); ok {
				// Try to convert to int
				if str == "42" {
					return 42
				}
			}
			return v
		}

		opt := WithTransform(transform)
		opts := &configcore.GetOptions{}
		opt(opts)

		result := opts.Transform("42")
		if result != 42 {
			t.Errorf("Transform() = %v, want 42", result)
		}

		result = opts.Transform("other")
		if result != "other" {
			t.Errorf("Transform() = %v, want other", result)
		}
	})

	t.Run("nil handling transform", func(t *testing.T) {
		transform := func(v interface{}) interface{} {
			if v == nil {
				return "default"
			}
			return v
		}

		opt := WithTransform(transform)
		opts := &configcore.GetOptions{}
		opt(opts)

		result := opts.Transform(nil)
		if result != "default" {
			t.Errorf("Transform(nil) = %v, want default", result)
		}

		result = opts.Transform("value")
		if result != "value" {
			t.Errorf("Transform() = %v, want value", result)
		}
	})
}

// =============================================================================
// ON MISSING FUNCTION TESTS
// =============================================================================

func TestOnMissingFunctions(t *testing.T) {
	t.Run("static fallback", func(t *testing.T) {
		onMissing := func(key string) interface{} {
			return "fallback"
		}

		opt := WithOnMissing(onMissing)
		opts := &configcore.GetOptions{}
		opt(opts)

		result := opts.OnMissing("any_key")
		if result != "fallback" {
			t.Errorf("OnMissing() = %v, want fallback", result)
		}
	})

	t.Run("key-based fallback", func(t *testing.T) {
		onMissing := func(key string) interface{} {
			defaults := map[string]interface{}{
				"host": "localhost",
				"port": 8080,
			}
			if val, ok := defaults[key]; ok {
				return val
			}
			return nil
		}

		opt := WithOnMissing(onMissing)
		opts := &configcore.GetOptions{}
		opt(opts)

		result := opts.OnMissing("host")
		if result != "localhost" {
			t.Errorf("OnMissing(\"host\") = %v, want localhost", result)
		}

		result = opts.OnMissing("port")
		if result != 8080 {
			t.Errorf("OnMissing(\"port\") = %v, want 8080", result)
		}

		result = opts.OnMissing("unknown")
		if result != nil {
			t.Errorf("OnMissing(\"unknown\") = %v, want nil", result)
		}
	})

	t.Run("computed fallback", func(t *testing.T) {
		onMissing := func(key string) interface{} {
			// Generate value based on key
			return key + "_generated"
		}

		opt := WithOnMissing(onMissing)
		opts := &configcore.GetOptions{}
		opt(opts)

		result := opts.OnMissing("test")
		if result != "test_generated" {
			t.Errorf("OnMissing() = %v, want test_generated", result)
		}
	})
}

// =============================================================================
// EDGE CASE TESTS
// =============================================================================

func TestOptionsEdgeCases(t *testing.T) {
	t.Run("empty string default", func(t *testing.T) {
		opt := WithDefault("")
		opts := &configcore.GetOptions{}
		opt(opts)

		if opts.DefaultValue != "" {
			t.Errorf("DefaultValue = %v, want empty string", opts.DefaultValue)
		}
		if !opts.HasDefault {
			t.Error("HasDefault should be true even for empty string")
		}
	})

	t.Run("zero value default", func(t *testing.T) {
		opt := WithDefault(0)
		opts := &configcore.GetOptions{}
		opt(opts)

		if opts.DefaultValue != 0 {
			t.Errorf("DefaultValue = %v, want 0", opts.DefaultValue)
		}
		if !opts.HasDefault {
			t.Error("HasDefault should be true even for zero value")
		}
	})

	t.Run("false default", func(t *testing.T) {
		opt := WithDefault(false)
		opts := &configcore.GetOptions{}
		opt(opts)

		if opts.DefaultValue != false {
			t.Errorf("DefaultValue = %v, want false", opts.DefaultValue)
		}
		if !opts.HasDefault {
			t.Error("HasDefault should be true even for false")
		}
	})

	t.Run("empty cache key", func(t *testing.T) {
		opt := WithCacheKey("")
		opts := &configcore.GetOptions{}
		opt(opts)

		if opts.CacheKey != "" {
			t.Errorf("CacheKey = %v, want empty string", opts.CacheKey)
		}
	})
}

// =============================================================================
// COMPLEX SCENARIO TESTS
// =============================================================================

func TestComplexScenarios(t *testing.T) {
	t.Run("validated and transformed value", func(t *testing.T) {
		validator := func(v interface{}) error {
			str, ok := v.(string)
			if !ok {
				return ErrValidationError("not a string", nil)
			}
			if len(str) < 3 {
				return ErrValidationError("too short", nil)
			}
			return nil
		}

		transform := func(v interface{}) interface{} {
			if str, ok := v.(string); ok {
				return str + "_transformed"
			}
			return v
		}

		opts := &configcore.GetOptions{}
		WithValidator(validator)(opts)
		WithTransform(transform)(opts)

		// Validate first
		err := opts.Validator("test")
		if err != nil {
			t.Errorf("Validator() error = %v", err)
		}

		// Then transform
		result := opts.Transform("test")
		if result != "test_transformed" {
			t.Errorf("Transform() = %v, want test_transformed", result)
		}

		// Validate with invalid value
		err = opts.Validator("ab")
		if err == nil {
			t.Error("Validator() should return error for short string")
		}
	})

	t.Run("required with default and onMissing", func(t *testing.T) {
		opts := &configcore.GetOptions{}

		WithRequired()(opts)
		WithDefault("default_value")(opts)
		WithOnMissing(func(key string) interface{} {
			return "missing_value"
		})(opts)

		// All options should be set
		if !opts.Required {
			t.Error("Required should be true")
		}
		if !opts.HasDefault {
			t.Error("HasDefault should be true")
		}
		if opts.OnMissing == nil {
			t.Error("OnMissing should not be nil")
		}

		// Test OnMissing
		result := opts.OnMissing("test")
		if result != "missing_value" {
			t.Errorf("OnMissing() = %v, want missing_value", result)
		}
	})
}

// =============================================================================
// OPTION REUSABILITY TESTS
// =============================================================================

func TestOptionReusability(t *testing.T) {
	t.Run("same option applied multiple times", func(t *testing.T) {
		opt := WithDefault("value")

		opts1 := &configcore.GetOptions{}
		opts2 := &configcore.GetOptions{}

		opt(opts1)
		opt(opts2)

		if opts1.DefaultValue != "value" {
			t.Errorf("opts1.DefaultValue = %v, want value", opts1.DefaultValue)
		}
		if opts2.DefaultValue != "value" {
			t.Errorf("opts2.DefaultValue = %v, want value", opts2.DefaultValue)
		}
	})

	t.Run("options are independent", func(t *testing.T) {
		opts1 := &configcore.GetOptions{}
		opts2 := &configcore.GetOptions{}

		WithDefault("value1")(opts1)
		WithDefault("value2")(opts2)

		if opts1.DefaultValue != "value1" {
			t.Errorf("opts1.DefaultValue = %v, want value1", opts1.DefaultValue)
		}
		if opts2.DefaultValue != "value2" {
			t.Errorf("opts2.DefaultValue = %v, want value2", opts2.DefaultValue)
		}
	})
}

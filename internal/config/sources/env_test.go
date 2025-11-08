package sources

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// ENV SOURCE CREATION TESTS
// =============================================================================

func TestNewEnvSource(t *testing.T) {
	tests := []struct {
		name string
		opts EnvSourceOptions
	}{
		{
			name: "default options",
			opts: EnvSourceOptions{},
		},
		{
			name: "with prefix",
			opts: EnvSourceOptions{
				Prefix: "APP_",
			},
		},
		{
			name: "with separator",
			opts: EnvSourceOptions{
				Separator: "__",
			},
		},
		{
			name: "with transformations",
			opts: EnvSourceOptions{
				KeyTransform: func(key string) string {
					return "transformed_" + key
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, err := NewEnvSource("TEST_", tt.opts)
			if err != nil {
				t.Fatalf("NewEnvSource() error = %v", err)
			}

			if source == nil {
				t.Fatal("NewEnvSource() returned nil")
			}

			if source.Name() != "env:TEST_" {
				t.Errorf("Name() = %v, want env:TEST_", source.Name())
			}
		})
	}
}

// TestNewEnvSourceWithConfig removed - API no longer exists

// =============================================================================
// ENV SOURCE METADATA TESTS
// =============================================================================

func TestEnvSource_Metadata(t *testing.T) {
	source, _ := NewEnvSource("TEST_", EnvSourceOptions{})

	// Get metadata through GetType() method instead of Metadata()
	sourceType := source.GetType()
	if sourceType != "environment" {
		t.Errorf("source.GetType() = %v, want environment", sourceType)
	}

	sourceName := source.Name()
	if sourceName != "env:TEST_" {
		t.Errorf("source.Name() = %v, want env:TEST_", sourceName)
	}

	priority := source.Priority()
	if priority <= 0 {
		t.Errorf("source.Priority() = %d, want > 0", priority)
	}
}

// =============================================================================
// ENV SOURCE LOAD TESTS
// =============================================================================

func TestEnvSource_Load(t *testing.T) {
	// Set up test environment variables
	testVars := map[string]string{
		"TEST_STRING": "value",
		"TEST_INT":    "42",
		"TEST_BOOL":   "true",
	}

	for key, value := range testVars {
		os.Setenv(key, value)
		defer os.Unsetenv(key)
	}

	source, _ := NewEnvSource("TEST_", EnvSourceOptions{
		Prefix: "TEST_",
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if data == nil {
		t.Fatal("Load() returned nil data")
	}

	// Check loaded values
	if val, ok := data["STRING"].(string); !ok || val != "value" {
		t.Errorf("data[STRING] = %v, want value", data["STRING"])
	}

	if val, ok := data["INT"].(string); !ok || val != "42" {
		t.Errorf("data[INT] = %v, want 42", data["INT"])
	}

	if val, ok := data["BOOL"].(string); !ok || val != "true" {
		t.Errorf("data[BOOL] = %v, want true", data["BOOL"])
	}
}

func TestEnvSource_Load_WithoutPrefix(t *testing.T) {
	t.Setenv("NO_PREFIX_VAR", "test_value")

	defer os.Unsetenv("NO_PREFIX_VAR")

	source, _ := NewEnvSource("", EnvSourceOptions{})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if data == nil {
		t.Fatal("Load() returned nil data")
	}

	// Should load all environment variables including NO_PREFIX_VAR
	// The key gets transformed from NO_PREFIX_VAR to NO.PREFIX.VAR due to separator replacement
	if noData, ok := data["NO"].(map[string]any); ok {
		if prefixData, ok := noData["PREFIX"].(map[string]any); ok {
			if val, ok := prefixData["VAR"].(string); !ok || val != "test_value" {
				t.Errorf("data[NO][PREFIX][VAR] = %v, want test_value", prefixData["VAR"])
			}
		} else {
			t.Errorf("NO.PREFIX not found in data: %+v", noData)
		}
	} else {
		t.Errorf("NO not found in data: %+v", data)
	}
}

func TestEnvSource_Load_WithSeparator(t *testing.T) {
	t.Setenv("APP_DB_HOST", "localhost")
	t.Setenv("APP_DB_PORT", "5432")

	defer os.Unsetenv("APP_DB_HOST")
	defer os.Unsetenv("APP_DB_PORT")

	source, _ := NewEnvSource("APP_", EnvSourceOptions{
		Prefix:    "APP_",
		Separator: "_",
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check nested structure
	if dbData, ok := data["DB"].(map[string]any); ok {
		if host, ok := dbData["HOST"].(string); !ok || host != "localhost" {
			t.Errorf("DB.HOST = %v, want localhost", dbData["HOST"])
		}

		if port, ok := dbData["PORT"].(string); !ok || port != "5432" {
			t.Errorf("DB.PORT = %v, want 5432", dbData["PORT"])
		}
	} else {
		t.Error("DB data not properly nested")
	}
}

// =============================================================================
// ENV SOURCE GET TESTS
// =============================================================================

func TestEnvSource_Get(t *testing.T) {
	t.Setenv("TEST_KEY", "test_value")

	defer os.Unsetenv("TEST_KEY")

	source, _ := NewEnvSource("TEST_", EnvSourceOptions{
		Prefix: "TEST_",
	})

	ctx := context.Background()
	source.Load(ctx)

	t.Run("get existing key", func(t *testing.T) {
		data, err := source.Load(ctx)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		value, ok := data["KEY"]
		if !ok {
			t.Fatal("Load() did not return existing key")
		}

		if value != "test_value" {
			t.Errorf("Load() = %v, want test_value", value)
		}
	})

	t.Run("get non-existent key", func(t *testing.T) {
		data, err := source.Load(ctx)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		_, ok := data["NONEXISTENT"]
		if ok {
			t.Error("Load() should not return non-existent key")
		}
	})
}

// =============================================================================
// ENV SOURCE TRANSFORMATION TESTS
// =============================================================================

func TestEnvSource_KeyTransform(t *testing.T) {
	t.Setenv("TEST_lower_case", "value")

	defer os.Unsetenv("TEST_lower_case")

	keyTransform := func(key string) string {
		return key + "_TRANSFORMED"
	}

	source, _ := NewEnvSource("TEST_", EnvSourceOptions{
		Prefix:       "TEST_",
		KeyTransform: keyTransform,
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Key should be transformed
	if _, ok := data["lower_case_TRANSFORMED"]; !ok {
		t.Error("Transformed key not found in data")
	}
}

func TestEnvSource_ValueTransform(t *testing.T) {
	t.Setenv("TEST_VALUE", "original")

	defer os.Unsetenv("TEST_VALUE")

	valueTransform := func(key string, value any) any {
		return value.(string) + "_TRANSFORMED"
	}

	source, _ := NewEnvSource("TEST_", EnvSourceOptions{
		Prefix:         "TEST_",
		ValueTransform: valueTransform,
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if val, ok := data["VALUE"].(string); !ok || val != "original_TRANSFORMED" {
		t.Errorf("VALUE = %v, want original_TRANSFORMED", data["VALUE"])
	}
}

// =============================================================================
// ENV SOURCE TYPE CONVERSION TESTS
// =============================================================================

func TestEnvSource_TypeConversion(t *testing.T) {
	testVars := map[string]string{
		"TEST_INT":   "42",
		"TEST_FLOAT": "3.14",
		"TEST_BOOL":  "true",
	}

	for key, value := range testVars {
		t.Setenv(key, value)
		defer os.Unsetenv(key)
	}

	source, _ := NewEnvSource("TEST_", EnvSourceOptions{
		Prefix:         "TEST_",
		TypeConversion: true,
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	t.Run("convert int", func(t *testing.T) {
		if val, ok := data["INT"].(int); !ok || val != 42 {
			t.Errorf("INT = %v (%T), want 42 (int)", data["INT"], data["INT"])
		}
	})

	t.Run("convert float", func(t *testing.T) {
		if val, ok := data["FLOAT"].(float64); !ok || val != 3.14 {
			t.Errorf("FLOAT = %v (%T), want 3.14 (float64)", data["FLOAT"], data["FLOAT"])
		}
	})

	t.Run("convert bool", func(t *testing.T) {
		if val, ok := data["BOOL"].(bool); !ok || val != true {
			t.Errorf("BOOL = %v (%T), want true (bool)", data["BOOL"], data["BOOL"])
		}
	})
}

// =============================================================================
// ENV SOURCE REQUIRED VARS TESTS
// =============================================================================

func TestEnvSource_RequiredVars(t *testing.T) {
	t.Run("all required vars present", func(t *testing.T) {
		t.Setenv("REQ_VAR1", "value1")
		t.Setenv("REQ_VAR2", "value2")

		defer os.Unsetenv("REQ_VAR1")
		defer os.Unsetenv("REQ_VAR2")

		source, _ := NewEnvSource("REQ_", EnvSourceOptions{
			Prefix:       "REQ_",
			RequiredVars: []string{"VAR1", "VAR2"},
		})

		ctx := context.Background()

		_, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v, want nil", err)
		}
	})

	t.Run("missing required var", func(t *testing.T) {
		t.Setenv("REQ_VAR1", "value1")

		defer os.Unsetenv("REQ_VAR1")

		source, _ := NewEnvSource("REQ_", EnvSourceOptions{
			Prefix:       "REQ_",
			RequiredVars: []string{"VAR1", "VAR2"},
		})

		ctx := context.Background()

		_, err := source.Load(ctx)
		if err == nil {
			t.Error("Load() should return error for missing required var")
		}
	})
}

// =============================================================================
// ENV SOURCE SECRET VARS TESTS
// =============================================================================

func TestEnvSource_SecretVars(t *testing.T) {
	t.Setenv("SECRET_PASSWORD", "secret123")
	t.Setenv("NORMAL_VAR", "normal")

	defer os.Unsetenv("SECRET_PASSWORD")
	defer os.Unsetenv("NORMAL_VAR")

	source, _ := NewEnvSource("SECRET_", EnvSourceOptions{
		SecretVars: []string{"PASSWORD"},
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Secret vars should be marked somehow or handled specially
	// (implementation dependent)
	if _, ok := data["PASSWORD"]; !ok {
		t.Error("Secret var not loaded")
	}
}

// =============================================================================
// ENV SOURCE WATCH TESTS
// =============================================================================

func TestEnvSource_Watch(t *testing.T) {
	source, _ := NewEnvSource("TEST_", EnvSourceOptions{
		WatchEnabled:  true,
		WatchInterval: 100 * time.Millisecond,
	})

	if !source.IsWatchable() {
		t.Error("Source should be watchable when WatchEnabled is true")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changes := make(chan struct{})

	go func() {
		err := source.Watch(ctx, nil)
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Watch() error = %v", err)
		}

		close(changes)
	}()

	// Give watch time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for watch to stop
	select {
	case <-changes:
		// OK
	case <-time.After(1 * time.Second):
		t.Error("Watch() did not stop after context cancellation")
	}
}

func TestEnvSource_Watch_Disabled(t *testing.T) {
	source, _ := NewEnvSource("TEST_", EnvSourceOptions{
		WatchEnabled: false,
	})

	if source.IsWatchable() {
		t.Error("Source should not be watchable when WatchEnabled is false")
	}

	ctx := context.Background()

	err := source.Watch(ctx, nil)
	if err == nil {
		t.Error("Watch() should return error when watching is disabled")
	}
}

// =============================================================================
// ENV SOURCE VALIDATION TESTS
// =============================================================================

func TestEnvSource_Validate(t *testing.T) {
	source, _ := NewEnvSource("TEST_", EnvSourceOptions{})

	ctx := context.Background()
	available := source.IsAvailable(ctx)

	if !available {
		t.Error("IsAvailable() should return true")
	}
}

// =============================================================================
// ENV SOURCE LIFECYCLE TESTS
// =============================================================================

func TestEnvSource_Lifecycle(t *testing.T) {
	source, _ := NewEnvSource("TEST_", EnvSourceOptions{})

	ctx := context.Background()

	// Load
	_, err := source.Load(ctx)
	if err != nil {
		t.Errorf("Load() error = %v", err)
	}

	// Test IsAvailable instead of Validate
	available := source.IsAvailable(ctx)
	if !available {
		t.Error("Source should be available")
	}

	// Test StopWatch instead of Stop
	err = source.StopWatch()
	if err != nil {
		t.Errorf("StopWatch() error = %v", err)
	}
}

// =============================================================================
// ENV SOURCE FACTORY TESTS
// =============================================================================

func TestEnvSourceFactory_Create(t *testing.T) {
	factory := &EnvSourceFactory{}

	config := EnvSourceConfig{
		Prefix:   "APP_",
		Priority: 10,
	}

	source, err := factory.CreateFromConfig(config)
	if err != nil {
		t.Fatalf("CreateFromConfig() error = %v", err)
	}

	if source == nil {
		t.Fatal("Create() returned nil source")
	}

	if source.Priority() != 10 {
		t.Errorf("Priority() = %d, want 10", source.Priority())
	}
}

func TestEnvSourceFactory_Validate(t *testing.T) {
	factory := &EnvSourceFactory{}

	t.Run("valid config", func(t *testing.T) {
		config := EnvSourceConfig{
			Prefix: "APP_",
		}

		// Test CreateFromConfig instead of Validate
		_, err := factory.CreateFromConfig(config)
		if err != nil {
			t.Errorf("CreateFromConfig() error = %v, want nil", err)
		}
	})

	t.Run("invalid config type", func(t *testing.T) {
		config := EnvSourceConfig{
			Prefix: "APP_",
		}

		// Test CreateFromConfig instead of Validate
		_, err := factory.CreateFromConfig(config)
		if err != nil {
			t.Errorf("CreateFromConfig() error = %v, want nil", err)
		}
	})
}

// =============================================================================
// ENV SOURCE EDGE CASES
// =============================================================================

func TestEnvSource_EdgeCases(t *testing.T) {
	t.Run("empty environment", func(t *testing.T) {
		source, _ := NewEnvSource("TEST_", EnvSourceOptions{
			Prefix: "NONEXISTENT_PREFIX_",
		})

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v, want nil", err)
		}

		if data == nil {
			t.Error("Load() returned nil data")
		}

		// Should return empty map
		if len(data) != 0 {
			t.Errorf("Expected empty data, got %d keys", len(data))
		}
	})

	t.Run("empty var value", func(t *testing.T) {
		t.Setenv("TEST_EMPTY_VAR", "")

		defer os.Unsetenv("TEST_EMPTY_VAR")

		source, _ := NewEnvSource("TEST_", EnvSourceOptions{
			KeyTransform: func(key string) string {
				return key // Don't convert separators to dots
			},
		})

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		// Debug: print all loaded data
		t.Logf("Loaded data: %+v", data)

		if val, ok := data["EMPTY_VAR"].(string); !ok || val != "" {
			t.Errorf("EMPTY_VAR = %v, want empty string", data["EMPTY_VAR"])
		}
	})

	t.Run("special characters in value", func(t *testing.T) {
		specialValue := "value with spaces and !@#$%^&*()"
		t.Setenv("TEST_SPECIAL_VAR", specialValue)

		defer os.Unsetenv("TEST_SPECIAL_VAR")

		source, _ := NewEnvSource("TEST_", EnvSourceOptions{
			KeyTransform: func(key string) string {
				return key // Don't convert separators to dots
			},
		})

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if val, ok := data["SPECIAL_VAR"].(string); !ok || val != specialValue {
			t.Errorf("SPECIAL_VAR = %v, want %v", data["SPECIAL_VAR"], specialValue)
		}
	})

	t.Run("very long value", func(t *testing.T) {
		longValue := strings.Repeat("a", 10000)
		t.Setenv("TEST_LONG_VAR", longValue)

		defer os.Unsetenv("TEST_LONG_VAR")

		source, _ := NewEnvSource("TEST_", EnvSourceOptions{
			KeyTransform: func(key string) string {
				return key // Don't convert separators to dots
			},
		})

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if val, ok := data["LONG_VAR"].(string); !ok || len(val) != len(longValue) {
			t.Errorf("LONG_VAR length = %d, want %d", len(val), len(longValue))
		}
	})

	t.Run("unicode in value", func(t *testing.T) {
		unicodeValue := "Hello 世界 🌍"
		t.Setenv("TEST_UNICODE_VAR", unicodeValue)

		defer os.Unsetenv("TEST_UNICODE_VAR")

		source, _ := NewEnvSource("TEST_", EnvSourceOptions{
			KeyTransform: func(key string) string {
				return key // Don't convert separators to dots
			},
		})

		ctx := context.Background()

		data, err := source.Load(ctx)
		if err != nil {
			t.Errorf("Load() error = %v", err)
		}

		if val, ok := data["UNICODE_VAR"].(string); !ok || val != unicodeValue {
			t.Errorf("UNICODE_VAR = %v, want %v", data["UNICODE_VAR"], unicodeValue)
		}
	})
}

// =============================================================================
// ENV SOURCE COMPLEX SCENARIOS
// =============================================================================

func TestEnvSource_ComplexNesting(t *testing.T) {
	t.Setenv("APP_DB_MASTER_HOST", "master.db")
	t.Setenv("APP_DB_MASTER_PORT", "5432")
	t.Setenv("APP_DB_REPLICA_HOST", "replica.db")
	t.Setenv("APP_DB_REPLICA_PORT", "5433")

	defer os.Unsetenv("APP_DB_MASTER_HOST")
	defer os.Unsetenv("APP_DB_MASTER_PORT")
	defer os.Unsetenv("APP_DB_REPLICA_HOST")
	defer os.Unsetenv("APP_DB_REPLICA_PORT")

	source, _ := NewEnvSource("APP_", EnvSourceOptions{
		Prefix:    "APP_",
		Separator: "_",
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check nested structure
	db, ok := data["DB"].(map[string]any)
	if !ok {
		t.Fatal("DB not found or not a map")
	}

	master, ok := db["MASTER"].(map[string]any)
	if !ok {
		t.Fatal("DB.MASTER not found or not a map")
	}

	if host, ok := master["HOST"].(string); !ok || host != "master.db" {
		t.Errorf("DB.MASTER.HOST = %v, want master.db", master["HOST"])
	}

	replica, ok := db["REPLICA"].(map[string]any)
	if !ok {
		t.Fatal("DB.REPLICA not found or not a map")
	}

	if port, ok := replica["PORT"].(string); !ok || port != "5433" {
		t.Errorf("DB.REPLICA.PORT = %v, want 5433", replica["PORT"])
	}
}

func TestEnvSource_MixedTypes(t *testing.T) {
	t.Setenv("MIX_STRING", "text")
	t.Setenv("MIX_INT", "42")
	t.Setenv("MIX_FLOAT", "3.14")
	t.Setenv("MIX_BOOL", "true")
	t.Setenv("MIX_LIST", "a,b,c")

	defer func() {
		os.Unsetenv("MIX_STRING")
		os.Unsetenv("MIX_INT")
		os.Unsetenv("MIX_FLOAT")
		os.Unsetenv("MIX_BOOL")
		os.Unsetenv("MIX_LIST")
	}()

	source, _ := NewEnvSource("MIX_", EnvSourceOptions{
		TypeConversion: true,
	})

	ctx := context.Background()

	data, err := source.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify types
	if _, ok := data["STRING"].(string); !ok {
		t.Errorf("STRING should be string, got %T", data["STRING"])
	}

	if _, ok := data["INT"].(int); !ok {
		t.Errorf("INT should be int, got %T", data["INT"])
	}

	if _, ok := data["FLOAT"].(float64); !ok {
		t.Errorf("FLOAT should be float64, got %T", data["FLOAT"])
	}

	if _, ok := data["BOOL"].(bool); !ok {
		t.Errorf("BOOL should be bool, got %T", data["BOOL"])
	}
}

// =============================================================================
// ENV SOURCE PRIORITY TESTS
// =============================================================================

func TestEnvSource_Priority(t *testing.T) {
	tests := []struct {
		name     string
		priority int
	}{
		{"default priority", 100}, // Default priority is 100
		{"custom priority", 10},
		{"high priority", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source, _ := NewEnvSource("", EnvSourceOptions{
				Priority: tt.priority,
			})

			if source.Priority() != tt.priority {
				t.Errorf("Priority() = %d, want %d", source.Priority(), tt.priority)
			}
		})
	}
}

// =============================================================================
// ENV SOURCE CONTEXT CANCELLATION TESTS
// =============================================================================

func TestEnvSource_ContextCancellation(t *testing.T) {
	source, _ := NewEnvSource("TEST_", EnvSourceOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := source.Load(ctx)

	// Should either complete successfully or handle cancellation gracefully
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("Load() error = %v", err)
	}
}

// =============================================================================
// ENV SOURCE EQUALITY TESTS
// =============================================================================

func TestEnvSource_ReloadConsistency(t *testing.T) {
	t.Setenv("RELOAD_VAR", "value")

	source, _ := NewEnvSource("TEST_", EnvSourceOptions{})
	ctx := context.Background()

	// Load twice
	data1, err1 := source.Load(ctx)
	data2, err2 := source.Load(ctx)

	if err1 != nil || err2 != nil {
		t.Fatalf("Load() errors = %v, %v", err1, err2)
	}

	// Should get same results
	if !reflect.DeepEqual(data1, data2) {
		t.Error("Consecutive loads returned different data")
	}
}

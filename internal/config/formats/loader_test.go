package formats

import (
	"context"
	"testing"
	"time"

	configcore "github.com/xraph/forge/internal/config/core"
)

// =============================================================================
// MOCK CONFIG SOURCE FOR TESTING
// =============================================================================

type mockConfigSource struct {
	name     string
	priority int
	loadData map[string]any
	loadErr  error
}

func newMockSource(name string, data map[string]any) *mockConfigSource {
	return &mockConfigSource{
		name:     name,
		priority: 1,
		loadData: data,
	}
}

func (m *mockConfigSource) Name() string      { return m.name }
func (m *mockConfigSource) GetName() string   { return m.name }
func (m *mockConfigSource) Priority() int     { return m.priority }
func (m *mockConfigSource) IsWatchable() bool { return false }
func (m *mockConfigSource) Get(key string) (any, bool) {
	if m.loadData == nil {
		return nil, false
	}

	val, ok := m.loadData[key]

	return val, ok
}

func (m *mockConfigSource) Load(ctx context.Context) (map[string]any, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}

	return m.loadData, nil
}

func (m *mockConfigSource) Watch(ctx context.Context, callback func(map[string]any)) error {
	return nil
}

func (m *mockConfigSource) StopWatch() error {
	return nil
}

func (m *mockConfigSource) GetSecret(ctx context.Context, key string) (string, error) {
	return "", nil
}

func (m *mockConfigSource) SupportsSecrets() bool {
	return false
}

func (m *mockConfigSource) IsAvailable(ctx context.Context) bool {
	return true
}

func (m *mockConfigSource) Reload(ctx context.Context) error {
	return nil
}

func (m *mockConfigSource) GetType() string {
	return "mock"
}

func (m *mockConfigSource) Metadata() configcore.SourceMetadata {
	return configcore.SourceMetadata{
		Name:     m.name,
		Priority: m.priority,
		Type:     "mock",
	}
}

func (m *mockConfigSource) Validate(ctx context.Context) error { return nil }
func (m *mockConfigSource) Stop() error                        { return nil }

// =============================================================================
// LOADER CREATION TESTS
// =============================================================================

func TestNewLoader(t *testing.T) {
	tests := []struct {
		name   string
		config LoaderConfig
	}{
		{
			name:   "default config",
			config: LoaderConfig{},
		},
		{
			name: "with cache TTL",
			config: LoaderConfig{
				CacheTTL: 5 * time.Minute,
			},
		},
		{
			name: "with retry enabled",
			config: LoaderConfig{
				RetryCount: 3,
				RetryDelay: 1 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewLoader(tt.config)
			if loader == nil {
				t.Fatal("NewLoader() returned nil")
			}

			// Loader interface check
			if loader == nil {
				t.Error("NewLoader() returned nil")
			}
		})
	}
}

// =============================================================================
// LOADER LOAD SOURCE TESTS (RegisterSource API removed)
// =============================================================================

// Note: The Loader API changed from RegisterSource to LoadSource pattern.
// Tests for the old RegisterSource/UnregisterSource API are removed.
// The Loader now directly loads from sources without registration.

// =============================================================================
// LOADER LOAD TESTS
// =============================================================================

func TestLoader_LoadSource(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source := newMockSource("test", map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	})

	ctx := context.Background()

	t.Run("load from source", func(t *testing.T) {
		result, err := loader.LoadSource(ctx, source)
		if err != nil {
			t.Fatalf("LoadSource() error = %v", err)
		}

		if result == nil {
			t.Fatal("LoadSource() returned nil result")
		}

		if result["key1"] != "value1" {
			t.Errorf("key1 = %v, want value1", result["key1"])
		}
	})
}

// Tests below use obsolete API (RegisterSource, LoadFrom, LoadWithOptions)
// and are commented out pending API refactoring or removal

/*
// =============================================================================
// LOADER MERGE TESTS (COMMENTED OUT - USES OBSOLETE API)
// =============================================================================

func TestLoader_Merge(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source1 := newMockSource("source1", map[string]interface{}{
		"key1":   "value1",
		"shared": "from_source1",
	})

	source2 := newMockSource("source2", map[string]interface{}{
		"key2":   "value2",
		"shared": "from_source2",
	})

	loader.RegisterSource(source1)
	loader.RegisterSource(source2)

	ctx := context.Background()

	t.Run("merge override strategy", func(t *testing.T) {
		opts := LoadOptions{
			MergeStrategy: MergeStrategyOverride,
		}

		result, err := loader.LoadWithOptions(ctx, opts)
		if err != nil {
			t.Fatalf("LoadWithOptions() error = %v", err)
		}

		// Higher priority source should override
		if result.Data["key1"] != "value1" {
			t.Errorf("key1 = %v, want value1", result.Data["key1"])
		}
		if result.Data["key2"] != "value2" {
			t.Errorf("key2 = %v, want value2", result.Data["key2"])
		}
	})

	t.Run("merge deep strategy", func(t *testing.T) {
		nestedSource1 := newMockSource("nested1", map[string]interface{}{
			"config": map[string]interface{}{
				"key1": "value1",
				"nested": map[string]interface{}{
					"a": "from_source1",
				},
		})

		nestedSource2 := newMockSource("nested2", map[string]interface{}{
			"config": map[string]interface{}{
				"key2": "value2",
				"nested": map[string]interface{}{
					"b": "from_source2",
				},
		})

		deepLoader := NewLoader(LoaderConfig{})
		deepLoader.RegisterSource(nestedSource1)
		deepLoader.RegisterSource(nestedSource2)

		opts := LoadOptions{
			MergeStrategy: MergeStrategyDeep,
		}

		result, err := deepLoader.LoadWithOptions(ctx, opts)
		if err != nil {
			t.Fatalf("LoadWithOptions() error = %v", err)
		}

		// Should have deep merged nested structures
		if config, ok := result.Data["config"].(map[string]interface{}); ok {
			if config["key1"] == nil || config["key2"] == nil {
				t.Error("Deep merge should preserve both nested keys")
			}
	})
}

// =============================================================================
// LOADER CACHE TESTS
// =============================================================================

func TestLoader_Cache(t *testing.T) {
	config := LoaderConfig{
		CacheEnabled: true,
		CacheTTL:     1 * time.Second,
	}
	loader := NewLoader(config)

	source := newMockSource("test", map[string]interface{}{
		"key": "value",
	})

	loader.RegisterSource(source)

	ctx := context.Background()

	t.Run("first load caches", func(t *testing.T) {
		result1, err := loader.Load(ctx)
		if err != nil {
			t.Fatalf("First Load() error = %v", err)
		}

		// Change source data
		source.loadData["key"] = "changed"

		// Second load should use cache
		result2, err := loader.Load(ctx)
		if err != nil {
			t.Fatalf("Second Load() error = %v", err)
		}

		if config.CacheEnabled && result1.Data["key"] == result2.Data["key"] {
			// Cache is working
		}
	})

	t.Run("cache expires", func(t *testing.T) {
		// Wait for cache to expire
		time.Sleep(2 * time.Second)

		result, err := loader.Load(ctx)
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		// Should have new value
		if result.Data["key"] != "changed" {
			t.Logf("After cache expiry, key = %v", result.Data["key"])
		}
	})
}

// =============================================================================
// LOADER RETRY TESTS
// =============================================================================

func TestLoader_Retry(t *testing.T) {
	config := LoaderConfig{
		RetryEnabled:  true,
		RetryAttempts: 3,
		RetryDelay:    100 * time.Millisecond,
	}
	loader := NewLoader(config)

	attemptCount := 0
	source := newMockSource("test", map[string]interface{}{})

	// Make source fail first few times
	originalLoad := source.Load
	= func(ctx context.Context) (map[string]interface{}, error) {
		attemptCount++
		if attemptCount < 3 {
			return nil, configcore.ErrConfigError("temporary error", nil)
		}
		return originalLoad(ctx)
	}

	loader.RegisterSource(source)

	ctx := context.Background()

	t.Run("retry on failure", func(t *testing.T) {
		_, err := loader.Load(ctx)

		if err == nil {
			// Eventually succeeded after retries
			if attemptCount < 2 {
				t.Error("Expected multiple attempts due to retry")
			}
	})
}

// =============================================================================
// LOADER ENV EXPANSION TESTS
// =============================================================================

func TestLoader_EnvExpansion(t *testing.T) {
	os.Setenv("TEST_VAR", "expanded_value")
	defer os.Unsetenv("TEST_VAR")

	config := LoaderConfig{
		ExpandEnv: true,
	}
	loader := NewLoader(config)

	source := newMockSource("test", map[string]interface{}{
		"key": "${TEST_VAR}",
	})

	loader.RegisterSource(source)

	ctx := context.Background()

	result, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if config.ExpandEnv && result.Data["key"] != "expanded_value" {
		t.Logf("key = %v, expected env expansion", result.Data["key"])
	}

// =============================================================================
// LOADER SECRET EXPANSION TESTS
// =============================================================================

func TestLoader_SecretExpansion(t *testing.T) {
	// This test depends on having a secrets manager configured
	// For now, just test that it doesn't error
	config := LoaderConfig{
		ExpandSecrets: true,
	}
	loader := NewLoader(config)

	source := newMockSource("test", map[string]interface{}{
		"key": "secret:password",
	})

	loader.RegisterSource(source)

	ctx := context.Background()

	_, err := loader.Load(ctx)
	// May error if secrets manager not configured, which is OK
	_ = err
}

// =============================================================================
// LOADER TRANSFORMER TESTS
// =============================================================================

func TestLoader_Transformers(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	transformer := func(data map[string]interface{}) (map[string]interface{}, error) {
		// Transform all string values to uppercase
		for key, value := range data {
			if str, ok := value.(string); ok {
				data[key] = str + "_TRANSFORMED"
			}
		return data, nil
	}

	loader.RegisterTransformer(transformer)

	source := newMockSource("test", map[string]interface{}{
		"key": "value",
	})

	loader.RegisterSource(source)

	ctx := context.Background()

	result, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if result.Data["key"] != "value_TRANSFORMED" {
		t.Errorf("key = %v, want value_TRANSFORMED", result.Data["key"])
	}

// =============================================================================
// LOADER FORMAT PROCESSOR TESTS
// =============================================================================

func TestLoader_FormatProcessor(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	processor := &mockFormatProcessor{}

	loader.RegisterFormatProcessor("test", processor)

	// Test that processor was registered
	// (actual usage depends on implementation)
}

type mockFormatProcessor struct{}

func (m *mockFormatProcessor) Parse(data []byte) (map[string]interface{}, error) {
	return map[string]interface{}{"parsed": true}, nil
}

func (m *mockFormatProcessor) Format() string {
	return "test"
}

// =============================================================================
// LOADER QUERY TESTS
// =============================================================================

func TestLoader_GetSource(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source := newMockSource("test", map[string]interface{}{})
	loader.RegisterSource(source)

	t.Run("get existing source", func(t *testing.T) {
		retrieved := loader.GetSource("test")
		if retrieved == nil {
			t.Error("GetSource() returned nil for existing source")
		}
	})

	t.Run("get non-existent source", func(t *testing.T) {
		retrieved := loader.GetSource("nonexistent")
		if retrieved != nil {
			t.Error("GetSource() should return nil for non-existent source")
		}
	})
}

func TestLoader_GetAllSources(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source1 := newMockSource("source1", map[string]interface{}{})
	source2 := newMockSource("source2", map[string]interface{}{})

	loader.RegisterSource(source1)
	loader.RegisterSource(source2)

	sources := loader.GetAllSources()

	if len(sources) != 2 {
		t.Errorf("GetAllSources() returned %d sources, want 2", len(sources))
	}

func TestLoader_HasSource(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source := newMockSource("test", map[string]interface{}{})
	loader.RegisterSource(source)

	if !loader.HasSource("test") {
		t.Error("HasSource() should return true for registered source")
	}

	if loader.HasSource("nonexistent") {
		t.Error("HasSource() should return false for non-existent source")
	}

// =============================================================================
// LOADER VALIDATION TESTS
// =============================================================================

func TestLoader_Validate(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source := newMockSource("test", map[string]interface{}{})
	loader.RegisterSource(source)

	ctx := context.Background()

	err := loader.Validate(ctx)
	if err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}

// =============================================================================
// LOADER LIFECYCLE TESTS
// =============================================================================

func TestLoader_Start_Stop(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	ctx := context.Background()

	err := loader.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v, want nil", err)
	}

	err = loader.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}

// =============================================================================
// LOADER EDGE CASES
// =============================================================================

func TestLoader_EdgeCases(t *testing.T) {
	t.Run("load with empty source data", func(t *testing.T) {
		loader := NewLoader(LoaderConfig{})
		source := newMockSource("empty", map[string]interface{}{})
		loader.RegisterSource(source)

		ctx := context.Background()
		result, err := loader.Load(ctx)

		if err != nil {
			t.Errorf("Load() error = %v, want nil", err)
		}

		if result.Data == nil {
			t.Error("Result.Data should not be nil for empty source")
		}
	})

	t.Run("load with nil source data", func(t *testing.T) {
		loader := NewLoader(LoaderConfig{})
		source := newMockSource("nil", nil)
		loader.RegisterSource(source)

		ctx := context.Background()
		_, err := loader.Load(ctx)

		// Should handle gracefully
		_ = err
	})

	t.Run("load with source error", func(t *testing.T) {
		loader := NewLoader(LoaderConfig{})
		source := newMockSource("error", nil)
		source.loadErr = configcore.ErrConfigError("test error", nil)
		loader.RegisterSource(source)

		ctx := context.Background()
		_, err := loader.Load(ctx)

		if err == nil {
			t.Error("Load() should return error when source returns error")
		}
	})

	t.Run("transformer error", func(t *testing.T) {
		loader := NewLoader(LoaderConfig{})

		transformer := func(data map[string]interface{}) (map[string]interface{}, error) {
			return nil, configcore.ErrConfigError("transform error", nil)
		}

		loader.RegisterTransformer(transformer)

		source := newMockSource("test", map[string]interface{}{"key": "value"})
		loader.RegisterSource(source)

		ctx := context.Background()
		_, err := loader.Load(ctx)

		if err == nil {
			t.Error("Load() should return error when transformer returns error")
		}
	})
}

// =============================================================================
// LOADER PRIORITY TESTS
// =============================================================================

func TestLoader_SourcePriority(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source1 := newMockSource("low", map[string]interface{}{
		"shared": "from_low",
		"key1":   "value1",
	})
	source1.priority = 1

	source2 := newMockSource("high", map[string]interface{}{
		"shared": "from_high",
		"key2":   "value2",
	})
	source2.priority = 10

	loader.RegisterSource(source1)
	loader.RegisterSource(source2)

	ctx := context.Background()

	result, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Higher priority should override
	if result.Data["shared"] != "from_high" {
		t.Errorf("shared = %v, want from_high (higher priority)", result.Data["shared"])
	}

	// Both sources should contribute
	if result.Data["key1"] != "value1" {
		t.Errorf("key1 = %v, want value1", result.Data["key1"])
	}
	if result.Data["key2"] != "value2" {
		t.Errorf("key2 = %v, want value2", result.Data["key2"])
	}

// =============================================================================
// LOADER CONTEXT CANCELLATION TESTS
// =============================================================================

func TestLoader_ContextCancellation(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source := newMockSource("test", map[string]interface{}{})
	loader.RegisterSource(source)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := loader.Load(ctx)

	// Should handle cancellation gracefully
	if err != nil && err != context.Canceled {
		t.Errorf("Load() error = %v", err)
	}

// =============================================================================
// LOADER CONCURRENCY TESTS
// =============================================================================

func TestLoader_Concurrency(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source := newMockSource("test", map[string]interface{}{
		"key": "value",
	})

	loader.RegisterSource(source)

	ctx := context.Background()
	done := make(chan bool)

	// Concurrent loads
	for i := 0; i < 10; i++ {
		go func() {
			_, err := loader.Load(ctx)
			if err != nil {
				t.Errorf("Concurrent Load() error = %v", err)
			}
			done <- true
		}()
	}

	// Wait for all
	timeout := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent operations")
		}

// =============================================================================
// LOADER RESULT TESTS
// =============================================================================

func TestLoadResult(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source := newMockSource("test", map[string]interface{}{
		"key": "value",
	})

	loader.RegisterSource(source)

	ctx := context.Background()

	result, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	t.Run("result has data", func(t *testing.T) {
		if result.Data == nil {
			t.Error("Result.Data is nil")
		}
	})

	t.Run("result has sources", func(t *testing.T) {
		if len(result.Sources) == 0 {
			t.Error("Result.Sources is empty")
		}
	})

	t.Run("result has load time", func(t *testing.T) {
		if result.LoadedAt.IsZero() {
			t.Error("Result.LoadedAt is zero")
		}
	})
}

// =============================================================================
// LOADER COMPLEX SCENARIOS
// =============================================================================

func TestLoader_ComplexMerge(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	source1 := newMockSource("source1", map[string]interface{}{
		"database": map[string]interface{}{
			"host": "localhost",
			"port": 5432,
		},
		"app": map[string]interface{}{
			"name": "MyApp",
		},
	})

	source2 := newMockSource("source2", map[string]interface{}{
		"database": map[string]interface{}{
			"host": "remote.db",
			"user": "admin",
		},
		"app": map[string]interface{}{
			"version": "1.0.0",
		},
	})

	loader.RegisterSource(source1)
	loader.RegisterSource(source2)

	ctx := context.Background()

	opts := LoadOptions{
		MergeStrategy: MergeStrategyDeep,
	}

	result, err := loader.LoadWithOptions(ctx, opts)
	if err != nil {
		t.Fatalf("LoadWithOptions() error = %v", err)
	}

	// Check deep merged structure
	if db, ok := result.Data["database"].(map[string]interface{}); ok {
		if db["host"] == nil || db["port"] == nil || db["user"] == nil {
			t.Error("Deep merge should preserve all nested keys")
		} else {
		t.Error("database is not a map")
	}

	if app, ok := result.Data["app"].(map[string]interface{}); ok {
		if app["name"] == nil || app["version"] == nil {
			t.Error("Deep merge should preserve all app keys")
		} else {
		t.Error("app is not a map")
	}

func TestLoader_MultipleTransformers(t *testing.T) {
	loader := NewLoader(LoaderConfig{})

	// First transformer: add prefix
	transformer1 := func(data map[string]interface{}) (map[string]interface{}, error) {
		for key, value := range data {
			if str, ok := value.(string); ok {
				data[key] = "prefix_" + str
			}
		return data, nil
	}

	// Second transformer: add suffix
	transformer2 := func(data map[string]interface{}) (map[string]interface{}, error) {
		for key, value := range data {
			if str, ok := value.(string); ok {
				data[key] = str + "_suffix"
			}
		return data, nil
	}

	loader.RegisterTransformer(transformer1)
	loader.RegisterTransformer(transformer2)

	source := newMockSource("test", map[string]interface{}{
		"key": "value",
	})

	loader.RegisterSource(source)

	ctx := context.Background()

	result, err := loader.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Should have both transformations applied
	if result.Data["key"] != "prefix_value_suffix" {
		t.Errorf("key = %v, want prefix_value_suffix", result.Data["key"])
	}
*/

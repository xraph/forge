package config

import (
	"reflect"
	"sync"
	"testing"
	"time"

	configcore "github.com/xraph/forge/internal/config/core"
)

// =============================================================================
// MANAGER CREATION TESTS
// =============================================================================

func TestNewManager(t *testing.T) {
	tests := []struct {
		name   string
		config ManagerConfig
		want   string
	}{
		{
			name:   "default config",
			config: ManagerConfig{},
			want:   ManagerKey,
		},
		{
			name: "custom config",
			config: ManagerConfig{
				WatchInterval:   60 * time.Second,
				ErrorRetryCount: 5,
				SecretsEnabled:  true,
			},
			want: ManagerKey,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(tt.config)
			if manager == nil {
				t.Fatal("NewManager() returned nil")
			}

			if name := manager.Name(); name != tt.want {
				t.Errorf("Name() = %v, want %v", name, tt.want)
			}

			// Verify default values are set
			if m, ok := manager.(*Manager); ok {
				if m.data == nil {
					t.Error("data map not initialized")
				}

				if m.watchCallbacks == nil {
					t.Error("watchCallbacks map not initialized")
				}

				if m.changeCallbacks == nil {
					t.Error("changeCallbacks slice not initialized")
				}
			}
		})
	}
}

// =============================================================================
// BASIC GETTER TESTS
// =============================================================================

func TestManager_Get(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	// Set some test data
	manager.data = map[string]any{
		"string": "value",
		"int":    42,
		"bool":   true,
		"float":  3.14,
		"nested": map[string]any{
			"key": "nested_value",
		},
	}

	tests := []struct {
		name string
		key  string
		want any
	}{
		{"string value", "string", "value"},
		{"int value", "int", 42},
		{"bool value", "bool", true},
		{"float value", "float", 3.14},
		{"nested value", "nested.key", "nested_value"},
		{"non-existent", "nonexistent", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.Get(tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Get(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_GetString(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"string": "value",
		"int":    42,
		"empty":  "",
	}

	tests := []struct {
		name         string
		key          string
		defaultValue []string
		want         string
	}{
		{"existing string", "string", nil, "value"},
		{"int converted", "int", nil, "42"},
		{"empty string", "empty", nil, ""},
		{"missing with default", "missing", []string{"default"}, "default"},
		{"missing without default", "nonexistent", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetString(tt.key, tt.defaultValue...)
			if got != tt.want {
				t.Errorf("GetString(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_GetInt(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"int":     42,
		"int8":    int8(10),
		"int16":   int16(100),
		"int32":   int32(1000),
		"int64":   int64(10000),
		"float64": float64(99.9),
		"string":  "123",
	}

	tests := []struct {
		name         string
		key          string
		defaultValue []int
		want         int
	}{
		{"int", "int", nil, 42},
		{"int8", "int8", nil, 10},
		{"int16", "int16", nil, 100},
		{"int32", "int32", nil, 1000},
		{"int64", "int64", nil, 10000},
		{"float64", "float64", nil, 99},
		{"string", "string", nil, 123},
		{"missing with default", "missing", []int{999}, 999},
		{"missing without default", "nonexistent", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetInt(tt.key, tt.defaultValue...)
			if got != tt.want {
				t.Errorf("GetInt(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_GetBool(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"bool_true":    true,
		"bool_false":   false,
		"string_true":  "true",
		"string_false": "false",
		"int_true":     1,
		"int_false":    0,
	}

	tests := []struct {
		name         string
		key          string
		defaultValue []bool
		want         bool
	}{
		{"bool true", "bool_true", nil, true},
		{"bool false", "bool_false", nil, false},
		{"string true", "string_true", nil, true},
		{"string false", "string_false", nil, false},
		{"int true", "int_true", nil, true},
		{"int false", "int_false", nil, false},
		{"missing with default", "missing", []bool{true}, true},
		{"missing without default", "nonexistent", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetBool(tt.key, tt.defaultValue...)
			if got != tt.want {
				t.Errorf("GetBool(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_GetDuration(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"duration": 5 * time.Second,
		"string":   "10s",
		"int":      30,
	}

	tests := []struct {
		name         string
		key          string
		defaultValue []time.Duration
		want         time.Duration
	}{
		{"duration", "duration", nil, 5 * time.Second},
		{"string", "string", nil, 10 * time.Second},
		{"int", "int", nil, 30 * time.Second},
		{"missing with default", "missing", []time.Duration{2 * time.Minute}, 2 * time.Minute},
		{"missing without default", "nonexistent", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetDuration(tt.key, tt.defaultValue...)
			if got != tt.want {
				t.Errorf("GetDuration(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_GetStringSlice(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"slice":     []string{"a", "b", "c"},
		"interface": []any{"x", "y", "z"},
		"comma":     "one,two,three",
	}

	tests := []struct {
		name         string
		key          string
		defaultValue [][]string
		want         []string
	}{
		{"string slice", "slice", nil, []string{"a", "b", "c"}},
		{"interface slice", "interface", nil, []string{"x", "y", "z"}},
		{"comma separated", "comma", nil, []string{"one", "two", "three"}},
		{"missing with default", "missing", [][]string{{"default"}}, []string{"default"}},
		{"missing without default", "nonexistent", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetStringSlice(tt.key, tt.defaultValue...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetStringSlice(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_GetStringMap(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"map_string":    map[string]string{"key": "value"},
		"map_interface": map[string]any{"foo": "bar"},
	}

	tests := []struct {
		name         string
		key          string
		defaultValue []map[string]string
		want         map[string]string
	}{
		{"string map", "map_string", nil, map[string]string{"key": "value"}},
		{"interface map", "map_interface", nil, map[string]string{"foo": "bar"}},
		{"missing with default", "missing", []map[string]string{{"default": "val"}}, map[string]string{"default": "val"}},
		{"missing without default", "nonexistent", nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetStringMap(tt.key, tt.defaultValue...)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetStringMap(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_GetSizeInBytes(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"int":       1024,
		"uint":      uint64(2048),
		"string_kb": "10KB",
		"string_mb": "5MB",
		"string_gb": "2GB",
	}

	tests := []struct {
		name         string
		key          string
		defaultValue []uint64
		want         uint64
	}{
		{"int", "int", nil, 1024},
		{"uint", "uint", nil, 2048},
		{"string KB", "string_kb", nil, 10 * 1024},
		{"string MB", "string_mb", nil, 5 * 1024 * 1024},
		{"string GB", "string_gb", nil, 2 * 1024 * 1024 * 1024},
		{"missing with default", "missing", []uint64{4096}, 4096},
		{"missing without default", "nonexistent", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetSizeInBytes(tt.key, tt.defaultValue...)
			if got != tt.want {
				t.Errorf("GetSizeInBytes(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

// =============================================================================
// ADVANCED GET WITH OPTIONS TESTS
// =============================================================================

func TestManager_GetWithOptions(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"value": "test",
		"empty": "",
	}

	t.Run("required key exists", func(t *testing.T) {
		val, err := manager.GetWithOptions("value", WithRequired())
		if err != nil {
			t.Errorf("GetWithOptions() error = %v, want nil", err)
		}

		if val != "test" {
			t.Errorf("GetWithOptions() = %v, want %v", val, "test")
		}
	})

	t.Run("required key missing", func(t *testing.T) {
		_, err := manager.GetWithOptions("missing", WithRequired())
		if err == nil {
			t.Error("GetWithOptions() expected error for required missing key")
		}
	})

	t.Run("with default", func(t *testing.T) {
		val, err := manager.GetWithOptions("missing", WithDefault("default"))
		if err != nil {
			t.Errorf("GetWithOptions() error = %v, want nil", err)
		}

		if val != "default" {
			t.Errorf("GetWithOptions() = %v, want %v", val, "default")
		}
	})

	t.Run("with transform", func(t *testing.T) {
		transform := func(v any) any {
			return "transformed"
		}

		val, err := manager.GetWithOptions("value", WithTransform(transform))
		if err != nil {
			t.Errorf("GetWithOptions() error = %v, want nil", err)
		}

		if val != "transformed" {
			t.Errorf("GetWithOptions() = %v, want %v", val, "transformed")
		}
	})

	t.Run("with validator", func(t *testing.T) {
		validator := func(v any) error {
			if v == "" {
				return ErrValidationError("empty", nil)
			}

			return nil
		}

		// Valid case
		_, err := manager.GetWithOptions("value", WithValidator(validator))
		if err != nil {
			t.Errorf("GetWithOptions() error = %v, want nil", err)
		}

		// Invalid case
		_, err = manager.GetWithOptions("empty", WithValidator(validator))
		if err == nil {
			t.Error("GetWithOptions() expected validation error")
		}
	})

	t.Run("with onMissing callback", func(t *testing.T) {
		onMissing := func(key string) any {
			return "callback_value"
		}

		val, err := manager.GetWithOptions("missing", WithOnMissing(onMissing))
		if err != nil {
			t.Errorf("GetWithOptions() error = %v, want nil", err)
		}

		if val != "callback_value" {
			t.Errorf("GetWithOptions() = %v, want %v", val, "callback_value")
		}
	})
}

func TestManager_GetStringWithOptions(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"value": "test",
		"empty": "",
	}

	t.Run("allow empty", func(t *testing.T) {
		val, err := manager.GetStringWithOptions("empty", AllowEmpty())
		if err != nil {
			t.Errorf("GetStringWithOptions() error = %v, want nil", err)
		}

		if val != "" {
			t.Errorf("GetStringWithOptions() = %v, want empty string", val)
		}
	})

	t.Run("disallow empty with default", func(t *testing.T) {
		val, err := manager.GetStringWithOptions("empty", WithDefault("fallback"))
		if err != nil {
			t.Errorf("GetStringWithOptions() error = %v, want nil", err)
		}

		if val != "fallback" {
			t.Errorf("GetStringWithOptions() = %v, want %v", val, "fallback")
		}
	})
}

// =============================================================================
// SET AND MODIFICATION TESTS
// =============================================================================

func TestManager_Set(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	tests := []struct {
		name  string
		key   string
		value any
	}{
		{"set string", "key1", "value1"},
		{"set int", "key2", 42},
		{"set nested", "nested.key", "nested_value"},
		{"set deep nested", "level1.level2.level3", "deep"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager.Set(tt.key, tt.value)

			got := manager.Get(tt.key)
			if !reflect.DeepEqual(got, tt.value) {
				t.Errorf("After Set(), Get(%q) = %v, want %v", tt.key, got, tt.value)
			}
		})
	}
}

func TestManager_Reset(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	// Add some data
	manager.data["key"] = "value"
	manager.watchCallbacks["key"] = []func(string, any){}

	// Reset
	manager.Reset()

	// Verify reset
	if len(manager.data) != 0 {
		t.Errorf("After Reset(), data length = %v, want 0", len(manager.data))
	}

	if len(manager.watchCallbacks) != 0 {
		t.Errorf("After Reset(), watchCallbacks length = %v, want 0", len(manager.watchCallbacks))
	}

	if len(manager.changeCallbacks) != 0 {
		t.Errorf("After Reset(), changeCallbacks length = %v, want 0", len(manager.changeCallbacks))
	}
}

// =============================================================================
// INTROSPECTION TESTS
// =============================================================================

func TestManager_GetKeys(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"key1": "value1",
		"key2": map[string]any{
			"nested": "value2",
		},
	}

	keys := manager.GetKeys()

	// Should include both top-level and nested keys
	expectedKeys := map[string]bool{
		"key1":        true,
		"key2":        true,
		"key2.nested": true,
	}

	for _, key := range keys {
		if !expectedKeys[key] {
			t.Errorf("Unexpected key: %s", key)
		}

		delete(expectedKeys, key)
	}

	if len(expectedKeys) > 0 {
		t.Errorf("Missing keys: %v", expectedKeys)
	}
}

func TestManager_HasKey(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"key1": "value1",
		"nested": map[string]any{
			"key2": "value2",
		},
	}

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"existing top-level", "key1", true},
		{"existing nested", "nested.key2", true},
		{"non-existent", "nonexistent", false},
		{"non-existent nested", "nested.nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.HasKey(tt.key)
			if got != tt.want {
				t.Errorf("HasKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_IsSet(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"string":       "value",
		"empty_string": "",
		"slice":        []any{"a", "b"},
		"empty_slice":  []any{},
		"map":          map[string]any{"key": "value"},
		"empty_map":    map[string]any{},
	}

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"string with value", "string", true},
		{"empty string", "empty_string", false},
		{"slice with values", "slice", true},
		{"empty slice", "empty_slice", false},
		{"map with values", "map", true},
		{"empty map", "empty_map", false},
		{"non-existent", "nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.IsSet(tt.key)
			if got != tt.want {
				t.Errorf("IsSet(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}

func TestManager_Size(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	if size := manager.Size(); size != 0 {
		t.Errorf("Empty manager Size() = %v, want 0", size)
	}

	manager.data = map[string]any{
		"key1": "value1",
		"key2": map[string]any{
			"nested": "value2",
		},
	}

	size := manager.Size()
	if size == 0 {
		t.Error("Size() = 0, want > 0")
	}
}

func TestManager_GetSection(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"section": map[string]any{
			"key1": "value1",
			"key2": "value2",
		},
		"string": "not_a_section",
	}

	tests := []struct {
		name    string
		key     string
		wantNil bool
		wantLen int
	}{
		{"existing section", "section", false, 2},
		{"non-existent section", "nonexistent", true, 0},
		{"not a section", "string", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manager.GetSection(tt.key)
			if tt.wantNil {
				if got != nil {
					t.Errorf("GetSection(%q) = %v, want nil", tt.key, got)
				}
			} else {
				if got == nil {
					t.Errorf("GetSection(%q) = nil, want map", tt.key)
				} else if len(got) != tt.wantLen {
					t.Errorf("GetSection(%q) length = %v, want %v", tt.key, len(got), tt.wantLen)
				}
			}
		})
	}
}

// =============================================================================
// STRUCTURE OPERATIONS TESTS
// =============================================================================

func TestManager_Sub(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"section": map[string]any{
			"key1": "value1",
			"key2": "value2",
		},
	}

	sub := manager.Sub("section")
	if sub == nil {
		t.Fatal("Sub() returned nil")
	}

	// Test that sub-manager can access values
	if val := sub.GetString("key1"); val != "value1" {
		t.Errorf("Sub().GetString(\"key1\") = %v, want %v", val, "value1")
	}

	// Test with non-existent section
	emptySub := manager.Sub("nonexistent")
	if emptySub == nil {
		t.Fatal("Sub() with non-existent key returned nil")
	}

	if size := emptySub.Size(); size != 0 {
		t.Errorf("Empty sub-manager Size() = %v, want 0", size)
	}
}

func TestManager_Clone(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"key1": "value1",
		"nested": map[string]any{
			"key2": "value2",
		},
	}

	clone := manager.Clone()
	if clone == nil {
		t.Fatal("Clone() returned nil")
	}

	// Verify clone has same data
	if val := clone.GetString("key1"); val != "value1" {
		t.Errorf("Clone().GetString(\"key1\") = %v, want %v", val, "value1")
	}

	// Verify modifications to clone don't affect original
	clone.Set("key1", "modified")

	if orig := manager.GetString("key1"); orig != "value1" {
		t.Errorf("Original modified after clone Set(), got %v, want %v", orig, "value1")
	}
}

func TestManager_GetAllSettings(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	testData := map[string]any{
		"key1": "value1",
		"key2": 42,
	}
	manager.data = testData

	allSettings := manager.GetAllSettings()
	if allSettings == nil {
		t.Fatal("GetAllSettings() returned nil")
	}

	if !reflect.DeepEqual(allSettings, testData) {
		t.Errorf("GetAllSettings() = %v, want %v", allSettings, testData)
	}

	// Verify returned map is a copy
	allSettings["key1"] = "modified"

	if orig := manager.GetString("key1"); orig != "value1" {
		t.Error("GetAllSettings() returned non-copied map")
	}
}

// =============================================================================
// BINDING TESTS
// =============================================================================

type TestConfig struct {
	String   string            `yaml:"string"`
	Int      int               `yaml:"int"`
	Bool     bool              `yaml:"bool"`
	Slice    []string          `yaml:"slice"`
	Map      map[string]string `yaml:"map"`
	Nested   TestNestedConfig  `yaml:"nested"`
	Duration time.Duration     `yaml:"duration"`
}

type TestNestedConfig struct {
	Key   string `yaml:"key"`
	Value int    `yaml:"value"`
}

func TestManager_Bind(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"string": "test",
		"int":    42,
		"bool":   true,
		"slice":  []any{"a", "b", "c"},
		"map": map[string]any{
			"key1": "value1",
		},
		"nested": map[string]any{
			"key":   "nested_key",
			"value": 100,
		},
		"duration": "10s",
	}

	var config TestConfig

	err := manager.Bind("", &config)
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	// Verify bound values
	if config.String != "test" {
		t.Errorf("config.String = %v, want %v", config.String, "test")
	}

	if config.Int != 42 {
		t.Errorf("config.Int = %v, want %v", config.Int, 42)
	}

	if config.Bool != true {
		t.Errorf("config.Bool = %v, want %v", config.Bool, true)
	}

	if config.Nested.Key != "nested_key" {
		t.Errorf("config.Nested.Key = %v, want %v", config.Nested.Key, "nested_key")
	}
}

func TestManager_Bind_WithKey(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"section": map[string]any{
			"key":   "value",
			"value": 99,
		},
	}

	var nested TestNestedConfig

	err := manager.Bind("section", &nested)
	if err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	if nested.Key != "value" {
		t.Errorf("nested.Key = %v, want %v", nested.Key, "value")
	}

	if nested.Value != 99 {
		t.Errorf("nested.Value = %v, want %v", nested.Value, 99)
	}
}

func TestManager_BindWithOptions(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"key": "value",
	}

	t.Run("with default value", func(t *testing.T) {
		var config TestNestedConfig

		defaultValue := map[string]any{
			"key":   "default_key",
			"value": 50,
		}

		err := manager.BindWithOptions("nonexistent", &config, configcore.BindOptions{
			DefaultValue: defaultValue,
			UseDefaults:  true,
		})
		if err != nil {
			t.Fatalf("BindWithOptions() error = %v", err)
		}

		if config.Key != "default_key" {
			t.Errorf("config.Key = %v, want %v", config.Key, "default_key")
		}
	})

	t.Run("error on missing", func(t *testing.T) {
		var config TestConfig

		err := manager.BindWithOptions("nonexistent", &config, configcore.BindOptions{
			ErrorOnMissing: true,
		})
		if err == nil {
			t.Error("BindWithOptions() expected error for missing key")
		}
	})
}

// =============================================================================
// STRUCT DEFAULT VALUE TESTS
// =============================================================================

// Test struct with both yaml and json tags
type TestStructDefaultConfig struct {
	MaxOrganizationsPerUser   int  `yaml:"maxOrganizationsPerUser" json:"maxOrganizationsPerUser"`
	MaxMembersPerOrganization int  `yaml:"maxMembersPerOrganization" json:"maxMembersPerOrganization"`
	MaxTeamsPerOrganization   int  `yaml:"maxTeamsPerOrganization" json:"maxTeamsPerOrganization"`
	EnableUserCreation        bool `yaml:"enableUserCreation" json:"enableUserCreation"`
	RequireInvitation         bool `yaml:"requireInvitation" json:"requireInvitation"`
	InvitationExpiryHours     int  `yaml:"invitationExpiryHours" json:"invitationExpiryHours"`
}

// Test struct with only json tags
type TestJSONOnlyConfig struct {
	MaxValue    int    `json:"maxValue"`
	MinValue    int    `json:"minValue"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// Test struct with nested structs
type TestNestedDefaultConfig struct {
	Name     string                  `yaml:"name" json:"name"`
	Settings TestStructDefaultConfig `yaml:"settings" json:"settings"`
	Active   bool                    `yaml:"active" json:"active"`
}

func TestManager_BindWithDefault_StructValue(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{} // Empty config

	t.Run("struct default with yaml tags", func(t *testing.T) {
		var config TestStructDefaultConfig

		defaultStruct := TestStructDefaultConfig{
			MaxOrganizationsPerUser:   5,
			MaxMembersPerOrganization: 50,
			MaxTeamsPerOrganization:   20,
			EnableUserCreation:        true,
			RequireInvitation:         false,
			InvitationExpiryHours:     72,
		}

		err := manager.BindWithDefault("nonexistent.key", &config, defaultStruct)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if config.MaxOrganizationsPerUser != 5 {
			t.Errorf("config.MaxOrganizationsPerUser = %v, want %v", config.MaxOrganizationsPerUser, 5)
		}
		if config.MaxMembersPerOrganization != 50 {
			t.Errorf("config.MaxMembersPerOrganization = %v, want %v", config.MaxMembersPerOrganization, 50)
		}
		if config.MaxTeamsPerOrganization != 20 {
			t.Errorf("config.MaxTeamsPerOrganization = %v, want %v", config.MaxTeamsPerOrganization, 20)
		}
		if !config.EnableUserCreation {
			t.Errorf("config.EnableUserCreation = %v, want %v", config.EnableUserCreation, true)
		}
		if config.RequireInvitation {
			t.Errorf("config.RequireInvitation = %v, want %v", config.RequireInvitation, false)
		}
		if config.InvitationExpiryHours != 72 {
			t.Errorf("config.InvitationExpiryHours = %v, want %v", config.InvitationExpiryHours, 72)
		}
	})

	t.Run("struct default with json tags only", func(t *testing.T) {
		var config TestJSONOnlyConfig

		defaultStruct := TestJSONOnlyConfig{
			MaxValue:    100,
			MinValue:    10,
			Description: "Test description",
			Enabled:     true,
		}

		err := manager.BindWithDefault("another.nonexistent.key", &config, defaultStruct)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if config.MaxValue != 100 {
			t.Errorf("config.MaxValue = %v, want %v", config.MaxValue, 100)
		}
		if config.MinValue != 10 {
			t.Errorf("config.MinValue = %v, want %v", config.MinValue, 10)
		}
		if config.Description != "Test description" {
			t.Errorf("config.Description = %v, want %v", config.Description, "Test description")
		}
		if !config.Enabled {
			t.Errorf("config.Enabled = %v, want %v", config.Enabled, true)
		}
	})

	t.Run("struct default with nested structs", func(t *testing.T) {
		var config TestNestedDefaultConfig

		defaultStruct := TestNestedDefaultConfig{
			Name: "Test Config",
			Settings: TestStructDefaultConfig{
				MaxOrganizationsPerUser:   3,
				MaxMembersPerOrganization: 25,
				MaxTeamsPerOrganization:   10,
				EnableUserCreation:        false,
				RequireInvitation:         true,
				InvitationExpiryHours:     48,
			},
			Active: true,
		}

		err := manager.BindWithDefault("nested.config", &config, defaultStruct)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if config.Name != "Test Config" {
			t.Errorf("config.Name = %v, want %v", config.Name, "Test Config")
		}
		if config.Settings.MaxOrganizationsPerUser != 3 {
			t.Errorf("config.Settings.MaxOrganizationsPerUser = %v, want %v", config.Settings.MaxOrganizationsPerUser, 3)
		}
		if config.Settings.MaxMembersPerOrganization != 25 {
			t.Errorf("config.Settings.MaxMembersPerOrganization = %v, want %v", config.Settings.MaxMembersPerOrganization, 25)
		}
		if !config.Active {
			t.Errorf("config.Active = %v, want %v", config.Active, true)
		}
	})

	t.Run("struct default with pointer", func(t *testing.T) {
		var config TestStructDefaultConfig

		defaultStruct := &TestStructDefaultConfig{
			MaxOrganizationsPerUser:   7,
			MaxMembersPerOrganization: 100,
			MaxTeamsPerOrganization:   30,
			EnableUserCreation:        true,
			RequireInvitation:         false,
			InvitationExpiryHours:     96,
		}

		err := manager.BindWithDefault("pointer.config", &config, defaultStruct)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if config.MaxOrganizationsPerUser != 7 {
			t.Errorf("config.MaxOrganizationsPerUser = %v, want %v", config.MaxOrganizationsPerUser, 7)
		}
		if config.MaxMembersPerOrganization != 100 {
			t.Errorf("config.MaxMembersPerOrganization = %v, want %v", config.MaxMembersPerOrganization, 100)
		}
	})

	t.Run("config overrides struct default", func(t *testing.T) {
		// Set actual config data
		manager.data = map[string]any{
			"override": map[string]any{
				"maxOrganizationsPerUser":   999,
				"maxMembersPerOrganization": 888,
			},
		}

		var config TestStructDefaultConfig

		defaultStruct := TestStructDefaultConfig{
			MaxOrganizationsPerUser:   5,
			MaxMembersPerOrganization: 50,
			MaxTeamsPerOrganization:   20,
		}

		err := manager.BindWithDefault("override", &config, defaultStruct)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		// Config values should override defaults
		if config.MaxOrganizationsPerUser != 999 {
			t.Errorf("config.MaxOrganizationsPerUser = %v, want %v", config.MaxOrganizationsPerUser, 999)
		}
		if config.MaxMembersPerOrganization != 888 {
			t.Errorf("config.MaxMembersPerOrganization = %v, want %v", config.MaxMembersPerOrganization, 888)
		}
		// This one was not in config, should use default
		if config.MaxTeamsPerOrganization != 20 {
			t.Errorf("config.MaxTeamsPerOrganization = %v, want %v (from default)", config.MaxTeamsPerOrganization, 20)
		}

		// Reset data
		manager.data = map[string]any{}
	})
}

func TestManager_BindWithDefault_PrimitiveValue(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{} // Empty config

	t.Run("int default", func(t *testing.T) {
		var value int
		defaultValue := 42

		err := manager.BindWithDefault("nonexistent.int", &value, defaultValue)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if value != 42 {
			t.Errorf("value = %v, want %v", value, 42)
		}
	})

	t.Run("string default", func(t *testing.T) {
		var value string
		defaultValue := "default string"

		err := manager.BindWithDefault("nonexistent.string", &value, defaultValue)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if value != "default string" {
			t.Errorf("value = %v, want %v", value, "default string")
		}
	})

	t.Run("bool default", func(t *testing.T) {
		var value bool
		defaultValue := true

		err := manager.BindWithDefault("nonexistent.bool", &value, defaultValue)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if !value {
			t.Errorf("value = %v, want %v", value, true)
		}
	})

	t.Run("float default", func(t *testing.T) {
		var value float64
		defaultValue := 3.14

		err := manager.BindWithDefault("nonexistent.float", &value, defaultValue)
		if err != nil {
			t.Fatalf("BindWithDefault() error = %v", err)
		}

		if value != 3.14 {
			t.Errorf("value = %v, want %v", value, 3.14)
		}
	})
}

func TestManager_structToMap(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	t.Run("yaml tags precedence over json", func(t *testing.T) {
		type TestBothTags struct {
			Field1 string `yaml:"yaml_name" json:"json_name"`
			Field2 int    `yaml:"yaml_field2" json:"json_field2"`
		}

		input := TestBothTags{
			Field1: "test value",
			Field2: 123,
		}

		result, err := manager.structToMap(input, "yaml")
		if err != nil {
			t.Fatalf("structToMap() error = %v", err)
		}

		// Should use yaml tag names
		if result["yaml_name"] != "test value" {
			t.Errorf("result[yaml_name] = %v, want %v", result["yaml_name"], "test value")
		}
		if result["yaml_field2"] != 123 {
			t.Errorf("result[yaml_field2] = %v, want %v", result["yaml_field2"], 123)
		}

		// json names should not exist
		if _, exists := result["json_name"]; exists {
			t.Error("result should not have json_name key when yaml tag exists")
		}
	})

	t.Run("json tags as fallback", func(t *testing.T) {
		type TestJSONOnly struct {
			Field1 string `json:"json_field1"`
			Field2 int    `json:"json_field2"`
		}

		input := TestJSONOnly{
			Field1: "json value",
			Field2: 456,
		}

		result, err := manager.structToMap(input, "yaml")
		if err != nil {
			t.Fatalf("structToMap() error = %v", err)
		}

		// Should fall back to json tags
		if result["json_field1"] != "json value" {
			t.Errorf("result[json_field1] = %v, want %v", result["json_field1"], "json value")
		}
		if result["json_field2"] != 456 {
			t.Errorf("result[json_field2] = %v, want %v", result["json_field2"], 456)
		}
	})

	t.Run("skip fields with dash tag", func(t *testing.T) {
		type TestSkipFields struct {
			Field1 string `yaml:"field1"`
			Field2 string `yaml:"-"`
			Field3 int    `json:"-"`
		}

		input := TestSkipFields{
			Field1: "included",
			Field2: "excluded",
			Field3: 999,
		}

		result, err := manager.structToMap(input, "yaml")
		if err != nil {
			t.Fatalf("structToMap() error = %v", err)
		}

		if result["field1"] != "included" {
			t.Errorf("result[field1] = %v, want %v", result["field1"], "included")
		}

		// Field2 and Field3 should not exist
		if _, exists := result["Field2"]; exists {
			t.Error("Field2 should be skipped due to - tag")
		}
		if _, exists := result["Field3"]; exists {
			t.Error("Field3 should be skipped due to - tag")
		}
	})

	t.Run("nested struct recursion", func(t *testing.T) {
		type Inner struct {
			InnerField string `yaml:"innerField"`
		}
		type Outer struct {
			OuterField string `yaml:"outerField"`
			Nested     Inner  `yaml:"nested"`
		}

		input := Outer{
			OuterField: "outer",
			Nested: Inner{
				InnerField: "inner",
			},
		}

		result, err := manager.structToMap(input, "yaml")
		if err != nil {
			t.Fatalf("structToMap() error = %v", err)
		}

		if result["outerField"] != "outer" {
			t.Errorf("result[outerField] = %v, want %v", result["outerField"], "outer")
		}

		nested, ok := result["nested"].(map[string]any)
		if !ok {
			t.Fatalf("result[nested] is not a map")
		}

		if nested["innerField"] != "inner" {
			t.Errorf("nested[innerField] = %v, want %v", nested["innerField"], "inner")
		}
	})

	t.Run("pointer to struct", func(t *testing.T) {
		type TestPointer struct {
			Field string `yaml:"field"`
		}

		input := &TestPointer{
			Field: "pointer value",
		}

		result, err := manager.structToMap(input, "yaml")
		if err != nil {
			t.Fatalf("structToMap() error = %v", err)
		}

		if result["field"] != "pointer value" {
			t.Errorf("result[field] = %v, want %v", result["field"], "pointer value")
		}
	})

	t.Run("nil pointer error", func(t *testing.T) {
		type TestPointer struct {
			Field string `yaml:"field"`
		}

		var input *TestPointer = nil

		_, err := manager.structToMap(input, "yaml")
		if err == nil {
			t.Error("structToMap() should return error for nil pointer")
		}
	})

	t.Run("non-struct error", func(t *testing.T) {
		input := "not a struct"

		_, err := manager.structToMap(input, "yaml")
		if err == nil {
			t.Error("structToMap() should return error for non-struct input")
		}
	})
}

// =============================================================================
// WATCH AND CALLBACK TESTS
// =============================================================================

func TestManager_WatchWithCallback(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"key": "initial",
	}

	var mu sync.Mutex

	callbackCalled := false

	var (
		callbackKey   string
		callbackValue any
	)

	manager.WatchWithCallback("key", func(key string, value any) {
		mu.Lock()
		defer mu.Unlock()

		callbackCalled = true
		callbackKey = key
		callbackValue = value
	})

	// Change the value - should trigger callback via Set
	manager.Set("key", "changed")

	// Give callback time to execute
	time.Sleep(100 * time.Millisecond)

	mu.Lock()

	called := callbackCalled
	key := callbackKey
	value := callbackValue

	mu.Unlock()

	if !called {
		t.Error("Watch callback was not called")
	}

	if key != "key" {
		t.Errorf("callback key = %v, want %v", key, "key")
	}

	if value != "changed" {
		t.Errorf("callback value = %v, want %v", value, "changed")
	}
}

func TestManager_WatchChanges(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	var mu sync.Mutex

	callbackCalled := false

	var change ConfigChange

	manager.WatchChanges(func(c ConfigChange) {
		mu.Lock()
		defer mu.Unlock()

		callbackCalled = true
		change = c
	})

	manager.Set("key", "value")

	// Give callback time to execute
	time.Sleep(100 * time.Millisecond)

	mu.Lock()

	called := callbackCalled
	changeData := change

	mu.Unlock()

	if !called {
		t.Error("Change callback was not called")
	}

	if changeData.Key != "key" {
		t.Errorf("change.Key = %v, want %v", changeData.Key, "key")
	}
}

// =============================================================================
// LIFECYCLE TESTS
// =============================================================================

func TestManager_Validate(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"key": "value",
	}

	// With default validator, validation should pass
	err := manager.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestManager_Reload(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	// Reload should not error even with no sources
	err := manager.Reload()
	if err != nil {
		t.Errorf("Reload() error = %v, want nil", err)
	}
}

func TestManager_Stop(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	err := manager.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}

	// Should be idempotent
	err = manager.Stop()
	if err != nil {
		t.Errorf("Second Stop() error = %v, want nil", err)
	}
}

// =============================================================================
// COMPATIBILITY ALIAS TESTS
// =============================================================================

func TestManager_CompatibilityAliases(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)
	manager.data = map[string]any{
		"key": "value",
	}

	// Test GetBytesSize
	if size := manager.GetBytesSize("key"); size != 0 {
		// OK - tested in GetSizeInBytes
	}

	// Test InConfig
	if has := manager.InConfig("key"); !has {
		t.Error("InConfig(\"key\") = false, want true")
	}

	// Test AllKeys
	keys := manager.AllKeys()
	if len(keys) == 0 {
		t.Error("AllKeys() returned empty slice")
	}

	// Test AllSettings
	settings := manager.AllSettings()
	if settings == nil {
		t.Error("AllSettings() returned nil")
	}

	// Test UnmarshalKey
	var value string

	err := manager.UnmarshalKey("key", &value)
	if err != nil {
		t.Errorf("UnmarshalKey() error = %v", err)
	}

	// Test ConfigFileUsed
	_ = manager.ConfigFileUsed()

	// Test SetConfigType
	manager.SetConfigType("yaml")

	// Test SetConfigFile
	err = manager.SetConfigFile("/path/to/config.yaml")
	if err != nil {
		t.Errorf("SetConfigFile() error = %v", err)
	}
}

// =============================================================================
// HELPER FUNCTION TESTS
// =============================================================================

func TestManager_ConversionHelpers(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	t.Run("convertToString", func(t *testing.T) {
		tests := []struct {
			input any
			want  string
		}{
			{"string", "string"},
			{42, "42"},
			{true, "true"},
			{[]byte("bytes"), "bytes"},
		}

		for _, tt := range tests {
			got := manager.convertToString(tt.input)
			if got != tt.want {
				t.Errorf("convertToString(%v) = %v, want %v", tt.input, got, tt.want)
			}
		}
	})

	t.Run("parseSizeInBytes", func(t *testing.T) {
		tests := []struct {
			input string
			want  uint64
		}{
			{"100", 100},
			{"1KB", 1024},
			{"1MB", 1024 * 1024},
			{"1GB", 1024 * 1024 * 1024},
			{"1K", 1000},
			{"1M", 1000 * 1000},
			{"", 0},
			{"invalid", 0},
		}

		for _, tt := range tests {
			got := manager.parseSizeInBytes(tt.input)
			if got != tt.want {
				t.Errorf("parseSizeInBytes(%q) = %v, want %v", tt.input, got, tt.want)
			}
		}
	})
}

// =============================================================================
// SECRETS MANAGER INTEGRATION TESTS
// =============================================================================

func TestManager_SecretsManager(t *testing.T) {
	config := ManagerConfig{
		SecretsEnabled: true,
	}
	manager := NewManager(config)

	sm := manager.SecretsManager()
	if sm == nil {
		t.Error("SecretsManager() returned nil when secrets enabled")
	}

	// Test with secrets disabled
	config2 := ManagerConfig{
		SecretsEnabled: false,
	}
	manager2 := NewManager(config2)

	sm2 := manager2.SecretsManager()
	if sm2 != nil {
		t.Error("SecretsManager() returned non-nil when secrets disabled")
	}
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestManager_Concurrency(t *testing.T) {
	manager := NewManager(ManagerConfig{}).(*Manager)

	// Test concurrent reads and writes
	done := make(chan bool)

	// Writer goroutines
	for i := range 10 {
		go func(val int) {
			manager.Set("key", val)

			done <- true
		}(i)
	}

	// Reader goroutines
	for range 10 {
		go func() {
			_ = manager.Get("key")

			done <- true
		}()
	}

	// Wait for all goroutines
	for range 20 {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}
}

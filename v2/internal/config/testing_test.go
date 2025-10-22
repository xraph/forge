package config

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// =============================================================================
// TEST CONFIG MANAGER CREATION TESTS
// =============================================================================

func TestNewTestConfigManager(t *testing.T) {
	tcm := NewTestConfigManager()

	if tcm == nil {
		t.Fatal("NewTestConfigManager() returned nil")
	}

	if tcm.data == nil {
		t.Error("data map not initialized")
	}

	// Should start empty
	if len(tcm.data) != 0 {
		t.Errorf("New test manager should be empty, got %d keys", len(tcm.data))
	}
}

// =============================================================================
// TEST CONFIG MANAGER BASIC OPERATIONS
// =============================================================================

func TestTestConfigManager_Set_Get(t *testing.T) {
	tcm := NewTestConfigManager()

	tests := []struct {
		name  string
		key   string
		value interface{}
	}{
		{"string", "string_key", "value"},
		{"int", "int_key", 42},
		{"bool", "bool_key", true},
		{"map", "map_key", map[string]interface{}{"nested": "value"}},
		{"slice", "slice_key", []interface{}{"a", "b", "c"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tcm.Set(tt.key, tt.value)

			got := tcm.Get(tt.key)
			if !reflect.DeepEqual(got, tt.value) {
				t.Errorf("Get() = %v, want %v", got, tt.value)
			}
		})
	}
}

func TestTestConfigManager_GetString(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("string", "test")
	tcm.Set("int", 42)

	tests := []struct {
		name string
		key  string
		want string
	}{
		{"existing string", "string", "test"},
		{"int converted", "int", "42"},
		{"missing key", "missing", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tcm.GetString(tt.key)
			if got != tt.want {
				t.Errorf("GetString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTestConfigManager_GetInt(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("int", 42)
	tcm.Set("string", "123")

	tests := []struct {
		name string
		key  string
		want int
	}{
		{"existing int", "int", 42},
		{"string converted", "string", 123},
		{"missing key", "missing", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tcm.GetInt(tt.key)
			if got != tt.want {
				t.Errorf("GetInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTestConfigManager_GetBool(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("bool", true)
	tcm.Set("string_true", "true")

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"existing bool", "bool", true},
		{"string converted", "string_true", true},
		{"missing key", "missing", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tcm.GetBool(tt.key)
			if got != tt.want {
				t.Errorf("GetBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// TEST CONFIG MANAGER INTROSPECTION
// =============================================================================

func TestTestConfigManager_HasKey(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("existing", "value")

	tests := []struct {
		name string
		key  string
		want bool
	}{
		{"existing key", "existing", true},
		{"missing key", "missing", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tcm.HasKey(tt.key)
			if got != tt.want {
				t.Errorf("HasKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTestConfigManager_GetKeys(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("key1", "value1")
	tcm.Set("key2", "value2")
	tcm.Set("nested.key", "nested_value")

	keys := tcm.GetKeys()

	if len(keys) < 3 {
		t.Errorf("GetKeys() returned %d keys, want at least 3", len(keys))
	}

	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	if !keyMap["key1"] {
		t.Error("key1 not in keys")
	}
	if !keyMap["key2"] {
		t.Error("key2 not in keys")
	}
}

func TestTestConfigManager_Size(t *testing.T) {
	tcm := NewTestConfigManager()

	if size := tcm.Size(); size != 0 {
		t.Errorf("Empty manager Size() = %d, want 0", size)
	}

	tcm.Set("key1", "value1")
	tcm.Set("key2", "value2")

	size := tcm.Size()
	if size == 0 {
		t.Error("Size() = 0 after adding keys")
	}
}

// =============================================================================
// TEST CONFIG MANAGER LIFECYCLE
// =============================================================================

func TestTestConfigManager_Start_Stop(t *testing.T) {
	tcm := NewTestConfigManager()

	ctx := context.Background()

	err := tcm.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v, want nil", err)
	}

	err = tcm.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}
}

func TestTestConfigManager_Validate(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("key", "value")

	err := tcm.Validate()
	if err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestTestConfigManager_Reload(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("key", "value")

	err := tcm.Reload()
	if err != nil {
		t.Errorf("Reload() error = %v, want nil", err)
	}

	// Value should remain after reload
	if val := tcm.GetString("key"); val != "value" {
		t.Errorf("After Reload(), value = %v, want value", val)
	}
}

// =============================================================================
// TEST CONFIG MANAGER RESET
// =============================================================================

func TestTestConfigManager_Reset(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("key1", "value1")
	tcm.Set("key2", "value2")

	tcm.Reset()

	if len(tcm.data) != 0 {
		t.Errorf("After Reset(), data length = %d, want 0", len(tcm.data))
	}

	if val := tcm.Get("key1"); val != nil {
		t.Errorf("After Reset(), Get() = %v, want nil", val)
	}
}

// =============================================================================
// TEST CONFIG BUILDER TESTS
// =============================================================================

func TestTestConfigBuilder(t *testing.T) {
	t.Run("build with data", func(t *testing.T) {
		tcm := NewTestConfigBuilder().
			WithData(map[string]interface{}{
				"key1": "value1",
				"key2": 42,
			}).
			Build()

		if tcm.GetString("key1") != "value1" {
			t.Error("key1 not set correctly")
		}
		if tcm.GetInt("key2") != 42 {
			t.Error("key2 not set correctly")
		}
	})

	t.Run("build with key-value pairs", func(t *testing.T) {
		tcm := NewTestConfigBuilder().
			WithKey("host", "localhost").
			WithKey("port", 8080).
			Build()

		if tcm.GetString("host") != "localhost" {
			t.Error("host not set correctly")
		}
		if tcm.GetInt("port") != 8080 {
			t.Error("port not set correctly")
		}
	})

	t.Run("build with nested values", func(t *testing.T) {
		tcm := NewTestConfigBuilder().
			WithKey("database.host", "localhost").
			WithKey("database.port", 5432).
			Build()

		if tcm.GetString("database.host") != "localhost" {
			t.Error("nested host not set correctly")
		}
		if tcm.GetInt("database.port") != 5432 {
			t.Error("nested port not set correctly")
		}
	})

	t.Run("chained building", func(t *testing.T) {
		tcm := NewTestConfigBuilder().
			WithKey("key1", "value1").
			WithData(map[string]interface{}{
				"key2": "value2",
			}).
			WithKey("key3", "value3").
			Build()

		if tcm.GetString("key1") != "value1" {
			t.Error("key1 not set correctly")
		}
		if tcm.GetString("key2") != "value2" {
			t.Error("key2 not set correctly")
		}
		if tcm.GetString("key3") != "value3" {
			t.Error("key3 not set correctly")
		}
	})
}

// =============================================================================
// TEST CONFIG ASSERTIONS TESTS
// =============================================================================

func TestTestConfigAssertions(t *testing.T) {
	tcm := NewTestConfigManager()
	tcm.Set("string", "value")
	tcm.Set("int", 42)
	tcm.Set("bool", true)

	assertions := NewTestConfigAssertions(t, tcm)

	t.Run("assert has key", func(t *testing.T) {
		assertions.AssertHasKey("string")
		assertions.AssertHasKey("int")
	})

	t.Run("assert value", func(t *testing.T) {
		assertions.AssertValue("string", "value")
		assertions.AssertValue("int", 42)
		assertions.AssertValue("bool", true)
	})

	t.Run("assert string value", func(t *testing.T) {
		assertions.AssertStringValue("string", "value")
	})

	t.Run("assert int value", func(t *testing.T) {
		assertions.AssertIntValue("int", 42)
	})

	t.Run("assert bool value", func(t *testing.T) {
		assertions.AssertBoolValue("bool", true)
	})

	t.Run("assert key count", func(t *testing.T) {
		assertions.AssertKeyCount(3)
	})

	t.Run("assert empty", func(t *testing.T) {
		emptyTcm := NewTestConfigManager()
		emptyAssertions := NewTestConfigAssertions(t, emptyTcm)
		emptyAssertions.AssertEmpty()
	})

	t.Run("assert not empty", func(t *testing.T) {
		assertions.AssertNotEmpty()
	})
}

// =============================================================================
// MOCK SECRETS MANAGER CREATION TESTS
// =============================================================================

func TestNewMockSecretsManager(t *testing.T) {
	msm := NewMockSecretsManager()

	if msm == nil {
		t.Fatal("NewMockSecretsManager() returned nil")
	}

	if msm.secrets == nil {
		t.Error("secrets map not initialized")
	}

	if msm.calls == nil {
		t.Error("calls map not initialized")
	}
}

// =============================================================================
// MOCK SECRETS MANAGER BASIC OPERATIONS
// =============================================================================

func TestMockSecretsManager_GetSecret(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	// Set a secret
	msm.SetSecret(ctx, "test:key", "value")

	t.Run("get existing secret", func(t *testing.T) {
		value, err := msm.GetSecret(ctx, "test:key")
		if err != nil {
			t.Errorf("GetSecret() error = %v, want nil", err)
		}
		if value != "value" {
			t.Errorf("GetSecret() = %v, want %v", value, "value")
		}
	})

	t.Run("get non-existent secret", func(t *testing.T) {
		_, err := msm.GetSecret(ctx, "test:nonexistent")
		if err == nil {
			t.Error("GetSecret() should return error for non-existent secret")
		}
	})
}

func TestMockSecretsManager_SetSecret(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	err := msm.SetSecret(ctx, "test:key", "value")
	if err != nil {
		t.Errorf("SetSecret() error = %v, want nil", err)
	}

	// Verify secret was set
	value, err := msm.GetSecret(ctx, "test:key")
	if err != nil {
		t.Errorf("GetSecret() after SetSecret() error = %v", err)
	}
	if value != "value" {
		t.Errorf("Secret value = %v, want %v", value, "value")
	}
}

func TestMockSecretsManager_DeleteSecret(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	msm.SetSecret(ctx, "test:key", "value")

	err := msm.DeleteSecret(ctx, "test:key")
	if err != nil {
		t.Errorf("DeleteSecret() error = %v, want nil", err)
	}

	// Verify secret was deleted
	_, err = msm.GetSecret(ctx, "test:key")
	if err == nil {
		t.Error("Secret should not exist after deletion")
	}
}

func TestMockSecretsManager_ListSecrets(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	msm.SetSecret(ctx, "test:key1", "value1")
	msm.SetSecret(ctx, "test:key2", "value2")
	msm.SetSecret(ctx, "test:key3", "value3")

	keys, err := msm.ListSecrets(ctx, "test")
	if err != nil {
		t.Errorf("ListSecrets() error = %v, want nil", err)
	}

	if len(keys) != 3 {
		t.Errorf("ListSecrets() returned %d keys, want 3", len(keys))
	}
}

// =============================================================================
// MOCK SECRETS MANAGER CALL TRACKING
// =============================================================================

func TestMockSecretsManager_CallTracking(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	t.Run("track GetSecret calls", func(t *testing.T) {
		msm.SetSecret(ctx, "test:key", "value")
		msm.GetSecret(ctx, "test:key")
		msm.GetSecret(ctx, "test:key")

		count := msm.GetCallCount("GetSecret")
		if count != 2 {
			t.Errorf("GetCallCount(\"GetSecret\") = %d, want 2", count)
		}
	})

	t.Run("track SetSecret calls", func(t *testing.T) {
		msm.ResetCalls()

		msm.SetSecret(ctx, "test:key1", "value1")
		msm.SetSecret(ctx, "test:key2", "value2")

		count := msm.GetCallCount("SetSecret")
		if count != 2 {
			t.Errorf("GetCallCount(\"SetSecret\") = %d, want 2", count)
		}
	})

	t.Run("track DeleteSecret calls", func(t *testing.T) {
		msm.ResetCalls()

		msm.SetSecret(ctx, "test:key", "value")
		msm.DeleteSecret(ctx, "test:key")

		count := msm.GetCallCount("DeleteSecret")
		if count != 1 {
			t.Errorf("GetCallCount(\"DeleteSecret\") = %d, want 1", count)
		}
	})

	t.Run("reset calls", func(t *testing.T) {
		msm.ResetCalls()

		count := msm.GetCallCount("GetSecret")
		if count != 0 {
			t.Errorf("After ResetCalls(), GetCallCount() = %d, want 0", count)
		}
	})
}

func TestMockSecretsManager_WasCalledWith(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	msm.SetSecret(ctx, "test:key", "value")
	msm.GetSecret(ctx, "test:key")

	t.Run("was called with correct key", func(t *testing.T) {
		if !msm.WasCalledWith("GetSecret", "test:key") {
			t.Error("WasCalledWith() should return true for called key")
		}
	})

	t.Run("was not called with other key", func(t *testing.T) {
		if msm.WasCalledWith("GetSecret", "test:other") {
			t.Error("WasCalledWith() should return false for uncalled key")
		}
	})
}

// =============================================================================
// MOCK SECRETS MANAGER ERROR SIMULATION
// =============================================================================

func TestMockSecretsManager_ErrorSimulation(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	t.Run("simulate GetSecret error", func(t *testing.T) {
		testErr := ErrConfigError("test error", nil)
		msm.SetError("GetSecret", testErr)

		_, err := msm.GetSecret(ctx, "test:key")
		if err != testErr {
			t.Errorf("GetSecret() error = %v, want %v", err, testErr)
		}
	})

	t.Run("simulate SetSecret error", func(t *testing.T) {
		testErr := ErrConfigError("set error", nil)
		msm.SetError("SetSecret", testErr)

		err := msm.SetSecret(ctx, "test:key", "value")
		if err != testErr {
			t.Errorf("SetSecret() error = %v, want %v", err, testErr)
		}
	})

	t.Run("clear error", func(t *testing.T) {
		msm.ClearError("GetSecret")

		msm.SetSecret(ctx, "test:key", "value")
		_, err := msm.GetSecret(ctx, "test:key")
		if err != nil {
			t.Errorf("After ClearError(), GetSecret() error = %v, want nil", err)
		}
	})
}

// =============================================================================
// MOCK SECRETS MANAGER PROVIDER OPERATIONS
// =============================================================================

func TestMockSecretsManager_Providers(t *testing.T) {
	msm := NewMockSecretsManager()

	mockProvider := NewMemorySecretProvider()

	t.Run("register provider", func(t *testing.T) {
		err := msm.RegisterProvider("test", mockProvider)
		if err != nil {
			t.Errorf("RegisterProvider() error = %v, want nil", err)
		}
	})

	t.Run("get provider", func(t *testing.T) {
		provider := msm.GetProvider("test")
		if provider == nil {
			t.Error("GetProvider() returned nil for registered provider")
		}
	})

	t.Run("has provider", func(t *testing.T) {
		if !msm.HasProvider("test") {
			t.Error("HasProvider() should return true for registered provider")
		}
		if msm.HasProvider("nonexistent") {
			t.Error("HasProvider() should return false for non-existent provider")
		}
	})

	t.Run("unregister provider", func(t *testing.T) {
		err := msm.UnregisterProvider("test")
		if err != nil {
			t.Errorf("UnregisterProvider() error = %v, want nil", err)
		}

		if msm.HasProvider("test") {
			t.Error("Provider should not exist after unregistration")
		}
	})
}

// =============================================================================
// MOCK SECRETS MANAGER LIFECYCLE
// =============================================================================

func TestMockSecretsManager_Lifecycle(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()

	t.Run("start", func(t *testing.T) {
		err := msm.Start(ctx)
		if err != nil {
			t.Errorf("Start() error = %v, want nil", err)
		}
	})

	t.Run("stop", func(t *testing.T) {
		err := msm.Stop()
		if err != nil {
			t.Errorf("Stop() error = %v, want nil", err)
		}
	})
}

// =============================================================================
// INTEGRATION TESTS
// =============================================================================

func TestTestConfigManager_Integration(t *testing.T) {
	t.Run("full configuration workflow", func(t *testing.T) {
		// Build test config
		tcm := NewTestConfigBuilder().
			WithKey("app.name", "TestApp").
			WithKey("app.version", "1.0.0").
			WithKey("database.host", "localhost").
			WithKey("database.port", 5432).
			WithKey("database.name", "testdb").
			WithKey("features.enabled", true).
			Build()

		// Start
		ctx := context.Background()
		err := tcm.Start(ctx)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		// Validate
		err = tcm.Validate()
		if err != nil {
			t.Errorf("Validate() error = %v", err)
		}

		// Access values
		assertions := NewTestConfigAssertions(t, tcm)
		assertions.AssertStringValue("app.name", "TestApp")
		assertions.AssertStringValue("app.version", "1.0.0")
		assertions.AssertStringValue("database.host", "localhost")
		assertions.AssertIntValue("database.port", 5432)
		assertions.AssertBoolValue("features.enabled", true)

		// Stop
		err = tcm.Stop()
		if err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	t.Run("dynamic configuration updates", func(t *testing.T) {
		tcm := NewTestConfigManager()

		// Initial config
		tcm.Set("value", "initial")

		// Update
		tcm.Set("value", "updated")

		if val := tcm.GetString("value"); val != "updated" {
			t.Errorf("After update, value = %v, want updated", val)
		}

		// Reset
		tcm.Reset()

		if val := tcm.GetString("value"); val != "" {
			t.Errorf("After reset, value = %v, want empty", val)
		}
	})
}

func TestMockSecretsManager_Integration(t *testing.T) {
	t.Run("full secrets workflow", func(t *testing.T) {
		msm := NewMockSecretsManager()
		ctx := context.Background()

		// Start
		err := msm.Start(ctx)
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		// Set secrets
		msm.SetSecret(ctx, "test:api_key", "secret123")
		msm.SetSecret(ctx, "test:db_password", "password456")

		// Get secrets
		apiKey, err := msm.GetSecret(ctx, "test:api_key")
		if err != nil || apiKey != "secret123" {
			t.Errorf("GetSecret(api_key) = %v, %v; want secret123, nil", apiKey, err)
		}

		// List secrets
		keys, err := msm.ListSecrets(ctx, "test")
		if err != nil || len(keys) != 2 {
			t.Errorf("ListSecrets() = %d keys, %v; want 2, nil", len(keys), err)
		}

		// Verify call tracking
		if count := msm.GetCallCount("GetSecret"); count != 1 {
			t.Errorf("GetSecret called %d times, want 1", count)
		}

		// Delete secret
		msm.DeleteSecret(ctx, "test:api_key")

		// Verify deletion
		_, err = msm.GetSecret(ctx, "test:api_key")
		if err == nil {
			t.Error("Secret should not exist after deletion")
		}

		// Stop
		err = msm.Stop()
		if err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	})

	t.Run("error simulation workflow", func(t *testing.T) {
		msm := NewMockSecretsManager()
		ctx := context.Background()

		// Simulate error
		testErr := ErrConfigError("simulated error", nil)
		msm.SetError("GetSecret", testErr)

		// Attempt to get secret
		_, err := msm.GetSecret(ctx, "test:key")
		if err != testErr {
			t.Errorf("GetSecret() error = %v, want %v", err, testErr)
		}

		// Clear error
		msm.ClearError("GetSecret")

		// Set and get should work now
		msm.SetSecret(ctx, "test:key", "value")
		value, err := msm.GetSecret(ctx, "test:key")
		if err != nil || value != "value" {
			t.Errorf("After ClearError(), GetSecret() = %v, %v; want value, nil", value, err)
		}
	})
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestTestConfigManager_Concurrency(t *testing.T) {
	tcm := NewTestConfigManager()
	done := make(chan bool)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(val int) {
			tcm.Set("key", val)
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func() {
			_ = tcm.Get("key")
			done <- true
		}()
	}

	// Wait for all
	timeout := time.After(5 * time.Second)
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}
}

func TestMockSecretsManager_Concurrency(t *testing.T) {
	msm := NewMockSecretsManager()
	ctx := context.Background()
	done := make(chan bool)

	// Concurrent operations
	for i := 0; i < 10; i++ {
		go func(idx int) {
			key := "test:key" + string(rune('0'+idx))
			msm.SetSecret(ctx, key, "value")
			msm.GetSecret(ctx, key)
			done <- true
		}(i)
	}

	// Wait for all
	timeout := time.After(5 * time.Second)
	for i := 0; i < 10; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}
}

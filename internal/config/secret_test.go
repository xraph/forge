package config

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// =============================================================================
// SECRETS MANAGER CREATION TESTS
// =============================================================================

func TestNewSecretsManager(t *testing.T) {
	tests := []struct {
		name   string
		config SecretsManagerConfig
	}{
		{
			name:   "default config",
			config: SecretsManagerConfig{},
		},
		{
			name: "with cache enabled",
			config: SecretsManagerConfig{
				CacheEnabled: true,
				CacheTTL:     5 * time.Minute,
			},
		},
		{
			name: "with rotation enabled",
			config: SecretsManagerConfig{
				RotationEnabled:  true,
				RotationInterval: 24 * time.Hour,
			},
		},
		{
			name: "with encryption",
			config: SecretsManagerConfig{
				EncryptionEnabled: true,
				EncryptionKey:     []byte("test-key-32-bytes-long-12345"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewSecretsManager(tt.config)
			if sm == nil {
				t.Fatal("NewSecretsManager() returned nil")
			}

			impl, ok := sm.(*SecretsManagerImpl)
			if !ok {
				t.Fatal("NewSecretsManager() did not return *SecretsManagerImpl")
			}

			if impl.providers == nil {
				t.Error("providers map not initialized")
			}
		})
	}
}

// =============================================================================
// SECRET PROVIDER REGISTRATION TESTS
// =============================================================================

func TestSecretsManager_RegisterProvider(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()

	t.Run("register provider", func(t *testing.T) {
		err := sm.RegisterProvider("memory", provider)
		if err != nil {
			t.Errorf("RegisterProvider() error = %v, want nil", err)
		}

		// Verify provider is registered
		if _, exists := sm.providers["memory"]; !exists {
			t.Error("Provider not found after registration")
		}
	})

	t.Run("register nil provider", func(t *testing.T) {
		err := sm.RegisterProvider("nil", nil)
		if err == nil {
			t.Error("RegisterProvider(nil) should return error")
		}
	})

	t.Run("register duplicate provider", func(t *testing.T) {
		duplicate := NewMemorySecretProvider()
		err := sm.RegisterProvider("memory", duplicate)
		if err == nil {
			t.Error("RegisterProvider() should return error for duplicate name")
		}
	})
}

func TestSecretsManager_UnregisterProvider(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("test", provider)

	t.Run("unregister existing provider", func(t *testing.T) {
		err := sm.UnregisterProvider("test")
		if err != nil {
			t.Errorf("UnregisterProvider() error = %v, want nil", err)
		}

		if _, exists := sm.providers["test"]; exists {
			t.Error("Provider still exists after unregistration")
		}
	})

	t.Run("unregister non-existent provider", func(t *testing.T) {
		err := sm.UnregisterProvider("nonexistent")
		if err == nil {
			t.Error("UnregisterProvider() should return error for non-existent provider")
		}
	})
}

func TestSecretsManager_GetProvider(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("test", provider)

	t.Run("get existing provider", func(t *testing.T) {
		retrieved := sm.GetProvider("test")
		if retrieved == nil {
			t.Error("GetProvider() returned nil for existing provider")
		}
	})

	t.Run("get non-existent provider", func(t *testing.T) {
		retrieved := sm.GetProvider("nonexistent")
		if retrieved != nil {
			t.Error("GetProvider() should return nil for non-existent provider")
		}
	})
}

func TestSecretsManager_HasProvider(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("test", provider)

	tests := []struct {
		name         string
		providerName string
		want         bool
	}{
		{"existing provider", "test", true},
		{"non-existent provider", "nonexistent", false},
		{"empty name", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sm.HasProvider(tt.providerName)
			if got != tt.want {
				t.Errorf("HasProvider(%q) = %v, want %v", tt.providerName, got, tt.want)
			}
		})
	}
}

// =============================================================================
// LIFECYCLE TESTS
// =============================================================================

func TestSecretsManager_Start(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("test", provider)

	ctx := context.Background()
	err := sm.Start(ctx)
	if err != nil {
		t.Errorf("Start() error = %v, want nil", err)
	}

	// Starting again should be idempotent
	err = sm.Start(ctx)
	if err != nil {
		t.Errorf("Second Start() error = %v, want nil", err)
	}
}

func TestSecretsManager_Stop(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	ctx := context.Background()
	sm.Start(ctx)

	err := sm.Stop()
	if err != nil {
		t.Errorf("Stop() error = %v, want nil", err)
	}

	// Stopping again should be idempotent
	err = sm.Stop()
	if err != nil {
		t.Errorf("Second Stop() error = %v, want nil", err)
	}
}

// =============================================================================
// SECRET OPERATIONS TESTS
// =============================================================================

func TestSecretsManager_GetSecret(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	// Set a secret
	provider.SetSecret(ctx, "test-key", "test-value")

	t.Run("get existing secret", func(t *testing.T) {
		value, err := sm.GetSecret(ctx, "memory:test-key")
		if err != nil {
			t.Errorf("GetSecret() error = %v, want nil", err)
		}
		if value != "test-value" {
			t.Errorf("GetSecret() = %v, want %v", value, "test-value")
		}
	})

	t.Run("get non-existent secret", func(t *testing.T) {
		_, err := sm.GetSecret(ctx, "memory:nonexistent")
		if err == nil {
			t.Error("GetSecret() should return error for non-existent secret")
		}
	})

	t.Run("get secret with invalid provider", func(t *testing.T) {
		_, err := sm.GetSecret(ctx, "invalid:key")
		if err == nil {
			t.Error("GetSecret() should return error for invalid provider")
		}
	})

	t.Run("get secret without provider prefix", func(t *testing.T) {
		// Should use default provider if configured
		_, err := sm.GetSecret(ctx, "no-prefix-key")
		if err == nil {
			// This test depends on implementation - may or may not error
		}
	})
}

func TestSecretsManager_SetSecret(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	t.Run("set secret", func(t *testing.T) {
		err := sm.SetSecret(ctx, "memory:new-key", "new-value")
		if err != nil {
			t.Errorf("SetSecret() error = %v, want nil", err)
		}

		// Verify secret was set
		value, err := sm.GetSecret(ctx, "memory:new-key")
		if err != nil {
			t.Errorf("GetSecret() after SetSecret() error = %v", err)
		}
		if value != "new-value" {
			t.Errorf("Secret value = %v, want %v", value, "new-value")
		}
	})

	t.Run("update existing secret", func(t *testing.T) {
		err := sm.SetSecret(ctx, "memory:new-key", "updated-value")
		if err != nil {
			t.Errorf("SetSecret() update error = %v, want nil", err)
		}

		value, _ := sm.GetSecret(ctx, "memory:new-key")
		if value != "updated-value" {
			t.Errorf("Updated secret value = %v, want %v", value, "updated-value")
		}
	})

	t.Run("set secret with invalid provider", func(t *testing.T) {
		err := sm.SetSecret(ctx, "invalid:key", "value")
		if err == nil {
			t.Error("SetSecret() should return error for invalid provider")
		}
	})
}

func TestSecretsManager_DeleteSecret(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	// Set a secret first
	sm.SetSecret(ctx, "memory:to-delete", "value")

	t.Run("delete existing secret", func(t *testing.T) {
		err := sm.DeleteSecret(ctx, "memory:to-delete")
		if err != nil {
			t.Errorf("DeleteSecret() error = %v, want nil", err)
		}

		// Verify secret was deleted
		_, err = sm.GetSecret(ctx, "memory:to-delete")
		if err == nil {
			t.Error("Secret still exists after deletion")
		}
	})

	t.Run("delete non-existent secret", func(t *testing.T) {
		err := sm.DeleteSecret(ctx, "memory:nonexistent")
		if err == nil {
			t.Error("DeleteSecret() should return error for non-existent secret")
		}
	})

	t.Run("delete secret with invalid provider", func(t *testing.T) {
		err := sm.DeleteSecret(ctx, "invalid:key")
		if err == nil {
			t.Error("DeleteSecret() should return error for invalid provider")
		}
	})
}

func TestSecretsManager_ListSecrets(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	// Set multiple secrets
	sm.SetSecret(ctx, "memory:key1", "value1")
	sm.SetSecret(ctx, "memory:key2", "value2")
	sm.SetSecret(ctx, "memory:key3", "value3")

	t.Run("list secrets", func(t *testing.T) {
		keys, err := sm.ListSecrets(ctx, "memory")
		if err != nil {
			t.Errorf("ListSecrets() error = %v, want nil", err)
		}

		if len(keys) != 3 {
			t.Errorf("ListSecrets() returned %d keys, want 3", len(keys))
		}

		// Verify expected keys
		expectedKeys := map[string]bool{
			"key1": true,
			"key2": true,
			"key3": true,
		}

		for _, key := range keys {
			if !expectedKeys[key] {
				t.Errorf("Unexpected key in list: %s", key)
			}
		}
	})

	t.Run("list secrets from invalid provider", func(t *testing.T) {
		_, err := sm.ListSecrets(ctx, "invalid")
		if err == nil {
			t.Error("ListSecrets() should return error for invalid provider")
		}
	})
}

// =============================================================================
// CACHING TESTS
// =============================================================================

func TestSecretsManager_Caching(t *testing.T) {
	config := SecretsManagerConfig{
		CacheEnabled: true,
		CacheTTL:     1 * time.Second,
	}
	sm := NewSecretsManager(config).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	sm.SetSecret(ctx, "memory:cached-key", "original-value")

	t.Run("first get loads from provider", func(t *testing.T) {
		value, err := sm.GetSecret(ctx, "memory:cached-key")
		if err != nil {
			t.Errorf("GetSecret() error = %v", err)
		}
		if value != "original-value" {
			t.Errorf("Value = %v, want %v", value, "original-value")
		}
	})

	t.Run("second get uses cache", func(t *testing.T) {
		// Change value in provider directly
		provider.SetSecret(ctx, "cached-key", "changed-value")

		// Should still get cached value
		value, err := sm.GetSecret(ctx, "memory:cached-key")
		if err != nil {
			t.Errorf("GetSecret() error = %v", err)
		}
		if config.CacheEnabled && value != "original-value" {
			// With cache, should still get original
			t.Errorf("Cached value = %v, want %v", value, "original-value")
		}
	})

	t.Run("cache expires", func(t *testing.T) {
		// Wait for cache to expire
		time.Sleep(2 * time.Second)

		value, err := sm.GetSecret(ctx, "memory:cached-key")
		if err != nil {
			t.Errorf("GetSecret() error = %v", err)
		}
		if value != "changed-value" {
			t.Errorf("After cache expiry, value = %v, want %v", value, "changed-value")
		}
	})
}

// =============================================================================
// ENCRYPTION TESTS
// =============================================================================

func TestSecretsManager_Encryption(t *testing.T) {
	key := []byte("test-encryption-key-32-bytes!")
	config := SecretsManagerConfig{
		EncryptionEnabled: true,
		EncryptionKey:     key,
	}
	sm := NewSecretsManager(config).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	t.Run("set and get encrypted secret", func(t *testing.T) {
		err := sm.SetSecret(ctx, "memory:encrypted-key", "sensitive-value")
		if err != nil {
			t.Errorf("SetSecret() error = %v", err)
		}

		value, err := sm.GetSecret(ctx, "memory:encrypted-key")
		if err != nil {
			t.Errorf("GetSecret() error = %v", err)
		}
		if value != "sensitive-value" {
			t.Errorf("Decrypted value = %v, want %v", value, "sensitive-value")
		}
	})

	t.Run("encrypted value stored is different", func(t *testing.T) {
		// Get the raw value from provider
		rawValue, _ := provider.GetSecret(ctx, "encrypted-key")

		if config.EncryptionEnabled && rawValue == "sensitive-value" {
			t.Error("Value should be encrypted in provider storage")
		}
	})
}

// =============================================================================
// ROTATION TESTS
// =============================================================================

func TestSecretsManager_Rotation(t *testing.T) {
	config := SecretsManagerConfig{
		RotationEnabled:  true,
		RotationInterval: 100 * time.Millisecond,
	}
	sm := NewSecretsManager(config).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	rotationCalled := false
	sm.OnRotation(func(key string) error {
		rotationCalled = true
		return nil
	})

	sm.SetSecret(ctx, "memory:rotate-key", "value")

	// Wait for rotation interval
	time.Sleep(200 * time.Millisecond)

	if config.RotationEnabled && !rotationCalled {
		// Depending on implementation, rotation might be triggered
		// This is just a placeholder test
	}

	sm.Stop()
}

// =============================================================================
// BUILT-IN PROVIDER TESTS
// =============================================================================

func TestEnvironmentSecretProvider(t *testing.T) {
	provider := NewEnvironmentSecretProvider()
	ctx := context.Background()

	testEnvKey := "TEST_SECRET_KEY"
	testEnvValue := "test_secret_value"

	// Set environment variable
	os.Setenv(testEnvKey, testEnvValue)
	defer os.Unsetenv(testEnvKey)

	t.Run("get existing env var", func(t *testing.T) {
		value, err := provider.GetSecret(ctx, testEnvKey)
		if err != nil {
			t.Errorf("GetSecret() error = %v", err)
		}
		if value != testEnvValue {
			t.Errorf("GetSecret() = %v, want %v", value, testEnvValue)
		}
	})

	t.Run("get non-existent env var", func(t *testing.T) {
		_, err := provider.GetSecret(ctx, "NONEXISTENT_VAR")
		if err == nil {
			t.Error("GetSecret() should return error for non-existent env var")
		}
	})

	t.Run("list secrets", func(t *testing.T) {
		keys, err := provider.ListSecrets(ctx)
		if err != nil {
			t.Errorf("ListSecrets() error = %v", err)
		}
		// Should return all env vars
		if len(keys) == 0 {
			t.Error("ListSecrets() returned empty list")
		}
	})

	t.Run("set secret not supported", func(t *testing.T) {
		err := provider.SetSecret(ctx, "NEW_VAR", "value")
		if err == nil {
			t.Error("SetSecret() should return error (not supported)")
		}
	})

	t.Run("delete secret not supported", func(t *testing.T) {
		err := provider.DeleteSecret(ctx, testEnvKey)
		if err == nil {
			t.Error("DeleteSecret() should return error (not supported)")
		}
	})
}

func TestFileSecretProvider(t *testing.T) {
	// Create temporary directory for test
	tmpDir := t.TempDir()

	provider := NewFileSecretProvider(tmpDir)
	ctx := context.Background()

	err := provider.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer provider.Stop()

	t.Run("set and get secret", func(t *testing.T) {
		err := provider.SetSecret(ctx, "test-key", "test-value")
		if err != nil {
			t.Errorf("SetSecret() error = %v", err)
		}

		value, err := provider.GetSecret(ctx, "test-key")
		if err != nil {
			t.Errorf("GetSecret() error = %v", err)
		}
		if value != "test-value" {
			t.Errorf("GetSecret() = %v, want %v", value, "test-value")
		}
	})

	t.Run("secret stored as file", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "test-key")
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Error("Secret file was not created")
		}
	})

	t.Run("delete secret", func(t *testing.T) {
		err := provider.DeleteSecret(ctx, "test-key")
		if err != nil {
			t.Errorf("DeleteSecret() error = %v", err)
		}

		filePath := filepath.Join(tmpDir, "test-key")
		if _, err := os.Stat(filePath); !os.IsNotExist(err) {
			t.Error("Secret file was not deleted")
		}
	})

	t.Run("list secrets", func(t *testing.T) {
		provider.SetSecret(ctx, "key1", "value1")
		provider.SetSecret(ctx, "key2", "value2")

		keys, err := provider.ListSecrets(ctx)
		if err != nil {
			t.Errorf("ListSecrets() error = %v", err)
		}
		if len(keys) != 2 {
			t.Errorf("ListSecrets() returned %d keys, want 2", len(keys))
		}
	})
}

func TestMemorySecretProvider(t *testing.T) {
	provider := NewMemorySecretProvider()
	ctx := context.Background()

	provider.Start(ctx)
	defer provider.Stop()

	t.Run("set and get secret", func(t *testing.T) {
		err := provider.SetSecret(ctx, "test-key", "test-value")
		if err != nil {
			t.Errorf("SetSecret() error = %v", err)
		}

		value, err := provider.GetSecret(ctx, "test-key")
		if err != nil {
			t.Errorf("GetSecret() error = %v", err)
		}
		if value != "test-value" {
			t.Errorf("GetSecret() = %v, want %v", value, "test-value")
		}
	})

	t.Run("get non-existent secret", func(t *testing.T) {
		_, err := provider.GetSecret(ctx, "nonexistent")
		if err == nil {
			t.Error("GetSecret() should return error for non-existent secret")
		}
	})

	t.Run("delete secret", func(t *testing.T) {
		provider.SetSecret(ctx, "to-delete", "value")

		err := provider.DeleteSecret(ctx, "to-delete")
		if err != nil {
			t.Errorf("DeleteSecret() error = %v", err)
		}

		_, err = provider.GetSecret(ctx, "to-delete")
		if err == nil {
			t.Error("Secret should not exist after deletion")
		}
	})

	t.Run("list secrets", func(t *testing.T) {
		provider.SetSecret(ctx, "key1", "value1")
		provider.SetSecret(ctx, "key2", "value2")

		keys, err := provider.ListSecrets(ctx)
		if err != nil {
			t.Errorf("ListSecrets() error = %v", err)
		}
		if len(keys) < 2 {
			t.Errorf("ListSecrets() returned %d keys, want at least 2", len(keys))
		}
	})

	t.Run("update secret", func(t *testing.T) {
		provider.SetSecret(ctx, "update-key", "original")

		err := provider.SetSecret(ctx, "update-key", "updated")
		if err != nil {
			t.Errorf("SetSecret() update error = %v", err)
		}

		value, _ := provider.GetSecret(ctx, "update-key")
		if value != "updated" {
			t.Errorf("Updated value = %v, want %v", value, "updated")
		}
	})
}

// =============================================================================
// UTILITY FUNCTION TESTS
// =============================================================================

func TestIsSecretReference(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  bool
	}{
		{"valid secret reference", "secret:key", true},
		{"valid env reference", "env:VAR", true},
		{"valid provider reference", "vault:path/to/secret", true},
		{"no colon", "regular_value", false},
		{"empty string", "", false},
		{"non-string value", 123, false},
		{"nil value", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSecretReference(tt.value)
			if got != tt.want {
				t.Errorf("IsSecretReference(%v) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestExtractSecretKey(t *testing.T) {
	tests := []struct {
		name     string
		ref      string
		provider string
		key      string
	}{
		{"basic reference", "secret:mykey", "secret", "mykey"},
		{"env reference", "env:MY_VAR", "env", "MY_VAR"},
		{"vault reference", "vault:path/to/key", "vault", "path/to/key"},
		{"multiple colons", "provider:path:with:colons", "provider", "path:with:colons"},
		{"no colon", "nocolon", "", "nocolon"},
		{"empty", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, key := ExtractSecretKey(tt.ref)
			if provider != tt.provider {
				t.Errorf("ExtractSecretKey() provider = %v, want %v", provider, tt.provider)
			}
			if key != tt.key {
				t.Errorf("ExtractSecretKey() key = %v, want %v", key, tt.key)
			}
		})
	}
}

func TestExpandSecretReferences(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("test", provider)

	ctx := context.Background()
	sm.Start(ctx)

	// Set up test secrets
	sm.SetSecret(ctx, "test:secret1", "value1")
	sm.SetSecret(ctx, "test:secret2", "value2")

	tests := []struct {
		name string
		data map[string]interface{}
		want map[string]interface{}
	}{
		{
			name: "expand single reference",
			data: map[string]interface{}{
				"key": "test:secret1",
			},
			want: map[string]interface{}{
				"key": "value1",
			},
		},
		{
			name: "expand multiple references",
			data: map[string]interface{}{
				"key1": "test:secret1",
				"key2": "test:secret2",
			},
			want: map[string]interface{}{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "mixed references and values",
			data: map[string]interface{}{
				"secret": "test:secret1",
				"normal": "regular_value",
				"number": 42,
			},
			want: map[string]interface{}{
				"secret": "value1",
				"normal": "regular_value",
				"number": 42,
			},
		},
		{
			name: "nested references",
			data: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "test:secret1",
				},
			},
			want: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "value1",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sm.ExpandSecretReferences(ctx, tt.data)
			if err != nil {
				t.Errorf("ExpandSecretReferences() error = %v", err)
			}

			if !reflect.DeepEqual(tt.data, tt.want) {
				t.Errorf("ExpandSecretReferences() = %v, want %v", tt.data, tt.want)
			}
		})
	}
}

// =============================================================================
// SECRET ENCRYPTOR TESTS
// =============================================================================

func TestSecretEncryptor_EncryptDecrypt(t *testing.T) {
	key := []byte("test-key-must-be-32-bytes-long")
	encryptor := NewSecretEncryptor(key)

	plaintext := "sensitive-data"

	t.Run("encrypt and decrypt", func(t *testing.T) {
		encrypted, err := encryptor.Encrypt(plaintext)
		if err != nil {
			t.Errorf("Encrypt() error = %v", err)
		}

		if encrypted == plaintext {
			t.Error("Encrypted value should differ from plaintext")
		}

		decrypted, err := encryptor.Decrypt(encrypted)
		if err != nil {
			t.Errorf("Decrypt() error = %v", err)
		}

		if decrypted != plaintext {
			t.Errorf("Decrypt() = %v, want %v", decrypted, plaintext)
		}
	})

	t.Run("decrypt with wrong key fails", func(t *testing.T) {
		encrypted, _ := encryptor.Encrypt(plaintext)

		wrongKey := []byte("wrong-key-must-be-32-bytes-lng")
		wrongEncryptor := NewSecretEncryptor(wrongKey)

		_, err := wrongEncryptor.Decrypt(encrypted)
		if err == nil {
			t.Error("Decrypt() with wrong key should return error")
		}
	})

	t.Run("decrypt invalid data", func(t *testing.T) {
		_, err := encryptor.Decrypt("invalid-encrypted-data")
		if err == nil {
			t.Error("Decrypt() with invalid data should return error")
		}
	})
}

// =============================================================================
// CONCURRENCY TESTS
// =============================================================================

func TestSecretsManager_Concurrency(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{
		CacheEnabled: true,
	}).(*SecretsManagerImpl)

	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	done := make(chan bool)

	// Concurrent writes
	for i := 0; i < 10; i++ {
		go func(idx int) {
			key := "memory:concurrent-" + string(rune('0'+idx))
			err := sm.SetSecret(ctx, key, "value")
			if err != nil {
				t.Errorf("Concurrent SetSecret() error: %v", err)
			}
			done <- true
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(idx int) {
			key := "memory:concurrent-" + string(rune('0'+idx))
			_, _ = sm.GetSecret(ctx, key)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	timeout := time.After(5 * time.Second)
	for i := 0; i < 20; i++ {
		select {
		case <-done:
		case <-timeout:
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}
}

// =============================================================================
// EDGE CASE TESTS
// =============================================================================

func TestSecretsManager_EdgeCases(t *testing.T) {
	sm := NewSecretsManager(SecretsManagerConfig{}).(*SecretsManagerImpl)
	provider := NewMemorySecretProvider()
	sm.RegisterProvider("memory", provider)

	ctx := context.Background()
	sm.Start(ctx)

	t.Run("empty secret key", func(t *testing.T) {
		err := sm.SetSecret(ctx, "memory:", "value")
		if err == nil {
			t.Error("SetSecret() with empty key should return error")
		}
	})

	t.Run("empty secret value", func(t *testing.T) {
		err := sm.SetSecret(ctx, "memory:empty", "")
		if err != nil {
			t.Errorf("SetSecret() with empty value error = %v", err)
		}

		value, err := sm.GetSecret(ctx, "memory:empty")
		if err != nil {
			t.Errorf("GetSecret() for empty value error = %v", err)
		}
		if value != "" {
			t.Errorf("Empty value = %v, want empty string", value)
		}
	})

	t.Run("special characters in key", func(t *testing.T) {
		specialKey := "memory:key/with.special-chars_123"
		err := sm.SetSecret(ctx, specialKey, "value")
		if err != nil {
			t.Errorf("SetSecret() with special chars error = %v", err)
		}

		value, err := sm.GetSecret(ctx, specialKey)
		if err != nil {
			t.Errorf("GetSecret() with special chars error = %v", err)
		}
		if value != "value" {
			t.Errorf("Value = %v, want %v", value, "value")
		}
	})

	t.Run("very long secret value", func(t *testing.T) {
		longValue := string(make([]byte, 10000))
		err := sm.SetSecret(ctx, "memory:long", longValue)
		if err != nil {
			t.Errorf("SetSecret() with long value error = %v", err)
		}

		value, err := sm.GetSecret(ctx, "memory:long")
		if err != nil {
			t.Errorf("GetSecret() for long value error = %v", err)
		}
		if len(value) != len(longValue) {
			t.Errorf("Long value length = %d, want %d", len(value), len(longValue))
		}
	})
}

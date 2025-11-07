package providers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/auth"
	"github.com/xraph/forge/internal/logger"
)

func newMockLogger() forge.Logger {
	return logger.NewTestLogger()
}

func TestDefaultLDAPConfig(t *testing.T) {
	config := DefaultLDAPConfig()

	assert.Equal(t, 389, config.Port)
	assert.True(t, config.UseTLS)
	assert.False(t, config.InsecureSkipVerify)
	assert.Equal(t, 10, config.PoolSize)
	assert.Equal(t, 10*time.Second, config.ConnectionTimeout)
	assert.Equal(t, 30*time.Second, config.RequestTimeout)
	assert.Equal(t, 5*time.Minute, config.IdleTimeout)
	assert.Equal(t, 3, config.MaxRetries)
	assert.Equal(t, 100*time.Millisecond, config.RetryDelay)
	assert.True(t, config.EnableCache)
	assert.Equal(t, 5*time.Minute, config.CacheTTL)
	assert.Equal(t, "(uid=%s)", config.SearchFilter)
	assert.Equal(t, "(member=%s)", config.GroupFilter)
	assert.True(t, config.EnableReferrals)
	assert.Equal(t, 1000, config.PageSize)
	assert.Contains(t, config.Attributes, "uid")
	assert.Contains(t, config.Attributes, "mail")
}

func TestNewLDAPProvider_ValidationErrors(t *testing.T) {
	logger := newMockLogger()

	tests := []struct {
		name        string
		config      LDAPConfig
		expectedErr string
	}{
		{
			name: "missing host",
			config: LDAPConfig{
				BaseDN: "dc=example,dc=com",
				BindDN: "cn=admin,dc=example,dc=com",
			},
			expectedErr: "ldap host is required",
		},
		{
			name: "missing base DN",
			config: LDAPConfig{
				Host:   "ldap.example.com",
				BindDN: "cn=admin,dc=example,dc=com",
			},
			expectedErr: "ldap base_dn is required",
		},
		{
			name: "missing bind DN",
			config: LDAPConfig{
				Host:   "ldap.example.com",
				BaseDN: "dc=example,dc=com",
			},
			expectedErr: "ldap bind_dn is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewLDAPProvider(tt.config, logger)
			assert.Error(t, err)
			assert.Nil(t, provider)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestLDAPProvider_Name(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1, // Small pool for testing
		EnableCache:  false,
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	assert.Equal(t, "ldap", provider.Name())
}

func TestLDAPProvider_Type(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	assert.Equal(t, auth.SecurityTypeHTTP, provider.Type())
}

func TestLDAPProvider_OpenAPIScheme(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	scheme := provider.OpenAPIScheme()
	assert.Equal(t, string(auth.SecurityTypeHTTP), scheme.Type)
	assert.Equal(t, "basic", scheme.Scheme)
	assert.Contains(t, scheme.Description, "LDAP")
}

func TestLDAPProvider_Authenticate_MissingCredentials(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	// Create request without auth header
	req := httptest.NewRequest("GET", "/", nil)

	authCtx, err := provider.Authenticate(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, authCtx)
	assert.Contains(t, err.Error(), "missing or invalid basic auth credentials")
}

func TestLDAPProvider_Authenticate_EmptyCredentials(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	// Create request with empty credentials
	req := httptest.NewRequest("GET", "/", nil)
	req.SetBasicAuth("", "")

	authCtx, err := provider.Authenticate(context.Background(), req)
	assert.Error(t, err)
	assert.Nil(t, authCtx)
	assert.Contains(t, err.Error(), "missing or invalid basic auth credentials")
}

func TestLDAPProvider_Middleware(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	middleware := provider.Middleware()
	assert.NotNil(t, middleware)

	// Test middleware with missing auth
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	assert.False(t, handlerCalled, "handler should not be called without auth")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestLDAPCache_SetAndGet(t *testing.T) {
	cache := &ldapCache{
		entries: make(map[string]*cacheEntry),
		ttl:     5 * time.Minute,
	}

	authCtx := &auth.AuthContext{
		Subject: "testuser",
		Claims: map[string]interface{}{
			"email": "test@example.com",
		},
		Scopes: []string{"user"},
	}

	// Set in cache
	cache.set("testuser", "password123", authCtx)

	// Get from cache
	retrieved := cache.get("testuser", "password123")
	assert.NotNil(t, retrieved)
	assert.Equal(t, "testuser", retrieved.Subject)
	assert.Equal(t, "test@example.com", retrieved.Claims["email"])
	assert.Contains(t, retrieved.Scopes, "user")
}

func TestLDAPCache_Expiration(t *testing.T) {
	cache := &ldapCache{
		entries: make(map[string]*cacheEntry),
		ttl:     100 * time.Millisecond, // Short TTL for testing
	}

	authCtx := &auth.AuthContext{
		Subject: "testuser",
	}

	// Set in cache
	cache.set("testuser", "password123", authCtx)

	// Should be retrievable immediately
	retrieved := cache.get("testuser", "password123")
	assert.NotNil(t, retrieved)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Should be expired
	retrieved = cache.get("testuser", "password123")
	assert.Nil(t, retrieved)
}

func TestLDAPCache_CacheKey(t *testing.T) {
	cache := &ldapCache{
		entries: make(map[string]*cacheEntry),
		ttl:     5 * time.Minute,
	}

	key1 := cache.cacheKey("user1", "pass1")
	key2 := cache.cacheKey("user1", "pass2")
	key3 := cache.cacheKey("user2", "pass1")

	// Different passwords should produce different keys
	assert.NotEqual(t, key1, key2)

	// Different users should produce different keys
	assert.NotEqual(t, key1, key3)

	// Same credentials should produce same key
	key1Again := cache.cacheKey("user1", "pass1")
	assert.Equal(t, key1, key1Again)
}

func TestLDAPCache_Cleanup(t *testing.T) {
	cache := &ldapCache{
		entries: make(map[string]*cacheEntry),
		ttl:     50 * time.Millisecond,
	}

	authCtx := &auth.AuthContext{Subject: "testuser"}

	// Add multiple entries
	cache.set("user1", "pass1", authCtx)
	cache.set("user2", "pass2", authCtx)
	cache.set("user3", "pass3", authCtx)

	assert.Len(t, cache.entries, 3)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Manually trigger cleanup (normally runs in goroutine)
	cache.mu.Lock()
	now := time.Now()
	for key, entry := range cache.entries {
		if now.After(entry.expiresAt) {
			delete(cache.entries, key)
		}
	}
	cache.mu.Unlock()

	// All entries should be cleaned up
	assert.Len(t, cache.entries, 0)
}

func TestLDAPProvider_MapGroupsToRoles(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
		RoleMapping: map[string]string{
			"cn=admins,ou=groups,dc=example,dc=com":     "admin",
			"cn=developers,ou=groups,dc=example,dc=com": "developer",
			"cn=users,ou=groups,dc=example,dc=com":      "user",
		},
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	tests := []struct {
		name     string
		groups   []string
		expected []string
	}{
		{
			name: "single mapped group",
			groups: []string{
				"cn=admins,ou=groups,dc=example,dc=com",
			},
			expected: []string{"admin"},
		},
		{
			name: "multiple mapped groups",
			groups: []string{
				"cn=admins,ou=groups,dc=example,dc=com",
				"cn=developers,ou=groups,dc=example,dc=com",
			},
			expected: []string{"admin", "developer"},
		},
		{
			name: "unmapped group",
			groups: []string{
				"cn=guests,ou=groups,dc=example,dc=com",
			},
			expected: []string{},
		},
		{
			name: "mixed mapped and unmapped",
			groups: []string{
				"cn=admins,ou=groups,dc=example,dc=com",
				"cn=guests,ou=groups,dc=example,dc=com",
				"cn=users,ou=groups,dc=example,dc=com",
			},
			expected: []string{"admin", "user"},
		},
		{
			name:     "empty groups",
			groups:   []string{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roles := provider.mapGroupsToRoles(tt.groups)
			assert.ElementsMatch(t, tt.expected, roles)
		})
	}
}

func TestLDAPProvider_MapGroupsToRoles_NoMapping(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
		// No RoleMapping configured
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	groups := []string{
		"cn=admins,ou=groups,dc=example,dc=com",
		"cn=users,ou=groups,dc=example,dc=com",
	}

	// When no mapping is configured, should return original groups
	roles := provider.mapGroupsToRoles(groups)
	assert.ElementsMatch(t, groups, roles)
}

func TestLDAPProvider_ConfigDefaults(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		// PoolSize not set - should use default
		// Timeouts not set - should use defaults
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)
	defer provider.Close()

	// Check defaults were applied
	assert.Equal(t, 10, provider.config.PoolSize)
	assert.Equal(t, 10*time.Second, provider.config.ConnectionTimeout)
	assert.Equal(t, 30*time.Second, provider.config.RequestTimeout)
	assert.Equal(t, 5*time.Minute, provider.config.CacheTTL)
}

func TestLDAPProvider_Close(t *testing.T) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     2,
		EnableCache:  false,
	}

	provider, err := NewLDAPProvider(config, newMockLogger())
	require.NoError(t, err)

	// Close should not error
	err = provider.Close()
	assert.NoError(t, err)

	// Pool should be closed
	assert.True(t, provider.connPool.closed)
}

func TestLDAPConnPool_CloseIdempotent(t *testing.T) {
	pool := &ldapConnPool{
		conns:  make(chan *ldap.Conn, 1),
		logger: newMockLogger(),
		closed: false,
	}

	// First close
	err := pool.close()
	assert.NoError(t, err)
	assert.True(t, pool.closed)

	// Second close should also work
	err = pool.close()
	assert.NoError(t, err)
	assert.True(t, pool.closed)
}

// Benchmark tests
func BenchmarkLDAPCache_Set(b *testing.B) {
	cache := &ldapCache{
		entries: make(map[string]*cacheEntry),
		ttl:     5 * time.Minute,
	}

	authCtx := &auth.AuthContext{
		Subject: "testuser",
		Claims:  map[string]interface{}{"email": "test@example.com"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		username := fmt.Sprintf("user%d", i%1000)
		cache.set(username, "password", authCtx)
	}
}

func BenchmarkLDAPCache_Get(b *testing.B) {
	cache := &ldapCache{
		entries: make(map[string]*cacheEntry),
		ttl:     5 * time.Minute,
	}

	authCtx := &auth.AuthContext{
		Subject: "testuser",
		Claims:  map[string]interface{}{"email": "test@example.com"},
	}

	// Pre-populate cache
	for i := 0; i < 1000; i++ {
		username := fmt.Sprintf("user%d", i)
		cache.set(username, "password", authCtx)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		username := fmt.Sprintf("user%d", i%1000)
		cache.get(username, "password")
	}
}

func BenchmarkLDAPProvider_MapGroupsToRoles(b *testing.B) {
	config := LDAPConfig{
		Host:         "ldap.example.com",
		BaseDN:       "dc=example,dc=com",
		BindDN:       "cn=admin,dc=example,dc=com",
		BindPassword: "password",
		PoolSize:     1,
		EnableCache:  false,
		RoleMapping: map[string]string{
			"cn=admins,ou=groups,dc=example,dc=com":     "admin",
			"cn=developers,ou=groups,dc=example,dc=com": "developer",
			"cn=users,ou=groups,dc=example,dc=com":      "user",
			"cn=guests,ou=groups,dc=example,dc=com":     "guest",
			"cn=managers,ou=groups,dc=example,dc=com":   "manager",
		},
	}

	provider, _ := NewLDAPProvider(config, newMockLogger())
	defer provider.Close()

	groups := []string{
		"cn=admins,ou=groups,dc=example,dc=com",
		"cn=developers,ou=groups,dc=example,dc=com",
		"cn=users,ou=groups,dc=example,dc=com",
		"cn=other,ou=groups,dc=example,dc=com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider.mapGroupsToRoles(groups)
	}
}

// Helper function to create basic auth header
func createBasicAuthHeader(username, password string) string {
	auth := username + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
}

// Test concurrent cache access
func TestLDAPCache_ConcurrentAccess(t *testing.T) {
	cache := &ldapCache{
		entries: make(map[string]*cacheEntry),
		ttl:     5 * time.Minute,
	}

	authCtx := &auth.AuthContext{
		Subject: "testuser",
		Claims:  map[string]interface{}{"email": "test@example.com"},
	}

	// Concurrent writes
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				username := fmt.Sprintf("user%d-%d", id, j)
				cache.set(username, "password", authCtx)
			}
			done <- true
		}(i)
	}

	// Wait for all writes
	for i := 0; i < 10; i++ {
		<-done
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				username := fmt.Sprintf("user%d-%d", id, j)
				cache.get(username, "password")
			}
			done <- true
		}(i)
	}

	// Wait for all reads
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should not panic or race
	assert.True(t, len(cache.entries) > 0)
}

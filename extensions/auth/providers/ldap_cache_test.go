package providers

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/auth"
)

// Every cache hit used to return the same *AuthContext, so a change made to
// one request's context (scopes merged by the registry, ProviderName) stuck
// to the cached login and showed up on later requests.
func TestLDAPCache_ReturnsCopies(t *testing.T) {
	cache := &ldapCache{entries: make(map[string]*cacheEntry), ttl: time.Minute}

	orig := &auth.AuthContext{Subject: "alice", Scopes: []string{"user"}, Claims: map[string]any{"mail": "a@x"}}
	cache.set("alice", "pw", orig)

	// Changing the caller's object after set must not reach the cache.
	orig.Scopes[0] = "admin"

	first := cache.get("alice", "pw")
	if first == nil || first.Scopes[0] != "user" {
		t.Fatalf("cache kept a reference to the caller's context: %+v", first)
	}

	first.Scopes = append(first.Scopes, "admin")
	first.Claims["extra"] = true
	first.ProviderName = "changed"

	second := cache.get("alice", "pw")
	if len(second.Scopes) != 1 || len(second.Claims) != 1 || second.ProviderName != "" {
		t.Fatalf("a change to one hit leaked into the next: %+v", second)
	}
}

// The cache key used to be "user:password" in plain text.
func TestLDAPCache_KeyDoesNotContainPassword(t *testing.T) {
	cache := &ldapCache{entries: make(map[string]*cacheEntry), ttl: time.Minute}

	key := cache.cacheKey("alice", "s3cret-password")
	if strings.Contains(key, "s3cret-password") || strings.Contains(key, "alice") {
		t.Fatalf("cache key exposes the credentials: %q", key)
	}

	if cache.cacheKey("ab", "c") == cache.cacheKey("a", "bc") {
		t.Fatal("username and password boundary is ambiguous")
	}

	cache.set("alice", "s3cret-password", &auth.AuthContext{Subject: "alice"})

	if cache.get("alice", "wrong") != nil {
		t.Fatal("wrong password hit the cache")
	}

	if cache.get("alice", "s3cret-password") == nil {
		t.Fatal("right password missed the cache")
	}
}

func TestLDAPProvider_CloseStopsCacheCleanup(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 20 {
		p, err := NewLDAPProvider(LDAPConfig{
			Host:         "ldap.example.com",
			BaseDN:       "dc=example,dc=com",
			BindDN:       "cn=admin,dc=example,dc=com",
			BindPassword: "password",
			PoolSize:     1,
			EnableCache:  true,
			CacheTTL:     time.Hour,
		}, newMockLogger())
		if err != nil {
			t.Fatal(err)
		}

		_ = p.Close()
		_ = p.Close() // idempotent
	}

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if n := runtime.NumGoroutine(); n > before+3 {
		t.Fatalf("%d goroutines after Close, started with %d", n, before)
	}
}

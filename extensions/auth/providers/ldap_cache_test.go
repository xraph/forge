package providers

import (
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

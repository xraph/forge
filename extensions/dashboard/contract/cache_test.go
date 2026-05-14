package contract

import (
	"testing"
	"time"
)

func TestGraphCache_HitMiss(t *testing.T) {
	c := NewGraphCache(2, time.Minute)
	key := GraphCacheKey{Route: "/users", PermissionsHash: "h1", ShellVersion: "v1"}
	if _, ok := c.Get(key); ok {
		t.Error("expected miss")
	}
	c.Put(key, &GraphNode{Intent: "page.shell"})
	got, ok := c.Get(key)
	if !ok || got.Intent != "page.shell" {
		t.Errorf("expected hit, got %+v ok=%v", got, ok)
	}
}

func TestGraphCache_Eviction(t *testing.T) {
	c := NewGraphCache(2, time.Minute)
	c.Put(GraphCacheKey{Route: "/a"}, &GraphNode{Intent: "a"})
	c.Put(GraphCacheKey{Route: "/b"}, &GraphNode{Intent: "b"})
	c.Put(GraphCacheKey{Route: "/c"}, &GraphNode{Intent: "c"}) // evicts /a
	if _, ok := c.Get(GraphCacheKey{Route: "/a"}); ok {
		t.Error("expected /a evicted")
	}
}

func TestGraphCache_TTLExpiry(t *testing.T) {
	c := NewGraphCache(2, 10*time.Millisecond)
	c.Put(GraphCacheKey{Route: "/x"}, &GraphNode{Intent: "x"})
	time.Sleep(20 * time.Millisecond)
	if _, ok := c.Get(GraphCacheKey{Route: "/x"}); ok {
		t.Error("expected ttl expiry")
	}
}

func TestGraphCache_BustAll(t *testing.T) {
	c := NewGraphCache(4, time.Minute)
	c.Put(GraphCacheKey{Route: "/a"}, &GraphNode{Intent: "a"})
	c.BustAll()
	if _, ok := c.Get(GraphCacheKey{Route: "/a"}); ok {
		t.Error("BustAll should clear")
	}
}

package idempotency

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func TestInMemory_LookupMissThenHit(t *testing.T) {
	s := NewInMemoryStore()
	if _, ok := s.Lookup(context.Background(), "k", "u"); ok {
		t.Error("expected miss")
	}
	c := Cached{Status: 200, WireBody: json.RawMessage(`{"x":1}`), StoredAt: time.Now(), TTL: time.Hour}
	if err := s.Store(context.Background(), "k", "u", c); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, ok := s.Lookup(context.Background(), "k", "u")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Status != 200 || string(got.WireBody) != `{"x":1}` {
		t.Errorf("cached value lost: %+v", got)
	}
}

func TestInMemory_DifferentIdentityIsIndependent(t *testing.T) {
	s := NewInMemoryStore()
	c := Cached{WireBody: json.RawMessage(`null`), StoredAt: time.Now(), TTL: time.Hour}
	_ = s.Store(context.Background(), "k", "alice", c)
	if _, ok := s.Lookup(context.Background(), "k", "bob"); ok {
		t.Error("bob should not see alice's cached entry")
	}
}

func TestInMemory_ExpiredEntryReturnsMiss(t *testing.T) {
	s := NewInMemoryStore()
	c := Cached{StoredAt: time.Now().Add(-2 * time.Hour), TTL: time.Hour, WireBody: json.RawMessage(`null`)}
	_ = s.Store(context.Background(), "k", "u", c)
	if _, ok := s.Lookup(context.Background(), "k", "u"); ok {
		t.Error("expected expired entry to miss")
	}
}

func TestInMemory_LRUEvictionAtCapacity(t *testing.T) {
	s := NewInMemoryStore(WithMaxEntries(2))
	now := time.Now()
	_ = s.Store(context.Background(), "k1", "u", Cached{StoredAt: now, TTL: time.Hour, WireBody: json.RawMessage(`1`)})
	_ = s.Store(context.Background(), "k2", "u", Cached{StoredAt: now, TTL: time.Hour, WireBody: json.RawMessage(`2`)})
	_ = s.Store(context.Background(), "k3", "u", Cached{StoredAt: now, TTL: time.Hour, WireBody: json.RawMessage(`3`)})
	// k1 should be evicted (oldest, capacity=2).
	if _, ok := s.Lookup(context.Background(), "k1", "u"); ok {
		t.Error("k1 should have been evicted")
	}
	if _, ok := s.Lookup(context.Background(), "k2", "u"); !ok {
		t.Error("k2 should still be present")
	}
	if _, ok := s.Lookup(context.Background(), "k3", "u"); !ok {
		t.Error("k3 should still be present")
	}
}

func TestInMemory_ConcurrentReadWrite(t *testing.T) {
	s := NewInMemoryStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			key := "k"
			s.Store(context.Background(), key, "u", Cached{StoredAt: time.Now(), TTL: time.Hour, WireBody: json.RawMessage(`null`)})
			_ = i
		}(i)
		go func(i int) {
			defer wg.Done()
			s.Lookup(context.Background(), "k", "u")
			_ = i
		}(i)
	}
	wg.Wait()
}

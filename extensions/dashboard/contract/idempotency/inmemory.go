package idempotency

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// DefaultMaxEntries is the default LRU cap for an in-memory store.
const DefaultMaxEntries = 10000

// Option configures an InMemoryStore.
type Option func(*InMemoryStore)

// WithMaxEntries caps the number of cached entries; oldest are evicted first.
func WithMaxEntries(n int) Option {
	return func(s *InMemoryStore) {
		if n > 0 {
			s.maxEntries = n
		}
	}
}

// InMemoryStore is a process-local Store with TTL and LRU eviction.
// Safe for concurrent use.
type InMemoryStore struct {
	mu         sync.Mutex
	maxEntries int
	entries    map[entryKey]*list.Element
	order      *list.List // front = MRU, back = LRU
}

type entryKey struct {
	Key      string
	Identity string
}

type entry struct {
	key entryKey
	val Cached
}

// NewInMemoryStore returns an in-memory Store with the given options.
func NewInMemoryStore(opts ...Option) *InMemoryStore {
	s := &InMemoryStore{
		maxEntries: DefaultMaxEntries,
		entries:    map[entryKey]*list.Element{},
		order:      list.New(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Lookup implements Store.
func (s *InMemoryStore) Lookup(_ context.Context, key, identity string) (*Cached, bool) {
	k := entryKey{key, identity}
	s.mu.Lock()
	defer s.mu.Unlock()
	el, ok := s.entries[k]
	if !ok {
		return nil, false
	}
	e := el.Value.(*entry)
	if e.val.Expired(time.Now()) {
		s.order.Remove(el)
		delete(s.entries, k)
		return nil, false
	}
	s.order.MoveToFront(el)
	c := e.val // copy
	return &c, true
}

// Store implements Store. Returns nil; signature reserves error for future
// backends (e.g., Redis).
func (s *InMemoryStore) Store(_ context.Context, key, identity string, c Cached) error {
	k := entryKey{key, identity}
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.entries[k]; ok {
		e := el.Value.(*entry)
		e.val = c
		s.order.MoveToFront(el)
		return nil
	}
	el := s.order.PushFront(&entry{key: k, val: c})
	s.entries[k] = el
	for s.order.Len() > s.maxEntries {
		oldest := s.order.Back()
		if oldest != nil {
			s.order.Remove(oldest)
			delete(s.entries, oldest.Value.(*entry).key)
		}
	}
	return nil
}

// Compile-time assertion.
var _ Store = (*InMemoryStore)(nil)

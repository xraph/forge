package idempotency

import (
	"bytes"
	"container/list"
	"context"
	"sync"
	"time"
)

// DefaultMaxEntries caps how many completed responses a MemoryStore keeps.
const DefaultMaxEntries = 10000

// MemoryOption configures a MemoryStore.
type MemoryOption func(*MemoryStore)

// WithMaxEntries caps the number of completed responses kept; the least
// recently used is evicted first. Values below 1 are ignored.
func WithMaxEntries(n int) MemoryOption {
	return func(s *MemoryStore) {
		if n > 0 {
			s.maxEntries = n
		}
	}
}

// WithClock replaces time.Now, for tests.
func WithClock(now func() time.Time) MemoryOption {
	return func(s *MemoryStore) {
		if now != nil {
			s.now = now
		}
	}
}

// MemoryStore is a process-local Store. Claims expire with their lease,
// responses with their ExpiresAt, and completed responses beyond the cap are
// evicted least recently used first. Claims are not counted against the cap,
// so a burst of new keys never pushes out a stored response.
type MemoryStore struct {
	mu         sync.Mutex
	maxEntries int
	now        func() time.Time
	entries    map[Key]*memEntry
	completed  *list.List // of *memEntry; front is most recently used
}

type memEntry struct {
	key         Key
	fingerprint string
	done        chan struct{} // non-nil while claimed
	leaseUntil  time.Time
	resp        *Response
	lru         *list.Element
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore(opts ...MemoryOption) *MemoryStore {
	s := &MemoryStore{
		maxEntries: DefaultMaxEntries,
		now:        time.Now,
		entries:    map[Key]*memEntry{},
		completed:  list.New(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

var (
	defaultOnce  sync.Once
	defaultStore *MemoryStore
)

// Default returns the process-wide store used when a route opts into
// idempotency without naming one.
func Default() *MemoryStore {
	defaultOnce.Do(func() { defaultStore = NewMemoryStore() })

	return defaultStore
}

// Begin implements Store.
func (s *MemoryStore) Begin(_ context.Context, key Key, fingerprint string, lease time.Duration) (Begun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()

	if e, ok := s.entries[key]; ok {
		switch {
		case e.resp != nil && (e.resp.ExpiresAt.IsZero() || now.Before(e.resp.ExpiresAt)):
			s.completed.MoveToFront(e.lru)
			r := copyResponse(*e.resp)

			return Begun{State: Replay, Fingerprint: r.Fingerprint, Response: &r}, nil
		case e.resp == nil && now.Before(e.leaseUntil):
			return Begun{State: InFlight, Fingerprint: e.fingerprint, Done: e.done}, nil
		}

		s.drop(e)
	}

	s.entries[key] = &memEntry{
		key:         key,
		fingerprint: fingerprint,
		done:        make(chan struct{}),
		leaseUntil:  now.Add(lease),
	}

	return Begun{State: Acquired}, nil
}

// Complete implements Store.
func (s *MemoryStore) Complete(_ context.Context, key Key, resp Response) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		e = &memEntry{key: key}
		s.entries[key] = e
	}

	if e.done != nil {
		close(e.done)
		e.done = nil
	}

	stored := copyResponse(resp)
	e.resp = &stored

	if e.lru == nil {
		e.lru = s.completed.PushFront(e)
	} else {
		s.completed.MoveToFront(e.lru)
	}

	for s.completed.Len() > s.maxEntries {
		oldest := s.completed.Back()
		s.drop(oldest.Value.(*memEntry))
	}

	return nil
}

// Release implements Store.
func (s *MemoryStore) Release(_ context.Context, key Key) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if e, ok := s.entries[key]; ok && e.resp == nil {
		s.drop(e)
	}

	return nil
}

// drop removes e and wakes anyone waiting on it. Caller holds s.mu.
func (s *MemoryStore) drop(e *memEntry) {
	if e.done != nil {
		close(e.done)
		e.done = nil
	}

	if e.lru != nil {
		s.completed.Remove(e.lru)
		e.lru = nil
	}

	delete(s.entries, e.key)
}

func copyResponse(r Response) Response {
	r.Header = r.Header.Clone()
	r.Body = bytes.Clone(r.Body)

	return r
}

var _ Store = (*MemoryStore)(nil)

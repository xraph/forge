package idempotency

import (
	"bytes"
	"container/heap"
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
//
// Lapsed claims are swept on every Begin, soonest lease first, so a client that
// abandons unique keys cannot grow the store without bound. Each claim is
// removed once, so the sweep costs O(log n) amortised per claim.
type MemoryStore struct {
	mu         sync.Mutex
	maxEntries int
	now        func() time.Time
	entries    map[Key]*memEntry
	completed  *list.List // of *memEntry; front is most recently used
	claims     claimQueue // claimed entries, soonest leaseUntil first
	lastToken  Token
}

// memEntry is either a claim (resp nil, token and done set, in claims) or a
// stored response (resp set, token zero, in completed).
type memEntry struct {
	key         Key
	fingerprint string
	token       Token
	done        chan struct{} // non-nil while claimed
	leaseUntil  time.Time
	claimIdx    int // index in MemoryStore.claims, -1 when not queued
	resp        *Response
	lru         *list.Element
}

// claimQueue is a min-heap of claimed entries ordered by leaseUntil.
type claimQueue []*memEntry

func (q *claimQueue) Len() int           { return len(*q) }
func (q *claimQueue) Less(i, j int) bool { return (*q)[i].leaseUntil.Before((*q)[j].leaseUntil) }

func (q *claimQueue) Swap(i, j int) {
	(*q)[i], (*q)[j] = (*q)[j], (*q)[i]
	(*q)[i].claimIdx = i
	(*q)[j].claimIdx = j
}

func (q *claimQueue) Push(x any) {
	e := x.(*memEntry)
	e.claimIdx = len(*q)
	*q = append(*q, e)
}

func (q *claimQueue) Pop() any {
	old := *q
	n := len(old)
	e := old[n-1]
	old[n-1] = nil
	e.claimIdx = -1
	*q = old[:n-1]

	return e
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

	s.sweep(now)

	if lease <= 0 {
		lease = DefaultLease
	}

	if e, ok := s.entries[key]; ok {
		switch {
		case e.resp != nil && (e.resp.ExpiresAt.IsZero() || now.Before(e.resp.ExpiresAt)):
			s.completed.MoveToFront(e.lru)
			r := copyResponse(*e.resp)

			return Begun{State: Replay, Fingerprint: r.Fingerprint, Response: &r}, nil
		case e.resp == nil:
			// A lapsed claim was swept above, so what is left is live.
			return Begun{State: InFlight, Fingerprint: e.fingerprint, Done: e.done}, nil
		}

		// A stored response past its ExpiresAt.
		s.drop(e)
	}

	s.lastToken++

	e := &memEntry{
		key:         key,
		fingerprint: fingerprint,
		token:       s.lastToken,
		done:        make(chan struct{}),
		leaseUntil:  now.Add(lease),
		claimIdx:    -1,
	}
	s.entries[key] = e
	heap.Push(&s.claims, e)

	return Begun{State: Acquired, Token: e.token}, nil
}

// Complete implements Store.
func (s *MemoryStore) Complete(_ context.Context, key Key, token Token, resp Response) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]

	switch {
	case token == 0 && ok && e.resp == nil:
		return ErrNotHolder // somebody holds the key
	case token != 0 && (!ok || e.token != token):
		return ErrNotHolder // the claim lapsed, was swept or was retaken
	}

	if !ok {
		e = &memEntry{key: key, claimIdx: -1}
		s.entries[key] = e
	}

	if resp.Fingerprint == "" {
		resp.Fingerprint = e.fingerprint
	}

	s.endClaim(e)

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
func (s *MemoryStore) Release(_ context.Context, key Key, token Token) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[key]
	if !ok {
		return nil
	}

	if e.resp != nil || e.token != token {
		return ErrNotHolder
	}

	s.drop(e)

	return nil
}

// sweep drops every claim whose lease has lapsed, waking its waiters. Caller
// holds s.mu.
func (s *MemoryStore) sweep(now time.Time) {
	for len(s.claims) > 0 && !now.Before(s.claims[0].leaseUntil) {
		s.drop(s.claims[0])
	}
}

// endClaim closes a claim's Done and takes it out of the lease queue, leaving
// the entry itself in place. Caller holds s.mu.
func (s *MemoryStore) endClaim(e *memEntry) {
	if e.done != nil {
		close(e.done)
		e.done = nil
	}

	if e.claimIdx >= 0 {
		heap.Remove(&s.claims, e.claimIdx)
	}

	e.token = 0
}

// drop removes e and wakes anyone waiting on it. Caller holds s.mu.
func (s *MemoryStore) drop(e *memEntry) {
	s.endClaim(e)

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

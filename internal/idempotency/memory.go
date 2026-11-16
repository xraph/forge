package idempotency

import (
	"bytes"
	"container/heap"
	"container/list"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DefaultMaxEntries caps how many completed responses a MemoryStore keeps.
const DefaultMaxEntries = 10000

// DefaultMaxBytes caps the approximate memory the completed responses in a
// MemoryStore take, across all principals.
const DefaultMaxBytes int64 = 256 << 20 // 256 MiB

// principalShare is the default fraction of the store-wide caps one principal
// may use: a tenth of the entries and a tenth of the bytes. With the
// store-wide defaults that is 1000 entries and 25.6 MiB per principal.
const principalShare = 10

// entryOverhead approximates the bookkeeping a stored response costs beyond
// its key, header and body bytes, so a flood of empty responses is not free.
const entryOverhead = 256

// ErrQuotaExceeded is returned, wrapped in a *QuotaError, by Begin when the
// key's principal already holds as many claims and stored responses, or as
// many stored bytes, as its quota allows. Nothing is evicted to make room.
var ErrQuotaExceeded = errors.New("idempotency: principal is over its storage quota")

// QuotaError is the error Begin returns when a principal is over its quota.
// errors.Is(err, ErrQuotaExceeded) matches it.
type QuotaError struct {
	// Principal is the principal that is over quota.
	Principal string
	// RetryAfter is how long until the principal's soonest stored response
	// expires and frees room. Zero when the store cannot tell.
	RetryAfter time.Duration
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("%v (principal %q)", ErrQuotaExceeded, e.Principal)
}

// Is reports whether target is ErrQuotaExceeded.
func (e *QuotaError) Is(target error) bool { return target == ErrQuotaExceeded }

// MemoryOption configures a MemoryStore.
type MemoryOption func(*MemoryStore)

// WithMaxEntries caps the number of completed responses kept across all
// principals; the least recently used is evicted first. Values below 1 are
// ignored. Unless WithMaxEntriesPerPrincipal is given, one principal may hold
// a tenth of this.
func WithMaxEntries(n int) MemoryOption {
	return func(s *MemoryStore) {
		if n > 0 {
			s.maxEntries = n
		}
	}
}

// WithMaxBytes caps the approximate bytes the completed responses take across
// all principals; the least recently used is evicted first. Values below 1
// are ignored. Unless WithMaxBytesPerPrincipal is given, one principal may
// hold a tenth of this.
func WithMaxBytes(n int64) MemoryOption {
	return func(s *MemoryStore) {
		if n > 0 {
			s.maxBytes = n
		}
	}
}

// WithMaxEntriesPerPrincipal caps the claims and stored responses one
// principal may hold. A principal at the cap has new keys refused with a
// *QuotaError until some of its responses expire. A negative value removes
// the cap, which lets one principal fill the store and push out other
// principals' responses; zero keeps the default.
func WithMaxEntriesPerPrincipal(n int) MemoryOption {
	return func(s *MemoryStore) {
		if n != 0 {
			s.principalEntries = n
		}
	}
}

// WithMaxBytesPerPrincipal caps the approximate bytes one principal's stored
// responses may take. A principal at the cap has new keys refused with a
// *QuotaError. A response that finishes while its principal is near the cap
// is still stored, so the cap can be overshot by the responses in flight. A
// negative value removes the cap; zero keeps the default.
func WithMaxBytesPerPrincipal(n int64) MemoryOption {
	return func(s *MemoryStore) {
		if n != 0 {
			s.principalBytes = n
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
// responses with their ExpiresAt, and completed responses beyond the
// store-wide caps are evicted least recently used first. Claims are not
// counted against the store-wide caps, so a burst of new keys never pushes
// out a stored response.
//
// Each principal also has a quota of entries (claims and responses) and of
// bytes, by default a tenth of the store-wide caps. A principal at its quota
// has new keys refused rather than being allowed to evict anything. Without
// the quota, one principal sending fresh keys could push every other
// principal's stored responses out of the shared LRU, and a victim's retry
// would then run the handler a second time. With it, filling the store takes
// many principals, not one.
//
// Lapsed claims are swept on every Begin, soonest lease first, so a client that
// abandons unique keys cannot grow the store without bound. Each claim is
// removed once, so the sweep costs O(log n) amortised per claim. Expired
// responses are dropped when they are looked up, and a principal's expired
// responses are dropped when it reaches its quota.
type MemoryStore struct {
	mu               sync.Mutex
	maxEntries       int
	maxBytes         int64
	principalEntries int   // 0 means derive from maxEntries; negative is unlimited
	principalBytes   int64 // 0 means derive from maxBytes; negative is unlimited
	now              func() time.Time
	entries          map[Key]*memEntry
	completed        *list.List // of *memEntry; front is most recently used
	bytes            int64      // size of every stored response
	usage            map[string]*principalUsage
	claims           claimQueue // claimed entries, soonest leaseUntil first
	lastToken        Token
}

// principalUsage is what one principal holds in the store.
type principalUsage struct {
	entries int        // claims plus stored responses
	bytes   int64      // size of the stored responses
	own     *list.List // of *memEntry, every entry the principal holds

	// After a scan for expired responses, scanned is set and nextExpiry is
	// the soonest ExpiresAt that remained (zero if none). Until then nothing
	// can have expired, so a principal hammering at its quota does not cost
	// a scan per request.
	scanned    bool
	nextExpiry time.Time
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
	size        int64 // approximate bytes resp takes; 0 for a claim
	lru         *list.Element
	own         *list.Element // in the principal's usage.own
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
		maxBytes:   DefaultMaxBytes,
		now:        time.Now,
		entries:    map[Key]*memEntry{},
		completed:  list.New(),
		usage:      map[string]*principalUsage{},
	}

	for _, opt := range opts {
		opt(s)
	}

	if s.principalEntries == 0 {
		s.principalEntries = max(s.maxEntries/principalShare, 1)
	}

	if s.principalBytes == 0 {
		s.principalBytes = max(s.maxBytes/principalShare, 1)
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

	if err := s.checkQuota(key.Principal, now); err != nil {
		return Begun{}, err
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
	s.track(e)
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
		// A Complete without a Begin is not checked against the quota,
		// because the work it records has already happened, but it is
		// counted.
		e = &memEntry{key: key, claimIdx: -1}
		s.entries[key] = e
		s.track(e)
	}

	if resp.Fingerprint == "" {
		resp.Fingerprint = e.fingerprint
	}

	s.endClaim(e)

	stored := copyResponse(resp)
	e.resp = &stored
	s.resize(e, responseSize(key, &stored))

	if e.lru == nil {
		e.lru = s.completed.PushFront(e)
	} else {
		s.completed.MoveToFront(e.lru)
	}

	// The store-wide caps are a backstop. The per-principal quota keeps any
	// one principal well below them, so reaching them takes many principals.
	// The entry just stored is never the one evicted, even if it alone is
	// over the byte cap.
	for s.completed.Len() > s.maxEntries || (s.bytes > s.maxBytes && s.completed.Len() > 1) {
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

	s.resize(e, 0)
	s.untrack(e)
	delete(s.entries, e.key)
}

// track counts a new entry against its principal. Caller holds s.mu.
func (s *MemoryStore) track(e *memEntry) {
	u := s.usage[e.key.Principal]
	if u == nil {
		u = &principalUsage{own: list.New()}
		s.usage[e.key.Principal] = u
	}

	u.entries++
	e.own = u.own.PushBack(e)
}

// untrack reverses track. Caller holds s.mu.
func (s *MemoryStore) untrack(e *memEntry) {
	if e.own == nil {
		return
	}

	u := s.usage[e.key.Principal]
	u.own.Remove(e.own)
	e.own = nil
	u.entries--

	if u.entries == 0 {
		delete(s.usage, e.key.Principal)
	}
}

// resize sets the size e's response is accounted at. Caller holds s.mu.
func (s *MemoryStore) resize(e *memEntry, size int64) {
	delta := size - e.size
	if delta == 0 {
		return
	}

	e.size = size
	s.bytes += delta

	u := s.usage[e.key.Principal]
	if u == nil {
		return
	}

	u.bytes += delta

	// An entry that expires sooner than anything seen at the last scan
	// moves the next useful scan forward.
	if size > 0 && u.scanned {
		if exp := e.resp.ExpiresAt; !exp.IsZero() && (u.nextExpiry.IsZero() || exp.Before(u.nextExpiry)) {
			u.nextExpiry = exp
		}
	}
}

// checkQuota returns a *QuotaError when principal cannot take another key.
// A principal at its quota first has its expired responses dropped, at most
// once per expiry, so a key frees up as soon as one of them lapses. Caller
// holds s.mu.
func (s *MemoryStore) checkQuota(principal string, now time.Time) error {
	u := s.usage[principal]
	if u == nil || !s.overQuota(u) {
		return nil
	}

	if !u.scanned || (!u.nextExpiry.IsZero() && !now.Before(u.nextExpiry)) {
		s.dropExpired(u, now)

		// dropExpired may have removed the principal's last entry.
		if u = s.usage[principal]; u == nil || !s.overQuota(u) {
			return nil
		}
	}

	qe := &QuotaError{Principal: principal}
	if !u.nextExpiry.IsZero() {
		qe.RetryAfter = u.nextExpiry.Sub(now)
	}

	return qe
}

func (s *MemoryStore) overQuota(u *principalUsage) bool {
	return (s.principalEntries > 0 && u.entries >= s.principalEntries) ||
		(s.principalBytes > 0 && u.bytes >= s.principalBytes)
}

// dropExpired drops u's expired responses and records when the next one
// expires. Caller holds s.mu.
func (s *MemoryStore) dropExpired(u *principalUsage, now time.Time) {
	var next time.Time

	for el := u.own.Front(); el != nil; {
		e := el.Value.(*memEntry)
		el = el.Next()

		if e.resp == nil || e.resp.ExpiresAt.IsZero() {
			continue
		}

		if !now.Before(e.resp.ExpiresAt) {
			s.drop(e)

			continue
		}

		if next.IsZero() || e.resp.ExpiresAt.Before(next) {
			next = e.resp.ExpiresAt
		}
	}

	u.scanned = true
	u.nextExpiry = next
}

// responseSize approximates the memory a stored response takes.
func responseSize(key Key, r *Response) int64 {
	n := entryOverhead + len(key.Principal) + len(key.Scope) + len(key.Value) + len(r.Fingerprint) + len(r.Body)

	for k, vs := range r.Header {
		n += len(k)
		for _, v := range vs {
			n += len(v)
		}
	}

	return int64(n)
}

func copyResponse(r Response) Response {
	r.Header = r.Header.Clone()
	r.Body = bytes.Clone(r.Body)

	return r
}

var _ Store = (*MemoryStore)(nil)

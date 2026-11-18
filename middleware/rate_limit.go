package middleware

import (
	"hash/maphash"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	forge "github.com/xraph/forge"
)

// defaultMaxBuckets caps how many distinct keys a RateLimiter tracks. Without a
// cap the bucket map is an unbounded, attacker-driven allocation: every new key
// adds an entry and cleanup only runs periodically.
const defaultMaxBuckets = 100_000

// maxRateLimiterShards is how many independently locked shards the buckets are
// spread over. One mutex for every key serialised all requests through the
// limiter, so it got slower the more cores the server had.
const maxRateLimiterShards = 64

// RateLimiter implements token bucket algorithm for rate limiting.
//
// Buckets are spread over shards by a seeded hash of the key, each with its own
// lock, so requests for different clients rarely contend. When a shard is full
// its least recently used bucket is evicted to make room. Evicting the stalest
// client costs that client its history (it starts again with a full burst);
// refusing new keys instead, as this limiter used to, let anyone who could
// create enough keys lock every new client out.
type RateLimiter struct {
	table atomic.Pointer[rlTable]
	seed  maphash.Seed

	rate     int           // tokens per second
	capacity int           // max tokens
	cleanup  time.Duration // cleanup interval

	resizeMu   sync.Mutex // serialises SetMaxBuckets
	maxBuckets int

	stopOnce sync.Once
	stopCh   chan struct{}
}

// rlTable is one generation of shards. SetMaxBuckets swaps in a new table
// rather than resizing in place, so Allow never needs a lock beyond its shard.
type rlTable struct {
	shards []rlShard
}

// rlShard holds a slice of the buckets, ordered by last use in an intrusive
// doubly linked list (head is the most recent) so eviction and cleanup can go
// straight to the stalest entries.
type rlShard struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	head    *bucket
	tail    *bucket
	limit   int
	retired bool // set when SetMaxBuckets replaced this shard's table

	// Keep neighbouring shards' locks off the same cache line.
	_ [64]byte
}

type bucket struct {
	key string
	// tokens is fractional so that time between requests is never lost. With
	// whole tokens a client calling more often than once per token interval
	// always rounded its refill down to zero, so it was never refilled at all.
	tokens    float64
	lastCheck time.Time

	prev, next *bucket
}

// NewRateLimiter creates a new rate limiter
// rate: maximum requests per second
// burst: maximum burst size (capacity)
//
// Call Stop when the limiter is no longer needed to release its cleanup
// goroutine.
func NewRateLimiter(rate, burst int) *RateLimiter {
	rl := &RateLimiter{
		seed:       maphash.MakeSeed(),
		rate:       rate,
		capacity:   burst,
		cleanup:    5 * time.Minute,
		maxBuckets: defaultMaxBuckets,
		stopCh:     make(chan struct{}),
	}

	rl.table.Store(newRLTable(defaultMaxBuckets))

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// newRLTable splits maxBuckets across the shards so the per-shard limits add up
// to exactly maxBuckets. Small limits get fewer shards, never a shard that can
// hold nothing.
func newRLTable(maxBuckets int) *rlTable {
	n := min(maxBuckets, maxRateLimiterShards)
	t := &rlTable{shards: make([]rlShard, n)}

	for i := range t.shards {
		limit := maxBuckets / n
		if i < maxBuckets%n {
			limit++
		}

		t.shards[i].limit = limit
		t.shards[i].buckets = make(map[string]*bucket)
	}

	return t
}

func (t *rlTable) shardFor(seed maphash.Seed, key string) *rlShard {
	return &t.shards[maphash.String(seed, key)%uint64(len(t.shards))]
}

// SetMaxBuckets overrides how many distinct keys are tracked. Once the limit is
// reached, the least recently used keys are forgotten to make room for new ones.
// Values below 1 are ignored.
//
// The limit is divided evenly across the limiter's shards, so eviction can start
// in a busy shard slightly before the limiter as a whole is full.
func (rl *RateLimiter) SetMaxBuckets(maxBuckets int) {
	if maxBuckets < 1 {
		return
	}

	rl.resizeMu.Lock()
	defer rl.resizeMu.Unlock()

	if maxBuckets == rl.maxBuckets {
		return
	}

	old := rl.table.Load()
	next := newRLTable(maxBuckets)

	// Hold every old shard while the buckets move, so no Allow can update a
	// bucket that has already been copied. Retired shards send Allow back to
	// load the new table.
	for i := range old.shards {
		old.shards[i].mu.Lock()
	}

	// Oldest first, so that if the new table is smaller the stalest buckets
	// are the ones evicted.
	for i := range old.shards {
		s := &old.shards[i]
		for b := s.tail; b != nil; {
			prev := b.prev
			b.prev, b.next = nil, nil
			next.shardFor(rl.seed, b.key).insert(b)
			b = prev
		}

		s.buckets, s.head, s.tail = nil, nil, nil
		s.retired = true
	}

	rl.maxBuckets = maxBuckets
	rl.table.Store(next)

	for i := range old.shards {
		old.shards[i].mu.Unlock()
	}
}

// Stop terminates the cleanup goroutine. Safe to call more than once.
func (rl *RateLimiter) Stop() {
	rl.stopOnce.Do(func() { close(rl.stopCh) })
}

// Allow checks if a request from the given key should be allowed.
func (rl *RateLimiter) Allow(key string) bool {
	return rl.allowAt(key, time.Now())
}

func (rl *RateLimiter) allowAt(key string, now time.Time) bool {
	for {
		s := rl.table.Load().shardFor(rl.seed, key)

		s.mu.Lock()

		if s.retired {
			// SetMaxBuckets swapped tables while we waited; retry on the new one.
			s.mu.Unlock()

			continue
		}

		allowed := rl.allowLocked(s, key, now)
		s.mu.Unlock()

		return allowed
	}
}

func (rl *RateLimiter) allowLocked(s *rlShard, key string, now time.Time) bool {
	b, exists := s.buckets[key]
	if !exists {
		b = &bucket{
			key:       key,
			tokens:    float64(rl.capacity - 1), // -1 for this request
			lastCheck: now,
		}
		s.insert(b)

		return true
	}

	s.moveToFront(b)

	// Refill for exactly the time that has passed. Carrying the fraction in
	// tokens is what lets a client that calls faster than the refill interval
	// still earn its tokens.
	if elapsed := now.Sub(b.lastCheck); elapsed > 0 {
		b.tokens = min(b.tokens+elapsed.Seconds()*float64(rl.rate), float64(rl.capacity))
		b.lastCheck = now
	}

	if b.tokens >= 1 {
		b.tokens--

		return true
	}

	return false
}

// insert adds b as the most recently used bucket, evicting the least recently
// used one first if the shard is full. Caller must hold s.mu, or own s outright.
func (s *rlShard) insert(b *bucket) {
	if len(s.buckets) >= s.limit && s.tail != nil {
		s.remove(s.tail)
	}

	s.buckets[b.key] = b
	s.pushFront(b)
}

func (s *rlShard) pushFront(b *bucket) {
	b.prev = nil
	b.next = s.head

	if s.head != nil {
		s.head.prev = b
	}

	s.head = b

	if s.tail == nil {
		s.tail = b
	}
}

func (s *rlShard) unlink(b *bucket) {
	if b.prev != nil {
		b.prev.next = b.next
	} else {
		s.head = b.next
	}

	if b.next != nil {
		b.next.prev = b.prev
	} else {
		s.tail = b.prev
	}

	b.prev, b.next = nil, nil
}

func (s *rlShard) moveToFront(b *bucket) {
	if s.head == b {
		return
	}

	s.unlink(b)
	s.pushFront(b)
}

func (s *rlShard) remove(b *bucket) {
	s.unlink(b)
	delete(s.buckets, b.key)
}

// bucketCount reports how many keys are tracked across all shards.
func (rl *RateLimiter) bucketCount() int {
	t := rl.table.Load()
	n := 0

	for i := range t.shards {
		s := &t.shards[i]
		s.mu.Lock()
		n += len(s.buckets)
		s.mu.Unlock()
	}

	return n
}

// cleanupLoop periodically removes old buckets.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.removeStale(time.Now())
		}
	}
}

// removeStale drops buckets idle for longer than the cleanup interval. The
// lists are ordered by last use, so each shard is walked from its tail only
// as far as the first bucket still in use.
func (rl *RateLimiter) removeStale(now time.Time) {
	t := rl.table.Load()

	for i := range t.shards {
		s := &t.shards[i]
		s.mu.Lock()

		for s.tail != nil && now.Sub(s.tail.lastCheck) > rl.cleanup {
			s.remove(s.tail)
		}

		s.mu.Unlock()
	}
}

// ClientIP returns the client's IP address from RemoteAddr, discarding the port.
//
// This is the default rate-limit key. Using RemoteAddr verbatim does not work:
// Go's HTTP server sets it to "IP:ephemeral-port" and the port changes per
// connection, so every connection would get a fresh bucket — no effective limit
// at all, plus unbounded map growth.
//
// This deliberately ignores X-Forwarded-For. Behind a proxy every client
// collapses into the proxy's IP, so supply a RateLimitKeyFunc that reads the
// forwarded header only for hops you actually trust — reading it
// unconditionally is a header-spoofing bypass.
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// RemoteAddr carried no port (some test servers, unix sockets).
		return r.RemoteAddr
	}

	return host
}

// ClientNetworkKey is the default rate-limit key: the client IP from RemoteAddr,
// with IPv6 addresses reduced to their /64 network.
//
// A single IPv6 subscriber is normally handed a whole /64, which is 2^64
// addresses. Keyed per address, one client could step through them to get a
// fresh bucket per request and churn the limiter's table. IPv4 addresses, and
// IPv4 addresses carried in IPv6 form, are returned unchanged. Like ClientIP
// this ignores X-Forwarded-For.
func ClientNetworkKey(r *http.Request) string {
	host := ClientIP(r)

	addr, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}

	if addr.Is4() {
		return host
	}

	if addr.Is4In6() {
		return addr.Unmap().String()
	}

	prefix, err := addr.WithZone("").Prefix(64)
	if err != nil {
		return host
	}

	return prefix.String()
}

// RateLimitKeyFunc derives the bucket key for a request. Return "" to skip rate
// limiting for that request.
type RateLimitKeyFunc func(*http.Request) string

// RateLimit middleware enforces rate limiting per client, keyed on the client
// IP, or on the /64 network for IPv6 clients (see ClientNetworkKey).
func RateLimit(limiter *RateLimiter, logger forge.Logger) forge.Middleware {
	return RateLimitWithKey(limiter, ClientNetworkKey, logger)
}

// RateLimitWithKey enforces rate limiting using a caller-supplied key function.
// Use this to key on an authenticated user ID, an API key, or a trusted
// forwarded-for hop instead of the client IP.
func RateLimitWithKey(limiter *RateLimiter, keyFunc RateLimitKeyFunc, logger forge.Logger) forge.Middleware {
	if keyFunc == nil {
		keyFunc = ClientNetworkKey
	}

	return func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			key := keyFunc(ctx.Request())
			if key == "" {
				return next(ctx)
			}

			if !limiter.Allow(key) {
				if logger != nil {
					logger.Warn("rate limit exceeded")
				}

				ctx.Response().Header().Set("X-Ratelimit-Limit", "exceeded")

				return ctx.String(http.StatusTooManyRequests, "Rate Limit Exceeded")
			}

			return next(ctx)
		}
	}
}

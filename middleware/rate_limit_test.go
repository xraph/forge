package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
)

func TestNewRateLimiter(t *testing.T) {
	limiter := NewRateLimiter(10, 20)

	assert.NotNil(t, limiter)
	assert.Equal(t, 10, limiter.rate)
	assert.Equal(t, 20, limiter.capacity)
}

func TestRateLimiter_Allow_FirstRequest(t *testing.T) {
	limiter := NewRateLimiter(10, 5)

	allowed := limiter.Allow("key1")
	assert.True(t, allowed)
}

func TestRateLimiter_Allow_BurstExceeded(t *testing.T) {
	limiter := NewRateLimiter(1, 3)

	// Burst of 3 requests should be allowed
	assert.True(t, limiter.Allow("key1"))
	assert.True(t, limiter.Allow("key1"))
	assert.True(t, limiter.Allow("key1"))

	// 4th request should be blocked
	assert.False(t, limiter.Allow("key1"))
}

func TestRateLimiter_Allow_TokenRefill(t *testing.T) {
	limiter := NewRateLimiter(10, 5)

	// Exhaust burst
	for range 5 {
		limiter.Allow("key1")
	}

	// Next request should be blocked
	assert.False(t, limiter.Allow("key1"))

	// Wait for token refill (100ms = 1 token at 10/sec rate)
	time.Sleep(150 * time.Millisecond)

	// Should be allowed now
	assert.True(t, limiter.Allow("key1"))
}

func TestRateLimiter_Allow_DifferentKeys(t *testing.T) {
	limiter := NewRateLimiter(1, 1)

	// Each key has its own bucket
	assert.True(t, limiter.Allow("key1"))
	assert.True(t, limiter.Allow("key2"))

	// Both exhausted
	assert.False(t, limiter.Allow("key1"))
	assert.False(t, limiter.Allow("key2"))
}

func TestRateLimit_AllowedRequest(t *testing.T) {
	logger := &mockLogger{}
	limiter := NewRateLimiter(10, 5)

	handler := RateLimit(limiter, logger)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	ctx := forge_http.NewContext(rec, req, nil)

	_ = handler(ctx)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ok", rec.Body.String())
}

func TestRateLimit_ExceededRequest(t *testing.T) {
	logger := &mockLogger{}
	limiter := NewRateLimiter(1, 1)

	handler := RateLimit(limiter, logger)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)

	// First request allowed
	rec1 := httptest.NewRecorder()
	ctx1 := forge_http.NewContext(rec1, req, nil)
	_ = handler(ctx1)

	assert.Equal(t, http.StatusOK, rec1.Code)

	// Second request blocked
	rec2 := httptest.NewRecorder()
	ctx2 := forge_http.NewContext(rec2, req, nil)
	_ = handler(ctx2)

	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
	assert.Contains(t, rec2.Body.String(), "Rate Limit Exceeded")
	assert.Len(t, logger.messages, 1)
}

func TestRateLimit_NilLogger(t *testing.T) {
	limiter := NewRateLimiter(1, 1)

	handler := RateLimit(limiter, nil)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/test", nil)
	rec1 := httptest.NewRecorder()
	ctx1 := forge_http.NewContext(rec1, req, nil)
	_ = handler(ctx1)

	rec2 := httptest.NewRecorder()
	ctx2 := forge_http.NewContext(rec2, req, nil)
	_ = handler(ctx2)

	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

// A client calling faster than one token interval used to be refilled zero
// whole tokens on every call, and the clock was reset each time, so after the
// burst it was never let through again.
func TestRateLimiter_RefillsClientsCallingFasterThanTheRate(t *testing.T) {
	limiter := NewRateLimiter(10, 2) // 10/s, burst 2
	defer limiter.Stop()

	start := time.Now()
	interval := time.Second / 16 // 16 rps, faster than the 100ms token interval
	allowed := 0

	for i := range 32 { // two seconds
		if limiter.allowAt("k", start.Add(time.Duration(i)*interval)) {
			allowed++
		}
	}

	// Burst 2 plus about 10/s for the 1.94s the calls span.
	assert.GreaterOrEqual(t, allowed, 20)
	assert.LessOrEqual(t, allowed, 22)
}

// A full table used to refuse every new key, so whoever filled it (easy from a
// single IPv6 /64) locked all new clients out.
func TestRateLimiter_AdmitsNewKeysWhenFull(t *testing.T) {
	limiter := NewRateLimiter(1, 1)
	defer limiter.Stop()

	limiter.SetMaxBuckets(100)

	for i := range 1000 {
		limiter.Allow(fmt.Sprintf("attacker-%d", i))
	}

	assert.True(t, limiter.Allow("new-client"), "new client refused while the table is full")
	assert.LessOrEqual(t, limiter.bucketCount(), 100)
}

func TestRateLimiterShard_EvictsLeastRecentlyUsed(t *testing.T) {
	s := &rlShard{limit: 3, buckets: make(map[string]*bucket)}

	for _, k := range []string{"a", "b", "c"} {
		s.insert(&bucket{key: k})
	}

	s.moveToFront(s.buckets["a"]) // b is now the least recently used
	s.insert(&bucket{key: "d"})

	assert.Len(t, s.buckets, 3)
	assert.NotContains(t, s.buckets, "b")

	var order []string
	for b := s.head; b != nil; b = b.next {
		order = append(order, b.key)
	}

	assert.Equal(t, []string{"d", "a", "c"}, order)
	assert.Equal(t, "c", s.tail.key)
}

func TestRateLimiter_SetMaxBucketsKeepsState(t *testing.T) {
	limiter := NewRateLimiter(1, 1)
	defer limiter.Stop()

	now := time.Now()
	assert.True(t, limiter.allowAt("k", now))
	assert.False(t, limiter.allowAt("k", now))

	limiter.SetMaxBuckets(50)

	// Resizing must not hand an exhausted client a fresh burst.
	assert.False(t, limiter.allowAt("k", now))
	assert.Equal(t, 1, limiter.bucketCount())
}

func TestRateLimiter_ConcurrentAllowAndResize(t *testing.T) {
	limiter := NewRateLimiter(1000, 1000)
	defer limiter.Stop()

	var wg sync.WaitGroup

	for g := range 8 {
		wg.Go(func() {
			for i := range 2000 {
				limiter.Allow(fmt.Sprintf("%d-%d", g, i%300))
			}
		})
	}

	for _, n := range []int{10, 5000, 3, 200, 100_000} {
		limiter.SetMaxBuckets(n)
	}

	wg.Wait()

	assert.LessOrEqual(t, limiter.bucketCount(), 100_000)
}

func TestRateLimiter_RemoveStale(t *testing.T) {
	limiter := NewRateLimiter(1, 1)
	defer limiter.Stop()

	now := time.Now()
	limiter.allowAt("old", now.Add(-2*limiter.cleanup))
	limiter.allowAt("fresh", now)

	limiter.removeStale(now)

	assert.Equal(t, 1, limiter.bucketCount())
	assert.True(t, limiter.allowAt("old", now), "a removed key starts over")
}

func TestClientNetworkKey(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7:4000":           "203.0.113.7",
		"[2001:db8:1:2:aaaa::1]:443": "2001:db8:1:2::/64",
		"[2001:db8:1:2:ffff::9]:443": "2001:db8:1:2::/64",
		"[2001:db8:1:3::1]:443":      "2001:db8:1:3::/64",
		"[::ffff:203.0.113.7]:80":    "203.0.113.7",
		"[fe80::1%eth0]:80":          "fe80::/64",
		"not-an-ip":                  "not-an-ip",
	}

	for remote, want := range cases {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		r.RemoteAddr = remote

		assert.Equal(t, want, ClientNetworkKey(r), remote)
	}
}

// Every address in an IPv6 /64 belongs to one subscriber, so they must share a
// bucket under the default key.
func TestRateLimit_IPv6ClientsShareTheirSlash64(t *testing.T) {
	limiter := NewRateLimiter(1, 2)
	defer limiter.Stop()

	handler := RateLimit(limiter, nil)(func(ctx forge.Context) error {
		return ctx.String(http.StatusOK, "ok")
	})

	codes := make([]int, 0, 4)

	for i := range 4 {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.RemoteAddr = fmt.Sprintf("[2001:db8:1:2::%x]:443", i+1)

		rec := httptest.NewRecorder()
		_ = handler(forge_http.NewContext(rec, req, nil))
		codes = append(codes, rec.Code)
	}

	assert.Equal(t, []int{200, 200, 429, 429}, codes)
}

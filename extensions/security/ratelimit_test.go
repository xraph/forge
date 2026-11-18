package security

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

func rateLimitStatus(t *testing.T, h func(*http.Request) int, remote string, headers map[string]string) int {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/login", nil)
	req.RemoteAddr = remote

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	return h(req)
}

func newRateLimitHandler(t *testing.T, cfg RateLimitConfig) (*MemoryRateLimiter, func(*http.Request) int) {
	t.Helper()

	rl := NewMemoryRateLimiter(cfg, logger.NewNoopLogger(), nil)
	t.Cleanup(rl.Stop)

	mw := RateLimitMiddleware(rl)(okHandler)

	return rl, func(req *http.Request) int {
		w := httptest.NewRecorder()
		_ = mw(forge_http.NewContext(w, req, nil))

		return w.Code
	}
}

// A client used to get a fresh bucket per request by varying X-Forwarded-For
// (1000 of 1000 requests passed a limit of 5), and could lock a victim out by
// sending the victim's address in X-Real-IP.
func TestRateLimit_IgnoresForwardingHeadersFromUntrustedPeers(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	cfg.RequestsPerWindow = 5
	rl, do := newRateLimitHandler(t, cfg)

	allowed := 0

	for i := range 1000 {
		code := rateLimitStatus(t, do, "203.0.113.9:1234", map[string]string{
			"X-Forwarded-For": fmt.Sprintf("10.0.%d.%d", i/256, i%256),
		})
		if code == http.StatusOK {
			allowed++
		}
	}

	if allowed != 5 {
		t.Fatalf("allowed %d of 1000 requests from one peer, want 5", allowed)
	}

	buckets := 0

	rl.buckets.Range(func(_, _ any) bool {
		buckets++

		return true
	})

	if buckets != 1 {
		t.Fatalf("created %d buckets for one peer, want 1", buckets)
	}

	// The attacker names the victim in X-Real-IP; it must not touch the
	// victim's bucket.
	for range 5 {
		rateLimitStatus(t, do, "203.0.113.10:999", map[string]string{"X-Real-IP": "198.51.100.7"})
	}

	if code := rateLimitStatus(t, do, "198.51.100.7:5555", nil); code != http.StatusOK {
		t.Fatalf("victim's first request: status %d, want 200", code)
	}
}

func TestRateLimit_KeyWithTrustedProxies(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	cfg.TrustedProxies = []string{"10.0.0.0/8", "192.0.2.1", "not-an-ip"}
	rl := NewMemoryRateLimiter(cfg, logger.NewNoopLogger(), nil)
	t.Cleanup(rl.Stop)

	cases := []struct {
		name   string
		remote string
		xff    []string
		realIP string
		want   string
	}{
		{"direct client, headers ignored", "203.0.113.5:1", []string{"1.1.1.1"}, "2.2.2.2", "203.0.113.5"},
		{"one proxy", "10.0.0.1:1", []string{"198.51.100.7"}, "", "198.51.100.7"},
		{"client-written entries on the left are skipped", "10.0.0.1:1", []string{"6.6.6.6, 198.51.100.7, 10.1.2.3"}, "", "198.51.100.7"},
		{"multiple header lines", "10.0.0.1:1", []string{"6.6.6.6", "198.51.100.7"}, "", "198.51.100.7"},
		{"single-address proxy entry", "192.0.2.1:1", []string{"198.51.100.8"}, "", "198.51.100.8"},
		{"X-Real-IP from a trusted proxy", "10.0.0.1:1", nil, "198.51.100.9", "198.51.100.9"},
		{"every hop trusted falls back to the peer", "10.0.0.1:1", []string{"10.9.9.9"}, "", "10.0.0.1"},
		{"malformed hop limits on the proxy", "10.0.0.1:1", []string{"198.51.100.7, garbage"}, "", "10.0.0.1"},
		{"IPv4-mapped IPv6 is folded", "[::ffff:203.0.113.5]:1", nil, "", "203.0.113.5"},
		{"IPv6 peer", "[2001:db8::1]:443", nil, "", "2001:db8::1"},
	}

	for _, c := range cases {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		req.RemoteAddr = c.remote

		for _, v := range c.xff {
			req.Header.Add("X-Forwarded-For", v)
		}

		if c.realIP != "" {
			req.Header.Set("X-Real-IP", c.realIP)
		}

		if got := rl.extractRateLimitKey(req); got != c.want {
			t.Errorf("%s: key %q, want %q", c.name, got, c.want)
		}
	}
}

func TestRateLimit_AllowDoesNotAllocateBucketForKnownKey(t *testing.T) {
	cfg := DefaultRateLimitConfig()
	cfg.RequestsPerWindow = 1 << 30
	rl := NewMemoryRateLimiter(cfg, logger.NewNoopLogger(), nil)
	t.Cleanup(rl.Stop)

	rl.Allow("1.2.3.4")

	// One allocation is the returned *RateLimitInfo; a new bucket would add more.
	if allocs := testing.AllocsPerRun(100, func() { rl.Allow("1.2.3.4") }); allocs > 1 {
		t.Fatalf("Allow allocated %.0f times per call for a known key, want at most 1", allocs)
	}
}

func TestRateLimit_StopEndsCleanupGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	limiters := make([]*MemoryRateLimiter, 50)
	for i := range limiters {
		limiters[i] = NewMemoryRateLimiter(DefaultRateLimitConfig(), logger.NewNoopLogger(), nil)
	}

	for _, rl := range limiters {
		rl.Stop()
		rl.Stop() // idempotent
	}

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if n := runtime.NumGoroutine(); n > before+5 {
		t.Fatalf("%d goroutines still running after Stop (started with %d)", n, before)
	}
}

func BenchmarkRateLimitAllowHotKey(b *testing.B) {
	cfg := DefaultRateLimitConfig()
	cfg.RequestsPerWindow = 1 << 30
	rl := NewMemoryRateLimiter(cfg, logger.NewNoopLogger(), nil)
	b.Cleanup(rl.Stop)

	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rl.Allow("1.2.3.4")
		}
	})
}

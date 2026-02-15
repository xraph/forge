package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 100,
		Burst:          10,
	}

	rl := NewRateLimiter(config)
	if rl == nil {
		t.Fatal("expected rate limiter, got nil")
	}
}

func TestRateLimiter_AllowGlobal(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 10,
		Burst:          5,
	}

	rl := NewRateLimiter(config)

	req := httptest.NewRequest("GET", "/test", nil)

	// Initial burst should be allowed
	for i := 0; i < 5; i++ {
		if !rl.Allow(req) {
			t.Errorf("request %d should be allowed (within burst)", i)
		}
	}

	// Next request should be rate limited
	if rl.Allow(req) {
		t.Error("request beyond burst should be rate limited")
	}
}

func TestRateLimiter_Disabled(t *testing.T) {
	config := RateLimitConfig{
		Enabled: false,
	}

	rl := NewRateLimiter(config)

	req := httptest.NewRequest("GET", "/test", nil)

	// All requests should be allowed when disabled
	for i := 0; i < 100; i++ {
		if !rl.Allow(req) {
			t.Errorf("request %d should be allowed (rate limiter disabled)", i)
		}
	}
}

func TestRateLimiter_RefillOverTime(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 10, // 10 rps = 1 token per 100ms
		Burst:          1,
	}

	rl := NewRateLimiter(config)

	req := httptest.NewRequest("GET", "/test", nil)

	// Use up burst
	if !rl.Allow(req) {
		t.Fatal("first request should be allowed")
	}

	// Should be rate limited immediately
	if rl.Allow(req) {
		t.Error("second request should be rate limited")
	}

	// Wait for token refill
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow(req) {
		t.Error("request should be allowed after refill")
	}
}

func TestRateLimiter_PerClient(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		PerClient:      true,
		KeyHeader:      "X-Client-ID",
		RequestsPerSec: 10,
		Burst:          2,
	}

	rl := NewRateLimiter(config)

	// Request from client-1
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-Client-ID", "client-1")

	// Request from client-2
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-Client-ID", "client-2")

	// Client-1 burst
	for i := 0; i < 2; i++ {
		if !rl.Allow(req1) {
			t.Errorf("client-1 request %d should be allowed", i)
		}
	}

	// Client-1 rate limited
	if rl.Allow(req1) {
		t.Error("client-1 should be rate limited")
	}

	// Client-2 should still be allowed (separate bucket)
	for i := 0; i < 2; i++ {
		if !rl.Allow(req2) {
			t.Errorf("client-2 request %d should be allowed", i)
		}
	}
}

func TestRateLimiter_AllowWithConfig(t *testing.T) {
	globalConfig := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 100,
		Burst:          50,
	}

	rl := NewRateLimiter(globalConfig)

	routeConfig := &RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 5,
		Burst:          1,
	}

	req := httptest.NewRequest("GET", "/api/test", nil)

	// First request allowed (burst)
	if !rl.AllowWithConfig(req, routeConfig) {
		t.Error("first request should be allowed")
	}

	// Second request should be limited (burst=1, exceeded)
	if rl.AllowWithConfig(req, routeConfig) {
		t.Error("second request should be rate limited by route config")
	}
}

func TestRateLimiter_AllowWithConfig_NilFallsToGlobal(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 10,
		Burst:          2,
	}

	rl := NewRateLimiter(config)

	req := httptest.NewRequest("GET", "/test", nil)

	// nil config should fall back to global
	if !rl.AllowWithConfig(req, nil) {
		t.Error("first request should be allowed via global config")
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		RequestsPerSec: 1000,
		Burst:          100,
	}

	rl := NewRateLimiter(config)

	done := make(chan bool)

	// Concurrent requests
	for i := 0; i < 50; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/test", nil)
			for j := 0; j < 100; j++ {
				_ = rl.Allow(req)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 50; i++ {
		<-done
	}

	// Should not panic or race
}

func TestRateLimiter_ExtractKey_Header(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		PerClient:      true,
		KeyHeader:      "X-API-Key",
		RequestsPerSec: 100,
		Burst:          5,
	}

	rl := NewRateLimiter(config)

	// Two requests with different API keys should have separate buckets
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-API-Key", "key-1")

	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-API-Key", "key-2")

	// Exhaust key-1 bucket
	for i := 0; i < 5; i++ {
		rl.Allow(req1)
	}

	// key-2 should still be allowed
	if !rl.Allow(req2) {
		t.Error("key-2 should be allowed (separate bucket)")
	}
}

func TestRateLimiter_ExtractKey_FallbackToRemoteAddr(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		PerClient:      true,
		RequestsPerSec: 100,
		Burst:          2,
	}

	rl := NewRateLimiter(config)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	// Should use remote addr when no key header configured
	if !rl.Allow(req) {
		t.Error("first request should be allowed")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		PerClient:      true,
		KeyHeader:      "X-Client-ID",
		RequestsPerSec: 100,
		Burst:          5,
	}

	rl := NewRateLimiter(config)

	// Create some buckets
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Client-ID", string(rune('A'+i)))
		rl.Allow(req)
	}

	// Cleanup with very short max age (will remove all)
	time.Sleep(10 * time.Millisecond)
	rl.Cleanup(1 * time.Millisecond)

	// Verify buckets were cleaned
	rl.mu.RLock()
	count := len(rl.buckets)
	rl.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected 0 buckets after cleanup, got %d", count)
	}
}

func TestRateLimiter_ClientIDExtraction(t *testing.T) {
	tests := []struct {
		name       string
		config     RateLimitConfig
		reqBuilder func() *http.Request
	}{
		{
			name: "from header",
			config: RateLimitConfig{
				Enabled:        true,
				PerClient:      true,
				KeyHeader:      "X-Client-ID",
				RequestsPerSec: 100,
				Burst:          5,
			},
			reqBuilder: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Client-ID", "client-123")

				return req
			},
		},
		{
			name: "from remote addr",
			config: RateLimitConfig{
				Enabled:        true,
				PerClient:      true,
				RequestsPerSec: 100,
				Burst:          5,
			},
			reqBuilder: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.RemoteAddr = "192.168.1.100:12345"

				return req
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rl := NewRateLimiter(tt.config)
			req := tt.reqBuilder()

			// Just verify it doesn't panic
			_ = rl.Allow(req)
		})
	}
}

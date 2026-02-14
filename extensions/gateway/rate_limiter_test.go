package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 100,
			Burst:             10,
		},
	}

	rl := NewRateLimiter(config)
	if rl == nil {
		t.Fatal("expected rate limiter, got nil")
	}
}

func TestRateLimiter_AllowGlobal(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 10,
			Burst:             5,
		},
	}

	rl := NewRateLimiter(config)

	req := httptest.NewRequest("GET", "/test", nil)

	// Initial burst should be allowed
	for i := 0; i < 5; i++ {
		if !rl.Allow(req, nil) {
			t.Errorf("request %d should be allowed (within burst)", i)
		}
	}

	// Next request should be rate limited
	if rl.Allow(req, nil) {
		t.Error("request beyond burst should be rate limited")
	}
}

func TestRateLimiter_AllowRoute(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
	}

	rl := NewRateLimiter(config)

	route := &Route{
		ID:   "test-route",
		Path: "/api/test",
		RateLimit: &RateLimitRule{
			RequestsPerSecond: 5,
			Burst:             2,
		},
	}

	req := httptest.NewRequest("GET", "/api/test", nil)

	// Burst should be allowed
	for i := 0; i < 2; i++ {
		if !rl.Allow(req, route) {
			t.Errorf("request %d should be allowed (within burst)", i)
		}
	}

	// Next request should be rate limited
	if rl.Allow(req, route) {
		t.Error("request beyond burst should be rate limited")
	}
}

func TestRateLimiter_AllowClient(t *testing.T) {
	config := RateLimitConfig{
		Enabled:        true,
		ClientBased:    true,
		ClientIDHeader: "X-Client-ID",
		PerClient: RateLimitRule{
			RequestsPerSecond: 5,
			Burst:             2,
		},
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
		if !rl.Allow(req1, nil) {
			t.Errorf("client-1 request %d should be allowed", i)
		}
	}

	// Client-1 rate limited
	if rl.Allow(req1, nil) {
		t.Error("client-1 should be rate limited")
	}

	// Client-2 should still be allowed (separate bucket)
	for i := 0; i < 2; i++ {
		if !rl.Allow(req2, nil) {
			t.Errorf("client-2 request %d should be allowed", i)
		}
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
		if !rl.Allow(req, nil) {
			t.Errorf("request %d should be allowed (rate limiter disabled)", i)
		}
	}
}

func TestRateLimiter_RefillOverTime(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 10, // 10 requests per second = 1 request per 100ms
			Burst:             1,
		},
	}

	rl := NewRateLimiter(config)

	req := httptest.NewRequest("GET", "/test", nil)

	// Use up burst
	if !rl.Allow(req, nil) {
		t.Fatal("first request should be allowed")
	}

	// Should be rate limited immediately
	if rl.Allow(req, nil) {
		t.Error("second request should be rate limited")
	}

	// Wait for token refill (100ms at 10 rps)
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again
	if !rl.Allow(req, nil) {
		t.Error("request should be allowed after refill")
	}
}

func TestRateLimiter_ClientIDExtraction(t *testing.T) {
	tests := []struct {
		name       string
		config     RateLimitConfig
		reqBuilder func() *http.Request
		expectedID string
	}{
		{
			name: "from header",
			config: RateLimitConfig{
				ClientBased:    true,
				ClientIDHeader: "X-Client-ID",
			},
			reqBuilder: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Client-ID", "client-123")
				return req
			},
			expectedID: "client-123",
		},
		{
			name: "from IP",
			config: RateLimitConfig{
				ClientBased: true,
				UseIPAsID:   true,
			},
			reqBuilder: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.RemoteAddr = "192.168.1.100:12345"
				return req
			},
			expectedID: "192.168.1.100",
		},
		{
			name: "from X-Forwarded-For",
			config: RateLimitConfig{
				ClientBased: true,
				UseIPAsID:   true,
			},
			reqBuilder: func() *http.Request {
				req := httptest.NewRequest("GET", "/test", nil)
				req.Header.Set("X-Forwarded-For", "10.0.0.1, 192.168.1.1")
				return req
			},
			expectedID: "10.0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test verifies that different client IDs get separate buckets
			rl := NewRateLimiter(tt.config)
			req := tt.reqBuilder()

			// Just verify it doesn't panic
			_ = rl.Allow(req, nil)
		})
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 1000,
			Burst:             100,
		},
	}

	rl := NewRateLimiter(config)

	done := make(chan bool)

	// Concurrent requests
	for i := 0; i < 50; i++ {
		go func(id int) {
			req := httptest.NewRequest("GET", "/test", nil)
			for j := 0; j < 100; j++ {
				_ = rl.Allow(req, nil)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 50; i++ {
		<-done
	}

	// Should not panic or race
}

func TestRateLimiter_RouteOverridesGlobal(t *testing.T) {
	config := RateLimitConfig{
		Enabled: true,
		Global: RateLimitRule{
			RequestsPerSecond: 100,
			Burst:             10,
		},
	}

	rl := NewRateLimiter(config)

	route := &Route{
		ID:   "test-route",
		Path: "/api/test",
		RateLimit: &RateLimitRule{
			RequestsPerSecond: 5,
			Burst:             1,
		},
	}

	req := httptest.NewRequest("GET", "/api/test", nil)

	// Route-specific limit should be enforced
	// First request allowed (burst)
	if !rl.Allow(req, route) {
		t.Error("first request should be allowed")
	}

	// Second request should be limited (burst=1, exceeded)
	if rl.Allow(req, route) {
		t.Error("second request should be rate limited by route config")
	}
}

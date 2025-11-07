package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	forge "github.com/xraph/forge"
	"github.com/xraph/forge/internal/di"
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
	for i := 0; i < 5; i++ {
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

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	ctx := di.NewContext(rec, req, nil)

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

	req := httptest.NewRequest("GET", "/test", nil)

	// First request allowed
	rec1 := httptest.NewRecorder()
	ctx1 := di.NewContext(rec1, req, nil)
	_ = handler(ctx1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Second request blocked
	rec2 := httptest.NewRecorder()
	ctx2 := di.NewContext(rec2, req, nil)
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

	req := httptest.NewRequest("GET", "/test", nil)
	rec1 := httptest.NewRecorder()
	ctx1 := di.NewContext(rec1, req, nil)
	_ = handler(ctx1)

	rec2 := httptest.NewRecorder()
	ctx2 := di.NewContext(rec2, req, nil)
	_ = handler(ctx2)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)
}

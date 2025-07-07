package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// RateLimitMiddleware provides rate limiting functionality
type RateLimitMiddleware struct {
	*BaseMiddleware
	config       router.RateLimitConfig
	limiter      RateLimiter
	keyGenerator KeyGenerator
}

// RateLimiter interface for rate limiting implementations
type RateLimiter interface {
	// Check if request is allowed
	Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, *RateLimitResult, error)

	// Get current limit information
	GetLimit(ctx context.Context, key string) (*RateLimitResult, error)

	// Reset limits for a key
	Reset(ctx context.Context, key string) error

	// Cleanup expired entries
	Cleanup(ctx context.Context) error
}

// RateLimitResult contains rate limit information
type RateLimitResult struct {
	Allowed      bool          `json:"allowed"`
	Limit        int64         `json:"limit"`
	Remaining    int64         `json:"remaining"`
	ResetTime    time.Time     `json:"reset_time"`
	ResetAfter   time.Duration `json:"reset_after"`
	RetryAfter   time.Duration `json:"retry_after,omitempty"`
	RequestCount int64         `json:"request_count"`
}

// KeyGenerator generates keys for rate limiting
type KeyGenerator interface {
	GenerateKey(r *http.Request) string
}

// NewRateLimitMiddleware creates a new rate limit middleware
func NewRateLimitMiddleware(config router.RateLimitConfig, limiter RateLimiter) Middleware {
	if limiter == nil {
		limiter = NewMemoryRateLimiter()
	}

	var keyGen KeyGenerator
	switch config.Strategy {
	case router.RateLimitByIP:
		keyGen = &IPKeyGenerator{}
	case router.RateLimitByUser:
		keyGen = &UserKeyGenerator{}
	case router.RateLimitByAPIKey:
		keyGen = &APIKeyKeyGenerator{}
	case router.RateLimitByHeader:
		keyGen = &HeaderKeyGenerator{HeaderName: "X-Client-ID"}
	case router.RateLimitGlobal:
		keyGen = &GlobalKeyGenerator{}
	default:
		keyGen = &IPKeyGenerator{}
	}

	// Use custom key generator if provided
	if config.KeyGenerator != nil {
		keyGen = &CustomKeyGenerator{Generator: config.KeyGenerator}
	}

	return &RateLimitMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"rate_limit",
			PriorityRateLimit,
			"Rate limiting middleware",
		),
		config:       config,
		limiter:      limiter,
		keyGenerator: keyGen,
	}
}

// DefaultRateLimitConfig returns default rate limit configuration
func DefaultRateLimitConfig() router.RateLimitConfig {
	return router.RateLimitConfig{
		RequestsPerSecond: 100,
		BurstSize:         200,
		Strategy:          router.RateLimitByIP,
		SkipPaths:         []string{"/health", "/metrics"},
	}
}

// StrictRateLimitConfig returns strict rate limit configuration
func StrictRateLimitConfig() router.RateLimitConfig {
	return router.RateLimitConfig{
		RequestsPerSecond: 10,
		BurstSize:         20,
		Strategy:          router.RateLimitByIP,
		SkipPaths:         []string{"/health"},
	}
}

// APIRateLimitConfig returns API-optimized rate limit configuration
func APIRateLimitConfig() router.RateLimitConfig {
	return router.RateLimitConfig{
		RequestsPerSecond: 1000,
		BurstSize:         2000,
		Strategy:          router.RateLimitByAPIKey,
		SkipPaths:         []string{"/health", "/metrics"},
	}
}

// Handle implements the Middleware interface
func (rlm *RateLimitMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for certain paths
		if rlm.shouldSkipRateLimit(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Generate key for rate limiting
		key := rlm.keyGenerator.GenerateKey(r)
		if key == "" {
			// If key generation fails, allow request but log warning
			rlm.logger.Warn("Failed to generate rate limit key",
				logger.String("path", r.URL.Path),
				logger.String("method", r.Method),
			)
			next.ServeHTTP(w, r)
			return
		}

		// Check rate limit
		window := time.Second
		if rlm.config.RequestsPerSecond > 0 {
			window = time.Duration(float64(time.Second) / rlm.config.RequestsPerSecond)
		}

		allowed, result, err := rlm.limiter.Allow(r.Context(), key, int64(rlm.config.BurstSize), window)
		if err != nil {
			rlm.logger.Error("Rate limit check failed",
				logger.Error(err),
				logger.String("key", key),
			)
			// On error, allow request but log the error
			next.ServeHTTP(w, r)
			return
		}

		// Set rate limit headers
		rlm.setRateLimitHeaders(w, result)

		// Check if request is allowed
		if !allowed {
			rlm.handleRateLimitExceeded(w, r, result)
			return
		}

		// Request is allowed, continue
		next.ServeHTTP(w, r)
	})
}

// shouldSkipRateLimit checks if rate limiting should be skipped
func (rlm *RateLimitMiddleware) shouldSkipRateLimit(r *http.Request) bool {
	path := r.URL.Path
	for _, skipPath := range rlm.config.SkipPaths {
		if path == skipPath || strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// setRateLimitHeaders sets rate limit headers
func (rlm *RateLimitMiddleware) setRateLimitHeaders(w http.ResponseWriter, result *RateLimitResult) {
	w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
	w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetTime.Unix(), 10))

	if result.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))
	}
}

// handleRateLimitExceeded handles rate limit exceeded responses
func (rlm *RateLimitMiddleware) handleRateLimitExceeded(w http.ResponseWriter, r *http.Request, result *RateLimitResult) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)

	response := map[string]interface{}{
		"error":      "Rate limit exceeded",
		"message":    "Too many requests. Please try again later.",
		"code":       "RATE_LIMIT_EXCEEDED",
		"limit":      result.Limit,
		"remaining":  result.Remaining,
		"reset_time": result.ResetTime.Unix(),
	}

	if result.RetryAfter > 0 {
		response["retry_after"] = int64(result.RetryAfter.Seconds())
	}

	json.NewEncoder(w).Encode(response)
}

// Configure implements the Middleware interface
func (rlm *RateLimitMiddleware) Configure(config map[string]interface{}) error {
	if requestsPerSecond, ok := config["requests_per_second"].(float64); ok {
		rlm.config.RequestsPerSecond = requestsPerSecond
	}
	if burstSize, ok := config["burst_size"].(int); ok {
		rlm.config.BurstSize = burstSize
	}
	if strategy, ok := config["strategy"].(string); ok {
		rlm.config.Strategy = router.RateLimitStrategy(strategy)
	}
	if skipPaths, ok := config["skip_paths"].([]string); ok {
		rlm.config.SkipPaths = skipPaths
	}

	return nil
}

// Health implements the Middleware interface
func (rlm *RateLimitMiddleware) Health(ctx context.Context) error {
	if rlm.limiter == nil {
		return fmt.Errorf("rate limiter not configured")
	}

	// Test the limiter with a dummy key
	_, _, err := rlm.limiter.Allow(ctx, "health-check", 1, time.Second)
	if err != nil {
		return fmt.Errorf("rate limiter health check failed: %w", err)
	}

	return nil
}

// Key generators

// IPKeyGenerator generates keys based on IP address
type IPKeyGenerator struct{}

func (g *IPKeyGenerator) GenerateKey(r *http.Request) string {
	// Try to get real IP from common headers
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if ips := strings.Split(ip, ","); len(ips) > 0 {
			return "ip:" + strings.TrimSpace(ips[0])
		}
	}

	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return "ip:" + ip
	}

	if ip := r.Header.Get("CF-Connecting-IP"); ip != "" {
		return "ip:" + ip
	}

	// Fall back to remote address
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "ip:" + r.RemoteAddr
	}

	return "ip:" + ip
}

// UserKeyGenerator generates keys based on authenticated user
type UserKeyGenerator struct{}

func (g *UserKeyGenerator) GenerateKey(r *http.Request) string {
	userID := router.GetUserID(r.Context())
	if userID == "" {
		// Fall back to IP if no user
		return (&IPKeyGenerator{}).GenerateKey(r)
	}
	return "user:" + userID
}

// APIKeyKeyGenerator generates keys based on API key
type APIKeyKeyGenerator struct{}

func (g *APIKeyKeyGenerator) GenerateKey(r *http.Request) string {
	// Try different common API key headers
	if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
		return "api:" + apiKey
	}

	if apiKey := r.Header.Get("Authorization"); apiKey != "" {
		if strings.HasPrefix(apiKey, "Bearer ") {
			return "api:" + apiKey[7:]
		}
	}

	// Check query parameter
	if apiKey := r.URL.Query().Get("api_key"); apiKey != "" {
		return "api:" + apiKey
	}

	// Fall back to IP
	return (&IPKeyGenerator{}).GenerateKey(r)
}

// HeaderKeyGenerator generates keys based on specific header
type HeaderKeyGenerator struct {
	HeaderName string
}

func (g *HeaderKeyGenerator) GenerateKey(r *http.Request) string {
	if value := r.Header.Get(g.HeaderName); value != "" {
		return "header:" + g.HeaderName + ":" + value
	}

	// Fall back to IP
	return (&IPKeyGenerator{}).GenerateKey(r)
}

// GlobalKeyGenerator generates a global key for all requests
type GlobalKeyGenerator struct{}

func (g *GlobalKeyGenerator) GenerateKey(r *http.Request) string {
	return "global"
}

// CustomKeyGenerator wraps a custom key generation function
type CustomKeyGenerator struct {
	Generator func(*http.Request) string
}

func (g *CustomKeyGenerator) GenerateKey(r *http.Request) string {
	return g.Generator(r)
}

// Memory-based rate limiter implementation

// MemoryRateLimiter provides in-memory rate limiting
type MemoryRateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*TokenBucket
	cleaner *time.Ticker
	done    chan struct{}
}

// TokenBucket represents a token bucket for rate limiting
type TokenBucket struct {
	tokens     int64
	capacity   int64
	refillRate float64
	lastRefill time.Time
	window     time.Duration
	requests   int64
	resetTime  time.Time
}

// NewMemoryRateLimiter creates a new memory-based rate limiter
func NewMemoryRateLimiter() *MemoryRateLimiter {
	rl := &MemoryRateLimiter{
		buckets: make(map[string]*TokenBucket),
		cleaner: time.NewTicker(time.Minute),
		done:    make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request is allowed
func (rl *MemoryRateLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, *RateLimitResult, error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[key]
	if !exists {
		bucket = &TokenBucket{
			tokens:     limit,
			capacity:   limit,
			refillRate: float64(limit) / window.Seconds(),
			lastRefill: time.Now(),
			window:     window,
			requests:   0,
			resetTime:  time.Now().Add(window),
		}
		rl.buckets[key] = bucket
	}

	// Refill tokens
	rl.refillBucket(bucket)

	// Check if request is allowed
	allowed := bucket.tokens > 0
	if allowed {
		bucket.tokens--
		bucket.requests++
	}

	// Calculate remaining and reset time
	remaining := bucket.tokens
	if remaining < 0 {
		remaining = 0
	}

	result := &RateLimitResult{
		Allowed:      allowed,
		Limit:        limit,
		Remaining:    remaining,
		ResetTime:    bucket.resetTime,
		ResetAfter:   time.Until(bucket.resetTime),
		RequestCount: bucket.requests,
	}

	if !allowed {
		result.RetryAfter = time.Until(bucket.resetTime)
	}

	return allowed, result, nil
}

// GetLimit returns current limit information
func (rl *MemoryRateLimiter) GetLimit(ctx context.Context, key string) (*RateLimitResult, error) {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	bucket, exists := rl.buckets[key]
	if !exists {
		return &RateLimitResult{
			Allowed:   true,
			Limit:     0,
			Remaining: 0,
			ResetTime: time.Now(),
		}, nil
	}

	return &RateLimitResult{
		Allowed:      bucket.tokens > 0,
		Limit:        bucket.capacity,
		Remaining:    bucket.tokens,
		ResetTime:    bucket.resetTime,
		ResetAfter:   time.Until(bucket.resetTime),
		RequestCount: bucket.requests,
	}, nil
}

// Reset resets limits for a key
func (rl *MemoryRateLimiter) Reset(ctx context.Context, key string) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	delete(rl.buckets, key)
	return nil
}

// Cleanup removes expired buckets
func (rl *MemoryRateLimiter) Cleanup(ctx context.Context) error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, bucket := range rl.buckets {
		if now.After(bucket.resetTime.Add(bucket.window)) {
			delete(rl.buckets, key)
		}
	}

	return nil
}

// refillBucket refills tokens in the bucket
func (rl *MemoryRateLimiter) refillBucket(bucket *TokenBucket) {
	now := time.Now()

	// Check if we need to reset the window
	if now.After(bucket.resetTime) {
		bucket.tokens = bucket.capacity
		bucket.requests = 0
		bucket.resetTime = now.Add(bucket.window)
		bucket.lastRefill = now
		return
	}

	// Calculate tokens to add based on time elapsed
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int64(elapsed.Seconds() * bucket.refillRate)

	if tokensToAdd > 0 {
		bucket.tokens += tokensToAdd
		if bucket.tokens > bucket.capacity {
			bucket.tokens = bucket.capacity
		}
		bucket.lastRefill = now
	}
}

// cleanupLoop runs periodic cleanup
func (rl *MemoryRateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.cleaner.C:
			rl.Cleanup(context.Background())
		case <-rl.done:
			return
		}
	}
}

// Stop stops the rate limiter
func (rl *MemoryRateLimiter) Stop() {
	rl.cleaner.Stop()
	close(rl.done)
}

// Redis-based rate limiter (placeholder implementation)

// RedisRateLimiter provides Redis-based rate limiting
type RedisRateLimiter struct {
	// redis client would be here
	prefix string
}

// NewRedisRateLimiter creates a new Redis-based rate limiter
func NewRedisRateLimiter(redisAddr, prefix string) *RedisRateLimiter {
	return &RedisRateLimiter{
		prefix: prefix,
	}
}

// Allow checks if a request is allowed (Redis implementation)
func (rl *RedisRateLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, *RateLimitResult, error) {
	// Implementation would use Redis commands for distributed rate limiting
	// This is a placeholder
	return true, &RateLimitResult{
		Allowed:   true,
		Limit:     limit,
		Remaining: limit - 1,
		ResetTime: time.Now().Add(window),
	}, nil
}

// GetLimit returns current limit information (Redis implementation)
func (rl *RedisRateLimiter) GetLimit(ctx context.Context, key string) (*RateLimitResult, error) {
	// Implementation would query Redis for current state
	return &RateLimitResult{}, nil
}

// Reset resets limits for a key (Redis implementation)
func (rl *RedisRateLimiter) Reset(ctx context.Context, key string) error {
	// Implementation would delete Redis keys
	return nil
}

// Cleanup removes expired entries (Redis implementation)
func (rl *RedisRateLimiter) Cleanup(ctx context.Context) error {
	// Redis handles TTL automatically
	return nil
}

// Utility functions

// RateLimitMiddlewareFunc creates a simple rate limit middleware function
func RateLimitMiddlewareFunc(config router.RateLimitConfig) func(http.Handler) http.Handler {
	middleware := NewRateLimitMiddleware(config, nil)
	return middleware.Handle
}

// WithMemoryRateLimit creates rate limit middleware with memory backend
func WithMemoryRateLimit(requestsPerSecond float64, burstSize int) func(http.Handler) http.Handler {
	config := router.RateLimitConfig{
		RequestsPerSecond: requestsPerSecond,
		BurstSize:         burstSize,
		Strategy:          router.RateLimitByIP,
	}
	return RateLimitMiddlewareFunc(config)
}

// WithRedisRateLimit creates rate limit middleware with Redis backend
func WithRedisRateLimit(redisAddr string, requestsPerSecond float64, burstSize int) func(http.Handler) http.Handler {
	config := router.RateLimitConfig{
		RequestsPerSecond: requestsPerSecond,
		BurstSize:         burstSize,
		Strategy:          router.RateLimitByIP,
	}

	limiter := NewRedisRateLimiter(redisAddr, "rate_limit:")
	middleware := NewRateLimitMiddleware(config, limiter)
	return middleware.Handle
}

// WithIPRateLimit creates IP-based rate limiting
func WithIPRateLimit(requestsPerSecond float64, burstSize int) func(http.Handler) http.Handler {
	config := router.RateLimitConfig{
		RequestsPerSecond: requestsPerSecond,
		BurstSize:         burstSize,
		Strategy:          router.RateLimitByIP,
	}
	return RateLimitMiddlewareFunc(config)
}

// WithUserRateLimit creates user-based rate limiting
func WithUserRateLimit(requestsPerSecond float64, burstSize int) func(http.Handler) http.Handler {
	config := router.RateLimitConfig{
		RequestsPerSecond: requestsPerSecond,
		BurstSize:         burstSize,
		Strategy:          router.RateLimitByUser,
	}
	return RateLimitMiddlewareFunc(config)
}

// WithAPIKeyRateLimit creates API key-based rate limiting
func WithAPIKeyRateLimit(requestsPerSecond float64, burstSize int) func(http.Handler) http.Handler {
	config := router.RateLimitConfig{
		RequestsPerSecond: requestsPerSecond,
		BurstSize:         burstSize,
		Strategy:          router.RateLimitByAPIKey,
	}
	return RateLimitMiddlewareFunc(config)
}

// WithGlobalRateLimit creates global rate limiting
func WithGlobalRateLimit(requestsPerSecond float64, burstSize int) func(http.Handler) http.Handler {
	config := router.RateLimitConfig{
		RequestsPerSecond: requestsPerSecond,
		BurstSize:         burstSize,
		Strategy:          router.RateLimitGlobal,
	}
	return RateLimitMiddlewareFunc(config)
}

// Rate limit testing utilities

// TestRateLimiter provides utilities for testing rate limits
type TestRateLimiter struct {
	allowNext bool
	result    *RateLimitResult
	err       error
}

// NewTestRateLimiter creates a test rate limiter
func NewTestRateLimiter() *TestRateLimiter {
	return &TestRateLimiter{
		allowNext: true,
		result: &RateLimitResult{
			Allowed:   true,
			Limit:     100,
			Remaining: 99,
			ResetTime: time.Now().Add(time.Hour),
		},
	}
}

// SetAllowNext sets whether next request should be allowed
func (trl *TestRateLimiter) SetAllowNext(allow bool) {
	trl.allowNext = allow
	trl.result.Allowed = allow
}

// SetResult sets the result to return
func (trl *TestRateLimiter) SetResult(result *RateLimitResult) {
	trl.result = result
}

// SetError sets the error to return
func (trl *TestRateLimiter) SetError(err error) {
	trl.err = err
}

// Allow implements RateLimiter interface
func (trl *TestRateLimiter) Allow(ctx context.Context, key string, limit int64, window time.Duration) (bool, *RateLimitResult, error) {
	return trl.allowNext, trl.result, trl.err
}

// GetLimit implements RateLimiter interface
func (trl *TestRateLimiter) GetLimit(ctx context.Context, key string) (*RateLimitResult, error) {
	return trl.result, trl.err
}

// Reset implements RateLimiter interface
func (trl *TestRateLimiter) Reset(ctx context.Context, key string) error {
	return trl.err
}

// Cleanup implements RateLimiter interface
func (trl *TestRateLimiter) Cleanup(ctx context.Context) error {
	return trl.err
}

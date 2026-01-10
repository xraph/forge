package sdk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/ai/llm"
)

// Middleware intercepts and processes AI requests/responses.
type Middleware interface {
	// Name returns the middleware name.
	Name() string
	// ProcessRequest processes a request and optionally short-circuits with a response.
	ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error)
}

// MiddlewareHandler is the next handler in the chain.
type MiddlewareHandler func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error)

// MiddlewareRequest wraps a chat request with metadata.
type MiddlewareRequest struct {
	ChatRequest llm.ChatRequest
	Metadata    map[string]any
	StartTime   time.Time
}

// MiddlewareResponse wraps a chat response with metadata.
type MiddlewareResponse struct {
	ChatResponse llm.ChatResponse
	Metadata     map[string]any
	Cached       bool
	CacheKey     string
	Duration     time.Duration
}

// MiddlewareChain manages a chain of middleware.
type MiddlewareChain struct {
	middlewares []Middleware
	logger      forge.Logger
	metrics     forge.Metrics
}

// NewMiddlewareChain creates a new middleware chain.
func NewMiddlewareChain(logger forge.Logger, metrics forge.Metrics) *MiddlewareChain {
	return &MiddlewareChain{
		middlewares: make([]Middleware, 0),
		logger:      logger,
		metrics:     metrics,
	}
}

// Use adds a middleware to the chain.
func (c *MiddlewareChain) Use(m Middleware) *MiddlewareChain {
	c.middlewares = append(c.middlewares, m)

	return c
}

// Execute executes the middleware chain.
func (c *MiddlewareChain) Execute(ctx context.Context, req *MiddlewareRequest, final MiddlewareHandler) (*MiddlewareResponse, error) {
	if req.Metadata == nil {
		req.Metadata = make(map[string]any)
	}

	req.StartTime = time.Now()

	// Build the chain from end to start
	handler := final

	for i := len(c.middlewares) - 1; i >= 0; i-- {
		m := c.middlewares[i]
		next := handler
		handler = func(ctx context.Context, req *MiddlewareRequest) (*MiddlewareResponse, error) {
			return m.ProcessRequest(ctx, req, next)
		}
	}

	return handler(ctx, req)
}

// Len returns the number of middleware in the chain.
func (c *MiddlewareChain) Len() int {
	return len(c.middlewares)
}

// LoggingMiddleware logs requests and responses.
type LoggingMiddleware struct {
	logger      forge.Logger
	logRequest  bool
	logResponse bool
	redactKeys  []string
}

// LoggingConfig configures logging middleware.
type LoggingConfig struct {
	LogRequest  bool
	LogResponse bool
	RedactKeys  []string // Keys to redact from logs
}

// NewLoggingMiddleware creates a logging middleware.
func NewLoggingMiddleware(logger forge.Logger, config LoggingConfig) *LoggingMiddleware {
	return &LoggingMiddleware{
		logger:      logger,
		logRequest:  config.LogRequest,
		logResponse: config.LogResponse,
		redactKeys:  config.RedactKeys,
	}
}

// Name implements Middleware.
func (m *LoggingMiddleware) Name() string {
	return "logging"
}

// ProcessRequest implements Middleware.
func (m *LoggingMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	startTime := time.Now()

	// Log request
	if m.logRequest && m.logger != nil {
		m.logger.Info("AI request",
			F("provider", req.ChatRequest.Provider),
			F("model", req.ChatRequest.Model),
			F("messages_count", len(req.ChatRequest.Messages)),
		)
	}

	// Call next
	resp, err := next(ctx, req)

	duration := time.Since(startTime)

	// Log response or error
	if m.logger != nil {
		if err != nil {
			m.logger.Error("AI request failed",
				F("provider", req.ChatRequest.Provider),
				F("model", req.ChatRequest.Model),
				F("duration", duration),
				F("error", err.Error()),
			)
		} else if m.logResponse {
			usage := "none"
			if resp.ChatResponse.Usage != nil {
				usage = fmt.Sprintf("%d tokens", resp.ChatResponse.Usage.TotalTokens)
			}

			m.logger.Info("AI response",
				F("provider", req.ChatRequest.Provider),
				F("model", req.ChatRequest.Model),
				F("duration", duration),
				F("cached", resp.Cached),
				F("usage", usage),
			)
		}
	}

	if resp != nil {
		resp.Duration = duration
	}

	return resp, err
}

// CachingMiddleware caches responses.
type CachingMiddleware struct {
	cache     Cache
	ttl       time.Duration
	keyFunc   func(*MiddlewareRequest) string
	skipCache func(*MiddlewareRequest) bool
}

// Cache interface for caching middleware.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, bool)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// CachingConfig configures caching middleware.
type CachingConfig struct {
	Cache     Cache
	TTL       time.Duration
	KeyFunc   func(*MiddlewareRequest) string
	SkipCache func(*MiddlewareRequest) bool
}

// NewCachingMiddleware creates a caching middleware.
func NewCachingMiddleware(config CachingConfig) *CachingMiddleware {
	keyFunc := config.KeyFunc
	if keyFunc == nil {
		keyFunc = DefaultCacheKeyFunc
	}

	return &CachingMiddleware{
		cache:     config.Cache,
		ttl:       config.TTL,
		keyFunc:   keyFunc,
		skipCache: config.SkipCache,
	}
}

// Name implements Middleware.
func (m *CachingMiddleware) Name() string {
	return "caching"
}

// ProcessRequest implements Middleware.
func (m *CachingMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	// Check if caching should be skipped
	if m.skipCache != nil && m.skipCache(req) {
		return next(ctx, req)
	}

	// Generate cache key
	key := m.keyFunc(req)

	// Try cache lookup
	if data, ok := m.cache.Get(ctx, key); ok {
		var cached llm.ChatResponse
		if err := json.Unmarshal(data, &cached); err == nil {
			return &MiddlewareResponse{
				ChatResponse: cached,
				Cached:       true,
				CacheKey:     key,
			}, nil
		}
	}

	// Call next
	resp, err := next(ctx, req)
	if err != nil {
		return resp, err
	}

	// Cache response
	if data, err := json.Marshal(resp.ChatResponse); err == nil {
		m.cache.Set(ctx, key, data, m.ttl)
	}

	resp.CacheKey = key

	return resp, nil
}

// DefaultCacheKeyFunc generates a default cache key from the request.
func DefaultCacheKeyFunc(req *MiddlewareRequest) string {
	hasher := sha256.New()
	hasher.Write([]byte(req.ChatRequest.Provider))
	hasher.Write([]byte(req.ChatRequest.Model))

	for _, msg := range req.ChatRequest.Messages {
		hasher.Write([]byte(msg.Role))
		hasher.Write([]byte(msg.Content))
	}

	if req.ChatRequest.Temperature != nil {
		fmt.Fprintf(hasher, "%f", *req.ChatRequest.Temperature)
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// RateLimitMiddleware implements rate limiting.
type RateLimitMiddleware struct {
	limiter    MiddlewareRateLimiter
	keyFunc    func(*MiddlewareRequest) string
	onRejected func(*MiddlewareRequest) error
}

// MiddlewareRateLimiter interface for rate limiting in middleware.
type MiddlewareRateLimiter interface {
	Allow(key string) bool
	AllowN(key string, n int) bool
	Reset(key string)
}

// MiddlewareRateLimitConfig configures rate limiting middleware.
type MiddlewareRateLimitConfig struct {
	Limiter    MiddlewareRateLimiter
	KeyFunc    func(*MiddlewareRequest) string
	OnRejected func(*MiddlewareRequest) error
}

// NewRateLimitMiddleware creates a rate limiting middleware.
func NewRateLimitMiddleware(config MiddlewareRateLimitConfig) *RateLimitMiddleware {
	keyFunc := config.KeyFunc
	if keyFunc == nil {
		keyFunc = func(req *MiddlewareRequest) string {
			return req.ChatRequest.Provider + ":" + req.ChatRequest.Model
		}
	}

	return &RateLimitMiddleware{
		limiter:    config.Limiter,
		keyFunc:    keyFunc,
		onRejected: config.OnRejected,
	}
}

// Name implements Middleware.
func (m *RateLimitMiddleware) Name() string {
	return "rate_limit"
}

// ProcessRequest implements Middleware.
func (m *RateLimitMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	key := m.keyFunc(req)

	if !m.limiter.Allow(key) {
		if m.onRejected != nil {
			return nil, m.onRejected(req)
		}

		return nil, fmt.Errorf("rate limit exceeded for %s", key)
	}

	return next(ctx, req)
}

// TokenBucketLimiter implements token bucket rate limiting.
type TokenBucketLimiter struct {
	buckets  map[string]*tokenBucket
	rate     float64 // tokens per second
	capacity int
	mu       sync.Mutex
}

type tokenBucket struct {
	tokens     float64
	lastUpdate time.Time
}

// NewTokenBucketLimiter creates a token bucket rate limiter.
func NewTokenBucketLimiter(rate float64, capacity int) *TokenBucketLimiter {
	return &TokenBucketLimiter{
		buckets:  make(map[string]*tokenBucket),
		rate:     rate,
		capacity: capacity,
	}
}

// Allow implements RateLimiter.
func (l *TokenBucketLimiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

// AllowN implements RateLimiter.
func (l *TokenBucketLimiter) AllowN(key string, n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	bucket, ok := l.buckets[key]
	if !ok {
		bucket = &tokenBucket{
			tokens:     float64(l.capacity),
			lastUpdate: time.Now(),
		}
		l.buckets[key] = bucket
	}

	// Refill tokens based on elapsed time
	now := time.Now()
	elapsed := now.Sub(bucket.lastUpdate).Seconds()

	bucket.tokens += elapsed * l.rate
	if bucket.tokens > float64(l.capacity) {
		bucket.tokens = float64(l.capacity)
	}

	bucket.lastUpdate = now

	// Try to consume tokens
	if bucket.tokens >= float64(n) {
		bucket.tokens -= float64(n)

		return true
	}

	return false
}

// Reset implements RateLimiter.
func (l *TokenBucketLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.buckets, key)
}

// SlidingWindowLimiter implements sliding window rate limiting.
type SlidingWindowLimiter struct {
	windows map[string]*slidingWindow
	limit   int
	window  time.Duration
	mu      sync.Mutex
}

type slidingWindow struct {
	count       int
	windowStart time.Time
}

// NewSlidingWindowLimiter creates a sliding window rate limiter.
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		windows: make(map[string]*slidingWindow),
		limit:   limit,
		window:  window,
	}
}

// Allow implements RateLimiter.
func (l *SlidingWindowLimiter) Allow(key string) bool {
	return l.AllowN(key, 1)
}

// AllowN implements RateLimiter.
func (l *SlidingWindowLimiter) AllowN(key string, n int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	w, ok := l.windows[key]

	if !ok || now.Sub(w.windowStart) >= l.window {
		// New window
		l.windows[key] = &slidingWindow{
			count:       n,
			windowStart: now,
		}

		return n <= l.limit
	}

	// Check if within limit
	if w.count+n <= l.limit {
		w.count += n

		return true
	}

	return false
}

// Reset implements RateLimiter.
func (l *SlidingWindowLimiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.windows, key)
}

// CostTrackingMiddleware tracks costs of AI requests.
type CostTrackingMiddleware struct {
	costs       map[string]float64 // model -> cost per 1K tokens
	totalCost   float64
	costFunc    func(req *MiddlewareRequest, resp *MiddlewareResponse) float64
	onCostEvent func(model string, cost float64, totalCost float64)
	mu          sync.Mutex
}

// CostTrackingConfig configures cost tracking middleware.
type CostTrackingConfig struct {
	Costs       map[string]float64
	CostFunc    func(req *MiddlewareRequest, resp *MiddlewareResponse) float64
	OnCostEvent func(model string, cost float64, totalCost float64)
}

// NewCostTrackingMiddleware creates a cost tracking middleware.
func NewCostTrackingMiddleware(config CostTrackingConfig) *CostTrackingMiddleware {
	costs := config.Costs
	if costs == nil {
		costs = make(map[string]float64)
	}

	return &CostTrackingMiddleware{
		costs:       costs,
		costFunc:    config.CostFunc,
		onCostEvent: config.OnCostEvent,
	}
}

// SetModelCost sets the cost per 1K tokens for a model.
func (m *CostTrackingMiddleware) SetModelCost(model string, costPer1K float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.costs[model] = costPer1K
}

// GetTotalCost returns the total accumulated cost.
func (m *CostTrackingMiddleware) GetTotalCost() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.totalCost
}

// ResetCost resets the total cost to zero.
func (m *CostTrackingMiddleware) ResetCost() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalCost = 0
}

// Name implements Middleware.
func (m *CostTrackingMiddleware) Name() string {
	return "cost_tracking"
}

// ProcessRequest implements Middleware.
func (m *CostTrackingMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	resp, err := next(ctx, req)
	if err != nil {
		return resp, err
	}

	// Calculate cost
	var cost float64
	if m.costFunc != nil {
		cost = m.costFunc(req, resp)
	} else {
		cost = m.defaultCostCalc(req, resp)
	}

	// Update total
	m.mu.Lock()
	m.totalCost += cost
	totalCost := m.totalCost
	m.mu.Unlock()

	// Callback
	if m.onCostEvent != nil {
		m.onCostEvent(req.ChatRequest.Model, cost, totalCost)
	}

	// Add to metadata
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}

	resp.Metadata["cost"] = cost
	resp.Metadata["total_cost"] = totalCost

	return resp, nil
}

// defaultCostCalc calculates cost based on token usage.
func (m *CostTrackingMiddleware) defaultCostCalc(req *MiddlewareRequest, resp *MiddlewareResponse) float64 {
	if resp.ChatResponse.Usage == nil {
		return 0
	}

	m.mu.Lock()
	costPer1K := m.costs[req.ChatRequest.Model]
	m.mu.Unlock()

	if costPer1K == 0 {
		return 0
	}

	totalTokens := float64(resp.ChatResponse.Usage.TotalTokens)

	return (totalTokens / 1000) * costPer1K
}

// RetryMiddleware implements retry logic with exponential backoff.
type RetryMiddleware struct {
	maxRetries   int
	initialDelay time.Duration
	maxDelay     time.Duration
	retryOn      func(error) bool
	logger       forge.Logger
}

// MiddlewareRetryConfig configures retry middleware.
type MiddlewareRetryConfig struct {
	MaxRetries   int
	InitialDelay time.Duration
	MaxDelay     time.Duration
	RetryOn      func(error) bool
	Logger       forge.Logger
}

// NewRetryMiddleware creates a retry middleware.
func NewRetryMiddleware(config MiddlewareRetryConfig) *RetryMiddleware {
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}

	if config.InitialDelay <= 0 {
		config.InitialDelay = time.Second
	}

	if config.MaxDelay <= 0 {
		config.MaxDelay = 30 * time.Second
	}

	if config.RetryOn == nil {
		config.RetryOn = DefaultRetryCondition
	}

	return &RetryMiddleware{
		maxRetries:   config.MaxRetries,
		initialDelay: config.InitialDelay,
		maxDelay:     config.MaxDelay,
		retryOn:      config.RetryOn,
		logger:       config.Logger,
	}
}

// Name implements Middleware.
func (m *RetryMiddleware) Name() string {
	return "retry"
}

// ProcessRequest implements Middleware.
func (m *RetryMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	var lastErr error

	delay := m.initialDelay

	for attempt := 0; attempt <= m.maxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}

			// Exponential backoff
			delay *= 2
			if delay > m.maxDelay {
				delay = m.maxDelay
			}

			if m.logger != nil {
				m.logger.Debug("Retrying request",
					F("attempt", attempt),
					F("delay", delay),
				)
			}
		}

		resp, err := next(ctx, req)
		if err == nil {
			if resp.Metadata == nil {
				resp.Metadata = make(map[string]any)
			}

			resp.Metadata["retry_attempts"] = attempt

			return resp, nil
		}

		lastErr = err

		// Check if we should retry
		if !m.retryOn(err) {
			return nil, err
		}
	}

	return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}

// DefaultRetryCondition returns true for retryable errors.
func DefaultRetryCondition(err error) bool {
	// Retry on context deadline exceeded, temporary errors, etc.
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Add more conditions as needed
	return false
}

// TimeoutMiddleware adds timeout to requests.
type TimeoutMiddleware struct {
	timeout time.Duration
}

// NewTimeoutMiddleware creates a timeout middleware.
func NewTimeoutMiddleware(timeout time.Duration) *TimeoutMiddleware {
	return &TimeoutMiddleware{timeout: timeout}
}

// Name implements Middleware.
func (m *TimeoutMiddleware) Name() string {
	return "timeout"
}

// ProcessRequest implements Middleware.
func (m *TimeoutMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	return next(ctx, req)
}

// InMemoryCache is a simple in-memory cache implementation.
type InMemoryCache struct {
	data map[string]*cacheEntry
	mu   sync.RWMutex
}

type cacheEntry struct {
	value     []byte
	expiresAt time.Time
}

// NewInMemoryCache creates a new in-memory cache.
func NewInMemoryCache() *InMemoryCache {
	cache := &InMemoryCache{
		data: make(map[string]*cacheEntry),
	}
	// Start cleanup goroutine
	go cache.cleanup()

	return cache
}

// Get implements Cache.
func (c *InMemoryCache) Get(ctx context.Context, key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.data[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.value, true
}

// Set implements Cache.
func (c *InMemoryCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.data[key] = &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	}

	return nil
}

// Delete implements Cache.
func (c *InMemoryCache) Delete(ctx context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.data, key)

	return nil
}

// cleanup periodically removes expired entries.
func (c *InMemoryCache) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()

		now := time.Now()
		for key, entry := range c.data {
			if now.After(entry.expiresAt) {
				delete(c.data, key)
			}
		}

		c.mu.Unlock()
	}
}

// MetricsMiddleware collects metrics on requests.
type MetricsMiddleware struct {
	metrics forge.Metrics
	prefix  string
}

// NewMetricsMiddleware creates a metrics middleware.
func NewMetricsMiddleware(metrics forge.Metrics, prefix string) *MetricsMiddleware {
	if prefix == "" {
		prefix = "forge.ai.sdk"
	}

	return &MetricsMiddleware{
		metrics: metrics,
		prefix:  prefix,
	}
}

// Name implements Middleware.
func (m *MetricsMiddleware) Name() string {
	return "metrics"
}

// ProcessRequest implements Middleware.
func (m *MetricsMiddleware) ProcessRequest(ctx context.Context, req *MiddlewareRequest, next MiddlewareHandler) (*MiddlewareResponse, error) {
	startTime := time.Now()

	// Increment request counter
	m.metrics.Counter(m.prefix+".requests",
		"provider", req.ChatRequest.Provider,
		"model", req.ChatRequest.Model,
	).Inc()

	resp, err := next(ctx, req)

	duration := time.Since(startTime)

	// Record duration
	m.metrics.Histogram(m.prefix+".duration",
		"provider", req.ChatRequest.Provider,
		"model", req.ChatRequest.Model,
	).Observe(duration.Seconds())

	if err != nil {
		// Increment error counter
		m.metrics.Counter(m.prefix+".errors",
			"provider", req.ChatRequest.Provider,
			"model", req.ChatRequest.Model,
		).Inc()
	} else {
		// Record token usage
		if resp.ChatResponse.Usage != nil {
			m.metrics.Histogram(m.prefix+".tokens",
				"provider", req.ChatRequest.Provider,
				"model", req.ChatRequest.Model,
			).Observe(float64(resp.ChatResponse.Usage.TotalTokens))
		}

		// Record cache hits
		if resp.Cached {
			m.metrics.Counter(m.prefix+".cache_hits",
				"provider", req.ChatRequest.Provider,
				"model", req.ChatRequest.Model,
			).Inc()
		}
	}

	return resp, err
}

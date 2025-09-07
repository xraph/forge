package hooks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/plugins"
)

// ExecutionEngine handles the execution of hooks with various strategies
type ExecutionEngine interface {
	Initialize(ctx context.Context) error
	Stop(ctx context.Context) error
	ExecuteSequential(ctx context.Context, hooks []plugins.Hook, data plugins.HookData) ([]plugins.HookResult, error)
	ExecuteConcurrent(ctx context.Context, hooks []plugins.Hook, data plugins.HookData) ([]plugins.HookResult, error)
	ExecuteConditional(ctx context.Context, hooks []plugins.Hook, data plugins.HookData, condition HookCondition) ([]plugins.HookResult, error)
	ExecuteWithTimeout(ctx context.Context, hooks []plugins.Hook, data plugins.HookData, timeout time.Duration) ([]plugins.HookResult, error)
	ExecuteWithRetry(ctx context.Context, hook plugins.Hook, data plugins.HookData, retryConfig RetryConfig) (plugins.HookResult, error)
	GetStats() ExecutionStats
}

// ExecutionEngineImpl implements the ExecutionEngine interface
type ExecutionEngineImpl struct {
	registry     HookRegistry
	executor     *HookExecutor
	circuitBrkr  *CircuitBreaker
	rateLimiter  *RateLimiter
	cache        *ExecutionCache
	interceptors []ExecutionInterceptor
	logger       common.Logger
	metrics      common.Metrics
	mu           sync.RWMutex
	initialized  bool
	stats        ExecutionStats
	config       ExecutionConfig
}

// ExecutionConfig contains configuration for hook execution
type ExecutionConfig struct {
	MaxConcurrentHooks   int                  `json:"max_concurrent_hooks" default:"10"`
	DefaultTimeout       time.Duration        `json:"default_timeout" default:"30s"`
	MaxRetries           int                  `json:"max_retries" default:"3"`
	RetryDelay           time.Duration        `json:"retry_delay" default:"1s"`
	CircuitBreakerConfig CircuitBreakerConfig `json:"circuit_breaker"`
	RateLimitConfig      RateLimitConfig      `json:"rate_limit"`
	CacheConfig          CacheConfig          `json:"cache"`
	EnableMetrics        bool                 `json:"enable_metrics" default:"true"`
	EnableTracing        bool                 `json:"enable_tracing" default:"true"`
}

// ExecutionStats contains execution statistics
type ExecutionStats struct {
	TotalExecutions      int64                              `json:"total_executions"`
	SuccessfulExecutions int64                              `json:"successful_executions"`
	FailedExecutions     int64                              `json:"failed_executions"`
	AverageLatency       time.Duration                      `json:"average_latency"`
	ExecutionsByType     map[plugins.HookType]int64         `json:"executions_by_type"`
	LatencyByType        map[plugins.HookType]time.Duration `json:"latency_by_type"`
	CircuitBreakerStats  CircuitBreakerStats                `json:"circuit_breaker_stats"`
	CacheStats           CacheStats                         `json:"cache_stats"`
	LastExecution        time.Time                          `json:"last_execution"`
}

// HookCondition defines conditions for conditional execution
type HookCondition interface {
	ShouldExecute(ctx context.Context, hook plugins.Hook, data plugins.HookData) bool
	Name() string
	Priority() int
}

// RetryConfig contains retry configuration
type RetryConfig struct {
	MaxRetries      int           `json:"max_retries"`
	InitialDelay    time.Duration `json:"initial_delay"`
	MaxDelay        time.Duration `json:"max_delay"`
	Multiplier      float64       `json:"multiplier"`
	Jitter          bool          `json:"jitter"`
	RetryableErrors []string      `json:"retryable_errors"`
}

// ExecutionInterceptor allows intercepting hook executions
type ExecutionInterceptor interface {
	BeforeExecution(ctx context.Context, hook plugins.Hook, data plugins.HookData) error
	AfterExecution(ctx context.Context, hook plugins.Hook, result plugins.HookResult) error
	OnError(ctx context.Context, hook plugins.Hook, err error) error
}

// NewExecutionEngine creates a new execution engine
func NewExecutionEngine(registry HookRegistry, logger common.Logger, metrics common.Metrics) *ExecutionEngineImpl {
	config := ExecutionConfig{
		MaxConcurrentHooks: 10,
		DefaultTimeout:     30 * time.Second,
		MaxRetries:         3,
		RetryDelay:         1 * time.Second,
		EnableMetrics:      true,
		EnableTracing:      true,
	}

	ee := &ExecutionEngineImpl{
		registry: registry,
		logger:   logger,
		metrics:  metrics,
		config:   config,
		stats: ExecutionStats{
			ExecutionsByType: make(map[plugins.HookType]int64),
			LatencyByType:    make(map[plugins.HookType]time.Duration),
		},
	}

	// Initialize components
	ee.executor = NewHookExecutor(logger, metrics)
	ee.circuitBrkr = NewCircuitBreaker(config.CircuitBreakerConfig)
	ee.rateLimiter = NewRateLimiter(config.RateLimitConfig)
	ee.cache = NewExecutionCache(config.CacheConfig)

	return ee
}

// Initialize initializes the execution engine
func (ee *ExecutionEngineImpl) Initialize(ctx context.Context) error {
	ee.mu.Lock()
	defer ee.mu.Unlock()

	if ee.initialized {
		return nil
	}

	// Initialize components
	if err := ee.executor.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize hook executor: %w", err)
	}

	if err := ee.circuitBrkr.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize circuit breaker: %w", err)
	}

	if err := ee.rateLimiter.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize rate limiter: %w", err)
	}

	if err := ee.cache.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize cache: %w", err)
	}

	ee.initialized = true

	if ee.logger != nil {
		ee.logger.Info("execution engine initialized")
	}

	if ee.metrics != nil {
		ee.metrics.Counter("forge.hooks.execution_engine_initialized").Inc()
	}

	return nil
}

// Stop stops the execution engine
func (ee *ExecutionEngineImpl) Stop(ctx context.Context) error {
	ee.mu.Lock()
	defer ee.mu.Unlock()

	if !ee.initialized {
		return nil
	}

	// Stop components
	if err := ee.cache.Stop(ctx); err != nil {
		if ee.logger != nil {
			ee.logger.Error("failed to stop cache", logger.Error(err))
		}
	}

	if err := ee.rateLimiter.Stop(ctx); err != nil {
		if ee.logger != nil {
			ee.logger.Error("failed to stop rate limiter", logger.Error(err))
		}
	}

	if err := ee.circuitBrkr.Stop(ctx); err != nil {
		if ee.logger != nil {
			ee.logger.Error("failed to stop circuit breaker", logger.Error(err))
		}
	}

	if err := ee.executor.Stop(ctx); err != nil {
		if ee.logger != nil {
			ee.logger.Error("failed to stop hook executor", logger.Error(err))
		}
	}

	ee.initialized = false

	if ee.logger != nil {
		ee.logger.Info("execution engine stopped")
	}

	return nil
}

// ExecuteSequential executes hooks sequentially
func (ee *ExecutionEngineImpl) ExecuteSequential(ctx context.Context, hooks []plugins.Hook, data plugins.HookData) ([]plugins.HookResult, error) {
	ee.mu.RLock()
	if !ee.initialized {
		ee.mu.RUnlock()
		return nil, fmt.Errorf("execution engine not initialized")
	}
	ee.mu.RUnlock()

	startTime := time.Now()
	results := make([]plugins.HookResult, 0, len(hooks))

	// Execute hooks one by one
	for _, hook := range hooks {
		// Check rate limit
		if !ee.rateLimiter.Allow(hook.Name()) {
			result := plugins.HookResult{
				Continue: false,
				Error:    fmt.Errorf("rate limit exceeded for hook %s", hook.Name()),
			}
			results = append(results, result)
			continue
		}

		// Check circuit breaker
		if !ee.circuitBrkr.CanExecute(hook.Name()) {
			result := plugins.HookResult{
				Continue: false,
				Error:    fmt.Errorf("circuit breaker open for hook %s", hook.Name()),
			}
			results = append(results, result)
			continue
		}

		// Execute hook with timeout
		hookCtx, cancel := context.WithTimeout(ctx, ee.config.DefaultTimeout)
		result, err := ee.executeHookWithInterceptors(hookCtx, hook, data)
		cancel()

		if err != nil {
			ee.circuitBrkr.RecordFailure(hook.Name())
			result = plugins.HookResult{
				Continue: false,
				Error:    err,
			}
		} else {
			ee.circuitBrkr.RecordSuccess(hook.Name())
		}

		results = append(results, result)

		// Stop execution if hook indicates to stop
		if !result.Continue && result.Error != nil {
			break
		}
	}

	// Update statistics
	ee.updateExecutionStats("sequential", startTime, len(results), len(hooks))

	return results, nil
}

// ExecuteConcurrent executes hooks concurrently
func (ee *ExecutionEngineImpl) ExecuteConcurrent(ctx context.Context, hooks []plugins.Hook, data plugins.HookData) ([]plugins.HookResult, error) {
	ee.mu.RLock()
	if !ee.initialized {
		ee.mu.RUnlock()
		return nil, fmt.Errorf("execution engine not initialized")
	}
	ee.mu.RUnlock()

	startTime := time.Now()

	// Create semaphore to limit concurrent executions
	semaphore := make(chan struct{}, ee.config.MaxConcurrentHooks)
	resultChan := make(chan indexedResult, len(hooks))

	// Execute hooks concurrently
	for i, hook := range hooks {
		go func(index int, h plugins.Hook) {
			// Acquire semaphore
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			// Check rate limit
			if !ee.rateLimiter.Allow(h.Name()) {
				resultChan <- indexedResult{
					Index: index,
					Result: plugins.HookResult{
						Continue: false,
						Error:    fmt.Errorf("rate limit exceeded for hook %s", h.Name()),
					},
				}
				return
			}

			// Check circuit breaker
			if !ee.circuitBrkr.CanExecute(h.Name()) {
				resultChan <- indexedResult{
					Index: index,
					Result: plugins.HookResult{
						Continue: false,
						Error:    fmt.Errorf("circuit breaker open for hook %s", h.Name()),
					},
				}
				return
			}

			// Execute hook with timeout
			hookCtx, cancel := context.WithTimeout(ctx, ee.config.DefaultTimeout)
			result, err := ee.executeHookWithInterceptors(hookCtx, h, data)
			cancel()

			if err != nil {
				ee.circuitBrkr.RecordFailure(h.Name())
				result = plugins.HookResult{
					Continue: false,
					Error:    err,
				}
			} else {
				ee.circuitBrkr.RecordSuccess(h.Name())
			}

			resultChan <- indexedResult{
				Index:  index,
				Result: result,
			}
		}(i, hook)
	}

	// Collect results in order
	results := make([]plugins.HookResult, len(hooks))
	for i := 0; i < len(hooks); i++ {
		indexedResult := <-resultChan
		results[indexedResult.Index] = indexedResult.Result
	}

	// Update statistics
	ee.updateExecutionStats("concurrent", startTime, len(results), len(hooks))

	return results, nil
}

// ExecuteConditional executes hooks based on conditions
func (ee *ExecutionEngineImpl) ExecuteConditional(ctx context.Context, hooks []plugins.Hook, data plugins.HookData, condition HookCondition) ([]plugins.HookResult, error) {
	// Filter hooks based on condition
	var filteredHooks []plugins.Hook
	for _, hook := range hooks {
		if condition.ShouldExecute(ctx, hook, data) {
			filteredHooks = append(filteredHooks, hook)
		}
	}

	// Execute filtered hooks sequentially
	return ee.ExecuteSequential(ctx, filteredHooks, data)
}

// ExecuteWithTimeout executes hooks with a custom timeout
func (ee *ExecutionEngineImpl) ExecuteWithTimeout(ctx context.Context, hooks []plugins.Hook, data plugins.HookData, timeout time.Duration) ([]plugins.HookResult, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return ee.ExecuteSequential(timeoutCtx, hooks, data)
}

// ExecuteWithRetry executes a single hook with retry logic
func (ee *ExecutionEngineImpl) ExecuteWithRetry(ctx context.Context, hook plugins.Hook, data plugins.HookData, retryConfig RetryConfig) (plugins.HookResult, error) {
	var lastErr error
	delay := retryConfig.InitialDelay

	for attempt := 0; attempt <= retryConfig.MaxRetries; attempt++ {
		// Execute hook
		result, err := ee.executeHookWithInterceptors(ctx, hook, data)
		if err == nil {
			ee.circuitBrkr.RecordSuccess(hook.Name())
			return result, nil
		}

		lastErr = err

		// Check if error is retryable
		if !ee.isRetryableError(err, retryConfig.RetryableErrors) {
			break
		}

		// Don't retry on last attempt
		if attempt == retryConfig.MaxRetries {
			break
		}

		// Wait before retry
		if delay > 0 {
			select {
			case <-ctx.Done():
				return plugins.HookResult{}, ctx.Err()
			case <-time.After(delay):
			}
		}

		// Calculate next delay
		delay = time.Duration(float64(delay) * retryConfig.Multiplier)
		if delay > retryConfig.MaxDelay {
			delay = retryConfig.MaxDelay
		}

		// Add jitter if enabled
		if retryConfig.Jitter && delay > 0 {
			jitter := time.Duration(float64(delay) * 0.1) // 10% jitter
			delay += time.Duration(float64(jitter) * (2*time.Now().UnixNano()%100 - 100) / 100)
		}
	}

	ee.circuitBrkr.RecordFailure(hook.Name())
	return plugins.HookResult{
		Continue: false,
		Error:    lastErr,
	}, lastErr
}

// GetStats returns execution statistics
func (ee *ExecutionEngineImpl) GetStats() ExecutionStats {
	ee.mu.RLock()
	defer ee.mu.RUnlock()

	return ee.stats
}

// Helper methods

func (ee *ExecutionEngineImpl) executeHookWithInterceptors(ctx context.Context, hook plugins.Hook, data plugins.HookData) (plugins.HookResult, error) {
	// Run before interceptors
	for _, interceptor := range ee.interceptors {
		if err := interceptor.BeforeExecution(ctx, hook, data); err != nil {
			return plugins.HookResult{}, err
		}
	}

	// Check cache
	if cacheKey := ee.getCacheKey(hook, data); cacheKey != "" {
		if cachedResult, found := ee.cache.Get(cacheKey); found {
			if ee.metrics != nil {
				ee.metrics.Counter("forge.hooks.cache_hit").Inc()
			}
			return cachedResult, nil
		}
	}

	// Execute hook
	result, err := hook.Execute(ctx, data)

	// Handle error with interceptors
	if err != nil {
		for _, interceptor := range ee.interceptors {
			if interceptorErr := interceptor.OnError(ctx, hook, err); interceptorErr != nil {
				ee.logger.Error("interceptor error handling failed", logger.Error(interceptorErr))
			}
		}
		return result, err
	}

	// Cache successful result if applicable
	if cacheKey := ee.getCacheKey(hook, data); cacheKey != "" && err == nil {
		ee.cache.Set(cacheKey, result, ee.getCacheTTL(hook))
		if ee.metrics != nil {
			ee.metrics.Counter("forge.hooks.cache_set").Inc()
		}
	}

	// Run after interceptors
	for _, interceptor := range ee.interceptors {
		if err := interceptor.AfterExecution(ctx, hook, result); err != nil {
			ee.logger.Error("after execution interceptor failed", logger.Error(err))
		}
	}

	return result, nil
}

func (ee *ExecutionEngineImpl) updateExecutionStats(executionType string, startTime time.Time, resultCount, hookCount int) {
	ee.mu.Lock()
	defer ee.mu.Unlock()

	duration := time.Since(startTime)

	ee.stats.TotalExecutions++
	ee.stats.LastExecution = time.Now()

	if resultCount == hookCount {
		ee.stats.SuccessfulExecutions++
	} else {
		ee.stats.FailedExecutions++
	}

	// Update average latency
	if ee.stats.TotalExecutions > 0 {
		totalLatency := time.Duration(ee.stats.TotalExecutions-1) * ee.stats.AverageLatency
		ee.stats.AverageLatency = (totalLatency + duration) / time.Duration(ee.stats.TotalExecutions)
	}

	// Record metrics
	if ee.metrics != nil {
		ee.metrics.Counter("forge.hooks.executions", "type", executionType).Inc()
		ee.metrics.Histogram("forge.hooks.execution_duration", "type", executionType).Observe(duration.Seconds())
		ee.metrics.Gauge("forge.hooks.success_rate").Set(float64(ee.stats.SuccessfulExecutions) / float64(ee.stats.TotalExecutions))
	}
}

func (ee *ExecutionEngineImpl) getCacheKey(hook plugins.Hook, data plugins.HookData) string {
	// Generate cache key based on hook name and data hash
	// This is a simplified implementation
	if data.Type == "" {
		return ""
	}
	return fmt.Sprintf("hook:%s:%s:%d", hook.Name(), data.Type, hash(data))
}

func (ee *ExecutionEngineImpl) getCacheTTL(hook plugins.Hook) time.Duration {
	// Default TTL, could be configurable per hook
	return 5 * time.Minute
}

func (ee *ExecutionEngineImpl) isRetryableError(err error, retryableErrors []string) bool {
	if len(retryableErrors) == 0 {
		return true // Retry all errors by default
	}

	errStr := err.Error()
	for _, retryableErr := range retryableErrors {
		if errStr == retryableErr {
			return true
		}
	}
	return false
}

// Supporting types and functions

type indexedResult struct {
	Index  int
	Result plugins.HookResult
}

func hash(data plugins.HookData) int64 {
	// Simple hash implementation
	return time.Now().UnixNano() % 1000000
}

// Additional components (simplified implementations)

type HookExecutor struct {
	logger  common.Logger
	metrics common.Metrics
}

func NewHookExecutor(logger common.Logger, metrics common.Metrics) *HookExecutor {
	return &HookExecutor{logger: logger, metrics: metrics}
}

func (he *HookExecutor) Initialize(ctx context.Context) error {
	return nil
}

func (he *HookExecutor) Stop(ctx context.Context) error {
	return nil
}

type CircuitBreaker struct {
	config CircuitBreakerConfig
	states map[string]*CircuitBreakerState
	mu     sync.RWMutex
}

type CircuitBreakerConfig struct {
	FailureThreshold int           `json:"failure_threshold" default:"5"`
	ResetTimeout     time.Duration `json:"reset_timeout" default:"60s"`
	MaxRequests      int           `json:"max_requests" default:"3"`
}

type CircuitBreakerState struct {
	State        string
	FailureCount int
	LastFailure  time.Time
}

type CircuitBreakerStats struct {
	OpenCircuits     int `json:"open_circuits"`
	ClosedCircuits   int `json:"closed_circuits"`
	HalfOpenCircuits int `json:"half_open_circuits"`
}

func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		states: make(map[string]*CircuitBreakerState),
	}
}

func (cb *CircuitBreaker) Initialize(ctx context.Context) error {
	return nil
}

func (cb *CircuitBreaker) Stop(ctx context.Context) error {
	return nil
}

func (cb *CircuitBreaker) CanExecute(hookName string) bool {
	// Simplified implementation
	return true
}

func (cb *CircuitBreaker) RecordSuccess(hookName string) {
	// Implementation would track successful executions
}

func (cb *CircuitBreaker) RecordFailure(hookName string) {
	// Implementation would track failed executions
}

type RateLimiter struct {
	config RateLimitConfig
}

type RateLimitConfig struct {
	RequestsPerSecond int `json:"requests_per_second" default:"100"`
	BurstSize         int `json:"burst_size" default:"10"`
}

func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	return &RateLimiter{config: config}
}

func (rl *RateLimiter) Initialize(ctx context.Context) error {
	return nil
}

func (rl *RateLimiter) Stop(ctx context.Context) error {
	return nil
}

func (rl *RateLimiter) Allow(hookName string) bool {
	// Simplified implementation
	return true
}

type ExecutionCache struct {
	config CacheConfig
	cache  map[string]cacheEntry
	mu     sync.RWMutex
}

type CacheConfig struct {
	MaxSize int           `json:"max_size" default:"1000"`
	TTL     time.Duration `json:"ttl" default:"5m"`
}

type cacheEntry struct {
	Result    plugins.HookResult
	ExpiresAt time.Time
}

type CacheStats struct {
	Hits   int64 `json:"hits"`
	Misses int64 `json:"misses"`
	Size   int   `json:"size"`
}

func NewExecutionCache(config CacheConfig) *ExecutionCache {
	return &ExecutionCache{
		config: config,
		cache:  make(map[string]cacheEntry),
	}
}

func (ec *ExecutionCache) Initialize(ctx context.Context) error {
	return nil
}

func (ec *ExecutionCache) Stop(ctx context.Context) error {
	return nil
}

func (ec *ExecutionCache) Get(key string) (plugins.HookResult, bool) {
	ec.mu.RLock()
	defer ec.mu.RUnlock()

	entry, exists := ec.cache[key]
	if !exists || time.Now().After(entry.ExpiresAt) {
		return plugins.HookResult{}, false
	}

	return entry.Result, true
}

func (ec *ExecutionCache) Set(key string, result plugins.HookResult, ttl time.Duration) {
	ec.mu.Lock()
	defer ec.mu.Unlock()

	ec.cache[key] = cacheEntry{
		Result:    result,
		ExpiresAt: time.Now().Add(ttl),
	}

	// Simple cleanup - remove expired entries
	if len(ec.cache) > ec.config.MaxSize {
		for k, v := range ec.cache {
			if time.Now().After(v.ExpiresAt) {
				delete(ec.cache, k)
			}
		}
	}
}

// Condition implementations

type PriorityCondition struct {
	MinPriority int
}

func (pc *PriorityCondition) ShouldExecute(ctx context.Context, hook plugins.Hook, data plugins.HookData) bool {
	return hook.Priority() >= pc.MinPriority
}

func (pc *PriorityCondition) Name() string {
	return "priority"
}

func (pc *PriorityCondition) Priority() int {
	return 1
}

type TypeCondition struct {
	AllowedTypes []plugins.HookType
}

func (tc *TypeCondition) ShouldExecute(ctx context.Context, hook plugins.Hook, data plugins.HookData) bool {
	for _, allowedType := range tc.AllowedTypes {
		if hook.Type() == allowedType {
			return true
		}
	}
	return false
}

func (tc *TypeCondition) Name() string {
	return "type"
}

func (tc *TypeCondition) Priority() int {
	return 1
}

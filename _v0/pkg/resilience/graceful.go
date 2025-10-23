package resilience

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// GracefulDegradation provides graceful degradation functionality
type GracefulDegradation struct {
	config    DegradationConfig
	fallbacks map[string]FallbackHandler
	mu        sync.RWMutex
	logger    common.Logger
	metrics   common.Metrics
	stats     DegradationStats
}

// DegradationConfig contains graceful degradation configuration
type DegradationConfig struct {
	Name                   string         `yaml:"name"`
	EnableFallbacks        bool           `yaml:"enable_fallbacks" default:"true"`
	FallbackTimeout        time.Duration  `yaml:"fallback_timeout" default:"5s"`
	EnableCircuitBreaker   bool           `yaml:"enable_circuit_breaker" default:"true"`
	EnableRetry            bool           `yaml:"enable_retry" default:"true"`
	MaxConcurrentFallbacks int            `yaml:"max_concurrent_fallbacks" default:"10"`
	EnableMetrics          bool           `yaml:"enable_metrics" default:"true"`
	Logger                 common.Logger  `yaml:"-"`
	Metrics                common.Metrics `yaml:"-"`
}

// DegradationStats contains degradation statistics
type GracefulDegradationStats struct {
	TotalRequests      int64
	SuccessfulRequests int64
	FailedRequests     int64
	FallbackRequests   int64
	LastRequest        time.Time
}

// FallbackHandler represents a fallback handler
type FallbackHandler interface {
	Execute(ctx context.Context, request interface{}) (interface{}, error)
	GetName() string
	GetPriority() int
	IsAvailable(ctx context.Context) bool
}

// DegradationResult represents the result of graceful degradation
type DegradationResult struct {
	Success      bool          `json:"success"`
	Result       interface{}   `json:"result,omitempty"`
	Error        error         `json:"error,omitempty"`
	Handler      string        `json:"handler"`
	FallbackUsed bool          `json:"fallback_used"`
	Duration     time.Duration `json:"duration"`
	Attempts     int           `json:"attempts"`
}

// DegradationStats represents degradation statistics
type DegradationStats struct {
	TotalRequests      int64     `json:"total_requests"`
	SuccessfulRequests int64     `json:"successful_requests"`
	FailedRequests     int64     `json:"failed_requests"`
	FallbackRequests   int64     `json:"fallback_requests"`
	LastRequest        time.Time `json:"last_request"`
	LastSuccess        time.Time `json:"last_success"`
	LastFailure        time.Time `json:"last_failure"`
}

// NewGracefulDegradation creates a new graceful degradation instance
func NewGracefulDegradation(config DegradationConfig) *GracefulDegradation {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	return &GracefulDegradation{
		config:    config,
		fallbacks: make(map[string]FallbackHandler),
		logger:    config.Logger,
		metrics:   config.Metrics,
	}
}

// Execute executes a function with graceful degradation
func (gd *GracefulDegradation) Execute(ctx context.Context, primaryHandler FallbackHandler, request interface{}) (*DegradationResult, error) {
	start := time.Now()

	gd.mu.Lock()
	gd.stats.TotalRequests++
	gd.stats.LastRequest = time.Now()
	gd.mu.Unlock()

	// Try primary handler first
	result, err := gd.tryHandler(ctx, primaryHandler, request)
	if err == nil {
		gd.recordSuccess(primaryHandler.GetName(), time.Since(start), false)
		return &DegradationResult{
			Success:      true,
			Result:       result,
			Handler:      primaryHandler.GetName(),
			FallbackUsed: false,
			Duration:     time.Since(start),
			Attempts:     1,
		}, nil
	}

	// Primary handler failed, try fallbacks
	if gd.config.EnableFallbacks {
		fallbackResult, fallbackErr := gd.tryFallbacks(ctx, request)
		if fallbackErr == nil {
			gd.recordSuccess(fallbackResult.Handler, time.Since(start), true)
			return fallbackResult, nil
		}
	}

	// All handlers failed
	gd.recordFailure(primaryHandler.GetName(), time.Since(start))
	return &DegradationResult{
		Success:      false,
		Error:        err,
		Handler:      primaryHandler.GetName(),
		FallbackUsed: false,
		Duration:     time.Since(start),
		Attempts:     1,
	}, err
}

// tryHandler tries to execute a handler
func (gd *GracefulDegradation) tryHandler(ctx context.Context, handler FallbackHandler, request interface{}) (interface{}, error) {
	// Check if handler is available
	if !handler.IsAvailable(ctx) {
		return nil, fmt.Errorf("handler %s is not available", handler.GetName())
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, gd.config.FallbackTimeout)
	defer cancel()

	// Execute handler
	return handler.Execute(timeoutCtx, request)
}

// tryFallbacks tries to execute fallback handlers
func (gd *GracefulDegradation) tryFallbacks(ctx context.Context, request interface{}) (*DegradationResult, error) {
	gd.mu.RLock()
	handlers := make([]FallbackHandler, 0, len(gd.fallbacks))
	for _, handler := range gd.fallbacks {
		handlers = append(handlers, handler)
	}
	gd.mu.RUnlock()

	// Sort handlers by priority (higher priority first)
	sortHandlersByPriority(handlers)

	start := time.Now()
	attempts := 0

	for _, handler := range handlers {
		attempts++

		// Check if handler is available
		if !handler.IsAvailable(ctx) {
			continue
		}

		// Try to execute handler
		result, err := gd.tryHandler(ctx, handler, request)
		if err == nil {
			gd.recordFallback(handler.GetName(), time.Since(start))
			return &DegradationResult{
				Success:      true,
				Result:       result,
				Handler:      handler.GetName(),
				FallbackUsed: true,
				Duration:     time.Since(start),
				Attempts:     attempts,
			}, nil
		}
	}

	return nil, fmt.Errorf("all fallback handlers failed")
}

// sortHandlersByPriority sorts handlers by priority
func sortHandlersByPriority(handlers []FallbackHandler) {
	for i := 0; i < len(handlers); i++ {
		for j := i + 1; j < len(handlers); j++ {
			if handlers[i].GetPriority() < handlers[j].GetPriority() {
				handlers[i], handlers[j] = handlers[j], handlers[i]
			}
		}
	}
}

// recordSuccess records a successful request
func (gd *GracefulDegradation) recordSuccess(handler string, duration time.Duration, fallback bool) {
	gd.mu.Lock()
	defer gd.mu.Unlock()

	gd.stats.SuccessfulRequests++
	gd.stats.LastSuccess = time.Now()

	if fallback {
		gd.stats.FallbackRequests++
	}

	if gd.config.EnableMetrics && gd.metrics != nil {
		gd.recordMetrics(handler, duration, true, fallback)
	}

	gd.logger.Info("graceful degradation succeeded",
		logger.String("name", gd.config.Name),
		logger.String("handler", handler),
		logger.String("duration", duration.String()),
		logger.Bool("fallback", fallback))
}

// recordFailure records a failed request
func (gd *GracefulDegradation) recordFailure(handler string, duration time.Duration) {
	gd.mu.Lock()
	defer gd.mu.Unlock()

	gd.stats.FailedRequests++
	gd.stats.LastFailure = time.Now()

	if gd.config.EnableMetrics && gd.metrics != nil {
		gd.recordMetrics(handler, duration, false, false)
	}

	gd.logger.Error("graceful degradation failed",
		logger.String("name", gd.config.Name),
		logger.String("handler", handler),
		logger.String("duration", duration.String()))
}

// recordFallback records a fallback request
func (gd *GracefulDegradation) recordFallback(handler string, duration time.Duration) {
	gd.mu.Lock()
	defer gd.mu.Unlock()

	gd.stats.FallbackRequests++

	if gd.config.EnableMetrics && gd.metrics != nil {
		gd.recordMetrics(handler, duration, true, true)
	}

	gd.logger.Info("fallback handler used",
		logger.String("name", gd.config.Name),
		logger.String("handler", handler),
		logger.String("duration", duration.String()))
}

// recordMetrics records degradation metrics
func (gd *GracefulDegradation) recordMetrics(handler string, duration time.Duration, success bool, fallback bool) {
	result := "success"
	if !success {
		result = "failure"
	}

	fallbackLabel := "false"
	if fallback {
		fallbackLabel = "true"
	}

	// Record request metrics
	if gd.metrics != nil {
		counter := gd.metrics.Counter("graceful_degradation_requests_total",
			"name", gd.config.Name,
			"handler", handler,
			"result", result,
			"fallback", fallbackLabel,
		)
		counter.Inc()

		// Record duration metrics
		histogram := gd.metrics.Histogram("graceful_degradation_duration_seconds",
			"name", gd.config.Name,
			"handler", handler,
			"result", result,
			"fallback", fallbackLabel,
		)
		histogram.Observe(duration.Seconds())
	}
}

// RegisterFallback registers a fallback handler
func (gd *GracefulDegradation) RegisterFallback(handler FallbackHandler) {
	gd.mu.Lock()
	defer gd.mu.Unlock()

	gd.fallbacks[handler.GetName()] = handler
}

// UnregisterFallback unregisters a fallback handler
func (gd *GracefulDegradation) UnregisterFallback(name string) {
	gd.mu.Lock()
	defer gd.mu.Unlock()

	delete(gd.fallbacks, name)
}

// GetFallback returns a fallback handler by name
func (gd *GracefulDegradation) GetFallback(name string) (FallbackHandler, error) {
	gd.mu.RLock()
	defer gd.mu.RUnlock()

	handler, exists := gd.fallbacks[name]
	if !exists {
		return nil, fmt.Errorf("fallback handler %s not found", name)
	}

	return handler, nil
}

// ListFallbacks returns all registered fallback handlers
func (gd *GracefulDegradation) ListFallbacks() []FallbackHandler {
	gd.mu.RLock()
	defer gd.mu.RUnlock()

	handlers := make([]FallbackHandler, 0, len(gd.fallbacks))
	for _, handler := range gd.fallbacks {
		handlers = append(handlers, handler)
	}

	return handlers
}

// GetStats returns the degradation statistics
func (gd *GracefulDegradation) GetStats() DegradationStats {
	gd.mu.RLock()
	defer gd.mu.RUnlock()
	return gd.stats
}

// Reset resets the degradation statistics
func (gd *GracefulDegradation) Reset() {
	gd.mu.Lock()
	defer gd.mu.Unlock()

	gd.stats = DegradationStats{
		LastRequest: time.Now(),
	}
}

// GetConfig returns the degradation configuration
func (gd *GracefulDegradation) GetConfig() DegradationConfig {
	return gd.config
}

// UpdateConfig updates the degradation configuration
func (gd *GracefulDegradation) UpdateConfig(config DegradationConfig) {
	gd.mu.Lock()
	defer gd.mu.Unlock()

	gd.config = config
}

// Built-in fallback handlers

// CacheFallbackHandler provides cache-based fallback
type CacheFallbackHandler struct {
	name     string
	priority int
	cache    map[string]interface{}
	mu       sync.RWMutex
}

func NewCacheFallbackHandler(name string, priority int) *CacheFallbackHandler {
	return &CacheFallbackHandler{
		name:     name,
		priority: priority,
		cache:    make(map[string]interface{}),
	}
}

func (h *CacheFallbackHandler) Execute(ctx context.Context, request interface{}) (interface{}, error) {
	// Simple cache implementation
	key := fmt.Sprintf("%v", request)

	h.mu.RLock()
	result, exists := h.cache[key]
	h.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("cache miss for key %s", key)
	}

	return result, nil
}

func (h *CacheFallbackHandler) GetName() string {
	return h.name
}

func (h *CacheFallbackHandler) GetPriority() int {
	return h.priority
}

func (h *CacheFallbackHandler) IsAvailable(ctx context.Context) bool {
	return true
}

// StaticFallbackHandler provides static response fallback
type StaticFallbackHandler struct {
	name     string
	priority int
	response interface{}
}

func NewStaticFallbackHandler(name string, priority int, response interface{}) *StaticFallbackHandler {
	return &StaticFallbackHandler{
		name:     name,
		priority: priority,
		response: response,
	}
}

func (h *StaticFallbackHandler) Execute(ctx context.Context, request interface{}) (interface{}, error) {
	return h.response, nil
}

func (h *StaticFallbackHandler) GetName() string {
	return h.name
}

func (h *StaticFallbackHandler) GetPriority() int {
	return h.priority
}

func (h *StaticFallbackHandler) IsAvailable(ctx context.Context) bool {
	return true
}

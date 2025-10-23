package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// Middleware defines the interface for service-aware middleware
type Middleware = common.Middleware

// MiddlewareStats contains statistics about middleware performance
type MiddlewareStats = common.MiddlewareStats

// MiddlewareConfig contains configuration for middleware
type MiddlewareConfig struct {
	Enabled      bool                   `yaml:"enabled" json:"enabled"`
	Priority     int                    `yaml:"priority" json:"priority"`
	Config       map[string]interface{} `yaml:"config" json:"config"`
	Dependencies []string               `yaml:"dependencies" json:"dependencies"`
}

// MiddlewareContext provides context for middleware execution
type MiddlewareContext struct {
	context.Context
	Request   *http.Request
	Response  http.ResponseWriter
	Container common.Container
	Logger    common.Logger
	Metrics   common.Metrics
	Config    common.ConfigManager
	Values    map[string]interface{}
	StartTime time.Time
	UserID    string
	RequestID string
	TraceID   string
}

// NewMiddlewareContext creates a new middleware context
func NewMiddlewareContext(ctx context.Context, req *http.Request, resp http.ResponseWriter, container common.Container) *MiddlewareContext {
	logger, _ := container.Resolve((*common.Logger)(nil))
	metrics, _ := container.Resolve((*common.Metrics)(nil))
	config, _ := container.Resolve((*common.ConfigManager)(nil))

	return &MiddlewareContext{
		Context:   ctx,
		Request:   req,
		Response:  resp,
		Container: container,
		Logger:    logger.(common.Logger),
		Metrics:   metrics.(common.Metrics),
		Config:    config.(common.ConfigManager),
		Values:    make(map[string]interface{}),
		StartTime: time.Now(),
	}
}

// Set sets a value in the middleware context
func (mc *MiddlewareContext) Set(key string, value interface{}) {
	mc.Values[key] = value
}

// Get gets a value from the middleware context
func (mc *MiddlewareContext) Get(key string) interface{} {
	return mc.Values[key]
}

// GetString gets a string value from the middleware context
func (mc *MiddlewareContext) GetString(key string) string {
	if value := mc.Get(key); value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return ""
}

// GetBool gets a boolean value from the middleware context
func (mc *MiddlewareContext) GetBool(key string) bool {
	if value := mc.Get(key); value != nil {
		if b, ok := value.(bool); ok {
			return b
		}
	}
	return false
}

// SetUserID sets the user ID in the context
func (mc *MiddlewareContext) SetUserID(userID string) {
	mc.UserID = userID
	mc.Set("user_id", userID)
}

// SetRequestID sets the request ID in the context
func (mc *MiddlewareContext) SetRequestID(requestID string) {
	mc.RequestID = requestID
	mc.Set("request_id", requestID)
}

// SetTraceID sets the trace ID in the context
func (mc *MiddlewareContext) SetTraceID(traceID string) {
	mc.TraceID = traceID
	mc.Set("trace_id", traceID)
}

// BaseServiceMiddleware provides a base implementation of Middleware
type BaseServiceMiddleware struct {
	name         string
	priority     int
	dependencies []string
	container    common.Container
	logger       common.Logger
	metrics      common.Metrics
	config       common.ConfigManager
	stats        MiddlewareStats
	started      bool
}

// NewBaseServiceMiddleware creates a new base service middleware
func NewBaseServiceMiddleware(name string, priority int, dependencies []string) *BaseServiceMiddleware {
	return &BaseServiceMiddleware{
		name:         name,
		priority:     priority,
		dependencies: dependencies,
		stats: MiddlewareStats{
			Name:         name,
			Priority:     priority,
			Dependencies: dependencies,
			MinLatency:   time.Hour, // Initialize with high value
			Status:       "not_initialized",
		},
	}
}

// Name returns the middleware name
func (bsm *BaseServiceMiddleware) Name() string {
	return bsm.name
}

// Priority returns the middleware priority
func (bsm *BaseServiceMiddleware) Priority() int {
	return bsm.priority
}

// Dependencies returns the middleware dependencies
func (bsm *BaseServiceMiddleware) Dependencies() []string {
	return bsm.dependencies
}

// Initialize initializes the middleware with the DI container
func (bsm *BaseServiceMiddleware) Initialize(container common.Container) error {
	bsm.container = container

	// Resolve dependencies
	if logger, err := container.Resolve((*common.Logger)(nil)); err == nil {
		bsm.logger = logger.(common.Logger)
	}

	if metrics, err := container.Resolve((*common.Metrics)(nil)); err == nil {
		bsm.metrics = metrics.(common.Metrics)
	}

	if config, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		bsm.config = config.(common.ConfigManager)
	}

	bsm.stats.Status = "initialized"
	return nil
}

func (bsm *BaseServiceMiddleware) Logger() logger.Logger {
	return bsm.logger
}

// OnStart is called when the middleware service starts
func (bsm *BaseServiceMiddleware) OnStart(ctx context.Context) error {
	bsm.started = true
	bsm.stats.Status = "running"
	bsm.stats.AppliedAt = time.Now()
	bsm.stats.Applied = true

	if bsm.logger != nil {
		bsm.logger.Info("middleware started",
			logger.String("middleware", bsm.name),
			logger.Int("priority", bsm.priority))
	}

	return nil
}

// OnStop is called when the middleware service stops
func (bsm *BaseServiceMiddleware) Stop(ctx context.Context) error {
	bsm.started = false
	bsm.stats.Status = "stopped"

	if bsm.logger != nil {
		bsm.logger.Info("middleware stopped",
			logger.String("middleware", bsm.name))
	}

	return nil
}

// HealthCheck performs a health check on the middleware
func (bsm *BaseServiceMiddleware) HealthCheck(ctx context.Context) error {
	if !bsm.started {
		return common.ErrHealthCheckFailed(bsm.name, fmt.Errorf("middleware not started"))
	}

	// Check for high error rates
	if bsm.stats.CallCount > 0 {
		errorRate := float64(bsm.stats.ErrorCount) / float64(bsm.stats.CallCount)
		if errorRate > 0.5 { // 50% error rate threshold
			return common.ErrHealthCheckFailed(bsm.name,
				fmt.Errorf("high error rate: %.2f%%", errorRate*100))
		}
	}

	return nil
}

// GetStats returns middleware statistics
func (bsm *BaseServiceMiddleware) GetStats() MiddlewareStats {
	// Calculate average latency
	if bsm.stats.CallCount > 0 {
		bsm.stats.AverageLatency = bsm.stats.TotalLatency / time.Duration(bsm.stats.CallCount)
	}

	return bsm.stats
}

// Handler returns a default handler that must be overridden
func (bsm *BaseServiceMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			bsm.stats.CallCount++

			// Create middleware context
			middlewareCtx := NewMiddlewareContext(r.Context(), r, w, bsm.container)

			// Execute next handler
			next.ServeHTTP(w, r.WithContext(middlewareCtx))

			// Update latency statistics
			latency := time.Since(start)
			bsm.stats.TotalLatency += latency

			if latency < bsm.stats.MinLatency {
				bsm.stats.MinLatency = latency
			}
			if latency > bsm.stats.MaxLatency {
				bsm.stats.MaxLatency = latency
			}

			// Record metrics
			if bsm.metrics != nil {
				bsm.metrics.Counter("forge.middleware.calls", "middleware", bsm.name).Inc()
				bsm.metrics.Histogram("forge.middleware.duration", "middleware", bsm.name).Observe(latency.Seconds())
			}
		})
	}
}

// UpdateStats updates the middleware statistics
func (bsm *BaseServiceMiddleware) UpdateStats(callCount, errorCount int64, latency time.Duration, err error) {
	bsm.stats.CallCount += callCount
	bsm.stats.ErrorCount += errorCount
	bsm.stats.TotalLatency += latency

	if latency < bsm.stats.MinLatency {
		bsm.stats.MinLatency = latency
	}
	if latency > bsm.stats.MaxLatency {
		bsm.stats.MaxLatency = latency
	}

	if err != nil {
		bsm.stats.LastError = err.Error()
	}
}

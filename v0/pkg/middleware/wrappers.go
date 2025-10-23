package middleware

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// =============================================================================
// MIDDLEWARE WRAPPERS FOR INTERNAL USE
// =============================================================================

// middlewareEntry represents a registered middleware with instrumentation
type middlewareEntry struct {
	middleware common.NamedMiddleware
	stats      *middlewareStats
	applied    bool
	appliedAt  time.Time
}

// middlewareStats provides thread-safe statistics collection
type middlewareStats struct {
	name         string
	priority     int
	callCount    int64
	errorCount   int64
	totalLatency int64 // in nanoseconds
	minLatency   int64 // in nanoseconds
	maxLatency   int64 // in nanoseconds
	lastError    string
	dependencies []string
	status       string
	applied      bool
	appliedAt    time.Time
}

func newMiddlewareStats(name string, priority int) *middlewareStats {
	return &middlewareStats{
		name:       name,
		priority:   priority,
		minLatency: int64(time.Hour), // Initialize with high value
		status:     "registered",
	}
}

func (ms *middlewareStats) updateCall() {
	atomic.AddInt64(&ms.callCount, 1)
}

func (ms *middlewareStats) updateError(err error) {
	atomic.AddInt64(&ms.errorCount, 1)
	if err != nil {
		ms.lastError = err.Error()
	}
}

func (ms *middlewareStats) updateLatency(latency time.Duration) {
	latencyNs := latency.Nanoseconds()
	atomic.AddInt64(&ms.totalLatency, latencyNs)

	// Update min latency (using compare-and-swap loop)
	for {
		current := atomic.LoadInt64(&ms.minLatency)
		if latencyNs >= current || atomic.CompareAndSwapInt64(&ms.minLatency, current, latencyNs) {
			break
		}
	}

	// Update max latency (using compare-and-swap loop)
	for {
		current := atomic.LoadInt64(&ms.maxLatency)
		if latencyNs <= current || atomic.CompareAndSwapInt64(&ms.maxLatency, current, latencyNs) {
			break
		}
	}
}

func (ms *middlewareStats) getStats() common.MiddlewareStats {
	callCount := atomic.LoadInt64(&ms.callCount)
	errorCount := atomic.LoadInt64(&ms.errorCount)
	totalLatency := atomic.LoadInt64(&ms.totalLatency)
	minLatency := atomic.LoadInt64(&ms.minLatency)
	maxLatency := atomic.LoadInt64(&ms.maxLatency)

	var averageLatency time.Duration
	if callCount > 0 {
		averageLatency = time.Duration(totalLatency / callCount)
	}

	return common.MiddlewareStats{
		Name:           ms.name,
		Priority:       ms.priority,
		Applied:        ms.applied,
		AppliedAt:      ms.appliedAt,
		CallCount:      callCount,
		ErrorCount:     errorCount,
		TotalLatency:   time.Duration(totalLatency),
		AverageLatency: averageLatency,
		MinLatency:     time.Duration(minLatency),
		MaxLatency:     time.Duration(maxLatency),
		LastError:      ms.lastError,
		Dependencies:   ms.dependencies,
		Status:         ms.status,
	}
}

// =============================================================================
// FUNCTION MIDDLEWARE WRAPPER
// =============================================================================

// functionMiddleware wraps a function middleware to implement NamedMiddleware
type functionMiddleware struct {
	name     string
	priority int
	handler  common.Middleware
}

func (fm *functionMiddleware) Name() string                                  { return fm.name }
func (fm *functionMiddleware) Priority() int                                 { return fm.priority }
func (fm *functionMiddleware) Handler() func(next http.Handler) http.Handler { return fm.handler }

// NewFunctionMiddleware wraps a function middleware with name and priority
func NewFunctionMiddleware(name string, priority int, handler common.Middleware) common.NamedMiddleware {
	return &functionMiddleware{
		name:     name,
		priority: priority,
		handler:  handler,
	}
}

// =============================================================================
// INSTRUMENTED WRAPPER (INTERNAL USE ONLY)
// =============================================================================

// instrumentedWrapper wraps any NamedMiddleware with automatic instrumentation
type instrumentedWrapper struct {
	common.NamedMiddleware
	stats *middlewareStats
}

func newInstrumentedWrapper(middleware common.NamedMiddleware) *instrumentedWrapper {
	stats := newMiddlewareStats(middleware.Name(), middleware.Priority())

	return &instrumentedWrapper{
		NamedMiddleware: middleware,
		stats:           stats,
	}
}

func (iw *instrumentedWrapper) Handler() func(next http.Handler) http.Handler {
	originalHandler := iw.NamedMiddleware.Handler()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			iw.stats.updateCall()

			// Create response wrapper to capture errors
			wrapper := &responseWrapper{ResponseWriter: w, statusCode: http.StatusOK}

			// Handle panics
			defer func() {
				if recovered := recover(); recovered != nil {
					err := fmt.Errorf("panic in middleware %s: %v", iw.Name(), recovered)
					iw.stats.updateError(err)
					panic(recovered) // Re-panic to let recovery middleware handle it
				}
			}()

			// Call original handler
			originalHandler(next).ServeHTTP(wrapper, r)

			// Update statistics
			latency := time.Since(start)
			iw.stats.updateLatency(latency)

			// Check for HTTP errors
			if wrapper.statusCode >= 400 {
				err := fmt.Errorf("middleware %s returned error status: %d", iw.Name(), wrapper.statusCode)
				iw.stats.updateError(err)
			}
		})
	}
}

func (iw *instrumentedWrapper) GetStats() common.MiddlewareStats {
	return iw.stats.getStats()
}

// Ensure instrumentedWrapper implements InstrumentedMiddleware
var _ common.InstrumentedMiddleware = (*instrumentedWrapper)(nil)

// responseWrapper captures response status for error detection
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

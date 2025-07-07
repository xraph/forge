package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/logger"
)

// TimeoutMiddleware provides request timeout handling
type TimeoutMiddleware struct {
	*BaseMiddleware
	config TimeoutConfig
}

// TimeoutConfig represents timeout configuration
type TimeoutConfig struct {
	// Timeout duration
	Timeout time.Duration `json:"timeout"`

	// Custom timeout message
	Message string `json:"message"`

	// Status code for timeout
	StatusCode int `json:"status_code"`

	// Skip timeout for certain paths
	SkipPaths []string `json:"skip_paths"`

	// Different timeouts for different paths
	PathTimeouts map[string]time.Duration `json:"path_timeouts"`

	// Different timeouts for different methods
	MethodTimeouts map[string]time.Duration `json:"method_timeouts"`

	// Custom timeout handler
	TimeoutHandler func(http.ResponseWriter, *http.Request) `json:"-"`

	// Error callback
	OnTimeout func(*http.Request, time.Duration) `json:"-"`

	// Enable timeout logging
	LogTimeouts bool `json:"log_timeouts"`

	// Skip timeout for WebSocket upgrades
	SkipWebSocket bool `json:"skip_websocket"`

	// Skip timeout for Server-Sent Events
	SkipSSE bool `json:"skip_sse"`
}

// NewTimeoutMiddleware creates a new timeout middleware
func NewTimeoutMiddleware(config TimeoutConfig) Middleware {
	return &TimeoutMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"timeout",
			PriorityBodyLimit-10, // Before body limit
			"Request timeout middleware",
		),
		config: config,
	}
}

// DefaultTimeoutConfig returns default timeout configuration
func DefaultTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		Timeout:        30 * time.Second,
		Message:        "Request timeout",
		StatusCode:     http.StatusRequestTimeout,
		SkipPaths:      []string{"/health", "/metrics"},
		PathTimeouts:   make(map[string]time.Duration),
		MethodTimeouts: make(map[string]time.Duration),
		LogTimeouts:    true,
		SkipWebSocket:  true,
		SkipSSE:        true,
	}
}

// LongTimeoutConfig returns configuration for long-running requests
func LongTimeoutConfig() TimeoutConfig {
	config := DefaultTimeoutConfig()
	config.Timeout = 5 * time.Minute
	return config
}

// FastTimeoutConfig returns configuration for fast API endpoints
func FastTimeoutConfig() TimeoutConfig {
	config := DefaultTimeoutConfig()
	config.Timeout = 10 * time.Second
	return config
}

// Handle implements the Middleware interface
func (tm *TimeoutMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if timeout should be skipped
		if tm.shouldSkipTimeout(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Get timeout duration for this request
		timeout := tm.getTimeoutForRequest(r)
		if timeout <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		// Create context with timeout
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		// Create request with timeout context
		r = r.WithContext(ctx)

		// Channel to capture completion
		done := make(chan struct{})

		// Execute handler in goroutine
		go func() {
			defer close(done)
			next.ServeHTTP(w, r)
		}()

		// Wait for completion or timeout
		select {
		case <-done:
			// Request completed successfully
			return
		case <-ctx.Done():
			// Request timed out
			tm.handleTimeout(w, r, timeout)
			return
		}
	})
}

// shouldSkipTimeout checks if timeout should be skipped for this request
func (tm *TimeoutMiddleware) shouldSkipTimeout(r *http.Request) bool {
	// Check skip paths
	for _, skipPath := range tm.config.SkipPaths {
		if r.URL.Path == skipPath || (len(skipPath) > 0 && skipPath[len(skipPath)-1] == '*' &&
			len(r.URL.Path) >= len(skipPath)-1 && r.URL.Path[:len(skipPath)-1] == skipPath[:len(skipPath)-1]) {
			return true
		}
	}

	// Check for WebSocket upgrade
	if tm.config.SkipWebSocket && tm.isWebSocketUpgrade(r) {
		return true
	}

	// Check for Server-Sent Events
	if tm.config.SkipSSE && tm.isSSERequest(r) {
		return true
	}

	return false
}

// getTimeoutForRequest gets the timeout duration for a specific request
func (tm *TimeoutMiddleware) getTimeoutForRequest(r *http.Request) time.Duration {
	// Check method-specific timeouts
	if methodTimeout, exists := tm.config.MethodTimeouts[r.Method]; exists {
		return methodTimeout
	}

	// Check path-specific timeouts
	for path, pathTimeout := range tm.config.PathTimeouts {
		if r.URL.Path == path || (len(path) > 0 && path[len(path)-1] == '*' &&
			len(r.URL.Path) >= len(path)-1 && r.URL.Path[:len(path)-1] == path[:len(path)-1]) {
			return pathTimeout
		}
	}

	// Return default timeout
	return tm.config.Timeout
}

// isWebSocketUpgrade checks if the request is a WebSocket upgrade
func (tm *TimeoutMiddleware) isWebSocketUpgrade(r *http.Request) bool {
	return r.Header.Get("Upgrade") == "websocket"
}

// isSSERequest checks if the request is for Server-Sent Events
func (tm *TimeoutMiddleware) isSSERequest(r *http.Request) bool {
	return r.Header.Get("Accept") == "text/event-stream"
}

// handleTimeout handles request timeout
func (tm *TimeoutMiddleware) handleTimeout(w http.ResponseWriter, r *http.Request, timeout time.Duration) {
	// Log timeout if enabled
	if tm.config.LogTimeouts {
		tm.logger.Warn("Request timed out",
			logger.String("method", r.Method),
			logger.String("path", r.URL.Path),
			logger.Duration("timeout", timeout),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}

	// Call timeout callback if provided
	if tm.config.OnTimeout != nil {
		tm.config.OnTimeout(r, timeout)
	}

	// Use custom timeout handler if provided
	if tm.config.TimeoutHandler != nil {
		tm.config.TimeoutHandler(w, r)
		return
	}

	// Default timeout response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(tm.config.StatusCode)

	response := fmt.Sprintf(`{
		"error": "%s",
		"message": "Request took too long to complete",
		"timeout": "%s",
		"code": "REQUEST_TIMEOUT"
	}`, tm.config.Message, timeout.String())

	w.Write([]byte(response))
}

// Configure implements the Middleware interface
func (tm *TimeoutMiddleware) Configure(config map[string]interface{}) error {
	if timeout, ok := config["timeout"].(time.Duration); ok {
		tm.config.Timeout = timeout
	}
	if timeoutStr, ok := config["timeout"].(string); ok {
		if timeout, err := time.ParseDuration(timeoutStr); err == nil {
			tm.config.Timeout = timeout
		}
	}
	if message, ok := config["message"].(string); ok {
		tm.config.Message = message
	}
	if statusCode, ok := config["status_code"].(int); ok {
		tm.config.StatusCode = statusCode
	}
	if skipPaths, ok := config["skip_paths"].([]string); ok {
		tm.config.SkipPaths = skipPaths
	}
	if logTimeouts, ok := config["log_timeouts"].(bool); ok {
		tm.config.LogTimeouts = logTimeouts
	}
	if skipWebSocket, ok := config["skip_websocket"].(bool); ok {
		tm.config.SkipWebSocket = skipWebSocket
	}
	if skipSSE, ok := config["skip_sse"].(bool); ok {
		tm.config.SkipSSE = skipSSE
	}

	return nil
}

// Health implements the Middleware interface
func (tm *TimeoutMiddleware) Health(ctx context.Context) error {
	if tm.config.Timeout <= 0 {
		return fmt.Errorf("timeout duration must be positive")
	}
	return nil
}

// Utility functions

// TimeoutMiddlewareFunc creates a simple timeout middleware function
func TimeoutMiddlewareFunc(timeout time.Duration) func(http.Handler) http.Handler {
	config := DefaultTimeoutConfig()
	config.Timeout = timeout
	middleware := NewTimeoutMiddleware(config)
	return middleware.Handle
}

// WithTimeout creates timeout middleware with specified duration
func WithTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return TimeoutMiddlewareFunc(timeout)
}

// WithTimeoutConfig creates timeout middleware with custom configuration
func WithTimeoutConfig(config TimeoutConfig) func(http.Handler) http.Handler {
	middleware := NewTimeoutMiddleware(config)
	return middleware.Handle
}

// WithAPITimeout creates timeout middleware optimized for API endpoints
func WithAPITimeout(timeout time.Duration) func(http.Handler) http.Handler {
	config := DefaultTimeoutConfig()
	config.Timeout = timeout
	config.SkipPaths = []string{"/health", "/metrics", "/debug"}
	config.MethodTimeouts = map[string]time.Duration{
		"GET":    timeout,
		"POST":   timeout * 2,
		"PUT":    timeout * 2,
		"PATCH":  timeout * 2,
		"DELETE": timeout,
	}
	return WithTimeoutConfig(config)
}

// WithUploadTimeout creates timeout middleware optimized for file uploads
func WithUploadTimeout() func(http.Handler) http.Handler {
	config := DefaultTimeoutConfig()
	config.Timeout = 10 * time.Minute
	config.MethodTimeouts = map[string]time.Duration{
		"POST": 15 * time.Minute,
		"PUT":  15 * time.Minute,
	}
	return WithTimeoutConfig(config)
}

// WithStreamingTimeout creates timeout middleware for streaming endpoints
func WithStreamingTimeout() func(http.Handler) http.Handler {
	config := DefaultTimeoutConfig()
	config.Timeout = 1 * time.Hour
	config.SkipWebSocket = true
	config.SkipSSE = true
	config.SkipPaths = []string{"/stream", "/events", "/ws"}
	return WithTimeoutConfig(config)
}

// Timeout testing utilities

// TestTimeoutMiddleware provides utilities for testing timeouts
type TestTimeoutMiddleware struct {
	*TimeoutMiddleware
}

// NewTestTimeoutMiddleware creates a test timeout middleware
func NewTestTimeoutMiddleware(timeout time.Duration) *TestTimeoutMiddleware {
	config := DefaultTimeoutConfig()
	config.Timeout = timeout
	config.LogTimeouts = false // Disable logging in tests
	return &TestTimeoutMiddleware{
		TimeoutMiddleware: NewTimeoutMiddleware(config).(*TimeoutMiddleware),
	}
}

// TestTimeout tests timeout behavior with a slow handler
func (ttm *TestTimeoutMiddleware) TestTimeout(delay time.Duration) (bool, error) {
	// Create test handler that sleeps for specified duration
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with timeout middleware
	timeoutHandler := ttm.Handle(handler)

	// Test with timeout
	req, err := http.NewRequest("GET", "/test", nil)
	if err != nil {
		return false, err
	}

	// Use a custom response writer that captures timeout
	recorder := &timeoutRecorder{
		timedOut: false,
		status:   0,
	}

	// Execute handler
	start := time.Now()
	timeoutHandler.ServeHTTP(recorder, req)
	duration := time.Since(start)
	fmt.Printf("Request took %s\n", duration)

	// Check if timeout occurred
	expectedTimeout := delay > ttm.config.Timeout
	actualTimeout := recorder.timedOut || recorder.status == http.StatusRequestTimeout

	return expectedTimeout == actualTimeout, nil
}

// timeoutRecorder records timeout events
type timeoutRecorder struct {
	timedOut bool
	status   int
}

func (tr *timeoutRecorder) Header() http.Header {
	return make(http.Header)
}

func (tr *timeoutRecorder) Write([]byte) (int, error) {
	return 0, nil
}

func (tr *timeoutRecorder) WriteHeader(statusCode int) {
	tr.status = statusCode
	if statusCode == http.StatusRequestTimeout {
		tr.timedOut = true
	}
}

// Context timeout helpers

// WithRequestTimeout adds timeout to request context
func WithRequestTimeout(r *http.Request, timeout time.Duration) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	return r.WithContext(ctx), cancel
}

// WithDeadline adds deadline to request context
func WithDeadline(r *http.Request, deadline time.Time) (*http.Request, context.CancelFunc) {
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	return r.WithContext(ctx), cancel
}

// GetRemainingTimeout gets remaining timeout from request context
func GetRemainingTimeout(r *http.Request) time.Duration {
	if deadline, ok := r.Context().Deadline(); ok {
		return time.Until(deadline)
	}
	return 0
}

// IsTimedOut checks if request context has timed out
func IsTimedOut(r *http.Request) bool {
	select {
	case <-r.Context().Done():
		return r.Context().Err() == context.DeadlineExceeded
	default:
		return false
	}
}

// Timeout monitoring utilities

// TimeoutMonitor monitors timeout events
type TimeoutMonitor struct {
	timeouts map[string]int
	logger   logger.Logger
}

// NewTimeoutMonitor creates a new timeout monitor
func NewTimeoutMonitor(logger logger.Logger) *TimeoutMonitor {
	return &TimeoutMonitor{
		timeouts: make(map[string]int),
		logger:   logger,
	}
}

// RecordTimeout records a timeout event
func (tm *TimeoutMonitor) RecordTimeout(path string) {
	tm.timeouts[path]++
	tm.logger.Warn("Timeout recorded", logger.String("path", path), logger.Int("count", tm.timeouts[path]))
}

// GetTimeouts returns timeout counts by path
func (tm *TimeoutMonitor) GetTimeouts() map[string]int {
	result := make(map[string]int)
	for path, count := range tm.timeouts {
		result[path] = count
	}
	return result
}

// Reset resets timeout counts
func (tm *TimeoutMonitor) Reset() {
	tm.timeouts = make(map[string]int)
}

// Timeout analysis utilities

// AnalyzeTimeouts analyzes timeout patterns
func AnalyzeTimeouts(timeouts map[string]int) TimeoutAnalysis {
	analysis := TimeoutAnalysis{
		TotalTimeouts: 0,
		PathTimeouts:  make(map[string]int),
		TopPaths:      make([]PathTimeout, 0),
	}

	for path, count := range timeouts {
		analysis.TotalTimeouts += count
		analysis.PathTimeouts[path] = count
		analysis.TopPaths = append(analysis.TopPaths, PathTimeout{
			Path:  path,
			Count: count,
		})
	}

	// Sort by count descending
	for i := 0; i < len(analysis.TopPaths)-1; i++ {
		for j := i + 1; j < len(analysis.TopPaths); j++ {
			if analysis.TopPaths[j].Count > analysis.TopPaths[i].Count {
				analysis.TopPaths[i], analysis.TopPaths[j] = analysis.TopPaths[j], analysis.TopPaths[i]
			}
		}
	}

	return analysis
}

// TimeoutAnalysis represents timeout analysis results
type TimeoutAnalysis struct {
	TotalTimeouts int            `json:"total_timeouts"`
	PathTimeouts  map[string]int `json:"path_timeouts"`
	TopPaths      []PathTimeout  `json:"top_paths"`
}

// PathTimeout represents timeout count for a specific path
type PathTimeout struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// Adaptive timeout utilities

// AdaptiveTimeoutConfig provides adaptive timeout configuration
type AdaptiveTimeoutConfig struct {
	BaseTimeout    time.Duration
	MaxTimeout     time.Duration
	MinTimeout     time.Duration
	IncreaseFactor float64
	DecreaseFactor float64
	WindowSize     int
	pathStats      map[string]*PathStats
}

// PathStats tracks statistics for a specific path
type PathStats struct {
	TotalRequests int
	Timeouts      int
	AvgDuration   time.Duration
	LastTimeout   time.Time
}

// NewAdaptiveTimeoutConfig creates adaptive timeout configuration
func NewAdaptiveTimeoutConfig(baseTimeout time.Duration) *AdaptiveTimeoutConfig {
	return &AdaptiveTimeoutConfig{
		BaseTimeout:    baseTimeout,
		MaxTimeout:     baseTimeout * 5,
		MinTimeout:     baseTimeout / 2,
		IncreaseFactor: 1.5,
		DecreaseFactor: 0.9,
		WindowSize:     100,
		pathStats:      make(map[string]*PathStats),
	}
}

// GetTimeoutForPath gets adaptive timeout for a specific path
func (atc *AdaptiveTimeoutConfig) GetTimeoutForPath(path string) time.Duration {
	stats, exists := atc.pathStats[path]
	if !exists {
		return atc.BaseTimeout
	}

	// Calculate timeout rate
	timeoutRate := float64(stats.Timeouts) / float64(stats.TotalRequests)

	// Adjust timeout based on rate
	timeout := atc.BaseTimeout
	if timeoutRate > 0.1 { // More than 10% timeout rate
		timeout = time.Duration(float64(timeout) * atc.IncreaseFactor)
	} else if timeoutRate < 0.01 { // Less than 1% timeout rate
		timeout = time.Duration(float64(timeout) * atc.DecreaseFactor)
	}

	// Clamp to limits
	if timeout > atc.MaxTimeout {
		timeout = atc.MaxTimeout
	}
	if timeout < atc.MinTimeout {
		timeout = atc.MinTimeout
	}

	return timeout
}

// RecordRequest records a request for adaptive timeout calculation
func (atc *AdaptiveTimeoutConfig) RecordRequest(path string, duration time.Duration, timedOut bool) {
	stats, exists := atc.pathStats[path]
	if !exists {
		stats = &PathStats{}
		atc.pathStats[path] = stats
	}

	stats.TotalRequests++
	if timedOut {
		stats.Timeouts++
		stats.LastTimeout = time.Now()
	}

	// Update average duration
	stats.AvgDuration = time.Duration(
		(int64(stats.AvgDuration)*int64(stats.TotalRequests-1) + int64(duration)) / int64(stats.TotalRequests),
	)

	// Reset stats if window size exceeded
	if stats.TotalRequests > atc.WindowSize {
		stats.TotalRequests = atc.WindowSize
		stats.Timeouts = stats.Timeouts * atc.WindowSize / stats.TotalRequests
	}
}

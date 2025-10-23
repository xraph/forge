package hooks

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	plugins "github.com/xraph/forge/v0/pkg/pluginengine/common"
)

// HookMiddleware integrates hooks into the HTTP middleware pipeline
type HookMiddleware interface {
	Initialize(ctx context.Context, hookSystem HookSystem) error
	CreateMiddleware() func(http.Handler) http.Handler
	ExecutePreRequestHooks(ctx context.Context, data *RequestHookData) (*RequestHookData, error)
	ExecutePostRequestHooks(ctx context.Context, data *ResponseHookData) (*ResponseHookData, error)
	ExecuteErrorHooks(ctx context.Context, data *ErrorHookData) (*ErrorHookData, error)
	GetStats() HookMiddlewareStats
}

// HookMiddlewareImpl implements the HookMiddleware interface
type HookMiddlewareImpl struct {
	hookSystem  HookSystem
	config      HookMiddlewareConfig
	stats       HookMiddlewareStats
	logger      common.Logger
	metrics     common.Metrics
	mu          sync.RWMutex
	initialized bool
}

// HookMiddlewareConfig contains configuration for hook middleware
type HookMiddlewareConfig struct {
	EnablePreRequestHooks  bool          `json:"enable_pre_request_hooks" default:"true"`
	EnablePostRequestHooks bool          `json:"enable_post_request_hooks" default:"true"`
	EnableErrorHooks       bool          `json:"enable_error_hooks" default:"true"`
	EnableMetrics          bool          `json:"enable_metrics" default:"true"`
	Timeout                time.Duration `json:"timeout" default:"10s"`
	MaxConcurrentHooks     int           `json:"max_concurrent_hooks" default:"5"`
	SkipOnError            bool          `json:"skip_on_error" default:"false"`
	AsyncExecution         bool          `json:"async_execution" default:"false"`
}

// HookMiddlewareStats contains middleware statistics
type HookMiddlewareStats struct {
	TotalRequests        int64                              `json:"total_requests"`
	PreRequestHookCalls  int64                              `json:"pre_request_hook_calls"`
	PostRequestHookCalls int64                              `json:"post_request_hook_calls"`
	ErrorHookCalls       int64                              `json:"error_hook_calls"`
	AverageLatency       time.Duration                      `json:"average_latency"`
	ErrorCount           int64                              `json:"error_count"`
	HookLatencyByType    map[plugins.HookType]time.Duration `json:"hook_latency_by_type"`
	LastExecution        time.Time                          `json:"last_execution"`
}

// RequestHookData contains data passed to request hooks
type RequestHookData struct {
	Request   *http.Request          `json:"-"`
	Method    string                 `json:"method"`
	Path      string                 `json:"path"`
	Headers   map[string][]string    `json:"headers"`
	Query     map[string][]string    `json:"query"`
	Body      []byte                 `json:"body,omitempty"`
	ClientIP  string                 `json:"client_ip"`
	UserAgent string                 `json:"user_agent"`
	Timestamp time.Time              `json:"timestamp"`
	RequestID string                 `json:"request_id"`
	UserID    string                 `json:"user_id,omitempty"`
	Metadata  map[string]interface{} `json:"metadata"`
	Context   context.Context        `json:"-"`
	Modified  bool                   `json:"modified"`
}

// ResponseHookData contains data passed to response hooks
type ResponseHookData struct {
	Request    *http.Request          `json:"-"`
	Response   *ResponseData          `json:"response"`
	StatusCode int                    `json:"status_code"`
	Headers    map[string][]string    `json:"headers"`
	Body       []byte                 `json:"body,omitempty"`
	Duration   time.Duration          `json:"duration"`
	Timestamp  time.Time              `json:"timestamp"`
	RequestID  string                 `json:"request_id"`
	UserID     string                 `json:"user_id,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
	Context    context.Context        `json:"-"`
	Modified   bool                   `json:"modified"`
}

// ErrorHookData contains data passed to error hooks
type ErrorHookData struct {
	Request    *http.Request          `json:"-"`
	Error      error                  `json:"-"`
	ErrorMsg   string                 `json:"error_message"`
	ErrorCode  string                 `json:"error_code"`
	StatusCode int                    `json:"status_code"`
	Stack      string                 `json:"stack"`
	Timestamp  time.Time              `json:"timestamp"`
	RequestID  string                 `json:"request_id"`
	UserID     string                 `json:"user_id,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
	Context    context.Context        `json:"-"`
	Handled    bool                   `json:"handled"`
}

// ResponseData represents HTTP response data
type ResponseData struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers"`
	Body       []byte              `json:"body"`
	Size       int64               `json:"size"`
}

// NewHookMiddleware creates a new hook middleware
func NewHookMiddleware(logger common.Logger, metrics common.Metrics) HookMiddleware {
	return &HookMiddlewareImpl{
		config: HookMiddlewareConfig{
			EnablePreRequestHooks:  true,
			EnablePostRequestHooks: true,
			EnableErrorHooks:       true,
			EnableMetrics:          true,
			Timeout:                10 * time.Second,
			MaxConcurrentHooks:     5,
			SkipOnError:            false,
			AsyncExecution:         false,
		},
		stats: HookMiddlewareStats{
			HookLatencyByType: make(map[plugins.HookType]time.Duration),
		},
		logger:  logger,
		metrics: metrics,
	}
}

// Initialize initializes the hook middleware
func (hm *HookMiddlewareImpl) Initialize(ctx context.Context, hookSystem HookSystem) error {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	if hm.initialized {
		return nil
	}

	hm.hookSystem = hookSystem
	hm.initialized = true

	if hm.logger != nil {
		hm.logger.Info("hook middleware initialized")
	}

	if hm.metrics != nil {
		hm.metrics.Counter("forge.hooks.middleware_initialized").Inc()
	}

	return nil
}

// CreateMiddleware creates the HTTP middleware function
func (hm *HookMiddlewareImpl) CreateMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()

			// Update request count
			hm.updateRequestCount()

			// Generate request ID if not present
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = hm.generateRequestID()
				r.Header.Set("X-Request-ID", requestID)
			}

			// Extract user ID from context if available
			userID := hm.extractUserID(r)

			// Create response writer wrapper to capture response data
			rww := &responseWriterWrapper{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				headers:        make(map[string][]string),
				body:           make([]byte, 0),
			}

			// Execute pre-request hooks
			if hm.config.EnablePreRequestHooks {
				requestData := &RequestHookData{
					Request:   r,
					Method:    r.Method,
					Path:      r.URL.Path,
					Headers:   convertHeaders(r.Header),
					Query:     convertQuery(r.URL.Query()),
					ClientIP:  hm.extractClientIP(r),
					UserAgent: r.UserAgent(),
					Timestamp: time.Now(),
					RequestID: requestID,
					UserID:    userID,
					Metadata:  make(map[string]interface{}),
					Context:   r.Context(),
				}

				// Read body if present
				if r.Body != nil {
					body, err := hm.readBody(r)
					if err == nil {
						requestData.Body = body
					}
				}

				modifiedData, err := hm.ExecutePreRequestHooks(r.Context(), requestData)
				if err != nil {
					hm.handleHookError(rww, r, err, "pre_request")
					return
				}

				// Apply modifications if any
				if modifiedData != nil && modifiedData.Modified {
					r = hm.applyRequestModifications(r, modifiedData)
				}
			}

			// Execute the next handler with error recovery
			defer func() {
				if recovered := recover(); recovered != nil {
					err := hm.convertPanicToError(recovered)
					if hm.config.EnableErrorHooks {
						errorData := &ErrorHookData{
							Request:    r,
							Error:      err,
							ErrorMsg:   err.Error(),
							ErrorCode:  "PANIC",
							StatusCode: http.StatusInternalServerError,
							Stack:      hm.captureStack(),
							Timestamp:  time.Now(),
							RequestID:  requestID,
							UserID:     userID,
							Metadata:   make(map[string]interface{}),
							Context:    r.Context(),
						}

						if modifiedErrorData, hookErr := hm.ExecuteErrorHooks(r.Context(), errorData); hookErr == nil && modifiedErrorData != nil && modifiedErrorData.Handled {
							// Error was handled by hooks
							return
						}
					}

					// Default panic handling
					rww.WriteHeader(http.StatusInternalServerError)
					rww.Write([]byte("Internal Server Error"))
				}
			}()

			// Execute next handler
			next.ServeHTTP(rww, r)

			// Execute post-request hooks
			if hm.config.EnablePostRequestHooks {
				responseData := &ResponseHookData{
					Request: r,
					Response: &ResponseData{
						StatusCode: rww.statusCode,
						Headers:    rww.headers,
						Body:       rww.body,
						Size:       int64(len(rww.body)),
					},
					StatusCode: rww.statusCode,
					Headers:    rww.headers,
					Body:       rww.body,
					Duration:   time.Since(startTime),
					Timestamp:  time.Now(),
					RequestID:  requestID,
					UserID:     userID,
					Metadata:   make(map[string]interface{}),
					Context:    r.Context(),
				}

				modifiedData, err := hm.ExecutePostRequestHooks(r.Context(), responseData)
				if err != nil {
					if hm.logger != nil {
						hm.logger.Error("post-request hook execution failed",
							logger.String("request_id", requestID),
							logger.Error(err),
						)
					}
				}

				// Apply response modifications if any
				if modifiedData != nil && modifiedData.Modified {
					hm.applyResponseModifications(rww, modifiedData)
				}
			}

			// Update statistics
			hm.updateLatencyStats(time.Since(startTime))
		})
	}
}

// ExecutePreRequestHooks executes pre-request hooks
func (hm *HookMiddlewareImpl) ExecutePreRequestHooks(ctx context.Context, data *RequestHookData) (*RequestHookData, error) {
	hm.mu.RLock()
	if !hm.initialized || hm.hookSystem == nil {
		hm.mu.RUnlock()
		return data, nil
	}
	hm.mu.RUnlock()

	startTime := time.Now()

	// Create hook data
	hookData := plugins.HookData{
		Type:      plugins.HookTypePreRequest,
		Context:   ctx,
		Data:      data,
		Metadata:  data.Metadata,
		Timestamp: time.Now(),
	}

	// Execute hooks
	var results []plugins.HookResult
	var err error

	if hm.config.AsyncExecution {
		results, err = hm.hookSystem.ExecuteHooksConcurrent(ctx, plugins.HookTypePreRequest, hookData)
	} else {
		results, err = hm.hookSystem.ExecuteHooksSequential(ctx, plugins.HookTypePreRequest, hookData)
	}

	if err != nil {
		hm.updateErrorStats("pre_request")
		return data, err
	}

	// Process results and apply modifications
	modifiedData := data
	for _, result := range results {
		if result.Data != nil {
			if requestData, ok := result.Data.(*RequestHookData); ok {
				modifiedData = requestData
				modifiedData.Modified = true
			}
		}

		if !result.Continue && result.Error != nil {
			if !hm.config.SkipOnError {
				return modifiedData, result.Error
			}
		}
	}

	// Update statistics
	hm.updateHookLatencyStats(plugins.HookTypePreRequest, time.Since(startTime))
	hm.stats.PreRequestHookCalls++

	return modifiedData, nil
}

// ExecutePostRequestHooks executes post-request hooks
func (hm *HookMiddlewareImpl) ExecutePostRequestHooks(ctx context.Context, data *ResponseHookData) (*ResponseHookData, error) {
	hm.mu.RLock()
	if !hm.initialized || hm.hookSystem == nil {
		hm.mu.RUnlock()
		return data, nil
	}
	hm.mu.RUnlock()

	startTime := time.Now()

	// Create hook data
	hookData := plugins.HookData{
		Type:      plugins.HookTypePostRequest,
		Context:   ctx,
		Data:      data,
		Metadata:  data.Metadata,
		Timestamp: time.Now(),
	}

	// Execute hooks
	var results []plugins.HookResult
	var err error

	if hm.config.AsyncExecution {
		results, err = hm.hookSystem.ExecuteHooksConcurrent(ctx, plugins.HookTypePostRequest, hookData)
	} else {
		results, err = hm.hookSystem.ExecuteHooksSequential(ctx, plugins.HookTypePostRequest, hookData)
	}

	if err != nil {
		hm.updateErrorStats("post_request")
		return data, err
	}

	// Process results and apply modifications
	modifiedData := data
	for _, result := range results {
		if result.Data != nil {
			if responseData, ok := result.Data.(*ResponseHookData); ok {
				modifiedData = responseData
				modifiedData.Modified = true
			}
		}

		if !result.Continue && result.Error != nil {
			if !hm.config.SkipOnError {
				return modifiedData, result.Error
			}
		}
	}

	// Update statistics
	hm.updateHookLatencyStats(plugins.HookTypePostRequest, time.Since(startTime))
	hm.stats.PostRequestHookCalls++

	return modifiedData, nil
}

// ExecuteErrorHooks executes error handling hooks
func (hm *HookMiddlewareImpl) ExecuteErrorHooks(ctx context.Context, data *ErrorHookData) (*ErrorHookData, error) {
	hm.mu.RLock()
	if !hm.initialized || hm.hookSystem == nil {
		hm.mu.RUnlock()
		return data, nil
	}
	hm.mu.RUnlock()

	startTime := time.Now()

	// Create hook data
	hookData := plugins.HookData{
		Type:      plugins.HookTypeError,
		Context:   ctx,
		Data:      data,
		Metadata:  data.Metadata,
		Timestamp: time.Now(),
	}

	// Execute hooks
	results, err := hm.hookSystem.ExecuteHooksSequential(ctx, plugins.HookTypeError, hookData)
	if err != nil {
		hm.updateErrorStats("error")
		return data, err
	}

	// Process results and check if error was handled
	modifiedData := data
	for _, result := range results {
		if result.Data != nil {
			if errorData, ok := result.Data.(*ErrorHookData); ok {
				modifiedData = errorData
				if errorData.Handled {
					modifiedData.Handled = true
				}
			}
		}
	}

	// Update statistics
	hm.updateHookLatencyStats(plugins.HookTypeError, time.Since(startTime))
	hm.stats.ErrorHookCalls++

	return modifiedData, nil
}

// GetStats returns middleware statistics
func (hm *HookMiddlewareImpl) GetStats() HookMiddlewareStats {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	return hm.stats
}

// Helper methods

func (hm *HookMiddlewareImpl) updateRequestCount() {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.stats.TotalRequests++
	hm.stats.LastExecution = time.Now()

	if hm.metrics != nil {
		hm.metrics.Counter("forge.hooks.middleware.requests_total").Inc()
	}
}

func (hm *HookMiddlewareImpl) updateLatencyStats(duration time.Duration) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	// Update average latency
	if hm.stats.TotalRequests > 0 {
		totalLatency := time.Duration(hm.stats.TotalRequests-1) * hm.stats.AverageLatency
		hm.stats.AverageLatency = (totalLatency + duration) / time.Duration(hm.stats.TotalRequests)
	}

	if hm.metrics != nil {
		hm.metrics.Histogram("forge.hooks.middleware.request_duration").Observe(duration.Seconds())
	}
}

func (hm *HookMiddlewareImpl) updateHookLatencyStats(hookType plugins.HookType, duration time.Duration) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.stats.HookLatencyByType[hookType] = duration

	if hm.metrics != nil {
		hm.metrics.Histogram("forge.hooks.middleware.hook_duration",
			"type", string(hookType)).Observe(duration.Seconds())
	}
}

func (hm *HookMiddlewareImpl) updateErrorStats(hookType string) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	hm.stats.ErrorCount++

	if hm.metrics != nil {
		hm.metrics.Counter("forge.hooks.middleware.errors",
			"hook_type", hookType).Inc()
	}
}

func (hm *HookMiddlewareImpl) generateRequestID() string {
	return fmt.Sprintf("req-%d", time.Now().UnixNano())
}

func (hm *HookMiddlewareImpl) extractUserID(r *http.Request) string {
	// Try to extract user ID from various sources
	if userID := r.Header.Get("X-User-ID"); userID != "" {
		return userID
	}

	if ctx := r.Context(); ctx != nil {
		if userID := ctx.Value("user_id"); userID != nil {
			if uid, ok := userID.(string); ok {
				return uid
			}
		}
	}

	return ""
}

func (hm *HookMiddlewareImpl) extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fallback to RemoteAddr
	ip := r.RemoteAddr
	if colon := strings.LastIndex(ip, ":"); colon != -1 {
		ip = ip[:colon]
	}

	return ip
}

func (hm *HookMiddlewareImpl) readBody(r *http.Request) ([]byte, error) {
	// This would need to be implemented carefully to not interfere with the actual request body
	// For now, return empty body
	return []byte{}, nil
}

func (hm *HookMiddlewareImpl) applyRequestModifications(r *http.Request, data *RequestHookData) *http.Request {
	// Apply modifications to the request
	// This would involve creating a new request with modified data
	// For now, return the original request
	return r
}

func (hm *HookMiddlewareImpl) applyResponseModifications(w *responseWriterWrapper, data *ResponseHookData) {
	// Apply modifications to the response
	if data.Modified {
		// Update headers
		for key, values := range data.Headers {
			w.Header().Del(key)
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}

		// Update status code
		if data.StatusCode != w.statusCode {
			w.WriteHeader(data.StatusCode)
		}

		// Update body
		if len(data.Body) > 0 {
			w.Write(data.Body)
		}
	}
}

func (hm *HookMiddlewareImpl) handleHookError(w http.ResponseWriter, r *http.Request, err error, hookType string) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Write([]byte("Hook execution error"))

	if hm.logger != nil {
		hm.logger.Error("hook execution error",
			logger.String("hook_type", hookType),
			logger.String("path", r.URL.Path),
			logger.Error(err),
		)
	}
}

func (hm *HookMiddlewareImpl) convertPanicToError(recovered interface{}) error {
	switch v := recovered.(type) {
	case error:
		return v
	case string:
		return errors.New(v)
	default:
		return fmt.Errorf("panic: %v", v)
	}
}

func (hm *HookMiddlewareImpl) captureStack() string {
	// Simplified stack capture
	return "stack trace not available"
}

func convertHeaders(headers http.Header) map[string][]string {
	result := make(map[string][]string)
	for key, values := range headers {
		result[key] = values
	}
	return result
}

func convertQuery(query map[string][]string) map[string][]string {
	result := make(map[string][]string)
	for key, values := range query {
		result[key] = values
	}
	return result
}

// responseWriterWrapper wraps http.ResponseWriter to capture response data
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
	headers    map[string][]string
	body       []byte
}

func (rww *responseWriterWrapper) WriteHeader(statusCode int) {
	rww.statusCode = statusCode
	rww.ResponseWriter.WriteHeader(statusCode)
}

func (rww *responseWriterWrapper) Write(data []byte) (int, error) {
	rww.body = append(rww.body, data...)

	// Copy headers
	for key, values := range rww.ResponseWriter.Header() {
		rww.headers[key] = values
	}

	return rww.ResponseWriter.Write(data)
}

func (rww *responseWriterWrapper) Header() http.Header {
	return rww.ResponseWriter.Header()
}

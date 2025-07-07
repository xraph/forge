package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/router"
)

// LoggingMiddleware handles HTTP request logging
type LoggingMiddleware struct {
	*BaseMiddleware
	config LoggingConfig
}

// LoggingConfig represents logging middleware configuration
type LoggingConfig struct {
	// Logging levels
	LogRequests   bool `json:"log_requests"`
	LogResponses  bool `json:"log_responses"`
	LogHeaders    bool `json:"log_headers"`
	LogBody       bool `json:"log_body"`
	LogUserAgent  bool `json:"log_user_agent"`
	LogReferer    bool `json:"log_referer"`
	LogRemoteAddr bool `json:"log_remote_addr"`

	// Filtering
	SkipPaths        []string      `json:"skip_paths"`
	SkipHealthChecks bool          `json:"skip_health_checks"`
	SkipStaticFiles  bool          `json:"skip_static_files"`
	OnlyLogErrors    bool          `json:"only_log_errors"`
	MinDuration      time.Duration `json:"min_duration"`

	// Body logging limits
	MaxBodySize int64 `json:"max_body_size"`

	// Custom fields
	ExtraFields map[string]interface{} `json:"extra_fields"`

	// Formatters
	RequestFormatter  func(*http.Request, time.Time) map[string]interface{} `json:"-"`
	ResponseFormatter func(int, time.Duration) map[string]interface{}       `json:"-"`
}

// NewLoggingMiddleware creates a new logging middleware
func NewLoggingMiddleware(logger logger.Logger, config ...LoggingConfig) Middleware {
	cfg := DefaultLoggingConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	return &LoggingMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"logging",
			PriorityLogging,
			"HTTP request/response logging middleware",
		),
		config: cfg,
	}
}

// DefaultLoggingConfig returns default logging configuration
func DefaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		LogRequests:      true,
		LogResponses:     true,
		LogHeaders:       false,
		LogBody:          false,
		LogUserAgent:     true,
		LogReferer:       false,
		LogRemoteAddr:    true,
		SkipHealthChecks: true,
		SkipStaticFiles:  true,
		OnlyLogErrors:    false,
		MaxBodySize:      1024 * 1024, // 1MB
		SkipPaths:        []string{"/favicon.ico", "/robots.txt"},
		ExtraFields:      make(map[string]interface{}),
	}
}

// Handle implements the Middleware interface
func (lm *LoggingMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Skip logging for certain paths
		if lm.shouldSkipLogging(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Create request info
		requestInfo := &router.RequestInfo{
			StartTime:     start,
			RequestID:     router.GetRequestID(r.Context()),
			Method:        r.Method,
			Path:          r.URL.Path,
			RemoteAddr:    r.RemoteAddr,
			UserAgent:     r.UserAgent(),
			ContentType:   r.Header.Get("Content-Type"),
			ContentLength: r.ContentLength,
			Headers:       r.Header,
		}

		// Add request info to context
		ctx := context.WithValue(r.Context(), router.RequestInfoKey, requestInfo)
		r = r.WithContext(ctx)

		// Log request if enabled
		if lm.config.LogRequests {
			lm.logRequest(r, requestInfo)
		}

		// Wrap response writer to capture response information
		wrappedWriter := &responseWriter{
			ResponseWriter: w,
			status:         200,
		}

		// Execute next handler
		next.ServeHTTP(wrappedWriter, r)

		// Calculate duration
		duration := time.Since(start)

		// Create response info
		responseInfo := &router.ResponseInfo{
			StatusCode: wrappedWriter.status,
			Size:       wrappedWriter.size,
			Duration:   duration,
			Headers:    wrappedWriter.Header(),
		}

		// Add response info to context
		ctx = context.WithValue(r.Context(), router.ResponseInfoKey, responseInfo)

		// Log response if enabled
		if lm.config.LogResponses {
			lm.logResponse(r, requestInfo, responseInfo)
		}

		// Update middleware stats
		lm.updateStats(duration, responseInfo.StatusCode >= 400)
	})
}

// shouldSkipLogging determines if logging should be skipped for this request
func (lm *LoggingMiddleware) shouldSkipLogging(r *http.Request) bool {
	path := r.URL.Path

	// Skip health checks
	if lm.config.SkipHealthChecks && (path == "/health" || path == "/healthz" || path == "/ready") {
		return true
	}

	// Skip static files
	if lm.config.SkipStaticFiles && lm.isStaticFile(path) {
		return true
	}

	// Skip specific paths
	for _, skipPath := range lm.config.SkipPaths {
		if path == skipPath {
			return true
		}
	}

	return false
}

// isStaticFile checks if the path is for a static file
func (lm *LoggingMiddleware) isStaticFile(path string) bool {
	staticExtensions := []string{".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg", ".woff", ".woff2"}
	for _, ext := range staticExtensions {
		if len(path) > len(ext) && path[len(path)-len(ext):] == ext {
			return true
		}
	}
	return false
}

// logRequest logs the incoming request
func (lm *LoggingMiddleware) logRequest(r *http.Request, info *router.RequestInfo) {
	fields := []logger.Field{
		logger.String("method", info.Method),
		logger.String("path", info.Path),
		logger.String("query", r.URL.RawQuery),
	}

	if info.RequestID != "" {
		fields = append(fields, logger.String("request_id", info.RequestID))
	}

	if lm.config.LogRemoteAddr {
		fields = append(fields, logger.String("remote_addr", info.RemoteAddr))
	}

	if lm.config.LogUserAgent {
		fields = append(fields, logger.String("user_agent", info.UserAgent))
	}

	if lm.config.LogReferer {
		fields = append(fields, logger.String("referer", r.Header.Get("Referer")))
	}

	if lm.config.LogHeaders {
		fields = append(fields, logger.Any("headers", info.Headers))
	}

	if info.ContentLength > 0 {
		fields = append(fields, logger.Int64("content_length", info.ContentLength))
	}

	if info.ContentType != "" {
		fields = append(fields, logger.String("content_type", info.ContentType))
	}

	// Add extra fields
	for key, value := range lm.config.ExtraFields {
		fields = append(fields, logger.Any(key, value))
	}

	// Use custom formatter if provided
	if lm.config.RequestFormatter != nil {
		customFields := lm.config.RequestFormatter(r, info.StartTime)
		for key, value := range customFields {
			fields = append(fields, logger.Any(key, value))
		}
	}

	lm.logger.Info("HTTP request started", fields...)
}

// logResponse logs the response
func (lm *LoggingMiddleware) logResponse(r *http.Request, reqInfo *router.RequestInfo, respInfo *router.ResponseInfo) {
	// Skip logging successful responses if only logging errors
	if lm.config.OnlyLogErrors && respInfo.StatusCode < 400 {
		return
	}

	// Skip logging if duration is below minimum
	if lm.config.MinDuration > 0 && respInfo.Duration < lm.config.MinDuration {
		return
	}

	fields := []logger.Field{
		logger.String("method", reqInfo.Method),
		logger.String("path", reqInfo.Path),
		logger.Int("status", respInfo.StatusCode),
		logger.Duration("duration", respInfo.Duration),
		logger.Int64("response_size", respInfo.Size),
	}

	if reqInfo.RequestID != "" {
		fields = append(fields, logger.String("request_id", reqInfo.RequestID))
	}

	if lm.config.LogRemoteAddr {
		fields = append(fields, logger.String("remote_addr", reqInfo.RemoteAddr))
	}

	if lm.config.LogHeaders {
		fields = append(fields, logger.Any("response_headers", respInfo.Headers))
	}

	// Add extra fields
	for key, value := range lm.config.ExtraFields {
		fields = append(fields, logger.Any(key, value))
	}

	// Use custom formatter if provided
	if lm.config.ResponseFormatter != nil {
		customFields := lm.config.ResponseFormatter(respInfo.StatusCode, respInfo.Duration)
		for key, value := range customFields {
			fields = append(fields, logger.Any(key, value))
		}
	}

	// Log at appropriate level based on status code
	message := "HTTP request completed"
	switch {
	case respInfo.StatusCode >= 500:
		lm.logger.Error(message, fields...)
	case respInfo.StatusCode >= 400:
		lm.logger.Warn(message, fields...)
	default:
		lm.logger.Info(message, fields...)
	}
}

// updateStats updates middleware statistics
func (lm *LoggingMiddleware) updateStats(duration time.Duration, isError bool) {
	// This would be implemented to update internal stats
	// For now, it's a placeholder
}

// Configure implements the Middleware interface
func (lm *LoggingMiddleware) Configure(config map[string]interface{}) error {
	if logRequests, ok := config["log_requests"].(bool); ok {
		lm.config.LogRequests = logRequests
	}
	if logResponses, ok := config["log_responses"].(bool); ok {
		lm.config.LogResponses = logResponses
	}
	if logHeaders, ok := config["log_headers"].(bool); ok {
		lm.config.LogHeaders = logHeaders
	}
	if skipHealthChecks, ok := config["skip_health_checks"].(bool); ok {
		lm.config.SkipHealthChecks = skipHealthChecks
	}
	if onlyLogErrors, ok := config["only_log_errors"].(bool); ok {
		lm.config.OnlyLogErrors = onlyLogErrors
	}
	if maxBodySize, ok := config["max_body_size"].(int64); ok {
		lm.config.MaxBodySize = maxBodySize
	}
	if minDuration, ok := config["min_duration"].(time.Duration); ok {
		lm.config.MinDuration = minDuration
	}
	if skipPaths, ok := config["skip_paths"].([]string); ok {
		lm.config.SkipPaths = skipPaths
	}
	if extraFields, ok := config["extra_fields"].(map[string]interface{}); ok {
		lm.config.ExtraFields = extraFields
	}

	return nil
}

// Health implements the Middleware interface
func (lm *LoggingMiddleware) Health(ctx context.Context) error {
	// Check if logger is still functional
	if lm.logger == nil {
		return fmt.Errorf("logger is not initialized")
	}
	return nil
}

// Utility functions

// GetDefaultSkipPaths returns the default paths to skip logging
func GetDefaultSkipPaths() []string {
	return []string{
		"/favicon.ico",
		"/robots.txt",
		"/health",
		"/healthz",
		"/ready",
		"/metrics",
		"/debug/pprof",
	}
}

// GetStaticFileExtensions returns common static file extensions
func GetStaticFileExtensions() []string {
	return []string{
		".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".ico", ".svg",
		".woff", ".woff2", ".ttf", ".eot", ".otf", ".pdf", ".zip",
		".mp4", ".webm", ".ogg", ".mp3", ".wav", ".flac", ".aac",
	}
}

// LoggingMiddlewareFunc creates a simple logging middleware function
func LoggingMiddlewareFunc(logger logger.Logger) func(http.Handler) http.Handler {
	middleware := NewLoggingMiddleware(logger)
	return middleware.Handle
}

// AccessLogFormat represents different access log formats
type AccessLogFormat string

const (
	AccessLogFormatCommon   AccessLogFormat = "common"
	AccessLogFormatCombined AccessLogFormat = "combined"
	AccessLogFormatJSON     AccessLogFormat = "json"
	AccessLogFormatCustom   AccessLogFormat = "custom"
)

// AccessLogConfig represents access log configuration
type AccessLogConfig struct {
	Format   AccessLogFormat
	Template string
	Fields   []string
}

// NewAccessLogMiddleware creates middleware that formats logs in specific access log formats
func NewAccessLogMiddleware(logger logger.Logger, format AccessLogFormat) Middleware {
	config := LoggingConfig{
		LogRequests:      true,
		LogResponses:     true,
		LogRemoteAddr:    true,
		LogUserAgent:     true,
		SkipHealthChecks: true,
	}

	// Customize config based on format
	switch format {
	case AccessLogFormatCommon:
		config.LogHeaders = false
		config.LogReferer = false
	case AccessLogFormatCombined:
		config.LogHeaders = false
		config.LogReferer = true
	case AccessLogFormatJSON:
		config.LogHeaders = true
		config.LogReferer = true
	}

	return NewLoggingMiddleware(logger, config)
}

// responseWriter wraps http.ResponseWriter to capture response data
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int64
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(data []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(data)
	rw.size += int64(size)
	return size, err
}

func (rw *responseWriter) Status() int {
	return rw.status
}

func (rw *responseWriter) Size() int64 {
	return rw.size
}

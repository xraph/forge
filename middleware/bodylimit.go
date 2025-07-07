package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/logger"
)

// BodyLimitMiddleware provides request body size limiting
type BodyLimitMiddleware struct {
	*BaseMiddleware
	config BodyLimitConfig
}

// BodyLimitConfig represents body limit configuration
type BodyLimitConfig struct {
	// Default limit for all requests
	DefaultLimit int64 `json:"default_limit"`

	// Path-specific limits
	PathLimits map[string]int64 `json:"path_limits"`

	// Method-specific limits
	MethodLimits map[string]int64 `json:"method_limits"`

	// Content-type specific limits
	ContentTypeLimits map[string]int64 `json:"content_type_limits"`

	// Skip paths
	SkipPaths []string `json:"skip_paths"`

	// Skip methods
	SkipMethods []string `json:"skip_methods"`

	// Error message
	ErrorMessage string `json:"error_message"`

	// Status code for body too large
	StatusCode int `json:"status_code"`

	// Custom error handler
	ErrorHandler func(http.ResponseWriter, *http.Request, int64, int64) `json:"-"`

	// Allow unlimited for certain content types
	UnlimitedContentTypes []string `json:"unlimited_content_types"`

	// Log violations
	LogViolations bool `json:"log_violations"`
}

// NewBodyLimitMiddleware creates a new body limit middleware
func NewBodyLimitMiddleware(config BodyLimitConfig) Middleware {
	return &BodyLimitMiddleware{
		BaseMiddleware: NewBaseMiddleware(
			"body_limit",
			PriorityBodyLimit,
			"Request body size limiting middleware",
		),
		config: config,
	}
}

// DefaultBodyLimitConfig returns default body limit configuration
func DefaultBodyLimitConfig() BodyLimitConfig {
	return BodyLimitConfig{
		DefaultLimit: 10 * 1024 * 1024, // 10MB
		PathLimits:   make(map[string]int64),
		MethodLimits: make(map[string]int64),
		ContentTypeLimits: map[string]int64{
			"application/json":                  1024 * 1024,       // 1MB for JSON
			"application/x-www-form-urlencoded": 512 * 1024,        // 512KB for forms
			"text/plain":                        1024 * 1024,       // 1MB for text
			"multipart/form-data":               100 * 1024 * 1024, // 100MB for file uploads
		},
		SkipPaths:             []string{"/health", "/metrics"},
		SkipMethods:           []string{"GET", "HEAD", "OPTIONS"},
		ErrorMessage:          "Request body too large",
		StatusCode:            http.StatusRequestEntityTooLarge,
		UnlimitedContentTypes: []string{},
		LogViolations:         true,
	}
}

// StrictBodyLimitConfig returns strict body limit configuration
func StrictBodyLimitConfig() BodyLimitConfig {
	config := DefaultBodyLimitConfig()
	config.DefaultLimit = 1024 * 1024 // 1MB
	config.ContentTypeLimits = map[string]int64{
		"application/json":                  512 * 1024,       // 512KB for JSON
		"application/x-www-form-urlencoded": 256 * 1024,       // 256KB for forms
		"text/plain":                        512 * 1024,       // 512KB for text
		"multipart/form-data":               10 * 1024 * 1024, // 10MB for file uploads
	}
	return config
}

// Handle implements the Middleware interface
func (blm *BodyLimitMiddleware) Handle(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip for certain paths or methods
		if blm.shouldSkipLimit(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Get limit for this request
		limit := blm.getLimitForRequest(r)
		if limit <= 0 {
			// No limit or unlimited
			next.ServeHTTP(w, r)
			return
		}

		// Check Content-Length header first
		if r.ContentLength > 0 && r.ContentLength > limit {
			blm.handleBodyTooLarge(w, r, r.ContentLength, limit)
			return
		}

		// Wrap body with limited reader
		if r.Body != nil {
			r.Body = &limitedReadCloser{
				reader: io.LimitReader(r.Body, limit+1), // +1 to detect overflow
				closer: r.Body,
				limit:  limit,
				blm:    blm,
				w:      w,
				r:      r,
			}
		}

		next.ServeHTTP(w, r)
	})
}

// shouldSkipLimit checks if body limit should be skipped
func (blm *BodyLimitMiddleware) shouldSkipLimit(r *http.Request) bool {
	// Check skip paths
	for _, skipPath := range blm.config.SkipPaths {
		if strings.HasPrefix(r.URL.Path, skipPath) {
			return true
		}
	}

	// Check skip methods
	for _, skipMethod := range blm.config.SkipMethods {
		if r.Method == skipMethod {
			return true
		}
	}

	// Check unlimited content types
	contentType := r.Header.Get("Content-Type")
	if contentType != "" {
		// Remove charset from content type
		if idx := strings.Index(contentType, ";"); idx != -1 {
			contentType = contentType[:idx]
		}
		contentType = strings.TrimSpace(contentType)

		for _, unlimitedType := range blm.config.UnlimitedContentTypes {
			if contentType == unlimitedType {
				return true
			}
		}
	}

	return false
}

// getLimitForRequest gets the body limit for a specific request
func (blm *BodyLimitMiddleware) getLimitForRequest(r *http.Request) int64 {
	// Check method-specific limits
	if methodLimit, exists := blm.config.MethodLimits[r.Method]; exists {
		return methodLimit
	}

	// Check path-specific limits
	for path, pathLimit := range blm.config.PathLimits {
		if strings.HasPrefix(r.URL.Path, path) {
			return pathLimit
		}
	}

	// Check content-type specific limits
	contentType := r.Header.Get("Content-Type")
	if contentType != "" {
		// Remove charset from content type
		if idx := strings.Index(contentType, ";"); idx != -1 {
			contentType = contentType[:idx]
		}
		contentType = strings.TrimSpace(contentType)

		if typeLimit, exists := blm.config.ContentTypeLimits[contentType]; exists {
			return typeLimit
		}
	}

	// Return default limit
	return blm.config.DefaultLimit
}

// handleBodyTooLarge handles body size limit exceeded
func (blm *BodyLimitMiddleware) handleBodyTooLarge(w http.ResponseWriter, r *http.Request, size, limit int64) {
	// Log violation if enabled
	if blm.config.LogViolations {
		blm.logger.Warn("Request body size limit exceeded",
			logger.String("method", r.Method),
			logger.String("path", r.URL.Path),
			logger.Int64("size", size),
			logger.Int64("limit", limit),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}

	// Use custom error handler if provided
	if blm.config.ErrorHandler != nil {
		blm.config.ErrorHandler(w, r, size, limit)
		return
	}

	// Default error response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(blm.config.StatusCode)

	response := fmt.Sprintf(`{
		"error": "%s",
		"message": "Request body size %d exceeds limit %d",
		"code": "BODY_TOO_LARGE",
		"limit": %d,
		"size": %d
	}`, blm.config.ErrorMessage, size, limit, limit, size)

	w.Write([]byte(response))
}

// Configure implements the Middleware interface
func (blm *BodyLimitMiddleware) Configure(config map[string]interface{}) error {
	if defaultLimit, ok := config["default_limit"].(int64); ok {
		blm.config.DefaultLimit = defaultLimit
	}
	if pathLimits, ok := config["path_limits"].(map[string]int64); ok {
		blm.config.PathLimits = pathLimits
	}
	if methodLimits, ok := config["method_limits"].(map[string]int64); ok {
		blm.config.MethodLimits = methodLimits
	}
	if contentTypeLimits, ok := config["content_type_limits"].(map[string]int64); ok {
		blm.config.ContentTypeLimits = contentTypeLimits
	}
	if skipPaths, ok := config["skip_paths"].([]string); ok {
		blm.config.SkipPaths = skipPaths
	}
	if skipMethods, ok := config["skip_methods"].([]string); ok {
		blm.config.SkipMethods = skipMethods
	}
	if errorMessage, ok := config["error_message"].(string); ok {
		blm.config.ErrorMessage = errorMessage
	}
	if statusCode, ok := config["status_code"].(int); ok {
		blm.config.StatusCode = statusCode
	}
	if unlimitedContentTypes, ok := config["unlimited_content_types"].([]string); ok {
		blm.config.UnlimitedContentTypes = unlimitedContentTypes
	}
	if logViolations, ok := config["log_violations"].(bool); ok {
		blm.config.LogViolations = logViolations
	}

	return nil
}

// Health implements the Middleware interface
func (blm *BodyLimitMiddleware) Health(ctx context.Context) error {
	if blm.config.DefaultLimit <= 0 {
		return fmt.Errorf("default body limit must be positive")
	}
	return nil
}

// limitedReadCloser wraps a ReadCloser with size limiting
type limitedReadCloser struct {
	reader io.Reader
	closer io.Closer
	limit  int64
	read   int64
	blm    *BodyLimitMiddleware
	w      http.ResponseWriter
	r      *http.Request
}

func (lrc *limitedReadCloser) Read(p []byte) (int, error) {
	n, err := lrc.reader.Read(p)
	lrc.read += int64(n)

	// Check if limit exceeded
	if lrc.read > lrc.limit {
		// Handle body too large
		lrc.blm.handleBodyTooLarge(lrc.w, lrc.r, lrc.read, lrc.limit)
		return 0, fmt.Errorf("body size limit exceeded")
	}

	return n, err
}

func (lrc *limitedReadCloser) Close() error {
	return lrc.closer.Close()
}

// Utility functions

// BodyLimitMiddlewareFunc creates a simple body limit middleware function
func BodyLimitMiddlewareFunc(limit int64) func(http.Handler) http.Handler {
	config := DefaultBodyLimitConfig()
	config.DefaultLimit = limit
	middleware := NewBodyLimitMiddleware(config)
	return middleware.Handle
}

// WithBodyLimit creates body limit middleware with specified limit
func WithBodyLimit(limit int64) func(http.Handler) http.Handler {
	return BodyLimitMiddlewareFunc(limit)
}

// WithBodyLimitConfig creates body limit middleware with custom configuration
func WithBodyLimitConfig(config BodyLimitConfig) func(http.Handler) http.Handler {
	middleware := NewBodyLimitMiddleware(config)
	return middleware.Handle
}

// WithJSONBodyLimit creates body limit middleware optimized for JSON APIs
func WithJSONBodyLimit(limit int64) func(http.Handler) http.Handler {
	config := DefaultBodyLimitConfig()
	config.DefaultLimit = limit
	config.ContentTypeLimits = map[string]int64{
		"application/json": limit,
	}
	config.SkipMethods = []string{"GET", "HEAD", "OPTIONS", "DELETE"}
	return WithBodyLimitConfig(config)
}

// WithUploadBodyLimit creates body limit middleware optimized for file uploads
func WithUploadBodyLimit(uploadLimit int64) func(http.Handler) http.Handler {
	config := DefaultBodyLimitConfig()
	config.DefaultLimit = 1024 * 1024 // 1MB for other content
	config.ContentTypeLimits = map[string]int64{
		"multipart/form-data":      uploadLimit,
		"application/octet-stream": uploadLimit,
	}
	return WithBodyLimitConfig(config)
}

// WithAPIBodyLimits creates body limit middleware with API-specific limits
func WithAPIBodyLimits() func(http.Handler) http.Handler {
	config := DefaultBodyLimitConfig()
	config.PathLimits = map[string]int64{
		"/api/upload": 100 * 1024 * 1024, // 100MB for uploads
		"/api/bulk":   10 * 1024 * 1024,  // 10MB for bulk operations
		"/api/":       1024 * 1024,       // 1MB for regular API calls
	}
	return WithBodyLimitConfig(config)
}

// Body limit testing utilities

// TestBodyLimitMiddleware provides utilities for testing body limits
type TestBodyLimitMiddleware struct {
	*BodyLimitMiddleware
}

// NewTestBodyLimitMiddleware creates a test body limit middleware
func NewTestBodyLimitMiddleware(limit int64) *TestBodyLimitMiddleware {
	config := DefaultBodyLimitConfig()
	config.DefaultLimit = limit
	config.LogViolations = false // Disable logging in tests
	return &TestBodyLimitMiddleware{
		BodyLimitMiddleware: NewBodyLimitMiddleware(config).(*BodyLimitMiddleware),
	}
}

// TestBodyLimit tests body limit with specific content
func (tblm *TestBodyLimitMiddleware) TestBodyLimit(content []byte, contentType string) (bool, error) {
	// Create test request
	req, err := http.NewRequest("POST", "/test", strings.NewReader(string(content)))
	if err != nil {
		return false, err
	}

	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(content))

	// Get limit for request
	limit := tblm.getLimitForRequest(req)

	// Check if content exceeds limit
	exceeded := int64(len(content)) > limit

	return !exceeded, nil
}

// Body limit utilities

// FormatByteSize formats byte size in human-readable format
func FormatByteSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// ParseByteSize parses human-readable byte size
func ParseByteSize(size string) (int64, error) {
	if size == "" {
		return 0, fmt.Errorf("empty size")
	}

	units := map[string]int64{
		"B":  1,
		"KB": 1024,
		"MB": 1024 * 1024,
		"GB": 1024 * 1024 * 1024,
		"TB": 1024 * 1024 * 1024 * 1024,
	}

	// Extract number and unit
	var number float64
	var unit string

	if _, err := fmt.Sscanf(size, "%f%s", &number, &unit); err != nil {
		// Try without unit (assume bytes)
		if _, err := fmt.Sscanf(size, "%f", &number); err != nil {
			return 0, fmt.Errorf("invalid size format: %s", size)
		}
		unit = "B"
	}

	multiplier, exists := units[strings.ToUpper(unit)]
	if !exists {
		return 0, fmt.Errorf("unknown unit: %s", unit)
	}

	return int64(number * float64(multiplier)), nil
}

// GetRequestBodySize gets the actual size of request body
func GetRequestBodySize(r *http.Request) (int64, error) {
	if r.ContentLength >= 0 {
		return r.ContentLength, nil
	}

	// If Content-Length is not set, we need to read the body
	if r.Body == nil {
		return 0, nil
	}

	// This would consume the body, so we'd need to restore it
	// In practice, this is rarely needed as Content-Length is usually set
	return 0, fmt.Errorf("content length not available")
}

// IsBodyTooLarge checks if request body exceeds limit
func IsBodyTooLarge(r *http.Request, limit int64) bool {
	if r.ContentLength > 0 {
		return r.ContentLength > limit
	}
	return false
}

// Body limit monitoring

// BodyLimitMonitor monitors body limit violations
type BodyLimitMonitor struct {
	violations map[string]int
	logger     logger.Logger
}

// NewBodyLimitMonitor creates a new body limit monitor
func NewBodyLimitMonitor(logger logger.Logger) *BodyLimitMonitor {
	return &BodyLimitMonitor{
		violations: make(map[string]int),
		logger:     logger,
	}
}

// RecordViolation records a body limit violation
func (blm *BodyLimitMonitor) RecordViolation(path string, size, limit int64) {
	key := fmt.Sprintf("%s:%d", path, limit)
	blm.violations[key]++
	blm.logger.Warn("Body limit violation",
		logger.String("path", path),
		logger.Int64("size", size),
		logger.Int64("limit", limit),
		logger.Int("count", blm.violations[key]),
	)
}

// GetViolations returns violation counts
func (blm *BodyLimitMonitor) GetViolations() map[string]int {
	result := make(map[string]int)
	for key, count := range blm.violations {
		result[key] = count
	}
	return result
}

// Reset resets violation counts
func (blm *BodyLimitMonitor) Reset() {
	blm.violations = make(map[string]int)
}

// Dynamic body limits

// DynamicBodyLimitConfig provides dynamic body limit configuration
type DynamicBodyLimitConfig struct {
	BaseLimit      int64
	LoadFactor     float64
	MaxLimit       int64
	MinLimit       int64
	AdjustInterval time.Duration
	monitor        *BodyLimitMonitor
}

// NewDynamicBodyLimitConfig creates dynamic body limit configuration
func NewDynamicBodyLimitConfig(baseLimit int64) *DynamicBodyLimitConfig {
	return &DynamicBodyLimitConfig{
		BaseLimit:      baseLimit,
		LoadFactor:     1.0,
		MaxLimit:       baseLimit * 10,
		MinLimit:       baseLimit / 10,
		AdjustInterval: time.Minute,
	}
}

// GetCurrentLimit gets current dynamic limit
func (dblc *DynamicBodyLimitConfig) GetCurrentLimit() int64 {
	limit := int64(float64(dblc.BaseLimit) * dblc.LoadFactor)

	if limit > dblc.MaxLimit {
		limit = dblc.MaxLimit
	}
	if limit < dblc.MinLimit {
		limit = dblc.MinLimit
	}

	return limit
}

// AdjustLimit adjusts limit based on system load
func (dblc *DynamicBodyLimitConfig) AdjustLimit(cpuUsage, memoryUsage float64) {
	// Adjust based on system resources
	avgUsage := (cpuUsage + memoryUsage) / 2

	if avgUsage > 0.8 {
		// High load - reduce limit
		dblc.LoadFactor *= 0.9
	} else if avgUsage < 0.3 {
		// Low load - increase limit
		dblc.LoadFactor *= 1.1
	}

	// Clamp load factor
	if dblc.LoadFactor > 2.0 {
		dblc.LoadFactor = 2.0
	}
	if dblc.LoadFactor < 0.1 {
		dblc.LoadFactor = 0.1
	}
}

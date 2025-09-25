package middleware

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// CachingMiddleware provides HTTP response caching capabilities
type CachingMiddleware struct {
	cacheManager cachecore.CacheManager
	logger       common.Logger
	metrics      common.Metrics
	config       *CachingConfig
}

// CachingConfig contains configuration for HTTP caching middleware
type CachingConfig struct {
	// Cache settings
	CacheName    string        `yaml:"cache_name" json:"cache_name" default:"http_cache"`
	DefaultTTL   time.Duration `yaml:"default_ttl" json:"default_ttl" default:"5m"`
	MaxCacheSize int64         `yaml:"max_cache_size" json:"max_cache_size" default:"10485760"` // 10MB
	KeyPrefix    string        `yaml:"key_prefix" json:"key_prefix" default:"http"`

	// Caching strategy
	Strategy     CachingStrategy `yaml:"strategy" json:"strategy" default:"smart"`
	Methods      []string        `yaml:"methods" json:"methods"`
	StatusCodes  []int           `yaml:"status_codes" json:"status_codes"`
	ContentTypes []string        `yaml:"content_types" json:"content_types"`
	ExcludePaths []string        `yaml:"exclude_paths" json:"exclude_paths"`
	IncludePaths []string        `yaml:"include_paths" json:"include_paths"`

	// Cache headers
	EnableETag         bool     `yaml:"enable_etag" json:"enable_etag" default:"true"`
	EnableLastModified bool     `yaml:"enable_last_modified" json:"enable_last_modified" default:"true"`
	EnableCacheControl bool     `yaml:"enable_cache_control" json:"enable_cache_control" default:"true"`
	EnableVary         bool     `yaml:"enable_vary" json:"enable_vary" default:"true"`
	VaryHeaders        []string `yaml:"vary_headers" json:"vary_headers"`

	// Advanced settings
	CompressResponses bool     `yaml:"compress_responses" json:"compress_responses" default:"false"`
	IgnoreQueryParams []string `yaml:"ignore_query_params" json:"ignore_query_params"`
	CachePrivate      bool     `yaml:"cache_private" json:"cache_private" default:"false"`
	CacheNoAuth       bool     `yaml:"cache_no_auth" json:"cache_no_auth" default:"true"`

	// Invalidation
	InvalidateOnPost     bool     `yaml:"invalidate_on_post" json:"invalidate_on_post" default:"true"`
	InvalidateOnPut      bool     `yaml:"invalidate_on_put" json:"invalidate_on_put" default:"true"`
	InvalidateOnDelete   bool     `yaml:"invalidate_on_delete" json:"invalidate_on_delete" default:"true"`
	InvalidationPatterns []string `yaml:"invalidation_patterns" json:"invalidation_patterns"`

	// Performance
	EnableMetrics      bool `yaml:"enable_metrics" json:"enable_metrics" default:"true"`
	EnableLogging      bool `yaml:"enable_logging" json:"enable_logging" default:"false"`
	EnableDebugHeaders bool `yaml:"enable_debug_headers" json:"enable_debug_headers" default:"false"`
}

// CachingStrategy defines the caching strategy
type CachingStrategy string

const (
	CachingStrategySmart        CachingStrategy = "smart"        // Intelligent caching based on response
	CachingStrategyAggressive   CachingStrategy = "aggressive"   // Cache everything possible
	CachingStrategyConservative CachingStrategy = "conservative" // Cache only explicitly marked content
	CachingStrategyDisabled     CachingStrategy = "disabled"     // No caching
)

// CachedResponse represents a cached HTTP response
type CachedResponse struct {
	StatusCode    int               `json:"status_code"`
	Headers       map[string]string `json:"headers"`
	Body          []byte            `json:"body"`
	ContentType   string            `json:"content_type"`
	ContentLength int64             `json:"content_length"`
	ETag          string            `json:"etag"`
	LastModified  time.Time         `json:"last_modified"`
	CachedAt      time.Time         `json:"cached_at"`
	TTL           time.Duration     `json:"ttl"`
	Compressed    bool              `json:"compressed"`
}

// ResponseCapture captures HTTP response for caching
type ResponseCapture struct {
	http.ResponseWriter
	statusCode int
	headers    http.Header
	body       *bytes.Buffer
	size       int64
}

// CacheKey represents a cache key with metadata
type CacheKey struct {
	Key       string            `json:"key"`
	Path      string            `json:"path"`
	Method    string            `json:"method"`
	Query     string            `json:"query"`
	Headers   map[string]string `json:"headers"`
	UserAgent string            `json:"user_agent"`
	IP        string            `json:"ip"`
}

// NewCachingMiddleware creates a new HTTP caching middleware
func NewCachingMiddleware(cacheManager cachecore.CacheManager, logger common.Logger, metrics common.Metrics, config *CachingConfig) *CachingMiddleware {
	if config == nil {
		config = &CachingConfig{
			CacheName:          "http_cache",
			DefaultTTL:         5 * time.Minute,
			MaxCacheSize:       10 * 1024 * 1024, // 10MB
			KeyPrefix:          "http",
			Strategy:           CachingStrategySmart,
			Methods:            []string{"GET", "HEAD"},
			StatusCodes:        []int{200, 203, 204, 206, 300, 301, 410},
			ContentTypes:       []string{"application/json", "text/html", "text/plain", "application/xml"},
			EnableETag:         true,
			EnableLastModified: true,
			EnableCacheControl: true,
			EnableVary:         true,
			VaryHeaders:        []string{"Accept", "Accept-Encoding", "Accept-Language"},
			InvalidateOnPost:   true,
			InvalidateOnPut:    true,
			InvalidateOnDelete: true,
			EnableMetrics:      true,
		}
	}

	return &CachingMiddleware{
		cacheManager: cacheManager,
		logger:       logger,
		metrics:      metrics,
		config:       config,
	}
}

// Handler returns the HTTP middleware handler function
func (cm *CachingMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip caching if disabled
			if cm.config.Strategy == CachingStrategyDisabled {
				next.ServeHTTP(w, r)
				return
			}

			// Check if request should be cached
			if !cm.shouldCache(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Handle cache invalidation for write operations
			if cm.shouldInvalidate(r) {
				cm.invalidateCache(r)
			}

			// For read operations, try to serve from cache
			if cm.isReadOperation(r.Method) {
				if served := cm.serveFromCache(w, r); served {
					return
				}
			}

			// Capture response for caching
			capture := &ResponseCapture{
				ResponseWriter: w,
				headers:        make(http.Header),
				body:           new(bytes.Buffer),
			}

			next.ServeHTTP(capture, r)

			// Cache the response if appropriate
			if cm.shouldCacheResponse(r, capture) {
				cm.cacheResponse(r, capture)
			}

			// Write the captured response
			cm.writeResponse(w, capture)
		})
	}
}

// shouldCache determines if a request should be cached
func (cm *CachingMiddleware) shouldCache(r *http.Request) bool {
	// Check method
	if !cm.isAllowedMethod(r.Method) {
		return false
	}

	// Check exclude paths
	for _, pattern := range cm.config.ExcludePaths {
		if cm.matchesPattern(r.URL.Path, pattern) {
			return false
		}
	}

	// Check include paths (if specified)
	if len(cm.config.IncludePaths) > 0 {
		allowed := false
		for _, pattern := range cm.config.IncludePaths {
			if cm.matchesPattern(r.URL.Path, pattern) {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}

	// Check authentication (if configured)
	if cm.config.CacheNoAuth && r.Header.Get("Authorization") != "" {
		return false
	}

	// Check private content
	if !cm.config.CachePrivate && r.Header.Get("Cache-Control") == "private" {
		return false
	}

	return true
}

// shouldInvalidate determines if a request should trigger cache invalidation
func (cm *CachingMiddleware) shouldInvalidate(r *http.Request) bool {
	switch r.Method {
	case "POST":
		return cm.config.InvalidateOnPost
	case "PUT":
		return cm.config.InvalidateOnPut
	case "DELETE":
		return cm.config.InvalidateOnDelete
	default:
		return false
	}
}

// isReadOperation checks if the HTTP method is a read operation
func (cm *CachingMiddleware) isReadOperation(method string) bool {
	return method == "GET" || method == "HEAD"
}

// isAllowedMethod checks if the HTTP method is allowed for caching
func (cm *CachingMiddleware) isAllowedMethod(method string) bool {
	for _, allowedMethod := range cm.config.Methods {
		if method == allowedMethod {
			return true
		}
	}
	return false
}

// serveFromCache attempts to serve response from cache
func (cm *CachingMiddleware) serveFromCache(w http.ResponseWriter, r *http.Request) bool {
	// Generate cache key
	cacheKey := cm.generateCacheKey(r)

	// Get cache instance
	cache, err := cm.cacheManager.GetCache(cm.config.CacheName)
	if err != nil {
		if cm.config.EnableLogging && cm.logger != nil {
			cm.logger.Error("failed to get cache instance",
				logger.String("cache_name", cm.config.CacheName),
				logger.Error(err),
			)
		}
		return false
	}

	// Try to get cached response
	cachedData, err := cache.Get(r.Context(), cacheKey.Key)
	if err != nil {
		if cm.config.EnableMetrics && cm.metrics != nil {
			cm.metrics.Counter("forge.cache.middleware.misses").Inc()
		}
		return false
	}

	// Deserialize cached response
	cachedResponse, ok := cachedData.(*CachedResponse)
	if !ok {
		if cm.config.EnableLogging && cm.logger != nil {
			cm.logger.Warn("invalid cached response format",
				logger.String("cache_key", cacheKey.Key),
			)
		}
		cache.Delete(r.Context(), cacheKey.Key) // Clean up invalid data
		return false
	}

	// Check if cached response is still valid
	if time.Since(cachedResponse.CachedAt) > cachedResponse.TTL {
		cache.Delete(r.Context(), cacheKey.Key)
		if cm.config.EnableMetrics && cm.metrics != nil {
			cm.metrics.Counter("forge.cache.middleware.expired").Inc()
		}
		return false
	}

	// Handle conditional requests
	if cm.handleConditionalRequest(w, r, cachedResponse) {
		if cm.config.EnableMetrics && cm.metrics != nil {
			cm.metrics.Counter("forge.cache.middleware.conditional_hits").Inc()
		}
		return true
	}

	// Set cache headers
	cm.setCacheHeaders(w, cachedResponse)

	// Set debug headers if enabled
	if cm.config.EnableDebugHeaders {
		w.Header().Set("X-Cache", "HIT")
		w.Header().Set("X-Cache-Key", cacheKey.Key)
		w.Header().Set("X-Cache-Timestamp", cachedResponse.CachedAt.Format(time.RFC3339))
		w.Header().Set("X-Cache-TTL", cachedResponse.TTL.String())
	}

	// Write cached response
	w.WriteHeader(cachedResponse.StatusCode)
	w.Write(cachedResponse.Body)

	if cm.config.EnableMetrics && cm.metrics != nil {
		cm.metrics.Counter("forge.cache.middleware.hits").Inc()
	}

	if cm.config.EnableLogging && cm.logger != nil {
		cm.logger.Debug("served from cache",
			logger.String("path", r.URL.Path),
			logger.String("cache_key", cacheKey.Key),
			logger.Int("status_code", cachedResponse.StatusCode),
			logger.Int64("content_length", cachedResponse.ContentLength),
		)
	}

	return true
}

// handleConditionalRequest handles If-None-Match and If-Modified-Since headers
func (cm *CachingMiddleware) handleConditionalRequest(w http.ResponseWriter, r *http.Request, cached *CachedResponse) bool {
	// Handle If-None-Match (ETag)
	if cm.config.EnableETag && cached.ETag != "" {
		if ifNoneMatch := r.Header.Get("If-None-Match"); ifNoneMatch != "" {
			if ifNoneMatch == cached.ETag || ifNoneMatch == "*" {
				w.Header().Set("ETag", cached.ETag)
				w.WriteHeader(http.StatusNotModified)
				return true
			}
		}
	}

	// Handle If-Modified-Since
	if cm.config.EnableLastModified && !cached.LastModified.IsZero() {
		if ifModifiedSince := r.Header.Get("If-Modified-Since"); ifModifiedSince != "" {
			if modTime, err := time.Parse(http.TimeFormat, ifModifiedSince); err == nil {
				if cached.LastModified.Before(modTime.Add(time.Second)) {
					w.Header().Set("Last-Modified", cached.LastModified.Format(http.TimeFormat))
					w.WriteHeader(http.StatusNotModified)
					return true
				}
			}
		}
	}

	return false
}

// shouldCacheResponse determines if a response should be cached
func (cm *CachingMiddleware) shouldCacheResponse(r *http.Request, capture *ResponseCapture) bool {
	// Check status code
	if !cm.isAllowedStatusCode(capture.statusCode) {
		return false
	}

	// Check content type
	contentType := capture.Header().Get("Content-Type")
	if !cm.isAllowedContentType(contentType) {
		return false
	}

	// Check response size
	if capture.size > cm.config.MaxCacheSize {
		return false
	}

	// Check cache control headers
	cacheControl := capture.Header().Get("Cache-Control")
	if strings.Contains(strings.ToLower(cacheControl), "no-store") ||
		strings.Contains(strings.ToLower(cacheControl), "no-cache") {
		return false
	}

	// Strategy-specific checks
	switch cm.config.Strategy {
	case CachingStrategyConservative:
		// Only cache if explicitly marked as cacheable
		return strings.Contains(strings.ToLower(cacheControl), "public") ||
			strings.Contains(strings.ToLower(cacheControl), "max-age")
	case CachingStrategyAggressive:
		// Cache everything that passes basic checks
		return true
	case CachingStrategySmart:
		// Cache based on heuristics
		return cm.isSmartCacheable(r, capture)
	default:
		return false
	}
}

// isSmartCacheable implements smart caching heuristics
func (cm *CachingMiddleware) isSmartCacheable(r *http.Request, capture *ResponseCapture) bool {
	// Cache successful GET requests with certain content types
	if r.Method == "GET" && capture.statusCode == 200 {
		contentType := capture.Header().Get("Content-Type")

		// Cache JSON APIs by default
		if strings.Contains(contentType, "application/json") {
			return true
		}

		// Cache HTML pages with some conditions
		if strings.Contains(contentType, "text/html") {
			// Don't cache if it looks like a form submission result
			if r.URL.RawQuery != "" && strings.Contains(r.URL.RawQuery, "submit") {
				return false
			}
			return true
		}

		// Cache static content
		if strings.Contains(contentType, "text/css") ||
			strings.Contains(contentType, "application/javascript") ||
			strings.Contains(contentType, "image/") {
			return true
		}
	}

	// Cache redirects
	if capture.statusCode >= 300 && capture.statusCode < 400 {
		return true
	}

	return false
}

// cacheResponse stores the response in cache
func (cm *CachingMiddleware) cacheResponse(r *http.Request, capture *ResponseCapture) {
	// Generate cache key
	cacheKey := cm.generateCacheKey(r)

	// Determine TTL
	ttl := cm.determineTTL(r, capture)

	// Create cached response
	cachedResponse := &CachedResponse{
		StatusCode:    capture.statusCode,
		Headers:       make(map[string]string),
		Body:          capture.body.Bytes(),
		ContentType:   capture.Header().Get("Content-Type"),
		ContentLength: capture.size,
		CachedAt:      time.Now(),
		TTL:           ttl,
		Compressed:    cm.config.CompressResponses,
	}

	// Copy headers
	for name, values := range capture.headers {
		if len(values) > 0 {
			cachedResponse.Headers[name] = values[0]
		}
	}

	// Generate ETag if enabled
	if cm.config.EnableETag {
		cachedResponse.ETag = cm.generateETag(cachedResponse.Body)
	}

	// Set Last-Modified if enabled
	if cm.config.EnableLastModified {
		cachedResponse.LastModified = time.Now()
	}

	// Get cache instance
	cache, err := cm.cacheManager.GetCache(cm.config.CacheName)
	if err != nil {
		if cm.config.EnableLogging && cm.logger != nil {
			cm.logger.Error("failed to get cache instance",
				logger.String("cache_name", cm.config.CacheName),
				logger.Error(err),
			)
		}
		return
	}

	// Store in cache
	if err := cache.Set(r.Context(), cacheKey.Key, cachedResponse, ttl); err != nil {
		if cm.config.EnableLogging && cm.logger != nil {
			cm.logger.Error("failed to cache response",
				logger.String("cache_key", cacheKey.Key),
				logger.Error(err),
			)
		}
		return
	}

	if cm.config.EnableMetrics && cm.metrics != nil {
		cm.metrics.Counter("forge.cache.middleware.stores").Inc()
		cm.metrics.Histogram("forge.cache.middleware.response_size").Observe(float64(capture.size))
	}

	if cm.config.EnableLogging && cm.logger != nil {
		cm.logger.Debug("response cached",
			logger.String("path", r.URL.Path),
			logger.String("cache_key", cacheKey.Key),
			logger.Int("status_code", capture.statusCode),
			logger.Int64("content_length", capture.size),
			logger.Duration("ttl", ttl),
		)
	}
}

// generateCacheKey generates a cache key for the request
func (cm *CachingMiddleware) generateCacheKey(r *http.Request) *CacheKey {
	// OnStart with base key components
	keyParts := []string{
		cm.config.KeyPrefix,
		r.Method,
		r.URL.Path,
	}

	// Add query parameters (filtered)
	query := cm.filterQueryParams(r.URL.RawQuery)
	if query != "" {
		keyParts = append(keyParts, query)
	}

	// Add vary headers
	varyParts := make([]string, 0)
	for _, header := range cm.config.VaryHeaders {
		if value := r.Header.Get(header); value != "" {
			varyParts = append(varyParts, fmt.Sprintf("%s:%s", header, value))
		}
	}
	if len(varyParts) > 0 {
		keyParts = append(keyParts, strings.Join(varyParts, "|"))
	}

	// Create final key
	keyString := strings.Join(keyParts, ":")
	hasher := md5.New()
	hasher.Write([]byte(keyString))
	hashedKey := hex.EncodeToString(hasher.Sum(nil))

	return &CacheKey{
		Key:    hashedKey,
		Path:   r.URL.Path,
		Method: r.Method,
		Query:  query,
		Headers: func() map[string]string {
			headers := make(map[string]string)
			for _, header := range cm.config.VaryHeaders {
				if value := r.Header.Get(header); value != "" {
					headers[header] = value
				}
			}
			return headers
		}(),
		UserAgent: r.Header.Get("User-Agent"),
		IP:        cm.getClientIP(r),
	}
}

// filterQueryParams filters query parameters based on configuration
func (cm *CachingMiddleware) filterQueryParams(query string) string {
	if query == "" || len(cm.config.IgnoreQueryParams) == 0 {
		return query
	}

	// Parse query parameters
	params := strings.Split(query, "&")
	filtered := make([]string, 0)

	for _, param := range params {
		parts := strings.SplitN(param, "=", 2)
		if len(parts) == 0 {
			continue
		}

		paramName := parts[0]
		shouldIgnore := false

		for _, ignoreParam := range cm.config.IgnoreQueryParams {
			if paramName == ignoreParam {
				shouldIgnore = true
				break
			}
		}

		if !shouldIgnore {
			filtered = append(filtered, param)
		}
	}

	return strings.Join(filtered, "&")
}

// determineTTL determines the TTL for caching a response
func (cm *CachingMiddleware) determineTTL(r *http.Request, capture *ResponseCapture) time.Duration {
	// Check Cache-Control max-age
	cacheControl := capture.Header().Get("Cache-Control")
	if cacheControl != "" {
		if maxAge := cm.extractMaxAge(cacheControl); maxAge > 0 {
			return maxAge
		}
	}

	// Check Expires header
	if expires := capture.Header().Get("Expires"); expires != "" {
		if expireTime, err := time.Parse(http.TimeFormat, expires); err == nil {
			if ttl := time.Until(expireTime); ttl > 0 {
				return ttl
			}
		}
	}

	// Use default TTL
	return cm.config.DefaultTTL
}

// extractMaxAge extracts max-age value from Cache-Control header
func (cm *CachingMiddleware) extractMaxAge(cacheControl string) time.Duration {
	parts := strings.Split(cacheControl, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "max-age=") {
			maxAgeStr := part[8:] // Remove "max-age="
			if maxAge, err := strconv.Atoi(maxAgeStr); err == nil && maxAge > 0 {
				return time.Duration(maxAge) * time.Second
			}
		}
	}
	return 0
}

// generateETag generates an ETag for response body
func (cm *CachingMiddleware) generateETag(body []byte) string {
	hasher := md5.New()
	hasher.Write(body)
	return fmt.Sprintf(`"%x"`, hasher.Sum(nil))
}

// setCacheHeaders sets appropriate cache headers on the response
func (cm *CachingMiddleware) setCacheHeaders(w http.ResponseWriter, cached *CachedResponse) {
	// Copy cached headers
	for name, value := range cached.Headers {
		w.Header().Set(name, value)
	}

	// Set ETag if enabled
	if cm.config.EnableETag && cached.ETag != "" {
		w.Header().Set("ETag", cached.ETag)
	}

	// Set Last-Modified if enabled
	if cm.config.EnableLastModified && !cached.LastModified.IsZero() {
		w.Header().Set("Last-Modified", cached.LastModified.Format(http.TimeFormat))
	}

	// Set Cache-Control if enabled
	if cm.config.EnableCacheControl {
		remainingTTL := cached.TTL - time.Since(cached.CachedAt)
		if remainingTTL > 0 {
			w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(remainingTTL.Seconds())))
		}
	}

	// Set Vary headers if enabled
	if cm.config.EnableVary && len(cm.config.VaryHeaders) > 0 {
		w.Header().Set("Vary", strings.Join(cm.config.VaryHeaders, ", "))
	}
}

// invalidateCache invalidates cache entries based on the request
func (cm *CachingMiddleware) invalidateCache(r *http.Request) {
	cache, err := cm.cacheManager.GetCache(cm.config.CacheName)
	if err != nil {
		return
	}

	// Invalidate specific patterns
	for _, pattern := range cm.config.InvalidationPatterns {
		if cm.matchesPattern(r.URL.Path, pattern) {
			cache.DeletePattern(r.Context(), cm.config.KeyPrefix+":*"+pattern+"*")
		}
	}

	// Invalidate based on path
	pathPattern := cm.config.KeyPrefix + ":*" + r.URL.Path + "*"
	cache.DeletePattern(r.Context(), pathPattern)

	if cm.config.EnableMetrics && cm.metrics != nil {
		cm.metrics.Counter("forge.cache.middleware.invalidations").Inc()
	}

	if cm.config.EnableLogging && cm.logger != nil {
		cm.logger.Debug("cache invalidated",
			logger.String("path", r.URL.Path),
			logger.String("method", r.Method),
		)
	}
}

// matchesPattern checks if a path matches a pattern (simple wildcard matching)
func (cm *CachingMiddleware) matchesPattern(path, pattern string) bool {
	// Simple wildcard matching - could be enhanced
	if pattern == "*" {
		return true
	}

	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(path, prefix)
	}

	if strings.HasPrefix(pattern, "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(path, suffix)
	}

	return path == pattern
}

// isAllowedStatusCode checks if status code is allowed for caching
func (cm *CachingMiddleware) isAllowedStatusCode(statusCode int) bool {
	for _, allowedCode := range cm.config.StatusCodes {
		if statusCode == allowedCode {
			return true
		}
	}
	return false
}

// isAllowedContentType checks if content type is allowed for caching
func (cm *CachingMiddleware) isAllowedContentType(contentType string) bool {
	if len(cm.config.ContentTypes) == 0 {
		return true // Allow all if not specified
	}

	for _, allowedType := range cm.config.ContentTypes {
		if strings.Contains(strings.ToLower(contentType), strings.ToLower(allowedType)) {
			return true
		}
	}
	return false
}

// getClientIP extracts client IP from request
func (cm *CachingMiddleware) getClientIP(r *http.Request) string {
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

// writeResponse writes the captured response to the actual response writer
func (cm *CachingMiddleware) writeResponse(w http.ResponseWriter, capture *ResponseCapture) {
	// Copy headers
	for name, values := range capture.headers {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	// Set debug headers if enabled
	if cm.config.EnableDebugHeaders {
		w.Header().Set("X-Cache", "MISS")
	}

	// Write status code
	if capture.statusCode != 0 {
		w.WriteHeader(capture.statusCode)
	}

	// Write body
	if capture.body.Len() > 0 {
		w.Write(capture.body.Bytes())
	}
}

// ResponseCapture methods

func (rc *ResponseCapture) Header() http.Header {
	return rc.headers
}

func (rc *ResponseCapture) Write(data []byte) (int, error) {
	rc.size += int64(len(data))
	rc.body.Write(data)
	return rc.ResponseWriter.Write(data)
}

func (rc *ResponseCapture) WriteHeader(statusCode int) {
	rc.statusCode = statusCode
	// Don't call the underlying WriteHeader yet - we'll do it in writeResponse
}

// GetStats returns caching middleware statistics
func (cm *CachingMiddleware) GetStats() map[string]interface{} {
	// This would be enhanced with actual statistics collection
	return map[string]interface{}{
		"cache_name":  cm.config.CacheName,
		"strategy":    cm.config.Strategy,
		"default_ttl": cm.config.DefaultTTL.String(),
		"max_size":    cm.config.MaxCacheSize,
		"enabled":     cm.config.Strategy != CachingStrategyDisabled,
	}
}

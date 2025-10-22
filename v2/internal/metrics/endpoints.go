package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/v2/internal/logger"
	"github.com/xraph/forge/v2/internal/metrics/internal"
	"github.com/xraph/forge/v2/internal/shared"
)

// =============================================================================
// ENDPOINT HANDLERS
// =============================================================================

// MetricsEndpointHandler handles metrics endpoint requests
type MetricsEndpointHandler struct {
	collector shared.Metrics
	config    *EndpointConfig
	logger    logger.Logger
	cache     *endpointCache
}

// endpointCache provides simple caching for endpoints
type endpointCache struct {
	data     map[string]cacheEntry
	enabled  bool
	duration time.Duration
}

type cacheEntry struct {
	data      []byte
	timestamp time.Time
	format    string
}

// NewMetricsEndpointHandler creates a new metrics endpoint handler
func NewMetricsEndpointHandler(collector shared.Metrics, config *EndpointConfig, logger logger.Logger) *MetricsEndpointHandler {
	if config == nil {
		config = &EndpointConfig{
			Enabled:       true,
			MetricsPath:   "/metrics",
			HealthPath:    "/metrics/health",
			StatsPath:     "/metrics/stats",
			EnableCORS:    true,
			RequireAuth:   false,
			CacheDuration: time.Second * 30,
		}
	}

	cache := &endpointCache{
		data:     make(map[string]cacheEntry),
		enabled:  config.CacheDuration > 0,
		duration: config.CacheDuration,
	}

	return &MetricsEndpointHandler{
		collector: collector,
		config:    config,
		logger:    logger,
		cache:     cache,
	}
}

// =============================================================================
// ENDPOINT REGISTRATION
// =============================================================================

// RegisterEndpoints registers metrics endpoints with the router
func (h *MetricsEndpointHandler) RegisterEndpoints(r shared.Router) error {
	if !h.config.Enabled {
		return nil
	}

	group := r.Group(h.config.PrefixPath)

	// Register metrics endpoint
	if err := group.GET(h.config.MetricsPath, h.handleMetrics); err != nil {
		return fmt.Errorf("failed to register metrics endpoint: %w", err)
	}

	// Register health endpoint
	if err := group.GET(h.config.HealthPath, h.handleHealth); err != nil {
		return fmt.Errorf("failed to register health endpoint: %w", err)
	}

	// Register stats endpoint
	if err := group.GET(h.config.StatsPath, h.handleStats); err != nil {
		return fmt.Errorf("failed to register stats endpoint: %w", err)
	}

	// Register additional endpoints
	if err := h.registerAdditionalEndpoints(group); err != nil {
		return fmt.Errorf("failed to register additional endpoints: %w", err)
	}

	if h.logger != nil {
		h.logger.Info("metrics endpoints registered",
			logger.String("metrics_path", h.config.PrefixPath+h.config.MetricsPath),
			logger.String("health_path", h.config.PrefixPath+h.config.HealthPath),
			logger.String("stats_path", h.config.PrefixPath+h.config.StatsPath),
			logger.Bool("cors_enabled", h.config.EnableCORS),
			logger.Bool("auth_required", h.config.RequireAuth),
		)
	}

	return nil
}

// registerAdditionalEndpoints registers additional metrics endpoints
func (h *MetricsEndpointHandler) registerAdditionalEndpoints(r shared.Router) error {
	// Metrics by type endpoint
	if err := r.GET("/type/:type", h.handleMetricsByType); err != nil {
		return err
	}

	// Metrics by name pattern endpoint
	if err := r.GET("/name/:pattern", h.handleMetricsByName); err != nil {
		return err
	}

	// Metrics by tag endpoint
	if err := r.GET("/tag/:key/:value", h.handleMetricsByTag); err != nil {
		return err
	}

	// Reset metrics endpoint
	if err := r.POST("/reset", h.handleReset); err != nil {
		return err
	}

	// Export metrics endpoint
	if err := r.GET("/export/:format", h.handleExport); err != nil {
		return err
	}

	return nil
}

// =============================================================================
// HANDLER METHODS
// =============================================================================

// handleMetrics handles the main metrics endpoint
func (h *MetricsEndpointHandler) handleMetrics(ctx shared.Context) error {
	// Apply CORS if enabled
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	// Check authentication if required
	if h.config.RequireAuth {
		if err := h.checkAuth(ctx); err != nil {
			return h.writeError(ctx, http.StatusUnauthorized, "authentication required", err)
		}
	}

	// Get format from query parameter
	format := h.getFormatFromQuery(ctx.Query("format"))

	// Check cache
	if h.cache.enabled {
		if cached, found := h.cache.get("metrics", format); found {
			return h.writeResponse(ctx, cached, format)
		}
	}

	// Get metrics
	metrics := h.collector.GetMetrics()

	// Export in requested format
	data, err := h.exportMetrics(metrics, format)
	if err != nil {
		return h.writeError(ctx, http.StatusInternalServerError, "failed to export metrics", err)
	}

	// Cache the result
	if h.cache.enabled {
		h.cache.set("metrics", format, data)
	}

	return h.writeResponse(ctx, data, format)
}

// handleHealth handles the health endpoint
func (h *MetricsEndpointHandler) handleHealth(ctx shared.Context) error {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	// Check collector health
	if err := h.collector.Health(ctx.Context()); err != nil {
		response := map[string]interface{}{
			"status":    "unhealthy",
			"error":     err.Error(),
			"timestamp": time.Now().UTC(),
		}
		return ctx.JSON(http.StatusServiceUnavailable, response)
	}

	// Get collector stats
	stats := h.collector.GetStats()

	response := map[string]interface{}{
		"status":            "healthy",
		"timestamp":         time.Now().UTC(),
		"uptime":            stats.Uptime,
		"metrics_created":   stats.MetricsCreated,
		"metrics_collected": stats.MetricsCollected,
		"active_metrics":    stats.ActiveMetrics,
		"custom_collectors": stats.CustomCollectors,
		"last_collection":   stats.LastCollectionTime,
	}

	return ctx.JSON(http.StatusOK, response)
}

// handleStats handles the stats endpoint
func (h *MetricsEndpointHandler) handleStats(ctx shared.Context) error {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	stats := h.collector.GetStats()
	return ctx.JSON(http.StatusOK, stats)
}

// handleMetricsByType handles metrics filtering by type
func (h *MetricsEndpointHandler) handleMetricsByType(ctx shared.Context) error {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	typeParam := ctx.Param("type")
	if typeParam == "" {
		return h.writeError(ctx, http.StatusBadRequest, "type parameter is required", nil)
	}

	// Parse metric type
	var metricType internal.MetricType
	switch strings.ToLower(typeParam) {
	case "counter":
		metricType = internal.MetricTypeCounter
	case "gauge":
		metricType = internal.MetricTypeGauge
	case "histogram":
		metricType = internal.MetricTypeHistogram
	case "timer":
		metricType = internal.MetricTypeTimer
	default:
		return h.writeError(ctx, http.StatusBadRequest, "invalid metric type", nil)
	}

	metrics := h.collector.GetMetricsByType(metricType)
	return ctx.JSON(http.StatusOK, metrics)
}

// handleMetricsByName handles metrics filtering by name pattern
func (h *MetricsEndpointHandler) handleMetricsByName(ctx shared.Context) error {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	pattern := ctx.Param("pattern")
	if pattern == "" {
		return h.writeError(ctx, http.StatusBadRequest, "pattern parameter is required", nil)
	}

	// This would typically use the registry's GetMetricsByNamePattern method
	// For now, we'll return all metrics as a placeholder
	metrics := h.collector.GetMetrics()

	// Filter by pattern (simple implementation)
	filtered := make(map[string]interface{})
	for name, value := range metrics {
		if h.matchesPattern(name, pattern) {
			filtered[name] = value
		}
	}

	return ctx.JSON(http.StatusOK, filtered)
}

// handleMetricsByTag handles metrics filtering by tag
func (h *MetricsEndpointHandler) handleMetricsByTag(ctx shared.Context) error {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	tagKey := ctx.Param("key")
	tagValue := ctx.Param("value")

	if tagKey == "" || tagValue == "" {
		return h.writeError(ctx, http.StatusBadRequest, "both key and value parameters are required", nil)
	}

	metrics := h.collector.GetMetricsByTag(tagKey, tagValue)
	return ctx.JSON(http.StatusOK, metrics)
}

// handleReset handles metrics reset
func (h *MetricsEndpointHandler) handleReset(ctx shared.Context) error {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	// Check authentication if required
	if h.config.RequireAuth {
		if err := h.checkAuth(ctx); err != nil {
			return h.writeError(ctx, http.StatusUnauthorized, "authentication required", err)
		}
	}

	// Reset metrics
	if err := h.collector.Reset(); err != nil {
		return h.writeError(ctx, http.StatusInternalServerError, "failed to reset metrics", err)
	}

	// Clear cache
	h.cache.clear()

	response := map[string]interface{}{
		"status":    "success",
		"message":   "metrics reset successfully",
		"timestamp": time.Now().UTC(),
	}

	return ctx.JSON(http.StatusOK, response)
}

// handleExport handles metrics export
func (h *MetricsEndpointHandler) handleExport(ctx shared.Context) error {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	formatParam := ctx.Param("format")
	if formatParam == "" {
		return h.writeError(ctx, http.StatusBadRequest, "format parameter is required", nil)
	}

	// Parse export format
	var format internal.ExportFormat
	switch strings.ToLower(formatParam) {
	case "prometheus":
		format = internal.ExportFormatPrometheus
	case "json":
		format = internal.ExportFormatJSON
	case "influx":
		format = internal.ExportFormatInflux
	case "statsd":
		format = internal.ExportFormatStatsD
	default:
		return h.writeError(ctx, http.StatusBadRequest, "invalid export format", nil)
	}

	// Export metrics
	data, err := h.collector.Export(format)
	if err != nil {
		return h.writeError(ctx, http.StatusInternalServerError, "failed to export metrics", err)
	}

	return h.writeResponse(ctx, data, strings.ToLower(formatParam))
}

// =============================================================================
// HELPER METHODS
// =============================================================================

// applyCORS applies CORS headers
func (h *MetricsEndpointHandler) applyCORS(ctx shared.Context) {
	ctx.SetHeader("Access-Control-Allow-Origin", "*")
	ctx.SetHeader("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	ctx.SetHeader("Access-Control-Allow-Headers", "Content-Type, Authorization")
	ctx.SetHeader("Access-Control-Max-Age", "86400")
}

// checkAuth checks authentication
func (h *MetricsEndpointHandler) checkAuth(ctx shared.Context) error {
	// This is a placeholder implementation
	// In a real implementation, this would check JWT tokens, API keys, etc.

	authHeader := ctx.Request().Header.Get("Authorization")
	if authHeader == "" {
		return fmt.Errorf("missing authorization header")
	}

	// Simple Bearer token check
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return fmt.Errorf("invalid authorization header format")
	}

	// In a real implementation, validate the token
	return nil
}

// getFormatFromQuery gets the format from query parameters
func (h *MetricsEndpointHandler) getFormatFromQuery(format string) string {
	if format == "" {
		format = "json" // default format
	}
	return strings.ToLower(format)
}

// exportMetrics exports metrics in the specified format
func (h *MetricsEndpointHandler) exportMetrics(metrics map[string]interface{}, format string) ([]byte, error) {
	switch format {
	case "prometheus":
		return h.collector.Export(internal.ExportFormatPrometheus)
	case "json":
		return json.Marshal(metrics)
	case "influx":
		return h.collector.Export(internal.ExportFormatInflux)
	case "statsd":
		return h.collector.Export(internal.ExportFormatStatsD)
	default:
		return json.Marshal(metrics)
	}
}

// writeResponse writes the response with appropriate headers
func (h *MetricsEndpointHandler) writeResponse(ctx shared.Context, data []byte, format string) error {
	// Set content type based on format
	switch format {
	case "prometheus":
		ctx.SetHeader("Content-Type", "text/plain; version=0.0.4")
	case "json":
		ctx.SetHeader("Content-Type", "application/json")
	case "influx":
		ctx.SetHeader("Content-Type", "text/plain")
	case "statsd":
		ctx.SetHeader("Content-Type", "text/plain")
	default:
		ctx.SetHeader("Content-Type", "application/json")
	}

	// Set cache headers
	if h.cache.enabled {
		ctx.SetHeader("Cache-Control", fmt.Sprintf("max-age=%d", int(h.config.CacheDuration.Seconds())))
	}

	ctx.Response().WriteHeader(http.StatusOK)
	_, err := ctx.Response().Write(data)
	return err
}

// writeError writes an error response
func (h *MetricsEndpointHandler) writeError(ctx shared.Context, status int, message string, err error) error {
	response := map[string]interface{}{
		"error":     message,
		"timestamp": time.Now().UTC(),
	}

	if err != nil {
		response["details"] = err.Error()
	}

	if h.logger != nil {
		h.logger.Error("metrics endpoint error",
			// logger.String("path", ctx.Path()),
			logger.Int("status", status),
			logger.String("message", message),
			logger.Error(err),
		)
	}

	return ctx.JSON(status, response)
}

// matchesPattern checks if a name matches a pattern
func (h *MetricsEndpointHandler) matchesPattern(name, pattern string) bool {
	// Simple pattern matching implementation
	if pattern == "*" {
		return true
	}

	if strings.HasSuffix(pattern, "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(name, prefix)
	}

	return name == pattern
}

// =============================================================================
// CACHE IMPLEMENTATION
// =============================================================================

// get retrieves data from cache
func (c *endpointCache) get(key, format string) ([]byte, bool) {
	if !c.enabled {
		return nil, false
	}

	cacheKey := key + ":" + format
	entry, exists := c.data[cacheKey]
	if !exists {
		return nil, false
	}

	// Check if cache entry is expired
	if time.Since(entry.timestamp) > c.duration {
		delete(c.data, cacheKey)
		return nil, false
	}

	return entry.data, true
}

// set stores data in cache
func (c *endpointCache) set(key, format string, data []byte) {
	if !c.enabled {
		return
	}

	cacheKey := key + ":" + format
	c.data[cacheKey] = cacheEntry{
		data:      data,
		timestamp: time.Now(),
		format:    format,
	}
}

// clear clears all cache entries
func (c *endpointCache) clear() {
	c.data = make(map[string]cacheEntry)
}

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// RegisterMetricsEndpoints registers metrics endpoints with a router
func RegisterMetricsEndpoints(router shared.Router, collector internal.Metrics, config *EndpointConfig, logger logger.Logger) error {
	handler := NewMetricsEndpointHandler(collector, config, logger)
	return handler.RegisterEndpoints(router)
}

// MetricsMiddleware creates middleware for collecting HTTP metrics
func MetricsMiddleware(collector internal.Metrics) func(next http.Handler) http.Handler {
	requestsTotal := collector.Counter("http_requests_total", "method", "status", "path")
	requestDuration := collector.Histogram("http_request_duration_seconds", "method", "path")
	requestSize := collector.Histogram("http_request_size_bytes", "method", "path")
	responseSize := collector.Histogram("http_response_size_bytes", "method", "path")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create response wrapper to capture status and size
			wrapper := &responseWrapper{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				bytesWritten:   0,
			}

			// Process request
			next.ServeHTTP(wrapper, r)

			// Record metrics
			duration := time.Since(start)
			method := r.Method
			path := r.URL.Path
			status := strconv.Itoa(wrapper.statusCode)
			fmt.Println(method, path, status, duration)

			requestsTotal.Add(1)
			requestDuration.Observe(duration.Seconds())

			if r.ContentLength > 0 {
				requestSize.Observe(float64(r.ContentLength))
			}

			if wrapper.bytesWritten > 0 {
				responseSize.Observe(float64(wrapper.bytesWritten))
			}
		})
	}
}

// responseWrapper wraps http.ResponseWriter to capture metrics
type responseWrapper struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

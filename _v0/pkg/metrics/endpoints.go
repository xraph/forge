package metrics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/router"
)

// =============================================================================
// ENDPOINT HANDLERS
// =============================================================================

// MetricsEndpointHandler handles metrics endpoint requests
type MetricsEndpointHandler struct {
	collector MetricsCollector
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
func NewMetricsEndpointHandler(collector MetricsCollector, config *EndpointConfig, logger logger.Logger) *MetricsEndpointHandler {
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
func (h *MetricsEndpointHandler) RegisterEndpoints(r common.Router) error {
	if !h.config.Enabled {
		return nil
	}

	group := r.Group(h.config.PrefixPath)

	// Register metrics endpoint
	if err := group.RegisterOpinionatedHandler("GET", h.config.MetricsPath, h.handleMetrics,
		router.WithOpenAPITags("metrics"),
		router.WithSummary("Get metrics"),
		router.WithDescription("Returns application metrics in various formats"),
	); err != nil {
		return fmt.Errorf("failed to register metrics endpoint: %w", err)
	}

	// Register health endpoint
	if err := group.RegisterOpinionatedHandler("GET", h.config.HealthPath, h.handleHealth,
		router.WithOpenAPITags("metrics"),
		router.WithSummary("Get metrics health"),
		router.WithDescription("Returns health status of the metrics system"),
	); err != nil {
		return fmt.Errorf("failed to register health endpoint: %w", err)
	}

	// Register stats endpoint
	if err := group.RegisterOpinionatedHandler("GET", h.config.StatsPath, h.handleStats,
		router.WithOpenAPITags("metrics"),
		router.WithSummary("Get metrics statistics"),
		router.WithDescription("Returns statistics about the metrics collector"),
	); err != nil {
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
func (h *MetricsEndpointHandler) registerAdditionalEndpoints(r common.Router) error {
	// Metrics by type endpoint
	if err := r.RegisterOpinionatedHandler("GET", "/type/:type", h.handleMetricsByType,
		router.WithOpenAPITags("metrics"),
		router.WithSummary("Get metrics by type"),
		router.WithDescription("Returns metrics filtered by type (counter, gauge, histogram, timer)"),
	); err != nil {
		return err
	}

	// Metrics by name pattern endpoint
	if err := r.RegisterOpinionatedHandler("GET", "/name/:pattern", h.handleMetricsByName,
		router.WithOpenAPITags("metrics"),
		router.WithSummary("Get metrics by name pattern"),
		router.WithDescription("Returns metrics filtered by name pattern"),
	); err != nil {
		return err
	}

	// Metrics by tag endpoint
	if err := r.RegisterOpinionatedHandler("GET", "/tag/:key/:value", h.handleMetricsByTag,
		router.WithOpenAPITags("metrics"),
		router.WithSummary("Get metrics by tag"),
		router.WithDescription("Returns metrics filtered by tag key and value"),
	); err != nil {
		return err
	}

	// // Reset metrics endpoint
	// if err := r.POST("/reset", h.handleReset,
	// 	router.WithOpenAPITags("metrics"),
	// 	router.WithSummary("Reset metrics"),
	// 	router.WithDescription("Resets all metrics to their initial state"),
	// ); err != nil {
	// 	return err
	// }

	// Export metrics endpoint
	if err := r.RegisterOpinionatedHandler("GET", "/export/:format", h.handleExport,
		router.WithOpenAPITags("metrics"),
		router.WithSummary("Export metrics"),
		router.WithDescription("Exports metrics in specified format (prometheus, json, influx, statsd)"),
	); err != nil {
		return err
	}

	return nil
}

// =============================================================================
// HANDLER METHODS
// =============================================================================

type MetricsByTypeRequest struct {
	Type string `json:"type" validate:"required" path:"type"`
}

type MetricsResponse struct {
	Body map[string]interface{} `json:"body"`
}

type ExportMetricsRequest struct {
	Format string `json:"format" validate:"required" path:"format"`
}

type ExportMetricsResponse struct {
	Body []byte `json:"body"`
}

type GetMetricsInput struct {
	Format string `json:"format" query:"format"`
}

type GetMetricsResponse struct {
	Body map[string]any `json:"body"`
}

// handleMetrics handles the main metrics endpoint
func (h *MetricsEndpointHandler) handleMetrics(ctx common.Context, req GetMetricsInput) (*GetMetricsResponse, error) {
	// Apply CORS if enabled
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	// Check authentication if required
	if h.config.RequireAuth {
		if err := h.checkAuth(ctx); err != nil {
			return nil, h.writeError(ctx, http.StatusUnauthorized, "authentication required", err)
		}
	}

	// Get format from query parameter
	format := h.getFormatFromQuery(req.Format)

	// Check cache
	if h.cache.enabled {
		if cached, found := h.cache.get("metrics", format); found {
			return nil, h.writeResponse(ctx, cached, format)
		}
	}

	// Get metrics
	metrics := h.collector.GetMetrics()

	// // Export in requested format
	// data, err := h.exportMetrics(metrics, format)
	// if err != nil {
	// 	return nil, h.writeError(ctx, http.StatusInternalServerError, "failed to export metrics", err)
	// }

	// Cache the result
	// if h.cache.enabled {
	// 	h.cache.set("metrics", format, data)
	// }

	return &GetMetricsResponse{
		Body: metrics,
	}, nil
}

type MetricsHealthResponse struct {
	Body map[string]interface{} `json:"body"`
}

// handleHealth handles the health endpoint
func (h *MetricsEndpointHandler) handleHealth(ctx common.Context, req interface{}) (*MetricsHealthResponse, error) {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	// Check collector health
	if err := h.collector.OnHealthCheck(ctx); err != nil {
		response := map[string]interface{}{
			"status":    "unhealthy",
			"error":     err.Error(),
			"timestamp": time.Now().UTC(),
		}
		return nil, ctx.JSON(http.StatusServiceUnavailable, response)
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

	return &MetricsHealthResponse{
		Body: response,
	}, nil
}

type MetricsStatsResponse struct {
	Body CollectorStats `json:"body"`
}

// handleStats handles the stats endpoint
func (h *MetricsEndpointHandler) handleStats(ctx common.Context, req interface{}) (*MetricsStatsResponse, error) {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	stats := h.collector.GetStats()
	return &MetricsStatsResponse{
		Body: stats,
	}, nil
}

// handleMetricsByType handles metrics filtering by type
func (h *MetricsEndpointHandler) handleMetricsByType(ctx common.Context, req *MetricsByTypeRequest) (*MetricsResponse, error) {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	typeParam := req.Type
	if typeParam == "" {
		return nil, h.writeError(ctx, http.StatusBadRequest, "type parameter is required", nil)
	}

	// Parse metric type
	var metricType MetricType
	switch strings.ToLower(typeParam) {
	case "counter":
		metricType = MetricTypeCounter
	case "gauge":
		metricType = MetricTypeGauge
	case "histogram":
		metricType = MetricTypeHistogram
	case "timer":
		metricType = MetricTypeTimer
	default:
		return nil, h.writeError(ctx, http.StatusBadRequest, "invalid metric type", nil)
	}

	metrics := h.collector.GetMetricsByType(metricType)

	return &MetricsResponse{
		Body: metrics,
	}, nil
}

type MetricsByNameRequest struct {
	Pattern string `json:"pattern" validate:"required" path:"pattern"`
}

// handleMetricsByName handles metrics filtering by name pattern
func (h *MetricsEndpointHandler) handleMetricsByName(ctx common.Context, req *MetricsByNameRequest) (*MetricsResponse, error) {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	if req.Pattern == "" {
		return nil, h.writeError(ctx, http.StatusBadRequest, "pattern parameter is required", nil)
	}

	// This would typically use the registry's GetMetricsByNamePattern method
	// For now, we'll return all metrics as a placeholder
	metrics := h.collector.GetMetrics()

	// Filter by pattern (simple implementation)
	filtered := make(map[string]interface{})
	for name, value := range metrics {
		if h.matchesPattern(name, req.Pattern) {
			filtered[name] = value
		}
	}

	return &MetricsResponse{
		Body: filtered,
	}, nil
}

type MetricsByTagRequest struct {
	Key   string `json:"key" validate:"required" path:"key"`
	Value string `json:"value" validate:"required" path:"value"`
}

// handleMetricsByTag handles metrics filtering by tag
func (h *MetricsEndpointHandler) handleMetricsByTag(ctx common.Context, req MetricsByTagRequest) (*MetricsResponse, error) {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	tagKey := req.Key
	tagValue := req.Value

	if tagKey == "" || tagValue == "" {
		return nil, h.writeError(ctx, http.StatusBadRequest, "both key and value parameters are required", nil)
	}

	metrics := h.collector.GetMetricsByTag(tagKey, tagValue)

	return &MetricsResponse{
		Body: metrics,
	}, nil
}

// handleReset handles metrics reset
func (h *MetricsEndpointHandler) handleReset(ctx common.Context) error {
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
func (h *MetricsEndpointHandler) handleExport(ctx common.Context, req *ExportMetricsRequest) (*ExportMetricsResponse, error) {
	if h.config.EnableCORS {
		h.applyCORS(ctx)
	}

	formatParam := req.Format
	if formatParam == "" {
		return nil, h.writeError(ctx, http.StatusBadRequest, "format parameter is required", nil)
	}

	// Parse export format
	var format ExportFormat
	switch strings.ToLower(formatParam) {
	case "prometheus":
		format = ExportFormatPrometheus
	case "json":
		format = ExportFormatJSON
	case "influx":
		format = ExportFormatInflux
	case "statsd":
		format = ExportFormatStatsD
	default:
		return nil, h.writeError(ctx, http.StatusBadRequest, "invalid export format", nil)
	}

	// Export metrics
	data, err := h.collector.Export(format)
	if err != nil {
		return nil, h.writeError(ctx, http.StatusInternalServerError, "failed to export metrics", err)
	}

	return &ExportMetricsResponse{
		Body: data,
	}, nil
}

// =============================================================================
// HELPER METHODS
// =============================================================================

// applyCORS applies CORS headers
func (h *MetricsEndpointHandler) applyCORS(ctx common.Context) {
	ctx.Header("Access-Control-Allow-Origin", "*")
	ctx.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	ctx.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
	ctx.Header("Access-Control-Max-Age", "86400")
}

// checkAuth checks authentication
func (h *MetricsEndpointHandler) checkAuth(ctx common.Context) error {
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
		return h.collector.Export(ExportFormatPrometheus)
	case "json":
		return json.Marshal(metrics)
	case "influx":
		return h.collector.Export(ExportFormatInflux)
	case "statsd":
		return h.collector.Export(ExportFormatStatsD)
	default:
		return json.Marshal(metrics)
	}
}

// writeResponse writes the response with appropriate headers
func (h *MetricsEndpointHandler) writeResponse(ctx common.Context, data []byte, format string) error {
	// Set content type based on format
	switch format {
	case "prometheus":
		ctx.Header("Content-Type", "text/plain; version=0.0.4")
	case "json":
		ctx.Header("Content-Type", "application/json")
	case "influx":
		ctx.Header("Content-Type", "text/plain")
	case "statsd":
		ctx.Header("Content-Type", "text/plain")
	default:
		ctx.Header("Content-Type", "application/json")
	}

	// Set cache headers
	if h.cache.enabled {
		ctx.Header("Cache-Control", fmt.Sprintf("max-age=%d", int(h.config.CacheDuration.Seconds())))
	}

	ctx.ResponseWriter().WriteHeader(http.StatusOK)
	_, err := ctx.ResponseWriter().Write(data)
	return err
}

// writeError writes an error response
func (h *MetricsEndpointHandler) writeError(ctx common.Context, status int, message string, err error) error {
	response := map[string]interface{}{
		"error":     message,
		"timestamp": time.Now().UTC(),
	}

	if err != nil {
		response["details"] = err.Error()
	}

	if h.logger != nil {
		h.logger.Error("metrics endpoint error",
			logger.String("path", ctx.Path()),
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
func RegisterMetricsEndpoints(router common.Router, collector MetricsCollector, config *EndpointConfig, logger logger.Logger) error {
	handler := NewMetricsEndpointHandler(collector, config, logger)
	return handler.RegisterEndpoints(router)
}

// MetricsMiddleware creates middleware for collecting HTTP metrics
func MetricsMiddleware(collector MetricsCollector) func(next http.Handler) http.Handler {
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

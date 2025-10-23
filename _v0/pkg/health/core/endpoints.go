package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	"github.com/xraph/forge/v0/pkg/router"
)

// HealthEndpointManager manages HTTP endpoints for health checks
type HealthEndpointManager struct {
	healthService HealthService
	logger        common.Logger
	metrics       common.Metrics
	config        *EndpointConfig
}

// EndpointConfig contains configuration for health endpoints
type EndpointConfig struct {
	PathPrefix        string            `yaml:"path_prefix" json:"path_prefix"`
	EnableDetailed    bool              `yaml:"enable_detailed" json:"enable_detailed"`
	EnableMetrics     bool              `yaml:"enable_metrics" json:"enable_metrics"`
	EnableLiveness    bool              `yaml:"enable_liveness" json:"enable_liveness"`
	EnableReadiness   bool              `yaml:"enable_readiness" json:"enable_readiness"`
	EnableInfo        bool              `yaml:"enable_info" json:"enable_info"`
	CacheMaxAge       int               `yaml:"cache_max_age" json:"cache_max_age"`
	Headers           map[string]string `yaml:"headers" json:"headers"`
	EnableCORS        bool              `yaml:"enable_cors" json:"enable_cors"`
	CORSOrigins       []string          `yaml:"cors_origins" json:"cors_origins"`
	AuthEnabled       bool              `yaml:"auth_enabled" json:"auth_enabled"`
	AuthToken         string            `yaml:"auth_token" json:"auth_token"`
	EnableCompression bool              `yaml:"enable_compression" json:"enable_compression"`
	ResponseTimeout   time.Duration     `yaml:"response_timeout" json:"response_timeout"`
}

// DefaultEndpointConfig returns default configuration for health endpoints
func DefaultEndpointConfig() *EndpointConfig {
	return &EndpointConfig{
		PathPrefix:        "/health",
		EnableDetailed:    true,
		EnableMetrics:     true,
		EnableLiveness:    true,
		EnableReadiness:   true,
		EnableInfo:        true,
		CacheMaxAge:       30,
		Headers:           make(map[string]string),
		EnableCORS:        true,
		CORSOrigins:       []string{"*"},
		AuthEnabled:       false,
		AuthToken:         "",
		EnableCompression: true,
		ResponseTimeout:   10 * time.Second,
	}
}

// NewHealthEndpointManager creates a new health endpoint manager
func NewHealthEndpointManager(healthService HealthService, logger common.Logger, metrics common.Metrics, config *EndpointConfig) *HealthEndpointManager {
	if config == nil {
		config = DefaultEndpointConfig()
	}

	return &HealthEndpointManager{
		healthService: healthService,
		logger:        logger,
		metrics:       metrics,
		config:        config,
	}
}

// RegisterEndpoints registers health endpoints with the router
func (hem *HealthEndpointManager) RegisterEndpoints(r common.Router) error {
	fmt.Println("Registering health endpoints", hem.config.PathPrefix)
	group := r.Group(hem.config.PathPrefix)
	// group.UseMiddleware(hem.wrapHandler)

	// Register base health endpoint
	if err := group.RegisterOpinionatedHandler("GET", "", hem.healthHandler,
		router.WithOpenAPITags("health"),
	); err != nil {
		return fmt.Errorf("failed to register health endpoint: %w", err)
	}

	// Register detailed health endpoint
	if hem.config.EnableDetailed {
		if err := r.RegisterOpinionatedHandler("GET", hem.config.PathPrefix+"/detailed",
			hem.detailedHealthHandler,
			router.WithOpenAPITags("health"),
		); err != nil {
			return fmt.Errorf("failed to register detailed health endpoint: %w", err)
		}
	}

	// Register individual service endpoints
	if err := group.RegisterOpinionatedHandler("GET", "/services/:name", hem.serviceHealthHandler,
		router.WithOpenAPITags("health"),
	); err != nil {
		return fmt.Errorf("failed to register service health endpoint: %w", err)
	}

	// Register liveness endpoint
	if hem.config.EnableLiveness {
		if err := r.RegisterOpinionatedHandler("GET", "/live", hem.livenessHandler,
			router.WithOpenAPITags("health"),
		); err != nil {
			return fmt.Errorf("failed to register liveness endpoint: %w", err)
		}
	}

	// Register readiness endpoint
	if hem.config.EnableReadiness {
		if err := r.RegisterOpinionatedHandler("GET", "/ready", hem.readinessHandler,
			router.WithOperation("GET"),
			router.WithSummary("ready"),
			router.WithOpenAPITags("health"),
		); err != nil {
			return fmt.Errorf("failed to register readiness endpoint: %w", err)
		}
	}

	// Register info endpoint
	if hem.config.EnableInfo {
		if err := group.RegisterOpinionatedHandler("GET", "/info", hem.infoHandler,
			router.WithOpenAPITags("health")); err != nil {
			return fmt.Errorf("failed to register info endpoint: %w", err)
		}
	}

	// Register stats endpoint
	if hem.config.EnableMetrics {
		if err := group.RegisterOpinionatedHandler("GET", "/stats", hem.statsHandler,
			router.WithOpenAPITags("health")); err != nil {
			return fmt.Errorf("failed to register stats endpoint: %w", err)
		}
	}

	if hem.logger != nil {
		hem.logger.Info("health endpoints registered",
			logger.String("path_prefix", hem.config.PathPrefix),
			logger.Bool("detailed", hem.config.EnableDetailed),
			logger.Bool("liveness", hem.config.EnableLiveness),
			logger.Bool("readiness", hem.config.EnableReadiness),
			logger.Bool("info", hem.config.EnableInfo),
			logger.Bool("metrics", hem.config.EnableMetrics),
		)
	}

	return nil
}

// wrapHandler wraps health handlers with common functionality
func (hem *HealthEndpointManager) wrapHandler(handler func(http.ResponseWriter, *http.Request)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Set common headers
		hem.setCommonHeaders(w)

		// Handle CORS
		if hem.config.EnableCORS {
			hem.handleCORS(w, r)
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Handle authentication
		if hem.config.AuthEnabled {
			if !hem.authenticateRequest(r) {
				hem.sendError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
		}

		// Create context with timeout
		ctx, cancel := context.WithTimeout(r.Context(), hem.config.ResponseTimeout)
		defer cancel()

		// Update request context
		r = r.WithContext(ctx)

		// Call the handler
		handler(w, r)

		// Record metrics
		if hem.metrics != nil {
			duration := time.Since(start)
			hem.metrics.Counter("forge.health.endpoint_requests").Inc()
			hem.metrics.Histogram("forge.health.endpoint_duration").Observe(duration.Seconds())
		}
	}
}

// setCommonHeaders sets common HTTP headers
func (hem *HealthEndpointManager) setCommonHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")

	if hem.config.CacheMaxAge > 0 {
		w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", hem.config.CacheMaxAge))
	} else {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}

	// Set custom headers
	for key, value := range hem.config.Headers {
		w.Header().Set(key, value)
	}
}

// handleCORS handles CORS headers
func (hem *HealthEndpointManager) handleCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}

	// Check if origin is allowed
	allowed := false
	for _, allowedOrigin := range hem.config.CORSOrigins {
		if allowedOrigin == "*" || allowedOrigin == origin {
			allowed = true
			break
		}
	}

	if allowed {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
	}
}

// authenticateRequest authenticates the request
func (hem *HealthEndpointManager) authenticateRequest(r *http.Request) bool {
	if hem.config.AuthToken == "" {
		return true
	}

	// Check Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return false
	}

	// Check Bearer token
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		return token == hem.config.AuthToken
	}

	// Check query parameter
	if r.URL.Query().Get("token") == hem.config.AuthToken {
		return true
	}

	return false
}

type HealthStatusOutput struct {
	Status    HealthStatus `json:"status"`
	Timestamp string       `json:"timestamp"`
}
type HealthStatusInput struct {
	Status    HealthStatus `json:"status"`
	Timestamp string       `json:"timestamp"`
}

// healthHandler handles the basic health endpoint
func (hem *HealthEndpointManager) healthHandler(ctx common.Context, input HealthStatusInput) (*HealthStatusOutput, error) {
	status := hem.healthService.GetStatus()

	// response := map[string]interface{}{
	// 	"status":    status,
	// 	"timestamp": time.Now().Format(time.RFC3339),
	// }

	// statusCode := hem.getStatusCode(status)
	return &HealthStatusOutput{
		Status:    status,
		Timestamp: time.Now().Format(time.RFC3339),
	}, nil
}

type DetailedHealthStatusInput struct {
}

type DetailedHealthStatusOutput struct {
	Body       *HealthReport `json:"overall"`
	StatusCode int           `json:"status_code"`
}

// detailedHealthHandler handles the detailed health endpoint
func (hem *HealthEndpointManager) detailedHealthHandler(ctx common.Context, input DetailedHealthStatusInput) (*DetailedHealthStatusOutput, error) {
	report := hem.healthService.CheckAll(ctx)

	statusCode := hem.getStatusCode(report.Overall)
	return &DetailedHealthStatusOutput{
		Body:       report,
		StatusCode: statusCode,
	}, nil
}

type ServiceHealthInput struct {
	Name string `json:"-" path:"name"`
}

type ServiceHealthOutput struct {
	Body *HealthResult
}

// serviceHealthHandler handles individual service health checks
func (hem *HealthEndpointManager) serviceHealthHandler(ctx common.Context, input ServiceHealthInput) (*ServiceHealthOutput, error) {
	serviceName := input.Name
	if serviceName == "" {
		return nil, fmt.Errorf("service name is required")
	}

	result := hem.healthService.CheckOne(ctx, serviceName)

	return &ServiceHealthOutput{
		Body: result,
	}, nil
}

type LivenessInput struct{}

type LivenessOutput struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	Uptime    string `json:"uptime"`
}

// livenessHandler handles the liveness endpoint
func (hem *HealthEndpointManager) livenessHandler(ctx common.Context, input LivenessInput) (*LivenessOutput, error) {
	output := &LivenessOutput{
		Status:    "alive",
		Timestamp: time.Now().Format(time.RFC3339),
		Uptime:    time.Since(hem.healthService.Checker().StartTime()).String(),
	}
	return output, nil
}

type ReadinessInput struct{}

type ReadinessOutput struct {
	Status    HealthStatus `json:"status"`
	Ready     bool         `json:"ready"`
	Timestamp string       `json:"timestamp"`
}

// readinessHandler handles the readiness endpoint
func (hem *HealthEndpointManager) readinessHandler(ctx common.Context, input ReadinessInput) (*ReadinessOutput, error) {
	status := hem.healthService.GetStatus()
	output := &ReadinessOutput{
		Status:    status,
		Ready:     status == HealthStatusHealthy,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if status != HealthStatusHealthy {
		return output, fmt.Errorf("service not ready")
	}

	return output, nil
}

type InfoInput struct{}

type InfoOutput struct {
	Service          string       `json:"service"`
	Version          string       `json:"version"`
	Environment      string       `json:"environment"`
	Hostname         string       `json:"hostname"`
	Uptime           string       `json:"uptime"`
	RegisteredChecks int          `json:"registered_checks"`
	Subscribers      int          `json:"subscribers"`
	LastReportTime   string       `json:"last_report_time"`
	OverallStatus    HealthStatus `json:"overall_status"`
	Timestamp        string       `json:"timestamp"`
}

// infoHandler handles the info endpoint
func (hem *HealthEndpointManager) infoHandler(ctx common.Context, input InfoInput) (*InfoOutput, error) {
	stats := hem.healthService.GetStats()

	output := &InfoOutput{
		Service:          hem.healthService.Name(),
		Version:          hem.healthService.Checker().Version(),
		Environment:      hem.healthService.Checker().Environment(),
		Hostname:         hem.healthService.Checker().Hostname(),
		Uptime:           stats.Uptime.String(),
		RegisteredChecks: stats.RegisteredChecks,
		Subscribers:      stats.Subscribers,
		LastReportTime:   stats.LastReportTime.Format(time.RFC3339),
		OverallStatus:    stats.OverallStatus,
		Timestamp:        time.Now().Format(time.RFC3339),
	}

	return output, nil
}

type StatsInput struct{}

type HealthStatusSummary struct {
	TotalChecks    int          `json:"total_checks"`
	OverallStatus  HealthStatus `json:"overall_status"`
	LastReportTime string       `json:"last_report_time"`
	HealthyCount   int          `json:"healthy_count,omitempty"`
	DegradedCount  int          `json:"degraded_count,omitempty"`
	UnhealthyCount int          `json:"unhealthy_count,omitempty"`
	CriticalCount  int          `json:"critical_count,omitempty"`
}

type StatsOutput struct {
	Stats   *HealthCheckerStats  `json:"stats"`
	Summary *HealthStatusSummary `json:"summary"`
}

// statsHandler handles the stats endpoint
func (hem *HealthEndpointManager) statsHandler(ctx common.Context, input StatsInput) (*StatsOutput, error) {
	stats := hem.healthService.GetStats()
	report := hem.healthService.GetLastReport()

	output := &StatsOutput{
		Stats: stats,
		Summary: &HealthStatusSummary{
			TotalChecks:    stats.RegisteredChecks,
			OverallStatus:  stats.OverallStatus,
			LastReportTime: stats.LastReportTime.Format(time.RFC3339),
		},
	}

	if report != nil {
		output.Summary.HealthyCount = report.GetHealthyCount()
		output.Summary.DegradedCount = report.GetDegradedCount()
		output.Summary.UnhealthyCount = report.GetUnhealthyCount()
		output.Summary.CriticalCount = report.GetCriticalCount()
	}

	return output, nil
}

// getStatusCode converts health status to HTTP status code
func (hem *HealthEndpointManager) getStatusCode(status HealthStatus) int {
	switch status {
	case HealthStatusHealthy:
		return http.StatusOK
	case HealthStatusDegraded:
		return http.StatusOK // 200 but with degraded status
	case HealthStatusUnhealthy:
		return http.StatusServiceUnavailable
	case HealthStatusUnknown:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

// sendJSON sends a JSON response
func (hem *HealthEndpointManager) sendJSON(w http.ResponseWriter, statusCode int, data interface{}) {
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		if hem.logger != nil {
			hem.logger.Error("failed to encode JSON response",
				logger.Error(err),
			)
		}

		// Send error response
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "failed to encode response"}`)
	}
}

// sendError sends an error response
func (hem *HealthEndpointManager) sendError(w http.ResponseWriter, statusCode int, message string) {
	response := map[string]interface{}{
		"error":     message,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	hem.sendJSON(w, statusCode, response)
}

// getPathParam extracts a path parameter from the request
func (hem *HealthEndpointManager) getPathParam(r *http.Request, param string) string {
	// This is a simplified implementation
	// In a real implementation, you would use the router's parameter extraction
	path := r.URL.Path
	segments := strings.Split(path, "/")

	// Look for the parameter in the path
	for i, segment := range segments {
		if segment == ":"+param && i+1 < len(segments) {
			return segments[i+1]
		}
	}

	return ""
}

// HealthEndpointHandlers provides direct handler functions for integration
type HealthEndpointHandlers struct {
	manager *HealthEndpointManager
}

// NewHealthEndpointHandlers creates new health endpoint handlers
func NewHealthEndpointHandlers(manager *HealthEndpointManager) *HealthEndpointHandlers {
	return &HealthEndpointHandlers{
		manager: manager,
	}
}

// HealthHandler returns the health handler function
func (heh *HealthEndpointHandlers) HealthHandler() func(ctx common.Context, input HealthStatusInput) (*HealthStatusOutput, error) {
	return heh.manager.healthHandler
}

// DetailedHealthHandler returns the detailed health handler function
func (heh *HealthEndpointHandlers) DetailedHealthHandler() func(ctx common.Context, input DetailedHealthStatusInput) (*DetailedHealthStatusOutput, error) {
	return heh.manager.detailedHealthHandler
}

// ServiceHealthHandler returns the service health handler function
func (heh *HealthEndpointHandlers) ServiceHealthHandler() func(ctx common.Context, input ServiceHealthInput) (*ServiceHealthOutput, error) {
	return heh.manager.serviceHealthHandler
}

// LivenessHandler returns the liveness handler function
func (heh *HealthEndpointHandlers) LivenessHandler() func(ctx common.Context, input LivenessInput) (*LivenessOutput, error) {
	return heh.manager.livenessHandler
}

// ReadinessHandler returns the readiness handler function
func (heh *HealthEndpointHandlers) ReadinessHandler() func(ctx common.Context, input ReadinessInput) (*ReadinessOutput, error) {
	return heh.manager.readinessHandler
}

// InfoHandler returns the info handler function
func (heh *HealthEndpointHandlers) InfoHandler() func(ctx common.Context, input InfoInput) (*InfoOutput, error) {
	return heh.manager.infoHandler
}

// StatsHandler returns the stats handler function
func (heh *HealthEndpointHandlers) StatsHandler() func(ctx common.Context, input StatsInput) (*StatsOutput, error) {
	return heh.manager.statsHandler
}

// CreateHealthEndpoints creates health endpoints for a router
func CreateHealthEndpoints(router common.Router, healthService HealthService, logger common.Logger, metrics common.Metrics) error {
	config := DefaultEndpointConfig()
	manager := NewHealthEndpointManager(healthService, logger, metrics, config)

	return manager.RegisterEndpoints(router)
}

// CreateHealthEndpointsWithConfig creates health endpoints with custom configuration
func CreateHealthEndpointsWithConfig(router common.Router, healthService HealthService, logger common.Logger, metrics common.Metrics, config *EndpointConfig) error {
	manager := NewHealthEndpointManager(healthService, logger, metrics, config)

	return manager.RegisterEndpoints(router)
}

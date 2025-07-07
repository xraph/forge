package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime/debug"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xraph/forge/core"
	"github.com/xraph/forge/logger"
	"github.com/xraph/forge/observability"
	"github.com/xraph/forge/router"
)

// ApplicationMetrics implementation
type applicationMetrics struct {
	metrics observability.Metrics
	logger  logger.Logger
}

func NewApplicationMetrics(metrics observability.Metrics, logger logger.Logger) ApplicationMetrics {
	return &applicationMetrics{
		metrics: metrics,
		logger:  logger,
	}
}

func (am *applicationMetrics) RecordStartup(duration float64) {
	am.metrics.ObserveHistogram("app_startup_duration_seconds", duration)
	am.metrics.IncrementCounter("app_startup_total", 1)
}

func (am *applicationMetrics) RecordShutdown(duration float64) {
	am.metrics.ObserveHistogram("app_shutdown_duration_seconds", duration)
	am.metrics.IncrementCounter("app_shutdown_total", 1)
}

func (am *applicationMetrics) RecordRequest(method, path string, status int, duration float64) {
	labels := []observability.Label{
		{Name: "method", Value: method},
		{Name: "path", Value: path},
		{Name: "status", Value: fmt.Sprintf("%d", status)},
	}
	am.metrics.IncrementCounter("http_requests_total", 1, labels...)
	am.metrics.ObserveHistogram("http_request_duration_seconds", duration, labels...)
}

func (am *applicationMetrics) RecordDatabaseQuery(database, operation string, duration float64) {
	labels := []observability.Label{
		{Name: "database", Value: database},
		{Name: "operation", Value: operation},
	}
	am.metrics.IncrementCounter("database_queries_total", 1, labels...)
	am.metrics.ObserveHistogram("database_query_duration_seconds", duration, labels...)
}

func (am *applicationMetrics) RecordCacheOperation(cache, operation string, hit bool, duration float64) {
	labels := []observability.Label{
		{Name: "cache", Value: cache},
		{Name: "operation", Value: operation},
		{Name: "result", Value: map[bool]string{true: "hit", false: "miss"}[hit]},
	}
	am.metrics.IncrementCounter("cache_operations_total", 1, labels...)
	am.metrics.ObserveHistogram("cache_operation_duration_seconds", duration, labels...)
}

func (am *applicationMetrics) RecordJobExecution(jobType string, success bool, duration float64) {
	labels := []observability.Label{
		{Name: "job_type", Value: jobType},
		{Name: "status", Value: map[bool]string{true: "success", false: "failure"}[success]},
	}
	am.metrics.IncrementCounter("job_executions_total", 1, labels...)
	am.metrics.ObserveHistogram("job_execution_duration_seconds", duration, labels...)
}

func (am *applicationMetrics) RecordMemoryUsage(bytes int64) {
	am.metrics.SetGauge("memory_usage_bytes", float64(bytes))
}

func (am *applicationMetrics) RecordCPUUsage(percent float64) {
	am.metrics.SetGauge("cpu_usage_percent", percent)
}

func (am *applicationMetrics) RecordGoroutineCount(count int) {
	am.metrics.SetGauge("goroutines_total", float64(count))
}

func (am *applicationMetrics) RecordGCDuration(duration float64) {
	am.metrics.ObserveHistogram("gc_duration_seconds", duration)
}

func (am *applicationMetrics) Counter(name string) observability.Counter {
	return am.metrics.Counter(name)
}

func (am *applicationMetrics) Gauge(name string) observability.Gauge {
	return am.metrics.Gauge(name)
}

func (am *applicationMetrics) Histogram(name string) observability.Histogram {
	return am.metrics.Histogram(name, []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10})
}

func (am *applicationMetrics) Summary(name string) observability.Summary {
	return am.metrics.Summary(name, map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001})
}

// Event Bus implementation
type eventBus struct {
	handlers map[ApplicationEventType][]ApplicationEventHandler
	mu       sync.RWMutex
	logger   logger.Logger
	stopped  bool
}

func NewEventBus(logger logger.Logger) ApplicationEventBus {
	return &eventBus{
		handlers: make(map[ApplicationEventType][]ApplicationEventHandler),
		logger:   logger.Named("event-bus"),
	}
}

func (eb *eventBus) Subscribe(eventType ApplicationEventType, handler ApplicationEventHandler) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	if eb.handlers[eventType] == nil {
		eb.handlers[eventType] = make([]ApplicationEventHandler, 0)
	}
	eb.handlers[eventType] = append(eb.handlers[eventType], handler)

	eb.logger.Debug("Event handler subscribed",
		logger.String("event_type", string(eventType)),
	)
	return nil
}

func (eb *eventBus) Unsubscribe(eventType ApplicationEventType, handler ApplicationEventHandler) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	handlers := eb.handlers[eventType]
	for i, h := range handlers {
		if h == handler {
			eb.handlers[eventType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}

	eb.logger.Debug("Event handler unsubscribed",
		logger.String("event_type", string(eventType)),
	)
	return nil
}

func (eb *eventBus) Publish(ctx context.Context, event ApplicationEvent) error {
	eb.mu.RLock()
	handlers := eb.handlers[event.Type]
	eb.mu.RUnlock()

	if eb.stopped {
		return nil
	}

	for _, handler := range handlers {
		if handler.CanHandle(event.Type) {
			if err := handler.HandleEvent(ctx, event); err != nil {
				eb.logger.Error("Event handler error",
					logger.String("event_type", string(event.Type)),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

func (eb *eventBus) PublishAsync(ctx context.Context, event ApplicationEvent) error {
	go func() {
		if err := eb.Publish(ctx, event); err != nil {
			eb.logger.Error("Async event publish failed",
				logger.String("event_type", string(event.Type)),
				logger.Error(err),
			)
		}
	}()
	return nil
}

func (eb *eventBus) Start(ctx context.Context) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.stopped = false
	eb.logger.Info("Event bus started")
	return nil
}

func (eb *eventBus) Stop(ctx context.Context) error {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	eb.stopped = true
	eb.logger.Info("Event bus stopped")
	return nil
}

// Service Manager implementation
type serviceManager struct {
	services  map[string]interface{}
	container core.Container
	logger    logger.Logger
	mu        sync.RWMutex
}

func NewServiceManager(container core.Container, logger logger.Logger) ServiceManager {
	return &serviceManager{
		services:  make(map[string]interface{}),
		container: container,
		logger:    logger.Named("service-manager"),
	}
}

func (sm *serviceManager) RegisterService(name string, service interface{}) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.services[name] = service
	sm.logger.Debug("Service registered", logger.String("name", name))
	return nil
}

func (sm *serviceManager) UnregisterService(name string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.services, name)
	sm.logger.Debug("Service unregistered", logger.String("name", name))
	return nil
}

func (sm *serviceManager) GetService(name string) (interface{}, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	service, exists := sm.services[name]
	if !exists {
		return nil, fmt.Errorf("service %s not found", name)
	}
	return service, nil
}

func (sm *serviceManager) ListServices() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]interface{})
	for name, service := range sm.services {
		result[name] = service
	}
	return result
}

func (sm *serviceManager) StartServices(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	for name, service := range sm.services {
		if starter, ok := service.(interface{ Start(context.Context) error }); ok {
			if err := starter.Start(ctx); err != nil {
				return fmt.Errorf("failed to start service %s: %w", name, err)
			}
			sm.logger.Debug("Service started", logger.String("name", name))
		}
	}
	return nil
}

func (sm *serviceManager) StopServices(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var errors []error
	for name, service := range sm.services {
		if stopper, ok := service.(interface{ Stop(context.Context) error }); ok {
			if err := stopper.Stop(ctx); err != nil {
				errors = append(errors, fmt.Errorf("failed to stop service %s: %w", name, err))
			} else {
				sm.logger.Debug("Service stopped", logger.String("name", name))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("multiple service stop errors: %v", errors)
	}
	return nil
}

func (sm *serviceManager) CheckServices(ctx context.Context) map[string]error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	results := make(map[string]error)
	for name, service := range sm.services {
		if checker, ok := service.(interface{ Health(context.Context) error }); ok {
			results[name] = checker.Health(ctx)
		}
	}
	return results
}

// Development Server implementation
type developmentServer struct {
	app       *application
	logger    logger.Logger
	hotReload bool
	dashboard *http.Server
	mu        sync.RWMutex
}

func NewDevelopmentServer(app *application, logger logger.Logger) DevelopmentServer {
	return &developmentServer{
		app:    app,
		logger: logger.Named("dev-server"),
	}
}

func (ds *developmentServer) EnableHotReload() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.hotReload = true
	ds.logger.Info("Hot reload enabled")
	return nil
}

func (ds *developmentServer) DisableHotReload() error {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.hotReload = false
	ds.logger.Info("Hot reload disabled")
	return nil
}

func (ds *developmentServer) IsHotReloadEnabled() bool {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.hotReload
}

func (ds *developmentServer) Reload() error {
	ds.logger.Info("Reloading application...")
	// Implementation would depend on specific reload strategy
	return nil
}

func (ds *developmentServer) OpenBrowser(path string) error {
	// Implementation would open system browser
	return nil
}

func (ds *developmentServer) ShowRoutes() []router.RouteInfo {
	// Implementation would return route information
	return []router.RouteInfo{}
}

func (ds *developmentServer) ShowMetrics() map[string]interface{} {
	// Implementation would return current metrics
	return map[string]interface{}{}
}

func (ds *developmentServer) ShowHealth() HealthReport {
	// Implementation would return current health status
	return HealthReport{}
}

func (ds *developmentServer) StartDashboard(port int) error {
	// Implementation would start development dashboard
	return nil
}

func (ds *developmentServer) StopDashboard() error {
	// Implementation would stop development dashboard
	return nil
}

func (ds *developmentServer) DashboardURL() string {
	return "http://localhost:8081"
}

// Middleware implementations

// RecoveryMiddleware recovers from panics and logs them
func RecoveryMiddleware(l logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := debug.Stack()
					l.Error("Panic recovered",
						logger.Any("panic", rec),
						logger.String("stack", string(stack)),
						logger.String("method", r.Method),
						logger.String("path", r.URL.Path),
						logger.String("remote_addr", r.RemoteAddr),
					)

					// Return error response
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(w).Encode(map[string]interface{}{
						"error":   "Internal server error",
						"code":    "INTERNAL_ERROR",
						"message": "An unexpected error occurred",
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDMiddleware adds a unique request ID to each request
func RequestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.New().String()
			}

			// Set response header
			w.Header().Set("X-Request-ID", requestID)

			// Add to context
			ctx := context.WithValue(r.Context(), router.RequestIDKey, requestID)
			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(l logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create response writer wrapper to capture status and size
			wrapped := &responseWriter{
				ResponseWriter: w,
				status:         200,
			}

			// Process request
			next.ServeHTTP(wrapped, r)

			// Log request
			duration := time.Since(start)
			requestID := r.Context().Value(router.RequestIDKey)

			fields := []logger.Field{
				logger.String("method", r.Method),
				logger.String("path", r.URL.Path),
				logger.Int("status", wrapped.status),
				logger.Duration("duration", duration),
				logger.String("remote_addr", r.RemoteAddr),
				logger.String("user_agent", r.UserAgent()),
				logger.Int64("response_size", wrapped.size),
			}

			if requestID != nil {
				fields = append(fields, logger.String("request_id", requestID.(string)))
			}

			if wrapped.status >= 500 {
				l.Error("HTTP request completed with server error", fields...)
			} else if wrapped.status >= 400 {
				l.Warn("HTTP request completed with client error", fields...)
			} else {
				l.Info("HTTP request completed", fields...)
			}
		})
	}
}

// MetricsMiddleware records HTTP metrics
func MetricsMiddleware(metrics observability.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Create response writer wrapper
			wrapped := &responseWriter{
				ResponseWriter: w,
				status:         200,
			}

			// Process request
			next.ServeHTTP(wrapped, r)

			// Record metrics
			duration := time.Since(start)
			labels := []observability.Label{
				{Name: "method", Value: r.Method},
				{Name: "path", Value: r.URL.Path},
				{Name: "status", Value: fmt.Sprintf("%d", wrapped.status)},
			}

			metrics.IncrementCounter("http_requests_total", 1, labels...)
			metrics.ObserveHistogram("http_request_duration_seconds", duration.Seconds(), labels...)
			metrics.ObserveHistogram("http_request_size_bytes", float64(r.ContentLength), labels...)
			metrics.ObserveHistogram("http_response_size_bytes", float64(wrapped.size), labels...)
		})
	}
}

// TracingMiddleware adds distributed tracing
func TracingMiddleware(tracer observability.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract trace context from headers
			ctx := tracer.Extract(r.Context(), r.Header)

			// Start new span
			spanName := fmt.Sprintf("%s %s", r.Method, r.URL.Path)
			ctx, span := tracer.StartSpan(ctx, spanName)
			defer span.End()

			// Set span attributes
			span.SetAttribute("http.method", r.Method)
			span.SetAttribute("http.url", r.URL.String())
			span.SetAttribute("http.scheme", r.URL.Scheme)
			span.SetAttribute("http.host", r.Host)
			span.SetAttribute("http.target", r.URL.Path)
			span.SetAttribute("http.user_agent", r.UserAgent())
			span.SetAttribute("http.remote_addr", r.RemoteAddr)

			// Inject trace context into response headers
			tracer.Inject(ctx, w.Header())

			// Create response writer wrapper
			wrapped := &responseWriter{
				ResponseWriter: w,
				status:         200,
			}

			// Process request with trace context
			next.ServeHTTP(wrapped, r.WithContext(ctx))

			// Set final span attributes
			span.SetAttribute("http.status_code", wrapped.status)
			span.SetAttribute("http.response_size", wrapped.size)

			// Set span status based on HTTP status
			if wrapped.status >= 400 {
				span.SetStatus(observability.StatusCodeError, fmt.Sprintf("HTTP %d", wrapped.status))
			} else {
				span.SetStatus(observability.StatusCodeOK, "")
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture response information
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

// Additional helper implementations

// LoadConfigFromBytes loads configuration from byte slice
func LoadConfigFromBytes(data []byte) (core.Config, error) {
	// Implementation would parse configuration from bytes
	// This is a placeholder implementation
	return core.NewConfig(), nil
}

// ApplicationLogger implementation
type applicationLogger struct {
	logger.Logger
	app *application
}

func NewApplicationLogger(baseLogger logger.Logger, app *application) ApplicationLogger {
	return &applicationLogger{
		Logger: baseLogger,
		app:    app,
	}
}

func (al *applicationLogger) LogStartup(fields ...logger.Field) {
	allFields := append(fields,
		logger.String("app_name", al.app.name),
		logger.String("app_version", al.app.version),
		logger.String("environment", al.app.environment),
	)
	al.Info("Application startup", allFields...)
}

func (al *applicationLogger) LogShutdown(fields ...logger.Field) {
	allFields := append(fields,
		logger.String("app_name", al.app.name),
		logger.Duration("uptime", time.Since(al.app.startTime)),
	)
	al.Info("Application shutdown", allFields...)
}

func (al *applicationLogger) LogRequest(method, path string, status int, duration float64, fields ...logger.Field) {
	allFields := append(fields,
		logger.String("method", method),
		logger.String("path", path),
		logger.Int("status", status),
		logger.Float64("duration_ms", duration*1000),
	)
	al.Info("HTTP request", allFields...)
}

func (al *applicationLogger) LogError(err error, context string, fields ...logger.Field) {
	allFields := append(fields,
		logger.Error(err),
		logger.String("context", context),
	)
	al.Error("Application error", allFields...)
}

func (al *applicationLogger) LogPanic(recovered interface{}, context string, fields ...logger.Field) {
	allFields := append(fields,
		logger.Any("panic", recovered),
		logger.String("context", context),
		logger.String("stack", string(debug.Stack())),
	)
	al.Error("Application panic", allFields...)
}

func (al *applicationLogger) DatabaseLogger() logger.Logger {
	return al.Logger.Named("database")
}

func (al *applicationLogger) CacheLogger() logger.Logger {
	return al.Logger.Named("cache")
}

func (al *applicationLogger) JobsLogger() logger.Logger {
	return al.Logger.Named("jobs")
}

func (al *applicationLogger) PluginLogger(pluginName string) logger.Logger {
	return al.Logger.Named("plugin").Named(pluginName)
}

func (al *applicationLogger) LogDevelopment(msg string, fields ...logger.Field) {
	if al.app.environment == "development" {
		al.Debug(msg, fields...)
	}
}

func (al *applicationLogger) LogDebugRequest(req *http.Request, fields ...logger.Field) {
	if al.app.environment == "development" {
		allFields := append(fields,
			logger.String("method", req.Method),
			logger.String("url", req.URL.String()),
			logger.String("headers", fmt.Sprintf("%v", req.Header)),
		)
		al.Debug("Debug HTTP request", allFields...)
	}
}

func (al *applicationLogger) LogDebugResponse(status int, headers http.Header, fields ...logger.Field) {
	if al.app.environment == "development" {
		allFields := append(fields,
			logger.Int("status", status),
			logger.String("headers", fmt.Sprintf("%v", headers)),
		)
		al.Debug("Debug HTTP response", allFields...)
	}
}

// Default event handlers

// ApplicationEventLogger logs application events
type ApplicationEventLogger struct {
	logger logger.Logger
}

func NewApplicationEventLogger(logger logger.Logger) *ApplicationEventLogger {
	return &ApplicationEventLogger{
		logger: logger.Named("app-events"),
	}
}

func (ael *ApplicationEventLogger) HandleEvent(ctx context.Context, event ApplicationEvent) error {
	fields := []logger.Field{
		logger.String("event_type", string(event.Type)),
		logger.String("source", event.Source),
		logger.String("timestamp", event.Timestamp),
	}

	if event.Error != nil {
		fields = append(fields, logger.Error(event.Error))
	}

	if len(event.Data) > 0 {
		fields = append(fields, logger.Any("data", event.Data))
	}

	switch event.Type {
	case EventApplicationStarted, EventApplicationReady:
		ael.logger.Info("Application event", fields...)
	case EventApplicationError, EventApplicationUnhealthy:
		ael.logger.Error("Application event", fields...)
	default:
		ael.logger.Debug("Application event", fields...)
	}

	return nil
}

func (ael *ApplicationEventLogger) CanHandle(eventType ApplicationEventType) bool {
	return true // Handle all event types
}

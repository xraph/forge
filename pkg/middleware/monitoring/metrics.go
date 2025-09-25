package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/middleware"
)

// Config contains configuration for monitoring middleware
type Config struct {
	// Metrics collection
	EnableMetrics   bool `yaml:"enable_metrics" json:"enable_metrics" default:"true"`
	EnableTracing   bool `yaml:"enable_tracing" json:"enable_tracing" default:"true"`
	EnableLogging   bool `yaml:"enable_logging" json:"enable_logging" default:"true"`
	EnableProfiling bool `yaml:"enable_profiling" json:"enable_profiling" default:"false"`

	// Sampling
	SampleRate           float64       `yaml:"sample_rate" json:"sample_rate" default:"1.0"` // 1.0 = 100%
	SlowRequestThreshold time.Duration `yaml:"slow_request_threshold" json:"slow_request_threshold" default:"1s"`
	ErrorRequestLogging  bool          `yaml:"error_request_logging" json:"error_request_logging" default:"true"`

	// Performance tracking
	TrackMemory        bool          `yaml:"track_memory" json:"track_memory" default:"true"`
	TrackGoroutines    bool          `yaml:"track_goroutines" json:"track_goroutines" default:"true"`
	CollectionInterval time.Duration `yaml:"collection_interval" json:"collection_interval" default:"30s"`

	// Request tracking
	TrackUserAgents   bool     `yaml:"track_user_agents" json:"track_user_agents" default:"true"`
	TrackIPs          bool     `yaml:"track_ips" json:"track_ips" default:"true"`
	TrackHeaders      []string `yaml:"track_headers" json:"track_headers"`
	SkipPaths         []string `yaml:"skip_paths" json:"skip_paths"`
	GroupSimilarPaths bool     `yaml:"group_similar_paths" json:"group_similar_paths" default:"true"`

	// Alerting thresholds
	HighLatencyThreshold   time.Duration `yaml:"high_latency_threshold" json:"high_latency_threshold" default:"5s"`
	HighErrorRateThreshold float64       `yaml:"high_error_rate_threshold" json:"high_error_rate_threshold" default:"0.05"` // 5%
	HighMemoryThreshold    int64         `yaml:"high_memory_threshold" json:"high_memory_threshold" default:"1073741824"`   // 1GB
	HighGoroutineThreshold int           `yaml:"high_goroutine_threshold" json:"high_goroutine_threshold" default:"10000"`
}

// MonitoringMiddleware implements comprehensive monitoring middleware
type MonitoringMiddleware struct {
	*middleware.BaseServiceMiddleware
	config          Config
	collector       *MetricsCollector
	tracer          *RequestTracer
	profiler        *Profiler
	alertManager    *AlertManager
	performanceData *PerformanceData
	mu              sync.RWMutex
}

// MetricsCollector collects and aggregates metrics
type MetricsCollector struct {
	requests       map[string]*RequestMetrics
	systemMetrics  *SystemMetrics
	mu             sync.RWMutex
	lastCollection time.Time
}

// RequestMetrics contains metrics for HTTP requests
type RequestMetrics struct {
	Count         int64           `json:"count"`
	TotalLatency  time.Duration   `json:"total_latency"`
	MinLatency    time.Duration   `json:"min_latency"`
	MaxLatency    time.Duration   `json:"max_latency"`
	AvgLatency    time.Duration   `json:"avg_latency"`
	P50Latency    time.Duration   `json:"p50_latency"`
	P95Latency    time.Duration   `json:"p95_latency"`
	P99Latency    time.Duration   `json:"p99_latency"`
	ErrorCount    int64           `json:"error_count"`
	ErrorRate     float64         `json:"error_rate"`
	SuccessCount  int64           `json:"success_count"`
	SuccessRate   float64         `json:"success_rate"`
	BytesSent     int64           `json:"bytes_sent"`
	BytesReceived int64           `json:"bytes_received"`
	StatusCodes   map[int]int64   `json:"status_codes"`
	LastRequest   time.Time       `json:"last_request"`
	latencies     []time.Duration // For percentile calculation
}

// SystemMetrics contains system-level metrics
type SystemMetrics struct {
	MemoryUsage    int64     `json:"memory_usage"`
	MemoryTotal    int64     `json:"memory_total"`
	MemoryPercent  float64   `json:"memory_percent"`
	GoroutineCount int       `json:"goroutine_count"`
	CPUUsage       float64   `json:"cpu_usage"`
	GCPauses       int64     `json:"gc_pauses"`
	HeapSize       int64     `json:"heap_size"`
	StackSize      int64     `json:"stack_size"`
	LastUpdated    time.Time `json:"last_updated"`
}

// RequestTracer handles distributed tracing
type RequestTracer struct {
	activeSpans map[string]*Span
	mu          sync.RWMutex
}

// Span represents a distributed tracing span
type Span struct {
	TraceID   string                 `json:"trace_id"`
	SpanID    string                 `json:"span_id"`
	ParentID  string                 `json:"parent_id,omitempty"`
	Operation string                 `json:"operation"`
	StartTime time.Time              `json:"start_time"`
	EndTime   time.Time              `json:"end_time"`
	Duration  time.Duration          `json:"duration"`
	Status    string                 `json:"status"`
	Tags      map[string]interface{} `json:"tags"`
	Logs      []LogEntry             `json:"logs"`
}

// LogEntry represents a log entry within a span
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields"`
}

// Profiler handles performance profiling
type Profiler struct {
	enabled  bool
	profiles map[string]*ProfileData
	mu       sync.RWMutex
}

// ProfileData contains profiling information
type ProfileData struct {
	Name        string        `json:"name"`
	Duration    time.Duration `json:"duration"`
	MemoryUsage int64         `json:"memory_usage"`
	CPUUsage    float64       `json:"cpu_usage"`
	Timestamp   time.Time     `json:"timestamp"`
}

// AlertManager handles monitoring alerts
type AlertManager struct {
	config     Config
	alerts     []*Alert
	thresholds map[string]interface{}
	mu         sync.RWMutex
}

// Alert represents a monitoring alert
type Alert struct {
	ID         string                 `json:"id"`
	Level      string                 `json:"level"` // info, warning, error, critical
	Type       string                 `json:"type"`
	Message    string                 `json:"message"`
	Timestamp  time.Time              `json:"timestamp"`
	Resolved   bool                   `json:"resolved"`
	ResolvedAt time.Time              `json:"resolved_at,omitempty"`
	Metadata   map[string]interface{} `json:"metadata"`
}

// PerformanceData aggregates performance information
type PerformanceData struct {
	StartTime      time.Time     `json:"start_time"`
	Uptime         time.Duration `json:"uptime"`
	TotalRequests  int64         `json:"total_requests"`
	RequestRate    float64       `json:"request_rate"`
	ErrorRate      float64       `json:"error_rate"`
	AverageLatency time.Duration `json:"average_latency"`
	ThroughputMBps float64       `json:"throughput_mbps"`
	ActiveRequests int64         `json:"active_requests"`
	PeakMemory     int64         `json:"peak_memory"`
	PeakGoroutines int           `json:"peak_goroutines"`
}

// NewMonitoringMiddleware creates a new monitoring middleware
func NewMonitoringMiddleware(config Config) *MonitoringMiddleware {
	mm := &MonitoringMiddleware{
		BaseServiceMiddleware: middleware.NewBaseServiceMiddleware("monitoring", 25, []string{"config-manager", "logger", "metrics"}),
		config:                config,
		collector:             NewMetricsCollector(),
		tracer:                NewRequestTracer(),
		profiler:              NewProfiler(config.EnableProfiling),
		alertManager:          NewAlertManager(config),
		performanceData: &PerformanceData{
			StartTime: time.Now(),
		},
	}

	return mm
}

// Initialize initializes the monitoring middleware
func (mm *MonitoringMiddleware) Initialize(container common.Container) error {
	if err := mm.BaseServiceMiddleware.Initialize(container); err != nil {
		return err
	}

	// Load configuration from container if needed
	if configManager, err := container.Resolve((*common.ConfigManager)(nil)); err == nil {
		var monitoringConfig Config
		if err := configManager.(common.ConfigManager).Bind("middleware.monitoring", &monitoringConfig); err == nil {
			mm.config = monitoringConfig
		}
	}

	// Set defaults
	if mm.config.SampleRate == 0 {
		mm.config.SampleRate = 1.0
	}
	if mm.config.SlowRequestThreshold == 0 {
		mm.config.SlowRequestThreshold = time.Second
	}
	if mm.config.CollectionInterval == 0 {
		mm.config.CollectionInterval = 30 * time.Second
	}
	if mm.config.HighLatencyThreshold == 0 {
		mm.config.HighLatencyThreshold = 5 * time.Second
	}
	if mm.config.HighErrorRateThreshold == 0 {
		mm.config.HighErrorRateThreshold = 0.05
	}
	if mm.config.HighMemoryThreshold == 0 {
		mm.config.HighMemoryThreshold = 1073741824 // 1GB
	}
	if mm.config.HighGoroutineThreshold == 0 {
		mm.config.HighGoroutineThreshold = 10000
	}

	return nil
}

// OnStart starts the monitoring middleware
func (mm *MonitoringMiddleware) OnStart(ctx context.Context) error {
	if err := mm.BaseServiceMiddleware.OnStart(ctx); err != nil {
		return err
	}

	// OnStart background collection goroutine
	go mm.startPerformanceCollection(ctx)

	// OnStart alert monitoring
	go mm.startAlertMonitoring(ctx)

	return nil
}

// Handler returns the monitoring handler
func (mm *MonitoringMiddleware) Handler() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Update call count
			mm.UpdateStats(1, 0, 0, nil)

			// Check if path should be skipped
			if mm.shouldSkipPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				mm.UpdateStats(0, 0, time.Since(start), nil)
				return
			}

			// Apply sampling
			if !mm.shouldSample() {
				next.ServeHTTP(w, r)
				return
			}

			// OnStart tracing if enabled
			var span *Span
			if mm.config.EnableTracing {
				span = mm.tracer.StartSpan(r)
			}

			// OnStart profiling if enabled
			var profileStart time.Time
			var memBefore runtime.MemStats
			if mm.config.EnableProfiling {
				profileStart = time.Now()
				runtime.ReadMemStats(&memBefore)
			}

			// Wrap response writer to capture metrics
			wrapper := &monitoringResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				bytesWritten:   0,
			}

			// Update active requests
			mm.incrementActiveRequests()
			defer mm.decrementActiveRequests()

			// Execute request
			next.ServeHTTP(wrapper, r)

			// Calculate metrics
			duration := time.Since(start)
			mm.recordRequestMetrics(r, wrapper, duration)

			// End tracing
			if span != nil {
				mm.tracer.EndSpan(span, wrapper.statusCode, duration)
			}

			// Record profiling data
			if mm.config.EnableProfiling {
				var memAfter runtime.MemStats
				runtime.ReadMemStats(&memAfter)
				mm.profiler.RecordProfile(r.URL.Path, time.Since(profileStart), int64(memAfter.TotalAlloc-memBefore.TotalAlloc))
			}

			// Log request if enabled
			if mm.config.EnableLogging {
				mm.logRequest(r, wrapper, duration)
			}

			// Check for alerts
			mm.checkAlertConditions(r, wrapper, duration)

			// Update middleware statistics
			if wrapper.statusCode >= 400 {
				mm.UpdateStats(0, 1, duration, fmt.Errorf("HTTP %d", wrapper.statusCode))
			} else {
				mm.UpdateStats(0, 0, duration, nil)
			}
		})
	}
}

// recordRequestMetrics records metrics for a request
func (mm *MonitoringMiddleware) recordRequestMetrics(r *http.Request, w *monitoringResponseWriter, duration time.Duration) {
	mm.collector.mu.Lock()
	defer mm.collector.mu.Unlock()

	// Group similar paths if configured
	path := r.URL.Path
	if mm.config.GroupSimilarPaths {
		path = mm.groupPath(path)
	}

	key := fmt.Sprintf("%s %s", r.Method, path)

	metrics, exists := mm.collector.requests[key]
	if !exists {
		metrics = &RequestMetrics{
			MinLatency:  duration,
			StatusCodes: make(map[int]int64),
			latencies:   make([]time.Duration, 0),
		}
		mm.collector.requests[key] = metrics
	}

	// Update metrics
	metrics.Count++
	metrics.TotalLatency += duration
	metrics.LastRequest = time.Now()

	// Update latency stats
	if duration < metrics.MinLatency {
		metrics.MinLatency = duration
	}
	if duration > metrics.MaxLatency {
		metrics.MaxLatency = duration
	}

	// Store latency for percentile calculation (keep last 1000)
	metrics.latencies = append(metrics.latencies, duration)
	if len(metrics.latencies) > 1000 {
		metrics.latencies = metrics.latencies[1:]
	}

	// Update status code counts
	metrics.StatusCodes[w.statusCode]++

	// Update success/error counts
	if w.statusCode >= 400 {
		metrics.ErrorCount++
	} else {
		metrics.SuccessCount++
	}

	// Update rates
	metrics.ErrorRate = float64(metrics.ErrorCount) / float64(metrics.Count)
	metrics.SuccessRate = float64(metrics.SuccessCount) / float64(metrics.Count)

	// Update average latency
	metrics.AvgLatency = metrics.TotalLatency / time.Duration(metrics.Count)

	// Calculate percentiles
	mm.calculatePercentiles(metrics)

	// Update bytes
	metrics.BytesReceived += r.ContentLength
	metrics.BytesSent += int64(w.bytesWritten)

	// Update performance data
	mm.updatePerformanceData()
}

// calculatePercentiles calculates latency percentiles
func (mm *MonitoringMiddleware) calculatePercentiles(metrics *RequestMetrics) {
	if len(metrics.latencies) == 0 {
		return
	}

	// Sort latencies (simple approach - in production, consider using a more efficient method)
	latencies := make([]time.Duration, len(metrics.latencies))
	copy(latencies, metrics.latencies)

	// Simple bubble sort for demonstration (use a proper sorting algorithm in production)
	for i := 0; i < len(latencies); i++ {
		for j := 0; j < len(latencies)-1-i; j++ {
			if latencies[j] > latencies[j+1] {
				latencies[j], latencies[j+1] = latencies[j+1], latencies[j]
			}
		}
	}

	// Calculate percentiles
	if len(latencies) > 0 {
		metrics.P50Latency = latencies[len(latencies)*50/100]
		metrics.P95Latency = latencies[len(latencies)*95/100]
		metrics.P99Latency = latencies[len(latencies)*99/100]
	}
}

// groupPath groups similar paths for better metrics aggregation
func (mm *MonitoringMiddleware) groupPath(path string) string {
	// Simple path grouping - replace numeric IDs with placeholders
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if mm.isNumericID(segment) {
			segments[i] = "{id}"
		} else if mm.isUUID(segment) {
			segments[i] = "{uuid}"
		}
	}
	return strings.Join(segments, "/")
}

// isNumericID checks if a string is a numeric ID
func (mm *MonitoringMiddleware) isNumericID(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil && len(s) > 0
}

// isUUID checks if a string looks like a UUID
func (mm *MonitoringMiddleware) isUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

// shouldSkipPath checks if monitoring should be skipped for this path
func (mm *MonitoringMiddleware) shouldSkipPath(path string) bool {
	for _, skipPath := range mm.config.SkipPaths {
		if strings.HasPrefix(path, skipPath) {
			return true
		}
	}
	return false
}

// shouldSample determines if this request should be sampled
func (mm *MonitoringMiddleware) shouldSample() bool {
	// Simple random sampling based on sample rate
	return mm.config.SampleRate >= 1.0 // For now, sample everything if rate is 1.0
}

// logRequest logs request information
func (mm *MonitoringMiddleware) logRequest(r *http.Request, w *monitoringResponseWriter, duration time.Duration) {
	if mm.Logger() == nil {
		return
	}

	fields := []logger.Field{
		logger.String("method", r.Method),
		logger.String("path", r.URL.Path),
		logger.Int("status", w.statusCode),
		logger.Duration("duration", duration),
		logger.Int64("content_length", r.ContentLength),
		logger.Int("bytes_written", w.bytesWritten),
	}

	if mm.config.TrackIPs {
		fields = append(fields, logger.String("client_ip", mm.getClientIP(r)))
	}

	if mm.config.TrackUserAgents {
		fields = append(fields, logger.String("user_agent", r.UserAgent()))
	}

	// Track specific headers
	for _, header := range mm.config.TrackHeaders {
		if value := r.Header.Get(header); value != "" {
			fields = append(fields, logger.String("header_"+strings.ToLower(header), value))
		}
	}

	// Log level based on status and duration
	if w.statusCode >= 500 {
		mm.Logger().Error("request completed", fields...)
	} else if w.statusCode >= 400 {
		mm.Logger().Warn("request completed", fields...)
	} else if duration > mm.config.SlowRequestThreshold {
		mm.Logger().Warn("slow request completed", fields...)
	} else {
		mm.Logger().Info("request completed", fields...)
	}
}

// getClientIP extracts client IP from request
func (mm *MonitoringMiddleware) getClientIP(r *http.Request) string {
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

	// Use remote address
	if ip := r.RemoteAddr; ip != "" {
		// Remove port if present
		if colon := strings.LastIndex(ip, ":"); colon != -1 {
			return ip[:colon]
		}
		return ip
	}

	return "unknown"
}

// checkAlertConditions checks for alert conditions
func (mm *MonitoringMiddleware) checkAlertConditions(r *http.Request, w *monitoringResponseWriter, duration time.Duration) {
	// Check for high latency
	if duration > mm.config.HighLatencyThreshold {
		mm.alertManager.TriggerAlert("high_latency", "warning",
			fmt.Sprintf("High latency detected: %v for %s %s", duration, r.Method, r.URL.Path),
			map[string]interface{}{
				"duration": duration,
				"method":   r.Method,
				"path":     r.URL.Path,
			})
	}

	// Check for errors
	if w.statusCode >= 500 {
		mm.alertManager.TriggerAlert("server_error", "error",
			fmt.Sprintf("Server error: %d for %s %s", w.statusCode, r.Method, r.URL.Path),
			map[string]interface{}{
				"status_code": w.statusCode,
				"method":      r.Method,
				"path":        r.URL.Path,
			})
	}
}

// incrementActiveRequests increments the active request counter
func (mm *MonitoringMiddleware) incrementActiveRequests() {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.performanceData.ActiveRequests++
}

// decrementActiveRequests decrements the active request counter
func (mm *MonitoringMiddleware) decrementActiveRequests() {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.performanceData.ActiveRequests--
}

// updatePerformanceData updates overall performance metrics
func (mm *MonitoringMiddleware) updatePerformanceData() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.performanceData.TotalRequests++
	mm.performanceData.Uptime = time.Since(mm.performanceData.StartTime)

	if mm.performanceData.Uptime > 0 {
		mm.performanceData.RequestRate = float64(mm.performanceData.TotalRequests) / mm.performanceData.Uptime.Seconds()
	}
}

// startPerformanceCollection starts background performance data collection
func (mm *MonitoringMiddleware) startPerformanceCollection(ctx context.Context) {
	ticker := time.NewTicker(mm.config.CollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mm.collectSystemMetrics()
		}
	}
}

// collectSystemMetrics collects system-level metrics
func (mm *MonitoringMiddleware) collectSystemMetrics() {
	if !mm.config.TrackMemory && !mm.config.TrackGoroutines {
		return
	}

	mm.collector.mu.Lock()
	defer mm.collector.mu.Unlock()

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	mm.collector.systemMetrics = &SystemMetrics{
		MemoryUsage:    int64(memStats.Alloc),
		MemoryTotal:    int64(memStats.Sys),
		MemoryPercent:  float64(memStats.Alloc) / float64(memStats.Sys) * 100,
		GoroutineCount: runtime.NumGoroutine(),
		GCPauses:       int64(memStats.NumGC),
		HeapSize:       int64(memStats.HeapAlloc),
		StackSize:      int64(memStats.StackSys),
		LastUpdated:    time.Now(),
	}

	// Update performance data peaks
	mm.mu.Lock()
	if mm.collector.systemMetrics.MemoryUsage > mm.performanceData.PeakMemory {
		mm.performanceData.PeakMemory = mm.collector.systemMetrics.MemoryUsage
	}
	if mm.collector.systemMetrics.GoroutineCount > mm.performanceData.PeakGoroutines {
		mm.performanceData.PeakGoroutines = mm.collector.systemMetrics.GoroutineCount
	}
	mm.mu.Unlock()

	// Check for system alerts
	if mm.collector.systemMetrics.MemoryUsage > mm.config.HighMemoryThreshold {
		mm.alertManager.TriggerAlert("high_memory", "warning",
			fmt.Sprintf("High memory usage: %d bytes", mm.collector.systemMetrics.MemoryUsage),
			map[string]interface{}{
				"memory_usage": mm.collector.systemMetrics.MemoryUsage,
				"threshold":    mm.config.HighMemoryThreshold,
			})
	}

	if mm.collector.systemMetrics.GoroutineCount > mm.config.HighGoroutineThreshold {
		mm.alertManager.TriggerAlert("high_goroutines", "warning",
			fmt.Sprintf("High goroutine count: %d", mm.collector.systemMetrics.GoroutineCount),
			map[string]interface{}{
				"goroutine_count": mm.collector.systemMetrics.GoroutineCount,
				"threshold":       mm.config.HighGoroutineThreshold,
			})
	}
}

// startAlertMonitoring starts background alert monitoring
func (mm *MonitoringMiddleware) startAlertMonitoring(ctx context.Context) {
	ticker := time.NewTicker(time.Minute) // Check every minute
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mm.alertManager.ProcessAlerts()
		}
	}
}

// GetMetrics returns current metrics
func (mm *MonitoringMiddleware) GetMetrics() map[string]*RequestMetrics {
	mm.collector.mu.RLock()
	defer mm.collector.mu.RUnlock()

	result := make(map[string]*RequestMetrics)
	for key, metrics := range mm.collector.requests {
		// Make a copy to avoid race conditions
		metricsCopy := *metrics
		result[key] = &metricsCopy
	}

	return result
}

// GetSystemMetrics returns current system metrics
func (mm *MonitoringMiddleware) GetSystemMetrics() *SystemMetrics {
	mm.collector.mu.RLock()
	defer mm.collector.mu.RUnlock()

	if mm.collector.systemMetrics == nil {
		return nil
	}

	// Make a copy to avoid race conditions
	metricsCopy := *mm.collector.systemMetrics
	return &metricsCopy
}

// GetPerformanceData returns current performance data
func (mm *MonitoringMiddleware) GetPerformanceData() *PerformanceData {
	mm.mu.RLock()
	defer mm.mu.RUnlock()

	// Make a copy to avoid race conditions
	dataCopy := *mm.performanceData
	dataCopy.Uptime = time.Since(mm.performanceData.StartTime)
	return &dataCopy
}

// GetAlerts returns current alerts
func (mm *MonitoringMiddleware) GetAlerts() []*Alert {
	return mm.alertManager.GetAlerts()
}

// monitoringResponseWriter wraps http.ResponseWriter to capture metrics
type monitoringResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (mrw *monitoringResponseWriter) WriteHeader(code int) {
	mrw.statusCode = code
	mrw.ResponseWriter.WriteHeader(code)
}

func (mrw *monitoringResponseWriter) Write(data []byte) (int, error) {
	bytes, err := mrw.ResponseWriter.Write(data)
	mrw.bytesWritten += bytes
	return bytes, err
}

// Helper constructors and methods for supporting types would go here...
// These are simplified implementations for demonstration

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		requests:       make(map[string]*RequestMetrics),
		lastCollection: time.Now(),
	}
}

func NewRequestTracer() *RequestTracer {
	return &RequestTracer{
		activeSpans: make(map[string]*Span),
	}
}

func (rt *RequestTracer) StartSpan(r *http.Request) *Span {
	// Simplified span creation
	span := &Span{
		TraceID:   fmt.Sprintf("trace-%d", time.Now().UnixNano()),
		SpanID:    fmt.Sprintf("span-%d", time.Now().UnixNano()),
		Operation: fmt.Sprintf("%s %s", r.Method, r.URL.Path),
		StartTime: time.Now(),
		Tags:      make(map[string]interface{}),
		Logs:      make([]LogEntry, 0),
	}

	rt.mu.Lock()
	rt.activeSpans[span.SpanID] = span
	rt.mu.Unlock()

	return span
}

func (rt *RequestTracer) EndSpan(span *Span, statusCode int, duration time.Duration) {
	span.EndTime = time.Now()
	span.Duration = duration
	span.Status = fmt.Sprintf("%d", statusCode)

	rt.mu.Lock()
	delete(rt.activeSpans, span.SpanID)
	rt.mu.Unlock()
}

func NewProfiler(enabled bool) *Profiler {
	return &Profiler{
		enabled:  enabled,
		profiles: make(map[string]*ProfileData),
	}
}

func (p *Profiler) RecordProfile(name string, duration time.Duration, memoryUsage int64) {
	if !p.enabled {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.profiles[name] = &ProfileData{
		Name:        name,
		Duration:    duration,
		MemoryUsage: memoryUsage,
		Timestamp:   time.Now(),
	}
}

func NewAlertManager(config Config) *AlertManager {
	return &AlertManager{
		config:     config,
		alerts:     make([]*Alert, 0),
		thresholds: make(map[string]interface{}),
	}
}

func (am *AlertManager) TriggerAlert(alertType, level, message string, metadata map[string]interface{}) {
	am.mu.Lock()
	defer am.mu.Unlock()

	alert := &Alert{
		ID:        fmt.Sprintf("alert-%d", time.Now().UnixNano()),
		Level:     level,
		Type:      alertType,
		Message:   message,
		Timestamp: time.Now(),
		Metadata:  metadata,
	}

	am.alerts = append(am.alerts, alert)

	// Keep only last 100 alerts
	if len(am.alerts) > 100 {
		am.alerts = am.alerts[1:]
	}
}

func (am *AlertManager) GetAlerts() []*Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()

	result := make([]*Alert, len(am.alerts))
	copy(result, am.alerts)
	return result
}

func (am *AlertManager) ProcessAlerts() {
	// Process and potentially send alerts to external systems
	// This is a placeholder for more sophisticated alert handling
}

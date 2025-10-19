package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	streaming "github.com/xraph/forge/pkg/streaming/core"
)

// MetricsMiddleware provides comprehensive metrics collection for streaming
type MetricsMiddleware struct {
	metrics    common.Metrics
	logger     common.Logger
	config     MetricsConfig
	collectors map[string]MetricsCollector
	mu         sync.RWMutex
	stats      MetricsStats
	registry   *MetricsRegistry
	startTime  time.Time
}

// MetricsConfig contains configuration for metrics middleware
type MetricsConfig struct {
	Enabled               bool               `yaml:"enabled" default:"true"`
	CollectConnections    bool               `yaml:"collect_connections" default:"true"`
	CollectMessages       bool               `yaml:"collect_messages" default:"true"`
	CollectPerformance    bool               `yaml:"collect_performance" default:"true"`
	CollectErrors         bool               `yaml:"collect_errors" default:"true"`
	CollectPresence       bool               `yaml:"collect_presence" default:"true"`
	CollectBandwidth      bool               `yaml:"collect_bandwidth" default:"true"`
	CollectLatency        bool               `yaml:"collect_latency" default:"true"`
	CollectCustomMetrics  bool               `yaml:"collect_custom_metrics" default:"true"`
	MetricsPrefix         string             `yaml:"metrics_prefix" default:"forge.streaming"`
	SamplingRate          float64            `yaml:"sampling_rate" default:"1.0"`
	HistogramBuckets      []float64          `yaml:"histogram_buckets"`
	CounterResetInterval  time.Duration      `yaml:"counter_reset_interval" default:"24h"`
	MetricsRetention      time.Duration      `yaml:"metrics_retention" default:"168h"`
	AggregationInterval   time.Duration      `yaml:"aggregation_interval" default:"10s"`
	EnableCardinality     bool               `yaml:"enable_cardinality" default:"true"`
	MaxCardinality        int                `yaml:"max_cardinality" default:"10000"`
	EnableExport          bool               `yaml:"enable_export" default:"true"`
	ExportInterval        time.Duration      `yaml:"export_interval" default:"60s"`
	ExportFormat          string             `yaml:"export_format" default:"prometheus"`
	CustomLabels          map[string]string  `yaml:"custom_labels"`
	MetricsEndpoint       string             `yaml:"metrics_endpoint" default:"/metrics"`
	EnableHealthMetrics   bool               `yaml:"enable_health_metrics" default:"true"`
	EnableResourceMetrics bool               `yaml:"enable_resource_metrics" default:"true"`
	EnableAlerts          bool               `yaml:"enable_alerts" default:"false"`
	AlertThresholds       map[string]float64 `yaml:"alert_thresholds"`
}

// DefaultMetricsConfig returns default metrics configuration
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:               true,
		CollectConnections:    true,
		CollectMessages:       true,
		CollectPerformance:    true,
		CollectErrors:         true,
		CollectPresence:       true,
		CollectBandwidth:      true,
		CollectLatency:        true,
		CollectCustomMetrics:  true,
		MetricsPrefix:         "forge.streaming",
		SamplingRate:          1.0,
		HistogramBuckets:      []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 25.0, 50.0, 100.0},
		CounterResetInterval:  24 * time.Hour,
		MetricsRetention:      168 * time.Hour, // 7 days
		AggregationInterval:   10 * time.Second,
		EnableCardinality:     true,
		MaxCardinality:        10000,
		EnableExport:          true,
		ExportInterval:        60 * time.Second,
		ExportFormat:          "prometheus",
		CustomLabels:          make(map[string]string),
		MetricsEndpoint:       "/metrics",
		EnableHealthMetrics:   true,
		EnableResourceMetrics: true,
		EnableAlerts:          false,
		AlertThresholds:       make(map[string]float64),
	}
}

// MetricsCollector interface for different types of metrics collectors
type MetricsCollector interface {
	Name() string
	Collect(ctx context.Context) error
	Reset() error
	GetMetrics() map[string]interface{}
}

// MetricsRegistry manages metrics registration and collection
type MetricsRegistry struct {
	counters   map[string]common.Counter
	gauges     map[string]common.Gauge
	histograms map[string]common.Histogram
	timers     map[string]common.Timer
	mu         sync.RWMutex
}

// NewMetricsRegistry creates a new metrics registry
func NewMetricsRegistry() *MetricsRegistry {
	return &MetricsRegistry{
		counters:   make(map[string]common.Counter),
		gauges:     make(map[string]common.Gauge),
		histograms: make(map[string]common.Histogram),
		timers:     make(map[string]common.Timer),
	}
}

// MetricsStats contains metrics statistics
type MetricsStats struct {
	TotalMetrics       int       `json:"total_metrics"`
	ActiveConnections  int       `json:"active_connections"`
	TotalMessages      int64     `json:"total_messages"`
	MessageRate        float64   `json:"message_rate"`
	ErrorRate          float64   `json:"error_rate"`
	AverageLatency     float64   `json:"average_latency"`
	PeakConnections    int       `json:"peak_connections"`
	TotalBandwidth     int64     `json:"total_bandwidth"`
	LastCollectionTime time.Time `json:"last_collection_time"`
	CollectionCount    int64     `json:"collection_count"`
	ExportCount        int64     `json:"export_count"`
	LastExportTime     time.Time `json:"last_export_time"`
	AlertsTriggered    int64     `json:"alerts_triggered"`
	MetricsCardinality int       `json:"metrics_cardinality"`
	MemoryUsage        int64     `json:"memory_usage"`
	CPUUsage           float64   `json:"cpu_usage"`
	DiskUsage          int64     `json:"disk_usage"`
	NetworkIO          int64     `json:"network_io"`
}

// NewMetricsMiddleware creates a new metrics middleware
func NewMetricsMiddleware(
	metrics common.Metrics,
	logger common.Logger,
	config MetricsConfig,
) *MetricsMiddleware {
	middleware := &MetricsMiddleware{
		metrics:    metrics,
		logger:     logger,
		config:     config,
		collectors: make(map[string]MetricsCollector),
		registry:   NewMetricsRegistry(),
		stats:      MetricsStats{},
		startTime:  time.Now(),
	}

	// Initialize default collectors
	middleware.initializeCollectors()

	// Start background collection routine
	if config.Enabled {
		go middleware.collectionRoutine()
	}

	return middleware
}

// Handler returns the HTTP middleware handler
func (mm *MetricsMiddleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !mm.config.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			startTime := time.Now()

			// Increment request counter
			mm.recordHTTPRequest(r)

			// Wrap response writer to capture metrics
			wrapper := &metricsResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
				size:           0,
			}

			// Continue to next handler
			next.ServeHTTP(wrapper, r)

			// Record response metrics
			mm.recordHTTPResponse(r, wrapper, time.Since(startTime))
		})
	}
}

// RecordConnection records connection metrics
func (mm *MetricsMiddleware) RecordConnection(event string, conn streaming.Connection) {
	if !mm.config.Enabled || !mm.config.CollectConnections {
		return
	}

	labels := mm.getConnectionLabels(conn)

	switch event {
	case "connected":
		mm.metrics.Counter(mm.metricName("connections.total"), labels...).Inc()
		mm.metrics.Gauge(mm.metricName("connections.active"), labels...).Inc()
		mm.recordConnectionsByProtocol(conn.Protocol(), 1)
		mm.recordConnectionsByRoom(conn.RoomID(), 1)
	case "disconnected":
		mm.metrics.Counter(mm.metricName("connections.disconnected"), labels...).Inc()
		mm.metrics.Gauge(mm.metricName("connections.active"), labels...).Dec()
		mm.recordConnectionsByProtocol(conn.Protocol(), -1)
		mm.recordConnectionsByRoom(conn.RoomID(), -1)
	case "error":
		mm.metrics.Counter(mm.metricName("connections.errors"), labels...).Inc()
	}

	// Update stats
	mm.updateConnectionStats(event, conn)
}

// RecordMessage records message metrics
func (mm *MetricsMiddleware) RecordMessage(event string, conn streaming.Connection, message *streaming.Message) {
	if !mm.config.Enabled || !mm.config.CollectMessages {
		return
	}

	labels := mm.getMessageLabels(conn, message)

	switch event {
	case "sent":
		mm.metrics.Counter(mm.metricName("messages.sent"), labels...).Inc()
		mm.recordMessageByType(message.Type, 1)
		mm.recordMessageByPriority(message.Priority, 1)
		mm.recordMessageSize(len(fmt.Sprintf("%v", message.Data)))
	case "received":
		mm.metrics.Counter(mm.metricName("messages.received"), labels...).Inc()
		mm.recordMessageByType(message.Type, 1)
	case "failed":
		mm.metrics.Counter(mm.metricName("messages.failed"), labels...).Inc()
	case "filtered":
		mm.metrics.Counter(mm.metricName("messages.filtered"), labels...).Inc()
	}

	// Update stats
	mm.updateMessageStats(event, message)
}

// RecordError records error metrics
func (mm *MetricsMiddleware) RecordError(errorType string, conn streaming.Connection, err error) {
	if !mm.config.Enabled || !mm.config.CollectErrors {
		return
	}

	labels := mm.getErrorLabels(conn, errorType)
	mm.metrics.Counter(mm.metricName("errors.total"), labels...).Inc()

	// Record error by type
	mm.recordErrorByType(errorType, 1)

	// Update stats
	mm.updateErrorStats(errorType, err)
}

// RecordPerformance records performance metrics
func (mm *MetricsMiddleware) RecordPerformance(operation string, duration time.Duration, conn streaming.Connection) {
	if !mm.config.Enabled || !mm.config.CollectPerformance {
		return
	}

	labels := mm.getPerformanceLabels(conn, operation)

	mm.metrics.Histogram(mm.metricName("performance.duration"), labels...).Observe(duration.Seconds())
	mm.metrics.Timer(mm.metricName("performance.timer"), labels...).Record(duration)

	// Record operation-specific metrics
	mm.recordOperationMetrics(operation, duration)

	// Update stats
	mm.updatePerformanceStats(operation, duration)
}

// RecordPresence records presence metrics
func (mm *MetricsMiddleware) RecordPresence(event string, userID string, roomID string, status streaming.PresenceStatus) {
	if !mm.config.Enabled || !mm.config.CollectPresence {
		return
	}

	labels := []string{
		"room_id", roomID,
		"status", string(status),
	}

	switch event {
	case "online":
		mm.metrics.Counter(mm.metricName("presence.online"), labels...).Inc()
		mm.metrics.Gauge(mm.metricName("presence.active_users"), "room_id", roomID).Inc()
	case "offline":
		mm.metrics.Counter(mm.metricName("presence.offline"), labels...).Inc()
		mm.metrics.Gauge(mm.metricName("presence.active_users"), "room_id", roomID).Dec()
	case "status_changed":
		mm.metrics.Counter(mm.metricName("presence.status_changes"), labels...).Inc()
	}

	// Update stats
	mm.updatePresenceStats(event, userID, roomID, status)
}

// RecordBandwidth records bandwidth metrics
func (mm *MetricsMiddleware) RecordBandwidth(conn streaming.Connection, bytesIn, bytesOut int64) {
	if !mm.config.Enabled || !mm.config.CollectBandwidth {
		return
	}

	labels := mm.getConnectionLabels(conn)

	mm.metrics.Counter(mm.metricName("bandwidth.bytes_in"), labels...).Add(float64(bytesIn))
	mm.metrics.Counter(mm.metricName("bandwidth.bytes_out"), labels...).Add(float64(bytesOut))
	mm.metrics.Gauge(mm.metricName("bandwidth.total"), labels...).Add(float64(bytesIn + bytesOut))

	// Update stats
	mm.updateBandwidthStats(bytesIn, bytesOut)
}

// RecordLatency records latency metrics
func (mm *MetricsMiddleware) RecordLatency(conn streaming.Connection, latency time.Duration) {
	if !mm.config.Enabled || !mm.config.CollectLatency {
		return
	}

	labels := mm.getConnectionLabels(conn)

	mm.metrics.Histogram(mm.metricName("latency.duration"), labels...).Observe(latency.Seconds())
	mm.metrics.Gauge(mm.metricName("latency.current"), labels...).Set(latency.Seconds())

	// Update stats
	mm.updateLatencyStats(latency)
}

// RecordCustomMetric records custom metrics
func (mm *MetricsMiddleware) RecordCustomMetric(name string, value float64, labels ...string) {
	if !mm.config.Enabled || !mm.config.CollectCustomMetrics {
		return
	}

	mm.metrics.Gauge(mm.metricName("custom."+name), labels...).Set(value)
}

// RecordCustomCounter records custom counter metrics
func (mm *MetricsMiddleware) RecordCustomCounter(name string, value float64, labels ...string) {
	if !mm.config.Enabled || !mm.config.CollectCustomMetrics {
		return
	}

	mm.metrics.Counter(mm.metricName("custom."+name), labels...).Add(value)
}

// RecordCustomHistogram records custom histogram metrics
func (mm *MetricsMiddleware) RecordCustomHistogram(name string, value float64, labels ...string) {
	if !mm.config.Enabled || !mm.config.CollectCustomMetrics {
		return
	}

	mm.metrics.Histogram(mm.metricName("custom."+name), labels...).Observe(value)
}

// Helper methods for labels generation

// getConnectionLabels generates labels for connection metrics
func (mm *MetricsMiddleware) getConnectionLabels(conn streaming.Connection) []string {
	labels := []string{
		"protocol", string(conn.Protocol()),
		"room_id", conn.RoomID(),
	}

	// Add custom labels
	for key, value := range mm.config.CustomLabels {
		labels = append(labels, key, value)
	}

	return labels
}

// getMessageLabels generates labels for message metrics
func (mm *MetricsMiddleware) getMessageLabels(conn streaming.Connection, message *streaming.Message) []string {
	labels := []string{
		"protocol", string(conn.Protocol()),
		"room_id", message.RoomID,
		"message_type", string(message.Type),
		"priority", strconv.Itoa(int(message.Priority)),
	}

	// Add custom labels
	for key, value := range mm.config.CustomLabels {
		labels = append(labels, key, value)
	}

	return labels
}

// getErrorLabels generates labels for error metrics
func (mm *MetricsMiddleware) getErrorLabels(conn streaming.Connection, errorType string) []string {
	labels := []string{
		"protocol", string(conn.Protocol()),
		"room_id", conn.RoomID(),
		"error_type", errorType,
	}

	// Add custom labels
	for key, value := range mm.config.CustomLabels {
		labels = append(labels, key, value)
	}

	return labels
}

// getPerformanceLabels generates labels for performance metrics
func (mm *MetricsMiddleware) getPerformanceLabels(conn streaming.Connection, operation string) []string {
	labels := []string{
		"protocol", string(conn.Protocol()),
		"room_id", conn.RoomID(),
		"operation", operation,
	}

	// Add custom labels
	for key, value := range mm.config.CustomLabels {
		labels = append(labels, key, value)
	}

	return labels
}

// Helper methods for recording specific metrics

// recordConnectionsByProtocol records connections by protocol
func (mm *MetricsMiddleware) recordConnectionsByProtocol(protocol streaming.ProtocolType, delta int) {
	mm.metrics.Gauge(mm.metricName("connections.by_protocol"), "protocol", string(protocol)).Add(float64(delta))
}

// recordConnectionsByRoom records connections by room
func (mm *MetricsMiddleware) recordConnectionsByRoom(roomID string, delta int) {
	if roomID != "" {
		mm.metrics.Gauge(mm.metricName("connections.by_room"), "room_id", roomID).Add(float64(delta))
	}
}

// recordMessageByType records messages by type
func (mm *MetricsMiddleware) recordMessageByType(messageType streaming.MessageType, delta int) {
	mm.metrics.Counter(mm.metricName("messages.by_type"), "type", string(messageType)).Add(float64(delta))
}

// recordMessageByPriority records messages by priority
func (mm *MetricsMiddleware) recordMessageByPriority(priority streaming.MessagePriority, delta int) {
	mm.metrics.Counter(mm.metricName("messages.by_priority"), "priority", strconv.Itoa(int(priority))).Add(float64(delta))
}

// recordMessageSize records message size
func (mm *MetricsMiddleware) recordMessageSize(size int) {
	mm.metrics.Histogram(mm.metricName("messages.size"), "unit", "bytes").Observe(float64(size))
}

// recordErrorByType records errors by type
func (mm *MetricsMiddleware) recordErrorByType(errorType string, delta int) {
	mm.metrics.Counter(mm.metricName("errors.by_type"), "type", errorType).Add(float64(delta))
}

// recordOperationMetrics records operation-specific metrics
func (mm *MetricsMiddleware) recordOperationMetrics(operation string, duration time.Duration) {
	mm.metrics.Histogram(mm.metricName("operations.duration"), "operation", operation).Observe(duration.Seconds())
	mm.metrics.Counter(mm.metricName("operations.total"), "operation", operation).Inc()
}

// recordHTTPRequest records HTTP request metrics
func (mm *MetricsMiddleware) recordHTTPRequest(r *http.Request) {
	labels := []string{
		"method", r.Method,
		"path", r.URL.Path,
	}
	mm.metrics.Counter(mm.metricName("http.requests"), labels...).Inc()
}

// recordHTTPResponse records HTTP response metrics
func (mm *MetricsMiddleware) recordHTTPResponse(r *http.Request, w *metricsResponseWriter, duration time.Duration) {
	labels := []string{
		"method", r.Method,
		"path", r.URL.Path,
		"status", strconv.Itoa(w.statusCode),
	}

	mm.metrics.Counter(mm.metricName("http.responses"), labels...).Inc()
	mm.metrics.Histogram(mm.metricName("http.duration"), labels...).Observe(duration.Seconds())
	mm.metrics.Counter(mm.metricName("http.bytes"), labels...).Add(float64(w.size))
}

// Stats update methods

// updateConnectionStats updates connection statistics
func (mm *MetricsMiddleware) updateConnectionStats(event string, conn streaming.Connection) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	switch event {
	case "connected":
		mm.stats.ActiveConnections++
		if mm.stats.ActiveConnections > mm.stats.PeakConnections {
			mm.stats.PeakConnections = mm.stats.ActiveConnections
		}
	case "disconnected":
		mm.stats.ActiveConnections--
	}
}

// updateMessageStats updates message statistics
func (mm *MetricsMiddleware) updateMessageStats(event string, message *streaming.Message) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	switch event {
	case "sent", "received":
		mm.stats.TotalMessages++

		// Calculate message rate
		elapsed := time.Since(mm.startTime).Seconds()
		if elapsed > 0 {
			mm.stats.MessageRate = float64(mm.stats.TotalMessages) / elapsed
		}
	}
}

// updateErrorStats updates error statistics
func (mm *MetricsMiddleware) updateErrorStats(errorType string, err error) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Calculate error rate
	if mm.stats.TotalMessages > 0 {
		// This is a simplified calculation; in practice, you'd track errors separately
		mm.stats.ErrorRate = 0.01 // Placeholder
	}
}

// updatePerformanceStats updates performance statistics
func (mm *MetricsMiddleware) updatePerformanceStats(operation string, duration time.Duration) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Update average latency (simplified calculation)
	if mm.stats.AverageLatency == 0 {
		mm.stats.AverageLatency = duration.Seconds()
	} else {
		mm.stats.AverageLatency = (mm.stats.AverageLatency + duration.Seconds()) / 2
	}
}

// updatePresenceStats updates presence statistics
func (mm *MetricsMiddleware) updatePresenceStats(event string, userID string, roomID string, status streaming.PresenceStatus) {
	// Implementation depends on specific presence tracking requirements
}

// updateBandwidthStats updates bandwidth statistics
func (mm *MetricsMiddleware) updateBandwidthStats(bytesIn, bytesOut int64) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	mm.stats.TotalBandwidth += bytesIn + bytesOut
}

// updateLatencyStats updates latency statistics
func (mm *MetricsMiddleware) updateLatencyStats(latency time.Duration) {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	// Update average latency (simplified calculation)
	if mm.stats.AverageLatency == 0 {
		mm.stats.AverageLatency = latency.Seconds()
	} else {
		mm.stats.AverageLatency = (mm.stats.AverageLatency + latency.Seconds()) / 2
	}
}

// Utility methods

// metricName generates a metric name with prefix
func (mm *MetricsMiddleware) metricName(name string) string {
	return mm.config.MetricsPrefix + "." + name
}

// shouldSample determines if this metric should be sampled
func (mm *MetricsMiddleware) shouldSample() bool {
	if mm.config.SamplingRate >= 1.0 {
		return true
	}

	// Simple sampling based on current time
	return time.Now().UnixNano()%100 < int64(mm.config.SamplingRate*100)
}

// initializeCollectors initializes default metrics collectors
func (mm *MetricsMiddleware) initializeCollectors() {
	// Add default collectors here
	// This is where you'd add specific collectors for different metrics
}

// collectionRoutine runs the background metrics collection
func (mm *MetricsMiddleware) collectionRoutine() {
	ticker := time.NewTicker(mm.config.AggregationInterval)
	defer ticker.Stop()

	for range ticker.C {
		mm.collectMetrics()
	}
}

// collectMetrics collects and aggregates metrics
func (mm *MetricsMiddleware) collectMetrics() {
	ctx := context.Background()

	for name, collector := range mm.collectors {
		if err := collector.Collect(ctx); err != nil && mm.logger != nil {
			mm.logger.Error("failed to collect metrics",
				logger.String("collector", name),
				logger.Error(err),
			)
		}
	}

	mm.mu.Lock()
	mm.stats.LastCollectionTime = time.Now()
	mm.stats.CollectionCount++
	mm.stats.TotalMetrics = len(mm.registry.counters) + len(mm.registry.gauges) + len(mm.registry.histograms) + len(mm.registry.timers)
	mm.mu.Unlock()
}

// AddCollector adds a custom metrics collector
func (mm *MetricsMiddleware) AddCollector(collector MetricsCollector) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.collectors[collector.Name()] = collector
}

// RemoveCollector removes a metrics collector
func (mm *MetricsMiddleware) RemoveCollector(name string) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	delete(mm.collectors, name)
}

// GetStats returns metrics statistics
func (mm *MetricsMiddleware) GetStats() MetricsStats {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.stats
}

// GetRegistry returns the metrics registry
func (mm *MetricsMiddleware) GetRegistry() *MetricsRegistry {
	return mm.registry
}

// UpdateConfig updates the metrics configuration
func (mm *MetricsMiddleware) UpdateConfig(config MetricsConfig) {
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.config = config
}

// GetConfig returns the current configuration
func (mm *MetricsMiddleware) GetConfig() MetricsConfig {
	mm.mu.RLock()
	defer mm.mu.RUnlock()
	return mm.config
}

// Reset resets all metrics
func (mm *MetricsMiddleware) Reset() {
	mm.mu.Lock()
	defer mm.mu.Unlock()

	for _, collector := range mm.collectors {
		if err := collector.Reset(); err != nil && mm.logger != nil {
			mm.logger.Error("failed to reset collector",
				logger.String("collector", collector.Name()),
				logger.Error(err),
			)
		}
	}

	mm.stats = MetricsStats{}
	mm.startTime = time.Now()
}

// metricsResponseWriter wraps http.ResponseWriter to capture metrics
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
	size       int
}

func (mrw *metricsResponseWriter) WriteHeader(code int) {
	mrw.statusCode = code
	mrw.ResponseWriter.WriteHeader(code)
}

func (mrw *metricsResponseWriter) Write(b []byte) (int, error) {
	size, err := mrw.ResponseWriter.Write(b)
	mrw.size += size
	return size, err
}

// ConnectionMetrics provides connection-specific metrics
type ConnectionMetrics struct {
	middleware *MetricsMiddleware
	connection streaming.Connection
	labels     []string
}

// NewConnectionMetrics creates a new connection metrics instance
func NewConnectionMetrics(middleware *MetricsMiddleware, conn streaming.Connection) *ConnectionMetrics {
	return &ConnectionMetrics{
		middleware: middleware,
		connection: conn,
		labels:     middleware.getConnectionLabels(conn),
	}
}

// RecordMessage records a message metric for this connection
func (cm *ConnectionMetrics) RecordMessage(event string, message *streaming.Message) {
	cm.middleware.RecordMessage(event, cm.connection, message)
}

// RecordError records an error metric for this connection
func (cm *ConnectionMetrics) RecordError(errorType string, err error) {
	cm.middleware.RecordError(errorType, cm.connection, err)
}

// RecordPerformance records a performance metric for this connection
func (cm *ConnectionMetrics) RecordPerformance(operation string, duration time.Duration) {
	cm.middleware.RecordPerformance(operation, duration, cm.connection)
}

// RecordBandwidth records bandwidth metrics for this connection
func (cm *ConnectionMetrics) RecordBandwidth(bytesIn, bytesOut int64) {
	cm.middleware.RecordBandwidth(cm.connection, bytesIn, bytesOut)
}

// RecordLatency records latency metrics for this connection
func (cm *ConnectionMetrics) RecordLatency(latency time.Duration) {
	cm.middleware.RecordLatency(cm.connection, latency)
}

// RoomMetrics provides room-specific metrics
type RoomMetrics struct {
	middleware *MetricsMiddleware
	roomID     string
	labels     []string
}

// NewRoomMetrics creates a new room metrics instance
func NewRoomMetrics(middleware *MetricsMiddleware, roomID string) *RoomMetrics {
	return &RoomMetrics{
		middleware: middleware,
		roomID:     roomID,
		labels:     []string{"room_id", roomID},
	}
}

// RecordMessage records a message metric for this room
func (rm *RoomMetrics) RecordMessage(messageType streaming.MessageType, count int) {
	rm.middleware.metrics.Counter(rm.middleware.metricName("room.messages"), append(rm.labels, "type", string(messageType))...).Add(float64(count))
}

// RecordUserCount records user count for this room
func (rm *RoomMetrics) RecordUserCount(count int) {
	rm.middleware.metrics.Gauge(rm.middleware.metricName("room.users"), rm.labels...).Set(float64(count))
}

// RecordConnectionCount records connection count for this room
func (rm *RoomMetrics) RecordConnectionCount(count int) {
	rm.middleware.metrics.Gauge(rm.middleware.metricName("room.connections"), rm.labels...).Set(float64(count))
}

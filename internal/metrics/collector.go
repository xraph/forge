package metrics

// Testing code and collectors use Reset() for cleanup without error handling.

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics/collectors"
	"github.com/xraph/forge/internal/metrics/exporters"
	"github.com/xraph/forge/internal/metrics/storage"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// =============================================================================
// METRICS COLLECTOR INTERFACE
// =============================================================================

// Metrics defines the interface for metrics collection.
type Metrics = metrics.Metrics

// HTTPMetricsProvider is implemented by metrics collectors that can provide
// HTTP middleware for automatic request tracking.
type HTTPMetricsProvider interface {
	// HTTPMiddleware returns middleware that automatically records HTTP request metrics.
	// Returns nil if HTTP metrics are not enabled.
	HTTPMiddleware() func(http.Handler) http.Handler
}

// MetricDetailProvider is implemented by metrics collectors that expose
// individual metric objects for detailed introspection (metadata, exemplars, etc.).
type MetricDetailProvider interface {
	// GetMetric returns a named metric from the registry.
	// The returned value can be type-asserted to Counter, Gauge, Histogram, or Timer.
	GetMetric(name string) any

	// GetCollector returns a named custom collector.
	GetCollector(name string) metrics.CustomCollector
}

// MetricQueryProvider is implemented by metrics collectors that support
// efficient querying without full map allocation. Consumers should type-assert
// the Metrics interface to this for zero-copy access.
type MetricQueryProvider interface {
	// MetricNames returns all registered metric names without computing values.
	MetricNames() []string

	// MetricValue returns the scalar float64 value for a named metric.
	// For counters/gauges, returns the direct value.
	// For histograms/timers, returns the mean.
	// Returns (0, false) if the metric does not exist.
	MetricValue(name string) (float64, bool)

	// MetricCountsByType returns the count of metrics for each type
	// without allocating value maps.
	MetricCountsByType() map[string]int
}

// TimeSeriesDataPoint is a single timestamped scalar value.
type TimeSeriesDataPoint struct {
	Timestamp time.Time
	Value     float64
}

// TimeSeriesQueryProvider is implemented by metrics collectors that store
// historical time-series data. Consumers can type-assert to this interface
// to query historical metric values.
type TimeSeriesQueryProvider interface {
	// QueryMetricRange returns time-series data points for a named metric
	// within the given time range.
	QueryMetricRange(name string, start, end time.Time) []TimeSeriesDataPoint
}

// =============================================================================
// COLLECTOR IMPLEMENTATION
// =============================================================================

// collector implements MetricsCollector interface.
type collector struct {
	metrics.Metrics

	name               string
	registry           Registry
	customCollectors   map[string]metrics.CustomCollector
	exporters          map[metrics.ExportFormat]metrics.Exporter
	logger             logger.Logger
	config             *CollectorConfig
	mu                 sync.RWMutex
	started            bool
	startTime          time.Time
	metricsCreated     int64
	metricsCollected   int64
	lastCollectionTime time.Time
	errors             []error
	httpCollector      *collectors.HTTPCollector
	tsStorage          *storage.TimeSeriesStorage
	promBridge         *exporters.PrometheusBridge
}

// CollectorConfig contains configuration for the metrics collector.
type CollectorConfig = shared.MetricsConfig

// StorageConfig contains storage configuration.
type StorageConfig = shared.MetricsStorageConfig[map[string]any]

// CollectorStats contains statistics about the metrics collector.
type CollectorStats = metrics.CollectorStats

// DefaultCollectorConfig returns default collector configuration.
func DefaultCollectorConfig() *CollectorConfig {
	return &CollectorConfig{
		Features: shared.MetricsFeatures{
			SystemMetrics:  true,
			RuntimeMetrics: true,
			HTTPMetrics:    true,
		},
		Exporters: make(map[string]ExporterConfig),
		Collection: shared.MetricsCollection{
			Interval:  time.Second * 10,
			Namespace: "forge",
		},
		Storage: &shared.MetricsStorageConfig[map[string]any]{
			Type:   "memory",
			Config: make(map[string]any),
		},
	}
}

// New creates a new metrics collector.
func New(config *CollectorConfig, logger logger.Logger) Metrics {
	if config == nil {
		config = DefaultCollectorConfig()
	}

	c := &collector{
		Metrics: metrics.NewMetricsCollector(
			shared.MetricsCollectorKey, metrics.WithConfig(config), metrics.WithLogger(logger),
		),
		name:             shared.MetricsCollectorKey,
		registry:         NewRegistry(),
		customCollectors: make(map[string]metrics.CustomCollector),
		exporters:        make(map[metrics.ExportFormat]metrics.Exporter),
		logger:           logger,
		config:           config,
		errors:           make([]error, 0),
	}

	// Initialize exporters
	c.initializeExporters()

	// Prometheus bridge: reads the merged snapshot fresh on each scrape.
	c.promBridge = exporters.NewPrometheusBridge(c.GetMetrics, exporters.PrometheusConfig{
		Namespace:              c.config.Collection.Namespace,
		EnableGoCollector:      c.config.Features.RuntimeMetrics,
		EnableProcessCollector: c.config.Features.RuntimeMetrics,
	})

	// Initialize time-series storage for historical metric queries.
	c.tsStorage = storage.NewTimeSeriesStorageWithConfig(&storage.TimeSeriesStorageConfig{
		Retention:          time.Hour,
		Resolution:         30 * time.Second,
		MaxSeries:          500,
		MaxPointsPerSeries: 120,
		CompressionEnabled: true,
		CompressionDelay:   15 * time.Minute,
		CleanupInterval:    5 * time.Minute,
		EnableStats:        false,
	})

	return c
}

// =============================================================================
// SERVICE LIFECYCLE IMPLEMENTATION
// =============================================================================

// Name returns the service name.
func (c *collector) Name() string {
	return c.name
}

// Dependencies returns the service dependencies.
func (c *collector) Dependencies() []string {
	return []string{"config-manager"}
}

// Start starts the metrics collector service.
func (c *collector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		c.logger.Warn("metrics collector already started", logger.String("name", c.name))

		return nil
		// todo: remove return common.ErrServiceAlreadyExists(c.name)
	}

	c.started = true
	c.startTime = time.Now()

	// Initialize built-in collectors
	if err := c.initializeBuiltinCollectors(); err != nil {
		return errors.ErrServiceStartFailed(c.name, err)
	}

	// Start time-series storage for historical queries.
	if c.tsStorage != nil {
		if err := c.tsStorage.Start(ctx); err != nil {
			c.logger.Warn("failed to start time-series storage", logger.Error(err))
			c.tsStorage = nil
		}
	}

	// Start collection goroutine
	go c.collectionLoop(ctx)

	if c.logger != nil {
		c.logger.Info("metrics collector started",
			logger.String("name", c.name),
			logger.Bool("system_metrics", c.config.Features.SystemMetrics),
			logger.Bool("runtime_metrics", c.config.Features.RuntimeMetrics),
			logger.Duration("collection_interval", c.config.Collection.Interval),
			logger.Int("max_metrics", c.config.Limits.MaxMetrics),
		)
	}

	return nil
}

// Stop stops the metrics collector service.
func (c *collector) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return errors.ErrServiceNotFound(c.name)
	}

	c.started = false

	// Stop all custom collectors
	for _, col := range c.customCollectors {
		_ = col.Reset()
	}

	// Reset registry
	_ = c.registry.Reset()

	if c.logger != nil {
		c.logger.Info("metrics collector stopped",
			logger.String("name", c.name),
			logger.Duration("uptime", time.Since(c.startTime)),
			logger.Int64("metrics_created", c.metricsCreated),
			logger.Int64("metrics_collected", c.metricsCollected),
		)
	}

	return nil
}

// Health performs health check.
func (c *collector) Health(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return errors.ErrHealthCheckFailed(c.name, errors.New("metrics collector not started"))
	}

	// Check if collection is working
	if !c.lastCollectionTime.IsZero() {
		timeSinceLastCollection := time.Since(c.lastCollectionTime)
		if timeSinceLastCollection > c.config.Collection.Interval*2 {
			return errors.ErrHealthCheckFailed(c.name, fmt.Errorf("metrics collection stalled for %v", timeSinceLastCollection))
		}
	}

	// Check error rate
	if len(c.errors) > 10 {
		return errors.ErrHealthCheckFailed(c.name, fmt.Errorf("too many errors: %d", len(c.errors)))
	}

	return nil
}

// =============================================================================
// METRIC CREATION METHODS
// =============================================================================

// Counter creates or retrieves a counter metric.
func (c *collector) Counter(name string, opts ...metrics.MetricOption) metrics.Counter {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := metrics.NormalizeMetricName(name)

	metric := c.registry.GetOrCreateCounter(normalizedName, opts...)
	c.metricsCreated++

	return metric
}

// Gauge creates or retrieves a gauge metric.
func (c *collector) Gauge(name string, opts ...metrics.MetricOption) metrics.Gauge {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := metrics.NormalizeMetricName(name)

	metric := c.registry.GetOrCreateGauge(normalizedName, opts...)
	c.metricsCreated++

	return metric
}

// Histogram creates or retrieves a histogram metric.
func (c *collector) Histogram(name string, opts ...metrics.MetricOption) metrics.Histogram {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := metrics.NormalizeMetricName(name)

	metric := c.registry.GetOrCreateHistogram(normalizedName, opts...)
	c.metricsCreated++

	return metric
}

// Timer creates or retrieves a timer metric.
func (c *collector) Timer(name string, opts ...metrics.MetricOption) metrics.Timer {
	normalizedName := metrics.NormalizeMetricName(name)

	return c.Metrics.Timer(normalizedName, opts...)
}

// =============================================================================
// CUSTOM COLLECTOR MANAGEMENT
// =============================================================================

// RegisterCollector registers a custom collector.
func (c *collector) RegisterCollector(collector metrics.CustomCollector) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	name := collector.Name()
	if _, exists := c.customCollectors[name]; exists {
		return errors.ErrServiceAlreadyExists(name)
	}

	c.customCollectors[name] = collector

	if c.logger != nil {
		c.logger.Debug("custom collector registered",
			logger.String("collector", name),
		)
	}

	return nil
}

// registerCollectorLocked registers a custom collector without acquiring the lock.
// Caller must already hold c.mu.
func (c *collector) registerCollectorLocked(coll metrics.CustomCollector) error {
	name := coll.Name()
	if _, exists := c.customCollectors[name]; exists {
		return errors.ErrServiceAlreadyExists(name)
	}

	c.customCollectors[name] = coll

	if c.logger != nil {
		c.logger.Debug("custom collector registered",
			logger.String("collector", name),
		)
	}

	return nil
}

// UnregisterCollector unregisters a custom collector.
func (c *collector) UnregisterCollector(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	collector, exists := c.customCollectors[name]
	if !exists {
		return errors.ErrServiceNotFound(name)
	}

	_ = collector.Reset()

	delete(c.customCollectors, name)

	if c.logger != nil {
		c.logger.Debug("custom collector unregistered",
			logger.String("collector", name),
		)
	}

	return nil
}

// GetCollectors returns all custom collectors.
func (c *collector) GetCollectors() []metrics.CustomCollector {
	c.mu.RLock()
	defer c.mu.RUnlock()

	collectors := make([]metrics.CustomCollector, 0, len(c.customCollectors))
	for _, collector := range c.customCollectors {
		collectors = append(collectors, collector)
	}

	return collectors
}

// =============================================================================
// INTERFACE OVERRIDES
// =============================================================================
// The following methods override the embedded metrics.Metrics implementation
// to read from the forge collector's internal state instead of the embedded
// go-utils state. Without these, calls like ListCollectors() fall through to
// the embedded implementation which has empty maps (since RegisterCollector,
// Counter, Gauge, Histogram, etc. are all overridden to use internal storage).

// ListCollectors returns all registered custom collectors.
func (c *collector) ListCollectors() []metrics.CustomCollector {
	return c.GetCollectors()
}

// ListMetrics returns all metrics from the internal registry and custom collectors.
func (c *collector) ListMetrics() map[string]any {
	return c.GetMetrics()
}

// ListMetricsByType returns metrics filtered by type from the internal registry.
func (c *collector) ListMetricsByType(metricType metrics.MetricType) map[string]any {
	return c.GetMetricsByType(metricType)
}

// ListMetricsByTag returns metrics filtered by tag from the internal registry.
func (c *collector) ListMetricsByTag(tagKey, tagValue string) map[string]any {
	return c.GetMetricsByTag(tagKey, tagValue)
}

// Stats returns collector statistics from internal state.
func (c *collector) Stats() metrics.CollectorStats {
	return c.GetStats()
}

// =============================================================================
// METRICS RETRIEVAL
// =============================================================================

// GetMetric returns a named metric from the registry.
// The returned value can be type-asserted to metrics.Counter, metrics.Gauge,
// metrics.Histogram, or metrics.Timer.
func (c *collector) GetMetric(name string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.registry.GetMetric(name, nil)
}

// GetCollector returns a named custom collector, or nil if not found.
func (c *collector) GetCollector(name string) metrics.CustomCollector {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.customCollectors[name]
}

// MetricNames returns all registered metric names without computing values.
func (c *collector) MetricNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.registry.MetricNames()
}

// MetricValue returns the scalar float64 value for a named metric.
func (c *collector) MetricValue(name string) (float64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.registry.ScalarValue(name)
}

// MetricCountsByType returns the count of metrics for each type.
func (c *collector) MetricCountsByType() map[string]int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	typeCounts := c.registry.CountByType()
	result := make(map[string]int, len(typeCounts))

	for mt, count := range typeCounts {
		result[string(mt)] = count
	}

	return result
}

// QueryMetricRange returns time-series data points for a named metric.
func (c *collector) QueryMetricRange(name string, start, end time.Time) []TimeSeriesDataPoint {
	c.mu.RLock()
	ts := c.tsStorage
	c.mu.RUnlock()

	if ts == nil {
		return nil
	}

	result, err := ts.QueryRange(context.Background(), storage.TimeRangeQuery{
		Start:   start,
		End:     end,
		Filters: map[string]string{"name": name},
	})
	if err != nil || result == nil {
		return nil
	}

	var points []TimeSeriesDataPoint
	for _, series := range result.Data {
		for _, p := range series.Points {
			points = append(points, TimeSeriesDataPoint{
				Timestamp: p.Timestamp,
				Value:     p.Value,
			})
		}
	}

	return points
}

// GetMetrics returns all metrics.
func (c *collector) GetMetrics() map[string]any {
	// Snapshot collectors and registry access without holding lock
	c.mu.RLock()
	allMetrics := c.registry.GetAllMetrics()

	collectors := make([]metrics.CustomCollector, 0, len(c.customCollectors))
	for _, collector := range c.customCollectors {
		collectors = append(collectors, collector)
	}

	c.mu.RUnlock()

	// Collect from custom collectors without holding the lock
	for _, collector := range collectors {
		customMetrics := collector.Collect()
		maps.Copy(allMetrics, customMetrics)
	}

	return allMetrics
}

// GetMetricsByType returns metrics filtered by type.
func (c *collector) GetMetricsByType(metricType metrics.MetricType) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.registry.GetMetricsByType(metricType)
}

// GetMetricsByTag returns metrics filtered by tag.
func (c *collector) GetMetricsByTag(tagKey, tagValue string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.registry.GetMetricsByTag(tagKey, tagValue)
}

// =============================================================================
// EXPORT FUNCTIONALITY
// =============================================================================

// Export exports metrics in the specified format.
func (c *collector) Export(format metrics.ExportFormat) ([]byte, error) {
	if format == metrics.ExportFormatPrometheus && c.promBridge != nil {
		return c.promBridge.GatherText()
	}

	c.mu.RLock()
	exporter, exists := c.exporters[format]
	c.mu.RUnlock()

	if !exists {
		return nil, errors.ErrServiceNotFound(string(format))
	}

	// Get metrics without holding the collector lock
	// This prevents deadlock with other systems (health checks, etc.) that may need locks
	allMetrics := c.GetMetrics()

	return exporter.Export(allMetrics)
}

// PrometheusHandler returns an http.Handler that serves the Prometheus scrape
// endpoint. Implements shared.PrometheusProvider.
func (c *collector) PrometheusHandler() http.Handler {
	if c.promBridge == nil {
		return http.NotFoundHandler()
	}
	return c.promBridge.Handler()
}

// ExportToFile exports metrics to a file.
func (c *collector) ExportToFile(format metrics.ExportFormat, filename string) error {
	data, err := c.Export(format)
	if err != nil {
		return err
	}

	// Write to file (simplified - would use proper file I/O)
	// This is a placeholder implementation
	if c.logger != nil {
		c.logger.Debug("metrics exported to file",
			logger.String("format", string(format)),
			logger.String("filename", filename),
			logger.Int("bytes", len(data)),
		)
	}

	return nil
}

// =============================================================================
// MANAGEMENT METHODS
// =============================================================================

// Reset resets all metrics.
func (c *collector) Reset() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	_ = c.registry.Reset()

	for _, collector := range c.customCollectors {
		_ = collector.Reset()
	}

	c.metricsCreated = 0
	c.metricsCollected = 0
	c.errors = c.errors[:0]

	if c.logger != nil {
		c.logger.Info("metrics collector reset")
	}

	return nil
}

// ResetMetric resets a specific metric.
func (c *collector) ResetMetric(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.registry.ResetMetric(name)
}

// =============================================================================
// HTTP METRICS MIDDLEWARE
// =============================================================================

// HTTPMiddleware returns HTTP middleware that automatically records request metrics.
// Returns nil if HTTP metrics are not enabled or the collector has not been started.
func (c *collector) HTTPMiddleware() func(http.Handler) http.Handler {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.httpCollector == nil {
		return nil
	}

	return c.httpCollector.Middleware()
}

// =============================================================================
// STATISTICS
// =============================================================================

// GetStats returns collector statistics.
func (c *collector) GetStats() CollectorStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	uptime := time.Duration(0)
	if c.started && !c.startTime.IsZero() {
		uptime = time.Since(c.startTime)
	}

	errorStrings := make([]string, len(c.errors))
	for i, err := range c.errors {
		errorStrings[i] = err.Error()
	}

	exporterStats := make(map[string]any)
	for format, exporter := range c.exporters {
		exporterStats[string(format)] = exporter.Stats()
	}

	return CollectorStats{
		Name:               c.name,
		Started:            c.started,
		StartTime:          c.startTime,
		Uptime:             uptime,
		MetricsCreated:     c.metricsCreated,
		MetricsCollected:   c.metricsCollected,
		CustomCollectors:   len(c.customCollectors),
		ActiveMetrics:      c.registry.Count(),
		LastCollectionTime: c.lastCollectionTime,
		Errors:             errorStrings,
		ExporterStats:      exporterStats,
	}
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// initializeExporters initializes all exporters.
func (c *collector) initializeExporters() {
	// Initialize JSON exporter
	c.exporters[metrics.ExportFormatJSON] = exporters.NewJSONExporter()

	// Initialize InfluxDB exporter
	c.exporters[metrics.ExportFormatInflux] = exporters.NewInfluxExporter()

	// Initialize StatsD exporter
	c.exporters[metrics.ExportFormatStatsD] = exporters.NewStatsDExporter()
}

// initializeBuiltinCollectors initializes built-in collectors.
func (c *collector) initializeBuiltinCollectors() error {
	// Register system metrics collector
	// NOTE: Uses registerCollectorLocked because this method is called from
	// Start() which already holds c.mu.Lock(). Go mutexes are not reentrant.
	if c.config.Features.SystemMetrics {
		systemCollector := collectors.NewSystemCollector()
		if err := c.registerCollectorLocked(systemCollector); err != nil {
			return fmt.Errorf("failed to register system collector: %w", err)
		}
	}

	// Register HTTP collector
	if c.config.Features.HTTPMetrics {
		httpCollector := collectors.NewHTTPCollector()
		if err := c.registerCollectorLocked(httpCollector); err != nil {
			return fmt.Errorf("failed to register HTTP collector: %w", err)
		}

		c.httpCollector = httpCollector.(*collectors.HTTPCollector)
	}

	if c.logger != nil {
		c.logger.Debug("built-in collectors registered",
			logger.Bool("system", c.config.Features.SystemMetrics),
			logger.Bool("runtime", c.config.Features.RuntimeMetrics),
			logger.Bool("http", c.config.Features.HTTPMetrics),
		)
	}

	return nil
}

// collectionLoop runs the metrics collection loop.
func (c *collector) collectionLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.Collection.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collectMetrics()
		}
	}
}

// collectMetrics collects metrics from all collectors.
func (c *collector) collectMetrics() {
	// Snapshot collectors without holding lock
	c.mu.RLock()

	lastCollectionTime := time.Now()

	collectors := make([]struct {
		name      string
		collector metrics.CustomCollector
	}, 0, len(c.customCollectors))
	for name, collector := range c.customCollectors {
		collectors = append(collectors, struct {
			name      string
			collector metrics.CustomCollector
		}{name: name, collector: collector})
	}

	c.mu.RUnlock()

	// Collect from custom collectors without holding the lock
	// This prevents deadlocks when collectors do I/O or take time
	errors := make([]error, 0)

	for _, item := range collectors {
		func() {
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("panic in collector %s: %v", item.name, r)
					errors = append(errors, err)

					if c.logger != nil {
						c.logger.Error("metrics collection panic",
							logger.String("collector", item.name),
							logger.Any("panic", r),
						)
					}
				}
			}()

			item.collector.Collect()
		}()
	}

	// Update state while holding write lock
	c.mu.Lock()
	c.lastCollectionTime = lastCollectionTime
	c.metricsCollected += int64(len(collectors))
	c.errors = append(c.errors, errors...)

	// Trim error list if too long
	if len(c.errors) > 100 {
		c.errors = c.errors[len(c.errors)-50:]
	}

	ts := c.tsStorage
	c.mu.Unlock()

	// Write scalar metric values to time-series storage for historical queries.
	if ts != nil {
		now := time.Now()
		ctx := context.Background()

		for _, name := range c.registry.MetricNames() {
			if val, ok := c.registry.ScalarValue(name); ok {
				_ = ts.Store(ctx, &storage.MetricEntry{
					Name:      name,
					Value:     val,
					Timestamp: now,
				})
			}
		}
	}
}

// =============================================================================
// RELOAD CONFIGURATION
// =============================================================================

// Reload reloads the metrics configuration at runtime.
func (c *collector) Reload(config *CollectorConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if config == nil {
		return errors.New("config cannot be nil")
	}

	if c.logger != nil {
		c.logger.Info("reloading metrics configuration",
			logger.Bool("system_metrics", config.Features.SystemMetrics),
			logger.Bool("runtime_metrics", config.Features.RuntimeMetrics),
			logger.Bool("http_metrics", config.Features.HTTPMetrics),
		)
	}

	oldConfig := c.config

	// Update config
	c.config = config

	// Reinitialize exporters if changed
	exportersChanged := len(oldConfig.Exporters) != len(config.Exporters)
	if !exportersChanged {
		for name, oldExporter := range oldConfig.Exporters {
			newExporter, exists := config.Exporters[name]
			if !exists || oldExporter.Enabled != newExporter.Enabled {
				exportersChanged = true

				break
			}
		}
	}

	if exportersChanged {
		c.initializeExporters()

		if c.logger != nil {
			c.logger.Debug("exporters reinitialized")
		}
	}

	// Reinitialize built-in collectors if changed
	collectorsChanged := oldConfig.Features.SystemMetrics != config.Features.SystemMetrics ||
		oldConfig.Features.RuntimeMetrics != config.Features.RuntimeMetrics ||
		oldConfig.Features.HTTPMetrics != config.Features.HTTPMetrics

	if collectorsChanged {
		// Clear existing built-in collectors
		for name := range c.customCollectors {
			// Only clear built-in collectors (system, runtime, http)
			if name == "system" || name == "runtime" || name == "http" {
				delete(c.customCollectors, name)
			}
		}

		if err := c.initializeBuiltinCollectors(); err != nil {
			if c.logger != nil {
				c.logger.Error("failed to reinitialize built-in collectors",
					logger.Error(err),
				)
			}

			return fmt.Errorf("failed to reinitialize built-in collectors: %w", err)
		}

		if c.logger != nil {
			c.logger.Debug("built-in collectors reinitialized")
		}
	}

	if c.logger != nil {
		c.logger.Info("metrics configuration reloaded successfully")
	}

	return nil
}

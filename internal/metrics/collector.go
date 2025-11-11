package metrics

// Testing code and collectors use Reset() for cleanup without error handling.

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/metrics/collectors"
	"github.com/xraph/forge/internal/metrics/exporters"
	metrics "github.com/xraph/forge/internal/metrics/internal"
	"github.com/xraph/forge/internal/shared"
)

// =============================================================================
// METRICS COLLECTOR INTERFACE
// =============================================================================

// Metrics defines the interface for metrics collection.
type Metrics = metrics.Metrics

// =============================================================================
// COLLECTOR IMPLEMENTATION
// =============================================================================

// collector implements MetricsCollector interface.
type collector struct {
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
		EnableSystemMetrics:  true,
		EnableRuntimeMetrics: true,
		CollectionInterval:   time.Second * 10,
		StorageConfig: &StorageConfig{
			Type:   "memory",
			Config: make(map[string]any),
		},
		Exporters:   make(map[string]ExporterConfig),
		DefaultTags: make(map[string]string),
		MaxMetrics:  10000,
		BufferSize:  1000,
	}
}

// New creates a new metrics collector.
func New(config *CollectorConfig, logger logger.Logger) Metrics {
	if config == nil {
		config = DefaultCollectorConfig()
	}

	c := &collector{
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

	// Start collection goroutine
	go c.collectionLoop(ctx)

	// Start exporters
	if err := c.startExporters(ctx); err != nil {
		c.logger.Error("failed to start exporters", logger.Error(err))
		// Don't fail startup for this
	}

	if c.logger != nil {
		c.logger.Info("metrics collector started",
			logger.String("name", c.name),
			logger.Bool("system_metrics", c.config.EnableSystemMetrics),
			logger.Bool("runtime_metrics", c.config.EnableRuntimeMetrics),
			logger.Duration("collection_interval", c.config.CollectionInterval),
			logger.Int("max_metrics", c.config.MaxMetrics),
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
		col.Reset()
	}

	// Reset registry
	c.registry.Reset()

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
		if timeSinceLastCollection > c.config.CollectionInterval*2 {
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
func (c *collector) Counter(name string, tags ...string) metrics.Counter {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := metrics.NormalizeMetricName(name)
	parsedTags := metrics.MergeTags(c.config.DefaultTags, metrics.ParseTags(tags...))

	metric := c.registry.GetOrCreateCounter(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
}

// Gauge creates or retrieves a gauge metric.
func (c *collector) Gauge(name string, tags ...string) metrics.Gauge {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := metrics.NormalizeMetricName(name)
	parsedTags := metrics.MergeTags(c.config.DefaultTags, metrics.ParseTags(tags...))

	metric := c.registry.GetOrCreateGauge(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
}

// Histogram creates or retrieves a histogram metric.
func (c *collector) Histogram(name string, tags ...string) metrics.Histogram {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := metrics.NormalizeMetricName(name)
	parsedTags := metrics.MergeTags(c.config.DefaultTags, metrics.ParseTags(tags...))

	metric := c.registry.GetOrCreateHistogram(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
}

// Timer creates or retrieves a timer metric.
func (c *collector) Timer(name string, tags ...string) metrics.Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := metrics.NormalizeMetricName(name)
	parsedTags := metrics.MergeTags(c.config.DefaultTags, metrics.ParseTags(tags...))

	metric := c.registry.GetOrCreateTimer(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
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
		c.logger.Info("custom collector registered",
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

	collector.Reset()
	delete(c.customCollectors, name)

	if c.logger != nil {
		c.logger.Info("custom collector unregistered",
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
// METRICS RETRIEVAL
// =============================================================================

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
	// Snapshot exporter without holding lock during export operation
	c.mu.RLock()
	exporter, exists := c.exporters[format]
	c.mu.RUnlock()

	if !exists {
		return nil, errors.ErrServiceNotFound(string(format))
	}

	// Get metrics without holding the collector lock
	// This prevents deadlock with other systems (health checks, etc.) that may need locks
	metrics := c.GetMetrics()

	return exporter.Export(metrics)
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
		c.logger.Info("metrics exported to file",
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

	c.registry.Reset()

	for _, collector := range c.customCollectors {
		collector.Reset()
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
	// Initialize Prometheus exporter
	c.exporters[metrics.ExportFormatPrometheus] = exporters.NewPrometheusExporter()

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
	if c.config.EnableSystemMetrics {
		systemCollector := collectors.NewSystemCollector()
		if err := c.RegisterCollector(systemCollector); err != nil {
			return fmt.Errorf("failed to register system collector: %w", err)
		}
	}

	// Register runtime metrics collector
	if c.config.EnableRuntimeMetrics {
		runtimeCollector := collectors.NewRuntimeCollector()
		if err := c.RegisterCollector(runtimeCollector); err != nil {
			return fmt.Errorf("failed to register runtime collector: %w", err)
		}
	}

	// Register HTTP collector
	if c.config.EnableHTTPMetrics {
		httpCollector := collectors.NewHTTPCollector()
		if err := c.RegisterCollector(httpCollector); err != nil {
			return fmt.Errorf("failed to register HTTP collector: %w", err)
		}
	}

	if c.logger != nil {
		c.logger.Info("built-in collectors registered",
			logger.Bool("system", c.config.EnableSystemMetrics),
			logger.Bool("runtime", c.config.EnableRuntimeMetrics),
			logger.Bool("http", c.config.EnableHTTPMetrics),
		)
	}

	return nil
}

// collectionLoop runs the metrics collection loop.
func (c *collector) collectionLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.CollectionInterval)
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
	defer c.mu.Unlock()

	c.lastCollectionTime = lastCollectionTime
	c.metricsCollected += int64(len(collectors))
	c.errors = append(c.errors, errors...)

	// Trim error list if too long
	if len(c.errors) > 100 {
		c.errors = c.errors[len(c.errors)-50:]
	}
}

// startExporters starts configured exporters.
func (c *collector) startExporters(ctx context.Context) error {
	if len(c.config.Exporters) == 0 {
		return nil
	}

	for name, config := range c.config.Exporters {
		if config.Enabled {
			if err := c.startExporter(ctx, name, config); err != nil {
				c.logger.Error("failed to start exporter",
					logger.String("exporter", name),
					logger.Error(err),
				)
			}
		}
	}

	return nil
}

// startExporter starts a specific exporter.
func (c *collector) startExporter(ctx context.Context, name string, config ExporterConfig) error {
	if c.logger != nil {
		c.logger.Info("starting exporter",
			logger.String("exporter", name),
			logger.Duration("interval", config.Interval),
		)
	}

	// Start exporter goroutine
	go c.exporterLoop(ctx, name, config)

	return nil
}

// exporterLoop runs the exporter loop.
func (c *collector) exporterLoop(ctx context.Context, name string, config ExporterConfig) {
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !c.started {
				return
			}

			if err := c.performExport(name, config); err != nil {
				c.logger.Error("export failed",
					logger.String("exporter", name),
					logger.Error(err),
				)
			}
		}
	}
}

// performExport performs a single export operation.
func (c *collector) performExport(name string, config ExporterConfig) error {
	// Determine export format based on exporter name
	var format metrics.ExportFormat

	switch name {
	case "prometheus":
		format = metrics.ExportFormatPrometheus
	case "json":
		format = metrics.ExportFormatJSON
	case "influx":
		format = metrics.ExportFormatInflux
	case "statsd":
		format = metrics.ExportFormatStatsD
	default:
		return fmt.Errorf("unknown exporter: %s", name)
	}

	// Export metrics
	data, err := c.Export(format)
	if err != nil {
		return fmt.Errorf("failed to export metrics: %w", err)
	}

	// Process exported data based on configuration
	if err := c.processExportedData(name, config, data); err != nil {
		return fmt.Errorf("failed to process exported data: %w", err)
	}

	return nil
}

// processExportedData processes exported data.
func (c *collector) processExportedData(exporterName string, config ExporterConfig, data []byte) error {
	// This is a placeholder implementation
	// In a real implementation, this would:
	// - Send data to monitoring systems
	// - Write to files
	// - Post to HTTP endpoints
	// - Send via network protocols
	if c.logger != nil {
		c.logger.Debug("exported metrics",
			logger.String("exporter", exporterName),
			logger.Int("bytes", len(data)),
		)
	}

	return nil
}

// stopExporters stops all exporters.
func (c *collector) stopExporters(ctx context.Context) error {
	// Exporters are stopped by context cancellation
	return nil
}

// getActiveExporters returns the number of active exporters.
func (c *collector) getActiveExporters() int {
	count := 0

	for _, config := range c.config.Exporters {
		if config.Enabled {
			count++
		}
	}

	return count
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
			logger.Bool("system_metrics", config.EnableSystemMetrics),
			logger.Bool("runtime_metrics", config.EnableRuntimeMetrics),
			logger.Bool("http_metrics", config.EnableHTTPMetrics),
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
			c.logger.Info("exporters reinitialized")
		}
	}

	// Reinitialize built-in collectors if changed
	collectorsChanged := oldConfig.EnableSystemMetrics != config.EnableSystemMetrics ||
		oldConfig.EnableRuntimeMetrics != config.EnableRuntimeMetrics ||
		oldConfig.EnableHTTPMetrics != config.EnableHTTPMetrics

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
			c.logger.Info("built-in collectors reinitialized")
		}
	}

	if c.logger != nil {
		c.logger.Info("metrics configuration reloaded successfully")
	}

	return nil
}

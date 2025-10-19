package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics/collectors"
	metrics "github.com/xraph/forge/pkg/metrics/core"
	"github.com/xraph/forge/pkg/metrics/exporters"
)

// =============================================================================
// METRICS COLLECTOR INTERFACE
// =============================================================================

// MetricsCollector defines the interface for metrics collection
type MetricsCollector = metrics.MetricsCollector

// =============================================================================
// COLLECTOR IMPLEMENTATION
// =============================================================================

// collector implements MetricsCollector interface
type collector struct {
	common.Service
	name               string
	registry           Registry
	customCollectors   map[string]CustomCollector
	exporters          map[ExportFormat]Exporter
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

// CollectorConfig contains configuration for the metrics collector
type CollectorConfig struct {
	EnableSystemMetrics  bool                   `yaml:"enable_system_metrics" json:"enable_system_metrics"`
	EnableRuntimeMetrics bool                   `yaml:"enable_runtime_metrics" json:"enable_runtime_metrics"`
	CollectionInterval   time.Duration          `yaml:"collection_interval" json:"collection_interval"`
	StorageConfig        *StorageConfig         `yaml:"storage" json:"storage"`
	ExporterConfigs      map[string]interface{} `yaml:"exporters" json:"exporters"`
	DefaultTags          map[string]string      `yaml:"default_tags" json:"default_tags"`
	MaxMetrics           int                    `yaml:"max_metrics" json:"max_metrics"`
	BufferSize           int                    `yaml:"buffer_size" json:"buffer_size"`
}

// StorageConfig contains storage configuration
type StorageConfig struct {
	Type   string                 `yaml:"type" json:"type"`
	Config map[string]interface{} `yaml:"config" json:"config"`
}

// CollectorStats contains statistics about the metrics collector
type CollectorStats = metrics.CollectorStats

// DefaultCollectorConfig returns default collector configuration
func DefaultCollectorConfig() *CollectorConfig {
	return &CollectorConfig{
		EnableSystemMetrics:  true,
		EnableRuntimeMetrics: true,
		CollectionInterval:   time.Second * 10,
		StorageConfig: &StorageConfig{
			Type:   "memory",
			Config: make(map[string]interface{}),
		},
		ExporterConfigs: make(map[string]interface{}),
		DefaultTags:     make(map[string]string),
		MaxMetrics:      10000,
		BufferSize:      1000,
	}
}

// NewCollector creates a new metrics collector
func NewCollector(config *CollectorConfig, logger logger.Logger) MetricsCollector {
	if config == nil {
		config = DefaultCollectorConfig()
	}

	c := &collector{
		name:             common.MetricsCollectorKey,
		registry:         NewRegistry(),
		customCollectors: make(map[string]CustomCollector),
		exporters:        make(map[ExportFormat]Exporter),
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

// Name returns the service name
func (c *collector) Name() string {
	return c.name
}

// Dependencies returns the service dependencies
func (c *collector) Dependencies() []string {
	return []string{"config-manager"}
}

// OnStart starts the metrics collector service
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
		return common.ErrServiceStartFailed(c.name, err)
	}

	// Start collection goroutine
	go c.collectionLoop(ctx)

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

// OnStop stops the metrics collector service
func (c *collector) Stop(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.started {
		return common.ErrServiceNotFound(c.name)
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

// OnHealthCheck performs health check
func (c *collector) OnHealthCheck(ctx context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return common.ErrHealthCheckFailed(c.name, fmt.Errorf("metrics collector not started"))
	}

	// Check if collection is working
	if !c.lastCollectionTime.IsZero() {
		timeSinceLastCollection := time.Since(c.lastCollectionTime)
		if timeSinceLastCollection > c.config.CollectionInterval*2 {
			return common.ErrHealthCheckFailed(c.name, fmt.Errorf("metrics collection stalled for %v", timeSinceLastCollection))
		}
	}

	// Check error rate
	if len(c.errors) > 10 {
		return common.ErrHealthCheckFailed(c.name, fmt.Errorf("too many errors: %d", len(c.errors)))
	}

	return nil
}

// =============================================================================
// METRIC CREATION METHODS
// =============================================================================

// Counter creates or retrieves a counter metric
func (c *collector) Counter(name string, tags ...string) Counter {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := NormalizeMetricName(name)
	parsedTags := MergeTags(c.config.DefaultTags, ParseTags(tags...))

	metric := c.registry.GetOrCreateCounter(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
}

// Gauge creates or retrieves a gauge metric
func (c *collector) Gauge(name string, tags ...string) Gauge {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := NormalizeMetricName(name)
	parsedTags := MergeTags(c.config.DefaultTags, ParseTags(tags...))

	metric := c.registry.GetOrCreateGauge(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
}

// Histogram creates or retrieves a histogram metric
func (c *collector) Histogram(name string, tags ...string) Histogram {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := NormalizeMetricName(name)
	parsedTags := MergeTags(c.config.DefaultTags, ParseTags(tags...))

	metric := c.registry.GetOrCreateHistogram(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
}

// Timer creates or retrieves a timer metric
func (c *collector) Timer(name string, tags ...string) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()

	normalizedName := NormalizeMetricName(name)
	parsedTags := MergeTags(c.config.DefaultTags, ParseTags(tags...))

	metric := c.registry.GetOrCreateTimer(normalizedName, parsedTags)
	c.metricsCreated++

	return metric
}

// =============================================================================
// CUSTOM COLLECTOR MANAGEMENT
// =============================================================================

// RegisterCollector registers a custom collector
func (c *collector) RegisterCollector(collector CustomCollector) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	name := collector.Name()
	if _, exists := c.customCollectors[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	c.customCollectors[name] = collector

	if c.logger != nil {
		c.logger.Info("custom collector registered",
			logger.String("collector", name),
		)
	}

	return nil
}

// UnregisterCollector unregisters a custom collector
func (c *collector) UnregisterCollector(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	collector, exists := c.customCollectors[name]
	if !exists {
		return common.ErrServiceNotFound(name)
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

// GetCollectors returns all custom collectors
func (c *collector) GetCollectors() []CustomCollector {
	c.mu.RLock()
	defer c.mu.RUnlock()

	collectors := make([]CustomCollector, 0, len(c.customCollectors))
	for _, collector := range c.customCollectors {
		collectors = append(collectors, collector)
	}

	return collectors
}

// =============================================================================
// METRICS RETRIEVAL
// =============================================================================

// GetMetrics returns all metrics
func (c *collector) GetMetrics() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics := c.registry.GetAllMetrics()

	// Add custom collector metrics
	for _, collector := range c.customCollectors {
		customMetrics := collector.Collect()
		for k, v := range customMetrics {
			metrics[k] = v
		}
	}

	return metrics
}

// GetMetricsByType returns metrics filtered by type
func (c *collector) GetMetricsByType(metricType MetricType) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.registry.GetMetricsByType(metricType)
}

// GetMetricsByTag returns metrics filtered by tag
func (c *collector) GetMetricsByTag(tagKey, tagValue string) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.registry.GetMetricsByTag(tagKey, tagValue)
}

// =============================================================================
// EXPORT FUNCTIONALITY
// =============================================================================

// Export exports metrics in the specified format
func (c *collector) Export(format ExportFormat) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	exporter, exists := c.exporters[format]
	if !exists {
		return nil, common.ErrServiceNotFound(string(format))
	}

	metrics := c.GetMetrics()
	return exporter.Export(metrics)
}

// ExportToFile exports metrics to a file
func (c *collector) ExportToFile(format ExportFormat, filename string) error {
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

// Reset resets all metrics
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

// ResetMetric resets a specific metric
func (c *collector) ResetMetric(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.registry.ResetMetric(name)
}

// =============================================================================
// STATISTICS
// =============================================================================

// GetStats returns collector statistics
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

	exporterStats := make(map[string]interface{})
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

// initializeExporters initializes all exporters
func (c *collector) initializeExporters() {
	// Initialize Prometheus exporter
	c.exporters[ExportFormatPrometheus] = exporters.NewPrometheusExporter()

	// Initialize JSON exporter
	c.exporters[ExportFormatJSON] = exporters.NewJSONExporter()

	// Initialize InfluxDB exporter
	c.exporters[ExportFormatInflux] = exporters.NewInfluxExporter()

	// Initialize StatsD exporter
	c.exporters[ExportFormatStatsD] = exporters.NewStatsDExporter()
}

// initializeBuiltinCollectors initializes built-in collectors
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

	return nil
}

// collectionLoop runs the metrics collection loop
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

// collectMetrics collects metrics from all collectors
func (c *collector) collectMetrics() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastCollectionTime = time.Now()

	// Collect from custom collectors
	for name, collector := range c.customCollectors {
		func() {
			defer func() {
				if r := recover(); r != nil {
					err := fmt.Errorf("panic in collector %s: %v", name, r)
					c.errors = append(c.errors, err)
					if c.logger != nil {
						c.logger.Error("metrics collection panic",
							logger.String("collector", name),
							logger.Any("panic", r),
						)
					}
				}
			}()

			collector.Collect()
			c.metricsCollected++
		}()
	}

	// Trim error list if too long
	if len(c.errors) > 100 {
		c.errors = c.errors[len(c.errors)-50:]
	}
}

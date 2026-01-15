package persistence

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge/errors"
	health "github.com/xraph/forge/internal/health/internal"
	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/go-utils/metrics"
)

// HealthMetricsCollector collects metrics from health data.
type HealthMetricsCollector struct {
	store       HealthStore
	metrics     shared.Metrics
	logger      logger.Logger
	config      *MetricsCollectorConfig
	stopCh      chan struct{}
	mu          sync.RWMutex
	started     bool
	lastCollect time.Time
}

// MetricsCollectorConfig contains configuration for health metrics collection.
type MetricsCollectorConfig struct {
	Enabled            bool          `json:"enabled"             yaml:"enabled"`
	CollectionInterval time.Duration `json:"collection_interval" yaml:"collection_interval"`
	HistoryDuration    time.Duration `json:"history_duration"    yaml:"history_duration"`
	EnableTrends       bool          `json:"enable_trends"       yaml:"enable_trends"`
	EnableStatistics   bool          `json:"enable_statistics"   yaml:"enable_statistics"`
	MetricPrefix       string        `json:"metric_prefix"       yaml:"metric_prefix"`
	Tags               []string      `json:"tags"                yaml:"tags"`
	ServicesFilter     []string      `json:"services_filter"     yaml:"services_filter"`
	ExcludeServices    []string      `json:"exclude_services"    yaml:"exclude_services"`
}

// DefaultMetricsCollectorConfig returns default configuration.
func DefaultMetricsCollectorConfig() *MetricsCollectorConfig {
	return &MetricsCollectorConfig{
		Enabled:            true,
		CollectionInterval: 30 * time.Second,
		HistoryDuration:    24 * time.Hour,
		EnableTrends:       true,
		EnableStatistics:   true,
		MetricPrefix:       "forge.health",
		Tags:               []string{},
		ServicesFilter:     []string{},
		ExcludeServices:    []string{},
	}
}

// NewHealthMetricsCollector creates a new health metrics collector.
func NewHealthMetricsCollector(store HealthStore, metrics shared.Metrics, logger logger.Logger, config *MetricsCollectorConfig) *HealthMetricsCollector {
	if config == nil {
		config = DefaultMetricsCollectorConfig()
	}

	return &HealthMetricsCollector{
		store:   store,
		metrics: metrics,
		logger:  logger,
		config:  config,
		stopCh:  make(chan struct{}),
	}
}

// Start starts the metrics collection.
func (hmc *HealthMetricsCollector) Start(ctx context.Context) error {
	hmc.mu.Lock()
	defer hmc.mu.Unlock()

	if hmc.started {
		return errors.ErrServiceAlreadyExists("health-metrics-collector")
	}

	if !hmc.config.Enabled {
		if hmc.logger != nil {
			hmc.logger.Info("health metrics collection disabled")
		}

		return nil
	}

	hmc.started = true

	// Start collection routine
	go hmc.collectionLoop(ctx)

	if hmc.logger != nil {
		hmc.logger.Info("health metrics collector started",
			logger.Duration("interval", hmc.config.CollectionInterval),
			logger.Duration("history", hmc.config.HistoryDuration),
			logger.Bool("trends", hmc.config.EnableTrends),
			logger.Bool("statistics", hmc.config.EnableStatistics),
		)
	}

	return nil
}

// Stop stops the metrics collection.
func (hmc *HealthMetricsCollector) Stop(ctx context.Context) error {
	hmc.mu.Lock()
	defer hmc.mu.Unlock()

	if !hmc.started {
		return nil
	}

	hmc.started = false
	close(hmc.stopCh)

	if hmc.logger != nil {
		hmc.logger.Info("health metrics collector stopped")
	}

	return nil
}

// collectionLoop runs the periodic metrics collection.
func (hmc *HealthMetricsCollector) collectionLoop(ctx context.Context) {
	ticker := time.NewTicker(hmc.config.CollectionInterval)
	defer ticker.Stop()

	// Initial collection
	hmc.collectMetrics(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-hmc.stopCh:
			return
		case <-ticker.C:
			hmc.collectMetrics(ctx)
		}
	}
}

// collectMetrics collects all health metrics.
func (hmc *HealthMetricsCollector) collectMetrics(ctx context.Context) {
	start := time.Now()

	hmc.mu.Lock()
	hmc.lastCollect = start
	hmc.mu.Unlock()

	// Collect current health status metrics
	hmc.collectCurrentMetrics(ctx)

	// Collect trend metrics if enabled
	if hmc.config.EnableTrends {
		hmc.collectTrendMetrics(ctx)
	}

	// Collect statistics metrics if enabled
	if hmc.config.EnableStatistics {
		hmc.collectStatisticsMetrics(ctx)
	}

	// Record collection metrics
	duration := time.Since(start)

	hmc.metrics.Counter(hmc.metricName("collection_runs")).Inc()
	hmc.metrics.Histogram(hmc.metricName("collection_duration")).Observe(duration.Seconds())

	if hmc.logger != nil {
		hmc.logger.Debug("health metrics collected",
			logger.Duration("duration", duration),
		)
	}
}

// collectCurrentMetrics collects current health status metrics.
func (hmc *HealthMetricsCollector) collectCurrentMetrics(ctx context.Context) {
	// Get recent health reports
	from := time.Now().Add(-hmc.config.CollectionInterval * 2)
	to := time.Now()

	reports, err := hmc.store.GetReports(ctx, from, to)
	if err != nil {
		if hmc.logger != nil {
			hmc.logger.Error("failed to get recent health reports",
				logger.Error(err),
			)
		}

		return
	}

	if len(reports) == 0 {
		return
	}

	// Use the most recent report
	report := reports[len(reports)-1]

	// Overall health status
	hmc.recordHealthStatus(hmc.metricName("overall_status"), report.Overall, map[string]string{})

	// Service-specific metrics
	for serviceName, result := range report.Services {
		if !hmc.shouldIncludeService(serviceName) {
			continue
		}

		tags := []string{"service", serviceName}
		if result.Critical {
			tags = append(tags, "critical", "true")
		}

		tagsMap := map[string]string{}
		for _, tag := range tags {
			tagsMap[tag] = tag
		}

		// Service health status
		hmc.recordHealthStatus(hmc.metricName("service_status"), result.Status, tagsMap)

		// Service response time
		hmc.metrics.Histogram(hmc.metricName("service_duration"), metrics.WithLabels(tagsMap)).
			Observe(result.Duration.Seconds())

		// Service uptime (1 for healthy, 0 for unhealthy)
		uptime := 0.0
		if result.Status == health.HealthStatusHealthy {
			uptime = 1.0
		}

		hmc.metrics.Gauge(hmc.metricName("service_uptime"), metrics.WithLabels(tagsMap)).Set(uptime)

		// Service error indicator
		hasError := 0.0
		if result.Error != "" {
			hasError = 1.0
		}

		hmc.metrics.Gauge(hmc.metricName("service_error"), metrics.WithLabels(tagsMap)).Set(hasError)
	}

	healthReportAnalyzer := metrics.NewHealthReportAnalyzer(report)

	// Report-level metrics
	hmc.metrics.Counter(hmc.metricName("reports_processed")).Inc()
	hmc.metrics.Histogram(hmc.metricName("report_duration")).Observe(report.Duration.Seconds())
	hmc.metrics.Gauge(hmc.metricName("services_total")).Set(float64(len(report.Services)))
	hmc.metrics.Gauge(hmc.metricName("services_healthy")).Set(float64(healthReportAnalyzer.HealthyCount()))
	hmc.metrics.Gauge(hmc.metricName("services_degraded")).Set(float64(healthReportAnalyzer.DegradedCount()))
	hmc.metrics.Gauge(hmc.metricName("services_unhealthy")).Set(float64(healthReportAnalyzer.UnhealthyCount()))
}

// collectTrendMetrics collects trend analysis metrics.
func (hmc *HealthMetricsCollector) collectTrendMetrics(ctx context.Context) {
	// Get services from recent reports
	services := hmc.getRecentServices(ctx)

	for _, serviceName := range services {
		if !hmc.shouldIncludeService(serviceName) {
			continue
		}

		// Get trend for this service
		trend, err := hmc.store.GetHealthTrend(ctx, serviceName, hmc.config.HistoryDuration)
		if err != nil {
			if hmc.logger != nil {
				hmc.logger.Error("failed to get health trend",
					logger.String("service", serviceName),
					logger.Error(err),
				)
			}

			continue
		}

		tags := map[string]string{"service": serviceName}

		// Trend metrics
		hmc.metrics.Gauge(hmc.metricName("trend_success_rate"), metrics.WithLabels(tags)).Set(trend.SuccessRate)
		hmc.metrics.Gauge(hmc.metricName("trend_error_rate"), metrics.WithLabels(tags)).Set(trend.ErrorRate)
		hmc.metrics.Gauge(hmc.metricName("trend_total_checks"), metrics.WithLabels(tags)).Set(float64(trend.TotalChecks))
		hmc.metrics.Gauge(hmc.metricName("trend_consecutive_failures"), metrics.WithLabels(tags)).Set(float64(trend.ConsecutiveFailures))
		hmc.metrics.Histogram(hmc.metricName("trend_avg_duration"), metrics.WithLabels(tags)).Observe(trend.AvgDuration.Seconds())

		// Status distribution
		for status, count := range trend.StatusDistribution {
			statusTags := map[string]string{"status": string(status)}
			hmc.metrics.Gauge(hmc.metricName("trend_status_distribution"), metrics.WithLabels(statusTags)).Set(float64(count))
		}

		// Trend direction
		trendValue := hmc.getTrendValue(trend.Trend)
		hmc.metrics.Gauge(hmc.metricName("trend_direction"), metrics.WithLabels(tags)).Set(trendValue)
	}
}

// collectStatisticsMetrics collects statistical metrics.
func (hmc *HealthMetricsCollector) collectStatisticsMetrics(ctx context.Context) {
	// Get services from recent reports
	services := hmc.getRecentServices(ctx)

	from := time.Now().Add(-hmc.config.HistoryDuration)
	to := time.Now()

	for _, serviceName := range services {
		if !hmc.shouldIncludeService(serviceName) {
			continue
		}

		// Get statistics for this service
		stats, err := hmc.store.GetHealthStatistics(ctx, serviceName, from, to)
		if err != nil {
			if hmc.logger != nil {
				hmc.logger.Error("failed to get health statistics",
					logger.String("service", serviceName),
					logger.Error(err),
				)
			}

			continue
		}

		tags := map[string]string{"service": serviceName}

		// Basic statistics
		hmc.metrics.Gauge(hmc.metricName("stats_total_checks"), metrics.WithLabels(tags)).Set(float64(stats.TotalChecks))
		hmc.metrics.Gauge(hmc.metricName("stats_success_count"), metrics.WithLabels(tags)).Set(float64(stats.SuccessCount))
		hmc.metrics.Gauge(hmc.metricName("stats_failure_count"), metrics.WithLabels(tags)).Set(float64(stats.FailureCount))
		hmc.metrics.Gauge(hmc.metricName("stats_success_rate"), metrics.WithLabels(tags)).Set(stats.SuccessRate)

		// Duration statistics
		hmc.metrics.Histogram(hmc.metricName("stats_avg_duration"), metrics.WithLabels(tags)).Observe(stats.AvgDuration.Seconds())
		hmc.metrics.Gauge(hmc.metricName("stats_min_duration"), metrics.WithLabels(tags)).Set(stats.MinDuration.Seconds())
		hmc.metrics.Gauge(hmc.metricName("stats_max_duration"), metrics.WithLabels(tags)).Set(stats.MaxDuration.Seconds())

		// Availability statistics
		hmc.metrics.Gauge(hmc.metricName("stats_uptime"), metrics.WithLabels(tags)).Set(stats.Uptime.Seconds())
		hmc.metrics.Gauge(hmc.metricName("stats_downtime"), metrics.WithLabels(tags)).Set(stats.Downtime.Seconds())
		hmc.metrics.Gauge(hmc.metricName("stats_mtbf"), metrics.WithLabels(tags)).Set(stats.MTBF.Seconds())
		hmc.metrics.Gauge(hmc.metricName("stats_mttr"), metrics.WithLabels(tags)).Set(stats.MTTR.Seconds())

		// Status distribution
		for status, count := range stats.StatusCounts {
			statusTags := map[string]string{"status": string(status)}
			hmc.metrics.Gauge(hmc.metricName("stats_status_counts"), metrics.WithLabels(statusTags)).Set(float64(count))
		}

		// Error categories
		for errorType, count := range stats.ErrorCategories {
			errorTags := map[string]string{"error_type": hmc.sanitizeTag(errorType)}
			hmc.metrics.Gauge(hmc.metricName("stats_error_categories"), metrics.WithLabels(errorTags)).Set(float64(count))
		}
	}
}

// getRecentServices gets list of services from recent reports.
func (hmc *HealthMetricsCollector) getRecentServices(ctx context.Context) []string {
	from := time.Now().Add(-hmc.config.CollectionInterval * 5)
	to := time.Now()

	reports, err := hmc.store.GetReports(ctx, from, to)
	if err != nil {
		return []string{}
	}

	serviceSet := make(map[string]bool)

	for _, report := range reports {
		for serviceName := range report.Services {
			serviceSet[serviceName] = true
		}
	}

	services := make([]string, 0, len(serviceSet))
	for serviceName := range serviceSet {
		services = append(services, serviceName)
	}

	return services
}

// shouldIncludeService determines if a service should be included in metrics.
func (hmc *HealthMetricsCollector) shouldIncludeService(serviceName string) bool {
	// Check exclude list first
	if slices.Contains(hmc.config.ExcludeServices, serviceName) {
		return false
	}

	// If filter list is empty, include all (except excluded)
	if len(hmc.config.ServicesFilter) == 0 {
		return true
	}

	// Check filter list
	return slices.Contains(hmc.config.ServicesFilter, serviceName)
}

// recordHealthStatus records health status as a metric.
func (hmc *HealthMetricsCollector) recordHealthStatus(metricName string, status health.HealthStatus, tags map[string]string) {
	// Record as gauge with numeric value
	value := hmc.getStatusValue(status)
	hmc.metrics.Gauge(metricName, metrics.WithLabels(tags)).Set(value)

	// Also record as separate metrics for each status
	statusTags := map[string]string{"status": string(status)}
	hmc.metrics.Gauge(metricName+"_by_status", metrics.WithLabels(statusTags)).Set(1.0)
}

// getStatusValue converts health status to numeric value.
func (hmc *HealthMetricsCollector) getStatusValue(status health.HealthStatus) float64 {
	switch status {
	case health.HealthStatusHealthy:
		return 1.0
	case health.HealthStatusDegraded:
		return 0.5
	case health.HealthStatusUnhealthy:
		return 0.0
	case health.HealthStatusUnknown:
		return -1.0
	default:
		return -1.0
	}
}

// getTrendValue converts trend direction to numeric value.
func (hmc *HealthMetricsCollector) getTrendValue(trend string) float64 {
	switch trend {
	case "improving":
		return 1.0
	case "stable":
		return 0.0
	case "degrading":
		return -1.0
	case "unhealthy":
		return -2.0
	default:
		return 0.0
	}
}

// metricName creates a metric name with prefix.
func (hmc *HealthMetricsCollector) metricName(name string) string {
	return fmt.Sprintf("%s.%s", hmc.config.MetricPrefix, name)
}

// sanitizeTag sanitizes tag values for metrics.
func (hmc *HealthMetricsCollector) sanitizeTag(tag string) string {
	// Replace problematic characters
	tag = strings.ReplaceAll(tag, " ", "_")
	tag = strings.ReplaceAll(tag, "-", "_")
	tag = strings.ReplaceAll(tag, ".", "_")
	tag = strings.ReplaceAll(tag, "/", "_")
	tag = strings.ToLower(tag)

	// Limit length
	if len(tag) > 50 {
		tag = tag[:50]
	}

	return tag
}

// GetLastCollectTime returns the last collection time.
func (hmc *HealthMetricsCollector) GetLastCollectTime() time.Time {
	hmc.mu.RLock()
	defer hmc.mu.RUnlock()

	return hmc.lastCollect
}

// IsStarted returns true if the collector is started.
func (hmc *HealthMetricsCollector) IsStarted() bool {
	hmc.mu.RLock()
	defer hmc.mu.RUnlock()

	return hmc.started
}

// HealthMetricsExporter exports health metrics to external systems.
type HealthMetricsExporter struct {
	collector *HealthMetricsCollector
	config    *ExporterConfig
	logger    logger.Logger
}

// ExporterConfig contains configuration for health metrics export.
type ExporterConfig struct {
	Format         string            `json:"format"         yaml:"format"`         // prometheus, json, influx
	Endpoint       string            `json:"endpoint"       yaml:"endpoint"`       // Export endpoint
	Interval       time.Duration     `json:"interval"       yaml:"interval"`       // Export interval
	Timeout        time.Duration     `json:"timeout"        yaml:"timeout"`        // Request timeout
	Headers        map[string]string `json:"headers"        yaml:"headers"`        // HTTP headers
	Authentication map[string]string `json:"authentication" yaml:"authentication"` // Auth config
}

// NewHealthMetricsExporter creates a new health metrics exporter.
func NewHealthMetricsExporter(collector *HealthMetricsCollector, config *ExporterConfig, logger logger.Logger) *HealthMetricsExporter {
	return &HealthMetricsExporter{
		collector: collector,
		config:    config,
		logger:    logger,
	}
}

// Export exports health metrics.
func (hme *HealthMetricsExporter) Export(ctx context.Context) error {
	// Implementation depends on the format and endpoint
	// This is a placeholder for actual export logic
	if hme.logger != nil {
		hme.logger.Info("exporting health metrics",
			logger.String("format", hme.config.Format),
			logger.String("endpoint", hme.config.Endpoint),
		)
	}

	return nil
}

// HealthMetricsService provides health metrics as a service.
type HealthMetricsService struct {
	collector *HealthMetricsCollector
	exporter  *HealthMetricsExporter
	store     HealthStore
	config    *HealthMetricsServiceConfig
	logger    logger.Logger
	metrics   shared.Metrics
}

// HealthMetricsServiceConfig contains configuration for the health metrics service.
type HealthMetricsServiceConfig struct {
	CollectorConfig *MetricsCollectorConfig `json:"collector" yaml:"collector"`
	ExporterConfig  *ExporterConfig         `json:"exporter"  yaml:"exporter"`
	Enabled         bool                    `json:"enabled"   yaml:"enabled"`
}

// NewHealthMetricsService creates a new health metrics service.
func NewHealthMetricsService(store HealthStore, metrics shared.Metrics, logger logger.Logger, config *HealthMetricsServiceConfig) *HealthMetricsService {
	if config == nil {
		config = &HealthMetricsServiceConfig{
			CollectorConfig: DefaultMetricsCollectorConfig(),
			Enabled:         true,
		}
	}

	collector := NewHealthMetricsCollector(store, metrics, logger, config.CollectorConfig)

	var exporter *HealthMetricsExporter
	if config.ExporterConfig != nil {
		exporter = NewHealthMetricsExporter(collector, config.ExporterConfig, logger)
	}

	return &HealthMetricsService{
		collector: collector,
		exporter:  exporter,
		store:     store,
		config:    config,
		logger:    logger,
		metrics:   metrics,
	}
}

// Name returns the service name.
func (hms *HealthMetricsService) Name() string {
	return "health-metrics"
}

// // Dependencies returns the service dependencies
// func (hms *HealthMetricsService) Dependencies() []string {
// 	return []string{shared.HealthCheckerKey, shared.MetricsCollectorKey}
// }

// OnStart starts the health metrics service.
func (hms *HealthMetricsService) Start(ctx context.Context) error {
	if !hms.config.Enabled {
		if hms.logger != nil {
			hms.logger.Info("health metrics service disabled")
		}

		return nil
	}

	// Start collector
	if err := hms.collector.Start(ctx); err != nil {
		return errors.ErrServiceStartFailed("health-metrics-collector", err)
	}

	// Start exporter if configured
	if hms.exporter != nil {
		// Start export routine
		go hms.exportLoop(ctx)
	}

	if hms.logger != nil {
		hms.logger.Info("health metrics service started")
	}

	return nil
}

// OnStop stops the health metrics service.
func (hms *HealthMetricsService) Stop(ctx context.Context) error {
	if !hms.config.Enabled {
		return nil
	}

	// Stop collector
	if err := hms.collector.Stop(ctx); err != nil {
		if hms.logger != nil {
			hms.logger.Error("failed to stop health metrics collector",
				logger.Error(err),
			)
		}
	}

	if hms.logger != nil {
		hms.logger.Info("health metrics service stopped")
	}

	return nil
}

// OnHealthCheck performs health check.
func (hms *HealthMetricsService) OnHealthCheck(ctx context.Context) error {
	if !hms.config.Enabled {
		return nil
	}

	if !hms.collector.IsStarted() {
		return errors.ErrServiceNotFound("health metrics collector not started")
	}

	// Check if collector is collecting metrics
	lastCollect := hms.collector.GetLastCollectTime()
	if time.Since(lastCollect) > hms.config.CollectorConfig.CollectionInterval*2 {
		return errors.ErrHealthCheckFailed(hms.Name(), errors.New("health metrics collection is stale"))
	}

	return nil
}

// exportLoop runs the export loop.
func (hms *HealthMetricsService) exportLoop(ctx context.Context) {
	if hms.exporter == nil || hms.config.ExporterConfig == nil {
		return
	}

	ticker := time.NewTicker(hms.config.ExporterConfig.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := hms.exporter.Export(ctx); err != nil {
				if hms.logger != nil {
					hms.logger.Error("failed to export health metrics",
						logger.Error(err),
					)
				}
			}
		}
	}
}

// GetCollector returns the metrics collector.
func (hms *HealthMetricsService) GetCollector() *HealthMetricsCollector {
	return hms.collector
}

// GetExporter returns the metrics exporter.
func (hms *HealthMetricsService) GetExporter() *HealthMetricsExporter {
	return hms.exporter
}

// CollectNow triggers immediate metrics collection.
func (hms *HealthMetricsService) CollectNow(ctx context.Context) error {
	hms.collector.collectMetrics(ctx)

	return nil
}

// ExportNow triggers immediate metrics export.
func (hms *HealthMetricsService) ExportNow(ctx context.Context) error {
	if hms.exporter == nil {
		return errors.ErrServiceNotFound("no exporter configured")
	}

	return hms.exporter.Export(ctx)
}

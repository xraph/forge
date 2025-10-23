package dashboard

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v2"
	"github.com/xraph/forge/v2/internal/shared"
)

// DataCollector collects dashboard data periodically
type DataCollector struct {
	healthManager forge.HealthManager
	metrics       forge.Metrics
	container     shared.Container
	logger        forge.Logger
	history       *DataHistory

	stopCh chan struct{}
	mu     sync.RWMutex
}

// NewDataCollector creates a new data collector
func NewDataCollector(
	healthManager forge.HealthManager,
	metrics forge.Metrics,
	container shared.Container,
	logger forge.Logger,
	history *DataHistory,
) *DataCollector {
	return &DataCollector{
		healthManager: healthManager,
		metrics:       metrics,
		container:     container,
		logger:        logger,
		history:       history,
		stopCh:        make(chan struct{}),
	}
}

// Start starts periodic data collection
func (dc *DataCollector) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Collect immediately on start (non-blocking to avoid deadlock during app startup)
	go dc.collect(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-dc.stopCh:
			return
		case <-ticker.C:
			dc.collect(ctx)
		}
	}
}

// Stop stops data collection
func (dc *DataCollector) Stop() {
	close(dc.stopCh)
}

// CollectOverview collects overview data
func (dc *DataCollector) CollectOverview(ctx context.Context) *OverviewData {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	// Get health status
	healthStatus := dc.healthManager.GetStatus()
	healthReport := dc.healthManager.GetLastReport()

	// Get metrics
	metrics := dc.metrics.GetMetrics()

	// Get services
	services := dc.container.Services()

	// Count healthy services
	healthyCount := 0
	if healthReport != nil {
		healthyCount = healthReport.GetHealthyCount()
	}

	// Get uptime
	var uptime time.Duration
	if !dc.healthManager.StartTime().IsZero() {
		uptime = time.Since(dc.healthManager.StartTime())
	}

	return &OverviewData{
		Timestamp:       time.Now(),
		OverallHealth:   string(healthStatus),
		TotalServices:   len(services),
		HealthyServices: healthyCount,
		TotalMetrics:    len(metrics),
		Uptime:          uptime,
		Version:         dc.healthManager.Version(),
		Environment:     dc.healthManager.Environment(),
		Summary: map[string]interface{}{
			"health_status": healthStatus,
			"services":      services,
			"metrics_count": len(metrics),
		},
	}
}

// CollectHealth collects health check data
func (dc *DataCollector) CollectHealth(ctx context.Context) *HealthData {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	healthReport := dc.healthManager.GetLastReport()
	if healthReport == nil {
		return &HealthData{
			OverallStatus: "unknown",
			Services:      make(map[string]ServiceHealth),
			CheckedAt:     time.Now(),
			Summary: HealthSummary{
				Unknown: 1,
				Total:   1,
			},
		}
	}

	// Convert health results to ServiceHealth
	services := make(map[string]ServiceHealth)
	summary := HealthSummary{
		Total: len(healthReport.Services),
	}

	for name, result := range healthReport.Services {
		status := string(result.Status)
		services[name] = ServiceHealth{
			Name:      name,
			Status:    status,
			Message:   result.Message,
			Duration:  result.Duration,
			Critical:  result.Critical,
			Timestamp: result.Timestamp,
		}

		// Update summary counts
		switch result.Status {
		case shared.HealthStatusHealthy:
			summary.Healthy++
		case shared.HealthStatusDegraded:
			summary.Degraded++
		case shared.HealthStatusUnhealthy:
			summary.Unhealthy++
		default:
			summary.Unknown++
		}
	}

	return &HealthData{
		OverallStatus: string(healthReport.Overall),
		Services:      services,
		CheckedAt:     healthReport.Timestamp,
		Duration:      healthReport.Duration,
		Summary:       summary,
	}
}

// CollectMetrics collects current metrics
func (dc *DataCollector) CollectMetrics(ctx context.Context) *MetricsData {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	metrics := dc.metrics.GetMetrics()
	stats := dc.metrics.GetStats()

	// Get metrics by type using the proper API
	counters := dc.metrics.GetMetricsByType(shared.MetricTypeCounter)
	gauges := dc.metrics.GetMetricsByType(shared.MetricTypeGauge)
	histograms := dc.metrics.GetMetricsByType(shared.MetricTypeHistogram)

	// Sanitize metrics to make them JSON-serializable
	// (histograms can have map[float64]uint64 which JSON can't handle)
	sanitizedMetrics := sanitizeMetricsForJSON(metrics)

	return &MetricsData{
		Timestamp: time.Now(),
		Metrics:   sanitizedMetrics,
		Stats: MetricsStats{
			TotalMetrics: len(metrics),
			Counters:     len(counters),
			Gauges:       len(gauges),
			Histograms:   len(histograms),
			LastUpdate:   stats.LastCollectionTime,
		},
	}
}

// CollectServices collects service information with health status
func (dc *DataCollector) CollectServices(ctx context.Context) []ServiceInfo {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	serviceNames := dc.container.Services()
	services := make([]ServiceInfo, 0, len(serviceNames))

	// Get health report to lookup service health
	healthReport := dc.healthManager.GetLastReport()
	healthMap := make(map[string]string)

	if healthReport != nil {
		for name, result := range healthReport.Services {
			healthMap[name] = string(result.Status)
		}
	}

	for _, name := range serviceNames {
		status := "unknown"
		if healthStatus, ok := healthMap[name]; ok {
			status = healthStatus
		}

		services = append(services, ServiceInfo{
			Name:   name,
			Type:   "service",
			Status: status,
		})
	}

	return services
}

// collect performs periodic data collection and stores in history
func (dc *DataCollector) collect(ctx context.Context) {
	// Collect overview
	overview := dc.CollectOverview(ctx)
	if dc.history != nil {
		dc.history.AddOverview(OverviewSnapshot{
			Timestamp:    overview.Timestamp,
			HealthStatus: overview.OverallHealth,
			ServiceCount: overview.TotalServices,
			HealthyCount: overview.HealthyServices,
			MetricsCount: overview.TotalMetrics,
		})
	}

	// Collect health
	health := dc.CollectHealth(ctx)
	if dc.history != nil {
		dc.history.AddHealth(HealthSnapshot{
			Timestamp:      health.CheckedAt,
			OverallStatus:  health.OverallStatus,
			HealthyCount:   health.Summary.Healthy,
			DegradedCount:  health.Summary.Degraded,
			UnhealthyCount: health.Summary.Unhealthy,
		})
	}

	// Collect metrics
	metricsData := dc.CollectMetrics(ctx)
	if dc.history != nil {
		dc.history.AddMetrics(MetricsSnapshot{
			Timestamp:    metricsData.Timestamp,
			TotalMetrics: metricsData.Stats.TotalMetrics,
			Values:       metricsData.Metrics,
		})
	}
}

// CollectServiceDetail collects detailed information about a specific service
func (dc *DataCollector) CollectServiceDetail(ctx context.Context, serviceName string) *ServiceDetail {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	// Get health information
	healthReport := dc.healthManager.GetLastReport()
	var serviceHealth *ServiceHealth
	var lastHealthCheck time.Time

	if healthReport != nil {
		if result, ok := healthReport.Services[serviceName]; ok {
			serviceHealth = &ServiceHealth{
				Name:      serviceName,
				Status:    string(result.Status),
				Message:   result.Message,
				Duration:  result.Duration,
				Critical:  result.Critical,
				Timestamp: result.Timestamp,
			}
			lastHealthCheck = result.Timestamp
		}
	}

	// Get service-specific metrics
	allMetrics := dc.metrics.GetMetrics()
	serviceMetrics := make(map[string]interface{})

	// Filter metrics by service name prefix
	for key, value := range allMetrics {
		// Check if metric name contains service name
		if contains(key, serviceName) {
			serviceMetrics[key] = value
		}
	}

	status := "unknown"
	if serviceHealth != nil {
		status = serviceHealth.Status
	}

	return &ServiceDetail{
		Name:            serviceName,
		Type:            "service",
		Status:          status,
		Health:          serviceHealth,
		Metrics:         serviceMetrics,
		Dependencies:    []string{}, // Could be populated from service metadata
		LastHealthCheck: lastHealthCheck,
	}
}

// CollectMetricsReport collects comprehensive metrics report
func (dc *DataCollector) CollectMetricsReport(ctx context.Context) *MetricsReport {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	stats := dc.metrics.GetStats()
	allMetrics := dc.metrics.GetMetrics()

	// Count metrics by type (heuristic)
	metricsByType := make(map[string]int)
	metricsByType["counter"] = countMetricsByTypeFromMap(allMetrics, "_total", "_count")
	metricsByType["gauge"] = countMetricsByTypeFromMap(allMetrics, "_gauge", "_current")
	metricsByType["histogram"] = countMetricsByTypeFromMap(allMetrics, "_bucket", "_histogram")
	metricsByType["timer"] = countMetricsByTypeFromMap(allMetrics, "_duration", "_latency")

	// Get collector information
	collectors := dc.metrics.GetCollectors()
	collectorInfos := make([]CollectorInfo, 0, len(collectors))

	for _, collector := range collectors {
		collectorMetrics := collector.Collect()
		collectorInfos = append(collectorInfos, CollectorInfo{
			Name:           collector.Name(),
			Type:           "custom",
			MetricsCount:   len(collectorMetrics),
			LastCollection: stats.LastCollectionTime,
			Status:         "active",
		})
	}

	// Get top metrics (by value, limited to numeric)
	topMetrics := make([]MetricEntry, 0)
	for name, value := range allMetrics {
		var ok bool

		switch value.(type) {
		case float64, float32, int, int64, int32, uint, uint64, uint32:
			ok = true
		}

		if ok && len(topMetrics) < 20 {
			topMetrics = append(topMetrics, MetricEntry{
				Name:      name,
				Type:      inferMetricType(name),
				Value:     value,
				Timestamp: time.Now(),
			})
		}
	}

	return &MetricsReport{
		Timestamp:      time.Now(),
		TotalMetrics:   len(allMetrics),
		MetricsByType:  metricsByType,
		Collectors:     collectorInfos,
		TopMetrics:     topMetrics,
		RecentActivity: []MetricActivity{}, // Could track metric changes over time
		Stats: MetricsReportStats{
			LastCollection:   stats.LastCollectionTime,
			TotalCollections: stats.MetricsCollected,
			ErrorCount:       len(stats.Errors),
			Uptime:           stats.Uptime,
		},
	}
}

// countMetricsByType counts metrics by their type (DEPRECATED - use Metrics.GetMetricsByType directly)
// This function is kept for backward compatibility but always returns 0
// Use the proper Metrics.GetMetricsByType() API instead
func countMetricsByType(metrics map[string]interface{}, metricType string) int {
	return 0
}

// countMetricsByTypeFromMap counts metrics containing specific suffixes
func countMetricsByTypeFromMap(metrics map[string]interface{}, suffixes ...string) int {
	count := 0
	for name := range metrics {
		for _, suffix := range suffixes {
			if contains(name, suffix) {
				count++
				break
			}
		}
	}
	return count
}

// inferMetricType infers metric type from name
func inferMetricType(name string) string {
	if contains(name, "_total") || contains(name, "_count") {
		return "counter"
	}
	if contains(name, "_bucket") || contains(name, "_histogram") {
		return "histogram"
	}
	if contains(name, "_duration") || contains(name, "_latency") {
		return "timer"
	}
	return "gauge"
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(s) > len(substr) &&
				(s[0:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// sanitizeMetricsForJSON converts metrics to JSON-serializable format
// This handles histograms with map[float64]uint64 buckets which JSON can't serialize
func sanitizeMetricsForJSON(metrics map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(metrics))

	for key, value := range metrics {
		result[key] = sanitizeValue(value)
	}

	return result
}

// sanitizeValue recursively sanitizes a value for JSON serialization
func sanitizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[float64]uint64:
		// Convert map[float64]uint64 to map[string]uint64 for JSON
		stringMap := make(map[string]uint64, len(v))
		for k, val := range v {
			stringMap[fmt.Sprintf("%.6f", k)] = val
		}
		return stringMap

	case map[string]interface{}:
		// Recursively sanitize nested maps
		sanitized := make(map[string]interface{}, len(v))
		for k, val := range v {
			sanitized[k] = sanitizeValue(val)
		}
		return sanitized

	case map[interface{}]interface{}:
		// Convert generic maps to string-keyed maps
		sanitized := make(map[string]interface{}, len(v))
		for k, val := range v {
			sanitized[fmt.Sprintf("%v", k)] = sanitizeValue(val)
		}
		return sanitized

	case []interface{}:
		// Recursively sanitize slices
		sanitized := make([]interface{}, len(v))
		for i, val := range v {
			sanitized[i] = sanitizeValue(val)
		}
		return sanitized

	default:
		// Return as-is for primitives and other types
		return value
	}
}

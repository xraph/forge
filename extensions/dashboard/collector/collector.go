package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/shared"
)

// DataCollector collects dashboard data periodically.
type DataCollector struct {
	healthManager forge.HealthManager
	metrics       forge.Metrics
	container     shared.Container
	logger        forge.Logger
	history       *DataHistory

	stopCh chan struct{}
	mu     sync.RWMutex
}

// NewDataCollector creates a new data collector.
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

// Start starts periodic data collection.
func (dc *DataCollector) Start(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Wait briefly before first collection so the health manager has time to
	// complete auto-discovery and its first check cycle. Without this delay,
	// the initial page load often shows empty or partial health data.
	go func() {
		select {
		case <-time.After(2 * time.Second):
			dc.collect(ctx)
		case <-ctx.Done():
			return
		case <-dc.stopCh:
			return
		}
	}()

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

// Stop stops data collection.
func (dc *DataCollector) Stop() {
	close(dc.stopCh)
}

// CollectOverview collects overview data.
func (dc *DataCollector) CollectOverview(ctx context.Context) *OverviewData {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	healthStatus := dc.healthManager.Status()
	healthReport := dc.healthManager.LastReport()
	metrics := dc.metrics.ListMetrics()
	services := dc.container.Services()

	healthyCount := 0
	if healthReport != nil {
		healthyCount = healthReport.HealthyCount()
	}

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
		Summary: map[string]any{
			"health_status": healthStatus,
			"services":      services,
			"metrics_count": len(metrics),
		},
	}
}

// CollectHealth collects health check data.
// It merges health report data with ALL container services so that services
// without registered health checks still appear (as "unknown" status).
func (dc *DataCollector) CollectHealth(ctx context.Context) *HealthData {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	healthReport := dc.healthManager.LastReport()

	services := make(map[string]ServiceHealth)
	summary := HealthSummary{}

	overallStatus := "unknown"
	checkedAt := time.Now()

	var duration time.Duration

	// Process health report results (if available)
	if healthReport != nil {
		overallStatus = string(healthReport.Overall)
		checkedAt = healthReport.Timestamp
		duration = healthReport.Duration

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
	}

	// Merge ALL container services — add missing ones as "unknown"
	// so every DI-registered service appears on the health page.
	for _, name := range dc.container.Services() {
		if _, exists := services[name]; !exists {
			services[name] = ServiceHealth{
				Name:    name,
				Status:  "unknown",
				Message: "Health check pending",
			}
			summary.Unknown++
		}
	}

	summary.Total = len(services)

	return &HealthData{
		OverallStatus: overallStatus,
		Services:      services,
		CheckedAt:     checkedAt,
		Duration:      duration,
		Summary:       summary,
	}
}

// CollectMetrics collects current metrics.
func (dc *DataCollector) CollectMetrics(ctx context.Context) *MetricsData {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	metrics := dc.metrics.ListMetrics()
	stats := dc.metrics.Stats()

	counters := dc.metrics.ListMetricsByType(shared.MetricTypeCounter)
	gauges := dc.metrics.ListMetricsByType(shared.MetricTypeGauge)
	histograms := dc.metrics.ListMetricsByType(shared.MetricTypeHistogram)

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

// CollectServices collects service information with health status.
func (dc *DataCollector) CollectServices(ctx context.Context) []ServiceInfo {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	serviceNames := dc.container.Services()
	services := make([]ServiceInfo, 0, len(serviceNames))

	healthReport := dc.healthManager.LastReport()
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

		// Use DI container metadata for service type
		svcType := "service"
		info := dc.container.Inspect(name)
		if info.Lifecycle != "" {
			svcType = info.Lifecycle
		}

		services = append(services, ServiceInfo{
			Name:   name,
			Type:   svcType,
			Status: status,
		})
	}

	return services
}

// collect performs periodic data collection and stores in history.
func (dc *DataCollector) collect(ctx context.Context) {
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

	metricsData := dc.CollectMetrics(ctx)
	if dc.history != nil {
		dc.history.AddMetrics(MetricsSnapshot{
			Timestamp:    metricsData.Timestamp,
			TotalMetrics: metricsData.Stats.TotalMetrics,
			Values:       metricsData.Metrics,
		})
	}
}

// CollectServiceDetail collects detailed information about a specific service.
func (dc *DataCollector) CollectServiceDetail(ctx context.Context, serviceName string) *ServiceDetail {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	healthReport := dc.healthManager.LastReport()

	var (
		serviceHealth   *ServiceHealth
		lastHealthCheck time.Time
	)

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

	allMetrics := dc.metrics.ListMetrics()
	serviceMetrics := make(map[string]any)

	for key, value := range allMetrics {
		if contains(key, serviceName) {
			serviceMetrics[key] = value
		}
	}

	status := "unknown"
	if serviceHealth != nil {
		status = serviceHealth.Status
	}

	// Use DI container metadata for service type and dependencies
	svcType := "service"
	var deps []string

	info := dc.container.Inspect(serviceName)
	if info.Lifecycle != "" {
		svcType = info.Lifecycle
	}

	if len(info.Dependencies) > 0 {
		deps = info.Dependencies
	} else {
		deps = []string{}
	}

	return &ServiceDetail{
		Name:            serviceName,
		Type:            svcType,
		Status:          status,
		Health:          serviceHealth,
		Metrics:         serviceMetrics,
		Dependencies:    deps,
		LastHealthCheck: lastHealthCheck,
	}
}

// CollectMetricsReport collects comprehensive metrics report.
func (dc *DataCollector) CollectMetricsReport(ctx context.Context) *MetricsReport {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	stats := dc.metrics.Stats()
	allMetrics := dc.metrics.ListMetrics()

	metricsByType := make(map[string]int)
	metricsByType["counter"] = countMetricsByTypeFromMap(allMetrics, "_total", "_count")
	metricsByType["gauge"] = countMetricsByTypeFromMap(allMetrics, "_gauge", "_current")
	metricsByType["histogram"] = countMetricsByTypeFromMap(allMetrics, "_bucket", "_histogram")
	metricsByType["timer"] = countMetricsByTypeFromMap(allMetrics, "_duration", "_latency")

	collectors := dc.metrics.ListCollectors()
	collectorInfos := make([]CollectorInfo, 0, len(collectors))

	for _, c := range collectors {
		collectorMetrics := c.Collect()
		collectorInfos = append(collectorInfos, CollectorInfo{
			Name:           c.Name(),
			Type:           "custom",
			MetricsCount:   len(collectorMetrics),
			LastCollection: stats.LastCollectionTime,
			Status:         "active",
		})
	}

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
		RecentActivity: []MetricActivity{},
		Stats: MetricsReportStats{
			LastCollection:   stats.LastCollectionTime,
			TotalCollections: stats.MetricsCollected,
			ErrorCount:       len(stats.Errors),
			Uptime:           stats.Uptime,
		},
	}
}

// countMetricsByTypeFromMap counts metrics containing specific suffixes.
func countMetricsByTypeFromMap(metrics map[string]any, suffixes ...string) int {
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

// inferMetricType infers metric type from name.
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

// contains checks if a string contains a substring.
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

// sanitizeMetricsForJSON converts metrics to JSON-serializable format.
func sanitizeMetricsForJSON(metrics map[string]any) map[string]any {
	result := make(map[string]any, len(metrics))

	for key, value := range metrics {
		result[key] = sanitizeValue(value)
	}

	return result
}

// sanitizeValue recursively sanitizes a value for JSON serialization.
func sanitizeValue(value any) any {
	switch v := value.(type) {
	case map[float64]uint64:
		stringMap := make(map[string]uint64, len(v))
		for k, val := range v {
			stringMap[fmt.Sprintf("%.6f", k)] = val
		}

		return stringMap

	case map[string]any:
		sanitized := make(map[string]any, len(v))
		for k, val := range v {
			sanitized[k] = sanitizeValue(val)
		}

		return sanitized

	case map[any]any:
		sanitized := make(map[string]any, len(v))
		for k, val := range v {
			sanitized[fmt.Sprintf("%v", k)] = sanitizeValue(val)
		}

		return sanitized

	case []any:
		sanitized := make([]any, len(v))
		for i, val := range v {
			sanitized[i] = sanitizeValue(val)
		}

		return sanitized

	default:
		return value
	}
}

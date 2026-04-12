package collector

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xraph/forge"
	internalmetrics "github.com/xraph/forge/internal/metrics"
	"github.com/xraph/forge/internal/shared"
)

// OnCollectFunc is called after each data collection cycle with the collected data.
type OnCollectFunc func(overview *OverviewData, health *HealthData, metrics *MetricsData)

// DataCollector collects dashboard data periodically.
type DataCollector struct {
	healthManager forge.HealthManager
	metrics       forge.Metrics
	container     shared.Container
	logger        forge.Logger
	history       *DataHistory

	stopCh    chan struct{}
	stopOnce  sync.Once
	mu        sync.RWMutex
	onCollect OnCollectFunc

	// Cached results from periodic collection to avoid redundant allocations.
	cachedOverview *OverviewData
	cachedHealth   *HealthData
	cachedMetrics  *MetricsData
	cachedAt       time.Time
	cacheTTL       time.Duration
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
		cacheTTL:      30 * time.Second,
	}
}

// SetCacheTTL sets the cache TTL for collected data. Should match RefreshInterval.
func (dc *DataCollector) SetCacheTTL(ttl time.Duration) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.cacheTTL = ttl
}

// SetOnCollect registers a callback invoked after each data collection cycle.
func (dc *DataCollector) SetOnCollect(fn OnCollectFunc) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	dc.onCollect = fn
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

// Stop stops data collection. Safe to call multiple times.
func (dc *DataCollector) Stop() {
	dc.stopOnce.Do(func() {
		close(dc.stopCh)
	})
}

// CollectOverview returns overview data, using a short-lived cache to
// avoid redundant allocations across page renders, API calls, and bridge requests.
func (dc *DataCollector) CollectOverview(ctx context.Context) *OverviewData {
	dc.mu.RLock()
	if dc.cachedOverview != nil && time.Since(dc.cachedAt) < dc.cacheTTL {
		cached := dc.cachedOverview
		dc.mu.RUnlock()
		return cached
	}
	dc.mu.RUnlock()

	return dc.collectOverviewFresh()
}

// CollectHealth returns health check data, using a short-lived cache.
// It merges health report data with ALL container services so that services
// without registered health checks still appear (as "unknown" status).
func (dc *DataCollector) CollectHealth(ctx context.Context) *HealthData {
	dc.mu.RLock()
	if dc.cachedHealth != nil && time.Since(dc.cachedAt) < dc.cacheTTL {
		cached := dc.cachedHealth
		dc.mu.RUnlock()
		return cached
	}
	dc.mu.RUnlock()

	return dc.collectHealthFresh()
}

// CollectMetrics returns current metrics, using a short-lived cache.
func (dc *DataCollector) CollectMetrics(ctx context.Context) *MetricsData {
	dc.mu.RLock()
	if dc.cachedMetrics != nil && time.Since(dc.cachedAt) < dc.cacheTTL {
		cached := dc.cachedMetrics
		dc.mu.RUnlock()
		return cached
	}
	dc.mu.RUnlock()

	return dc.collectMetricsFresh()
}

// collectOverviewFresh performs the actual overview data collection.
func (dc *DataCollector) collectOverviewFresh() *OverviewData {
	healthStatus := dc.healthManager.Status()
	healthReport := dc.healthManager.LastReport()
	services := dc.container.Services()

	// Get metrics count without allocating a full map.
	var metricsCount int
	if qp, ok := dc.metrics.(internalmetrics.MetricQueryProvider); ok {
		metricsCount = len(qp.MetricNames())
	} else {
		metricsCount = len(dc.metrics.ListMetrics())
	}

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
		TotalMetrics:    metricsCount,
		Uptime:          uptime,
		Version:         dc.healthManager.Version(),
		Environment:     dc.healthManager.Environment(),
		Summary: map[string]any{
			"health_status": healthStatus,
			"services":      services,
			"metrics_count": metricsCount,
		},
	}
}

// collectHealthFresh performs the actual health data collection.
func (dc *DataCollector) collectHealthFresh() *HealthData {
	healthReport := dc.healthManager.LastReport()

	services := make(map[string]ServiceHealth)
	summary := HealthSummary{}

	overallStatus := "unknown"
	checkedAt := time.Now()

	var duration time.Duration

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

	for _, name := range dc.container.Services() {
		if _, exists := services[name]; !exists {
			services[name] = ServiceHealth{
				Name:    name,
				Status:  "healthy",
				Message: "Service registered",
			}
			summary.Healthy++
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

// collectMetricsFresh performs the actual metrics data collection.
func (dc *DataCollector) collectMetricsFresh() *MetricsData {
	stats := dc.metrics.Stats()

	// Prefer MetricQueryProvider for zero-allocation access.
	if qp, ok := dc.metrics.(internalmetrics.MetricQueryProvider); ok {
		names := qp.MetricNames()
		typeCounts := qp.MetricCountsByType()

		metricsMap := make(map[string]any, len(names))
		for _, name := range names {
			if v, ok := qp.MetricValue(name); ok {
				metricsMap[name] = v
			}
		}

		return &MetricsData{
			Timestamp: time.Now(),
			Metrics:   metricsMap,
			Stats: MetricsStats{
				TotalMetrics: len(names),
				Counters:     typeCounts["counter"],
				Gauges:       typeCounts["gauge"],
				Histograms:   typeCounts["histogram"],
				LastUpdate:   stats.LastCollectionTime,
			},
		}
	}

	// Fallback for non-forge metrics providers.
	metrics := dc.metrics.ListMetrics()

	var counters, gauges, histograms int
	for name := range metrics {
		switch inferMetricType(name) {
		case "counter":
			counters++
		case "histogram":
			histograms++
		default:
			gauges++
		}
	}

	return &MetricsData{
		Timestamp: time.Now(),
		Metrics:   sanitizeMetricsForJSON(metrics),
		Stats: MetricsStats{
			TotalMetrics: len(metrics),
			Counters:     counters,
			Gauges:       gauges,
			Histograms:   histograms,
			LastUpdate:   stats.LastCollectionTime,
		},
	}
}

// CollectServices collects service information with health status.
func (dc *DataCollector) CollectServices(_ context.Context) []ServiceInfo {
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

// collect performs periodic data collection, stores in history, and updates the cache.
func (dc *DataCollector) collect(ctx context.Context) {
	overview := dc.collectOverviewFresh()
	health := dc.collectHealthFresh()
	metricsData := dc.collectMetricsFresh()

	// Update cache so subsequent requests within this interval reuse results.
	dc.mu.Lock()
	dc.cachedOverview = overview
	dc.cachedHealth = health
	dc.cachedMetrics = metricsData
	dc.cachedAt = time.Now()
	callback := dc.onCollect
	dc.mu.Unlock()

	if dc.history != nil {
		dc.history.AddOverview(OverviewSnapshot{
			Timestamp:    overview.Timestamp,
			HealthStatus: overview.OverallHealth,
			ServiceCount: overview.TotalServices,
			HealthyCount: overview.HealthyServices,
			MetricsCount: overview.TotalMetrics,
		})

		dc.history.AddHealth(HealthSnapshot{
			Timestamp:      health.CheckedAt,
			OverallStatus:  health.OverallStatus,
			HealthyCount:   health.Summary.Healthy,
			DegradedCount:  health.Summary.Degraded,
			UnhealthyCount: health.Summary.Unhealthy,
		})
	}

	// Notify SSE subscribers of new data without blocking the collection cycle.
	if callback != nil {
		go callback(overview, health, metricsData)
	}
}

// CollectServiceDetail collects detailed information about a specific service.
func (dc *DataCollector) CollectServiceDetail(_ context.Context, serviceName string) *ServiceDetail {
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

	serviceMetrics := make(map[string]any)

	if qp, ok := dc.metrics.(internalmetrics.MetricQueryProvider); ok {
		for _, name := range qp.MetricNames() {
			if contains(name, serviceName) {
				if v, ok := qp.MetricValue(name); ok {
					serviceMetrics[name] = v
				}
			}
		}
	} else {
		allMetrics := dc.metrics.ListMetrics()
		for key, value := range allMetrics {
			if contains(key, serviceName) {
				serviceMetrics[key] = value
			}
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
func (dc *DataCollector) CollectMetricsReport(_ context.Context) *MetricsReport {
	stats := dc.metrics.Stats()

	var metricsByType map[string]int
	var topMetrics []MetricEntry
	var totalMetrics int

	if qp, ok := dc.metrics.(internalmetrics.MetricQueryProvider); ok {
		metricsByType = qp.MetricCountsByType()
		names := qp.MetricNames()
		totalMetrics = len(names)

		topMetrics = make([]MetricEntry, 0, len(names))
		now := time.Now()
		for _, name := range names {
			var val any
			if v, ok := qp.MetricValue(name); ok {
				val = v
			}
			topMetrics = append(topMetrics, MetricEntry{
				Name:      name,
				Type:      inferMetricType(name),
				Value:     val,
				Timestamp: now,
			})
		}
	} else {
		allMetrics := dc.metrics.ListMetrics()
		totalMetrics = len(allMetrics)

		metricsByType = make(map[string]int)
		metricsByType["counter"] = countMetricsByTypeFromMap(allMetrics, "_total", "_count")
		metricsByType["gauge"] = countMetricsByTypeFromMap(allMetrics, "_gauge", "_current")
		metricsByType["histogram"] = countMetricsByTypeFromMap(allMetrics, "_bucket", "_histogram")
		metricsByType["timer"] = countMetricsByTypeFromMap(allMetrics, "_duration", "_latency")

		topMetrics = make([]MetricEntry, 0, len(allMetrics))
		now := time.Now()
		for name, value := range allMetrics {
			topMetrics = append(topMetrics, MetricEntry{
				Name:      name,
				Type:      inferMetricType(name),
				Value:     value,
				Timestamp: now,
			})
		}
	}

	// Sort by name for consistent display
	sort.Slice(topMetrics, func(i, j int) bool {
		return topMetrics[i].Name < topMetrics[j].Name
	})

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

	return &MetricsReport{
		Timestamp:      time.Now(),
		TotalMetrics:   totalMetrics,
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

// CollectCollectorDetail collects detailed information about a specific collector.
func (dc *DataCollector) CollectCollectorDetail(_ context.Context, collectorName string) *CollectorDetail {
	stats := dc.metrics.Stats()
	collectors := dc.metrics.ListCollectors()

	for _, c := range collectors {
		if c.Name() != collectorName {
			continue
		}

		collectorMetrics := sanitizeMetricsForJSON(c.Collect())

		return &CollectorDetail{
			Name:           c.Name(),
			Type:           "custom",
			Status:         "active",
			MetricsCount:   len(collectorMetrics),
			LastCollection: stats.LastCollectionTime,
			Metrics:        collectorMetrics,
			Metadata:       make(map[string]string),
		}
	}

	return nil
}

// CollectMetricDetail collects detailed information about a specific metric.
func (dc *DataCollector) CollectMetricDetail(_ context.Context, metricName string) *MetricDetail {
	// Prefer MetricQueryProvider for zero-allocation access.
	var value any
	if qp, ok := dc.metrics.(internalmetrics.MetricQueryProvider); ok {
		v, exists := qp.MetricValue(metricName)
		if !exists {
			return nil
		}
		value = v
	} else {
		allMetrics := dc.metrics.ListMetrics()
		v, exists := allMetrics[metricName]
		if !exists {
			return nil
		}
		value = v
	}

	detail := &MetricDetail{
		Name:  metricName,
		Type:  inferMetricType(metricName),
		Value: sanitizeValue(value),
	}

	// Try to get the typed metric object for rich introspection
	provider, ok := dc.metrics.(internalmetrics.MetricDetailProvider)
	if !ok {
		return detail
	}

	rawMetric := provider.GetMetric(metricName)
	if rawMetric == nil {
		return detail
	}

	// Extract metadata and type-specific details
	switch m := rawMetric.(type) {
	case shared.Counter:
		md := m.Describe()
		detail.Type = "counter"
		detail.Description = md.Description
		detail.Unit = md.Unit
		detail.Namespace = md.Namespace
		detail.Subsystem = md.Subsystem
		detail.Labels = md.Labels
		detail.ConstLabels = md.ConstLabels
		detail.Created = md.Created
		detail.Updated = md.Updated

		exemplars := m.Exemplars()
		exemplarInfos := make([]ExemplarInfo, 0, len(exemplars))
		for _, e := range exemplars {
			exemplarInfos = append(exemplarInfos, ExemplarInfo{
				Value:     e.Value,
				Timestamp: e.Timestamp,
				TraceID:   e.TraceID,
				SpanID:    e.SpanID,
				Labels:    e.Labels,
			})
		}

		detail.CounterData = &CounterDetail{
			Value:     m.Value(),
			Exemplars: exemplarInfos,
		}

	case shared.Gauge:
		md := m.Describe()
		detail.Type = "gauge"
		detail.Description = md.Description
		detail.Unit = md.Unit
		detail.Namespace = md.Namespace
		detail.Subsystem = md.Subsystem
		detail.Labels = md.Labels
		detail.ConstLabels = md.ConstLabels
		detail.Created = md.Created
		detail.Updated = md.Updated

		detail.GaugeData = &GaugeDetail{
			Value: m.Value(),
		}

	case shared.Histogram:
		md := m.Describe()
		detail.Type = "histogram"
		detail.Description = md.Description
		detail.Unit = md.Unit
		detail.Namespace = md.Namespace
		detail.Subsystem = md.Subsystem
		detail.Labels = md.Labels
		detail.ConstLabels = md.ConstLabels
		detail.Created = md.Created
		detail.Updated = md.Updated

		// Convert bucket keys to strings for JSON compatibility
		rawBuckets := m.Buckets()
		buckets := make(map[string]uint64, len(rawBuckets))
		for bound, count := range rawBuckets {
			buckets[fmt.Sprintf("%.6f", bound)] = count
		}

		exemplars := m.Exemplars()
		exemplarInfos := make([]ExemplarInfo, 0, len(exemplars))
		for _, e := range exemplars {
			exemplarInfos = append(exemplarInfos, ExemplarInfo{
				Value:     e.Value,
				Timestamp: e.Timestamp,
				TraceID:   e.TraceID,
				SpanID:    e.SpanID,
				Labels:    e.Labels,
			})
		}

		detail.HistogramData = &HistogramDetail{
			Count:     m.Count(),
			Sum:       m.Sum(),
			Mean:      m.Mean(),
			StdDev:    m.StdDev(),
			Min:       m.Min(),
			Max:       m.Max(),
			P50:       m.Percentile(0.5),
			P90:       m.Percentile(0.9),
			P95:       m.Percentile(0.95),
			P99:       m.Percentile(0.99),
			Buckets:   buckets,
			Exemplars: exemplarInfos,
		}

	case shared.Timer:
		md := m.Describe()
		detail.Type = "timer"
		detail.Description = md.Description
		detail.Unit = md.Unit
		detail.Namespace = md.Namespace
		detail.Subsystem = md.Subsystem
		detail.Labels = md.Labels
		detail.ConstLabels = md.ConstLabels
		detail.Created = md.Created
		detail.Updated = md.Updated

		exemplars := m.Exemplars()
		exemplarInfos := make([]ExemplarInfo, 0, len(exemplars))
		for _, e := range exemplars {
			exemplarInfos = append(exemplarInfos, ExemplarInfo{
				Value:     e.Value,
				Timestamp: e.Timestamp,
				TraceID:   e.TraceID,
				SpanID:    e.SpanID,
				Labels:    e.Labels,
			})
		}

		detail.TimerData = &TimerDetail{
			Count:     m.Count(),
			Sum:       m.Sum(),
			Mean:      m.Mean(),
			StdDev:    m.StdDev(),
			Min:       m.Min(),
			Max:       m.Max(),
			P50:       m.Percentile(0.5),
			P95:       m.Percentile(0.95),
			P99:       m.Percentile(0.99),
			Exemplars: exemplarInfos,
		}
	}

	return detail
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

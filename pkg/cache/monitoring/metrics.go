package monitoring

import (
	"context"
	"fmt"
	"sync"
	"time"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// CacheMetricsCollector provides comprehensive metrics collection for cache systems
type CacheMetricsCollector struct {
	cacheManager    cachecore.CacheManager
	metricsRegistry common.Metrics
	logger          common.Logger
	config          *CacheMetricsConfig
	collectors      map[string]*CacheMetricsInfo
	aggregatedStats *AggregatedCacheStats
	mu              sync.RWMutex
	started         bool
	stopChan        chan struct{}
}

// CacheMetricsConfig contains configuration for cache metrics collection
type CacheMetricsConfig struct {
	CollectionInterval    time.Duration          `yaml:"collection_interval" json:"collection_interval" default:"30s"`
	AggregationInterval   time.Duration          `yaml:"aggregation_interval" json:"aggregation_interval" default:"5m"`
	RetentionPeriod       time.Duration          `yaml:"retention_period" json:"retention_period" default:"24h"`
	EnableDetailedMetrics bool                   `yaml:"enable_detailed_metrics" json:"enable_detailed_metrics" default:"true"`
	EnableLatencyMetrics  bool                   `yaml:"enable_latency_metrics" json:"enable_latency_metrics" default:"true"`
	EnableSizeMetrics     bool                   `yaml:"enable_size_metrics" json:"enable_size_metrics" default:"true"`
	EnableThroughput      bool                   `yaml:"enable_throughput" json:"enable_throughput" default:"true"`
	EnablePercentiles     bool                   `yaml:"enable_percentiles" json:"enable_percentiles" default:"true"`
	PercentileTargets     []float64              `yaml:"percentile_targets" json:"percentile_targets"`
	CustomMetrics         map[string]interface{} `yaml:"custom_metrics" json:"custom_metrics"`
	Tags                  map[string]string      `yaml:"tags" json:"tags"`
	MetricNamePrefix      string                 `yaml:"metric_name_prefix" json:"metric_name_prefix" default:"forge.cache"`
	EnableHistograms      bool                   `yaml:"enable_histograms" json:"enable_histograms" default:"true"`
	HistogramBuckets      []float64              `yaml:"histogram_buckets" json:"histogram_buckets"`
}

// CacheMetricsInfo contains metrics information for a cache
type CacheMetricsInfo struct {
	CacheName       string                        `json:"cache_name"`
	CacheType       string                        `json:"cache_type"`
	Metrics         *cachecore.CacheStats         `json:"metrics"`
	DetailedMetrics *DetailedCacheMetrics         `json:"detailed_metrics"`
	HistoricalData  []*CacheMetricsSnapshot       `json:"historical_data"`
	Aggregations    map[string]*AggregatedMetrics `json:"aggregations"`
	LastCollected   time.Time                     `json:"last_collected"`
	CollectionCount int64                         `json:"collection_count"`
	mu              sync.RWMutex                  `json:"-"`
}

// DetailedCacheMetrics contains detailed cache metrics
type DetailedCacheMetrics struct {
	// Operation counts
	GetOperations    int64 `json:"get_operations"`
	SetOperations    int64 `json:"set_operations"`
	DeleteOperations int64 `json:"delete_operations"`
	FlushOperations  int64 `json:"flush_operations"`

	// Latency metrics
	GetLatency    LatencyMetrics `json:"get_latency"`
	SetLatency    LatencyMetrics `json:"set_latency"`
	DeleteLatency LatencyMetrics `json:"delete_latency"`

	// Size metrics
	AverageKeySize   float64 `json:"average_key_size"`
	AverageValueSize float64 `json:"average_value_size"`
	TotalKeySize     int64   `json:"total_key_size"`
	TotalValueSize   int64   `json:"total_value_size"`

	// Throughput metrics
	OperationsPerSecond float64 `json:"operations_per_second"`
	BytesPerSecond      float64 `json:"bytes_per_second"`
	HitsPerSecond       float64 `json:"hits_per_second"`
	MissesPerSecond     float64 `json:"misses_per_second"`

	// Error metrics
	GetErrors     int64 `json:"get_errors"`
	SetErrors     int64 `json:"set_errors"`
	DeleteErrors  int64 `json:"delete_errors"`
	TimeoutErrors int64 `json:"timeout_errors"`

	// Cache-specific metrics
	EvictionCount     int64   `json:"eviction_count"`
	ExpirationCount   int64   `json:"expiration_count"`
	MemoryUtilization float64 `json:"memory_utilization"`
	ItemUtilization   float64 `json:"item_utilization"`

	// Network metrics (for distributed caches)
	NetworkRequests     int64          `json:"network_requests"`
	NetworkLatency      LatencyMetrics `json:"network_latency"`
	NetworkErrors       int64          `json:"network_errors"`
	ConnectionCount     int            `json:"connection_count"`
	ConnectionPoolUsage float64        `json:"connection_pool_usage"`
}

// LatencyMetrics contains latency statistics
type LatencyMetrics struct {
	Min       time.Duration `json:"min"`
	Max       time.Duration `json:"max"`
	Mean      time.Duration `json:"mean"`
	Median    time.Duration `json:"median"`
	P95       time.Duration `json:"p95"`
	P99       time.Duration `json:"p99"`
	P999      time.Duration `json:"p999"`
	StdDev    time.Duration `json:"std_dev"`
	Count     int64         `json:"count"`
	TotalTime time.Duration `json:"total_time"`
}

// CacheMetricsSnapshot represents a point-in-time metrics snapshot
type CacheMetricsSnapshot struct {
	Timestamp       time.Time             `json:"timestamp"`
	Stats           *cachecore.CacheStats `json:"stats"`
	DetailedMetrics *DetailedCacheMetrics `json:"detailed_metrics"`
	CustomMetrics   map[string]float64    `json:"custom_metrics"`
}

// AggregatedMetrics contains aggregated metrics over time
type AggregatedMetrics struct {
	Period      time.Duration `json:"period"`
	StartTime   time.Time     `json:"start_time"`
	EndTime     time.Time     `json:"end_time"`
	SampleCount int64         `json:"sample_count"`

	// Aggregated values
	AvgHitRatio     float64       `json:"avg_hit_ratio"`
	AvgLatency      time.Duration `json:"avg_latency"`
	AvgThroughput   float64       `json:"avg_throughput"`
	TotalOperations int64         `json:"total_operations"`
	TotalErrors     int64         `json:"total_errors"`
	MaxMemoryUsage  int64         `json:"max_memory_usage"`
	MinMemoryUsage  int64         `json:"min_memory_usage"`
	AvgMemoryUsage  int64         `json:"avg_memory_usage"`

	// Trends
	HitRatioTrend    TrendDirection `json:"hit_ratio_trend"`
	LatencyTrend     TrendDirection `json:"latency_trend"`
	ThroughputTrend  TrendDirection `json:"throughput_trend"`
	MemoryUsageTrend TrendDirection `json:"memory_usage_trend"`
}

// AggregatedCacheStats contains system-wide aggregated cache statistics
type AggregatedCacheStats struct {
	TotalCaches      int                          `json:"total_caches"`
	ActiveCaches     int                          `json:"active_caches"`
	TotalOperations  int64                        `json:"total_operations"`
	TotalHits        int64                        `json:"total_hits"`
	TotalMisses      int64                        `json:"total_misses"`
	TotalErrors      int64                        `json:"total_errors"`
	SystemHitRatio   float64                      `json:"system_hit_ratio"`
	SystemErrorRate  float64                      `json:"system_error_rate"`
	TotalMemoryUsage int64                        `json:"total_memory_usage"`
	TotalItems       int64                        `json:"total_items"`
	AverageLatency   time.Duration                `json:"average_latency"`
	SystemThroughput float64                      `json:"system_throughput"`
	CacheBreakdown   map[string]*CacheMetricsInfo `json:"cache_breakdown"`
	LastUpdate       time.Time                    `json:"last_update"`
	CollectionErrors int64                        `json:"collection_errors"`
}

// TrendDirection represents the direction of a metric trend
type TrendDirection string

const (
	TrendDirectionUp       TrendDirection = "up"
	TrendDirectionDown     TrendDirection = "down"
	TrendDirectionStable   TrendDirection = "stable"
	TrendDirectionVolatile TrendDirection = "volatile"
	TrendDirectionUnknown  TrendDirection = "unknown"
)

// NewCacheMetricsCollector creates a new cache metrics collector
func NewCacheMetricsCollector(cacheManager cachecore.CacheManager, metricsRegistry common.Metrics, logger common.Logger, config *CacheMetricsConfig) *CacheMetricsCollector {
	if config == nil {
		config = &CacheMetricsConfig{
			CollectionInterval:    30 * time.Second,
			AggregationInterval:   5 * time.Minute,
			RetentionPeriod:       24 * time.Hour,
			EnableDetailedMetrics: true,
			EnableLatencyMetrics:  true,
			EnableSizeMetrics:     true,
			EnableThroughput:      true,
			EnablePercentiles:     true,
			PercentileTargets:     []float64{0.5, 0.95, 0.99, 0.999},
			MetricNamePrefix:      "forge.cache",
			EnableHistograms:      true,
			HistogramBuckets:      []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.5, 5.0, 10.0},
			Tags:                  make(map[string]string),
		}
	}

	return &CacheMetricsCollector{
		cacheManager:    cacheManager,
		metricsRegistry: metricsRegistry,
		logger:          logger,
		config:          config,
		collectors:      make(map[string]*CacheMetricsInfo),
		aggregatedStats: &AggregatedCacheStats{
			CacheBreakdown: make(map[string]*CacheMetricsInfo),
		},
		stopChan: make(chan struct{}),
	}
}

// Start starts the cache metrics collector
func (cmc *CacheMetricsCollector) Start(ctx context.Context) error {
	cmc.mu.Lock()
	defer cmc.mu.Unlock()

	if cmc.started {
		return fmt.Errorf("cache metrics collector already started")
	}

	cmc.started = true

	// Register metrics
	cmc.registerMetrics()

	// OnStart collection goroutines
	go cmc.collectionLoop(ctx)
	go cmc.aggregationLoop(ctx)

	if cmc.logger != nil {
		cmc.logger.Info("cache metrics collector started",
			logger.Duration("collection_interval", cmc.config.CollectionInterval),
			logger.Duration("aggregation_interval", cmc.config.AggregationInterval),
		)
	}

	if cmc.metricsRegistry != nil {
		cmc.metricsRegistry.Counter("forge.cache.metrics.collector_started").Inc()
	}

	return nil
}

// Stop stops the cache metrics collector
func (cmc *CacheMetricsCollector) Stop() error {
	cmc.mu.Lock()
	defer cmc.mu.Unlock()

	if !cmc.started {
		return fmt.Errorf("cache metrics collector not started")
	}

	close(cmc.stopChan)
	cmc.started = false

	if cmc.logger != nil {
		cmc.logger.Info("cache metrics collector stopped")
	}

	if cmc.metricsRegistry != nil {
		cmc.metricsRegistry.Counter("forge.cache.metrics.collector_stopped").Inc()
	}

	return nil
}

// GetMetrics returns current metrics for all caches
func (cmc *CacheMetricsCollector) GetMetrics() map[string]*CacheMetricsInfo {
	cmc.mu.RLock()
	defer cmc.mu.RUnlock()

	metrics := make(map[string]*CacheMetricsInfo)
	for name, info := range cmc.collectors {
		info.mu.RLock()
		metrics[name] = &CacheMetricsInfo{
			CacheName:       info.CacheName,
			CacheType:       info.CacheType,
			Metrics:         info.Metrics,
			DetailedMetrics: info.DetailedMetrics,
			LastCollected:   info.LastCollected,
			CollectionCount: info.CollectionCount,
		}
		info.mu.RUnlock()
	}

	return metrics
}

// GetCacheMetrics returns metrics for a specific cache
func (cmc *CacheMetricsCollector) GetCacheMetrics(cacheName string) (*CacheMetricsInfo, error) {
	cmc.mu.RLock()
	defer cmc.mu.RUnlock()

	info, exists := cmc.collectors[cacheName]
	if !exists {
		return nil, fmt.Errorf("metrics not found for cache: %s", cacheName)
	}

	info.mu.RLock()
	defer info.mu.RUnlock()

	return &CacheMetricsInfo{
		CacheName:       info.CacheName,
		CacheType:       info.CacheType,
		Metrics:         info.Metrics,
		DetailedMetrics: info.DetailedMetrics,
		LastCollected:   info.LastCollected,
		CollectionCount: info.CollectionCount,
	}, nil
}

// GetAggregatedStats returns system-wide aggregated statistics
func (cmc *CacheMetricsCollector) GetAggregatedStats() *AggregatedCacheStats {
	cmc.mu.RLock()
	defer cmc.mu.RUnlock()

	// Return a copy to prevent race conditions
	return &AggregatedCacheStats{
		TotalCaches:      cmc.aggregatedStats.TotalCaches,
		ActiveCaches:     cmc.aggregatedStats.ActiveCaches,
		TotalOperations:  cmc.aggregatedStats.TotalOperations,
		TotalHits:        cmc.aggregatedStats.TotalHits,
		TotalMisses:      cmc.aggregatedStats.TotalMisses,
		TotalErrors:      cmc.aggregatedStats.TotalErrors,
		SystemHitRatio:   cmc.aggregatedStats.SystemHitRatio,
		SystemErrorRate:  cmc.aggregatedStats.SystemErrorRate,
		TotalMemoryUsage: cmc.aggregatedStats.TotalMemoryUsage,
		TotalItems:       cmc.aggregatedStats.TotalItems,
		AverageLatency:   cmc.aggregatedStats.AverageLatency,
		SystemThroughput: cmc.aggregatedStats.SystemThroughput,
		LastUpdate:       cmc.aggregatedStats.LastUpdate,
		CollectionErrors: cmc.aggregatedStats.CollectionErrors,
	}
}

// registerMetrics registers cache metrics with the metrics registry
func (cmc *CacheMetricsCollector) registerMetrics() {
	if cmc.metricsRegistry == nil {
		return
	}

	prefix := cmc.config.MetricNamePrefix

	// Cache operation counters
	cmc.metricsRegistry.Counter(prefix + ".operations.get.total")
	cmc.metricsRegistry.Counter(prefix + ".operations.set.total")
	cmc.metricsRegistry.Counter(prefix + ".operations.delete.total")
	cmc.metricsRegistry.Counter(prefix + ".operations.hits.total")
	cmc.metricsRegistry.Counter(prefix + ".operations.misses.total")
	cmc.metricsRegistry.Counter(prefix + ".operations.errors.total")

	// Cache latency histograms
	if cmc.config.EnableLatencyMetrics && cmc.config.EnableHistograms {
		cmc.metricsRegistry.Histogram(prefix + ".latency.get")
		cmc.metricsRegistry.Histogram(prefix + ".latency.set")
		cmc.metricsRegistry.Histogram(prefix + ".latency.delete")
	}

	// Cache size gauges
	if cmc.config.EnableSizeMetrics {
		cmc.metricsRegistry.Gauge(prefix + ".size.items")
		cmc.metricsRegistry.Gauge(prefix + ".size.memory")
		cmc.metricsRegistry.Gauge(prefix + ".size.keys")
		cmc.metricsRegistry.Gauge(prefix + ".size.values")
	}

	// Cache ratios
	cmc.metricsRegistry.Gauge(prefix + ".ratio.hit")
	cmc.metricsRegistry.Gauge(prefix + ".ratio.miss")
	cmc.metricsRegistry.Gauge(prefix + ".ratio.error")

	// System-wide metrics
	cmc.metricsRegistry.Gauge(prefix + ".system.total_caches")
	cmc.metricsRegistry.Gauge(prefix + ".system.active_caches")
	cmc.metricsRegistry.Gauge(prefix + ".system.hit_ratio")
	cmc.metricsRegistry.Gauge(prefix + ".system.error_rate")
	cmc.metricsRegistry.Gauge(prefix + ".system.memory_usage")
}

// collectionLoop runs the metrics collection loop
func (cmc *CacheMetricsCollector) collectionLoop(ctx context.Context) {
	ticker := time.NewTicker(cmc.config.CollectionInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cmc.collectMetrics(ctx)

		case <-cmc.stopChan:
			return

		case <-ctx.Done():
			return
		}
	}
}

// aggregationLoop runs the metrics aggregation loop
func (cmc *CacheMetricsCollector) aggregationLoop(ctx context.Context) {
	ticker := time.NewTicker(cmc.config.AggregationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cmc.aggregateMetrics(ctx)

		case <-cmc.stopChan:
			return

		case <-ctx.Done():
			return
		}
	}
}

// collectMetrics collects metrics from all caches
func (cmc *CacheMetricsCollector) collectMetrics(ctx context.Context) {
	startTime := time.Now()

	// Get all caches
	caches := cmc.cacheManager.ListCaches()

	var collectErrors int64

	for _, cache := range caches {
		if err := cmc.collectCacheMetrics(ctx, cache); err != nil {
			collectErrors++
			if cmc.logger != nil {
				cmc.logger.Error("failed to collect cache metrics",
					logger.String("cache", cache.Name()),
					logger.Error(err),
				)
			}
		}
	}

	// Update collection metrics
	if cmc.metricsRegistry != nil {
		cmc.metricsRegistry.Counter("forge.cache.metrics.collections_total").Inc()
		cmc.metricsRegistry.Histogram("forge.cache.metrics.collection_duration").Observe(time.Since(startTime).Seconds())

		if collectErrors > 0 {
			cmc.metricsRegistry.Counter("forge.cache.metrics.collection_errors").Add(float64(collectErrors))
		}
	}

	// Update aggregated stats
	cmc.mu.Lock()
	cmc.aggregatedStats.CollectionErrors += collectErrors
	cmc.mu.Unlock()
}

// collectCacheMetrics collects metrics for a specific cache
func (cmc *CacheMetricsCollector) collectCacheMetrics(ctx context.Context, cache cachecore.Cache) error {
	startTime := time.Now()

	// Get cache stats
	stats := cache.Stats()

	// Get or create metrics info
	cacheName := cache.Name()
	cmc.mu.Lock()
	info, exists := cmc.collectors[cacheName]
	if !exists {
		info = &CacheMetricsInfo{
			CacheName:      cacheName,
			CacheType:      stats.Type,
			HistoricalData: make([]*CacheMetricsSnapshot, 0),
			Aggregations:   make(map[string]*AggregatedMetrics),
		}
		cmc.collectors[cacheName] = info
	}
	cmc.mu.Unlock()

	info.mu.Lock()
	defer info.mu.Unlock()

	// Update basic metrics
	info.Metrics = &stats
	info.LastCollected = startTime
	info.CollectionCount++

	// Collect detailed metrics if enabled
	if cmc.config.EnableDetailedMetrics {
		detailedMetrics := cmc.collectDetailedMetrics(cache, &stats)
		info.DetailedMetrics = detailedMetrics
	}

	// Create snapshot
	snapshot := &CacheMetricsSnapshot{
		Timestamp:       startTime,
		Stats:           &stats,
		DetailedMetrics: info.DetailedMetrics,
		CustomMetrics:   cmc.collectCustomMetrics(cache),
	}

	// Add to historical data
	info.HistoricalData = append(info.HistoricalData, snapshot)

	// Trim historical data if needed
	if len(info.HistoricalData) > 1000 {
		info.HistoricalData = info.HistoricalData[len(info.HistoricalData)-1000:]
	}

	// Publish metrics to registry
	cmc.publishCacheMetrics(cacheName, &stats, info.DetailedMetrics)

	return nil
}

// collectDetailedMetrics collects detailed metrics for a cache
func (cmc *CacheMetricsCollector) collectDetailedMetrics(cache cachecore.Cache, stats *cachecore.CacheStats) *DetailedCacheMetrics {
	detailed := &DetailedCacheMetrics{
		GetOperations:    stats.Hits + stats.Misses,
		SetOperations:    stats.Sets,
		DeleteOperations: stats.Deletes,

		// Calculate averages and rates
		MemoryUtilization: float64(stats.Memory) / (1024 * 1024 * 1024),   // Convert to GB
		ItemUtilization:   float64(stats.Size) / float64(stats.Size+1000), // Assume max capacity
		ConnectionCount:   stats.Connections,
	}

	// Calculate throughput if we have time-based data
	if stats.Uptime > 0 {
		detailed.OperationsPerSecond = float64(detailed.GetOperations+detailed.SetOperations+detailed.DeleteOperations) / stats.Uptime.Seconds()
		detailed.HitsPerSecond = float64(stats.Hits) / stats.Uptime.Seconds()
		detailed.MissesPerSecond = float64(stats.Misses) / stats.Uptime.Seconds()
	}

	return detailed
}

// collectCustomMetrics collects custom metrics defined in configuration
func (cmc *CacheMetricsCollector) collectCustomMetrics(cache cachecore.Cache) map[string]float64 {
	customMetrics := make(map[string]float64)

	// Add any custom metrics defined in configuration
	for name, config := range cmc.config.CustomMetrics {
		// This would be extended based on specific custom metric implementations
		if metricFunc, ok := config.(func(cachecore.Cache) float64); ok {
			customMetrics[name] = metricFunc(cache)
		}
	}

	return customMetrics
}

// publishCacheMetrics publishes cache metrics to the metrics registry
func (cmc *CacheMetricsCollector) publishCacheMetrics(cacheName string, stats *cachecore.CacheStats, detailed *DetailedCacheMetrics) {
	if cmc.metricsRegistry == nil {
		return
	}

	prefix := cmc.config.MetricNamePrefix
	tags := []string{"cache", cacheName}

	// Add custom tags
	for key, value := range cmc.config.Tags {
		tags = append(tags, key, value)
	}

	// Basic counters
	cmc.metricsRegistry.Counter(prefix+".operations.hits.total", tags...).Add(float64(stats.Hits))
	cmc.metricsRegistry.Counter(prefix+".operations.misses.total", tags...).Add(float64(stats.Misses))
	cmc.metricsRegistry.Counter(prefix+".operations.sets.total", tags...).Add(float64(stats.Sets))
	cmc.metricsRegistry.Counter(prefix+".operations.deletes.total", tags...).Add(float64(stats.Deletes))
	cmc.metricsRegistry.Counter(prefix+".operations.errors.total", tags...).Add(float64(stats.Errors))

	// Gauges
	cmc.metricsRegistry.Gauge(prefix+".size.items", tags...).Set(float64(stats.Size))
	cmc.metricsRegistry.Gauge(prefix+".size.memory", tags...).Set(float64(stats.Memory))
	cmc.metricsRegistry.Gauge(prefix+".ratio.hit", tags...).Set(stats.HitRatio)
	cmc.metricsRegistry.Gauge(prefix+".connections", tags...).Set(float64(stats.Connections))

	// Detailed metrics
	if detailed != nil && cmc.config.EnableDetailedMetrics {
		cmc.metricsRegistry.Gauge(prefix+".utilization.memory", tags...).Set(detailed.MemoryUtilization)
		cmc.metricsRegistry.Gauge(prefix+".utilization.items", tags...).Set(detailed.ItemUtilization)

		if cmc.config.EnableThroughput {
			cmc.metricsRegistry.Gauge(prefix+".throughput.operations_per_second", tags...).Set(detailed.OperationsPerSecond)
			cmc.metricsRegistry.Gauge(prefix+".throughput.hits_per_second", tags...).Set(detailed.HitsPerSecond)
			cmc.metricsRegistry.Gauge(prefix+".throughput.misses_per_second", tags...).Set(detailed.MissesPerSecond)
		}
	}
}

// aggregateMetrics aggregates metrics across all caches
func (cmc *CacheMetricsCollector) aggregateMetrics(ctx context.Context) {
	cmc.mu.Lock()
	defer cmc.mu.Unlock()

	totalCaches := len(cmc.collectors)
	activeCaches := 0
	totalHits := int64(0)
	totalMisses := int64(0)
	totalErrors := int64(0)
	totalMemory := int64(0)
	totalItems := int64(0)
	totalOperations := int64(0)

	for _, info := range cmc.collectors {
		info.mu.RLock()
		if info.Metrics != nil {
			activeCaches++
			totalHits += info.Metrics.Hits
			totalMisses += info.Metrics.Misses
			totalErrors += info.Metrics.Errors
			totalMemory += info.Metrics.Memory
			totalItems += info.Metrics.Size
			totalOperations += info.Metrics.Hits + info.Metrics.Misses + info.Metrics.Sets + info.Metrics.Deletes
		}
		info.mu.RUnlock()
	}

	// Calculate ratios
	systemHitRatio := 0.0
	if totalHits+totalMisses > 0 {
		systemHitRatio = float64(totalHits) / float64(totalHits+totalMisses)
	}

	systemErrorRate := 0.0
	if totalOperations > 0 {
		systemErrorRate = float64(totalErrors) / float64(totalOperations)
	}

	// Update aggregated stats
	cmc.aggregatedStats.TotalCaches = totalCaches
	cmc.aggregatedStats.ActiveCaches = activeCaches
	cmc.aggregatedStats.TotalHits = totalHits
	cmc.aggregatedStats.TotalMisses = totalMisses
	cmc.aggregatedStats.TotalErrors = totalErrors
	cmc.aggregatedStats.TotalMemoryUsage = totalMemory
	cmc.aggregatedStats.TotalItems = totalItems
	cmc.aggregatedStats.TotalOperations = totalOperations
	cmc.aggregatedStats.SystemHitRatio = systemHitRatio
	cmc.aggregatedStats.SystemErrorRate = systemErrorRate
	cmc.aggregatedStats.LastUpdate = time.Now()

	// Publish system-wide metrics
	if cmc.metricsRegistry != nil {
		cmc.metricsRegistry.Gauge(cmc.config.MetricNamePrefix + ".system.total_caches").Set(float64(totalCaches))
		cmc.metricsRegistry.Gauge(cmc.config.MetricNamePrefix + ".system.active_caches").Set(float64(activeCaches))
		cmc.metricsRegistry.Gauge(cmc.config.MetricNamePrefix + ".system.hit_ratio").Set(systemHitRatio)
		cmc.metricsRegistry.Gauge(cmc.config.MetricNamePrefix + ".system.error_rate").Set(systemErrorRate)
		cmc.metricsRegistry.Gauge(cmc.config.MetricNamePrefix + ".system.memory_usage").Set(float64(totalMemory))
		cmc.metricsRegistry.Gauge(cmc.config.MetricNamePrefix + ".system.total_items").Set(float64(totalItems))
	}
}

// GetHistoricalData returns historical metrics data for a cache
func (cmc *CacheMetricsCollector) GetHistoricalData(cacheName string, duration time.Duration) ([]*CacheMetricsSnapshot, error) {
	cmc.mu.RLock()
	defer cmc.mu.RUnlock()

	info, exists := cmc.collectors[cacheName]
	if !exists {
		return nil, fmt.Errorf("cache not found: %s", cacheName)
	}

	info.mu.RLock()
	defer info.mu.RUnlock()

	cutoff := time.Now().Add(-duration)
	var filtered []*CacheMetricsSnapshot

	for _, snapshot := range info.HistoricalData {
		if snapshot.Timestamp.After(cutoff) {
			filtered = append(filtered, snapshot)
		}
	}

	return filtered, nil
}

// CalculateTrend calculates trend direction for a metric
func CalculateTrend(values []float64) TrendDirection {
	if len(values) < 2 {
		return TrendDirectionUnknown
	}

	// Simple trend calculation - could be enhanced with more sophisticated algorithms
	first := values[0]
	last := values[len(values)-1]

	if len(values) < 5 {
		if last > first*1.1 {
			return TrendDirectionUp
		} else if last < first*0.9 {
			return TrendDirectionDown
		} else {
			return TrendDirectionStable
		}
	}

	// Calculate variance to detect volatility
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	variance := 0.0
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values))

	// If variance is high, it's volatile
	if variance > mean*0.5 {
		return TrendDirectionVolatile
	}

	// Otherwise, check trend
	if last > first*1.05 {
		return TrendDirectionUp
	} else if last < first*0.95 {
		return TrendDirectionDown
	} else {
		return TrendDirectionStable
	}
}

// ResetMetrics resets metrics for a specific cache
func (cmc *CacheMetricsCollector) ResetMetrics(cacheName string) error {
	cmc.mu.Lock()
	defer cmc.mu.Unlock()

	info, exists := cmc.collectors[cacheName]
	if !exists {
		return fmt.Errorf("cache not found: %s", cacheName)
	}

	info.mu.Lock()
	defer info.mu.Unlock()

	// Clear historical data
	info.HistoricalData = make([]*CacheMetricsSnapshot, 0)
	info.Aggregations = make(map[string]*AggregatedMetrics)
	info.CollectionCount = 0

	if cmc.logger != nil {
		cmc.logger.Info("cache metrics reset",
			logger.String("cache", cacheName),
		)
	}

	return nil
}

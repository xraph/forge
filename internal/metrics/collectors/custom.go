package collectors

// Custom collector methods don't return errors by design.

import (
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
	"github.com/xraph/forge/internal/shared"
)

// =============================================================================
// CUSTOM COLLECTOR
// =============================================================================

// CustomCollector provides a framework for creating custom application metrics collectors.
type CustomCollector struct {
	name               string
	interval           time.Duration
	logger             logger.Logger
	metrics            map[string]any
	enabled            bool
	mu                 sync.RWMutex
	lastCollectionTime time.Time

	// Custom metric collectors
	collectors       map[string]shared.CustomCollector
	businessMetrics  map[string]*BusinessMetric
	gaugeMetrics     map[string]*GaugeMetric
	counterMetrics   map[string]*CounterMetric
	histogramMetrics map[string]*HistogramMetric
	timerMetrics     map[string]*TimerMetric

	// Custom callbacks
	customCallbacks []CustomCallback
}

// CustomCollectorConfig contains configuration for the custom collector.
type CustomCollectorConfig struct {
	Interval                 time.Duration `json:"interval"                   yaml:"interval"`
	EnableBusinessMetrics    bool          `json:"enable_business_metrics"    yaml:"enable_business_metrics"`
	EnablePerformanceMetrics bool          `json:"enable_performance_metrics" yaml:"enable_performance_metrics"`
	EnableUserMetrics        bool          `json:"enable_user_metrics"        yaml:"enable_user_metrics"`
	EnableFeatureMetrics     bool          `json:"enable_feature_metrics"     yaml:"enable_feature_metrics"`
	MaxCustomMetrics         int           `json:"max_custom_metrics"         yaml:"max_custom_metrics"`
	CallbackTimeout          time.Duration `json:"callback_timeout"           yaml:"callback_timeout"`
}

// CustomCallback defines a custom callback function for metrics collection.
type CustomCallback func() (map[string]any, error)

// BusinessMetric represents a business-specific metric.
type BusinessMetric struct {
	Name        string            `json:"name"`
	Value       any               `json:"value"`
	Type        string            `json:"type"`
	Unit        string            `json:"unit"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Timestamp   time.Time         `json:"timestamp"`
	Source      string            `json:"source"`
}

// GaugeMetric represents a gauge metric.
type GaugeMetric struct {
	Name        string            `json:"name"`
	Value       float64           `json:"value"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Timestamp   time.Time         `json:"timestamp"`
	MinValue    float64           `json:"min_value"`
	MaxValue    float64           `json:"max_value"`
	Unit        string            `json:"unit"`
}

// CounterMetric represents a counter metric.
type CounterMetric struct {
	Name        string            `json:"name"`
	Value       int64             `json:"value"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Timestamp   time.Time         `json:"timestamp"`
	Increment   int64             `json:"increment"`
	Rate        float64           `json:"rate"`
	Unit        string            `json:"unit"`
}

// HistogramMetric represents a histogram metric.
type HistogramMetric struct {
	Name        string            `json:"name"`
	Count       int64             `json:"count"`
	Sum         float64           `json:"sum"`
	Mean        float64           `json:"mean"`
	Min         float64           `json:"min"`
	Max         float64           `json:"max"`
	Buckets     map[float64]int64 `json:"buckets"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Timestamp   time.Time         `json:"timestamp"`
	Unit        string            `json:"unit"`
}

// TimerMetric represents a timer metric.
type TimerMetric struct {
	Name        string            `json:"name"`
	Count       int64             `json:"count"`
	TotalTime   time.Duration     `json:"total_time"`
	MeanTime    time.Duration     `json:"mean_time"`
	MinTime     time.Duration     `json:"min_time"`
	MaxTime     time.Duration     `json:"max_time"`
	P50Time     time.Duration     `json:"p50_time"`
	P95Time     time.Duration     `json:"p95_time"`
	P99Time     time.Duration     `json:"p99_time"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Timestamp   time.Time         `json:"timestamp"`
	Durations   []time.Duration   `json:"-"`
}

// DefaultCustomCollectorConfig returns default configuration.
func DefaultCustomCollectorConfig() *CustomCollectorConfig {
	return &CustomCollectorConfig{
		Interval:                 time.Second * 60,
		EnableBusinessMetrics:    true,
		EnablePerformanceMetrics: true,
		EnableUserMetrics:        true,
		EnableFeatureMetrics:     true,
		MaxCustomMetrics:         1000,
		CallbackTimeout:          time.Second * 5,
	}
}

// NewCustomCollector creates a new custom collector.
func NewCustomCollector(logger logger.Logger) shared.CustomCollector {
	return NewCustomCollectorWithConfig(DefaultCustomCollectorConfig(), logger)
}

// NewCustomCollectorWithConfig creates a new custom collector with configuration.
func NewCustomCollectorWithConfig(config *CustomCollectorConfig, logger logger.Logger) shared.CustomCollector {
	return &CustomCollector{
		name:             "custom",
		interval:         config.Interval,
		logger:           logger,
		metrics:          make(map[string]any),
		enabled:          true,
		collectors:       make(map[string]shared.CustomCollector),
		businessMetrics:  make(map[string]*BusinessMetric),
		gaugeMetrics:     make(map[string]*GaugeMetric),
		counterMetrics:   make(map[string]*CounterMetric),
		histogramMetrics: make(map[string]*HistogramMetric),
		timerMetrics:     make(map[string]*TimerMetric),
		customCallbacks:  make([]CustomCallback, 0),
	}
}

// =============================================================================
// CUSTOM COLLECTOR INTERFACE IMPLEMENTATION
// =============================================================================

// Name returns the collector name.
func (cc *CustomCollector) Name() string {
	return cc.name
}

// Collect collects custom metrics.
func (cc *CustomCollector) Collect() map[string]any {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if !cc.enabled {
		return cc.metrics
	}

	now := time.Now()

	// Only collect if enough time has passed
	if !cc.lastCollectionTime.IsZero() && now.Sub(cc.lastCollectionTime) < cc.interval {
		return cc.metrics
	}

	cc.lastCollectionTime = now

	// Clear previous metrics
	cc.metrics = make(map[string]any)

	// Collect from custom collectors
	if err := cc.collectFromCustomCollectors(); err != nil && cc.logger != nil {
		cc.logger.Error("failed to collect from custom collectors",
			logger.Error(err),
		)
	}

	// Collect business metrics
	if err := cc.collectBusinessMetrics(); err != nil && cc.logger != nil {
		cc.logger.Error("failed to collect business metrics",
			logger.Error(err),
		)
	}

	// Collect gauge metrics
	if err := cc.collectGaugeMetrics(); err != nil && cc.logger != nil {
		cc.logger.Error("failed to collect gauge metrics",
			logger.Error(err),
		)
	}

	// Collect counter metrics
	if err := cc.collectCounterMetrics(); err != nil && cc.logger != nil {
		cc.logger.Error("failed to collect counter metrics",
			logger.Error(err),
		)
	}

	// Collect histogram metrics
	if err := cc.collectHistogramMetrics(); err != nil && cc.logger != nil {
		cc.logger.Error("failed to collect histogram metrics",
			logger.Error(err),
		)
	}

	// Collect timer metrics
	if err := cc.collectTimerMetrics(); err != nil && cc.logger != nil {
		cc.logger.Error("failed to collect timer metrics",
			logger.Error(err),
		)
	}

	// Execute custom callbacks
	if err := cc.executeCustomCallbacks(); err != nil && cc.logger != nil {
		cc.logger.Error("failed to execute custom callbacks",
			logger.Error(err),
		)
	}

	return cc.metrics
}

// Reset resets the collector.
func (cc *CustomCollector) Reset() error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.metrics = make(map[string]any)
	cc.businessMetrics = make(map[string]*BusinessMetric)
	cc.gaugeMetrics = make(map[string]*GaugeMetric)
	cc.counterMetrics = make(map[string]*CounterMetric)
	cc.histogramMetrics = make(map[string]*HistogramMetric)
	cc.timerMetrics = make(map[string]*TimerMetric)
	cc.lastCollectionTime = time.Time{}

	// Reset custom collectors
	for _, collector := range cc.collectors {
		err := collector.Reset()
		if err != nil {
			return err
		}
	}

	return nil
}

// =============================================================================
// CUSTOM COLLECTOR MANAGEMENT
// =============================================================================

// RegisterCustomCollector registers a custom metric collector.
func (cc *CustomCollector) RegisterCustomCollector(collector shared.CustomCollector) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	name := collector.Name()
	if _, exists := cc.collectors[name]; exists {
		return fmt.Errorf("custom collector %s already registered", name)
	}

	cc.collectors[name] = collector

	if cc.logger != nil {
		cc.logger.Info("custom collector registered",
			logger.String("collector", name),
		)
	}

	return nil
}

// UnregisterCustomCollector unregisters a custom metric collector.
func (cc *CustomCollector) UnregisterCustomCollector(name string) error {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	collector, exists := cc.collectors[name]
	if !exists {
		return fmt.Errorf("custom collector %s not found", name)
	}

	collector.Reset()
	delete(cc.collectors, name)

	if cc.logger != nil {
		cc.logger.Info("custom collector unregistered",
			logger.String("collector", name),
		)
	}

	return nil
}

// RegisterCustomCallback registers a custom callback function.
func (cc *CustomCollector) RegisterCustomCallback(callback CustomCallback) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.customCallbacks = append(cc.customCallbacks, callback)
}

// =============================================================================
// BUSINESS METRICS
// =============================================================================

// RecordBusinessMetric records a business-specific metric.
func (cc *CustomCollector) RecordBusinessMetric(name string, value any, metricType, unit, description string, tags map[string]string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if !cc.enabled {
		return
	}

	cc.businessMetrics[name] = &BusinessMetric{
		Name:        name,
		Value:       value,
		Type:        metricType,
		Unit:        unit,
		Description: description,
		Tags:        tags,
		Timestamp:   time.Now(),
		Source:      "application",
	}
}

// collectBusinessMetrics collects business metrics.
func (cc *CustomCollector) collectBusinessMetrics() error {
	for name, metric := range cc.businessMetrics {
		prefix := "custom.business." + name
		cc.metrics[prefix+".value"] = metric.Value
		cc.metrics[prefix+".type"] = metric.Type
		cc.metrics[prefix+".unit"] = metric.Unit
		cc.metrics[prefix+".description"] = metric.Description
		cc.metrics[prefix+".timestamp"] = metric.Timestamp
		cc.metrics[prefix+".source"] = metric.Source

		// Add tags
		for tagKey, tagValue := range metric.Tags {
			cc.metrics[prefix+".tags."+tagKey] = tagValue
		}
	}

	return nil
}

// =============================================================================
// GAUGE METRICS
// =============================================================================

// SetGaugeMetric sets a gauge metric value.
func (cc *CustomCollector) SetGaugeMetric(name string, value float64, description, unit string, tags map[string]string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if !cc.enabled {
		return
	}

	metric, exists := cc.gaugeMetrics[name]
	if !exists {
		metric = &GaugeMetric{
			Name:        name,
			Description: description,
			Tags:        tags,
			Unit:        unit,
			MinValue:    value,
			MaxValue:    value,
		}
		cc.gaugeMetrics[name] = metric
	}

	metric.Value = value
	metric.Timestamp = time.Now()

	// Update min/max
	if value < metric.MinValue {
		metric.MinValue = value
	}

	if value > metric.MaxValue {
		metric.MaxValue = value
	}
}

// collectGaugeMetrics collects gauge metrics.
func (cc *CustomCollector) collectGaugeMetrics() error {
	for name, metric := range cc.gaugeMetrics {
		prefix := "custom.gauge." + name
		cc.metrics[prefix+".value"] = metric.Value
		cc.metrics[prefix+".min_value"] = metric.MinValue
		cc.metrics[prefix+".max_value"] = metric.MaxValue
		cc.metrics[prefix+".description"] = metric.Description
		cc.metrics[prefix+".unit"] = metric.Unit
		cc.metrics[prefix+".timestamp"] = metric.Timestamp

		// Add tags
		for tagKey, tagValue := range metric.Tags {
			cc.metrics[prefix+".tags."+tagKey] = tagValue
		}
	}

	return nil
}

// =============================================================================
// COUNTER METRICS
// =============================================================================

// IncrementCounter increments a counter metric.
func (cc *CustomCollector) IncrementCounter(name string, increment int64, description, unit string, tags map[string]string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if !cc.enabled {
		return
	}

	metric, exists := cc.counterMetrics[name]
	if !exists {
		metric = &CounterMetric{
			Name:        name,
			Description: description,
			Tags:        tags,
			Unit:        unit,
		}
		cc.counterMetrics[name] = metric
	}

	metric.Value += increment
	metric.Increment = increment
	metric.Timestamp = time.Now()

	// Calculate rate (simple approximation)
	if metric.Timestamp.Sub(metric.Timestamp) > 0 {
		metric.Rate = float64(increment) / time.Since(metric.Timestamp).Seconds()
	}
}

// collectCounterMetrics collects counter metrics.
func (cc *CustomCollector) collectCounterMetrics() error {
	for name, metric := range cc.counterMetrics {
		prefix := "custom.counter." + name
		cc.metrics[prefix+".value"] = metric.Value
		cc.metrics[prefix+".increment"] = metric.Increment
		cc.metrics[prefix+".rate"] = metric.Rate
		cc.metrics[prefix+".description"] = metric.Description
		cc.metrics[prefix+".unit"] = metric.Unit
		cc.metrics[prefix+".timestamp"] = metric.Timestamp

		// Add tags
		for tagKey, tagValue := range metric.Tags {
			cc.metrics[prefix+".tags."+tagKey] = tagValue
		}
	}

	return nil
}

// =============================================================================
// HISTOGRAM METRICS
// =============================================================================

// RecordHistogramValue records a value in a histogram metric.
func (cc *CustomCollector) RecordHistogramValue(name string, value float64, description, unit string, tags map[string]string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if !cc.enabled {
		return
	}

	metric, exists := cc.histogramMetrics[name]
	if !exists {
		metric = &HistogramMetric{
			Name:        name,
			Description: description,
			Tags:        tags,
			Unit:        unit,
			Min:         value,
			Max:         value,
			Buckets:     make(map[float64]int64),
		}
		cc.histogramMetrics[name] = metric
	}

	metric.Count++
	metric.Sum += value
	metric.Mean = metric.Sum / float64(metric.Count)
	metric.Timestamp = time.Now()

	// Update min/max
	if value < metric.Min {
		metric.Min = value
	}

	if value > metric.Max {
		metric.Max = value
	}

	// Update buckets (simplified bucketing)
	buckets := []float64{0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 25.0, 50.0, 100.0}
	for _, bucket := range buckets {
		if value <= bucket {
			metric.Buckets[bucket]++
		}
	}
}

// collectHistogramMetrics collects histogram metrics.
func (cc *CustomCollector) collectHistogramMetrics() error {
	for name, metric := range cc.histogramMetrics {
		prefix := "custom.histogram." + name
		cc.metrics[prefix+".count"] = metric.Count
		cc.metrics[prefix+".sum"] = metric.Sum
		cc.metrics[prefix+".mean"] = metric.Mean
		cc.metrics[prefix+".min"] = metric.Min
		cc.metrics[prefix+".max"] = metric.Max
		cc.metrics[prefix+".description"] = metric.Description
		cc.metrics[prefix+".unit"] = metric.Unit
		cc.metrics[prefix+".timestamp"] = metric.Timestamp

		// Add buckets
		for bucket, count := range metric.Buckets {
			cc.metrics[prefix+fmt.Sprintf(".bucket_%.1f", bucket)] = count
		}

		// Add tags
		for tagKey, tagValue := range metric.Tags {
			cc.metrics[prefix+".tags."+tagKey] = tagValue
		}
	}

	return nil
}

// =============================================================================
// TIMER METRICS
// =============================================================================

// RecordTimerValue records a duration in a timer metric.
func (cc *CustomCollector) RecordTimerValue(name string, duration time.Duration, description string, tags map[string]string) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	if !cc.enabled {
		return
	}

	metric, exists := cc.timerMetrics[name]
	if !exists {
		metric = &TimerMetric{
			Name:        name,
			Description: description,
			Tags:        tags,
			MinTime:     duration,
			MaxTime:     duration,
			Durations:   make([]time.Duration, 0),
		}
		cc.timerMetrics[name] = metric
	}

	metric.Count++
	metric.TotalTime += duration
	metric.MeanTime = metric.TotalTime / time.Duration(metric.Count)
	metric.Timestamp = time.Now()

	// Update min/max
	if duration < metric.MinTime {
		metric.MinTime = duration
	}

	if duration > metric.MaxTime {
		metric.MaxTime = duration
	}

	// Store duration for percentile calculation
	metric.Durations = append(metric.Durations, duration)

	// Limit stored durations for memory efficiency
	if len(metric.Durations) > 1000 {
		metric.Durations = metric.Durations[100:]
	}

	// Calculate percentiles
	cc.calculateTimerPercentiles(metric)
}

// calculateTimerPercentiles calculates percentiles for timer metrics.
func (cc *CustomCollector) calculateTimerPercentiles(metric *TimerMetric) {
	if len(metric.Durations) == 0 {
		return
	}

	// Sort durations
	durations := make([]time.Duration, len(metric.Durations))
	copy(durations, metric.Durations)

	// Simple bubble sort
	for i := range len(durations) - 1 {
		for j := range len(durations) - i - 1 {
			if durations[j] > durations[j+1] {
				durations[j], durations[j+1] = durations[j+1], durations[j]
			}
		}
	}

	// Calculate percentiles
	n := len(durations)
	metric.P50Time = durations[n*50/100]
	metric.P95Time = durations[n*95/100]
	metric.P99Time = durations[n*99/100]
}

// collectTimerMetrics collects timer metrics.
func (cc *CustomCollector) collectTimerMetrics() error {
	for name, metric := range cc.timerMetrics {
		prefix := "custom.timer." + name
		cc.metrics[prefix+".count"] = metric.Count
		cc.metrics[prefix+".total_time"] = metric.TotalTime.Seconds()
		cc.metrics[prefix+".mean_time"] = metric.MeanTime.Seconds()
		cc.metrics[prefix+".min_time"] = metric.MinTime.Seconds()
		cc.metrics[prefix+".max_time"] = metric.MaxTime.Seconds()
		cc.metrics[prefix+".p50_time"] = metric.P50Time.Seconds()
		cc.metrics[prefix+".p95_time"] = metric.P95Time.Seconds()
		cc.metrics[prefix+".p99_time"] = metric.P99Time.Seconds()
		cc.metrics[prefix+".description"] = metric.Description
		cc.metrics[prefix+".timestamp"] = metric.Timestamp

		// Add tags
		for tagKey, tagValue := range metric.Tags {
			cc.metrics[prefix+".tags."+tagKey] = tagValue
		}
	}

	return nil
}

// =============================================================================
// CUSTOM COLLECTION METHODS
// =============================================================================

// collectFromCustomCollectors collects metrics from registered custom collectors.
func (cc *CustomCollector) collectFromCustomCollectors() error {
	for name, collector := range cc.collectors {
		if !collector.IsEnabled() {
			continue
		}

		collectorMetrics := collector.Collect()
		if collectorMetrics == nil {
			if cc.logger != nil {
				cc.logger.Error("failed to collect from custom collector",
					logger.String("collector", name),
				)
			}

			continue
		}

		// Add metrics with collector prefix
		for metricName, metricValue := range collectorMetrics {
			prefixedName := fmt.Sprintf("custom.collectors.%s.%s", name, metricName)
			cc.metrics[prefixedName] = metricValue
		}
	}

	return nil
}

// executeCustomCallbacks executes custom callback functions.
func (cc *CustomCollector) executeCustomCallbacks() error {
	for i, callback := range cc.customCallbacks {
		callbackMetrics, err := callback()
		if err != nil {
			if cc.logger != nil {
				cc.logger.Error("failed to execute custom callback",
					logger.Int("callback_index", i),
					logger.Error(err),
				)
			}

			continue
		}

		// Add metrics with callback prefix
		for metricName, metricValue := range callbackMetrics {
			prefixedName := fmt.Sprintf("custom.callbacks.%d.%s", i, metricName)
			cc.metrics[prefixedName] = metricValue
		}
	}

	return nil
}

// =============================================================================
// CONFIGURATION METHODS
// =============================================================================

// Enable enables the collector.
func (cc *CustomCollector) Enable() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.enabled = true
}

// Disable disables the collector.
func (cc *CustomCollector) Disable() {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.enabled = false
}

// IsEnabled returns whether the collector is enabled.
func (cc *CustomCollector) IsEnabled() bool {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return cc.enabled
}

// SetInterval sets the collection interval.
func (cc *CustomCollector) SetInterval(interval time.Duration) {
	cc.mu.Lock()
	defer cc.mu.Unlock()

	cc.interval = interval
}

// GetInterval returns the collection interval.
func (cc *CustomCollector) GetInterval() time.Duration {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	return cc.interval
}

// GetStats returns collector statistics.
func (cc *CustomCollector) GetStats() map[string]any {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	stats := make(map[string]any)
	stats["enabled"] = cc.enabled
	stats["interval"] = cc.interval
	stats["last_collection"] = cc.lastCollectionTime
	stats["custom_collectors"] = len(cc.collectors)
	stats["business_metrics"] = len(cc.businessMetrics)
	stats["gauge_metrics"] = len(cc.gaugeMetrics)
	stats["counter_metrics"] = len(cc.counterMetrics)
	stats["histogram_metrics"] = len(cc.histogramMetrics)
	stats["timer_metrics"] = len(cc.timerMetrics)
	stats["custom_callbacks"] = len(cc.customCallbacks)

	return stats
}

// =============================================================================
// EXAMPLE CUSTOM COLLECTORS
// =============================================================================

// BusinessMetricsCollector is an example custom collector for business metrics.
type BusinessMetricsCollector struct {
	name    string
	enabled bool
}

func (bmc *BusinessMetricsCollector) Name() string {
	return bmc.name
}

func (bmc *BusinessMetricsCollector) Collect() (map[string]any, error) {
	if !bmc.enabled {
		return nil, nil
	}

	// Example business metrics
	metrics := map[string]any{
		"orders_completed":      1250,
		"revenue_today":         45000.50,
		"active_users":          3200,
		"conversion_rate":       0.045,
		"average_order_value":   36.25,
		"cart_abandonment_rate": 0.68,
	}

	return metrics, nil
}

func (bmc *BusinessMetricsCollector) Reset() error {
	// Nothing to reset for this example
	return nil
}

func (bmc *BusinessMetricsCollector) IsEnabled() bool {
	return bmc.enabled
}

// FeatureUsageCollector is an example custom collector for feature usage metrics.
type FeatureUsageCollector struct {
	name    string
	enabled bool
}

// NewFeatureUsageCollector creates a new feature usage collector.
func NewFeatureUsageCollector() shared.CustomCollector {
	return &FeatureUsageCollector{
		name:    "feature_usage",
		enabled: true,
	}
}

func (fuc *FeatureUsageCollector) Name() string {
	return fuc.name
}

func (fuc *FeatureUsageCollector) Collect() map[string]any {
	if !fuc.enabled {
		return nil
	}

	// Example feature usage metrics
	metrics := map[string]any{
		"search_usage":       8500,
		"export_usage":       1200,
		"sharing_usage":      3400,
		"notification_usage": 5600,
		"dashboard_views":    12000,
		"mobile_app_usage":   2800,
	}

	return metrics
}

func (fuc *FeatureUsageCollector) Reset() error {
	// Nothing to reset for this example
	return nil
}

func (fuc *FeatureUsageCollector) IsEnabled() bool {
	return fuc.enabled
}

// =============================================================================
// UTILITY METHODS
// =============================================================================

// GetBusinessMetrics returns all business metrics.
func (cc *CustomCollector) GetBusinessMetrics() map[string]*BusinessMetric {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	result := make(map[string]*BusinessMetric)
	maps.Copy(result, cc.businessMetrics)

	return result
}

// GetGaugeMetrics returns all gauge metrics.
func (cc *CustomCollector) GetGaugeMetrics() map[string]*GaugeMetric {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	result := make(map[string]*GaugeMetric)
	maps.Copy(result, cc.gaugeMetrics)

	return result
}

// GetCounterMetrics returns all counter metrics.
func (cc *CustomCollector) GetCounterMetrics() map[string]*CounterMetric {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	result := make(map[string]*CounterMetric)
	maps.Copy(result, cc.counterMetrics)

	return result
}

// GetHistogramMetrics returns all histogram metrics.
func (cc *CustomCollector) GetHistogramMetrics() map[string]*HistogramMetric {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	result := make(map[string]*HistogramMetric)
	maps.Copy(result, cc.histogramMetrics)

	return result
}

// GetTimerMetrics returns all timer metrics.
func (cc *CustomCollector) GetTimerMetrics() map[string]*TimerMetric {
	cc.mu.RLock()
	defer cc.mu.RUnlock()

	result := make(map[string]*TimerMetric)
	maps.Copy(result, cc.timerMetrics)

	return result
}

// CreateMetricsWrapper creates a wrapper for metrics integration.
func (cc *CustomCollector) CreateMetricsWrapper(metricsCollector shared.Metrics) *CustomMetricsWrapper {
	return &CustomMetricsWrapper{
		collector:        cc,
		metricsCollector: metricsCollector,
	}
}

// CustomMetricsWrapper wraps the custom collector with metrics integration.
type CustomMetricsWrapper struct {
	collector        *CustomCollector
	metricsCollector shared.Metrics
}

// RecordBusinessMetric records a business metric via the wrapper.
func (cmw *CustomMetricsWrapper) RecordBusinessMetric(name string, value float64, tags map[string]string) {
	cmw.collector.RecordBusinessMetric(name, value, "business", "count", "Business metric: "+name, tags)
}

// SetGaugeValue sets a gauge value via the wrapper.
func (cmw *CustomMetricsWrapper) SetGaugeValue(name string, value float64, tags map[string]string) {
	cmw.collector.SetGaugeMetric(name, value, "Custom gauge: "+name, "value", tags)
}

// IncrementCustomCounter increments a custom counter via the wrapper.
func (cmw *CustomMetricsWrapper) IncrementCustomCounter(name string, increment int64, tags map[string]string) {
	cmw.collector.IncrementCounter(name, increment, "Custom counter: "+name, "count", tags)
}

// RecordCustomHistogram records a custom histogram value via the wrapper.
func (cmw *CustomMetricsWrapper) RecordCustomHistogram(name string, value float64, tags map[string]string) {
	cmw.collector.RecordHistogramValue(name, value, "Custom histogram: "+name, "value", tags)
}

// RecordCustomTimer records a custom timer value via the wrapper.
func (cmw *CustomMetricsWrapper) RecordCustomTimer(name string, duration time.Duration, tags map[string]string) {
	cmw.collector.RecordTimerValue(name, duration, "Custom timer: "+name, tags)
}

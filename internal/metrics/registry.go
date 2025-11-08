package metrics

// The Reset() implementations are intentionally void for testability and simplicity.

import (
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/internal/errors"
	"github.com/xraph/forge/internal/metrics/internal"
)

// =============================================================================
// REGISTRY INTERFACE
// =============================================================================

// Registry manages metric storage and retrieval.
type Registry interface {
	// Metric creation and retrieval
	GetOrCreateCounter(name string, tags map[string]string) internal.Counter
	GetOrCreateGauge(name string, tags map[string]string) internal.Gauge
	GetOrCreateHistogram(name string, tags map[string]string) internal.Histogram
	GetOrCreateTimer(name string, tags map[string]string) internal.Timer

	// Metric retrieval
	GetMetric(name string, tags map[string]string) any
	GetAllMetrics() map[string]any
	GetMetricsByType(metricType internal.MetricType) map[string]any
	GetMetricsByTag(tagKey, tagValue string) map[string]any
	GetMetricsByNamePattern(pattern string) map[string]any

	// Metric management
	RegisterMetric(name string, metric any, metricType internal.MetricType, tags map[string]string) error
	UnregisterMetric(name string, tags map[string]string) error
	ResetMetric(name string) error
	Reset() error

	// Statistics
	Count() int
	GetRegisteredMetrics() []*RegisteredMetric
	GetMetricMetadata(name string, tags map[string]string) *internal.MetricMetadata

	// Lifecycle
	Start() error
	Stop() error
}

// =============================================================================
// REGISTERED METRIC
// =============================================================================

// RegisteredMetric represents a registered metric with metadata.
type RegisteredMetric struct {
	Name        string                   `json:"name"`
	Type        internal.MetricType      `json:"type"`
	Tags        map[string]string        `json:"tags"`
	Metric      any                      `json:"-"`
	Metadata    *internal.MetricMetadata `json:"metadata"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
	AccessCount int64                    `json:"access_count"`
	LastAccess  time.Time                `json:"last_access"`
}

// GetValue returns the current value of the metric.
func (rm *RegisteredMetric) GetValue() any {
	switch rm.Type {
	case internal.MetricTypeCounter:
		if counter, ok := rm.Metric.(internal.Counter); ok {
			return counter.Get()
		}
	case internal.MetricTypeGauge:
		if gauge, ok := rm.Metric.(internal.Gauge); ok {
			return gauge.Get()
		}
	case internal.MetricTypeHistogram:
		if histogram, ok := rm.Metric.(internal.Histogram); ok {
			return map[string]any{
				"count":   histogram.GetCount(),
				"sum":     histogram.GetSum(),
				"mean":    histogram.GetMean(),
				"buckets": histogram.GetBuckets(),
			}
		}
	case internal.MetricTypeTimer:
		if timer, ok := rm.Metric.(internal.Timer); ok {
			return map[string]any{
				"count": timer.GetCount(),
				"mean":  timer.GetMean(),
				"min":   timer.GetMin(),
				"max":   timer.GetMax(),
				"p50":   timer.GetPercentile(50),
				"p95":   timer.GetPercentile(95),
				"p99":   timer.GetPercentile(99),
			}
		}
	}

	return nil
}

// Reset resets the metric.
func (rm *RegisteredMetric) Reset() error {
	switch rm.Type {
	case internal.MetricTypeCounter:
		if counter, ok := rm.Metric.(internal.Counter); ok {
			counter.Reset()
		}
	case internal.MetricTypeGauge:
		if gauge, ok := rm.Metric.(internal.Gauge); ok {
			gauge.Reset()
		}
	case internal.MetricTypeHistogram:
		if histogram, ok := rm.Metric.(internal.Histogram); ok {
			histogram.Reset()
		}
	case internal.MetricTypeTimer:
		if timer, ok := rm.Metric.(internal.Timer); ok {
			timer.Reset()
		}
	}

	rm.UpdatedAt = time.Now()

	return nil
}

// UpdateAccess updates access statistics.
func (rm *RegisteredMetric) UpdateAccess() {
	rm.AccessCount++
	rm.LastAccess = time.Now()
}

// =============================================================================
// REGISTRY IMPLEMENTATION
// =============================================================================

// registry implements Registry interface.
type registry struct {
	metrics         map[string]*RegisteredMetric
	nameIndex       map[string][]*RegisteredMetric
	typeIndex       map[internal.MetricType][]*RegisteredMetric
	tagIndex        map[string]map[string][]*RegisteredMetric
	mu              sync.RWMutex
	started         bool
	maxMetrics      int
	cleanupInterval time.Duration
	lastCleanup     time.Time
}

// RegistryConfig contains configuration for the registry.
type RegistryConfig struct {
	MaxMetrics      int           `json:"max_metrics"      yaml:"max_metrics"`
	CleanupInterval time.Duration `json:"cleanup_interval" yaml:"cleanup_interval"`
	EnableIndexing  bool          `json:"enable_indexing"  yaml:"enable_indexing"`
}

// DefaultRegistryConfig returns default registry configuration.
func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		MaxMetrics:      10000,
		CleanupInterval: time.Hour,
		EnableIndexing:  true,
	}
}

// NewRegistry creates a new metrics registry.
func NewRegistry() Registry {
	return NewRegistryWithConfig(DefaultRegistryConfig())
}

// NewRegistryWithConfig creates a new metrics registry with custom configuration.
func NewRegistryWithConfig(config *RegistryConfig) Registry {
	return &registry{
		metrics:         make(map[string]*RegisteredMetric),
		nameIndex:       make(map[string][]*RegisteredMetric),
		typeIndex:       make(map[internal.MetricType][]*RegisteredMetric),
		tagIndex:        make(map[string]map[string][]*RegisteredMetric),
		maxMetrics:      config.MaxMetrics,
		cleanupInterval: config.CleanupInterval,
	}
}

// =============================================================================
// METRIC CREATION AND RETRIEVAL
// =============================================================================

// GetOrCreateCounter creates or retrieves a counter metric.
func (r *registry) GetOrCreateCounter(name string, tags map[string]string) internal.Counter {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()

		if counter, ok := registered.Metric.(internal.Counter); ok {
			return counter
		}
	}

	// Create new counter
	counter := internal.NewCounter()
	r.registerMetricInternal(name, counter, internal.MetricTypeCounter, tags)

	return counter
}

// GetOrCreateGauge creates or retrieves a gauge metric.
func (r *registry) GetOrCreateGauge(name string, tags map[string]string) internal.Gauge {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()

		if gauge, ok := registered.Metric.(internal.Gauge); ok {
			return gauge
		}
	}

	// Create new gauge
	gauge := internal.NewGauge()
	r.registerMetricInternal(name, gauge, internal.MetricTypeGauge, tags)

	return gauge
}

// GetOrCreateHistogram creates or retrieves a histogram metric.
func (r *registry) GetOrCreateHistogram(name string, tags map[string]string) internal.Histogram {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()

		if histogram, ok := registered.Metric.(internal.Histogram); ok {
			return histogram
		}
	}

	// Create new histogram
	histogram := internal.NewHistogram()
	r.registerMetricInternal(name, histogram, internal.MetricTypeHistogram, tags)

	return histogram
}

// GetOrCreateTimer creates or retrieves a timer metric.
func (r *registry) GetOrCreateTimer(name string, tags map[string]string) internal.Timer {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()

		if timer, ok := registered.Metric.(internal.Timer); ok {
			return timer
		}
	}

	// Create new timer
	timer := internal.NewTimer()
	r.registerMetricInternal(name, timer, internal.MetricTypeTimer, tags)

	return timer
}

// =============================================================================
// METRIC RETRIEVAL
// =============================================================================

// GetMetric retrieves a specific metric.
func (r *registry) GetMetric(name string, tags map[string]string) any {
	key := r.buildKey(name, tags)

	r.mu.RLock()
	defer r.mu.RUnlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()

		return registered.Metric
	}

	return nil
}

// GetAllMetrics returns all metrics.
func (r *registry) GetAllMetrics() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]any)

	for key, registered := range r.metrics {
		registered.UpdateAccess()
		result[key] = registered.GetValue()
	}

	return result
}

// GetMetricsByType returns metrics filtered by type.
func (r *registry) GetMetricsByType(metricType internal.MetricType) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]any)

	if metrics, exists := r.typeIndex[metricType]; exists {
		for _, registered := range metrics {
			registered.UpdateAccess()
			key := r.buildKey(registered.Name, registered.Tags)
			result[key] = registered.GetValue()
		}
	}

	return result
}

// GetMetricsByTag returns metrics filtered by tag.
func (r *registry) GetMetricsByTag(tagKey, tagValue string) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]any)

	if tagMap, exists := r.tagIndex[tagKey]; exists {
		if metrics, exists := tagMap[tagValue]; exists {
			for _, registered := range metrics {
				registered.UpdateAccess()
				key := r.buildKey(registered.Name, registered.Tags)
				result[key] = registered.GetValue()
			}
		}
	}

	return result
}

// GetMetricsByNamePattern returns metrics filtered by name pattern.
func (r *registry) GetMetricsByNamePattern(pattern string) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]any)

	// Simple pattern matching - could be enhanced with regex
	for key, registered := range r.metrics {
		if r.matchesPattern(registered.Name, pattern) {
			registered.UpdateAccess()
			result[key] = registered.GetValue()
		}
	}

	return result
}

// =============================================================================
// METRIC MANAGEMENT
// =============================================================================

// RegisterMetric registers a metric manually.
func (r *registry) RegisterMetric(name string, metric any, metricType internal.MetricType, tags map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.registerMetricInternal(name, metric, metricType, tags)
}

// UnregisterMetric unregisters a metric.
func (r *registry) UnregisterMetric(name string, tags map[string]string) error {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	registered, exists := r.metrics[key]
	if !exists {
		return errors.ErrServiceNotFound(key)
	}

	// Remove from indexes
	r.removeFromIndexes(registered)

	// Remove from main registry
	delete(r.metrics, key)

	return nil
}

// ResetMetric resets a specific metric.
func (r *registry) ResetMetric(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	found := false

	for _, registered := range r.metrics {
		if registered.Name == name {
			registered.Reset()

			found = true
		}
	}

	if !found {
		return errors.ErrServiceNotFound(name)
	}

	return nil
}

// Reset resets all metrics.
func (r *registry) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, registered := range r.metrics {
		registered.Reset()
	}

	r.metrics = make(map[string]*RegisteredMetric)
	r.nameIndex = make(map[string][]*RegisteredMetric)
	r.typeIndex = make(map[internal.MetricType][]*RegisteredMetric)
	r.tagIndex = make(map[string]map[string][]*RegisteredMetric)

	return nil
}

// =============================================================================
// STATISTICS
// =============================================================================

// Count returns the number of registered metrics.
func (r *registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.metrics)
}

// GetRegisteredMetrics returns all registered metrics.
func (r *registry) GetRegisteredMetrics() []*RegisteredMetric {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics := make([]*RegisteredMetric, 0, len(r.metrics))
	for _, registered := range r.metrics {
		metrics = append(metrics, registered)
	}

	return metrics
}

// GetMetricMetadata returns metadata for a specific metric.
func (r *registry) GetMetricMetadata(name string, tags map[string]string) *internal.MetricMetadata {
	key := r.buildKey(name, tags)

	r.mu.RLock()
	defer r.mu.RUnlock()

	if registered, exists := r.metrics[key]; exists {
		return registered.Metadata
	}

	return nil
}

// =============================================================================
// LIFECYCLE
// =============================================================================

// Start starts the registry.
func (r *registry) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return errors.ErrServiceAlreadyExists("registry")
	}

	r.started = true
	r.lastCleanup = time.Now()

	// Start cleanup goroutine
	go r.cleanupLoop()

	return nil
}

// Stop stops the registry.
func (r *registry) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return errors.ErrServiceNotFound("registry")
	}

	r.started = false

	return nil
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// buildKey builds a unique key for a metric.
func (r *registry) buildKey(name string, tags map[string]string) string {
	if len(tags) == 0 {
		return name
	}

	return name + "{" + internal.TagsToString(tags) + "}"
}

// registerMetricInternal registers a metric internally (assumes lock held).
func (r *registry) registerMetricInternal(name string, metric any, metricType internal.MetricType, tags map[string]string) error {
	if len(r.metrics) >= r.maxMetrics {
		return errors.ErrInvalidConfig("max_metrics", fmt.Errorf("maximum number of metrics reached: %d", r.maxMetrics))
	}

	key := r.buildKey(name, tags)

	// Check if metric already exists
	if _, exists := r.metrics[key]; exists {
		return errors.ErrServiceAlreadyExists(key)
	}

	// Create registered metric
	registered := &RegisteredMetric{
		Name:      name,
		Type:      metricType,
		Tags:      tags,
		Metric:    metric,
		Metadata:  r.createMetadata(name, metricType, tags),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Add to main registry
	r.metrics[key] = registered

	// Add to indexes
	r.addToIndexes(registered)

	return nil
}

// createMetadata creates metadata for a metric.
func (r *registry) createMetadata(name string, metricType internal.MetricType, tags map[string]string) *internal.MetricMetadata {
	return &internal.MetricMetadata{
		Name:        name,
		Type:        metricType,
		Description: r.generateDescription(name, metricType),
		Tags:        tags,
		Unit:        r.inferUnit(name, metricType),
		Created:     time.Now(),
		Updated:     time.Now(),
	}
}

// generateDescription generates a description for a metric.
func (r *registry) generateDescription(name string, metricType internal.MetricType) string {
	switch metricType {
	case internal.MetricTypeCounter:
		return "Counter metric: " + name
	case internal.MetricTypeGauge:
		return "Gauge metric: " + name
	case internal.MetricTypeHistogram:
		return "Histogram metric: " + name
	case internal.MetricTypeTimer:
		return "Timer metric: " + name
	default:
		return "Metric: " + name
	}
}

// inferUnit infers the unit for a metric based on name and type.
func (r *registry) inferUnit(name string, metricType internal.MetricType) string {
	// Simple unit inference based on common patterns
	switch metricType {
	case internal.MetricTypeTimer:
		return "seconds"
	case internal.MetricTypeCounter:
		if name == "requests" || name == "errors" {
			return "count"
		}

		return "count"
	case internal.MetricTypeGauge:
		if name == "memory" || name == "bytes" {
			return "bytes"
		}

		if name == "cpu" {
			return "percent"
		}

		return "value"
	case internal.MetricTypeHistogram:
		if name == "latency" || name == "duration" {
			return "seconds"
		}

		return "value"
	}

	return "value"
}

// addToIndexes adds a metric to all indexes.
func (r *registry) addToIndexes(registered *RegisteredMetric) {
	// Add to name index
	r.nameIndex[registered.Name] = append(r.nameIndex[registered.Name], registered)

	// Add to type index
	r.typeIndex[registered.Type] = append(r.typeIndex[registered.Type], registered)

	// Add to tag index
	for tagKey, tagValue := range registered.Tags {
		if r.tagIndex[tagKey] == nil {
			r.tagIndex[tagKey] = make(map[string][]*RegisteredMetric)
		}

		r.tagIndex[tagKey][tagValue] = append(r.tagIndex[tagKey][tagValue], registered)
	}
}

// removeFromIndexes removes a metric from all indexes.
func (r *registry) removeFromIndexes(registered *RegisteredMetric) {
	// Remove from name index
	if metrics, exists := r.nameIndex[registered.Name]; exists {
		for i, metric := range metrics {
			if metric == registered {
				r.nameIndex[registered.Name] = append(metrics[:i], metrics[i+1:]...)

				break
			}
		}

		if len(r.nameIndex[registered.Name]) == 0 {
			delete(r.nameIndex, registered.Name)
		}
	}

	// Remove from type index
	if metrics, exists := r.typeIndex[registered.Type]; exists {
		for i, metric := range metrics {
			if metric == registered {
				r.typeIndex[registered.Type] = append(metrics[:i], metrics[i+1:]...)

				break
			}
		}

		if len(r.typeIndex[registered.Type]) == 0 {
			delete(r.typeIndex, registered.Type)
		}
	}

	// Remove from tag index
	for tagKey, tagValue := range registered.Tags {
		if tagMap, exists := r.tagIndex[tagKey]; exists {
			if metrics, exists := tagMap[tagValue]; exists {
				for i, metric := range metrics {
					if metric == registered {
						tagMap[tagValue] = append(metrics[:i], metrics[i+1:]...)

						break
					}
				}

				if len(tagMap[tagValue]) == 0 {
					delete(tagMap, tagValue)
				}
			}

			if len(tagMap) == 0 {
				delete(r.tagIndex, tagKey)
			}
		}
	}
}

// matchesPattern checks if a name matches a pattern.
func (r *registry) matchesPattern(name, pattern string) bool {
	// Simple pattern matching - could be enhanced with regex
	// For now, just check if pattern is a substring
	return name == pattern || (pattern == "*") ||
		(len(pattern) > 0 && pattern[len(pattern)-1] == '*' &&
			len(name) >= len(pattern)-1 && name[:len(pattern)-1] == pattern[:len(pattern)-1])
}

// cleanupLoop runs periodic cleanup.
func (r *registry) cleanupLoop() {
	ticker := time.NewTicker(r.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !r.started {
				return
			}

			r.cleanup()
		}
	}
}

// cleanup performs registry cleanup.
func (r *registry) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastCleanup = time.Now()

	// Could implement cleanup logic here:
	// - Remove old unused metrics
	// - Compact indexes
	// - Reset inactive metrics
}

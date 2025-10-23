package metrics

import (
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// =============================================================================
// REGISTRY INTERFACE
// =============================================================================

// Registry manages metric storage and retrieval
type Registry interface {
	// Metric creation and retrieval
	GetOrCreateCounter(name string, tags map[string]string) Counter
	GetOrCreateGauge(name string, tags map[string]string) Gauge
	GetOrCreateHistogram(name string, tags map[string]string) Histogram
	GetOrCreateTimer(name string, tags map[string]string) Timer

	// Metric retrieval
	GetMetric(name string, tags map[string]string) interface{}
	GetAllMetrics() map[string]interface{}
	GetMetricsByType(metricType MetricType) map[string]interface{}
	GetMetricsByTag(tagKey, tagValue string) map[string]interface{}
	GetMetricsByNamePattern(pattern string) map[string]interface{}

	// Metric management
	RegisterMetric(name string, metric interface{}, metricType MetricType, tags map[string]string) error
	UnregisterMetric(name string, tags map[string]string) error
	ResetMetric(name string) error
	Reset() error

	// Statistics
	Count() int
	GetRegisteredMetrics() []*RegisteredMetric
	GetMetricMetadata(name string, tags map[string]string) *MetricMetadata

	// Lifecycle
	Start() error
	Stop() error
}

// =============================================================================
// REGISTERED METRIC
// =============================================================================

// RegisteredMetric represents a registered metric with metadata
type RegisteredMetric struct {
	Name        string            `json:"name"`
	Type        MetricType        `json:"type"`
	Tags        map[string]string `json:"tags"`
	Metric      interface{}       `json:"-"`
	Metadata    *MetricMetadata   `json:"metadata"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	AccessCount int64             `json:"access_count"`
	LastAccess  time.Time         `json:"last_access"`
}

// GetValue returns the current value of the metric
func (rm *RegisteredMetric) GetValue() interface{} {
	switch rm.Type {
	case MetricTypeCounter:
		if counter, ok := rm.Metric.(Counter); ok {
			return counter.Get()
		}
	case MetricTypeGauge:
		if gauge, ok := rm.Metric.(Gauge); ok {
			return gauge.Get()
		}
	case MetricTypeHistogram:
		if histogram, ok := rm.Metric.(Histogram); ok {
			return map[string]interface{}{
				"count":   histogram.GetCount(),
				"sum":     histogram.GetSum(),
				"mean":    histogram.GetMean(),
				"buckets": histogram.GetBuckets(),
			}
		}
	case MetricTypeTimer:
		if timer, ok := rm.Metric.(Timer); ok {
			return map[string]interface{}{
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

// Reset resets the metric
func (rm *RegisteredMetric) Reset() error {
	switch rm.Type {
	case MetricTypeCounter:
		if counter, ok := rm.Metric.(Counter); ok {
			counter.Reset()
		}
	case MetricTypeGauge:
		if gauge, ok := rm.Metric.(Gauge); ok {
			gauge.Reset()
		}
	case MetricTypeHistogram:
		if histogram, ok := rm.Metric.(Histogram); ok {
			histogram.Reset()
		}
	case MetricTypeTimer:
		if timer, ok := rm.Metric.(Timer); ok {
			timer.Reset()
		}
	}
	rm.UpdatedAt = time.Now()
	return nil
}

// UpdateAccess updates access statistics
func (rm *RegisteredMetric) UpdateAccess() {
	rm.AccessCount++
	rm.LastAccess = time.Now()
}

// =============================================================================
// REGISTRY IMPLEMENTATION
// =============================================================================

// registry implements Registry interface
type registry struct {
	metrics         map[string]*RegisteredMetric
	nameIndex       map[string][]*RegisteredMetric
	typeIndex       map[MetricType][]*RegisteredMetric
	tagIndex        map[string]map[string][]*RegisteredMetric
	mu              sync.RWMutex
	started         bool
	maxMetrics      int
	cleanupInterval time.Duration
	lastCleanup     time.Time
}

// RegistryConfig contains configuration for the registry
type RegistryConfig struct {
	MaxMetrics      int           `yaml:"max_metrics" json:"max_metrics"`
	CleanupInterval time.Duration `yaml:"cleanup_interval" json:"cleanup_interval"`
	EnableIndexing  bool          `yaml:"enable_indexing" json:"enable_indexing"`
}

// DefaultRegistryConfig returns default registry configuration
func DefaultRegistryConfig() *RegistryConfig {
	return &RegistryConfig{
		MaxMetrics:      10000,
		CleanupInterval: time.Hour,
		EnableIndexing:  true,
	}
}

// NewRegistry creates a new metrics registry
func NewRegistry() Registry {
	return NewRegistryWithConfig(DefaultRegistryConfig())
}

// NewRegistryWithConfig creates a new metrics registry with custom configuration
func NewRegistryWithConfig(config *RegistryConfig) Registry {
	return &registry{
		metrics:         make(map[string]*RegisteredMetric),
		nameIndex:       make(map[string][]*RegisteredMetric),
		typeIndex:       make(map[MetricType][]*RegisteredMetric),
		tagIndex:        make(map[string]map[string][]*RegisteredMetric),
		maxMetrics:      config.MaxMetrics,
		cleanupInterval: config.CleanupInterval,
	}
}

// =============================================================================
// METRIC CREATION AND RETRIEVAL
// =============================================================================

// GetOrCreateCounter creates or retrieves a counter metric
func (r *registry) GetOrCreateCounter(name string, tags map[string]string) Counter {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()
		if counter, ok := registered.Metric.(Counter); ok {
			return counter
		}
	}

	// Create new counter
	counter := NewCounter()
	r.registerMetricInternal(name, counter, MetricTypeCounter, tags)
	return counter
}

// GetOrCreateGauge creates or retrieves a gauge metric
func (r *registry) GetOrCreateGauge(name string, tags map[string]string) Gauge {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()
		if gauge, ok := registered.Metric.(Gauge); ok {
			return gauge
		}
	}

	// Create new gauge
	gauge := NewGauge()
	r.registerMetricInternal(name, gauge, MetricTypeGauge, tags)
	return gauge
}

// GetOrCreateHistogram creates or retrieves a histogram metric
func (r *registry) GetOrCreateHistogram(name string, tags map[string]string) Histogram {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()
		if histogram, ok := registered.Metric.(Histogram); ok {
			return histogram
		}
	}

	// Create new histogram
	histogram := NewHistogram()
	r.registerMetricInternal(name, histogram, MetricTypeHistogram, tags)
	return histogram
}

// GetOrCreateTimer creates or retrieves a timer metric
func (r *registry) GetOrCreateTimer(name string, tags map[string]string) Timer {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()
		if timer, ok := registered.Metric.(Timer); ok {
			return timer
		}
	}

	// Create new timer
	timer := NewTimer()
	r.registerMetricInternal(name, timer, MetricTypeTimer, tags)
	return timer
}

// =============================================================================
// METRIC RETRIEVAL
// =============================================================================

// GetMetric retrieves a specific metric
func (r *registry) GetMetric(name string, tags map[string]string) interface{} {
	key := r.buildKey(name, tags)

	r.mu.RLock()
	defer r.mu.RUnlock()

	if registered, exists := r.metrics[key]; exists {
		registered.UpdateAccess()
		return registered.Metric
	}

	return nil
}

// GetAllMetrics returns all metrics
func (r *registry) GetAllMetrics() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]interface{})
	for key, registered := range r.metrics {
		registered.UpdateAccess()
		result[key] = registered.GetValue()
	}

	return result
}

// GetMetricsByType returns metrics filtered by type
func (r *registry) GetMetricsByType(metricType MetricType) map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]interface{})

	if metrics, exists := r.typeIndex[metricType]; exists {
		for _, registered := range metrics {
			registered.UpdateAccess()
			key := r.buildKey(registered.Name, registered.Tags)
			result[key] = registered.GetValue()
		}
	}

	return result
}

// GetMetricsByTag returns metrics filtered by tag
func (r *registry) GetMetricsByTag(tagKey, tagValue string) map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]interface{})

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

// GetMetricsByNamePattern returns metrics filtered by name pattern
func (r *registry) GetMetricsByNamePattern(pattern string) map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]interface{})

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

// RegisterMetric registers a metric manually
func (r *registry) RegisterMetric(name string, metric interface{}, metricType MetricType, tags map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.registerMetricInternal(name, metric, metricType, tags)
}

// UnregisterMetric unregisters a metric
func (r *registry) UnregisterMetric(name string, tags map[string]string) error {
	key := r.buildKey(name, tags)

	r.mu.Lock()
	defer r.mu.Unlock()

	registered, exists := r.metrics[key]
	if !exists {
		return common.ErrServiceNotFound(key)
	}

	// Remove from indexes
	r.removeFromIndexes(registered)

	// Remove from main registry
	delete(r.metrics, key)

	return nil
}

// ResetMetric resets a specific metric
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
		return common.ErrServiceNotFound(name)
	}

	return nil
}

// Reset resets all metrics
func (r *registry) Reset() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, registered := range r.metrics {
		registered.Reset()
	}

	r.metrics = make(map[string]*RegisteredMetric)
	r.nameIndex = make(map[string][]*RegisteredMetric)
	r.typeIndex = make(map[MetricType][]*RegisteredMetric)
	r.tagIndex = make(map[string]map[string][]*RegisteredMetric)

	return nil
}

// =============================================================================
// STATISTICS
// =============================================================================

// Count returns the number of registered metrics
func (r *registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.metrics)
}

// GetRegisteredMetrics returns all registered metrics
func (r *registry) GetRegisteredMetrics() []*RegisteredMetric {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metrics := make([]*RegisteredMetric, 0, len(r.metrics))
	for _, registered := range r.metrics {
		metrics = append(metrics, registered)
	}

	return metrics
}

// GetMetricMetadata returns metadata for a specific metric
func (r *registry) GetMetricMetadata(name string, tags map[string]string) *MetricMetadata {
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

// Start starts the registry
func (r *registry) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.started {
		return common.ErrServiceAlreadyExists("registry")
	}

	r.started = true
	r.lastCleanup = time.Now()

	// Start cleanup goroutine
	go r.cleanupLoop()

	return nil
}

// Stop stops the registry
func (r *registry) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.started {
		return common.ErrServiceNotFound("registry")
	}

	r.started = false
	return nil
}

// =============================================================================
// PRIVATE METHODS
// =============================================================================

// buildKey builds a unique key for a metric
func (r *registry) buildKey(name string, tags map[string]string) string {
	if len(tags) == 0 {
		return name
	}

	return name + "{" + TagsToString(tags) + "}"
}

// registerMetricInternal registers a metric internally (assumes lock held)
func (r *registry) registerMetricInternal(name string, metric interface{}, metricType MetricType, tags map[string]string) error {
	if len(r.metrics) >= r.maxMetrics {
		return common.ErrInvalidConfig("max_metrics", fmt.Errorf("maximum number of metrics reached: %d", r.maxMetrics))
	}

	key := r.buildKey(name, tags)

	// Check if metric already exists
	if _, exists := r.metrics[key]; exists {
		return common.ErrServiceAlreadyExists(key)
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

// createMetadata creates metadata for a metric
func (r *registry) createMetadata(name string, metricType MetricType, tags map[string]string) *MetricMetadata {
	return &MetricMetadata{
		Name:        name,
		Type:        metricType,
		Description: r.generateDescription(name, metricType),
		Tags:        tags,
		Unit:        r.inferUnit(name, metricType),
		Created:     time.Now(),
		Updated:     time.Now(),
	}
}

// generateDescription generates a description for a metric
func (r *registry) generateDescription(name string, metricType MetricType) string {
	switch metricType {
	case MetricTypeCounter:
		return fmt.Sprintf("Counter metric: %s", name)
	case MetricTypeGauge:
		return fmt.Sprintf("Gauge metric: %s", name)
	case MetricTypeHistogram:
		return fmt.Sprintf("Histogram metric: %s", name)
	case MetricTypeTimer:
		return fmt.Sprintf("Timer metric: %s", name)
	default:
		return fmt.Sprintf("Metric: %s", name)
	}
}

// inferUnit infers the unit for a metric based on name and type
func (r *registry) inferUnit(name string, metricType MetricType) string {
	// Simple unit inference based on common patterns
	switch metricType {
	case MetricTypeTimer:
		return "seconds"
	case MetricTypeCounter:
		if name == "requests" || name == "errors" {
			return "count"
		}
		return "count"
	case MetricTypeGauge:
		if name == "memory" || name == "bytes" {
			return "bytes"
		}
		if name == "cpu" {
			return "percent"
		}
		return "value"
	case MetricTypeHistogram:
		if name == "latency" || name == "duration" {
			return "seconds"
		}
		return "value"
	}
	return "value"
}

// addToIndexes adds a metric to all indexes
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

// removeFromIndexes removes a metric from all indexes
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

// matchesPattern checks if a name matches a pattern
func (r *registry) matchesPattern(name, pattern string) bool {
	// Simple pattern matching - could be enhanced with regex
	// For now, just check if pattern is a substring
	return name == pattern || (pattern == "*") ||
		(len(pattern) > 0 && pattern[len(pattern)-1] == '*' &&
			len(name) >= len(pattern)-1 && name[:len(pattern)-1] == pattern[:len(pattern)-1])
}

// cleanupLoop runs periodic cleanup
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

// cleanup performs registry cleanup
func (r *registry) cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastCleanup = time.Now()

	// Could implement cleanup logic here:
	// - Remove old unused metrics
	// - Compact indexes
	// - Reset inactive metrics
}

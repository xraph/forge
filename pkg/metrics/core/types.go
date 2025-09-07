package core

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge/pkg/common"
)

// =============================================================================
// METRIC INTERFACES
// =============================================================================

// Counter represents a counter metric
type Counter = common.Counter

// Gauge represents a gauge metric
type Gauge = common.Gauge

// Histogram represents a histogram metric
type Histogram = common.Histogram

// Timer represents a timer metric
type Timer = common.Timer

// CustomCollector defines interface for custom metrics collectors
type CustomCollector = common.CustomCollector

// =============================================================================
// EXPORTER INTERFACE
// =============================================================================

// Exporter defines the interface for metrics export
type Exporter = common.Exporter

// ExportFormat represents the format for metrics export
type ExportFormat = common.ExportFormat

const (
	ExportFormatPrometheus = common.ExportFormatPrometheus
	ExportFormatJSON       = common.ExportFormatJSON
	ExportFormatInflux     = common.ExportFormatInflux
	ExportFormatStatsD     = common.ExportFormatStatsD
)

// MetricType represents the type of metric
type MetricType = common.MetricType

const (
	MetricTypeCounter   = common.MetricTypeCounter
	MetricTypeGauge     = common.MetricTypeGauge
	MetricTypeHistogram = common.MetricTypeHistogram
	MetricTypeTimer     = common.MetricTypeTimer
)

// =============================================================================
// UTILITY FUNCTIONS
// =============================================================================

// TagsToString converts tags map to string representation
func TagsToString(tags map[string]string) string {
	if len(tags) == 0 {
		return ""
	}

	var parts []string
	for k, v := range tags {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)

	result := ""
	for i, part := range parts {
		if i > 0 {
			result += ","
		}
		result += part
	}
	return result
}

// ParseTags parses tags from string array
func ParseTags(tags ...string) map[string]string {
	result := make(map[string]string)

	for i := 0; i < len(tags); i += 2 {
		if i+1 < len(tags) {
			result[tags[i]] = tags[i+1]
		}
	}

	return result
}

// MergeTags merges multiple tag maps
func MergeTags(tagMaps ...map[string]string) map[string]string {
	result := make(map[string]string)

	for _, tags := range tagMaps {
		for k, v := range tags {
			result[k] = v
		}
	}

	return result
}

// ValidateMetricName validates metric name format
func ValidateMetricName(name string) bool {
	if name == "" {
		return false
	}

	// Basic validation - alphanumeric, underscore, dot, hyphen
	for _, char := range name {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '.' || char == '-') {
			return false
		}
	}

	return true
}

// NormalizeMetricName normalizes metric name
func NormalizeMetricName(name string) string {
	// Replace invalid characters with underscore
	normalized := ""
	for _, char := range name {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' || char == '.' || char == '-' {
			normalized += string(char)
		} else {
			normalized += "_"
		}
	}
	return normalized
}

// =============================================================================
// METRIC IMPLEMENTATIONS
// =============================================================================

// counterImpl implements Counter interface
type counterImpl struct {
	value int64
}

// NewCounter creates a new counter
func NewCounter() Counter {
	return &counterImpl{}
}

func (c *counterImpl) Inc() {
	atomic.AddInt64(&c.value, 1)
}
func (c *counterImpl) Dec() {
	atomic.AddInt64(&c.value, -1)
}

func (c *counterImpl) Add(value float64) {
	if value < 0 {
		return // Counters can only increase
	}
	atomic.AddInt64(&c.value, int64(value))
}

func (c *counterImpl) Get() float64 {
	return float64(atomic.LoadInt64(&c.value))
}

func (c *counterImpl) Reset() {
	atomic.StoreInt64(&c.value, 0)
}

// gaugeImpl implements Gauge interface
type gaugeImpl struct {
	value int64 // Store as bits for atomic operations
}

// NewGauge creates a new gauge
func NewGauge() Gauge {
	return &gaugeImpl{}
}

func (g *gaugeImpl) Set(value float64) {
	atomic.StoreInt64(&g.value, int64(math.Float64bits(value)))
}

func (g *gaugeImpl) Inc() {
	g.Add(1)
}

func (g *gaugeImpl) Dec() {
	g.Add(-1)
}

func (g *gaugeImpl) Add(value float64) {
	for {
		old := atomic.LoadInt64(&g.value)
		oldVal := math.Float64frombits(uint64(old))
		newVal := oldVal + value
		if atomic.CompareAndSwapInt64(&g.value, old, int64(math.Float64bits(newVal))) {
			break
		}
	}
}

func (g *gaugeImpl) Get() float64 {
	return math.Float64frombits(uint64(atomic.LoadInt64(&g.value)))
}

func (g *gaugeImpl) Reset() {
	atomic.StoreInt64(&g.value, 0)
}

// histogramImpl implements Histogram interface
type histogramImpl struct {
	mu      sync.RWMutex
	buckets map[float64]uint64
	count   uint64
	sum     float64
	values  []float64 // For percentile calculation
}

// NewHistogram creates a new histogram with default buckets
func NewHistogram() Histogram {
	return NewHistogramWithBuckets(DefaultBuckets)
}

// NewHistogramWithBuckets creates a new histogram with custom buckets
func NewHistogramWithBuckets(buckets []float64) Histogram {
	bucketMap := make(map[float64]uint64)
	for _, bucket := range buckets {
		bucketMap[bucket] = 0
	}

	return &histogramImpl{
		buckets: bucketMap,
		values:  make([]float64, 0),
	}
}

// DefaultBuckets provides default histogram buckets
var DefaultBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50, 100, 250, 500, 1000,
}

func (h *histogramImpl) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	h.sum += value
	h.values = append(h.values, value)

	// Update buckets
	for bucket := range h.buckets {
		if value <= bucket {
			h.buckets[bucket]++
		}
	}
}

func (h *histogramImpl) GetBuckets() map[float64]uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	buckets := make(map[float64]uint64)
	for k, v := range h.buckets {
		buckets[k] = v
	}
	return buckets
}

func (h *histogramImpl) GetCount() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}

func (h *histogramImpl) GetSum() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sum
}

func (h *histogramImpl) GetMean() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.count == 0 {
		return 0
	}
	return h.sum / float64(h.count)
}

func (h *histogramImpl) GetPercentile(percentile float64) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.values) == 0 {
		return 0
	}

	// Sort values for percentile calculation
	sortedValues := make([]float64, len(h.values))
	copy(sortedValues, h.values)
	sort.Float64s(sortedValues)

	index := int(percentile/100*float64(len(sortedValues))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedValues) {
		index = len(sortedValues) - 1
	}

	return sortedValues[index]
}

func (h *histogramImpl) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count = 0
	h.sum = 0
	h.values = h.values[:0]

	for bucket := range h.buckets {
		h.buckets[bucket] = 0
	}
}

// timerImpl implements Timer interface
type timerImpl struct {
	mu        sync.RWMutex
	durations []time.Duration
	count     uint64
	sum       time.Duration
	min       time.Duration
	max       time.Duration
}

// NewTimer creates a new timer
func NewTimer() Timer {
	return &timerImpl{
		durations: make([]time.Duration, 0),
		min:       time.Duration(math.MaxInt64),
		max:       time.Duration(0),
	}
}

func (t *timerImpl) Record(duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.count++
	t.sum += duration
	t.durations = append(t.durations, duration)

	if duration < t.min {
		t.min = duration
	}
	if duration > t.max {
		t.max = duration
	}
}

func (t *timerImpl) Time() func() {
	start := time.Now()
	return func() {
		t.Record(time.Since(start))
	}
}

func (t *timerImpl) GetCount() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.count
}

func (t *timerImpl) GetMean() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.count == 0 {
		return 0
	}
	return t.sum / time.Duration(t.count)
}

func (t *timerImpl) GetPercentile(percentile float64) time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.durations) == 0 {
		return 0
	}

	// Sort durations for percentile calculation
	sortedDurations := make([]time.Duration, len(t.durations))
	copy(sortedDurations, t.durations)
	sort.Slice(sortedDurations, func(i, j int) bool {
		return sortedDurations[i] < sortedDurations[j]
	})

	index := int(percentile/100*float64(len(sortedDurations))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedDurations) {
		index = len(sortedDurations) - 1
	}

	return sortedDurations[index]
}

func (t *timerImpl) GetMin() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.count == 0 {
		return 0
	}
	return t.min
}

func (t *timerImpl) GetMax() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.count == 0 {
		return 0
	}
	return t.max
}

func (t *timerImpl) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.count = 0
	t.sum = 0
	t.min = time.Duration(math.MaxInt64)
	t.max = time.Duration(0)
	t.durations = t.durations[:0]
}

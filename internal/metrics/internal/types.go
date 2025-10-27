package internal

//nolint:gosec // G115: Integer conversions for metrics storage are safe and intentional
// These conversions are used for encoding float64 values using bit patterns.

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge/internal/shared"
)

// =============================================================================
// METRIC INTERFACES
// =============================================================================

type Metrics = shared.Metrics

type MetricsConfig = shared.MetricsConfig

type CollectorStats = shared.CollectorStats

// Counter represents a counter metric
type Counter = shared.Counter

// Gauge represents a gauge metric
type Gauge = shared.Gauge

// Histogram represents a histogram metric
type Histogram = shared.Histogram

// Timer represents a timer metric
type Timer = shared.Timer

// CustomCollector defines interface for custom metrics collectors
type CustomCollector = shared.CustomCollector

// =============================================================================
// METRIC METADATA
// =============================================================================

// MetricType represents the type of metric
type MetricType = shared.MetricType

const (
	MetricTypeCounter   = shared.MetricTypeCounter
	MetricTypeGauge     = shared.MetricTypeGauge
	MetricTypeHistogram = shared.MetricTypeHistogram
	MetricTypeTimer     = shared.MetricTypeTimer
)

// MetricMetadata contains metadata about a metric
type MetricMetadata struct {
	Name        string            `json:"name"`
	Type        MetricType        `json:"type"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Unit        string            `json:"unit"`
	Created     time.Time         `json:"created"`
	Updated     time.Time         `json:"updated"`
}

// MetricValue represents a metric value with metadata
type MetricValue struct {
	Metadata  *MetricMetadata `json:"metadata"`
	Value     interface{}     `json:"value"`
	Timestamp time.Time       `json:"timestamp"`
}

// ExportFormat represents the format for metrics export
type ExportFormat = shared.ExportFormat

const (
	ExportFormatPrometheus = shared.ExportFormatPrometheus
	ExportFormatJSON       = shared.ExportFormatJSON
	ExportFormatInflux     = shared.ExportFormatInflux
	ExportFormatStatsD     = shared.ExportFormatStatsD
)

// =============================================================================
// METRIC SAMPLE
// =============================================================================

// MetricSample represents a single metric sample
type MetricSample struct {
	Name      string            `json:"name"`
	Type      MetricType        `json:"type"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
	Unit      string            `json:"unit"`
}

// HistogramSample represents a histogram sample
type HistogramSample struct {
	Name      string             `json:"name"`
	Count     uint64             `json:"count"`
	Sum       float64            `json:"sum"`
	Buckets   map[float64]uint64 `json:"buckets"`
	Tags      map[string]string  `json:"tags"`
	Timestamp time.Time          `json:"timestamp"`
}

// TimerSample represents a timer sample
type TimerSample struct {
	Name      string            `json:"name"`
	Count     uint64            `json:"count"`
	Mean      time.Duration     `json:"mean"`
	Min       time.Duration     `json:"min"`
	Max       time.Duration     `json:"max"`
	P50       time.Duration     `json:"p50"`
	P95       time.Duration     `json:"p95"`
	P99       time.Duration     `json:"p99"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
}

// Exporter defines the interface for metrics export
type Exporter = shared.Exporter

// =============================================================================
// LABEL CONFIGURATION & LIMITS
// =============================================================================

const (
	// MaxLabelsPerMetric limits labels per metric to prevent cardinality explosion
	MaxLabelsPerMetric = 20

	// MaxLabelKeyLength limits label key length
	MaxLabelKeyLength = 128

	// MaxLabelValueLength limits label value length
	MaxLabelValueLength = 256

	// MaxLabelCardinality limits unique label combinations
	MaxLabelCardinality = 10000
)

// ReservedLabels are protected system labels
var ReservedLabels = map[string]bool{
	"__name__":     true,
	"__instance__": true,
	"__job__":      true,
	"__replica__":  true,
	"__tenant__":   true,
	"job":          true,
	"instance":     true,
	"le":           true, // Prometheus histogram bucket
	"quantile":     true, // Prometheus summary quantile
}

// LabelValidationError represents a label validation error
type LabelValidationError struct {
	Label  string
	Reason string
	Value  string
}

func (e *LabelValidationError) Error() string {
	return fmt.Sprintf("invalid label %q: %s (value: %q)", e.Label, e.Reason, e.Value)
}

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

// MergeTags merges multiple tag maps with validation
func MergeTags(tagMaps ...map[string]string) map[string]string {
	result := make(map[string]string)

	for _, tags := range tagMaps {
		for k, v := range tags {
			result[k] = v
		}
	}

	return result
}

// ValidateAndSanitizeTags validates and sanitizes tags for production use
func ValidateAndSanitizeTags(tags map[string]string) (map[string]string, error) {
	if len(tags) > MaxLabelsPerMetric {
		return nil, &LabelValidationError{
			Label:  "count",
			Reason: fmt.Sprintf("exceeds maximum %d labels", MaxLabelsPerMetric),
		}
	}

	sanitized := make(map[string]string, len(tags))

	for key, value := range tags {
		// Validate key
		if err := ValidateLabelKey(key); err != nil {
			return nil, err
		}

		// Validate value
		if err := ValidateLabelValue(key, value); err != nil {
			return nil, err
		}

		// Sanitize key and value
		sanitizedKey := SanitizeLabelKey(key)
		sanitizedValue := SanitizeLabelValue(value)

		sanitized[sanitizedKey] = sanitizedValue
	}

	return sanitized, nil
}

// ValidateLabelKey validates a label key
func ValidateLabelKey(key string) error {
	if key == "" {
		return &LabelValidationError{
			Label:  key,
			Reason: "empty label key",
		}
	}

	if len(key) > MaxLabelKeyLength {
		return &LabelValidationError{
			Label:  key,
			Reason: fmt.Sprintf("key exceeds maximum length %d", MaxLabelKeyLength),
		}
	}

	// Check reserved labels
	if ReservedLabels[key] {
		return &LabelValidationError{
			Label:  key,
			Reason: "reserved system label",
		}
	}

	// Validate format: must start with letter or underscore
	if key[0] >= '0' && key[0] <= '9' {
		return &LabelValidationError{
			Label:  key,
			Reason: "label key cannot start with a digit",
		}
	}

	// Check for valid characters
	for i, char := range key {
		if !((char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_') {
			return &LabelValidationError{
				Label:  key,
				Reason: fmt.Sprintf("invalid character at position %d: must be alphanumeric or underscore", i),
			}
		}
	}

	return nil
}

// ValidateLabelValue validates a label value
func ValidateLabelValue(key, value string) error {
	if len(value) > MaxLabelValueLength {
		return &LabelValidationError{
			Label:  key,
			Reason: fmt.Sprintf("value exceeds maximum length %d", MaxLabelValueLength),
			Value:  value[:50] + "...",
		}
	}

	// Check for null bytes and control characters
	for i, char := range value {
		if char == 0 || (char < 32 && char != '\t' && char != '\n' && char != '\r') {
			return &LabelValidationError{
				Label:  key,
				Reason: fmt.Sprintf("contains invalid control character at position %d", i),
				Value:  value,
			}
		}
	}

	return nil
}

// SanitizeLabelKey sanitizes a label key for safe use
func SanitizeLabelKey(key string) string {
	if key == "" {
		return "unknown"
	}

	// Ensure it starts with letter or underscore
	if key[0] >= '0' && key[0] <= '9' {
		key = "_" + key
	}

	// Replace invalid characters with underscores
	sanitized := ""
	for _, char := range key {
		if (char >= 'a' && char <= 'z') ||
			(char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') ||
			char == '_' {
			sanitized += string(char)
		} else {
			sanitized += "_"
		}
	}

	// Truncate if too long
	if len(sanitized) > MaxLabelKeyLength {
		sanitized = sanitized[:MaxLabelKeyLength]
	}

	return sanitized
}

// SanitizeLabelValue sanitizes a label value for safe use
func SanitizeLabelValue(value string) string {
	// Remove null bytes and control characters
	sanitized := ""
	for _, char := range value {
		if char >= 32 || char == '\t' || char == '\n' || char == '\r' {
			sanitized += string(char)
		}
	}

	// Truncate if too long
	if len(sanitized) > MaxLabelValueLength {
		sanitized = sanitized[:MaxLabelValueLength]
	}

	return sanitized
}

// CopyLabels creates a deep copy of labels map
func CopyLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return make(map[string]string)
	}

	copied := make(map[string]string, len(labels))
	for k, v := range labels {
		copied[k] = v
	}
	return copied
}

// FilterReservedLabels removes reserved labels from a tag map
func FilterReservedLabels(tags map[string]string) map[string]string {
	filtered := make(map[string]string)
	for k, v := range tags {
		if !ReservedLabels[k] {
			filtered[k] = v
		}
	}
	return filtered
}

// LabelCardinality tracks label cardinality to prevent metric explosion
type LabelCardinality struct {
	mu             sync.RWMutex
	combinations   map[string]int
	maxCardinality int
}

// NewLabelCardinality creates a new label cardinality tracker
func NewLabelCardinality(maxCardinality int) *LabelCardinality {
	if maxCardinality <= 0 {
		maxCardinality = MaxLabelCardinality
	}
	return &LabelCardinality{
		combinations:   make(map[string]int),
		maxCardinality: maxCardinality,
	}
}

// Check checks if adding this label combination would exceed cardinality limits
func (lc *LabelCardinality) Check(metricName string, labels map[string]string) bool {
	lc.mu.RLock()
	defer lc.mu.RUnlock()

	key := lc.buildKey(metricName, labels)
	return len(lc.combinations) < lc.maxCardinality || lc.combinations[key] > 0
}

// Record records a label combination
func (lc *LabelCardinality) Record(metricName string, labels map[string]string) error {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	key := lc.buildKey(metricName, labels)

	if lc.combinations[key] == 0 && len(lc.combinations) >= lc.maxCardinality {
		return fmt.Errorf("label cardinality limit exceeded: %d combinations", lc.maxCardinality)
	}

	lc.combinations[key]++
	return nil
}

// GetCardinality returns current cardinality count
func (lc *LabelCardinality) GetCardinality() int {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return len(lc.combinations)
}

// Reset resets the cardinality tracker
func (lc *LabelCardinality) Reset() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.combinations = make(map[string]int)
}

// buildKey builds a unique key for metric name and labels
func (lc *LabelCardinality) buildKey(metricName string, labels map[string]string) string {
	// Sort labels for consistent key generation
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString(metricName)
	buf.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(k)
		buf.WriteString("=")
		buf.WriteString(labels[k])
	}
	buf.WriteString("}")

	return buf.String()
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
	labels map[string]string
	value  int64
}

// NewCounter creates a new counter
func NewCounter() Counter {
	return &counterImpl{
		labels: make(map[string]string),
	}
}

// NewCounterWithLabels creates a new counter with labels
func NewCounterWithLabels(labels map[string]string) Counter {
	return &counterImpl{
		labels: CopyLabels(labels),
	}
}

func (c *counterImpl) WithLabels(labels map[string]string) Counter {
	// Create new counter with copied labels and current value
	newCounter := &counterImpl{
		labels: CopyLabels(labels),
		value:  atomic.LoadInt64(&c.value),
	}
	return newCounter
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

func (c *counterImpl) Reset() error {
	atomic.StoreInt64(&c.value, 0)
	return nil
}

func (c *counterImpl) GetLabels() map[string]string {
	return CopyLabels(c.labels)
}

// gaugeImpl implements Gauge interface
type gaugeImpl struct {
	value  int64 // Store as bits for atomic operations
	labels map[string]string
}

// NewGauge creates a new gauge
func NewGauge() Gauge {
	return &gaugeImpl{
		labels: make(map[string]string),
	}
}

// NewGaugeWithLabels creates a new gauge with labels
func NewGaugeWithLabels(labels map[string]string) Gauge {
	return &gaugeImpl{
		labels: CopyLabels(labels),
	}
}

func (g *gaugeImpl) WithLabels(labels map[string]string) Gauge {
	// Create new gauge with copied labels and current value
	newGauge := &gaugeImpl{
		labels: CopyLabels(labels),
		value:  atomic.LoadInt64(&g.value),
	}
	return newGauge
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

func (g *gaugeImpl) Reset() error {
	atomic.StoreInt64(&g.value, 0)
	return nil
}

func (g *gaugeImpl) GetLabels() map[string]string {
	return CopyLabels(g.labels)
}

// histogramImpl implements Histogram interface
type histogramImpl struct {
	labels  map[string]string
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
		labels:  make(map[string]string),
		buckets: bucketMap,
		values:  make([]float64, 0),
	}
}

// NewHistogramWithLabels creates a new histogram with labels
func NewHistogramWithLabels(labels map[string]string, buckets []float64) Histogram {
	if buckets == nil {
		buckets = DefaultBuckets
	}

	bucketMap := make(map[float64]uint64)
	for _, bucket := range buckets {
		bucketMap[bucket] = 0
	}

	return &histogramImpl{
		labels:  CopyLabels(labels),
		buckets: bucketMap,
		values:  make([]float64, 0),
	}
}

// DefaultBuckets provides default histogram buckets
// Values in seconds for latency measurements
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

func (h *histogramImpl) ObserveDuration(start time.Time) {
	h.Observe(time.Since(start).Seconds())
}

func (h *histogramImpl) WithLabels(labels map[string]string) Histogram {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Create new histogram with copied labels and bucket configuration
	// but fresh state (don't copy values/counts)
	bucketsCopy := make(map[float64]uint64)
	for bucket := range h.buckets {
		bucketsCopy[bucket] = 0
	}

	return &histogramImpl{
		labels:  CopyLabels(labels),
		buckets: bucketsCopy,
		values:  make([]float64, 0),
	}
}

func (h *histogramImpl) GetLabels() map[string]string {
	return CopyLabels(h.labels)
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

func (h *histogramImpl) Reset() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count = 0
	h.sum = 0
	h.values = h.values[:0]

	for bucket := range h.buckets {
		h.buckets[bucket] = 0
	}

	return nil
}

// timerImpl implements Timer interface
type timerImpl struct {
	mu        sync.RWMutex
	labels    map[string]string
	durations []time.Duration
	count     uint64
	sum       time.Duration
	min       time.Duration
	max       time.Duration
}

// NewTimer creates a new timer
func NewTimer() Timer {
	return &timerImpl{
		labels:    make(map[string]string),
		durations: make([]time.Duration, 0),
		min:       time.Duration(math.MaxInt64),
		max:       time.Duration(0),
	}
}

// NewTimerWithLabels creates a new timer with labels
func NewTimerWithLabels(labels map[string]string) Timer {
	return &timerImpl{
		labels:    CopyLabels(labels),
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

func (t *timerImpl) Get() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sum
}

func (t *timerImpl) GetDurations() []time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.durations
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

func (t *timerImpl) GetLabels() map[string]string {
	return CopyLabels(t.labels)
}

// =============================================================================
// PRODUCTION LABEL UTILITIES
// =============================================================================

// LabelMetadata tracks metadata about labels for production observability
type LabelMetadata struct {
	Key               string    `json:"key"`
	ValueCount        int       `json:"value_count"`         // Number of unique values seen
	LastSeen          time.Time `json:"last_seen"`           // Last time this label was used
	IsHighCardinality bool      `json:"is_high_cardinality"` // Flag if cardinality > threshold
	SampleValues      []string  `json:"sample_values"`       // Sample of values for debugging
}

// LabelRegistry tracks label usage for production monitoring
type LabelRegistry struct {
	mu         sync.RWMutex
	labels     map[string]*LabelMetadata
	maxSamples int
}

// NewLabelRegistry creates a new label registry
func NewLabelRegistry() *LabelRegistry {
	return &LabelRegistry{
		labels:     make(map[string]*LabelMetadata),
		maxSamples: 10, // Keep 10 sample values per label
	}
}

// RecordLabel records usage of a label
func (lr *LabelRegistry) RecordLabel(key, value string) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	meta, exists := lr.labels[key]
	if !exists {
		meta = &LabelMetadata{
			Key:          key,
			SampleValues: make([]string, 0, lr.maxSamples),
		}
		lr.labels[key] = meta
	}

	meta.LastSeen = time.Now()
	meta.ValueCount++

	// Track high cardinality (threshold: 100 unique values)
	if meta.ValueCount > 100 {
		meta.IsHighCardinality = true
	}

	// Add sample value if not already present and room available
	if len(meta.SampleValues) < lr.maxSamples {
		found := false
		for _, sample := range meta.SampleValues {
			if sample == value {
				found = true
				break
			}
		}
		if !found {
			meta.SampleValues = append(meta.SampleValues, value)
		}
	}
}

// GetHighCardinalityLabels returns labels with high cardinality
func (lr *LabelRegistry) GetHighCardinalityLabels() []string {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	var result []string
	for key, meta := range lr.labels {
		if meta.IsHighCardinality {
			result = append(result, key)
		}
	}
	sort.Strings(result)
	return result
}

// GetLabelStats returns statistics about labels
func (lr *LabelRegistry) GetLabelStats() map[string]*LabelMetadata {
	lr.mu.RLock()
	defer lr.mu.RUnlock()

	stats := make(map[string]*LabelMetadata, len(lr.labels))
	for k, v := range lr.labels {
		stats[k] = v
	}
	return stats
}

// =============================================================================
// FORMAT-SPECIFIC LABEL SANITIZATION
// =============================================================================

// SanitizeLabelForPrometheus sanitizes labels for Prometheus format
func SanitizeLabelForPrometheus(key, value string) (string, string) {
	// Prometheus label naming: [a-zA-Z_][a-zA-Z0-9_]*
	sanitizedKey := SanitizeLabelKey(key)

	// Escape quotes in value
	sanitizedValue := value
	if len(value) > MaxLabelValueLength {
		sanitizedValue = value[:MaxLabelValueLength]
	}

	return sanitizedKey, sanitizedValue
}

// SanitizeLabelForInflux sanitizes labels for InfluxDB format
func SanitizeLabelForInflux(key, value string) (string, string) {
	// InfluxDB tags: escape spaces, commas, equals
	sanitizedKey := SanitizeLabelKey(key)
	sanitizedValue := value

	// Escape special characters
	replacer := strings.NewReplacer(
		" ", "\\ ",
		",", "\\,",
		"=", "\\=",
	)
	sanitizedValue = replacer.Replace(sanitizedValue)

	if len(sanitizedValue) > MaxLabelValueLength {
		sanitizedValue = sanitizedValue[:MaxLabelValueLength]
	}

	return sanitizedKey, sanitizedValue
}

// SanitizeLabelForStatsD sanitizes labels for StatsD format
func SanitizeLabelForStatsD(key, value string) (string, string) {
	// StatsD: replace dots, colons, pipes with underscores
	sanitizedKey := SanitizeLabelKey(key)

	replacer := strings.NewReplacer(
		":", "_",
		"|", "_",
		"@", "_",
		".", "_",
	)
	sanitizedValue := replacer.Replace(value)

	if len(sanitizedValue) > MaxLabelValueLength {
		sanitizedValue = sanitizedValue[:MaxLabelValueLength]
	}

	return sanitizedKey, sanitizedValue
}

// FormatLabelsPrometheus formats labels for Prometheus export
func FormatLabelsPrometheus(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString("{")
	first := true
	for _, k := range keys {
		key, value := SanitizeLabelForPrometheus(k, labels[k])
		if !first {
			buf.WriteString(",")
		}
		// Escape quotes in value
		escapedValue := strings.ReplaceAll(value, "\"", "\\\"")
		buf.WriteString(fmt.Sprintf("%s=\"%s\"", key, escapedValue))
		first = false
	}
	buf.WriteString("}")

	return buf.String()
}

// FormatLabelsInflux formats labels for InfluxDB export
func FormatLabelsInflux(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	first := true
	for _, k := range keys {
		key, value := SanitizeLabelForInflux(k, labels[k])
		if !first {
			buf.WriteString(",")
		}
		buf.WriteString(fmt.Sprintf("%s=%s", key, value))
		first = false
	}

	return buf.String()
}

// FormatLabelsJSON formats labels for JSON export (no special formatting needed)
func FormatLabelsJSON(labels map[string]string) map[string]string {
	sanitized := make(map[string]string, len(labels))
	for k, v := range labels {
		sanitized[SanitizeLabelKey(k)] = SanitizeLabelValue(v)
	}
	return sanitized
}

// =============================================================================
// COMMON LABEL PATTERNS FOR PRODUCTION
// =============================================================================

// CommonLabels provides standard labels for production metrics
type CommonLabels struct {
	Service     string
	Environment string
	Version     string
	Instance    string
	Region      string
	Zone        string
}

// ToMap converts CommonLabels to map
func (cl *CommonLabels) ToMap() map[string]string {
	labels := make(map[string]string)

	if cl.Service != "" {
		labels["service"] = cl.Service
	}
	if cl.Environment != "" {
		labels["environment"] = cl.Environment
	}
	if cl.Version != "" {
		labels["version"] = cl.Version
	}
	if cl.Instance != "" {
		labels["instance"] = cl.Instance
	}
	if cl.Region != "" {
		labels["region"] = cl.Region
	}
	if cl.Zone != "" {
		labels["zone"] = cl.Zone
	}

	return labels
}

// AddCommonLabels adds common production labels to existing labels
func AddCommonLabels(labels map[string]string, common *CommonLabels) map[string]string {
	if common == nil {
		return labels
	}
	return MergeTags(labels, common.ToMap())
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// Helper functions
func formatLabels(labels map[string]string) string {
	return FormatLabelsPrometheus(labels)
}

func mergeMaps(m1, m2 map[string]string) map[string]string {
	result := make(map[string]string, len(m1)+len(m2))
	for k, v := range m1 {
		result[k] = v
	}
	for k, v := range m2 {
		result[k] = v
	}
	return result
}

// parseLabels parses variadic label parameters into a map
// Labels should be provided as key-value pairs: "key1", "value1", "key2", "value2"
func parseLabels(labels []string) map[string]string {
	if len(labels) == 0 {
		return make(map[string]string)
	}

	result := make(map[string]string, len(labels)/2)
	for i := 0; i < len(labels)-1; i += 2 {
		result[labels[i]] = labels[i+1]
	}
	return result
}

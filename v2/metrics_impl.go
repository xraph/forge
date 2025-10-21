package forge

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// simpleMetrics implements Metrics interface
type simpleMetrics struct {
	namespace  string
	counters   sync.Map // name -> *simpleCounter
	gauges     sync.Map // name -> *simpleGauge
	histograms sync.Map // name -> *simpleHistogram
}

// NewMetrics creates a new metrics instance
func NewMetrics(namespace string) Metrics {
	return &simpleMetrics{
		namespace: namespace,
	}
}

// Counter returns a counter metric with optional labels
func (m *simpleMetrics) Counter(name string, labels ...string) Counter {
	key := m.namespace + "_" + name
	labelMap := parseLabels(labels)
	labelKey := key + formatLabels(labelMap)

	if v, ok := m.counters.Load(labelKey); ok {
		return v.(*simpleCounter)
	}

	counter := &simpleCounter{
		name:   key,
		labels: labelMap,
	}
	m.counters.Store(labelKey, counter)
	return counter
}

// Gauge returns a gauge metric with optional labels
func (m *simpleMetrics) Gauge(name string, labels ...string) Gauge {
	key := m.namespace + "_" + name
	labelMap := parseLabels(labels)
	labelKey := key + formatLabels(labelMap)

	if v, ok := m.gauges.Load(labelKey); ok {
		return v.(*simpleGauge)
	}

	gauge := &simpleGauge{
		name:   key,
		labels: labelMap,
	}
	m.gauges.Store(labelKey, gauge)
	return gauge
}

// Histogram returns a histogram metric with optional labels
func (m *simpleMetrics) Histogram(name string, labels ...string) Histogram {
	key := m.namespace + "_" + name
	labelMap := parseLabels(labels)
	labelKey := key + formatLabels(labelMap)

	if v, ok := m.histograms.Load(labelKey); ok {
		return v.(*simpleHistogram)
	}

	histogram := &simpleHistogram{
		name:    key,
		labels:  labelMap,
		buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		counts:  make([]uint64, 12), // buckets + +Inf
	}
	m.histograms.Store(labelKey, histogram)
	return histogram
}

// Export exports metrics in Prometheus format
func (m *simpleMetrics) Export() ([]byte, error) {
	var buf bytes.Buffer

	// Export counters
	m.counters.Range(func(key, value interface{}) bool {
		counter := value.(*simpleCounter)
		buf.WriteString(counter.export())
		return true
	})

	// Export gauges
	m.gauges.Range(func(key, value interface{}) bool {
		gauge := value.(*simpleGauge)
		buf.WriteString(gauge.export())
		return true
	})

	// Export histograms
	m.histograms.Range(func(key, value interface{}) bool {
		histogram := value.(*simpleHistogram)
		buf.WriteString(histogram.export())
		return true
	})

	return buf.Bytes(), nil
}

// simpleCounter implements Counter
type simpleCounter struct {
	name   string
	value  uint64
	labels map[string]string
	mu     sync.RWMutex
}

func (c *simpleCounter) Inc() {
	atomic.AddUint64(&c.value, 1)
}

func (c *simpleCounter) Add(delta float64) {
	atomic.AddUint64(&c.value, uint64(delta))
}

func (c *simpleCounter) WithLabels(labels map[string]string) Counter {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Create new counter with merged labels
	newCounter := &simpleCounter{
		name:   c.name,
		labels: mergeMaps(c.labels, labels),
	}

	// Note: In a production implementation, you'd want to register this
	// labeled counter back to the metrics store. For simplicity, we return
	// a standalone counter that tracks its own value.
	return newCounter
}

func (c *simpleCounter) export() string {
	value := atomic.LoadUint64(&c.value)
	return fmt.Sprintf("# TYPE %s counter\n%s%s %d\n",
		c.name, c.name, formatLabels(c.labels), value)
}

// simpleGauge implements Gauge
type simpleGauge struct {
	name   string
	value  int64 // Use int64 for atomic operations
	labels map[string]string
	mu     sync.RWMutex
}

func (g *simpleGauge) Set(value float64) {
	atomic.StoreInt64(&g.value, int64(value))
}

func (g *simpleGauge) Inc() {
	atomic.AddInt64(&g.value, 1)
}

func (g *simpleGauge) Dec() {
	atomic.AddInt64(&g.value, -1)
}

func (g *simpleGauge) Add(delta float64) {
	atomic.AddInt64(&g.value, int64(delta))
}

func (g *simpleGauge) WithLabels(labels map[string]string) Gauge {
	g.mu.Lock()
	defer g.mu.Unlock()

	newGauge := &simpleGauge{
		name:   g.name,
		labels: mergeMaps(g.labels, labels),
	}
	return newGauge
}

func (g *simpleGauge) export() string {
	value := atomic.LoadInt64(&g.value)
	return fmt.Sprintf("# TYPE %s gauge\n%s%s %d\n",
		g.name, g.name, formatLabels(g.labels), value)
}

// simpleHistogram implements Histogram
type simpleHistogram struct {
	name    string
	labels  map[string]string
	buckets []float64
	counts  []uint64
	sum     uint64
	count   uint64
	mu      sync.RWMutex
}

func (h *simpleHistogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Update sum and count
	atomic.AddUint64(&h.sum, uint64(value*1000)) // Store as milliseconds
	atomic.AddUint64(&h.count, 1)

	// Update buckets
	for i, bucket := range h.buckets {
		if value <= bucket {
			atomic.AddUint64(&h.counts[i], 1)
		}
	}
	// +Inf bucket
	atomic.AddUint64(&h.counts[len(h.buckets)], 1)
}

func (h *simpleHistogram) ObserveDuration(start time.Time) {
	duration := time.Since(start).Seconds()
	h.Observe(duration)
}

func (h *simpleHistogram) WithLabels(labels map[string]string) Histogram {
	h.mu.Lock()
	defer h.mu.Unlock()

	newHistogram := &simpleHistogram{
		name:    h.name,
		labels:  mergeMaps(h.labels, labels),
		buckets: h.buckets,
		counts:  make([]uint64, len(h.counts)),
	}
	return newHistogram
}

func (h *simpleHistogram) export() string {
	var buf bytes.Buffer

	labelStr := formatLabels(h.labels)

	buf.WriteString(fmt.Sprintf("# TYPE %s histogram\n", h.name))

	// Export buckets
	for i, bucket := range h.buckets {
		count := atomic.LoadUint64(&h.counts[i])
		buf.WriteString(fmt.Sprintf("%s_bucket{le=\"%.3f\"%s} %d\n",
			h.name, bucket, labelStr, count))
	}

	// +Inf bucket
	infCount := atomic.LoadUint64(&h.counts[len(h.buckets)])
	buf.WriteString(fmt.Sprintf("%s_bucket{le=\"+Inf\"%s} %d\n",
		h.name, labelStr, infCount))

	// Sum and count
	sum := atomic.LoadUint64(&h.sum)
	count := atomic.LoadUint64(&h.count)
	buf.WriteString(fmt.Sprintf("%s_sum%s %.3f\n", h.name, labelStr, float64(sum)/1000))
	buf.WriteString(fmt.Sprintf("%s_count%s %d\n", h.name, labelStr, count))

	return buf.String()
}

// Helper functions
func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}

	// Sort keys for consistent output
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	buf.WriteString("{")
	first := true
	for _, k := range keys {
		if !first {
			buf.WriteString(",")
		}
		buf.WriteString(fmt.Sprintf("%s=\"%s\"", k, labels[k]))
		first = false
	}
	buf.WriteString("}")

	return buf.String()
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

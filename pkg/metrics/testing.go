package metrics

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "github.com/json-iterator/go"
	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/metrics/exporters"
)

// =============================================================================
// MOCK IMPLEMENTATIONS
// =============================================================================

// MockMetricsCollector implements MetricsCollector interface for testing
type MockMetricsCollector struct {
	mu                sync.RWMutex
	counters          map[string]*MockCounter
	gauges            map[string]*MockGauge
	histograms        map[string]*MockHistogram
	timers            map[string]*MockTimer
	customCollectors  map[string]CustomCollector
	exportFormats     map[ExportFormat][]byte
	started           bool
	healthCheckError  error
	exportError       error
	resetCalled       bool
	resetMetricCalled map[string]bool
}

// NewMockMetricsCollector creates a new mock metrics collector
func NewMockMetricsCollector() *MockMetricsCollector {
	return &MockMetricsCollector{
		counters:          make(map[string]*MockCounter),
		gauges:            make(map[string]*MockGauge),
		histograms:        make(map[string]*MockHistogram),
		timers:            make(map[string]*MockTimer),
		customCollectors:  make(map[string]CustomCollector),
		exportFormats:     make(map[ExportFormat][]byte),
		resetMetricCalled: make(map[string]bool),
	}
}

// Service lifecycle methods
func (m *MockMetricsCollector) Name() string {
	return "mock-metrics-collector"
}

func (m *MockMetricsCollector) Dependencies() []string {
	return []string{}
}

func (m *MockMetricsCollector) OnStart(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

func (m *MockMetricsCollector) OnStop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

func (m *MockMetricsCollector) OnHealthCheck(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.healthCheckError
}

// Mock metric creation methods
func (m *MockMetricsCollector) Counter(name string, tags ...string) Counter {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := buildTestKey(name, tags)
	if counter, exists := m.counters[key]; exists {
		return counter
	}

	counter := NewMockCounter(name, ParseTags(tags...))
	m.counters[key] = counter
	return counter
}

func (m *MockMetricsCollector) Gauge(name string, tags ...string) Gauge {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := buildTestKey(name, tags)
	if gauge, exists := m.gauges[key]; exists {
		return gauge
	}

	gauge := NewMockGauge(name, ParseTags(tags...))
	m.gauges[key] = gauge
	return gauge
}

func (m *MockMetricsCollector) Histogram(name string, tags ...string) Histogram {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := buildTestKey(name, tags)
	if histogram, exists := m.histograms[key]; exists {
		return histogram
	}

	histogram := NewMockHistogram(name, ParseTags(tags...))
	m.histograms[key] = histogram
	return histogram
}

func (m *MockMetricsCollector) Timer(name string, tags ...string) Timer {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := buildTestKey(name, tags)
	if timer, exists := m.timers[key]; exists {
		return timer
	}

	timer := NewMockTimer(name, ParseTags(tags...))
	m.timers[key] = timer
	return timer
}

// Custom collector management
func (m *MockMetricsCollector) RegisterCollector(collector CustomCollector) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := collector.Name()
	if _, exists := m.customCollectors[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	m.customCollectors[name] = collector
	return nil
}

func (m *MockMetricsCollector) UnregisterCollector(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.customCollectors[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(m.customCollectors, name)
	return nil
}

func (m *MockMetricsCollector) GetCollectors() []CustomCollector {
	m.mu.RLock()
	defer m.mu.RUnlock()

	collectors := make([]CustomCollector, 0, len(m.customCollectors))
	for _, collector := range m.customCollectors {
		collectors = append(collectors, collector)
	}
	return collectors
}

// Metrics retrieval
func (m *MockMetricsCollector) GetMetrics() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make(map[string]interface{})

	// Add counters
	for key, counter := range m.counters {
		metrics[key] = counter.Get()
	}

	// Add gauges
	for key, gauge := range m.gauges {
		metrics[key] = gauge.Get()
	}

	// Add histograms
	for key, histogram := range m.histograms {
		metrics[key] = map[string]interface{}{
			"count": histogram.GetCount(),
			"sum":   histogram.GetSum(),
			"mean":  histogram.GetMean(),
		}
	}

	// Add timers
	for key, timer := range m.timers {
		metrics[key] = map[string]interface{}{
			"count": timer.GetCount(),
			"mean":  timer.GetMean(),
		}
	}

	// Add custom collector metrics
	for _, collector := range m.customCollectors {
		for k, v := range collector.Collect() {
			metrics[k] = v
		}
	}

	return metrics
}

func (m *MockMetricsCollector) GetMetricsByType(metricType MetricType) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	metrics := make(map[string]interface{})

	switch metricType {
	case MetricTypeCounter:
		for key, counter := range m.counters {
			metrics[key] = counter.Get()
		}
	case MetricTypeGauge:
		for key, gauge := range m.gauges {
			metrics[key] = gauge.Get()
		}
	case MetricTypeHistogram:
		for key, histogram := range m.histograms {
			metrics[key] = map[string]interface{}{
				"count": histogram.GetCount(),
				"sum":   histogram.GetSum(),
				"mean":  histogram.GetMean(),
			}
		}
	case MetricTypeTimer:
		for key, timer := range m.timers {
			metrics[key] = map[string]interface{}{
				"count": timer.GetCount(),
				"mean":  timer.GetMean(),
			}
		}
	}

	return metrics
}

func (m *MockMetricsCollector) GetMetricsByTag(tagKey, tagValue string) map[string]interface{} {
	// Simplified implementation for testing
	return m.GetMetrics()
}

// Export functionality
func (m *MockMetricsCollector) Export(format ExportFormat) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.exportError != nil {
		return nil, m.exportError
	}

	if data, exists := m.exportFormats[format]; exists {
		return data, nil
	}

	// Default export
	return []byte(fmt.Sprintf("# %s format export\n", format)), nil
}

func (m *MockMetricsCollector) ExportToFile(format ExportFormat, filename string) error {
	_, err := m.Export(format)
	return err
}

// Management
func (m *MockMetricsCollector) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.resetCalled = true

	// Reset all metrics
	for _, counter := range m.counters {
		counter.Reset()
	}
	for _, gauge := range m.gauges {
		gauge.Reset()
	}
	for _, histogram := range m.histograms {
		histogram.Reset()
	}
	for _, timer := range m.timers {
		timer.Reset()
	}

	return nil
}

func (m *MockMetricsCollector) ResetMetric(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.resetMetricCalled[name] = true
	return nil
}

// Statistics
func (m *MockMetricsCollector) GetStats() CollectorStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return CollectorStats{
		Name:             m.Name(),
		Started:          m.started,
		StartTime:        time.Now(),
		Uptime:           time.Hour,
		MetricsCreated:   int64(len(m.counters) + len(m.gauges) + len(m.histograms) + len(m.timers)),
		MetricsCollected: 100,
		CustomCollectors: len(m.customCollectors),
		ActiveMetrics:    len(m.counters) + len(m.gauges) + len(m.histograms) + len(m.timers),
	}
}

// Test helper methods
func (m *MockMetricsCollector) SetHealthCheckError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.healthCheckError = err
}

func (m *MockMetricsCollector) SetExportError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exportError = err
}

func (m *MockMetricsCollector) SetExportData(format ExportFormat, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.exportFormats[format] = data
}

func (m *MockMetricsCollector) WasResetCalled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resetCalled
}

func (m *MockMetricsCollector) WasResetMetricCalled(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.resetMetricCalled[name]
}

// =============================================================================
// MOCK METRIC IMPLEMENTATIONS
// =============================================================================

// MockCounter implements Counter interface
type MockCounter struct {
	name        string
	tags        map[string]string
	value       float64
	incCalled   int
	decCalled   int
	addCalled   int
	resetCalled bool
	mu          sync.RWMutex
}

func (c *MockCounter) Dec() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value--
	c.decCalled++
}

func NewMockCounter(name string, tags map[string]string) *MockCounter {
	return &MockCounter{
		name: name,
		tags: tags,
	}
}

func (c *MockCounter) Inc() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value++
	c.incCalled++
}

func (c *MockCounter) Add(value float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value += value
	c.addCalled++
}

func (c *MockCounter) Get() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.value
}

func (c *MockCounter) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.value = 0
	c.resetCalled = true
}

func (c *MockCounter) Name() string            { return c.name }
func (c *MockCounter) Tags() map[string]string { return c.tags }
func (c *MockCounter) IncCallCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.incCalled
}
func (c *MockCounter) AddCallCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.addCalled
}
func (c *MockCounter) WasResetCalled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resetCalled
}

// MockGauge implements Gauge interface
type MockGauge struct {
	name        string
	tags        map[string]string
	value       float64
	setCalled   int
	resetCalled bool
	mu          sync.RWMutex
}

func NewMockGauge(name string, tags map[string]string) *MockGauge {
	return &MockGauge{
		name: name,
		tags: tags,
	}
}

func (g *MockGauge) Set(value float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = value
	g.setCalled++
}

func (g *MockGauge) Inc() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value++
}

func (g *MockGauge) Dec() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value--
}

func (g *MockGauge) Add(value float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value += value
}

func (g *MockGauge) Get() float64 {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.value
}

func (g *MockGauge) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.value = 0
	g.resetCalled = true
}

func (g *MockGauge) Name() string            { return g.name }
func (g *MockGauge) Tags() map[string]string { return g.tags }
func (g *MockGauge) SetCallCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.setCalled
}
func (g *MockGauge) WasResetCalled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.resetCalled
}

// MockHistogram implements Histogram interface
type MockHistogram struct {
	name          string
	tags          map[string]string
	count         uint64
	sum           float64
	observeCalled int
	resetCalled   bool
	mu            sync.RWMutex
}

func NewMockHistogram(name string, tags map[string]string) *MockHistogram {
	return &MockHistogram{
		name: name,
		tags: tags,
	}
}

func (h *MockHistogram) Observe(value float64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
	h.sum += value
	h.observeCalled++
}

func (h *MockHistogram) GetBuckets() map[float64]uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return map[float64]uint64{
		1:   h.count / 4,
		10:  h.count / 2,
		100: h.count,
	}
}

func (h *MockHistogram) GetCount() uint64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}

func (h *MockHistogram) GetSum() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sum
}

func (h *MockHistogram) GetMean() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.count == 0 {
		return 0
	}
	return h.sum / float64(h.count)
}

func (h *MockHistogram) GetPercentile(percentile float64) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	// Simple mock implementation
	return h.GetMean() * (percentile / 100)
}

func (h *MockHistogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count = 0
	h.sum = 0
	h.resetCalled = true
}

func (h *MockHistogram) Name() string            { return h.name }
func (h *MockHistogram) Tags() map[string]string { return h.tags }
func (h *MockHistogram) ObserveCallCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.observeCalled
}
func (h *MockHistogram) WasResetCalled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.resetCalled
}

// MockTimer implements Timer interface
type MockTimer struct {
	name          string
	tags          map[string]string
	count         uint64
	totalDuration time.Duration
	recordCalled  int
	resetCalled   bool
	mu            sync.RWMutex
}

func NewMockTimer(name string, tags map[string]string) *MockTimer {
	return &MockTimer{
		name: name,
		tags: tags,
	}
}

func (t *MockTimer) Record(duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count++
	t.totalDuration += duration
	t.recordCalled++
}

func (t *MockTimer) Time() func() {
	start := time.Now()
	return func() {
		t.Record(time.Since(start))
	}
}

func (t *MockTimer) GetCount() uint64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.count
}

func (t *MockTimer) GetMean() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.count == 0 {
		return 0
	}
	return t.totalDuration / time.Duration(t.count)
}

func (t *MockTimer) GetPercentile(percentile float64) time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	// Simple mock implementation
	return time.Duration(float64(t.GetMean()) * (percentile / 100))
}

func (t *MockTimer) GetMin() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.GetMean() / 2
}

func (t *MockTimer) GetMax() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.GetMean() * 2
}

func (t *MockTimer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.count = 0
	t.totalDuration = 0
	t.resetCalled = true
}

func (t *MockTimer) Name() string            { return t.name }
func (t *MockTimer) Tags() map[string]string { return t.tags }
func (t *MockTimer) RecordCallCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.recordCalled
}
func (t *MockTimer) WasResetCalled() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.resetCalled
}

// =============================================================================
// MOCK CUSTOM COLLECTOR
// =============================================================================

// MockCustomCollector implements CustomCollector interface
type MockCustomCollector struct {
	name          string
	metrics       map[string]interface{}
	collectCalled int
	resetCalled   bool
	mu            sync.RWMutex
}

func NewMockCustomCollector(name string) *MockCustomCollector {
	return &MockCustomCollector{
		name:    name,
		metrics: make(map[string]interface{}),
	}
}

func (c *MockCustomCollector) Name() string {
	return c.name
}

func (c *MockCustomCollector) Collect() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.collectCalled++

	result := make(map[string]interface{})
	for k, v := range c.metrics {
		result[k] = v
	}
	return result
}

func (c *MockCustomCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = make(map[string]interface{})
	c.resetCalled = true
}

func (c *MockCustomCollector) SetMetric(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics[key] = value
}

func (c *MockCustomCollector) CollectCallCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.collectCalled
}

func (c *MockCustomCollector) WasResetCalled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.resetCalled
}

// =============================================================================
// MOCK EXPORTER
// =============================================================================

// MockExporter implements Exporter interface
type MockExporter struct {
	format       string
	exportData   []byte
	exportError  error
	exportCalled int
	mu           sync.RWMutex
}

func NewMockExporter(format string) *MockExporter {
	return &MockExporter{
		format: format,
	}
}

func (e *MockExporter) Export(metrics map[string]interface{}) ([]byte, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exportCalled++

	if e.exportError != nil {
		return nil, e.exportError
	}

	if e.exportData != nil {
		return e.exportData, nil
	}

	return []byte(fmt.Sprintf("# %s export\n", e.format)), nil
}

func (e *MockExporter) Format() string {
	return e.format
}

func (e *MockExporter) Stats() interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return map[string]interface{}{
		"export_called": e.exportCalled,
		"format":        e.format,
	}
}

func (e *MockExporter) SetExportData(data []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exportData = data
}

func (e *MockExporter) SetExportError(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.exportError = err
}

func (e *MockExporter) ExportCallCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.exportCalled
}

// =============================================================================
// TEST UTILITIES
// =============================================================================

// MetricsTestFixture provides a complete test environment for metrics
type MetricsTestFixture struct {
	Collector *MockMetricsCollector
	Registry  Registry
	Exporters map[ExportFormat]*MockExporter
	Logger    logger.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewMetricsTestFixture creates a new test fixture
func NewMetricsTestFixture() *MetricsTestFixture {
	ctx, cancel := context.WithCancel(context.Background())

	return &MetricsTestFixture{
		Collector: NewMockMetricsCollector(),
		Registry:  NewRegistry(),
		Exporters: map[ExportFormat]*MockExporter{
			ExportFormatPrometheus: NewMockExporter("prometheus"),
			ExportFormatJSON:       NewMockExporter("json"),
			ExportFormatInflux:     NewMockExporter("influx"),
			ExportFormatStatsD:     NewMockExporter("statsd"),
		},
		Logger: logger.NewNoopLogger(),
		ctx:    ctx,
		cancel: cancel,
	}
}

// Cleanup cleans up the test fixture
func (f *MetricsTestFixture) Cleanup() {
	f.cancel()
	f.Collector.OnStop(f.ctx)
	f.Registry.Stop()
}

// Context returns the test context
func (f *MetricsTestFixture) Context() context.Context {
	return f.ctx
}

// StartCollector starts the mock collector
func (f *MetricsTestFixture) StartCollector() error {
	return f.Collector.OnStart(f.ctx)
}

// CreateTestMetrics creates a set of test metrics
func (f *MetricsTestFixture) CreateTestMetrics() {
	// Create test counters
	counter1 := f.Collector.Counter("test_counter_1", "service", "test", "env", "dev")
	counter1.Inc()
	counter1.Add(5)

	counter2 := f.Collector.Counter("test_counter_2", "service", "test", "env", "prod")
	counter2.Inc()

	// Create test gauges
	gauge1 := f.Collector.Gauge("test_gauge_1", "service", "test")
	gauge1.Set(42.5)

	gauge2 := f.Collector.Gauge("test_gauge_2", "service", "test")
	gauge2.Set(100)
	gauge2.Inc()

	// Create test histograms
	histogram1 := f.Collector.Histogram("test_histogram_1", "service", "test")
	histogram1.Observe(0.1)
	histogram1.Observe(0.5)
	histogram1.Observe(1.2)

	// Create test timers
	timer1 := f.Collector.Timer("test_timer_1", "service", "test")
	timer1.Record(100 * time.Millisecond)
	timer1.Record(250 * time.Millisecond)
	timer1.Record(500 * time.Millisecond)
}

// =============================================================================
// TEST DATA GENERATORS
// =============================================================================

// MetricsDataGenerator generates test metrics data
type MetricsDataGenerator struct {
	rand *rand.Rand
}

// NewMetricsDataGenerator creates a new data generator
func NewMetricsDataGenerator(seed int64) *MetricsDataGenerator {
	return &MetricsDataGenerator{
		rand: rand.New(rand.NewSource(seed)),
	}
}

// GenerateCounterData generates counter test data
func (g *MetricsDataGenerator) GenerateCounterData(count int) []CounterData {
	data := make([]CounterData, count)
	for i := 0; i < count; i++ {
		data[i] = CounterData{
			Name:  fmt.Sprintf("test_counter_%d", i),
			Value: g.rand.Float64() * 1000,
			Tags: map[string]string{
				"service": fmt.Sprintf("service_%d", i%5),
				"env":     []string{"dev", "test", "prod"}[i%3],
			},
		}
	}
	return data
}

// GenerateGaugeData generates gauge test data
func (g *MetricsDataGenerator) GenerateGaugeData(count int) []GaugeData {
	data := make([]GaugeData, count)
	for i := 0; i < count; i++ {
		data[i] = GaugeData{
			Name:  fmt.Sprintf("test_gauge_%d", i),
			Value: g.rand.Float64() * 100,
			Tags: map[string]string{
				"service": fmt.Sprintf("service_%d", i%5),
				"env":     []string{"dev", "test", "prod"}[i%3],
			},
		}
	}
	return data
}

// GenerateHistogramData generates histogram test data
func (g *MetricsDataGenerator) GenerateHistogramData(count int) []HistogramData {
	data := make([]HistogramData, count)
	for i := 0; i < count; i++ {
		values := make([]float64, 10+g.rand.Intn(90))
		for j := range values {
			values[j] = g.rand.Float64() * 10
		}

		data[i] = HistogramData{
			Name:   fmt.Sprintf("test_histogram_%d", i),
			Values: values,
			Tags: map[string]string{
				"service": fmt.Sprintf("service_%d", i%5),
				"env":     []string{"dev", "test", "prod"}[i%3],
			},
		}
	}
	return data
}

// GenerateTimerData generates timer test data
func (g *MetricsDataGenerator) GenerateTimerData(count int) []TimerData {
	data := make([]TimerData, count)
	for i := 0; i < count; i++ {
		durations := make([]time.Duration, 10+g.rand.Intn(90))
		for j := range durations {
			durations[j] = time.Duration(g.rand.Intn(1000)) * time.Millisecond
		}

		data[i] = TimerData{
			Name:      fmt.Sprintf("test_timer_%d", i),
			Durations: durations,
			Tags: map[string]string{
				"service": fmt.Sprintf("service_%d", i%5),
				"env":     []string{"dev", "test", "prod"}[i%3],
			},
		}
	}
	return data
}

// Test data structures
type CounterData struct {
	Name  string
	Value float64
	Tags  map[string]string
}

type GaugeData struct {
	Name  string
	Value float64
	Tags  map[string]string
}

type HistogramData struct {
	Name   string
	Values []float64
	Tags   map[string]string
}

type TimerData struct {
	Name      string
	Durations []time.Duration
	Tags      map[string]string
}

// =============================================================================
// PERFORMANCE TEST UTILITIES
// =============================================================================

// PerformanceTestRunner runs performance tests on metrics
type PerformanceTestRunner struct {
	collector MetricsCollector
	duration  time.Duration
	workers   int
}

// NewPerformanceTestRunner creates a new performance test runner
func NewPerformanceTestRunner(collector MetricsCollector, duration time.Duration, workers int) *PerformanceTestRunner {
	return &PerformanceTestRunner{
		collector: collector,
		duration:  duration,
		workers:   workers,
	}
}

// RunCounterTest runs a performance test on counters
func (r *PerformanceTestRunner) RunCounterTest() PerformanceResult {
	return r.runTest(func(worker int) {
		counter := r.collector.Counter(fmt.Sprintf("perf_counter_%d", worker), "worker", fmt.Sprintf("%d", worker))
		for {
			counter.Inc()
		}
	})
}

// RunGaugeTest runs a performance test on gauges
func (r *PerformanceTestRunner) RunGaugeTest() PerformanceResult {
	return r.runTest(func(worker int) {
		gauge := r.collector.Gauge(fmt.Sprintf("perf_gauge_%d", worker), "worker", fmt.Sprintf("%d", worker))
		value := 0.0
		for {
			gauge.Set(value)
			value++
		}
	})
}

// RunHistogramTest runs a performance test on histograms
func (r *PerformanceTestRunner) RunHistogramTest() PerformanceResult {
	return r.runTest(func(worker int) {
		histogram := r.collector.Histogram(fmt.Sprintf("perf_histogram_%d", worker), "worker", fmt.Sprintf("%d", worker))
		value := 0.0
		for {
			histogram.Observe(value)
			value += 0.1
		}
	})
}

// RunTimerTest runs a performance test on timers
func (r *PerformanceTestRunner) RunTimerTest() PerformanceResult {
	return r.runTest(func(worker int) {
		timer := r.collector.Timer(fmt.Sprintf("perf_timer_%d", worker), "worker", fmt.Sprintf("%d", worker))
		duration := time.Millisecond
		for {
			timer.Record(duration)
			duration += time.Millisecond
		}
	})
}

// runTest runs a performance test with the given worker function
func (r *PerformanceTestRunner) runTest(workerFn func(int)) PerformanceResult {
	var operations int64
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), r.duration)
	defer cancel()

	start := time.Now()

	// OnStart workers
	for i := 0; i < r.workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			workerOps := int64(0)

			for {
				select {
				case <-ctx.Done():
					atomic.AddInt64(&operations, workerOps)
					return
				default:
					workerFn(worker)
					workerOps++
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(start)

	return PerformanceResult{
		Operations:   operations,
		Duration:     elapsed,
		Workers:      r.workers,
		OpsPerSecond: float64(operations) / elapsed.Seconds(),
		OpsPerWorker: float64(operations) / float64(r.workers),
	}
}

// PerformanceResult contains performance test results
type PerformanceResult struct {
	Operations   int64         `json:"operations"`
	Duration     time.Duration `json:"duration"`
	Workers      int           `json:"workers"`
	OpsPerSecond float64       `json:"ops_per_second"`
	OpsPerWorker float64       `json:"ops_per_worker"`
}

// =============================================================================
// INTEGRATION TEST UTILITIES
// =============================================================================

// IntegrationTestEnvironment provides a complete integration test environment
type IntegrationTestEnvironment struct {
	Collector MetricsCollector
	Registry  Registry
	Exporters map[ExportFormat]Exporter
	Logger    logger.Logger
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewIntegrationTestEnvironment creates a new integration test environment
func NewIntegrationTestEnvironment(useRealImplementations bool) *IntegrationTestEnvironment {
	ctx, cancel := context.WithCancel(context.Background())

	var collector MetricsCollector
	var registry Registry
	var exporterz map[ExportFormat]Exporter

	if useRealImplementations {
		// Use real implementations for integration tests
		registry = NewRegistry()
		collector = NewCollector(DefaultCollectorConfig(), logger.NewNoopLogger())
		exporterz = map[ExportFormat]Exporter{
			ExportFormatPrometheus: exporters.NewPrometheusExporter(),
			ExportFormatJSON:       exporters.NewJSONExporter(),
			ExportFormatInflux:     exporters.NewInfluxExporter(),
			ExportFormatStatsD:     exporters.NewStatsDExporter(),
		}
	} else {
		// Use mock implementations for unit tests
		collector = NewMockMetricsCollector()
		registry = NewRegistry()
		exporterz = map[ExportFormat]Exporter{
			ExportFormatPrometheus: NewMockExporter("prometheus"),
			ExportFormatJSON:       NewMockExporter("json"),
			ExportFormatInflux:     NewMockExporter("influx"),
			ExportFormatStatsD:     NewMockExporter("statsd"),
		}
	}

	return &IntegrationTestEnvironment{
		Collector: collector,
		Registry:  registry,
		Exporters: exporterz,
		Logger:    logger.NewNoopLogger(),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Setup sets up the integration test environment
func (e *IntegrationTestEnvironment) Setup() error {
	// OnStart registry
	if err := e.Registry.Start(); err != nil {
		return fmt.Errorf("failed to start registry: %w", err)
	}

	// OnStart collector
	if err := e.Collector.OnStart(e.ctx); err != nil {
		return fmt.Errorf("failed to start collector: %w", err)
	}

	return nil
}

// Cleanup cleans up the integration test environment
func (e *IntegrationTestEnvironment) Cleanup() {
	e.cancel()
	e.Collector.OnStop(e.ctx)
	e.Registry.Stop()
}

// RunFullIntegrationTest runs a complete integration test
func (e *IntegrationTestEnvironment) RunFullIntegrationTest() error {
	// Create various metrics
	counter := e.Collector.Counter("integration_counter", "test", "full")
	gauge := e.Collector.Gauge("integration_gauge", "test", "full")
	histogram := e.Collector.Histogram("integration_histogram", "test", "full")
	timer := e.Collector.Timer("integration_timer", "test", "full")

	// Use metrics
	counter.Inc()
	counter.Add(10)

	gauge.Set(42.5)
	gauge.Inc()

	histogram.Observe(0.1)
	histogram.Observe(0.5)
	histogram.Observe(1.0)

	timer.Record(100 * time.Millisecond)
	timer.Record(200 * time.Millisecond)

	// Test custom collector
	customCollector := NewMockCustomCollector("integration_custom")
	customCollector.SetMetric("custom_metric", 123.45)

	if err := e.Collector.RegisterCollector(customCollector); err != nil {
		return fmt.Errorf("failed to register custom collector: %w", err)
	}

	// Test metrics retrieval
	metrics := e.Collector.GetMetrics()
	if len(metrics) == 0 {
		return fmt.Errorf("no metrics collected")
	}

	// Test export
	for format := range e.Exporters {
		if _, err := e.Collector.Export(format); err != nil {
			return fmt.Errorf("failed to export %s format: %w", format, err)
		}
	}

	// Test health check
	if err := e.Collector.OnHealthCheck(e.ctx); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// =============================================================================
// HELPER FUNCTIONS
// =============================================================================

// buildTestKey builds a test key for metrics
func buildTestKey(name string, tags []string) string {
	if len(tags) == 0 {
		return name
	}

	parsedTags := ParseTags(tags...)
	return name + "{" + TagsToString(parsedTags) + "}"
}

// AssertMetricValue asserts that a metric has the expected value
func AssertMetricValue(t interface{}, collector MetricsCollector, name string, expected float64, tags ...string) {
	// This would use a proper testing framework like testify
	// For now, this is a placeholder
	metrics := collector.GetMetrics()
	key := buildTestKey(name, tags)

	if value, exists := metrics[key]; !exists {
		panic(fmt.Sprintf("metric %s not found", key))
	} else if floatValue, ok := value.(float64); !ok {
		panic(fmt.Sprintf("metric %s is not a float64", key))
	} else if floatValue != expected {
		panic(fmt.Sprintf("metric %s: expected %f, got %f", key, expected, floatValue))
	}
}

// AssertMetricExists asserts that a metric exists
func AssertMetricExists(t interface{}, collector MetricsCollector, name string, tags ...string) {
	metrics := collector.GetMetrics()
	key := buildTestKey(name, tags)

	if _, exists := metrics[key]; !exists {
		panic(fmt.Sprintf("metric %s not found", key))
	}
}

// AssertMetricDoesNotExist asserts that a metric does not exist
func AssertMetricDoesNotExist(t interface{}, collector MetricsCollector, name string, tags ...string) {
	metrics := collector.GetMetrics()
	key := buildTestKey(name, tags)

	if _, exists := metrics[key]; exists {
		panic(fmt.Sprintf("metric %s should not exist", key))
	}
}

// WaitForMetricValue waits for a metric to reach the expected value
func WaitForMetricValue(collector MetricsCollector, name string, expected float64, timeout time.Duration, tags ...string) error {
	key := buildTestKey(name, tags)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for metric %s to reach %f", key, expected)
		case <-ticker.C:
			metrics := collector.GetMetrics()
			if value, exists := metrics[key]; exists {
				if floatValue, ok := value.(float64); ok && floatValue == expected {
					return nil
				}
			}
		}
	}
}

// ValidateExportFormat validates that export format is valid
func ValidateExportFormat(data []byte, format ExportFormat) error {
	switch format {
	case ExportFormatPrometheus:
		return validatePrometheusFormat(data)
	case ExportFormatJSON:
		return validateJSONFormat(data)
	case ExportFormatInflux:
		return validateInfluxFormat(data)
	case ExportFormatStatsD:
		return validateStatsDFormat(data)
	default:
		return fmt.Errorf("unknown export format: %s", format)
	}
}

// validatePrometheusFormat validates Prometheus format
func validatePrometheusFormat(data []byte) error {
	// Simple validation - would use proper Prometheus parser in real implementation
	content := string(data)
	if !strings.Contains(content, "# HELP") && !strings.Contains(content, "# TYPE") {
		return fmt.Errorf("invalid Prometheus format")
	}
	return nil
}

// validateJSONFormat validates JSON format
func validateJSONFormat(data []byte) error {
	var result map[string]interface{}
	return json.Unmarshal(data, &result)
}

// validateInfluxFormat validates InfluxDB format
func validateInfluxFormat(data []byte) error {
	// Simple validation - would use proper InfluxDB parser in real implementation
	content := string(data)
	if len(content) == 0 {
		return fmt.Errorf("empty InfluxDB format")
	}
	return nil
}

// validateStatsDFormat validates StatsD format
func validateStatsDFormat(data []byte) error {
	// Simple validation - would use proper StatsD parser in real implementation
	content := string(data)
	if len(content) == 0 {
		return fmt.Errorf("empty StatsD format")
	}
	return nil
}

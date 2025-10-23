package metrics

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/internal/shared"
)

// TestNoOpMetrics verifies that no-op metrics collector implements the interface
// correctly and all operations are safe no-ops
func TestNoOpMetrics(t *testing.T) {
	ctx := context.Background()
	m := NewNoOpMetrics()

	// Test service lifecycle
	if err := m.Start(ctx); err != nil {
		t.Errorf("Start() should not error: %v", err)
	}

	if err := m.Health(ctx); err != nil {
		t.Errorf("Health() should not error: %v", err)
	}

	if name := m.Name(); name != "noop-metrics" {
		t.Errorf("Name() = %q, want %q", name, "noop-metrics")
	}

	if err := m.Stop(ctx); err != nil {
		t.Errorf("Stop() should not error: %v", err)
	}
}

// TestNoOpCounter verifies no-op counter operations
func TestNoOpCounter(t *testing.T) {
	m := NewNoOpMetrics()
	counter := m.Counter("test_counter", "env", "test")

	// All operations should be safe no-ops
	counter.Inc()
	counter.Add(10.5)

	if val := counter.Get(); val != 0 {
		t.Errorf("Counter.Get() = %f, want 0", val)
	}

	labeled := counter.WithLabels(map[string]string{"key": "value"})
	if labeled == nil {
		t.Error("WithLabels() should not return nil")
	}

	if err := counter.Reset(); err != nil {
		t.Errorf("Reset() should not error: %v", err)
	}
}

// TestNoOpGauge verifies no-op gauge operations
func TestNoOpGauge(t *testing.T) {
	m := NewNoOpMetrics()
	gauge := m.Gauge("test_gauge", "env", "test")

	// All operations should be safe no-ops
	gauge.Set(42.5)
	gauge.Inc()
	gauge.Dec()
	gauge.Add(10.5)

	if val := gauge.Get(); val != 0 {
		t.Errorf("Gauge.Get() = %f, want 0", val)
	}

	labeled := gauge.WithLabels(map[string]string{"key": "value"})
	if labeled == nil {
		t.Error("WithLabels() should not return nil")
	}

	if err := gauge.Reset(); err != nil {
		t.Errorf("Reset() should not error: %v", err)
	}
}

// TestNoOpHistogram verifies no-op histogram operations
func TestNoOpHistogram(t *testing.T) {
	m := NewNoOpMetrics()
	histogram := m.Histogram("test_histogram", "env", "test")

	// All operations should be safe no-ops
	histogram.Observe(0.5)
	histogram.ObserveDuration(time.Now())

	if count := histogram.GetCount(); count != 0 {
		t.Errorf("GetCount() = %d, want 0", count)
	}

	if sum := histogram.GetSum(); sum != 0 {
		t.Errorf("GetSum() = %f, want 0", sum)
	}

	if mean := histogram.GetMean(); mean != 0 {
		t.Errorf("GetMean() = %f, want 0", mean)
	}

	if p99 := histogram.GetPercentile(99); p99 != 0 {
		t.Errorf("GetPercentile(99) = %f, want 0", p99)
	}

	buckets := histogram.GetBuckets()
	if len(buckets) != 0 {
		t.Errorf("GetBuckets() = %v, want empty map", buckets)
	}

	labeled := histogram.WithLabels(map[string]string{"key": "value"})
	if labeled == nil {
		t.Error("WithLabels() should not return nil")
	}

	if err := histogram.Reset(); err != nil {
		t.Errorf("Reset() should not error: %v", err)
	}
}

// TestNoOpTimer verifies no-op timer operations
func TestNoOpTimer(t *testing.T) {
	m := NewNoOpMetrics()
	timer := m.Timer("test_timer", "env", "test")

	// All operations should be safe no-ops
	timer.Record(100 * time.Millisecond)

	stopFunc := timer.Time()
	if stopFunc == nil {
		t.Error("Time() should return a function")
	}
	stopFunc() // Should be safe to call

	if count := timer.GetCount(); count != 0 {
		t.Errorf("GetCount() = %d, want 0", count)
	}

	if mean := timer.GetMean(); mean != 0 {
		t.Errorf("GetMean() = %v, want 0", mean)
	}

	if min := timer.GetMin(); min != 0 {
		t.Errorf("GetMin() = %v, want 0", min)
	}

	if max := timer.GetMax(); max != 0 {
		t.Errorf("GetMax() = %v, want 0", max)
	}

	if p99 := timer.GetPercentile(99); p99 != 0 {
		t.Errorf("GetPercentile(99) = %v, want 0", p99)
	}

	if val := timer.Get(); val != 0 {
		t.Errorf("Get() = %v, want 0", val)
	}

	timer.Reset() // Should be safe
}

// TestNoOpExport verifies no-op export operations
func TestNoOpExport(t *testing.T) {
	m := NewNoOpMetrics()

	// Test all export formats
	formats := []struct {
		name   string
		format shared.ExportFormat
	}{
		{"prometheus", shared.ExportFormatPrometheus},
		{"json", shared.ExportFormatJSON},
		{"influx", shared.ExportFormatInflux},
		{"statsd", shared.ExportFormatStatsD},
	}
	for _, tc := range formats {
		t.Run(tc.name, func(t *testing.T) {
			data, err := m.Export(tc.format)
			if err != nil {
				t.Errorf("Export(%q) error = %v, want nil", tc.format, err)
			}
			if len(data) != 0 {
				t.Errorf("Export(%q) = %d bytes, want empty", tc.format, len(data))
			}
		})
	}

	// Test export to file
	if err := m.ExportToFile(shared.ExportFormatPrometheus, "/tmp/test.txt"); err != nil {
		t.Errorf("ExportToFile() error = %v, want nil", err)
	}
}

// TestNoOpCollectorManagement verifies custom collector management
func TestNoOpCollectorManagement(t *testing.T) {
	m := NewNoOpMetrics()

	// Register should be no-op
	mockCollector := NewMockCustomCollector("test")
	if err := m.RegisterCollector(mockCollector); err != nil {
		t.Errorf("RegisterCollector() error = %v, want nil", err)
	}

	// GetCollectors should return empty slice
	collectors := m.GetCollectors()
	if len(collectors) != 0 {
		t.Errorf("GetCollectors() = %d collectors, want 0", len(collectors))
	}

	// Unregister should be no-op
	if err := m.UnregisterCollector("test"); err != nil {
		t.Errorf("UnregisterCollector() error = %v, want nil", err)
	}
}

// TestNoOpMetricsRetrieval verifies metrics retrieval operations
func TestNoOpMetricsRetrieval(t *testing.T) {
	m := NewNoOpMetrics()

	// Create some metrics (no-ops)
	m.Counter("test_counter").Inc()
	m.Gauge("test_gauge").Set(42)
	m.Histogram("test_histogram").Observe(0.5)
	m.Timer("test_timer").Record(100 * time.Millisecond)

	// GetMetrics should return empty map
	metrics := m.GetMetrics()
	if len(metrics) != 0 {
		t.Errorf("GetMetrics() = %d metrics, want 0", len(metrics))
	}

	// GetMetricsByType should return empty map
	metricsByType := m.GetMetricsByType("counter")
	if len(metricsByType) != 0 {
		t.Errorf("GetMetricsByType() = %d metrics, want 0", len(metricsByType))
	}

	// GetMetricsByTag should return empty map
	metricsByTag := m.GetMetricsByTag("env", "test")
	if len(metricsByTag) != 0 {
		t.Errorf("GetMetricsByTag() = %d metrics, want 0", len(metricsByTag))
	}
}

// TestNoOpManagement verifies management operations
func TestNoOpManagement(t *testing.T) {
	m := NewNoOpMetrics()

	// Reset should be no-op
	if err := m.Reset(); err != nil {
		t.Errorf("Reset() error = %v, want nil", err)
	}

	// ResetMetric should be no-op
	if err := m.ResetMetric("test_counter"); err != nil {
		t.Errorf("ResetMetric() error = %v, want nil", err)
	}
}

// TestNoOpStats verifies statistics retrieval
func TestNoOpStats(t *testing.T) {
	m := NewNoOpMetrics()

	stats := m.GetStats()

	if stats.Name != "noop-metrics" {
		t.Errorf("GetStats().Name = %q, want %q", stats.Name, "noop-metrics")
	}

	if stats.Started {
		t.Error("GetStats().Started = true, want false")
	}

	if stats.MetricsCreated != 0 {
		t.Errorf("GetStats().MetricsCreated = %d, want 0", stats.MetricsCreated)
	}

	if stats.MetricsCollected != 0 {
		t.Errorf("GetStats().MetricsCollected = %d, want 0", stats.MetricsCollected)
	}

	if stats.CustomCollectors != 0 {
		t.Errorf("GetStats().CustomCollectors = %d, want 0", stats.CustomCollectors)
	}

	if stats.ActiveMetrics != 0 {
		t.Errorf("GetStats().ActiveMetrics = %d, want 0", stats.ActiveMetrics)
	}

	if len(stats.Errors) != 0 {
		t.Errorf("GetStats().Errors = %v, want empty slice", stats.Errors)
	}

	if len(stats.ExporterStats) != 0 {
		t.Errorf("GetStats().ExporterStats = %v, want empty map", stats.ExporterStats)
	}
}

// BenchmarkNoOpCounter benchmarks no-op counter operations
func BenchmarkNoOpCounter(b *testing.B) {
	m := NewNoOpMetrics()
	counter := m.Counter("benchmark_counter")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		counter.Inc()
	}
}

// BenchmarkNoOpGauge benchmarks no-op gauge operations
func BenchmarkNoOpGauge(b *testing.B) {
	m := NewNoOpMetrics()
	gauge := m.Gauge("benchmark_gauge")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		gauge.Set(float64(i))
	}
}

// BenchmarkNoOpHistogram benchmarks no-op histogram operations
func BenchmarkNoOpHistogram(b *testing.B) {
	m := NewNoOpMetrics()
	histogram := m.Histogram("benchmark_histogram")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		histogram.Observe(float64(i))
	}
}

// BenchmarkNoOpTimer benchmarks no-op timer operations
func BenchmarkNoOpTimer(b *testing.B) {
	m := NewNoOpMetrics()
	timer := m.Timer("benchmark_timer")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		timer.Record(time.Duration(i) * time.Millisecond)
	}
}

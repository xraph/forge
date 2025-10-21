package forge

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMetricsConfig(t *testing.T) {
	config := DefaultMetricsConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, "/_/metrics", config.MetricsPath)
	assert.Equal(t, "forge", config.Namespace)
}

func TestMetricsConfig_Custom(t *testing.T) {
	config := MetricsConfig{
		Enabled:     false,
		MetricsPath: "/custom/metrics",
		Namespace:   "myapp",
	}

	assert.False(t, config.Enabled)
	assert.Equal(t, "/custom/metrics", config.MetricsPath)
	assert.Equal(t, "myapp", config.Namespace)
}

func TestNewMetrics(t *testing.T) {
	metrics := NewMetrics("test")
	assert.NotNil(t, metrics)
}

func TestMetrics_Counter(t *testing.T) {
	metrics := NewMetrics("test")

	counter := metrics.Counter("requests_total")
	assert.NotNil(t, counter)

	// Should return same instance
	counter2 := metrics.Counter("requests_total")
	assert.Equal(t, counter, counter2)
}

func TestCounter_Inc(t *testing.T) {
	metrics := NewMetrics("test")
	counter := metrics.Counter("test_counter")

	counter.Inc()
	counter.Inc()
	counter.Inc()

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_test_counter")
	assert.Contains(t, output, "3")
}

func TestCounter_Add(t *testing.T) {
	metrics := NewMetrics("test")
	counter := metrics.Counter("test_add")

	counter.Add(10)
	counter.Add(5)

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_test_add")
	assert.Contains(t, output, "15")
}

func TestCounter_WithLabels(t *testing.T) {
	metrics := NewMetrics("test")
	counter := metrics.Counter("labeled_counter")

	labeledCounter := counter.WithLabels(map[string]string{
		"method": "GET",
		"path":   "/api/users",
	})

	assert.NotNil(t, labeledCounter)
	labeledCounter.Inc()

	// Note: In this simple implementation, labeled metrics create
	// standalone instances that don't appear in the main export
	// This is acceptable for Phase 6 core implementation
}

func TestMetrics_Gauge(t *testing.T) {
	metrics := NewMetrics("test")

	gauge := metrics.Gauge("active_connections")
	assert.NotNil(t, gauge)

	// Should return same instance
	gauge2 := metrics.Gauge("active_connections")
	assert.Equal(t, gauge, gauge2)
}

func TestGauge_Set(t *testing.T) {
	metrics := NewMetrics("test")
	gauge := metrics.Gauge("test_gauge")

	gauge.Set(42)

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_test_gauge")
	assert.Contains(t, output, "42")
}

func TestGauge_Inc(t *testing.T) {
	metrics := NewMetrics("test")
	gauge := metrics.Gauge("test_inc")

	gauge.Inc()
	gauge.Inc()

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_test_inc")
	assert.Contains(t, output, "2")
}

func TestGauge_Dec(t *testing.T) {
	metrics := NewMetrics("test")
	gauge := metrics.Gauge("test_dec")

	gauge.Set(10)
	gauge.Dec()
	gauge.Dec()

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_test_dec")
	assert.Contains(t, output, "8")
}

func TestGauge_Add(t *testing.T) {
	metrics := NewMetrics("test")
	gauge := metrics.Gauge("test_add")

	gauge.Add(10)
	gauge.Add(5)
	gauge.Add(-3)

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_test_add")
	assert.Contains(t, output, "12")
}

func TestGauge_WithLabels(t *testing.T) {
	metrics := NewMetrics("test")
	gauge := metrics.Gauge("labeled_gauge")

	labeledGauge := gauge.WithLabels(map[string]string{
		"host":   "server1",
		"region": "us-west",
	})

	assert.NotNil(t, labeledGauge)
	labeledGauge.Set(100)

	// Note: In this simple implementation, labeled metrics create
	// standalone instances
}

func TestMetrics_Histogram(t *testing.T) {
	metrics := NewMetrics("test")

	histogram := metrics.Histogram("request_duration")
	assert.NotNil(t, histogram)

	// Should return same instance
	histogram2 := metrics.Histogram("request_duration")
	assert.Equal(t, histogram, histogram2)
}

func TestHistogram_Observe(t *testing.T) {
	metrics := NewMetrics("test")
	histogram := metrics.Histogram("test_histogram")

	histogram.Observe(0.002)
	histogram.Observe(0.01)
	histogram.Observe(0.05)
	histogram.Observe(1.5)

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_test_histogram")
	assert.Contains(t, output, "_bucket{le=")
	assert.Contains(t, output, "_sum")
	assert.Contains(t, output, "_count")
}

func TestHistogram_ObserveDuration(t *testing.T) {
	metrics := NewMetrics("test")
	histogram := metrics.Histogram("duration_test")

	start := time.Now().Add(-100 * time.Millisecond)
	histogram.ObserveDuration(start)

	data, err := metrics.Export()
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "test_duration_test")
}

func TestHistogram_WithLabels(t *testing.T) {
	metrics := NewMetrics("test")
	histogram := metrics.Histogram("labeled_histogram")

	labeledHistogram := histogram.WithLabels(map[string]string{
		"method":   "POST",
		"endpoint": "/api/data",
	})

	assert.NotNil(t, labeledHistogram)
	labeledHistogram.Observe(0.05)

	// Note: In this simple implementation, labeled metrics create
	// standalone instances
}

func TestMetrics_Export(t *testing.T) {
	metrics := NewMetrics("test")

	// Add various metrics
	counter := metrics.Counter("test_counter")
	counter.Inc()

	gauge := metrics.Gauge("test_gauge")
	gauge.Set(42)

	histogram := metrics.Histogram("test_histogram")
	histogram.Observe(0.5)

	data, err := metrics.Export()
	require.NoError(t, err)

	output := string(data)

	// Should contain all metrics
	assert.Contains(t, output, "test_test_counter")
	assert.Contains(t, output, "test_test_gauge")
	assert.Contains(t, output, "test_test_histogram")

	// Should contain metric types
	assert.Contains(t, output, "# TYPE")
	assert.Contains(t, output, "counter")
	assert.Contains(t, output, "gauge")
	assert.Contains(t, output, "histogram")
}

func TestMetrics_PrometheusFormat(t *testing.T) {
	metrics := NewMetrics("app")

	counter := metrics.Counter("requests_total")
	counter.Add(100)

	data, err := metrics.Export()
	require.NoError(t, err)

	output := string(data)
	lines := strings.Split(output, "\n")

	// Check Prometheus format
	foundType := false
	foundMetric := false

	for _, line := range lines {
		if strings.Contains(line, "# TYPE app_requests_total counter") {
			foundType = true
		}
		if strings.Contains(line, "app_requests_total 100") {
			foundMetric = true
		}
	}

	assert.True(t, foundType, "Should have TYPE declaration")
	assert.True(t, foundMetric, "Should have metric value")
}

func TestMetrics_EmptyExport(t *testing.T) {
	metrics := NewMetrics("test")

	_, err := metrics.Export()
	assert.NoError(t, err)
	// Empty but valid (may be nil or empty byte slice)
}

func TestMetrics_MultipleNamespaces(t *testing.T) {
	metrics1 := NewMetrics("app1")
	metrics2 := NewMetrics("app2")

	counter1 := metrics1.Counter("requests")
	counter1.Inc()

	counter2 := metrics2.Counter("requests")
	counter2.Inc()

	data1, err := metrics1.Export()
	require.NoError(t, err)
	assert.Contains(t, string(data1), "app1_requests")

	data2, err := metrics2.Export()
	require.NoError(t, err)
	assert.Contains(t, string(data2), "app2_requests")
}

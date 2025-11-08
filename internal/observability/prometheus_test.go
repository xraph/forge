package observability

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/xraph/forge/internal/logger"
)

func TestNewPrometheusExporter(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9090",
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}

	if exporter == nil {
		t.Fatal("NewPrometheusExporter() returned nil")
	}

	// Test shutdown
	ctx := context.Background()
	if err := exporter.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
}

func TestPrometheusExporter_ExportMetric(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9090",
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Test counter metric
	metric := &Metric{
		Name:        "test_counter",
		Type:        MetricTypeCounter,
		Value:       1.0,
		Unit:        "count",
		Labels:      map[string]string{"test": "true"},
		Description: "Test counter metric",
	}

	err = exporter.ExportMetric(context.Background(), metric)
	if err != nil {
		t.Errorf("ExportMetric() error = %v", err)
	}

	// Test gauge metric
	gaugeMetric := &Metric{
		Name:        "test_gauge",
		Type:        MetricTypeGauge,
		Value:       42.0,
		Unit:        "units",
		Labels:      map[string]string{"test": "true"},
		Description: "Test gauge metric",
	}

	err = exporter.ExportMetric(context.Background(), gaugeMetric)
	if err != nil {
		t.Errorf("ExportMetric() error = %v", err)
	}

	// Test histogram metric
	histogramMetric := &Metric{
		Name:        "test_histogram",
		Type:        MetricTypeHistogram,
		Value:       0.5,
		Unit:        "seconds",
		Labels:      map[string]string{"test": "true"},
		Description: "Test histogram metric",
	}

	err = exporter.ExportMetric(context.Background(), histogramMetric)
	if err != nil {
		t.Errorf("ExportMetric() error = %v", err)
	}

	// Test summary metric
	summaryMetric := &Metric{
		Name:        "test_summary",
		Type:        MetricTypeSummary,
		Value:       0.25,
		Unit:        "seconds",
		Labels:      map[string]string{"test": "true"},
		Description: "Test summary metric",
	}

	err = exporter.ExportMetric(context.Background(), summaryMetric)
	if err != nil {
		t.Errorf("ExportMetric() error = %v", err)
	}
}

func TestPrometheusExporter_Start(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9091", // Use different port to avoid conflicts
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}

	// Test starting server
	err = exporter.Start()
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Test stopping server
	ctx := context.Background()
	if err := exporter.Stop(ctx); err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

func TestPrometheusExporter_GetHandler(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9090",
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Test getting handler
	handler := exporter.GetHandler()
	if handler == nil {
		t.Error("GetHandler() returned nil")
	}
}

func TestPrometheusExporter_GetRegistry(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9090",
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Test getting registry
	registry := exporter.GetRegistry()
	if registry == nil {
		t.Error("GetRegistry() returned nil")
	}
}

func TestPrometheusExporter_GetStats(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9090",
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Test stats
	stats := exporter.GetStats()
	if stats == nil {
		t.Error("GetStats() returned nil stats")
	}

	// Check for expected fields
	expectedFields := []string{"metrics_count", "listen_address", "metrics_path", "enable_runtime", "enable_go_metrics"}
	for _, field := range expectedFields {
		if _, exists := stats[field]; !exists {
			t.Errorf("GetStats() missing field: %s", field)
		}
	}
}

func TestPrometheusExporter_GetName(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9090",
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Test getting name
	name := exporter.GetName()
	if name != "prometheus" {
		t.Errorf("GetName() = %v, want %v", name, "prometheus")
	}
}

func TestPrometheusExporter_ConcurrentAccess(t *testing.T) {
	config := PrometheusConfig{
		EnableMetrics:   true,
		EnableRuntime:   true,
		ListenAddress:   ":9090",
		MetricsPath:     "/metrics",
		EnableGoMetrics: true,
		Logger:          logger.NewNoopLogger(),
	}

	exporter, err := NewPrometheusExporter(config)
	if err != nil {
		t.Fatalf("NewPrometheusExporter() error = %v", err)
	}
	defer exporter.Shutdown(context.Background())

	// Test concurrent metric export
	done := make(chan bool, 10)

	for i := range 10 {
		go func(i int) {
			metric := &Metric{
				Name:        "concurrent_metric",
				Type:        MetricTypeCounter,
				Value:       1.0,
				Unit:        "count",
				Labels:      map[string]string{"test": "true", "id": string(rune(i))},
				Description: "Concurrent test metric",
			}
			exporter.ExportMetric(context.Background(), metric)

			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for range 10 {
		<-done
	}
}

func TestSystemMetricsCollector(t *testing.T) {
	collector := NewSystemMetricsCollector()
	if collector == nil {
		t.Fatal("NewSystemMetricsCollector() returned nil")
	}

	// Test that collector implements prometheus.Collector interface
	var _ prometheus.Collector = collector
}

func TestApplicationMetricsCollector(t *testing.T) {
	collector := NewApplicationMetricsCollector()
	if collector == nil {
		t.Fatal("NewApplicationMetricsCollector() returned nil")
	}

	// Test that collector implements prometheus.Collector interface
	var _ prometheus.Collector = collector
}

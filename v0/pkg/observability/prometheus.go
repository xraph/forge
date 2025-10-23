package observability

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// PrometheusExporter provides Prometheus metrics export
type PrometheusExporter struct {
	config   PrometheusConfig
	registry *prometheus.Registry
	metrics  map[string]prometheus.Collector
	mu       sync.RWMutex
	logger   common.Logger
	server   *http.Server
	stopC    chan struct{}
}

// PrometheusConfig contains Prometheus configuration
type PrometheusConfig struct {
	EnableMetrics   bool          `yaml:"enable_metrics" default:"true"`
	EnableRuntime   bool          `yaml:"enable_runtime" default:"true"`
	ListenAddress   string        `yaml:"listen_address" default:":9090"`
	MetricsPath     string        `yaml:"metrics_path" default:"/metrics"`
	EnableGoMetrics bool          `yaml:"enable_go_metrics" default:"true"`
	Logger          common.Logger `yaml:"-"`
}

// MetricExporter interface for metric exporters
type MetricExporter interface {
	ExportMetric(ctx context.Context, metric *Metric) error
	Shutdown(ctx context.Context) error
	GetName() string
}

// NewPrometheusExporter creates a new Prometheus exporter
func NewPrometheusExporter(config PrometheusConfig) (*PrometheusExporter, error) {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	exporter := &PrometheusExporter{
		config:   config,
		registry: prometheus.NewRegistry(),
		metrics:  make(map[string]prometheus.Collector),
		logger:   config.Logger,
		stopC:    make(chan struct{}),
	}

	// Register standard Go metrics if enabled
	if config.EnableGoMetrics {
		exporter.registry.MustRegister(prometheus.NewGoCollector())
		exporter.registry.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	}

	return exporter, nil
}

// ExportMetric exports a metric to Prometheus
func (p *PrometheusExporter) ExportMetric(ctx context.Context, metric *Metric) error {
	if !p.config.EnableMetrics {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Convert labels to Prometheus format
	labels := make(prometheus.Labels)
	for k, v := range metric.Labels {
		labels[k] = v
	}

	// Get or create metric
	collector, exists := p.metrics[metric.Name]
	if !exists {
		var newCollector prometheus.Collector
		var err error

		switch metric.Type {
		case MetricTypeCounter:
			newCollector, err = p.createCounter(metric, labels)
		case MetricTypeGauge:
			newCollector, err = p.createGauge(metric, labels)
		case MetricTypeHistogram:
			newCollector, err = p.createHistogram(metric, labels)
		case MetricTypeSummary:
			newCollector, err = p.createSummary(metric, labels)
		default:
			return fmt.Errorf("unsupported metric type: %v", metric.Type)
		}

		if err != nil {
			return fmt.Errorf("failed to create metric %s: %w", metric.Name, err)
		}

		// Register the new collector
		if err := p.registry.Register(newCollector); err != nil {
			return fmt.Errorf("failed to register metric %s: %w", metric.Name, err)
		}

		p.metrics[metric.Name] = newCollector
		collector = newCollector
	}

	// Update the metric value
	return p.updateMetric(collector, metric, labels)
}

// createCounter creates a Prometheus counter
func (p *PrometheusExporter) createCounter(metric *Metric, labels prometheus.Labels) (prometheus.Collector, error) {
	opts := prometheus.CounterOpts{
		Name:        metric.Name,
		Help:        metric.Description,
		ConstLabels: nil, // Don't use const labels to avoid conflicts
	}

	// Extract label names from the metric labels
	labelNames := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}

	counter := prometheus.NewCounterVec(opts, labelNames)
	return counter, nil
}

// createGauge creates a Prometheus gauge
func (p *PrometheusExporter) createGauge(metric *Metric, labels prometheus.Labels) (prometheus.Collector, error) {
	opts := prometheus.GaugeOpts{
		Name:        metric.Name,
		Help:        metric.Description,
		ConstLabels: nil, // Don't use const labels to avoid conflicts
	}

	// Extract label names from the metric labels
	labelNames := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}

	gauge := prometheus.NewGaugeVec(opts, labelNames)
	return gauge, nil
}

// createHistogram creates a Prometheus histogram
func (p *PrometheusExporter) createHistogram(metric *Metric, labels prometheus.Labels) (prometheus.Collector, error) {
	opts := prometheus.HistogramOpts{
		Name:        metric.Name,
		Help:        metric.Description,
		ConstLabels: nil, // Don't use const labels to avoid conflicts
		Buckets:     prometheus.DefBuckets,
	}

	// Extract label names from the metric labels
	labelNames := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}

	histogram := prometheus.NewHistogramVec(opts, labelNames)
	return histogram, nil
}

// createSummary creates a Prometheus summary
func (p *PrometheusExporter) createSummary(metric *Metric, labels prometheus.Labels) (prometheus.Collector, error) {
	opts := prometheus.SummaryOpts{
		Name:        metric.Name,
		Help:        metric.Description,
		ConstLabels: nil, // Don't use const labels to avoid conflicts
	}

	// Extract label names from the metric labels
	labelNames := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}

	summary := prometheus.NewSummaryVec(opts, labelNames)
	return summary, nil
}

// updateMetric updates a metric value
func (p *PrometheusExporter) updateMetric(collector prometheus.Collector, metric *Metric, labels prometheus.Labels) error {
	switch c := collector.(type) {
	case *prometheus.CounterVec:
		c.With(labels).Add(metric.Value)
	case *prometheus.GaugeVec:
		c.With(labels).Set(metric.Value)
	case *prometheus.HistogramVec:
		c.With(labels).Observe(metric.Value)
	case *prometheus.SummaryVec:
		c.With(labels).Observe(metric.Value)
	default:
		return fmt.Errorf("unsupported collector type: %T", collector)
	}
	return nil
}

// Start starts the Prometheus metrics server
func (p *PrometheusExporter) Start() error {
	if p.server != nil {
		return fmt.Errorf("server already started")
	}

	// Create HTTP handler
	handler := promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})

	// Create HTTP server
	p.server = &http.Server{
		Addr:    p.config.ListenAddress,
		Handler: handler,
	}

	// Start server in goroutine
	go func() {
		p.mu.RLock()
		server := p.server
		p.mu.RUnlock()

		if server != nil {
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				p.logger.Error("prometheus server error", logger.String("error", err.Error()))
			}
		}
	}()

	p.logger.Info("prometheus metrics server started", logger.String("address", p.config.ListenAddress))
	return nil
}

// Stop stops the Prometheus metrics server
func (p *PrometheusExporter) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.server == nil {
		return nil
	}

	close(p.stopC)

	if err := p.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown prometheus server: %w", err)
	}

	p.server = nil
	p.logger.Info("prometheus metrics server stopped")
	return nil
}

// GetHandler returns the HTTP handler for metrics
func (p *PrometheusExporter) GetHandler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// GetRegistry returns the Prometheus registry
func (p *PrometheusExporter) GetRegistry() *prometheus.Registry {
	return p.registry
}

// GetStats returns Prometheus exporter statistics
func (p *PrometheusExporter) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return map[string]interface{}{
		"metrics_count":     len(p.metrics),
		"listen_address":    p.config.ListenAddress,
		"metrics_path":      p.config.MetricsPath,
		"enable_runtime":    p.config.EnableRuntime,
		"enable_go_metrics": p.config.EnableGoMetrics,
	}
}

// Shutdown shuts down the exporter
func (p *PrometheusExporter) Shutdown(ctx context.Context) error {
	return p.Stop(ctx)
}

// GetName returns the exporter name
func (p *PrometheusExporter) GetName() string {
	return "prometheus"
}

// Built-in metric collectors for common system metrics

// SystemMetricsCollector collects system-level metrics
type SystemMetricsCollector struct {
	cpuUsage    *prometheus.Desc
	memoryUsage *prometheus.Desc
	diskUsage   *prometheus.Desc
	networkIO   *prometheus.Desc
}

// NewSystemMetricsCollector creates a new system metrics collector
func NewSystemMetricsCollector() *SystemMetricsCollector {
	return &SystemMetricsCollector{
		cpuUsage: prometheus.NewDesc(
			"system_cpu_usage_percent",
			"CPU usage percentage",
			[]string{"cpu"}, nil,
		),
		memoryUsage: prometheus.NewDesc(
			"system_memory_usage_bytes",
			"Memory usage in bytes",
			[]string{"type"}, nil,
		),
		diskUsage: prometheus.NewDesc(
			"system_disk_usage_bytes",
			"Disk usage in bytes",
			[]string{"device", "mountpoint"}, nil,
		),
		networkIO: prometheus.NewDesc(
			"system_network_io_bytes",
			"Network I/O in bytes",
			[]string{"interface", "direction"}, nil,
		),
	}
}

// Describe implements prometheus.Collector
func (c *SystemMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.cpuUsage
	ch <- c.memoryUsage
	ch <- c.diskUsage
	ch <- c.networkIO
}

// Collect implements prometheus.Collector
func (c *SystemMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	// In a real implementation, this would collect actual system metrics
	// For now, we'll provide placeholder values

	// CPU usage (placeholder)
	ch <- prometheus.MustNewConstMetric(c.cpuUsage, prometheus.GaugeValue, 25.5, "cpu0")

	// Memory usage (placeholder)
	ch <- prometheus.MustNewConstMetric(c.memoryUsage, prometheus.GaugeValue, 1024*1024*1024, "used")
	ch <- prometheus.MustNewConstMetric(c.memoryUsage, prometheus.GaugeValue, 2*1024*1024*1024, "total")

	// Disk usage (placeholder)
	ch <- prometheus.MustNewConstMetric(c.diskUsage, prometheus.GaugeValue, 50*1024*1024*1024, "sda1", "/")

	// Network I/O (placeholder)
	ch <- prometheus.MustNewConstMetric(c.networkIO, prometheus.CounterValue, 1024*1024, "eth0", "rx")
	ch <- prometheus.MustNewConstMetric(c.networkIO, prometheus.CounterValue, 512*1024, "eth0", "tx")
}

// ApplicationMetricsCollector collects application-level metrics
type ApplicationMetricsCollector struct {
	requestDuration   *prometheus.Desc
	requestCount      *prometheus.Desc
	errorCount        *prometheus.Desc
	activeConnections *prometheus.Desc
}

// NewApplicationMetricsCollector creates a new application metrics collector
func NewApplicationMetricsCollector() *ApplicationMetricsCollector {
	return &ApplicationMetricsCollector{
		requestDuration: prometheus.NewDesc(
			"app_request_duration_seconds",
			"Request duration in seconds",
			[]string{"method", "endpoint", "status"}, nil,
		),
		requestCount: prometheus.NewDesc(
			"app_requests_total",
			"Total number of requests",
			[]string{"method", "endpoint", "status"}, nil,
		),
		errorCount: prometheus.NewDesc(
			"app_errors_total",
			"Total number of errors",
			[]string{"type", "severity"}, nil,
		),
		activeConnections: prometheus.NewDesc(
			"app_active_connections",
			"Number of active connections",
			[]string{"type"}, nil,
		),
	}
}

// Describe implements prometheus.Collector
func (c *ApplicationMetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.requestDuration
	ch <- c.requestCount
	ch <- c.errorCount
	ch <- c.activeConnections
}

// Collect implements prometheus.Collector
func (c *ApplicationMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	// In a real implementation, this would collect actual application metrics
	// For now, we'll provide placeholder values

	// Request metrics (placeholder)
	ch <- prometheus.MustNewConstMetric(c.requestDuration, prometheus.CounterValue, 0.1, "GET", "/api/users", "200")
	ch <- prometheus.MustNewConstMetric(c.requestCount, prometheus.CounterValue, 100, "GET", "/api/users", "200")

	// Error metrics (placeholder)
	ch <- prometheus.MustNewConstMetric(c.errorCount, prometheus.CounterValue, 5, "validation", "warning")

	// Connection metrics (placeholder)
	ch <- prometheus.MustNewConstMetric(c.activeConnections, prometheus.GaugeValue, 25, "http")
}

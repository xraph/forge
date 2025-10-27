package observability

//nolint:gosec // G104: RecordError intentionally discards errors
// Observability tracking intentionally doesn't handle error returns.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/internal/logger"
)

// Observability provides unified observability functionality
type Observability struct {
	config   ObservabilityConfig
	monitor  *Monitor
	tracer   *OTelTracer
	exporter *PrometheusExporter
	mu       sync.RWMutex
	logger   logger.Logger
	stopC    chan struct{}
	wg       sync.WaitGroup
}

// ObservabilityConfig contains unified observability configuration
type ObservabilityConfig struct {
	// Monitor configuration
	Monitor MonitoringConfig `yaml:"monitor"`

	// Tracer configuration
	Tracer OTelTracingConfig `yaml:"tracer"`

	// Prometheus configuration
	Prometheus PrometheusConfig `yaml:"prometheus"`

	// Global settings
	EnableMetrics   bool          `yaml:"enable_metrics" default:"true"`
	EnableTracing   bool          `yaml:"enable_tracing" default:"true"`
	EnableAlerts    bool          `yaml:"enable_alerts" default:"true"`
	EnableHealth    bool          `yaml:"enable_health" default:"true"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout" default:"30s"`

	Logger logger.Logger `yaml:"-"`
}

// NewObservability creates a new observability instance
func NewObservability(config ObservabilityConfig) (*Observability, error) {
	if config.Logger == nil {
		config.Logger = logger.NewLogger(logger.LoggingConfig{Level: "info"})
	}

	obs := &Observability{
		config: config,
		logger: config.Logger,
		stopC:  make(chan struct{}),
	}

	// Initialize monitor
	if config.EnableMetrics || config.EnableAlerts || config.EnableHealth {
		monitorConfig := config.Monitor
		monitorConfig.Logger = config.Logger
		obs.monitor = NewMonitor(monitorConfig)
	}

	// Initialize tracer
	if config.EnableTracing {
		tracerConfig := config.Tracer
		tracerConfig.Logger = config.Logger
		tracer, err := NewOTelTracer(tracerConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create tracer: %w", err)
		}
		obs.tracer = tracer
	}

	// Initialize Prometheus exporter
	if config.EnableMetrics {
		prometheusConfig := config.Prometheus
		prometheusConfig.Logger = config.Logger
		exporter, err := NewPrometheusExporter(prometheusConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create Prometheus exporter: %w", err)
		}
		obs.exporter = exporter

		// Start Prometheus server
		if err := exporter.Start(); err != nil {
			return nil, fmt.Errorf("failed to start Prometheus server: %w", err)
		}
	}

	return obs, nil
}

// StartSpan starts a new span with tracing
func (o *Observability) StartSpan(ctx context.Context, name string, opts ...interface{}) (context.Context, interface{}) {
	if o.tracer == nil {
		return ctx, nil
	}

	// Start span with OpenTelemetry
	ctx, span := o.tracer.StartSpan(ctx, name)
	return ctx, span
}

// EndSpan ends a span
func (o *Observability) EndSpan(span interface{}) {
	if o.tracer == nil || span == nil {
		return
	}

	// Type assertion to OpenTelemetry span
	if otelSpan, ok := span.(interface{ End() }); ok {
		otelSpan.End()
	}
}

// RecordMetric records a metric
func (o *Observability) RecordMetric(metric *Metric) {
	if o.monitor != nil {
		o.monitor.RecordMetric(metric)
	}

	if o.exporter != nil {
		ctx := context.Background()
		if err := o.exporter.ExportMetric(ctx, metric); err != nil {
			o.logger.Error("failed to export metric",
				logger.String("metric", metric.Name),
				logger.String("error", err.Error()))
		}
	}
}

// RecordError records an error with tracing and metrics
func (o *Observability) RecordError(ctx context.Context, err error, attributes map[string]string) error {
	if o.tracer != nil {
		span := o.tracer.GetSpanFromContext(ctx)
		if span != nil {
			// Set span status to error using OpenTelemetry tracer
			o.tracer.SetStatus(span, 1, err.Error()) // 1 = error code

			// Add error attributes
			for k, v := range attributes {
				o.tracer.SetAttribute(span, k, v)
			}
		}
	}

	// Record error metric
	errorMetric := &Metric{
		Name:        "errors_total",
		Type:        MetricTypeCounter,
		Value:       1.0,
		Unit:        "count",
		Labels:      attributes,
		Description: "Total number of errors",
	}
	o.RecordMetric(errorMetric)

	return nil
}

// ObserveRequest observes a request with tracing and metrics
func (o *Observability) ObserveRequest(ctx context.Context, name string, handler func(context.Context) error) error {
	// Start span
	ctx, span := o.StartSpan(ctx, name)
	defer o.EndSpan(span)

	// Record request start
	startTime := time.Now()
	requestMetric := &Metric{
		Name:        "requests_total",
		Type:        MetricTypeCounter,
		Value:       1.0,
		Unit:        "count",
		Labels:      map[string]string{"operation": name},
		Description: "Total number of requests",
	}
	o.RecordMetric(requestMetric)

	// Execute handler
	err := handler(ctx)

	// Record request duration
	duration := time.Since(startTime)
	durationMetric := &Metric{
		Name:        "request_duration_seconds",
		Type:        MetricTypeHistogram,
		Value:       duration.Seconds(),
		Unit:        "seconds",
		Labels:      map[string]string{"operation": name},
		Description: "Request duration in seconds",
	}
	o.RecordMetric(durationMetric)

	// Record error if occurred
	if err != nil {
		o.RecordError(ctx, err, map[string]string{"operation": name})
	}

	return err
}

// AddAlert adds an alert
func (o *Observability) AddAlert(alert *Alert) error {
	if o.monitor == nil {
		return fmt.Errorf("monitor not initialized")
	}
	return o.monitor.AddAlert(alert)
}

// GetHealthStatus returns the current health status
func (o *Observability) GetHealthStatus() *HealthCheck {
	if o.monitor == nil {
		return &HealthCheck{
			Name:      "observability",
			Status:    HealthStatusUnknown,
			Message:   "Monitor not initialized",
			Timestamp: time.Now(),
		}
	}
	return o.monitor.GetHealthStatus()
}

// GetStats returns observability statistics
func (o *Observability) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if o.monitor != nil {
		stats["monitor"] = o.monitor.GetStats()
	}

	if o.tracer != nil {
		stats["tracer"] = o.tracer.GetStats()
	}

	if o.exporter != nil {
		stats["prometheus"] = o.exporter.GetStats()
	}

	return stats
}

// GetPrometheusHandler returns the Prometheus metrics HTTP handler
func (o *Observability) GetPrometheusHandler() interface{} {
	if o.exporter == nil {
		return nil
	}
	return o.exporter.GetHandler()
}

// RegisterAlertHandler registers an alert handler
func (o *Observability) RegisterAlertHandler(handler AlertHandler) {
	if o.monitor != nil {
		o.monitor.RegisterAlertHandler(handler)
	}
}

// Shutdown shuts down the observability instance
func (o *Observability) Shutdown(ctx context.Context) error {
	close(o.stopC)

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, o.config.ShutdownTimeout)
	defer cancel()

	// Shutdown components
	var shutdownErrors []error

	if o.monitor != nil {
		if err := o.monitor.Shutdown(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("monitor shutdown: %w", err))
		}
	}

	if o.tracer != nil {
		if err := o.tracer.Shutdown(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("tracer shutdown: %w", err))
		}
	}

	if o.exporter != nil {
		if err := o.exporter.Shutdown(shutdownCtx); err != nil {
			shutdownErrors = append(shutdownErrors, fmt.Errorf("prometheus shutdown: %w", err))
		}
	}

	// Wait for all goroutines to complete
	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines completed
	case <-shutdownCtx.Done():
		// Timeout reached
		o.logger.Warn("shutdown timeout reached, some goroutines may still be running")
	}

	// Return combined errors
	if len(shutdownErrors) > 0 {
		return fmt.Errorf("shutdown errors: %v", shutdownErrors)
	}

	return nil
}

// Helper functions for common observability patterns

// WithSpan creates a span and ensures it's properly closed
func (o *Observability) WithSpan(ctx context.Context, name string, fn func(context.Context, interface{}) error) error {
	ctx, span := o.StartSpan(ctx, name)
	defer o.EndSpan(span)
	return fn(ctx, span)
}

// WithMetrics records metrics around a function execution
func (o *Observability) WithMetrics(name string, labels map[string]string, fn func() error) error {
	startTime := time.Now()

	// Record start metric
	startMetric := &Metric{
		Name:        name + "_started_total",
		Type:        MetricTypeCounter,
		Value:       1.0,
		Unit:        "count",
		Labels:      labels,
		Description: "Total number of started operations",
	}
	o.RecordMetric(startMetric)

	// Execute function
	err := fn()

	// Record completion metric
	duration := time.Since(startTime)
	completionMetric := &Metric{
		Name:        name + "_duration_seconds",
		Type:        MetricTypeHistogram,
		Value:       duration.Seconds(),
		Unit:        "seconds",
		Labels:      labels,
		Description: "Operation duration in seconds",
	}
	o.RecordMetric(completionMetric)

	if err != nil {
		// Record error metric
		errorMetric := &Metric{
			Name:        name + "_errors_total",
			Type:        MetricTypeCounter,
			Value:       1.0,
			Unit:        "count",
			Labels:      labels,
			Description: "Total number of errors",
		}
		o.RecordMetric(errorMetric)
	}

	return err
}

// CreateDefaultConfig creates a default observability configuration
func CreateDefaultConfig() ObservabilityConfig {
	return ObservabilityConfig{
		Monitor: MonitoringConfig{
			EnableMetrics:     true,
			EnableAlerts:      true,
			EnableHealth:      true,
			EnableUptime:      true,
			EnablePerformance: true,
			CheckInterval:     30 * time.Second,
			AlertTimeout:      5 * time.Minute,
			RetentionPeriod:   7 * 24 * time.Hour,
			MaxMetrics:        10000,
			MaxAlerts:         1000,
		},
		Tracer: OTelTracingConfig{
			ServiceName:    "forge-service",
			ServiceVersion: "1.0.0",
			Environment:    "development",
			SamplingRate:   1.0,
			MaxSpans:       10000,
			FlushInterval:  5 * time.Second,
			EnableJaeger:   false,
			EnableOTLP:     true,
			EnableConsole:  false,
			EnableB3:       true,
			EnableW3C:      true,
		},
		Prometheus: PrometheusConfig{
			EnableMetrics:   true,
			EnableRuntime:   true,
			ListenAddress:   ":9090",
			MetricsPath:     "/metrics",
			EnableGoMetrics: true,
		},
		EnableMetrics:   true,
		EnableTracing:   true,
		EnableAlerts:    true,
		EnableHealth:    true,
		ShutdownTimeout: 30 * time.Second,
	}
}

package observability

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
)

// metrics implements the Metrics interface using Prometheus
type metrics struct {
	config   MetricsConfig
	registry *prometheus.Registry
	server   *http.Server
	mu       sync.RWMutex

	// Metric caches
	counters   map[string]*prometheusCounter
	gauges     map[string]*prometheusGauge
	histograms map[string]*prometheusHistogram
	summaries  map[string]*prometheusSummary

	// Built-in metrics
	httpRequestsTotal        *prometheus.CounterVec
	httpRequestDuration      *prometheus.HistogramVec
	httpRequestSize          *prometheus.HistogramVec
	httpResponseSize         *prometheus.HistogramVec
	httpActiveRequests       *prometheus.GaugeVec
	databaseConnections      *prometheus.GaugeVec
	databaseOperationsTotal  *prometheus.CounterVec
	databaseOperationErrors  *prometheus.CounterVec
	databaseOperationLatency *prometheus.HistogramVec
	cacheOperationsTotal     *prometheus.CounterVec
	cacheHits                *prometheus.CounterVec
	cacheMisses              *prometheus.CounterVec
	cacheSize                *prometheus.GaugeVec

	// Lifecycle
	shutdownFuncs []func(context.Context) error
}

// prometheusCounter wraps a Prometheus counter
type prometheusCounter struct {
	counter prometheus.Counter
	vec     *prometheus.CounterVec
}

// prometheusGauge wraps a Prometheus gauge
type prometheusGauge struct {
	gauge prometheus.Gauge
	vec   *prometheus.GaugeVec
}

// prometheusHistogram wraps a Prometheus histogram
type prometheusHistogram struct {
	observer prometheus.Observer
	vec      *prometheus.HistogramVec
}

// prometheusSummary wraps a Prometheus summary
type prometheusSummary struct {
	observer prometheus.Observer
	vec      *prometheus.SummaryVec
}

// prometheusTimer implements the Timer interface
type prometheusTimer struct {
	start time.Time
	obs   prometheus.Observer
}

// NewMetrics creates a new metrics instance
func NewMetrics(config MetricsConfig) (Metrics, error) {
	if !config.Enabled {
		return &noopMetrics{}, nil
	}

	m := &metrics{
		config:        config,
		registry:      prometheus.NewRegistry(),
		counters:      make(map[string]*prometheusCounter),
		gauges:        make(map[string]*prometheusGauge),
		histograms:    make(map[string]*prometheusHistogram),
		summaries:     make(map[string]*prometheusSummary),
		shutdownFuncs: make([]func(context.Context) error, 0),
	}

	if err := m.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	return m, nil
}

// initialize sets up the metrics system
func (m *metrics) initialize() error {
	// Register standard collectors
	if m.config.EnableGo {
		m.registry.MustRegister(collectors.NewGoCollector())
	}

	if m.config.EnableProcess {
		m.registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}

	// Initialize built-in metrics
	m.initializeBuiltinMetrics()

	// Setup HTTP server
	if m.config.Host != "" && m.config.Port > 0 {
		m.setupHTTPServer()
	}

	return nil
}

// initializeBuiltinMetrics creates built-in metrics
func (m *metrics) initializeBuiltinMetrics() {
	namespace := m.config.Namespace
	subsystem := m.config.Subsystem

	// HTTP metrics
	if m.config.EnableHTTP {
		m.httpRequestsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "http_requests_total",
				Help:      "Total number of HTTP requests",
			},
			[]string{"method", "path", "status_code"},
		)
		m.registry.MustRegister(m.httpRequestsTotal)

		m.httpRequestDuration = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "http_request_duration_seconds",
				Help:      "HTTP request duration in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "path", "status_code"},
		)
		m.registry.MustRegister(m.httpRequestDuration)

		m.httpRequestSize = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "http_request_size_bytes",
				Help:      "HTTP request size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, 7),
			},
			[]string{"method", "path"},
		)
		m.registry.MustRegister(m.httpRequestSize)

		m.httpResponseSize = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "http_response_size_bytes",
				Help:      "HTTP response size in bytes",
				Buckets:   prometheus.ExponentialBuckets(100, 10, 7),
			},
			[]string{"method", "path"},
		)
		m.registry.MustRegister(m.httpResponseSize)

		m.httpActiveRequests = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "http_active_requests",
				Help:      "Number of active HTTP requests",
			},
			[]string{"method", "path"},
		)
		m.registry.MustRegister(m.httpActiveRequests)
	}

	// Database metrics
	if m.config.EnableDatabase {
		m.databaseConnections = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "database_connections",
				Help:      "Number of database connections",
			},
			[]string{"database", "state"},
		)
		m.registry.MustRegister(m.databaseConnections)

		m.databaseOperationsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "database_operations_total",
				Help:      "Total number of database operations",
			},
			[]string{"database", "operation"},
		)
		m.registry.MustRegister(m.databaseOperationsTotal)

		m.databaseOperationErrors = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "database_operation_errors_total",
				Help:      "Total number of database operation errors",
			},
			[]string{"database", "operation"},
		)
		m.registry.MustRegister(m.databaseOperationErrors)

		m.databaseOperationLatency = prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "database_operation_duration_seconds",
				Help:      "Database operation duration in seconds",
				Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
			},
			[]string{"database", "operation"},
		)
		m.registry.MustRegister(m.databaseOperationLatency)
	}

	// Cache metrics
	if m.config.EnableCache {
		m.cacheOperationsTotal = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "cache_operations_total",
				Help:      "Total number of cache operations",
			},
			[]string{"cache", "operation"},
		)
		m.registry.MustRegister(m.cacheOperationsTotal)

		m.cacheHits = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "cache_hits_total",
				Help:      "Total number of cache hits",
			},
			[]string{"cache"},
		)
		m.registry.MustRegister(m.cacheHits)

		m.cacheMisses = prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "cache_misses_total",
				Help:      "Total number of cache misses",
			},
			[]string{"cache"},
		)
		m.registry.MustRegister(m.cacheMisses)

		m.cacheSize = prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: subsystem,
				Name:      "cache_size_bytes",
				Help:      "Cache size in bytes",
			},
			[]string{"cache"},
		)
		m.registry.MustRegister(m.cacheSize)
	}
}

// setupHTTPServer sets up the HTTP server for metrics endpoint
func (m *metrics) setupHTTPServer() {
	mux := http.NewServeMux()

	// Add metrics endpoint
	path := m.config.Path
	if path == "" {
		path = "/metrics"
	}

	mux.Handle(path, promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	}))

	// Add health endpoint for metrics server
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	addr := fmt.Sprintf("%s:%d", m.config.Host, m.config.Port)
	m.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

// Counter returns a counter metric
func (m *metrics) Counter(name string, labels ...Label) Counter {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.buildKey(name, labels)
	if counter, exists := m.counters[key]; exists {
		return counter
	}

	labelNames := make([]string, len(labels))
	for i, label := range labels {
		labelNames[i] = label.Name
	}

	vec := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: m.config.Namespace,
			Subsystem: m.config.Subsystem,
			Name:      name,
			Help:      fmt.Sprintf("Counter metric for %s", name),
		},
		labelNames,
	)

	m.registry.MustRegister(vec)

	labelValues := make([]string, len(labels))
	for i, label := range labels {
		labelValues[i] = label.Value
	}

	counter := &prometheusCounter{
		counter: vec.WithLabelValues(labelValues...),
		vec:     vec,
	}

	m.counters[key] = counter
	return counter
}

// IncrementCounter increments a counter by a value
func (m *metrics) IncrementCounter(name string, value float64, labels ...Label) {
	counter := m.Counter(name, labels...)
	counter.Add(value)
}

// Gauge returns a gauge metric
func (m *metrics) Gauge(name string, labels ...Label) Gauge {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.buildKey(name, labels)
	if gauge, exists := m.gauges[key]; exists {
		return gauge
	}

	labelNames := make([]string, len(labels))
	for i, label := range labels {
		labelNames[i] = label.Name
	}

	vec := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: m.config.Namespace,
			Subsystem: m.config.Subsystem,
			Name:      name,
			Help:      fmt.Sprintf("Gauge metric for %s", name),
		},
		labelNames,
	)

	m.registry.MustRegister(vec)

	labelValues := make([]string, len(labels))
	for i, label := range labels {
		labelValues[i] = label.Value
	}

	gauge := &prometheusGauge{
		gauge: vec.WithLabelValues(labelValues...),
		vec:   vec,
	}

	m.gauges[key] = gauge
	return gauge
}

// SetGauge sets a gauge value
func (m *metrics) SetGauge(name string, value float64, labels ...Label) {
	gauge := m.Gauge(name, labels...)
	gauge.Set(value)
}

func (m *metrics) IncrementGauge(name string, value float64, labels ...Label) {
	gauge := m.Gauge(name, labels...)
	gauge.Add(value)
}

func (m *metrics) DecrementGauge(name string, value float64, labels ...Label) {
	gauge := m.Gauge(name, labels...)
	gauge.Sub(value)
}

// Histogram returns a histogram metric
func (m *metrics) Histogram(name string, buckets []float64, labels ...Label) Histogram {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.buildKey(name, labels)
	if histogram, exists := m.histograms[key]; exists {
		return histogram
	}

	labelNames := make([]string, len(labels))
	for i, label := range labels {
		labelNames[i] = label.Name
	}

	if len(buckets) == 0 {
		buckets = prometheus.DefBuckets
	}

	vec := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: m.config.Namespace,
			Subsystem: m.config.Subsystem,
			Name:      name,
			Help:      fmt.Sprintf("Histogram metric for %s", name),
			Buckets:   buckets,
		},
		labelNames,
	)

	m.registry.MustRegister(vec)

	labelValues := make([]string, len(labels))
	for i, label := range labels {
		labelValues[i] = label.Value
	}

	histogram := &prometheusHistogram{
		observer: vec.WithLabelValues(labelValues...),
		vec:      vec,
	}

	m.histograms[key] = histogram
	return histogram
}

// ObserveHistogram observes a histogram value
func (m *metrics) ObserveHistogram(name string, value float64, labels ...Label) {
	histogram := m.Histogram(name, nil, labels...)
	histogram.Observe(value)
}

// Summary returns a summary metric
func (m *metrics) Summary(name string, objectives map[float64]float64, labels ...Label) Summary {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.buildKey(name, labels)
	if summary, exists := m.summaries[key]; exists {
		return summary
	}

	labelNames := make([]string, len(labels))
	for i, label := range labels {
		labelNames[i] = label.Name
	}

	if len(objectives) == 0 {
		objectives = map[float64]float64{
			0.5:  0.05,  // 50th percentile with 5% tolerance
			0.9:  0.01,  // 90th percentile with 1% tolerance
			0.99: 0.001, // 99th percentile with 0.1% tolerance
		}
	}

	vec := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace:  m.config.Namespace,
			Subsystem:  m.config.Subsystem,
			Name:       name,
			Help:       fmt.Sprintf("Summary metric for %s", name),
			Objectives: objectives,
		},
		labelNames,
	)

	m.registry.MustRegister(vec)

	labelValues := make([]string, len(labels))
	for i, label := range labels {
		labelValues[i] = label.Value
	}

	summary := &prometheusSummary{
		observer: vec.WithLabelValues(labelValues...),
		vec:      vec,
	}

	m.summaries[key] = summary
	return summary
}

// ObserveSummary observes a summary value
func (m *metrics) ObserveSummary(name string, value float64, labels ...Label) {
	summary := m.Summary(name, nil, labels...)
	summary.Observe(value)
}

// HTTPMiddleware returns HTTP middleware for metrics collection
func (m *metrics) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			path := r.URL.Path
			method := r.Method

			// Track active requests
			if m.httpActiveRequests != nil {
				m.httpActiveRequests.WithLabelValues(method, path).Inc()
				defer m.httpActiveRequests.WithLabelValues(method, path).Dec()
			}

			// Track request size
			if m.httpRequestSize != nil && r.ContentLength > 0 {
				m.httpRequestSize.WithLabelValues(method, path).Observe(float64(r.ContentLength))
			}

			// Wrap response writer to capture status code and response size
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			next.ServeHTTP(wrapped, r)

			// Record metrics
			duration := time.Since(start).Seconds()
			statusCode := fmt.Sprintf("%d", wrapped.statusCode)

			if m.httpRequestsTotal != nil {
				m.httpRequestsTotal.WithLabelValues(method, path, statusCode).Inc()
			}

			if m.httpRequestDuration != nil {
				m.httpRequestDuration.WithLabelValues(method, path, statusCode).Observe(duration)
			}

			if m.httpResponseSize != nil && wrapped.bytesWritten > 0 {
				m.httpResponseSize.WithLabelValues(method, path).Observe(float64(wrapped.bytesWritten))
			}
		})
	}
}

// StartServer starts the metrics HTTP server
func (m *metrics) StartServer(ctx context.Context) error {
	if m.server == nil {
		return nil
	}

	go func() {
		if err := m.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log error but don't return it as this is in a goroutine
			fmt.Printf("Metrics server error: %v\n", err)
		}
	}()

	return nil
}

// StopServer stops the metrics HTTP server
func (m *metrics) StopServer(ctx context.Context) error {
	if m.server == nil {
		return nil
	}

	return m.server.Shutdown(ctx)
}

// Register registers a custom collector
func (m *metrics) Register(collector Collector) error {
	return m.registry.Register(collector)
}

// Unregister unregisters a custom collector
func (m *metrics) Unregister(collector Collector) error {
	ok := m.registry.Unregister(collector)
	if !ok {
		return fmt.Errorf("collector not registered")
	}
	return nil
}

// Export exports metrics
func (m *metrics) Export(ctx context.Context) error {
	// This method can be extended to export metrics to different backends
	return nil
}

// Gather gathers metrics
func (m *metrics) Gather() ([]*MetricFamily, error) {
	metricFamilies, err := m.registry.Gather()
	if err != nil {
		return nil, err
	}

	result := make([]*MetricFamily, len(metricFamilies))
	for i, mf := range metricFamilies {
		result[i] = &MetricFamily{
			Name:    mf.GetName(),
			Help:    mf.GetHelp(),
			Type:    convertMetricType(mf.GetType()),
			Metrics: convertMetrics(mf.GetMetric()),
		}
	}

	return result, nil
}

// buildKey builds a cache key for metrics
func (m *metrics) buildKey(name string, labels []Label) string {
	key := name
	for _, label := range labels {
		key += fmt.Sprintf(":%s=%s", label.Name, label.Value)
	}
	return key
}

// Counter implementation

func (c *prometheusCounter) Inc() {
	c.counter.Inc()
}

func (c *prometheusCounter) Add(value float64) {
	c.counter.Add(value)
}

func (c *prometheusCounter) Get() float64 {
	metric := &dto.Metric{}
	c.counter.Write(metric)
	return metric.GetCounter().GetValue()
}

// Gauge implementation

func (g *prometheusGauge) Set(value float64) {
	g.gauge.Set(value)
}

func (g *prometheusGauge) Inc() {
	g.gauge.Inc()
}

func (g *prometheusGauge) Dec() {
	g.gauge.Dec()
}

func (g *prometheusGauge) Add(value float64) {
	g.gauge.Add(value)
}

func (g *prometheusGauge) Sub(value float64) {
	g.gauge.Sub(value)
}

func (g *prometheusGauge) Get() float64 {
	metric := &dto.Metric{}
	g.gauge.Write(metric)
	return metric.GetGauge().GetValue()
}

// Histogram implementation

func (h *prometheusHistogram) Observe(value float64) {
	h.observer.Observe(value)
}

func (h *prometheusHistogram) ObserveWithContext(ctx context.Context, value float64) {
	h.observer.Observe(value)
}

func (h *prometheusHistogram) Timer() Timer {
	return &prometheusTimer{
		start: time.Now(),
		obs:   h.observer,
	}
}

// Summary implementation

func (s *prometheusSummary) Observe(value float64) {
	s.observer.Observe(value)
}

func (s *prometheusSummary) ObserveWithContext(ctx context.Context, value float64) {
	s.observer.Observe(value)
}

func (s *prometheusSummary) Timer() Timer {
	return &prometheusTimer{
		start: time.Now(),
		obs:   s.observer,
	}
}

// Timer implementation

func (t *prometheusTimer) ObserveDuration() {
	t.obs.Observe(time.Since(t.start).Seconds())
}

// responseWriter wraps http.ResponseWriter to capture status code and response size
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (w *responseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytesWritten += int64(n)
	return n, err
}

// Helper functions

func convertMetricType(t dto.MetricType) MetricType {
	switch t {
	case dto.MetricType_COUNTER:
		return MetricTypeCounter
	case dto.MetricType_GAUGE:
		return MetricTypeGauge
	case dto.MetricType_HISTOGRAM:
		return MetricTypeHistogram
	case dto.MetricType_SUMMARY:
		return MetricTypeSummary
	default:
		return MetricTypeCounter
	}
}

func convertMetrics(metrics []*dto.Metric) []Metric {
	result := make([]Metric, len(metrics))
	for i, m := range metrics {
		result[i] = &prometheusMetric{
			metric: m,
		}
	}
	return result
}

// prometheusMetric implements the Metric interface
type prometheusMetric struct {
	metric *dto.Metric
}

func (m *prometheusMetric) Name() string {
	return "metric" // This would need to be passed from parent
}

func (m *prometheusMetric) Type() MetricType {
	if m.metric.Counter != nil {
		return MetricTypeCounter
	} else if m.metric.Gauge != nil {
		return MetricTypeGauge
	} else if m.metric.Histogram != nil {
		return MetricTypeHistogram
	} else if m.metric.Summary != nil {
		return MetricTypeSummary
	}
	return MetricTypeCounter
}

func (m *prometheusMetric) Value() float64 {
	if m.metric.Counter != nil {
		return m.metric.Counter.GetValue()
	} else if m.metric.Gauge != nil {
		return m.metric.Gauge.GetValue()
	} else if m.metric.Histogram != nil {
		return float64(m.metric.Histogram.GetSampleCount())
	} else if m.metric.Summary != nil {
		return float64(m.metric.Summary.GetSampleCount())
	}
	return 0
}

func (m *prometheusMetric) Labels() map[string]string {
	labels := make(map[string]string)
	for _, label := range m.metric.Label {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}

func (m *prometheusMetric) Timestamp() time.Time {
	if m.metric.TimestampMs != nil {
		return time.Unix(0, m.metric.GetTimestampMs()*int64(time.Millisecond))
	}
	return time.Time{}
}

// noopMetrics is a no-op implementation for when metrics are disabled
type noopMetrics struct{}

func (n *noopMetrics) IncrementGauge(name string, value float64, labels ...Label) {
}

func (n *noopMetrics) DecrementGauge(name string, value float64, labels ...Label) {
}

func (n *noopMetrics) Counter(name string, labels ...Label) Counter                 { return &noopCounter{} }
func (n *noopMetrics) IncrementCounter(name string, value float64, labels ...Label) {}
func (n *noopMetrics) Gauge(name string, labels ...Label) Gauge                     { return &noopGauge{} }
func (n *noopMetrics) SetGauge(name string, value float64, labels ...Label)         {}
func (n *noopMetrics) Histogram(name string, buckets []float64, labels ...Label) Histogram {
	return &noopHistogram{}
}
func (n *noopMetrics) ObserveHistogram(name string, value float64, labels ...Label) {}
func (n *noopMetrics) Summary(name string, objectives map[float64]float64, labels ...Label) Summary {
	return &noopSummary{}
}
func (n *noopMetrics) ObserveSummary(name string, value float64, labels ...Label) {}
func (n *noopMetrics) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler { return h }
}
func (n *noopMetrics) StartServer(ctx context.Context) error { return nil }
func (n *noopMetrics) StopServer(ctx context.Context) error  { return nil }
func (n *noopMetrics) Register(collector Collector) error    { return nil }
func (n *noopMetrics) Unregister(collector Collector) error  { return nil }
func (n *noopMetrics) Export(ctx context.Context) error      { return nil }
func (n *noopMetrics) Gather() ([]*MetricFamily, error)      { return nil, nil }

// Noop implementations

type noopCounter struct{}

func (n *noopCounter) Inc()              {}
func (n *noopCounter) Add(value float64) {}
func (n *noopCounter) Get() float64      { return 0 }

type noopGauge struct{}

func (n *noopGauge) Set(value float64) {}
func (n *noopGauge) Inc()              {}
func (n *noopGauge) Dec()              {}
func (n *noopGauge) Add(value float64) {}
func (n *noopGauge) Sub(value float64) {}
func (n *noopGauge) Get() float64      { return 0 }

type noopHistogram struct{}

func (n *noopHistogram) Observe(value float64)                                 {}
func (n *noopHistogram) ObserveWithContext(ctx context.Context, value float64) {}
func (n *noopHistogram) Timer() Timer                                          { return &noopTimer{} }

type noopSummary struct{}

func (n *noopSummary) Observe(value float64)                                 {}
func (n *noopSummary) ObserveWithContext(ctx context.Context, value float64) {}
func (n *noopSummary) Timer() Timer                                          { return &noopTimer{} }

type noopTimer struct{}

func (n *noopTimer) ObserveDuration() {}

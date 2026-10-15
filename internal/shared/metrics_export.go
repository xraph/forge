package shared

import "time"

// MetricsExportConfig configures pushing metrics over OTLP/HTTP. Push runs
// when an endpoint is set here or through OTEL_EXPORTER_OTLP_METRICS_ENDPOINT
// or OTEL_EXPORTER_OTLP_ENDPOINT. Scraping /_/metrics is not affected.
type MetricsExportConfig struct {
	// Enabled false turns push off even when OTEL_* sets an endpoint.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// Endpoint is the full OTLP/HTTP metrics URL, for example
	// https://collector:4318/v1/metrics.
	Endpoint string            `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"  yaml:"headers,omitempty"`
	// Interval between pushes. Zero means OTEL_METRIC_EXPORT_INTERVAL, else 60s.
	Interval time.Duration `json:"interval,omitempty" yaml:"interval,omitempty"`
	// Temporality is cumulative (default), delta or lowmemory (treated as delta).
	Temporality string `json:"temporality,omitempty" yaml:"temporality,omitempty"`
}

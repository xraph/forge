package shared

// LogsExportConfig configures exporting logs over OTLP/HTTP. Export runs when
// an endpoint is set here or through OTEL_EXPORTER_OTLP_LOGS_ENDPOINT or
// OTEL_EXPORTER_OTLP_ENDPOINT. The logger's own output is not affected.
type LogsExportConfig struct {
	// Enabled false turns export off even when OTEL_* sets an endpoint.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	// Endpoint is the full OTLP/HTTP logs URL, for example
	// https://collector:4318/v1/logs.
	Endpoint string            `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"  yaml:"headers,omitempty"`
	// Level is the lowest level exported: debug, info, warn, error or fatal.
	// Empty exports every line the logger writes. It only narrows what the
	// logger already emits, so it cannot export a line the logger drops.
	Level string `json:"level,omitempty" yaml:"level,omitempty"`
}

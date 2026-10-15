package forge

import "github.com/xraph/forge/internal/shared"

// MetricsExportConfig configures pushing metrics over OTLP/HTTP. See
// shared.MetricsExportConfig for the fields.
type MetricsExportConfig = shared.MetricsExportConfig

// WithAppMetricsExportConfig sets the OTLP metrics push configuration. Values
// set here win over the "metrics_export" config key and OTEL_* environment
// variables.
func WithAppMetricsExportConfig(config MetricsExportConfig) AppOption {
	return func(c *AppConfig) {
		c.MetricsExportConfig = config
	}
}

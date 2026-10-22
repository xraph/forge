package forge

import "github.com/xraph/forge/internal/shared"

// LogsExportConfig configures exporting logs over OTLP/HTTP. See
// shared.LogsExportConfig for the fields.
type LogsExportConfig = shared.LogsExportConfig

// WithAppLogsExportConfig sets the OTLP logs export configuration. Values set
// here win over the "logs_export" config key and OTEL_* environment variables.
func WithAppLogsExportConfig(config LogsExportConfig) AppOption {
	return func(c *AppConfig) {
		c.LogsExportConfig = config
	}
}

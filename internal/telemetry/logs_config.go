package telemetry

import (
	"fmt"
	"maps"
	"strings"

	"github.com/xraph/forge/internal/shared"
)

// LogsInputs is everything ResolveLogs reads.
type LogsInputs struct {
	Code   shared.LogsExportConfig // from WithAppLogsExportConfig
	File   shared.LogsExportConfig // from the forge config "logs_export" key
	Getenv func(string) string
	// UnderTest is true in a go test binary. There only an explicit
	// Enabled true in Code or File turns export on, so a test run on a
	// machine with OTEL_EXPORTER_OTLP_ENDPOINT set doesn't ship every
	// test's lines to a real collector.
	UnderTest bool
}

// LogsSettings is the resolved logs export configuration.
type LogsSettings struct {
	Enabled  bool   // export runs
	Endpoint string // full OTLP/HTTP logs URL
	Headers  map[string]string
	// Level is "" (every written line), "debug", "info", "warn", "error" or "fatal".
	Level    string
	Warnings []string
}

// ResolveLogs applies the precedence code, forge config, OTEL_* env, default.
// Export runs only when an endpoint resolves and nothing turned it off. Under
// test it also needs an explicit Enabled true in Code or File.
func ResolveLogs(in LogsInputs) LogsSettings {
	getenv := in.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	// The result never shares the caller's headers map.
	s := LogsSettings{
		Endpoint: resolveLogsEndpoint(in, getenv),
		Headers: maps.Clone(firstMap(in.Code.Headers, in.File.Headers, parseHeaders(firstNonEmpty(
			getenv("OTEL_EXPORTER_OTLP_LOGS_HEADERS"), getenv("OTEL_EXPORTER_OTLP_HEADERS"))))),
	}

	// A bad level only matters when something is exported, so a stray value
	// doesn't log for a feature that's off.
	var exportWarnings []string

	s.Level = resolveLogsLevel(in, &exportWarnings)
	s.Enabled = resolveLogsEnabled(in, getenv, s.Endpoint, &s.Warnings)

	if s.Enabled {
		s.Warnings = append(s.Warnings, exportWarnings...)
	}

	return s
}

func resolveLogsEndpoint(in LogsInputs, getenv func(string) string) string {
	if v := firstNonEmpty(in.Code.Endpoint, in.File.Endpoint,
		strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"))); v != "" {
		return v
	}

	if base := strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); base != "" {
		return strings.TrimRight(base, "/") + "/v1/logs"
	}

	return ""
}

func resolveLogsEnabled(in LogsInputs, getenv func(string) string, endpoint string, warnings *[]string) bool {
	explicit := in.Code.Enabled
	if explicit == nil {
		explicit = in.File.Enabled
	}

	switch {
	case explicit != nil && !*explicit:
		return false
	case explicit == nil && in.UnderTest:
		// Off without a warning: a test binary is not a misconfiguration.
		return false
	case explicit == nil && (strings.EqualFold(strings.TrimSpace(getenv("OTEL_SDK_DISABLED")), "true") ||
		!exporterListAllowsOTLP("OTEL_LOGS_EXPORTER", getenv("OTEL_LOGS_EXPORTER"), warnings)):
		return false
	case endpoint == "":
		if explicit != nil {
			*warnings = append(*warnings,
				"telemetry: logs_export is enabled but no OTLP endpoint is set, so logs are not exported")
		}

		return false
	default:
		return true
	}
}

func resolveLogsLevel(in LogsInputs, warnings *[]string) string {
	raw := firstNonEmpty(strings.TrimSpace(in.Code.Level), strings.TrimSpace(in.File.Level))

	switch strings.ToLower(raw) {
	case "":
		return ""
	case "debug", "info", "error", "fatal":
		return strings.ToLower(raw)
	case "warn", "warning":
		return "warn"
	default:
		*warnings = append(*warnings, fmt.Sprintf(
			"telemetry: logs_export.level %q is not a level, exporting every written line", raw))

		return ""
	}
}

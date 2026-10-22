package telemetry

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/xraph/forge/internal/shared"
)

// Temporality values MetricsSettings carries.
const (
	TemporalityCumulative = "cumulative"
	TemporalityDelta      = "delta"
)

// defaultMetricsInterval is the OTel default export interval.
const defaultMetricsInterval = 60 * time.Second

// MetricsInputs is everything ResolveMetrics reads.
type MetricsInputs struct {
	Code   shared.MetricsExportConfig // from WithAppMetricsExportConfig
	File   shared.MetricsExportConfig // from the forge config "metrics_export" key
	Getenv func(string) string
}

// MetricsSettings is the resolved metrics push configuration.
type MetricsSettings struct {
	Enabled     bool   // push runs
	Endpoint    string // full OTLP/HTTP metrics URL
	Headers     map[string]string
	Interval    time.Duration
	Temporality string // TemporalityCumulative or TemporalityDelta
	Warnings    []string
}

// ResolveMetrics applies the precedence code, forge config, OTEL_* env,
// default. Push runs only when an endpoint resolves and nothing turned it off.
func ResolveMetrics(in MetricsInputs) MetricsSettings {
	getenv := in.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	// The result never shares the caller's headers map.
	s := MetricsSettings{
		Endpoint: resolveMetricsEndpoint(in, getenv),
		Headers: maps.Clone(firstMap(in.Code.Headers, in.File.Headers, parseHeaders(firstNonEmpty(
			getenv("OTEL_EXPORTER_OTLP_METRICS_HEADERS"), getenv("OTEL_EXPORTER_OTLP_HEADERS"))))),
	}

	// Interval and temporality warnings only matter when something is pushed,
	// so a stray env value doesn't log for a feature that is off.
	var pushWarnings []string

	s.Interval = resolveMetricsInterval(in, getenv, &pushWarnings)
	s.Temporality = resolveTemporality(in, getenv, &pushWarnings)
	s.Enabled = resolveMetricsEnabled(in, getenv, s.Endpoint, &s.Warnings)

	if s.Enabled {
		s.Warnings = append(s.Warnings, pushWarnings...)
	}

	return s
}

func resolveMetricsEndpoint(in MetricsInputs, getenv func(string) string) string {
	if v := firstNonEmpty(in.Code.Endpoint, in.File.Endpoint,
		strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"))); v != "" {
		return v
	}

	if base := strings.TrimSpace(getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); base != "" {
		return strings.TrimRight(base, "/") + "/v1/metrics"
	}

	return ""
}

func resolveMetricsEnabled(in MetricsInputs, getenv func(string) string, endpoint string, warnings *[]string) bool {
	explicit := in.Code.Enabled
	if explicit == nil {
		explicit = in.File.Enabled
	}

	switch {
	case explicit != nil && !*explicit:
		return false
	case explicit == nil && (strings.EqualFold(strings.TrimSpace(getenv("OTEL_SDK_DISABLED")), "true") ||
		!exporterListAllowsOTLP("OTEL_METRICS_EXPORTER", getenv("OTEL_METRICS_EXPORTER"), warnings)):
		return false
	case endpoint == "":
		if explicit != nil {
			*warnings = append(*warnings,
				"telemetry: metrics_export is enabled but no OTLP endpoint is set, so metrics are not pushed")
		}

		return false
	default:
		return true
	}
}

// exporterListAllowsOTLP reads an OTEL_*_EXPORTER variable, a comma-separated list
// of exporter names. An unset or blank value allows export. A list allows it only
// when it names otlp, so none, console and (for metrics) prometheus all turn it off.
// Entries OTel doesn't define for that signal are named in one warning.
func exporterListAllowsOTLP(envName, raw string, warnings *[]string) bool {
	known := []string{"otlp", "console", "none"}
	if envName == "OTEL_METRICS_EXPORTER" {
		known = []string{"otlp", "prometheus", "console", "none"}
	}

	var (
		entries []string
		unknown []string
		otlp    bool
	)

	for e := range strings.SplitSeq(raw, ",") {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" {
			continue
		}

		entries = append(entries, e)

		if e == "otlp" {
			otlp = true
		}

		if !slices.Contains(known, e) {
			unknown = append(unknown, e)
		}
	}

	if len(unknown) > 0 {
		*warnings = append(*warnings, fmt.Sprintf(
			"telemetry: %s has unknown entries %q (known: %s)",
			envName, strings.Join(unknown, ","), strings.Join(known, ", ")))
	}

	return len(entries) == 0 || otlp
}

func resolveMetricsInterval(in MetricsInputs, getenv func(string) string, warnings *[]string) time.Duration {
	for _, d := range []time.Duration{in.Code.Interval, in.File.Interval} {
		if d > 0 {
			return d
		}
	}

	raw := getenv("OTEL_METRIC_EXPORT_INTERVAL")
	if raw == "" {
		return defaultMetricsInterval
	}

	ms, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || ms <= 0 {
		*warnings = append(*warnings, fmt.Sprintf(
			"telemetry: OTEL_METRIC_EXPORT_INTERVAL=%q is not a positive number of milliseconds, using %s",
			raw, defaultMetricsInterval))

		return defaultMetricsInterval
	}

	return time.Duration(ms) * time.Millisecond
}

func resolveTemporality(in MetricsInputs, getenv func(string) string, warnings *[]string) string {
	source, raw := "metrics_export.temporality", firstNonEmpty(in.Code.Temporality, in.File.Temporality)
	if raw == "" {
		source = "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE"
		raw = getenv(source)
	}

	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", TemporalityCumulative:
		return TemporalityCumulative
	case TemporalityDelta, "lowmemory":
		return TemporalityDelta
	default:
		*warnings = append(*warnings, fmt.Sprintf(
			"telemetry: %s=%q is not cumulative, delta or lowmemory, using cumulative", source, raw))

		return TemporalityCumulative
	}
}

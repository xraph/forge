package telemetry

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xraph/forge/internal/shared"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestResolveMetrics(t *testing.T) {
	on, off := true, false

	tests := []struct {
		name        string
		in          MetricsInputs
		enabled     bool
		endpoint    string
		interval    time.Duration
		temporality string
		warning     string // substring of a warning, empty for none
	}{
		{name: "nothing set is off", in: MetricsInputs{}, interval: defaultMetricsInterval, temporality: TemporalityCumulative},
		{
			name:    "base endpoint gets the metrics path",
			in:      MetricsInputs{Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318/"})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "metrics endpoint is used as is",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT":         "http://base:4318",
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://m:9000/push",
			})},
			enabled: true, endpoint: "http://m:9000/push", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "file beats env",
			in: MetricsInputs{
				File:   shared.MetricsExportConfig{Endpoint: "http://file:4318/v1/metrics"},
				Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "http://env:4318/v1/metrics"}),
			},
			enabled: true, endpoint: "http://file:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "code beats file",
			in: MetricsInputs{
				Code: shared.MetricsExportConfig{Endpoint: "http://code:4318/v1/metrics", Interval: 5 * time.Second},
				File: shared.MetricsExportConfig{Endpoint: "http://file:4318/v1/metrics", Interval: time.Minute},
			},
			enabled: true, endpoint: "http://code:4318/v1/metrics", interval: 5 * time.Second, temporality: TemporalityCumulative,
		},
		{
			name: "interval env is milliseconds",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRIC_EXPORT_INTERVAL": "15000",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: 15 * time.Second, temporality: TemporalityCumulative,
		},
		{
			name: "bad interval env warns",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRIC_EXPORT_INTERVAL": "fast",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
			warning: "OTEL_METRIC_EXPORT_INTERVAL",
		},
		{
			name: "delta from env",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE": "Delta",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityDelta,
		},
		{
			name:    "lowmemory is delta",
			in:      MetricsInputs{Code: shared.MetricsExportConfig{Endpoint: "http://c:4318/v1/metrics", Temporality: "lowmemory"}},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityDelta,
		},
		{
			name:    "unknown temporality warns",
			in:      MetricsInputs{Code: shared.MetricsExportConfig{Endpoint: "http://c:4318/v1/metrics", Temporality: "sideways"}},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
			warning: "sideways",
		},
		{
			name: "sdk disabled",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_SDK_DISABLED": "true",
			})},
			endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "metrics exporter none",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "none",
			})},
			endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "code enabled beats env off",
			in: MetricsInputs{
				Code:   shared.MetricsExportConfig{Enabled: &on},
				Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "none"}),
			},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "file disabled beats env endpoint",
			in: MetricsInputs{
				File:   shared.MetricsExportConfig{Enabled: &off},
				Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318"}),
			},
			endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name:     "enabled without endpoint warns",
			in:       MetricsInputs{Code: shared.MetricsExportConfig{Enabled: &on}},
			interval: defaultMetricsInterval, temporality: TemporalityCumulative,
			warning: "no OTLP endpoint",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ResolveMetrics(tt.in)

			if s.Enabled != tt.enabled || s.Endpoint != tt.endpoint || s.Interval != tt.interval || s.Temporality != tt.temporality {
				t.Fatalf("got enabled=%v endpoint=%q interval=%v temporality=%q", s.Enabled, s.Endpoint, s.Interval, s.Temporality)
			}

			joined := strings.Join(s.Warnings, "\n")

			switch {
			case tt.warning == "" && joined != "":
				t.Fatalf("unexpected warnings: %s", joined)
			case tt.warning != "" && !strings.Contains(joined, tt.warning):
				t.Fatalf("warnings %q do not mention %q", joined, tt.warning)
			}
		})
	}
}

func TestResolveMetricsHeaders(t *testing.T) {
	s := ResolveMetrics(MetricsInputs{Getenv: envOf(map[string]string{
		"OTEL_EXPORTER_OTLP_HEADERS":         "x-base=1",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS": "Authorization=Basic%20abc,x-team=obs",
	})})

	want := map[string]string{"Authorization": "Basic abc", "x-team": "obs"}
	if !reflect.DeepEqual(s.Headers, want) {
		t.Fatalf("headers = %v, want %v (metrics headers win and values are URL-decoded)", s.Headers, want)
	}

	s = ResolveMetrics(MetricsInputs{
		File:   shared.MetricsExportConfig{Headers: map[string]string{"k": "file"}},
		Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_HEADERS": "k=env"}),
	})
	if s.Headers["k"] != "file" {
		t.Fatalf("headers = %v, want the file's", s.Headers)
	}
}

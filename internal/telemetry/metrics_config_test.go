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
			name: "metrics exporter prometheus is off",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "prometheus",
			})},
			endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "metrics exporter otlp with prometheus is on",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "otlp,prometheus",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "metrics exporter none with spaces is off",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": " none ",
			})},
			endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "metrics exporter uppercase otlp is on",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "OTLP",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "metrics exporter list without otlp is off and warns",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "bogus",
			})},
			endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
			warning: "bogus",
		},
		{
			name: "metrics exporter otlp with an unknown entry is on and warns",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "otlp,bogus",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
			warning: "bogus",
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
		{
			name: "non-positive intervals in code and file fall through to env",
			in: MetricsInputs{
				Code: shared.MetricsExportConfig{Interval: -time.Second},
				File: shared.MetricsExportConfig{Interval: 0},
				Getenv: envOf(map[string]string{
					"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRIC_EXPORT_INTERVAL": "15000",
				}),
			},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: 15 * time.Second, temporality: TemporalityCumulative,
		},
		{
			name: "non-positive intervals in code and file fall through to the default",
			in: MetricsInputs{
				Code:   shared.MetricsExportConfig{Endpoint: "http://c:4318/v1/metrics", Interval: -time.Second},
				File:   shared.MetricsExportConfig{Interval: 0},
				Getenv: envOf(nil),
			},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "zero interval env warns",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRIC_EXPORT_INTERVAL": "0",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
			warning: "OTEL_METRIC_EXPORT_INTERVAL",
		},
		{
			name: "negative interval env warns",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRIC_EXPORT_INTERVAL": "-5",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
			warning: "OTEL_METRIC_EXPORT_INTERVAL",
		},
		{
			name: "bad interval env with push off does not warn",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_METRIC_EXPORT_INTERVAL": "fast",
			})},
			interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "bad temporality env with push off does not warn",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE": "sideways",
			})},
			interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "sdk disabled in upper case is off",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_SDK_DISABLED": "TRUE",
			})},
			endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "sdk disabled false is on",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_SDK_DISABLED": "false",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "code temporality beats env",
			in: MetricsInputs{
				Code: shared.MetricsExportConfig{Temporality: "cumulative"},
				Getenv: envOf(map[string]string{
					"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE": "delta",
				}),
			},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "file enabled beats env off",
			in: MetricsInputs{
				File:   shared.MetricsExportConfig{Enabled: &on},
				Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_METRICS_EXPORTER": "none"}),
			},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "metrics endpoint env is trimmed",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "  http://m:9000/push \t",
			})},
			enabled: true, endpoint: "http://m:9000/push", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "base endpoint env is trimmed",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": " http://c:4318/ ",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
		},
		{
			name: "blank metrics endpoint env falls back to the base",
			in: MetricsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT": "   ", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318",
			})},
			enabled: true, endpoint: "http://c:4318/v1/metrics", interval: defaultMetricsInterval, temporality: TemporalityCumulative,
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

	s = ResolveMetrics(MetricsInputs{
		Code: shared.MetricsExportConfig{Headers: map[string]string{"k": "code"}},
		File: shared.MetricsExportConfig{Headers: map[string]string{"k": "file"}},
	})
	if !reflect.DeepEqual(s.Headers, map[string]string{"k": "code"}) {
		t.Fatalf("headers = %v, want the code's", s.Headers)
	}
}

func TestResolveMetricsHeadersAreACopy(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   func(h map[string]string) MetricsInputs
	}{
		{"code", func(h map[string]string) MetricsInputs {
			return MetricsInputs{Code: shared.MetricsExportConfig{Headers: h}}
		}},
		{"file", func(h map[string]string) MetricsInputs {
			return MetricsInputs{File: shared.MetricsExportConfig{Headers: h}}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]string{"k": "v"}

			s := ResolveMetrics(tt.in(input))
			s.Headers["k"] = "changed"
			s.Headers["extra"] = "added"

			if !reflect.DeepEqual(input, map[string]string{"k": "v"}) {
				t.Fatalf("input headers = %v after changing the result, want them untouched", input)
			}
		})
	}
}

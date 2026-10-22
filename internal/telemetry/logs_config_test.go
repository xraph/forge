package telemetry

import (
	"maps"
	"reflect"
	"strings"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

func TestResolveLogs(t *testing.T) {
	on, off := true, false

	generic := map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318"}

	withGeneric := func(extra map[string]string) func(string) string {
		m := map[string]string{}
		maps.Copy(m, generic)
		maps.Copy(m, extra)

		return envOf(m)
	}

	tests := []struct {
		name     string
		in       LogsInputs
		enabled  bool
		endpoint string
		level    string
		warnings []string // substrings, one per expected warning, in order
	}{
		{name: "nothing set is off", in: LogsInputs{}},
		{
			name:    "base endpoint gets the logs path",
			in:      LogsInputs{Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318/"})},
			enabled: true, endpoint: "http://c:4318/v1/logs",
		},
		{
			name: "logs endpoint beats the generic one and is used as is",
			in: LogsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT":      "http://base:4318",
				"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://l/x",
			})},
			enabled: true, endpoint: "http://l/x",
		},
		{
			name: "file beats env",
			in: LogsInputs{
				File:   shared.LogsExportConfig{Endpoint: "http://file:4318/v1/logs"},
				Getenv: envOf(map[string]string{"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://env:4318/v1/logs"}),
			},
			enabled: true, endpoint: "http://file:4318/v1/logs",
		},
		{
			name: "code beats file",
			in: LogsInputs{
				Code: shared.LogsExportConfig{Endpoint: "http://code:4318/v1/logs"},
				File: shared.LogsExportConfig{Endpoint: "http://file:4318/v1/logs"},
			},
			enabled: true, endpoint: "http://code:4318/v1/logs",
		},
		{
			name: "env endpoints are trimmed",
			in: LogsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "  http://l/x \t",
			})},
			enabled: true, endpoint: "http://l/x",
		},
		{
			name: "blank logs endpoint env falls back to the base",
			in: LogsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "   ", "OTEL_EXPORTER_OTLP_ENDPOINT": " http://c:4318/ ",
			})},
			enabled: true, endpoint: "http://c:4318/v1/logs",
		},
		{
			name:     "logs exporter none is off",
			in:       LogsInputs{Getenv: withGeneric(map[string]string{"OTEL_LOGS_EXPORTER": "none"})},
			endpoint: "http://c:4318/v1/logs",
		},
		{
			name:    "logs exporter otlp with console is on and quiet",
			in:      LogsInputs{Getenv: withGeneric(map[string]string{"OTEL_LOGS_EXPORTER": "otlp,console"})},
			enabled: true, endpoint: "http://c:4318/v1/logs",
		},
		{
			name:     "logs exporter console alone is off",
			in:       LogsInputs{Getenv: withGeneric(map[string]string{"OTEL_LOGS_EXPORTER": "console"})},
			endpoint: "http://c:4318/v1/logs",
		},
		{
			name:    "logs exporter otlp with an unknown entry is on and warns",
			in:      LogsInputs{Getenv: withGeneric(map[string]string{"OTEL_LOGS_EXPORTER": "otlp,bogus"})},
			enabled: true, endpoint: "http://c:4318/v1/logs",
			warnings: []string{`OTEL_LOGS_EXPORTER has unknown entries "bogus" (known: otlp, console, none)`},
		},
		{
			name:     "sdk disabled is off",
			in:       LogsInputs{Getenv: withGeneric(map[string]string{"OTEL_SDK_DISABLED": "true"})},
			endpoint: "http://c:4318/v1/logs",
		},
		{
			name: "code enabled beats sdk disabled",
			in: LogsInputs{
				Code:   shared.LogsExportConfig{Enabled: &on},
				Getenv: withGeneric(map[string]string{"OTEL_SDK_DISABLED": "true"}),
			},
			enabled: true, endpoint: "http://c:4318/v1/logs",
		},
		{
			name: "file enabled beats logs exporter none",
			in: LogsInputs{
				File:   shared.LogsExportConfig{Enabled: &on},
				Getenv: withGeneric(map[string]string{"OTEL_LOGS_EXPORTER": "none"}),
			},
			enabled: true, endpoint: "http://c:4318/v1/logs",
		},
		{
			name: "code disabled beats every env",
			in: LogsInputs{
				Code: shared.LogsExportConfig{Enabled: &off},
				Getenv: withGeneric(map[string]string{
					"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "http://l/x", "OTEL_LOGS_EXPORTER": "otlp",
				}),
			},
			endpoint: "http://l/x",
		},
		{
			name: "code disabled beats file enabled",
			in: LogsInputs{
				Code: shared.LogsExportConfig{Enabled: &off},
				File: shared.LogsExportConfig{Enabled: &on, Endpoint: "http://file:4318/v1/logs"},
			},
			endpoint: "http://file:4318/v1/logs",
		},
		{
			name: "enabled without an endpoint warns and stays off",
			in:   LogsInputs{Code: shared.LogsExportConfig{Enabled: &on}},
			warnings: []string{
				"telemetry: logs_export is enabled but no OTLP endpoint is set, so logs are not exported",
			},
		},
		{
			name:    "level is normalised",
			in:      LogsInputs{Code: shared.LogsExportConfig{Endpoint: "http://c:4318/v1/logs", Level: " WARNING "}},
			enabled: true, endpoint: "http://c:4318/v1/logs", level: "warn",
		},
		{
			name: "code level beats file level",
			in: LogsInputs{
				Code: shared.LogsExportConfig{Endpoint: "http://c:4318/v1/logs", Level: "error"},
				File: shared.LogsExportConfig{Level: "debug"},
			},
			enabled: true, endpoint: "http://c:4318/v1/logs", level: "error",
		},
		{
			name: "file level is used when code has none",
			in: LogsInputs{
				Code: shared.LogsExportConfig{Endpoint: "http://c:4318/v1/logs"},
				File: shared.LogsExportConfig{Level: "Fatal"},
			},
			enabled: true, endpoint: "http://c:4318/v1/logs", level: "fatal",
		},
		{
			name:    "unknown level warns with export on and exports everything",
			in:      LogsInputs{Code: shared.LogsExportConfig{Endpoint: "http://c:4318/v1/logs", Level: "loud"}},
			enabled: true, endpoint: "http://c:4318/v1/logs",
			warnings: []string{`telemetry: logs_export.level "loud" is not a level, exporting every written line`},
		},
		{
			name:     "unknown level with export off does not warn",
			in:       LogsInputs{Code: shared.LogsExportConfig{Endpoint: "http://c:4318/v1/logs", Enabled: &off, Level: "loud"}},
			endpoint: "http://c:4318/v1/logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ResolveLogs(tt.in)

			if s.Enabled != tt.enabled || s.Endpoint != tt.endpoint || s.Level != tt.level {
				t.Fatalf("got enabled=%v endpoint=%q level=%q, want enabled=%v endpoint=%q level=%q",
					s.Enabled, s.Endpoint, s.Level, tt.enabled, tt.endpoint, tt.level)
			}

			if len(s.Warnings) != len(tt.warnings) {
				t.Fatalf("warnings = %q, want %d containing %q", s.Warnings, len(tt.warnings), tt.warnings)
			}

			for i, w := range tt.warnings {
				if !strings.Contains(s.Warnings[i], w) {
					t.Fatalf("warning %d = %q, want it to contain %q", i, s.Warnings[i], w)
				}
			}
		})
	}
}

func TestResolveLogsUnknownExporterNamesTheKnownList(t *testing.T) {
	s := ResolveLogs(LogsInputs{Getenv: envOf(map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c:4318", "OTEL_LOGS_EXPORTER": "otlp,bogus",
	})})

	if !s.Enabled {
		t.Fatal("want export on when the list names otlp")
	}

	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], "bogus") || !strings.Contains(s.Warnings[0], "otlp, console, none") {
		t.Fatalf("warnings = %q, want one naming bogus and the known list", s.Warnings)
	}
}

func TestResolveLogsHeaders(t *testing.T) {
	env := envOf(map[string]string{
		"OTEL_EXPORTER_OTLP_HEADERS":      "k=generic,only-generic=g",
		"OTEL_EXPORTER_OTLP_LOGS_HEADERS": "a=1,Authorization=Basic%20abc",
	})

	tests := []struct {
		name string
		in   LogsInputs
		want map[string]string
	}{
		{
			name: "code wins",
			in: LogsInputs{
				Code:   shared.LogsExportConfig{Headers: map[string]string{"k": "code"}},
				File:   shared.LogsExportConfig{Headers: map[string]string{"k": "file"}},
				Getenv: env,
			},
			want: map[string]string{"k": "code"},
		},
		{
			name: "file beats env",
			in: LogsInputs{
				File:   shared.LogsExportConfig{Headers: map[string]string{"k": "file"}},
				Getenv: env,
			},
			want: map[string]string{"k": "file"},
		},
		{
			name: "logs env beats generic env and values are URL-decoded",
			in:   LogsInputs{Getenv: env},
			want: map[string]string{"a": "1", "Authorization": "Basic abc"},
		},
		{
			name: "generic env is the last resort",
			in: LogsInputs{Getenv: envOf(map[string]string{
				"OTEL_EXPORTER_OTLP_HEADERS": "b=2",
			})},
			want: map[string]string{"b": "2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveLogs(tt.in).Headers; !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("headers = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveLogsDoesNotShareHeaderMap(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   func(h map[string]string) LogsInputs
	}{
		{"code", func(h map[string]string) LogsInputs { return LogsInputs{Code: shared.LogsExportConfig{Headers: h}} }},
		{"file", func(h map[string]string) LogsInputs { return LogsInputs{File: shared.LogsExportConfig{Headers: h}} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			headers := map[string]string{"k": "v"}
			in := tt.in(headers)

			s := ResolveLogs(in)

			headers["k"] = "changed"
			headers["extra"] = "added"

			if !reflect.DeepEqual(s.Headers, map[string]string{"k": "v"}) {
				t.Fatalf("result headers = %v after changing the input, want them untouched", s.Headers)
			}

			s.Headers["k"] = "result"

			if headers["k"] != "changed" {
				t.Fatalf("input headers = %v after changing the result, want them untouched", headers)
			}
		})
	}
}

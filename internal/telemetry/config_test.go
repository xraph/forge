package telemetry

import (
	"math"
	"slices"
	"testing"

	"github.com/xraph/forge/internal/shared"
)

func env(kv map[string]string) func(string) string {
	return func(k string) string { return kv[k] }
}

func TestResolveDefaults(t *testing.T) {
	s := Resolve(Inputs{Environment: "development", Getenv: env(nil)})

	if !s.Enabled || s.Endpoint != "" || s.SampleRatio != 1.0 || s.SamplerName != "parentbased_traceidratio" {
		t.Fatalf("defaults wrong: %+v", s)
	}

	if !slices.Equal(s.Propagators, []string{"tracecontext", "baggage"}) {
		t.Fatalf("propagators = %v", s.Propagators)
	}

	if !slices.Equal(s.ExcludePaths, []string{"/_/"}) || s.ResponseHeader != "traceparent" {
		t.Fatalf("exclude/response header wrong: %+v", s)
	}
}

func TestResolveDefaultRatioByEnvironment(t *testing.T) {
	for envName, want := range map[string]float64{
		"": 1.0, "development": 1.0, "dev": 1.0, "local": 1.0, "test": 1.0,
		"production": 0.1, "staging": 0.1,
	} {
		if got := Resolve(Inputs{Environment: envName, Getenv: env(nil)}).SampleRatio; got != want {
			t.Errorf("env %q: ratio %v, want %v", envName, got, want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	in := Inputs{
		Code:        shared.TracingConfig{Endpoint: "http://code:4318/v1/traces"},
		File:        shared.TracingConfig{Endpoint: "http://file:4318/v1/traces", SampleRatio: new(0.5)},
		Environment: "production",
		Getenv: env(map[string]string{
			"OTEL_EXPORTER_OTLP_ENDPOINT": "http://env:4318",
			"OTEL_TRACES_SAMPLER":         "parentbased_traceidratio",
			"OTEL_TRACES_SAMPLER_ARG":     "0.9",
		}),
	}

	s := Resolve(in)

	if s.Endpoint != "http://code:4318/v1/traces" {
		t.Errorf("endpoint = %q, code should win", s.Endpoint)
	}

	if s.SampleRatio != 0.5 {
		t.Errorf("ratio = %v, file should beat env", s.SampleRatio)
	}
}

func TestResolveBaseEndpointGetsTracesPath(t *testing.T) {
	s := Resolve(Inputs{Getenv: env(map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://collector:4318/"})})

	if s.Endpoint != "http://collector:4318/v1/traces" {
		t.Fatalf("endpoint = %q", s.Endpoint)
	}
}

func TestResolveSignalEndpointUsedAsIs(t *testing.T) {
	s := Resolve(Inputs{Getenv: env(map[string]string{
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "https://otlp.example.com/custom",
		"OTEL_EXPORTER_OTLP_ENDPOINT":        "http://ignored:4318",
	})})

	if s.Endpoint != "https://otlp.example.com/custom" {
		t.Fatalf("endpoint = %q", s.Endpoint)
	}
}

func TestResolveHeadersFromEnv(t *testing.T) {
	s := Resolve(Inputs{Getenv: env(map[string]string{
		"OTEL_EXPORTER_OTLP_HEADERS": "authorization=Basic%20abc,x-scope=team-a",
	})})

	if s.Headers["authorization"] != "Basic abc" || s.Headers["x-scope"] != "team-a" {
		t.Fatalf("headers = %v", s.Headers)
	}
}

func TestResolveSDKDisabled(t *testing.T) {
	if Resolve(Inputs{Getenv: env(map[string]string{"OTEL_SDK_DISABLED": "true"})}).Enabled {
		t.Fatal("OTEL_SDK_DISABLED=true should disable tracing")
	}

	if !Resolve(Inputs{
		Code:   shared.TracingConfig{Enabled: new(true)},
		Getenv: env(map[string]string{"OTEL_SDK_DISABLED": "true"}),
	}).Enabled {
		t.Fatal("code enabled=true should beat OTEL_SDK_DISABLED")
	}
}

func TestResolveExplicitZeroValues(t *testing.T) {
	s := Resolve(Inputs{
		File:   shared.TracingConfig{SampleRatio: new(0.0), ResponseHeader: new("")},
		Getenv: env(nil),
	})

	if s.SampleRatio != 0 || s.ResponseHeader != "" {
		t.Fatalf("explicit zeros lost: ratio %v header %q", s.SampleRatio, s.ResponseHeader)
	}
}

func TestResolveUnsupportedSamplerFallsBack(t *testing.T) {
	s := Resolve(Inputs{Environment: "production", Getenv: env(map[string]string{"OTEL_TRACES_SAMPLER": "jaeger_remote"})})

	if s.SamplerName != "parentbased_traceidratio" || s.SampleRatio != 0.1 {
		t.Fatalf("fallback wrong: %+v", s)
	}

	if len(s.Warnings) != 1 {
		t.Fatalf("want one warning, got %v", s.Warnings)
	}
}

func TestResolveEnvSamplerNames(t *testing.T) {
	s := Resolve(Inputs{Getenv: env(map[string]string{"OTEL_TRACES_SAMPLER": "always_on"})})
	if s.SamplerName != "always_on" {
		t.Fatalf("sampler = %q", s.SamplerName)
	}
}

func TestResolveClampsRatio(t *testing.T) {
	s := Resolve(Inputs{File: shared.TracingConfig{SampleRatio: new(1.7)}, Getenv: env(nil)})

	if s.SampleRatio != 1.0 || len(s.Warnings) != 1 {
		t.Fatalf("ratio %v warnings %v", s.SampleRatio, s.Warnings)
	}
}

func TestResolveNaNRatioFallsBackToDefault(t *testing.T) {
	s := Resolve(Inputs{
		Environment: "production",
		Getenv: env(map[string]string{
			"OTEL_TRACES_SAMPLER":     "parentbased_traceidratio",
			"OTEL_TRACES_SAMPLER_ARG": "NaN",
		}),
	})

	if s.SampleRatio != 0.1 || len(s.Warnings) != 1 {
		t.Fatalf("ratio %v warnings %v", s.SampleRatio, s.Warnings)
	}
}

func TestResolveNaNFileRatioFallsBackToDefault(t *testing.T) {
	s := Resolve(Inputs{File: shared.TracingConfig{SampleRatio: new(math.NaN())}, Getenv: env(nil)})

	if s.SampleRatio != 1.0 || len(s.Warnings) != 1 {
		t.Fatalf("ratio %v warnings %v", s.SampleRatio, s.Warnings)
	}
}

func TestResolveHeaderKeepsLiteralPlus(t *testing.T) {
	s := Resolve(Inputs{Getenv: env(map[string]string{
		"OTEL_EXPORTER_OTLP_HEADERS": "authorization=Basic%20ab+c/d==",
	})})

	if s.Headers["authorization"] != "Basic ab+c/d==" {
		t.Fatalf("authorization = %q", s.Headers["authorization"])
	}
}

func TestResolvePropagatorsFromEnv(t *testing.T) {
	s := Resolve(Inputs{Getenv: env(map[string]string{"OTEL_PROPAGATORS": "tracecontext"})})

	if !slices.Equal(s.Propagators, []string{"tracecontext"}) {
		t.Fatalf("propagators = %v", s.Propagators)
	}
}

func TestResolveUnparseableSamplerArgWarns(t *testing.T) {
	s := Resolve(Inputs{
		Environment: "production",
		Getenv: env(map[string]string{
			"OTEL_TRACES_SAMPLER":     "traceidratio",
			"OTEL_TRACES_SAMPLER_ARG": "half",
		}),
	})

	want := `telemetry: OTEL_TRACES_SAMPLER_ARG="half" is not a number, using 1.0`
	if s.SampleRatio != 1.0 || !slices.Equal(s.Warnings, []string{want}) {
		t.Fatalf("ratio %v warnings %q", s.SampleRatio, s.Warnings)
	}
}

func TestResolveRequestCaptureDefaultsOff(t *testing.T) {
	s := Resolve(Inputs{Getenv: env(nil)})

	if s.RecordRawPath || s.RecordUserAgent || s.AllowedQueryKeys != nil {
		t.Fatalf("request capture is on by default: %+v", s)
	}
}

func TestResolveRequestCapturePrecedence(t *testing.T) {
	s := Resolve(Inputs{
		Code: shared.TracingConfig{RecordRawPath: new(false), AllowedQueryKeys: []string{"page"}},
		File: shared.TracingConfig{
			RecordRawPath:    new(true),
			RecordUserAgent:  new(true),
			AllowedQueryKeys: []string{"sort"},
		},
		Getenv: env(nil),
	})

	if s.RecordRawPath {
		t.Fatal("code record_raw_path: false should beat the file's true")
	}

	if !s.RecordUserAgent {
		t.Fatal("file record_user_agent: true should apply when code leaves it unset")
	}

	if !slices.Equal(s.AllowedQueryKeys, []string{"page"}) {
		t.Fatalf("allowed_query_keys = %v, want the code value", s.AllowedQueryKeys)
	}
}

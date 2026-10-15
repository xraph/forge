// Package telemetry builds forge's OpenTelemetry tracing: config resolution,
// the shared resource, the sampler, the tracer provider and the HTTP server
// middleware.
package telemetry

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/shared"
)

const defaultSamplerName = "parentbased_traceidratio"

// Inputs is everything Resolve reads.
type Inputs struct {
	Code        shared.TracingConfig // from WithAppTracingConfig
	File        shared.TracingConfig // from the forge config "tracing" key
	Environment string
	Getenv      func(string) string
}

// Settings is the resolved tracing configuration.
type Settings struct {
	Enabled        bool
	Endpoint       string // full OTLP/HTTP traces URL; empty means no export
	Headers        map[string]string
	SamplerName    string
	SampleRatio    float64
	Propagators    []string
	ExcludePaths   []string
	ResponseHeader string
	// SpanLabels labels each sampled request's goroutine with span_id and
	// span_name for the CPU profiler. Resolve never sets it; forge.New copies
	// it from the profiling settings.
	SpanLabels bool
	Resource   map[string]string
	Warnings   []string
}

var supportedSamplers = map[string]bool{
	"always_on":                true,
	"always_off":               true,
	"traceidratio":             true,
	"parentbased_always_on":    true,
	"parentbased_always_off":   true,
	"parentbased_traceidratio": true,
}

// Resolve applies the precedence code, forge config, OTEL_* env, default.
func Resolve(in Inputs) Settings {
	getenv := in.Getenv
	if getenv == nil {
		getenv = func(string) string { return "" }
	}

	s := Settings{}

	s.Enabled = resolveEnabled(in, getenv)
	s.Endpoint = resolveEndpoint(in, getenv)
	s.Headers = firstMap(in.Code.Headers, in.File.Headers, parseHeaders(firstNonEmpty(
		getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS"), getenv("OTEL_EXPORTER_OTLP_HEADERS"))))
	s.Propagators = firstSlice(in.Code.Propagators, in.File.Propagators,
		splitList(getenv("OTEL_PROPAGATORS")), []string{"tracecontext", "baggage"})
	s.ExcludePaths = firstSlice(in.Code.ExcludePaths, in.File.ExcludePaths, nil, []string{"/_/"})
	s.ResponseHeader = firstStringPtr(in.Code.ResponseHeader, in.File.ResponseHeader, "traceparent")
	s.Resource = firstMap(in.Code.Resource, in.File.Resource, nil)

	s.SamplerName, s.SampleRatio = resolveSampler(in, getenv, &s.Warnings)

	return s
}

func resolveEnabled(in Inputs, getenv func(string) string) bool {
	switch {
	case in.Code.Enabled != nil:
		return *in.Code.Enabled
	case in.File.Enabled != nil:
		return *in.File.Enabled
	case strings.EqualFold(getenv("OTEL_SDK_DISABLED"), "true"):
		return false
	default:
		return true
	}
}

func resolveEndpoint(in Inputs, getenv func(string) string) string {
	if v := firstNonEmpty(in.Code.Endpoint, in.File.Endpoint, getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")); v != "" {
		return v
	}

	if base := getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); base != "" {
		return strings.TrimRight(base, "/") + "/v1/traces"
	}

	return ""
}

func resolveSampler(in Inputs, getenv func(string) string, warnings *[]string) (string, float64) {
	ratio := defaultRatio(in.Environment)
	name := defaultSamplerName

	switch {
	case in.Code.SampleRatio != nil:
		ratio = *in.Code.SampleRatio
	case in.File.SampleRatio != nil:
		ratio = *in.File.SampleRatio
	default:
		if envName := getenv("OTEL_TRACES_SAMPLER"); envName != "" {
			if !supportedSamplers[envName] {
				*warnings = append(*warnings, fmt.Sprintf(
					"telemetry: OTEL_TRACES_SAMPLER=%q is not supported, using %s", envName, defaultSamplerName))

				break
			}

			name = envName

			if strings.HasSuffix(envName, "traceidratio") {
				ratio = 1.0 // the OTel default argument

				if arg := getenv("OTEL_TRACES_SAMPLER_ARG"); arg != "" {
					if v, err := strconv.ParseFloat(arg, 64); err == nil {
						ratio = v
					} else {
						*warnings = append(*warnings, fmt.Sprintf(
							"telemetry: OTEL_TRACES_SAMPLER_ARG=%q is not a number, using 1.0", arg))
					}
				}
			}
		}
	}

	switch {
	case math.IsNaN(ratio):
		ratio = defaultRatio(in.Environment)
		*warnings = append(*warnings, fmt.Sprintf(
			"telemetry: sample ratio NaN is not a number, using %v", ratio))
	case ratio < 0 || ratio > 1:
		*warnings = append(*warnings, fmt.Sprintf("telemetry: sample ratio %v is outside [0, 1], clamping", ratio))
		ratio = min(max(ratio, 0), 1)
	}

	return name, ratio
}

func defaultRatio(environment string) float64 {
	switch strings.ToLower(environment) {
	case "", "development", "dev", "local", "test":
		return 1.0
	default:
		return 0.1
	}
}

// parseHeaders reads the OTEL_EXPORTER_OTLP_HEADERS format: comma separated
// key=value pairs with URL-encoded values.
func parseHeaders(raw string) map[string]string {
	if raw == "" {
		return nil
	}

	out := make(map[string]string)

	for pair := range strings.SplitSeq(raw, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}

		if dec, err := url.PathUnescape(strings.TrimSpace(v)); err == nil {
			v = dec
		}

		out[strings.TrimSpace(k)] = v
	}

	return out
}

func splitList(raw string) []string {
	if raw == "" {
		return nil
	}

	var out []string

	for part := range strings.SplitSeq(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}

	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return ""
}

func firstMap(maps ...map[string]string) map[string]string {
	for _, m := range maps {
		if len(m) > 0 {
			return m
		}
	}

	return nil
}

func firstSlice(slices ...[]string) []string {
	for _, s := range slices {
		if len(s) > 0 {
			return s
		}
	}

	return nil
}

func firstStringPtr(code, file *string, def string) string {
	switch {
	case code != nil:
		return *code
	case file != nil:
		return *file
	default:
		return def
	}
}

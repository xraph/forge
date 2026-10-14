package telemetry

import sdktrace "go.opentelemetry.io/otel/sdk/trace"

// BuildSampler maps a resolved sampler name to an SDK sampler.
func BuildSampler(s Settings) sdktrace.Sampler {
	switch s.SamplerName {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(s.SampleRatio)
	case "parentbased_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(s.SampleRatio))
	}
}

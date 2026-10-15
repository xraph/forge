package telemetry

import (
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// LifecycleKey marks the root span of an app's boot or shutdown. The forge
// sampler always samples such a root unless sampling is turned off, because a
// boot happens once per process and the slow one is the one you need.
const LifecycleKey = attribute.Key("forge.lifecycle")

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

// SamplerOff reports whether the settings turn sampling off entirely.
func SamplerOff(s Settings) bool {
	switch s.SamplerName {
	case "always_off", "parentbased_always_off":
		return true
	case "always_on", "parentbased_always_on":
		return false
	default:
		return s.SampleRatio <= 0
	}
}

// forgeSampler adds two rules on top of the configured sampler: lifecycle
// roots are always sampled unless sampling is off, and a span the inner
// sampler drops is recorded (never exported) while a processor wants
// unsampled spans.
type forgeSampler struct {
	inner sdktrace.Sampler
	off   bool
	wants *atomic.Pointer[[]func() bool]
}

// NewForgeSampler wraps inner. wants holds the WantsUnsampled functions of the
// registered processors; the provider owns it and replaces it on register.
func NewForgeSampler(inner sdktrace.Sampler, off bool, wants *atomic.Pointer[[]func() bool]) sdktrace.Sampler {
	return &forgeSampler{inner: inner, off: off, wants: wants}
}

func (f *forgeSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	parent := trace.SpanContextFromContext(p.ParentContext)

	if !f.off && !parent.IsValid() && isLifecycle(p.Attributes) {
		return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample, Tracestate: parent.TraceState()}
	}

	res := f.inner.ShouldSample(p)
	if res.Decision == sdktrace.Drop && f.anyWants() {
		res.Decision = sdktrace.RecordOnly
	}

	return res
}

func (f *forgeSampler) Description() string {
	return "ForgeSampler{" + f.inner.Description() + "}"
}

func (f *forgeSampler) anyWants() bool {
	list := f.wants.Load()
	if list == nil {
		return false
	}

	for _, want := range *list {
		if want() {
			return true
		}
	}

	return false
}

func isLifecycle(attrs []attribute.KeyValue) bool {
	for _, kv := range attrs {
		if kv.Key == LifecycleKey {
			return kv.Value.AsBool()
		}
	}

	return false
}

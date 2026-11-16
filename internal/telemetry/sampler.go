package telemetry

import (
	"fmt"
	"hash/maphash"
	"math"
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
		root := sdktrace.TraceIDRatioBased(s.SampleRatio)
		if s.TrustRemoteSampling {
			return sdktrace.ParentBased(root)
		}

		// A remote parent's sampled flag comes from whoever sent the
		// request. Following it lets any client force every one of its
		// requests to be sampled and exported. So unless the callers are
		// trusted, a remote parent that says "sampled" is sampled at the
		// configured ratio instead. One that says "not sampled" is still
		// followed: a client can only lower its own share that way, and an
		// upstream proxy that already sampled keeps its traces whole. The
		// trace ID is the parent's either way, so propagation is unaffected.
		return sdktrace.ParentBased(root,
			sdktrace.WithRemoteParentSampled(NewRemoteRatioSampler(s.SampleRatio)),
		)
	}
}

// remoteRatioSampler samples a fraction of traces chosen by a keyed hash of
// the trace ID. TraceIDRatioBased reads the trace ID's low bits directly,
// which is fine for IDs this process generated but not for one a client
// sent: picking IDs whose low bits are zero would get every request sampled.
// The hash key is random per process, so the client cannot choose IDs that
// pass, while every span of one trace still gets the same decision here.
type remoteRatioSampler struct {
	seed  maphash.Seed
	bound uint64 // sample when hash < bound
	all   bool   // ratio >= 1
	desc  string
}

// NewRemoteRatioSampler returns a sampler that samples ratio of the traces it
// sees, in a way the trace ID's sender cannot steer.
func NewRemoteRatioSampler(ratio float64) sdktrace.Sampler {
	s := &remoteRatioSampler{
		seed: maphash.MakeSeed(),
		desc: fmt.Sprintf("RemoteRatioSampler{%g}", ratio),
	}

	switch {
	case ratio >= 1:
		s.all = true
	case ratio > 0:
		s.bound = uint64(ratio * math.MaxUint64)
	}

	return s
}

func (s *remoteRatioSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	ts := trace.SpanContextFromContext(p.ParentContext).TraceState()

	if s.all || maphash.Bytes(s.seed, p.TraceID[:]) < s.bound {
		return sdktrace.SamplingResult{Decision: sdktrace.RecordAndSample, Tracestate: ts}
	}

	return sdktrace.SamplingResult{Decision: sdktrace.Drop, Tracestate: ts}
}

func (s *remoteRatioSampler) Description() string { return s.desc }

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

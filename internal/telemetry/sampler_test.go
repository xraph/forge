package telemetry

import (
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func sampled(t *testing.T, s sdktrace.Sampler, parent trace.SpanContext) bool {
	t.Helper()

	ctx := trace.ContextWithRemoteSpanContext(t.Context(), parent)

	res := s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: ctx,
		TraceID:       trace.TraceID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 1},
		Name:          "x",
	})

	return res.Decision == sdktrace.RecordAndSample
}

func remoteParent(sampledFlag bool) trace.SpanContext {
	flags := trace.TraceFlags(0)
	if sampledFlag {
		flags = trace.FlagsSampled
	}

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{1},
		TraceFlags: flags,
		Remote:     true,
	})
}

func TestDefaultSamplerFollowsParent(t *testing.T) {
	s := BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 1.0})
	if sampled(t, s, remoteParent(false)) {
		t.Fatal("an unsampled parent must not be sampled, even at ratio 1.0")
	}

	s = BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 0})
	if !sampled(t, s, remoteParent(true)) {
		t.Fatal("a sampled parent must be sampled, even at ratio 0")
	}
}

func TestDefaultSamplerRootUsesRatio(t *testing.T) {
	if !sampled(t, BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 1.0}), trace.SpanContext{}) {
		t.Fatal("ratio 1.0 should sample a root span")
	}

	if sampled(t, BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 0}), trace.SpanContext{}) {
		t.Fatal("ratio 0 should drop a root span")
	}
}

func TestAlwaysOnIgnoresParent(t *testing.T) {
	if !sampled(t, BuildSampler(Settings{SamplerName: "always_on"}), remoteParent(false)) {
		t.Fatal("always_on samples regardless of parent")
	}
}

package telemetry

import (
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel/attribute"
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

	s = BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 0, TrustRemoteSampling: true})
	if !sampled(t, s, remoteParent(true)) {
		t.Fatal("with trusted callers a sampled parent must be sampled, even at ratio 0")
	}
}

// By default a remote "sampled" flag is not trusted: the sender is whoever
// made the request, and following the flag let any client have every request
// sampled and exported.
func TestDefaultSamplerDoesNotTrustRemoteSampledFlag(t *testing.T) {
	s := BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 0})
	if sampled(t, s, remoteParent(true)) {
		t.Fatal("ratio 0 must drop a remote parent that claims to be sampled")
	}

	s = BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 1.0})
	if !sampled(t, s, remoteParent(true)) {
		t.Fatal("ratio 1.0 must still sample a sampled remote parent")
	}
}

// The remote ratio cannot be steered by choosing trace IDs. TraceIDRatioBased
// samples every ID whose low bits are small, so sequential IDs, which is what
// a client forcing sampling would send, were all sampled.
func TestRemoteRatioSamplerIgnoresChosenTraceIDs(t *testing.T) {
	s := BuildSampler(Settings{SamplerName: defaultSamplerName, SampleRatio: 0.1})

	const n = 10000

	got := 0

	for i := range n {
		var id trace.TraceID

		id[15] = byte(i)
		id[14] = byte(i >> 8)

		parent := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID: id, SpanID: trace.SpanID{1}, TraceFlags: trace.FlagsSampled, Remote: true,
		})

		res := s.ShouldSample(sdktrace.SamplingParameters{
			ParentContext: trace.ContextWithRemoteSpanContext(t.Context(), parent),
			TraceID:       id,
			Name:          "x",
		})

		if res.Decision == sdktrace.RecordAndSample {
			got++
		}
	}

	// 10% of 10000 is 1000; the binomial standard deviation is 30.
	if got < 850 || got > 1150 {
		t.Fatalf("sampled %d of %d remote parents with chosen IDs at ratio 0.1, want about 1000", got, n)
	}
}

// One trace gets one decision, so a trace that is kept is kept whole here.
func TestRemoteRatioSamplerIsConsistentPerTrace(t *testing.T) {
	s := NewRemoteRatioSampler(0.5)

	for i := range 200 {
		id := trace.TraceID{byte(i), 7, byte(i * 3)}
		p := sdktrace.SamplingParameters{TraceID: id, ParentContext: t.Context()}

		first := s.ShouldSample(p).Decision
		for range 5 {
			if s.ShouldSample(p).Decision != first {
				t.Fatalf("trace %v got different decisions", id)
			}
		}
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

func decide(t *testing.T, s sdktrace.Sampler, parent trace.SpanContext, attrs ...attribute.KeyValue) sdktrace.SamplingDecision {
	t.Helper()

	ctx := t.Context()
	if parent.IsValid() {
		ctx = trace.ContextWithRemoteSpanContext(ctx, parent)
	}

	return s.ShouldSample(sdktrace.SamplingParameters{
		ParentContext: ctx,
		// TraceIDRatioBased reads the low 8 bytes; all 0xff is above every
		// ratio below 1, so a plain root drops unless the ratio is 1.
		TraceID:    trace.TraceID{8: 0xff, 9: 0xff, 10: 0xff, 11: 0xff, 12: 0xff, 13: 0xff, 14: 0xff, 15: 0xff},
		Name:       "x",
		Attributes: attrs,
	}).Decision
}

func wantsList(fns ...func() bool) *atomic.Pointer[[]func() bool] {
	p := &atomic.Pointer[[]func() bool]{}
	if len(fns) > 0 {
		p.Store(&fns)
	}

	return p
}

func TestForgeSampler_LifecycleRootAlwaysSampled(t *testing.T) {
	settings := Settings{SamplerName: defaultSamplerName, SampleRatio: 0.1}
	s := NewForgeSampler(BuildSampler(settings), SamplerOff(settings), wantsList())

	if got := decide(t, s, trace.SpanContext{}, LifecycleKey.Bool(true)); got != sdktrace.RecordAndSample {
		t.Fatalf("lifecycle root at ratio 0.1: %v, want RecordAndSample", got)
	}

	// decide's trace ID is above the 0.1 threshold, so a plain root drops.
	if got := decide(t, s, trace.SpanContext{}); got != sdktrace.Drop {
		t.Fatalf("plain root at ratio 0.1: %v, want Drop", got)
	}
}

func TestForgeSampler_LifecycleRespectsSamplingOff(t *testing.T) {
	for _, settings := range []Settings{
		{SamplerName: defaultSamplerName, SampleRatio: 0},
		{SamplerName: "traceidratio", SampleRatio: 0},
		{SamplerName: "always_off"},
		{SamplerName: "parentbased_always_off"},
	} {
		s := NewForgeSampler(BuildSampler(settings), SamplerOff(settings), wantsList())
		if got := decide(t, s, trace.SpanContext{}, LifecycleKey.Bool(true)); got != sdktrace.Drop {
			t.Errorf("%s ratio %v: lifecycle root %v, want Drop", settings.SamplerName, settings.SampleRatio, got)
		}
	}
}

func TestForgeSampler_DropBecomesRecordOnlyWhileWanted(t *testing.T) {
	settings := Settings{SamplerName: defaultSamplerName, SampleRatio: 0}

	var want atomic.Bool

	s := NewForgeSampler(BuildSampler(settings), SamplerOff(settings), wantsList(want.Load))

	if got := decide(t, s, trace.SpanContext{}); got != sdktrace.Drop {
		t.Fatalf("not wanted: %v, want Drop", got)
	}

	want.Store(true)

	if got := decide(t, s, trace.SpanContext{}); got != sdktrace.RecordOnly {
		t.Fatalf("wanted: %v, want RecordOnly", got)
	}

	want.Store(false)

	if got := decide(t, s, trace.SpanContext{}); got != sdktrace.Drop {
		t.Fatalf("no longer wanted: %v, want Drop", got)
	}
}

func TestForgeSampler_UnsampledParentRecordOnlyWhileWanted(t *testing.T) {
	settings := Settings{SamplerName: defaultSamplerName, SampleRatio: 1}
	s := NewForgeSampler(BuildSampler(settings), SamplerOff(settings), wantsList(func() bool { return true }))

	if got := decide(t, s, remoteParent(false)); got != sdktrace.RecordOnly {
		t.Fatalf("child of an unsampled caller: %v, want RecordOnly", got)
	}

	if got := decide(t, s, remoteParent(true)); got != sdktrace.RecordAndSample {
		t.Fatalf("child of a sampled caller: %v, want RecordAndSample", got)
	}
}

func TestForgeSampler_LifecycleChildIsNotForced(t *testing.T) {
	settings := Settings{SamplerName: defaultSamplerName, SampleRatio: 1}
	s := NewForgeSampler(BuildSampler(settings), SamplerOff(settings), wantsList())

	// Only roots are forced. A lifecycle-tagged span under an unsampled parent
	// follows the parent.
	if got := decide(t, s, remoteParent(false), LifecycleKey.Bool(true)); got != sdktrace.Drop {
		t.Fatalf("lifecycle child of unsampled parent: %v, want Drop", got)
	}
}

func BenchmarkForgeSampler_NothingRegistered(b *testing.B) {
	settings := Settings{SamplerName: defaultSamplerName, SampleRatio: 0}
	bare := BuildSampler(settings)
	wrapped := NewForgeSampler(bare, SamplerOff(settings), wantsList())
	p := sdktrace.SamplingParameters{ParentContext: b.Context(), TraceID: trace.TraceID{0xff, 1}, Name: "x"}

	b.Run("bare", func(b *testing.B) {
		for b.Loop() {
			_ = bare.ShouldSample(p)
		}
	})
	b.Run("wrapped", func(b *testing.B) {
		for b.Loop() {
			_ = wrapped.ShouldSample(p)
		}
	})
}

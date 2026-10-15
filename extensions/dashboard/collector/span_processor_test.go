package collector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func newProcessorTP(t testing.TB, p *SpanProcessor, sampler sdktrace.Sampler) trace.Tracer {
	t.Helper()

	if sampler == nil {
		sampler = sdktrace.AlwaysSample()
	}

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sampler), sdktrace.WithSpanProcessor(p))

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	return tp.Tracer("test")
}

func flush(t *testing.T, p *SpanProcessor) {
	t.Helper()

	if err := p.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestSpanProcessor_StoresSpansWithRealIDs(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	p := NewSpanProcessor(store, "/dashboard")
	tr := newProcessorTP(t, p, nil)

	ctx, root := tr.Start(t.Context(), "GET /users/:id", trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("http.request.method", "GET"), attribute.String("url.path", "/users/7")))
	_, child := tr.Start(ctx, "SELECT users")
	child.End()
	root.End()

	flush(t, p)

	traceID := root.SpanContext().TraceID().String()

	d := store.GetTrace(traceID)
	if d == nil || d.SpanCount != 2 {
		t.Fatalf("trace %s = %+v", traceID, d)
	}

	if len(traceID) != 32 || d.RootSpan.SpanID != root.SpanContext().SpanID().String() {
		t.Fatalf("root = %+v", d.RootSpan)
	}

	if d.RootSpan.Kind != SpanKindServer || d.Protocol != "REST" {
		t.Fatalf("kind %v protocol %q", d.RootSpan.Kind, d.Protocol)
	}

	for _, s := range d.Spans {
		if s.Name == "SELECT users" && (s.ParentSpanID != d.RootSpan.SpanID || s.Depth != 1) {
			t.Fatalf("child not nested: %+v", s)
		}
	}
}

func TestSpanProcessor_JoinsExchange(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	p := NewSpanProcessor(store, "/dashboard")
	tr := newProcessorTP(t, p, nil)

	_, span := tr.Start(t.Context(), "POST /orders")
	store.AttachExchange(span.SpanContext().SpanID().String(), &HTTPExchange{Request: HTTPMessage{Body: `{"a":1}`}})
	span.End()

	flush(t, p)

	d := store.GetTrace(span.SpanContext().TraceID().String())
	if d == nil || d.RootSpan.HTTP == nil || d.RootSpan.HTTP.Request.Body != `{"a":1}` {
		t.Fatalf("exchange not joined: %+v", d)
	}
}

func TestConvertSpan_RemoteParentIsRoot(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	p := NewSpanProcessor(store, "/dashboard")
	tr := newProcessorTP(t, p, nil)

	remote := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: trace.TraceID{7}, SpanID: trace.SpanID{9}, TraceFlags: trace.FlagsSampled, Remote: true,
	})

	_, span := tr.Start(trace.ContextWithRemoteSpanContext(t.Context(), remote), "GET /x")
	span.End()

	flush(t, p)

	summaries, _ := store.ListTraces(TraceFilter{})
	if len(summaries) != 1 || summaries[0].RootSpanName != "GET /x" {
		t.Fatalf("summaries = %+v", summaries)
	}

	d := store.GetTrace(summaries[0].TraceID)
	if d.RootSpan.ParentSpanID != "" || d.RootSpan.Attributes["parent.remote_span_id"] != remote.SpanID().String() {
		t.Fatalf("root = %+v", d.RootSpan)
	}
}

func TestConvertSpan_ErrorStatusProtocolAndCaps(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	p := NewSpanProcessor(store, "/dashboard")
	tr := newProcessorTP(t, p, nil)

	attrs := []attribute.KeyValue{attribute.String("rpc.system", "grpc"), attribute.String("long", strings.Repeat("v", 5000))}
	for i := range 100 {
		attrs = append(attrs, attribute.Int(fmt.Sprintf("k%03d", i), i))
	}

	_, span := tr.Start(t.Context(), strings.Repeat("n", 5000), trace.WithAttributes(attrs...))
	for i := range 50 {
		span.AddEvent("e", trace.WithAttributes(attribute.Int("i", i)))
	}

	span.RecordError(errors.New("boom"))
	span.SetStatus(codes.Error, "boom")
	span.End()

	flush(t, p)

	d := store.GetTrace(span.SpanContext().TraceID().String())
	s := d.RootSpan

	if s.Status != SpanStatusError || s.Attributes["error"] != "boom" {
		t.Fatalf("status %v error %q", s.Status, s.Attributes["error"])
	}

	if d.Protocol != "gRPC" {
		t.Fatalf("protocol %q, want gRPC", d.Protocol)
	}

	if len(s.Name) > MaxAttrValueLen {
		t.Fatalf("name not truncated: %d bytes", len(s.Name))
	}

	if v, ok := s.Attributes["long"]; ok && len(v) > MaxAttrValueLen {
		t.Fatalf("attribute not truncated: %d bytes", len(v))
	}

	// 64 copied from the span, plus at most error and protocol added here.
	if len(s.Attributes) > maxSpanAttributes+3 {
		t.Fatalf("attributes = %d, cap %d", len(s.Attributes), maxSpanAttributes)
	}

	if len(s.Events) > maxSpanEvents {
		t.Fatalf("events = %d, cap %d", len(s.Events), maxSpanEvents)
	}
}

func TestSpanProcessor_SkipsDashboardNoise(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	p := NewSpanProcessor(store, "/dashboard")
	tr := newProcessorTP(t, p, nil)

	_, span := tr.Start(t.Context(), "GET", trace.WithAttributes(attribute.String("url.path", "/dashboard/static/app.js")))
	span.End()

	_, internal := tr.Start(t.Context(), "forge.start")
	internal.End()

	flush(t, p)

	summaries, _ := store.ListTraces(TraceFilter{})
	if len(summaries) != 1 || summaries[0].RootSpanName != "forge.start" || summaries[0].Protocol != "Internal" {
		t.Fatalf("summaries = %+v", summaries)
	}
}

func TestSpanProcessor_ClosedGateStoresNothing(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	store.SetIngestGate(func() bool { return false })

	p := NewSpanProcessor(store, "/dashboard")
	tr := newProcessorTP(t, p, nil)

	if p.WantsUnsampled() {
		t.Fatal("closed gate wants unsampled spans")
	}

	_, span := tr.Start(t.Context(), "x")
	span.End()

	flush(t, p)

	if summaries, _ := store.ListTraces(TraceFilter{}); len(summaries) != 0 {
		t.Fatalf("closed gate stored %d traces", len(summaries))
	}
}

func TestSpanProcessor_FullQueueDropsInsteadOfBlocking(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	p := newSpanProcessor(store, "/dashboard", false) // drain goroutine not started
	tr := newProcessorTP(t, p, nil)

	done := make(chan struct{})

	go func() {
		defer close(done)

		for range spanQueueSize + 10 {
			_, s := tr.Start(context.Background(), "x")
			s.End()
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("OnEnd blocked on a full queue")
	}

	if got := store.DroppedSpans(); got < 10 {
		t.Fatalf("dropped = %d, want at least 10", got)
	}
}

func TestSpanProcessor_OnEndAfterShutdown(t *testing.T) {
	store := NewTraceStore(10, time.Hour)
	p := NewSpanProcessor(store, "/dashboard")

	// A provider that does not own p, the way the app's provider still holds
	// it after the dashboard stopped.
	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	tp.RegisterSpanProcessor(p)
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := p.Shutdown(t.Context()); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}

	_, span := tp.Tracer("t").Start(t.Context(), "late")
	span.End()

	if err := p.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}

	if p.WantsUnsampled() {
		t.Fatal("a stopped processor still wants spans")
	}
}

func BenchmarkSpanProcessor(b *testing.B) {
	run := func(b *testing.B, opts ...sdktrace.TracerProviderOption) {
		tp := sdktrace.NewTracerProvider(append([]sdktrace.TracerProviderOption{sdktrace.WithSampler(sdktrace.AlwaysSample())}, opts...)...)

		b.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

		tr := tp.Tracer("bench")
		ctx := b.Context()

		b.ReportAllocs()

		for b.Loop() {
			_, s := tr.Start(ctx, "x")
			s.End()
		}
	}

	b.Run("no-processor", func(b *testing.B) { run(b) })
	b.Run("gate-closed", func(b *testing.B) {
		store := NewTraceStore(10, time.Hour)
		store.SetIngestGate(func() bool { return false })
		run(b, sdktrace.WithSpanProcessor(NewSpanProcessor(store, "/dashboard")))
	})
	b.Run("gate-open", func(b *testing.B) {
		run(b, sdktrace.WithSpanProcessor(NewSpanProcessor(NewTraceStore(1000, time.Hour), "/dashboard")))
	})
}

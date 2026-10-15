package forge

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge/internal/logger"
)

// keepExporter keeps its spans through Shutdown. The in-memory exporter
// clears itself on Shutdown, and Stop shuts the provider down.
type keepExporter struct{ *tracetest.InMemoryExporter }

func (keepExporter) Shutdown(context.Context) error { return nil }

func newLifecycleApp(t *testing.T, ratio float64, parallel bool, exts ...Extension) (App, *tracetest.InMemoryExporter) {
	t.Helper()

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_SDK_DISABLED", "")

	opts := []AppOption{
		WithAppName("lifecycle"),
		WithAppLogger(logger.NewTestLogger()),
		WithAppTracingConfig(TracingConfig{SampleRatio: &ratio}),
	}
	if parallel {
		opts = append(opts, WithParallelExtensionStartup())
	}

	a := New(opts...)

	for _, ext := range exts {
		if err := a.RegisterExtension(ext); err != nil {
			t.Fatal(err)
		}
	}

	exp := tracetest.NewInMemoryExporter()
	if err := RegisterSpanProcessor(a, sdktrace.NewSimpleSpanProcessor(keepExporter{exp})); err != nil {
		t.Fatal(err)
	}

	return a, exp
}

func spanByName(spans tracetest.SpanStubs, name string) *tracetest.SpanStub {
	for i := range spans {
		if spans[i].Name == name {
			return &spans[i]
		}
	}

	return nil
}

func TestLifecycleSpans_StartAndStop(t *testing.T) {
	// Ratio 0.1 with a lifecycle root still yields a full boot trace.
	a, exp := newLifecycleApp(t, 0.1, false, newStartupExt("alpha", nil), newStartupExt("beta", []string{"alpha"}))

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}

	spans := exp.GetSpans()

	root := spanByName(spans, "forge.start")
	if root == nil {
		t.Fatalf("no forge.start span in %v", spans)
	}

	for _, name := range []string{"register alpha", "start alpha", "register beta", "start beta"} {
		s := spanByName(spans, name)
		if s == nil {
			t.Fatalf("no %q span", name)
		}

		if s.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Errorf("%q is not a child of forge.start", name)
		}
	}

	stop := spanByName(spans, "forge.stop")
	if stop == nil {
		t.Fatal("no forge.stop span: Stop did not flush its own span")
	}

	for _, name := range []string{"stop alpha", "stop beta"} {
		s := spanByName(spans, name)
		if s == nil || s.Parent.SpanID() != stop.SpanContext.SpanID() {
			t.Errorf("%q missing or not under forge.stop", name)
		}
	}

	alpha := spanByName(spans, "start alpha")
	if got := attrString(alpha, "forge.extension.name"); got != "alpha" {
		t.Errorf("forge.extension.name = %q", got)
	}
}

func TestLifecycleSpans_FailedStartIsError(t *testing.T) {
	bad := newStartupExt("bad", nil)
	bad.failOn = true

	a, exp := newLifecycleApp(t, 1, false, bad)

	if err := a.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded")
	}

	spans := exp.GetSpans()

	for _, name := range []string{"start bad", "forge.start"} {
		s := spanByName(spans, name)
		if s == nil || s.Status.Code != codes.Error {
			t.Errorf("%q missing or not Error: %+v", name, s)
		}
	}
}

func TestLifecycleSpans_ParallelPanicIsError(t *testing.T) {
	a1 := newStartupExt("calm", nil)
	b1 := newStartupExt("wild", nil)
	b1.panicOn = true

	a, exp := newLifecycleApp(t, 1, true, a1, b1)

	if err := a.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded despite a panic")
	}

	s := spanByName(exp.GetSpans(), "start wild")
	if s == nil || s.Status.Code != codes.Error {
		t.Fatalf("start wild missing or not Error: %+v", s)
	}
}

func TestLifecycleSpans_SerialPanicStillPanics(t *testing.T) {
	wild := newStartupExt("wild", nil)
	wild.panicOn = true

	a, exp := newLifecycleApp(t, 1, false, wild)

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("serial Start no longer panics")
			}
		}()

		_ = a.Start(t.Context())
	}()

	if s := spanByName(exp.GetSpans(), "start wild"); s == nil || s.Status.Code != codes.Error {
		t.Fatalf("start wild missing or not Error: %+v", s)
	}
}

func TestLifecycleSpans_OffWhenSamplingOff(t *testing.T) {
	a, exp := newLifecycleApp(t, 0, false, newStartupExt("alpha", nil))

	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	_ = a.Stop(t.Context())

	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("sample_ratio 0 still exported %d spans", n)
	}
}

func attrString(s *tracetest.SpanStub, key string) string {
	if s == nil {
		return ""
	}

	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value.AsString()
		}
	}

	return ""
}

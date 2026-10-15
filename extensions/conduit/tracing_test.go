package conduit_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/conduit"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
	"github.com/xraph/forge/internal/shared"
)

func installSpans(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())

		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	return exp
}

func spanNamed(t *testing.T, exp *tracetest.InMemoryExporter, name string) sdktrace.ReadOnlySpan {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range exp.GetSpans().Snapshots() {
			if s.Name() == name {
				return s
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("no span %q in %v", name, exp.GetSpans().Snapshots())

	return nil
}

// scrapeUntil polls the Prometheus scrape until it contains want, because a
// consumer span ends and its duration is recorded after the handler returns.
func scrapeUntil(t *testing.T, m forge.Metrics, want string) string {
	t.Helper()

	var body string

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		m.(shared.PrometheusProvider).PrometheusHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

		body = rec.Body.String()
		if strings.Contains(body, want) {
			return body
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("scrape never contained %q:\n%s", want, body)

	return ""
}

func TestConduitCreateSendProcessIsOneTrace(t *testing.T) {
	m := forge.New(forge.WithAppName("conduit-trace")).Metrics()
	exp := installSpans(t)

	r := runtimeFor(t, config("billing", "one", false, conduit.Competing), memory.New())
	if err := r.RegisterMetrics(m); err != nil {
		t.Fatal(err)
	}

	seen := make(chan trace.SpanContext, 1)

	if err := conduit.Subscribe(r, placed, func(ctx context.Context, _ conduit.Message[order]) error {
		seen <- trace.SpanContextFromContext(ctx)

		return nil
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	start(t, r)

	if _, err := conduit.Publish(t.Context(), r, placed, order{ID: "1"}); err != nil {
		t.Fatal(err)
	}

	handler := <-seen
	create, send, process := spanNamed(t, exp, "create orders"), spanNamed(t, exp, "send orders"), spanNamed(t, exp, "process orders")

	if process.Parent().SpanID() != create.SpanContext().SpanID() || handler.SpanID() != process.SpanContext().SpanID() {
		t.Fatal("process is not create's child, or the handler doesn't see the process span")
	}

	if len(send.Links()) != 1 || send.Links()[0].SpanContext.SpanID() != create.SpanContext().SpanID() {
		t.Fatalf("send links %v", send.Links())
	}

	// The consumer span ends and its duration is recorded after the handler
	// returns, so wait for that series before asserting on the others.
	body := scrapeUntil(t, m, `messaging_process_duration_seconds_count{`)

	for _, want := range []string{`messaging_client_operation_duration_seconds_count{`, `messaging_process_duration_seconds_count{`, `messaging_system="memory"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("scrape lacks %s", want)
		}
	}
}

// TestConduitLaterSendJoinsCreatingTrace stands in for the outbox relay:
// Prepare runs in the caller's trace, Send runs later on an unrelated ctx.
func TestConduitLaterSendJoinsCreatingTrace(t *testing.T) {
	exp := installSpans(t)
	r := runtimeFor(t, config("billing", "one", false, conduit.Competing), memory.New())

	seen := make(chan trace.SpanContext, 1)

	if err := conduit.Subscribe(r, placed, func(ctx context.Context, _ conduit.Message[order]) error {
		seen <- trace.SpanContextFromContext(ctx)

		return nil
	}, conduit.Consumer("process")); err != nil {
		t.Fatal(err)
	}

	start(t, r)

	callerCtx, caller := otel.Tracer("test").Start(t.Context(), "request")
	pub, err := conduit.Prepare(callerCtx, r, placed, order{ID: "1"})

	caller.End()

	if err != nil {
		t.Fatal(err)
	}

	if _, err := r.Send(context.Background(), pub.Stream, pub.Envelope); err != nil {
		t.Fatal(err)
	}

	if got := <-seen; got.TraceID() != caller.SpanContext().TraceID() {
		t.Fatal("the handler didn't join the trace that created the message")
	}

	if s := spanNamed(t, exp, "send orders"); s.Parent().IsValid() {
		t.Fatal("a relay send has no caller span, so it should be a root linked to create")
	}
}

func TestConduitRPCClientAndServerSpans(t *testing.T) {
	exp := installSpans(t)
	broker := memory.New()

	server := rpcRuntime(t, "billing", "a", broker, conduit.RPCConfig{})
	if err := conduit.Handle(server, double, func(_ context.Context, req conduit.Request[rpcInput]) (rpcOutput, error) {
		return rpcOutput{Value: req.Data.Value * 2}, nil
	}); err != nil {
		t.Fatal(err)
	}

	start(t, server)

	client := rpcRuntime(t, "client", "caller", broker, conduit.RPCConfig{})
	start(t, client)

	if _, err := conduit.Call(t.Context(), client, "billing", double, rpcInput{Value: 2}); err != nil {
		t.Fatal(err)
	}

	call := spanNamed(t, exp, "call billing.billing.double.v1")
	serve := spanNamed(t, exp, "serve billing.billing.double.v1")

	if call.SpanKind() != trace.SpanKindClient || serve.SpanKind() != trace.SpanKindServer || serve.Parent().SpanID() != call.SpanContext().SpanID() {
		t.Fatalf("call %v serve %v parent %v", call.SpanKind(), serve.SpanKind(), serve.Parent())
	}
}

func TestConduitRPCFailureMarksBothSpans(t *testing.T) {
	exp := installSpans(t)
	broker := memory.New()

	server := rpcRuntime(t, "billing", "a", broker, conduit.RPCConfig{})
	if err := conduit.Handle(server, double, func(context.Context, conduit.Request[rpcInput]) (rpcOutput, error) {
		return rpcOutput{}, errors.New("boom")
	}); err != nil {
		t.Fatal(err)
	}

	start(t, server)

	client := rpcRuntime(t, "client", "caller", broker, conduit.RPCConfig{})
	start(t, client)

	if _, err := conduit.Call(t.Context(), client, "billing", double, rpcInput{Value: 2}); err == nil {
		t.Fatal("want an error")
	}

	for _, name := range []string{"call billing.billing.double.v1", "serve billing.billing.double.v1"} {
		if code := spanNamed(t, exp, name).Status().Code; code != codes.Error {
			t.Fatalf("%s status %v, want Error", name, code)
		}
	}
}

func TestConduitRegisterMetricsAfterStart(t *testing.T) {
	m := forge.New(forge.WithAppName("conduit-late")).Metrics()
	r := runtimeFor(t, config("billing", "one", false, conduit.Competing), memory.New())

	start(t, r)

	if err := r.RegisterMetrics(m); err == nil {
		t.Fatal("RegisterMetrics after Start should fail")
	}
}

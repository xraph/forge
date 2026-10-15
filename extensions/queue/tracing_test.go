package queue

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
	"github.com/xraph/forge/internal/shared"
)

func installTracing(t *testing.T) *tracetest.InMemoryExporter {
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

// tracedService builds a QueueService over the inmemory backend with a real
// metrics registry, and declares queue "orders".
func tracedService(t *testing.T, mutate func(*Config)) (*QueueService, forge.Metrics, *tracetest.InMemoryExporter) {
	t.Helper()

	m := forge.New(forge.WithAppName("queue-trace")).Metrics()
	exp := installTracing(t)

	cfg := DefaultConfig()
	cfg.Driver = "inmemory"

	if mutate != nil {
		mutate(&cfg)
	}

	backend := NewInMemoryQueue(cfg, forge.NewNoopLogger(), m)
	if err := backend.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := backend.DeclareQueue(t.Context(), "orders", DefaultQueueOptions()); err != nil {
		t.Fatal(err)
	}

	return NewQueueService(cfg, backend, forge.NewNoopLogger(), m), m, exp
}

func consumeOne(t *testing.T, svc *QueueService, handler MessageHandler) {
	t.Helper()

	done := make(chan struct{}, 1)

	if err := svc.Consume(t.Context(), "orders", func(ctx context.Context, msg Message) error {
		defer func() { done <- struct{}{} }()

		return handler(ctx, msg)
	}, DefaultConsumeOptions()); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}
}

// waitSpans waits for n finished spans. The handler's own return runs before the consumer
// span ends, so a test that reads the exporter straight after the handler would race.
func waitSpans(t *testing.T, exp *tracetest.InMemoryExporter, n int) tracetest.SpanStubs {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for len(exp.GetSpans()) < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}

	spans := exp.GetSpans()
	if len(spans) < n {
		t.Fatalf("got %d spans, want %d", len(spans), n)
	}

	return spans
}

func scrapeQueue(t *testing.T, m forge.Metrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	m.(shared.PrometheusProvider).PrometheusHandler().
		ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	return rec.Body.String()
}

func TestQueueTraceFromPublishToHandler(t *testing.T) {
	svc, m, exp := tracedService(t, nil)

	if err := svc.Publish(t.Context(), "orders", Message{Body: []byte("x")}); err != nil {
		t.Fatal(err)
	}

	var handlerSpan trace.SpanContext

	consumeOne(t, svc, func(ctx context.Context, _ Message) error {
		handlerSpan = trace.SpanContextFromContext(ctx)

		return nil
	})

	spans := waitSpans(t, exp, 2).Snapshots()
	if len(spans) != 2 || spans[0].Name() != "publish orders" || spans[1].Name() != "process orders" {
		t.Fatalf("spans %v", spans)
	}

	if spans[1].Parent().SpanID() != spans[0].SpanContext().SpanID() || handlerSpan.SpanID() != spans[1].SpanContext().SpanID() {
		t.Fatal("process span is not the publish span's child, or the handler doesn't see it")
	}

	// The consumer span ends just before its duration is observed, so give the scrape a moment.
	body := scrapeQueue(t, m)
	for deadline := time.Now().Add(2 * time.Second); !strings.Contains(body, `messaging_process_duration_seconds_count{`) &&
		time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)

		body = scrapeQueue(t, m)
	}

	for _, want := range []string{
		`messaging_client_operation_duration_seconds_count{`, `messaging_process_duration_seconds_count{`, `messaging_system="memory"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("scrape lacks %s:\n%s", want, body)
		}
	}
}

func TestQueueHandlerErrorMarksSpan(t *testing.T) {
	svc, _, exp := tracedService(t, nil)

	_ = svc.Publish(t.Context(), "orders", Message{Body: []byte("x")})
	consumeOne(t, svc, func(context.Context, Message) error { return errors.New("nope") })

	if s := waitSpans(t, exp, 2).Snapshots()[1]; s.Status().Code != codes.Error {
		t.Fatalf("status %v", s.Status())
	}
}

func TestQueuePublishDoesNotMutateHeaders(t *testing.T) {
	svc, _, _ := tracedService(t, nil)
	headers := map[string]string{"tenant": "a"}

	ctx, span := otel.Tracer("test").Start(t.Context(), "caller")
	defer span.End()

	for range 3 {
		if err := svc.Publish(ctx, "orders", Message{Body: []byte("x"), Headers: headers}); err != nil {
			t.Fatal(err)
		}
	}

	if len(headers) != 1 {
		t.Fatalf("caller's headers mutated: %v", headers)
	}
}

func TestQueueNilHeaders(t *testing.T) {
	svc, _, exp := tracedService(t, nil)

	// A message published straight to the backend, as an old publisher would, has no trace headers.
	if err := svc.queue.Publish(t.Context(), "orders", Message{Body: []byte("x")}); err != nil {
		t.Fatal(err)
	}

	consumeOne(t, svc, func(context.Context, Message) error { return nil })

	if s := waitSpans(t, exp, 1).Snapshots()[0]; s.Parent().IsValid() {
		t.Fatal("process span without headers should be a root")
	}
}

func TestQueueFlagsGateSpansAndMetrics(t *testing.T) {
	svc, m, exp := tracedService(t, func(c *Config) {
		c.EnableTracing = false
		c.EnableMetrics = false
	})

	ctx, span := otel.Tracer("test").Start(t.Context(), "caller")
	_ = svc.Publish(ctx, "orders", Message{Body: []byte("x")})

	span.End()

	var got trace.SpanContext

	consumeOne(t, svc, func(ctx context.Context, _ Message) error {
		got = trace.SpanContextFromContext(ctx)

		return nil
	})

	if n := len(exp.GetSpans()); n != 1 { // only "caller"
		t.Fatalf("spans = %d, want 1", n)
	}

	if got.TraceID() != span.SpanContext().TraceID() {
		t.Fatal("trace context didn't pass through with tracing off")
	}

	if strings.Contains(scrapeQueue(t, m), "messaging_") {
		t.Fatal("metrics recorded with EnableMetrics false")
	}
}

func TestQueuePublishBatchSpansEachMessage(t *testing.T) {
	svc, _, exp := tracedService(t, nil)

	if err := svc.PublishBatch(t.Context(), "orders", []Message{{Body: []byte("a")}, {Body: []byte("b")}}); err != nil {
		t.Fatal(err)
	}

	if n := len(waitSpans(t, exp, 2)); n != 2 {
		t.Fatalf("spans = %d, want 2", n)
	}

	if err := svc.PublishBatch(t.Context(), "missing", []Message{{Body: []byte("a")}}); err == nil {
		t.Fatal("publish to an undeclared queue should fail with the backend's error")
	}
}

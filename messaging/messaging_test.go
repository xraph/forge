package messaging_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/go-utils/log"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/forge/messaging"
)

func installTracing(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())))
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

func newMetrics(t *testing.T) forge.Metrics {
	t.Helper()

	return forge.New(forge.WithAppName("messaging-test")).Metrics()
}

func scrape(t *testing.T, m forge.Metrics) string {
	t.Helper()

	rec := httptest.NewRecorder()
	m.(shared.PrometheusProvider).PrometheusHandler().
		ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	return rec.Body.String()
}

// seriesValue returns the value of the first sample line of name whose labels
// include every pair in want, and false when there is none.
func seriesValue(body, name string, want map[string]string) (string, bool) {
	for line := range strings.SplitSeq(body, "\n") {
		if !strings.HasPrefix(line, name+"{") {
			continue
		}

		ok := true

		for k, v := range want {
			if !strings.Contains(line, fmt.Sprintf(`%s=%q`, k, v)) {
				ok = false

				break
			}
		}

		if ok {
			return line[strings.LastIndex(line, " ")+1:], true
		}
	}

	return "", false
}

func attrs(s sdktrace.ReadOnlySpan) map[string]string {
	out := map[string]string{}
	for _, kv := range s.Attributes() {
		out[string(kv.Key)] = kv.Value.String()
	}

	return out
}

func TestSendSpanShape(t *testing.T) {
	exp := installTracing(t)
	mt := messaging.New("memory")

	_, op := mt.StartSend(t.Context(), "orders", "publish")
	op.End(nil)

	spans := exp.GetSpans().Snapshots()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}

	s := spans[0]
	if s.Name() != "publish orders" || s.SpanKind() != trace.SpanKindProducer {
		t.Fatalf("name %q kind %v", s.Name(), s.SpanKind())
	}

	got := attrs(s)
	for k, v := range map[string]string{
		"messaging.system": "memory", "messaging.destination.name": "orders",
		"messaging.operation.name": "publish", "messaging.operation.type": "send",
	} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}

	if _, has := got["error.type"]; has {
		t.Error("error.type set on success")
	}
}

func TestCreateSpanShape(t *testing.T) {
	exp := installTracing(t)

	_, op := messaging.New("kafka").StartCreate(t.Context(), "orders")
	op.End(nil)

	s := exp.GetSpans().Snapshots()[0]
	if s.Name() != "create orders" || s.SpanKind() != trace.SpanKindProducer || attrs(s)["messaging.operation.type"] != "create" {
		t.Fatalf("name %q kind %v attrs %v", s.Name(), s.SpanKind(), attrs(s))
	}
}

func TestEmptyDestinationNamesSpanByOperation(t *testing.T) {
	exp := installTracing(t)

	_, op := messaging.New("kafka").StartSend(t.Context(), "", "send")
	op.End(nil)

	s := exp.GetSpans().Snapshots()[0]
	if s.Name() != "send" {
		t.Fatalf("name %q", s.Name())
	}

	if _, has := attrs(s)["messaging.destination.name"]; has {
		t.Error("destination set for an empty destination")
	}
}

func TestProcessIsChildOfInjectedContext(t *testing.T) {
	exp := installTracing(t)

	sendCtx, send := messaging.New("redis").StartSend(t.Context(), "orders", "publish")
	carrier := propagation.MapCarrier{}
	messaging.Inject(sendCtx, carrier)
	send.End(nil)

	// A separate Telemetry stands in for another service: only the carrier links them.
	procCtx, proc := messaging.New("redis").StartProcess(context.Background(), carrier, "orders", "process")
	proc.End(nil)

	spans := exp.GetSpans().Snapshots()
	producer, consumer := spans[0], spans[1]

	if consumer.SpanKind() != trace.SpanKindConsumer || consumer.Name() != "process orders" {
		t.Fatalf("consumer name %q kind %v", consumer.Name(), consumer.SpanKind())
	}

	if consumer.Parent().SpanID() != producer.SpanContext().SpanID() || consumer.SpanContext().TraceID() != producer.SpanContext().TraceID() {
		t.Fatal("consumer is not the producer's child")
	}

	if log.TraceIDFromContext(procCtx) != consumer.SpanContext().TraceID().String() ||
		log.SpanIDFromContext(procCtx) != consumer.SpanContext().SpanID().String() {
		t.Fatal("process ctx lacks log correlation IDs")
	}
}

type brokerError struct{}

func (*brokerError) Error() string { return "broker down" }

func TestEndRecordsError(t *testing.T) {
	m := newMetrics(t) // forge.New installs its own tracer provider, so build it first
	exp := installTracing(t)

	_, op := messaging.New("kafka", messaging.WithMetrics(m)).StartSend(t.Context(), "orders", "send")
	op.End(fmt.Errorf("send failed: %w", &brokerError{}))

	s := exp.GetSpans().Snapshots()[0]
	if s.Status().Code != codes.Error || attrs(s)["error.type"] != "*messaging_test.brokerError" {
		t.Fatalf("status %v error.type %q", s.Status(), attrs(s)["error.type"])
	}

	if v, ok := seriesValue(scrape(t, m), "messaging_client_operation_duration_seconds_count",
		map[string]string{"error_type": "*messaging_test.brokerError", "messaging_system": "kafka"}); !ok || v != "1" {
		t.Fatalf("count %q found %v", v, ok)
	}
}

func TestContextErrorTypes(t *testing.T) {
	exp := installTracing(t)
	mt := messaging.New("memory")

	for _, err := range []error{context.DeadlineExceeded, fmt.Errorf("wrapped: %w", context.Canceled)} {
		_, op := mt.StartSend(t.Context(), "orders", "publish")
		op.End(err)
	}

	spans := exp.GetSpans().Snapshots()
	if attrs(spans[0])["error.type"] != "context.DeadlineExceeded" || attrs(spans[1])["error.type"] != "context.Canceled" {
		t.Fatalf("got %q and %q", attrs(spans[0])["error.type"], attrs(spans[1])["error.type"])
	}
}

func TestLinksBatchAndKind(t *testing.T) {
	exp := installTracing(t)
	mt := messaging.New("kafka")

	createCtx, create := mt.StartCreate(t.Context(), "orders")
	create.End(nil)

	link := trace.Link{SpanContext: trace.SpanContextFromContext(createCtx)}

	_, op := mt.StartSend(t.Context(), "orders", "call",
		messaging.WithLinks(link), messaging.WithBatchSize(3), messaging.WithSpanKind(trace.SpanKindClient))
	op.End(nil)

	s := exp.GetSpans().Snapshots()[1]
	if s.SpanKind() != trace.SpanKindClient || attrs(s)["messaging.batch.message_count"] != "3" {
		t.Fatalf("kind %v attrs %v", s.SpanKind(), attrs(s))
	}

	if len(s.Links()) != 1 || s.Links()[0].SpanContext.SpanID() != link.SpanContext.SpanID() {
		t.Fatalf("links %v", s.Links())
	}
}

func TestTracingOffStillPropagates(t *testing.T) {
	exp := installTracing(t)

	parentCtx, parent := otel.Tracer("test").Start(t.Context(), "caller")
	carrier := propagation.MapCarrier{}
	messaging.Inject(parentCtx, carrier)
	parent.End()

	mt := messaging.New("memory", messaging.WithTracing(false))

	sendCtx, send := mt.StartSend(parentCtx, "orders", "publish")
	send.End(nil)

	if trace.SpanContextFromContext(sendCtx).SpanID() != trace.SpanContextFromContext(parentCtx).SpanID() {
		t.Fatal("StartSend with tracing off changed the span on ctx")
	}

	procCtx, proc := mt.StartProcess(context.Background(), carrier, "orders", "process")
	proc.End(nil)

	if trace.SpanContextFromContext(procCtx).TraceID() != trace.SpanContextFromContext(parentCtx).TraceID() {
		t.Fatal("StartProcess with tracing off dropped the remote trace")
	}

	if n := len(exp.GetSpans()); n != 1 { // only the test's own "caller" span
		t.Fatalf("spans = %d, want 1", n)
	}
}

func TestNilTelemetryAndOperation(t *testing.T) {
	installTracing(t)

	var mt *messaging.Telemetry

	ctx := t.Context()
	if got, op := mt.StartSend(ctx, "orders", "publish"); got != ctx || op != nil {
		t.Fatal("nil Telemetry StartSend changed ctx or returned an operation")
	}

	var op *messaging.Operation
	op.End(errors.New("ignored"))

	called := false
	if err := mt.Process(ctx, propagation.MapCarrier{}, "orders", "process", func(context.Context) error {
		called = true

		return nil
	}); err != nil || !called {
		t.Fatalf("err %v called %v", err, called)
	}
}

func TestEndTwiceRecordsOnce(t *testing.T) {
	m := newMetrics(t) // forge.New installs its own tracer provider, so build it first
	installTracing(t)

	_, op := messaging.New("memory", messaging.WithMetrics(m)).StartSend(t.Context(), "orders", "publish")
	op.End(nil)
	op.End(nil)

	if v, _ := seriesValue(scrape(t, m), "messaging_client_operation_duration_seconds_count",
		map[string]string{"messaging_system": "memory"}); v != "1" {
		t.Fatalf("count %q, want 1", v)
	}
}

func TestHistogramsLabelsAndBuckets(t *testing.T) {
	m := newMetrics(t) // forge.New installs its own tracer provider, so build it first
	installTracing(t)

	mt := messaging.New("rabbitmq", messaging.WithMetrics(m))

	_, send := mt.StartSend(t.Context(), "orders", "publish")
	send.End(nil)

	_, proc := mt.StartProcess(t.Context(), propagation.MapCarrier{}, "orders", "process")
	proc.End(nil)

	body := scrape(t, m)
	want := map[string]string{
		"messaging_system": "rabbitmq", "messaging_destination_name": "orders",
		"messaging_operation_name": "publish", "messaging_operation_type": "send",
	}

	if _, ok := seriesValue(body, "messaging_client_operation_duration_seconds_bucket", map[string]string{"le": "0.005", "messaging_system": "rabbitmq"}); !ok {
		t.Fatalf("no 0.005 bucket:\n%s", body)
	}

	if v, ok := seriesValue(body, "messaging_client_operation_duration_seconds_count", want); !ok || v != "1" {
		t.Fatalf("send count %q:\n%s", v, body)
	}

	if v, ok := seriesValue(body, "messaging_process_duration_seconds_count",
		map[string]string{"messaging_operation_name": "process", "messaging_operation_type": "process"}); !ok || v != "1" {
		t.Fatalf("process count %q:\n%s", v, body)
	}
}

func TestProcessPanicEndsSpanAndRepanics(t *testing.T) {
	m := newMetrics(t) // forge.New installs its own tracer provider, so build it first
	exp := installTracing(t)
	mt := messaging.New("memory", messaging.WithMetrics(m))

	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("recovered %v, want boom", r)
			}
		}()

		_ = mt.Process(t.Context(), propagation.MapCarrier{}, "orders", "process", func(context.Context) error { panic("boom") })
	}()

	s := exp.GetSpans().Snapshots()[0]
	if s.Status().Code != codes.Error || attrs(s)["error.type"] != "*messaging.PanicError" {
		t.Fatalf("status %v error.type %q", s.Status(), attrs(s)["error.type"])
	}

	if v, _ := seriesValue(scrape(t, m), "messaging_process_duration_seconds_count",
		map[string]string{"error_type": "*messaging.PanicError"}); v != "1" {
		t.Fatalf("count %q, want 1", v)
	}
}

type sentinelError struct{ code int }

func (e *sentinelError) Error() string { return fmt.Sprintf("sentinel %d", e.code) }

func TestPanicErrorUnwrapsAnErrorValue(t *testing.T) {
	sentinel := errors.New("sentinel")

	var err error = &messaging.PanicError{Value: sentinel}
	if !errors.Is(err, sentinel) {
		t.Fatal("errors.Is can't reach the panicked error")
	}

	typed := &sentinelError{code: 7}

	var target *sentinelError
	if !errors.As(fmt.Errorf("wrapped: %w", &messaging.PanicError{Value: typed}), &target) || target != typed {
		t.Fatal("errors.As can't reach the panicked error")
	}

	if errors.Unwrap(&messaging.PanicError{Value: "boom"}) != nil {
		t.Fatal("a non-error panic value should not unwrap")
	}
}

func TestPanicWithAnErrorValueStillReportsPanicError(t *testing.T) {
	m := newMetrics(t)
	exp := installTracing(t)
	mt := messaging.New("memory", messaging.WithMetrics(m))

	func() {
		defer func() { _ = recover() }()

		_ = mt.Process(t.Context(), propagation.MapCarrier{}, "orders", "process",
			func(context.Context) error { panic(context.Canceled) })
	}()

	if got := attrs(exp.GetSpans().Snapshots()[0])["error.type"]; got != "*messaging.PanicError" {
		t.Fatalf("error.type %q, want *messaging.PanicError", got)
	}

	if v, _ := seriesValue(scrape(t, m), "messaging_process_duration_seconds_count",
		map[string]string{"error_type": "*messaging.PanicError"}); v != "1" {
		t.Fatalf("count %q, want 1", v)
	}
}

func TestProcessWithoutOrMalformedTraceparent(t *testing.T) {
	exp := installTracing(t)
	mt := messaging.New("memory")

	for _, carrier := range []propagation.MapCarrier{nil, {"traceparent": "not-a-traceparent"}} {
		err := mt.Process(t.Context(), carrier, "orders", "process", func(context.Context) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, s := range exp.GetSpans().Snapshots() {
		if s.Parent().IsValid() {
			t.Fatalf("span %q has a parent, want a root", s.Name())
		}
	}
}

func TestUnsampledParentStaysUnsampled(t *testing.T) {
	exp := installTracing(t)

	carrier := propagation.MapCarrier{"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"}

	ctx, op := messaging.New("memory").StartProcess(context.Background(), carrier, "orders", "process")
	op.End(nil)

	if n := len(exp.GetSpans()); n != 0 {
		t.Fatalf("exported %d spans, want 0", n)
	}

	if got := trace.SpanContextFromContext(ctx).TraceID().String(); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace ID %s", got)
	}

	if log.TraceIDFromContext(ctx) != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatal("no log correlation for an unsampled trace")
	}
}

func TestWithTraceContextNeverMutates(t *testing.T) {
	installTracing(t)

	if got := messaging.WithTraceContext(t.Context(), nil); got != nil {
		t.Fatalf("no span: got %v, want nil", got)
	}

	ctx, span := otel.Tracer("test").Start(t.Context(), "caller")
	defer span.End()

	in := map[string]string{"k": "v"}
	out := messaging.WithTraceContext(ctx, in)

	if len(in) != 1 || out["k"] != "v" || out["traceparent"] == "" {
		t.Fatalf("in %v out %v", in, out)
	}
}

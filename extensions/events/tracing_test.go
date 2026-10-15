package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events/brokers"
	"github.com/xraph/forge/extensions/events/core"
	"github.com/xraph/forge/extensions/events/stores"
	"github.com/xraph/forge/internal/shared"
	"github.com/xraph/forge/messaging"
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

// tracedBus starts a bus over one memory broker per name, with tracing and a
// real metrics registry. With several brokers there is no default, so Publish
// fans out to all of them.
func tracedBus(t *testing.T, brokerNames ...string) (*EventBusImpl, forge.Metrics, *tracetest.InMemoryExporter) {
	t.Helper()

	m := forge.New(forge.WithAppName("events-trace")).Metrics()
	exp := installTracing(t)

	defaultBroker := ""
	if len(brokerNames) == 1 {
		defaultBroker = brokerNames[0]
	}

	b, err := NewEventBus(EventBusOptions{
		Store: stores.NewMemoryEventStore(nil, nil), HandlerRegistry: core.NewHandlerRegistry(nil, nil),
		Metrics: m, Config: EventBusConfig{DefaultBroker: defaultBroker, EnableTracing: true, EnableMetrics: true},
	})
	require.NoError(t, err)

	bus := b.(*EventBusImpl)
	for _, name := range brokerNames {
		require.NoError(t, bus.RegisterBroker(name, brokers.NewMemoryBroker(forge.NewNoopLogger(), nil)))
	}

	require.NoError(t, bus.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, bus.Stop(context.Background())) })

	return bus, m, exp
}

func waitSpans(t *testing.T, exp *tracetest.InMemoryExporter, n int) []sdktrace.ReadOnlySpan {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if spans := exp.GetSpans().Snapshots(); len(spans) >= n {
			return spans
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("got %d spans, want %d", len(exp.GetSpans()), n)

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

func byKind(spans []sdktrace.ReadOnlySpan, kind trace.SpanKind) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan

	for _, s := range spans {
		if s.SpanKind() == kind {
			out = append(out, s)
		}
	}

	return out
}

func TestEventTraceFromPublishToHandler(t *testing.T) {
	bus, m, exp := tracedBus(t, "memory")

	seen := make(chan trace.SpanContext, 1)

	require.NoError(t, bus.Subscribe("order.placed", core.EventHandlerFunc(func(ctx context.Context, _ *core.Event) error {
		seen <- trace.SpanContextFromContext(ctx)

		return nil
	})))

	require.NoError(t, bus.Publish(t.Context(), core.NewEvent("order.placed", "o-1", "x")))

	spans := waitSpans(t, exp, 2)
	producer, consumer := byKind(spans, trace.SpanKindProducer)[0], byKind(spans, trace.SpanKindConsumer)[0]

	require.Equal(t, "publish order.placed", producer.Name())
	require.Equal(t, "process order.placed", consumer.Name())
	require.Equal(t, producer.SpanContext().SpanID(), consumer.Parent().SpanID())
	require.Equal(t, consumer.SpanContext().SpanID(), (<-seen).SpanID())

	body := scrapeUntil(t, m, "messaging_process_duration_seconds_count{")

	require.Contains(t, body, "messaging_client_operation_duration_seconds_count{")
	require.NotContains(t, body, "events_publish_duration")
	require.NotContains(t, body, "events_published_total")
	require.NotContains(t, body, "events_broker_publish_duration")
}

func TestEventTwoBrokersEachGetTheirOwnSendSpan(t *testing.T) {
	bus, _, exp := tracedBus(t, "memory", "second")

	var wg sync.WaitGroup

	wg.Add(2)
	require.NoError(t, bus.Subscribe("order.placed", core.EventHandlerFunc(func(context.Context, *core.Event) error {
		wg.Done()

		return nil
	})))

	event := core.NewEvent("order.placed", "o-1", "x")
	require.NoError(t, bus.Publish(t.Context(), event))
	wg.Wait()

	spans := waitSpans(t, exp, 4)
	producers, consumers := byKind(spans, trace.SpanKindProducer), byKind(spans, trace.SpanKindConsumer)
	require.Len(t, producers, 2)
	require.Len(t, consumers, 2)
	require.NotEqual(t, consumers[0].Parent().SpanID(), consumers[1].Parent().SpanID())
	require.NotContains(t, event.Metadata, "traceparent", "the caller's event was modified")
}

func TestEventNilMetadata(t *testing.T) {
	bus, _, exp := tracedBus(t, "memory")

	done := make(chan struct{})

	require.NoError(t, bus.Subscribe("order.placed", core.EventHandlerFunc(func(context.Context, *core.Event) error {
		close(done)

		return nil
	})))

	event := core.NewEvent("order.placed", "o-1", "x")
	event.Metadata = nil

	// Straight to the broker, as an old publisher would: no trace metadata.
	require.NoError(t, bus.brokers["memory"].Publish(t.Context(), event.Type, *event))
	<-done

	consumer := byKind(waitSpans(t, exp, 1), trace.SpanKindConsumer)[0]
	require.False(t, consumer.Parent().IsValid())
}

func TestTraceMetadataSurvivesJSON(t *testing.T) {
	installTracing(t)

	ctx, span := otel.Tracer("test").Start(t.Context(), "caller")
	defer span.End()

	event := withTraceMetadata(ctx, core.NewEvent("order.placed", "o-1", "x"))

	raw, err := json.Marshal(event)
	require.NoError(t, err)

	var decoded core.Event
	require.NoError(t, json.Unmarshal(raw, &decoded))

	got := trace.SpanContextFromContext(messaging.Extract(context.Background(), metadataCarrier(decoded.Metadata)))
	require.Equal(t, span.SpanContext().TraceID(), got.TraceID())
}

type logicalHandler struct{ core.EventHandlerFunc }

func (logicalHandler) Name() string        { return "billing" }
func (logicalHandler) LogicalName() string { return "billing-group" }

func TestTracedHandlerKeepsNames(t *testing.T) {
	plain := (&EventBusImpl{}).traced("memory", core.EventHandlerFunc(func(context.Context, *core.Event) error { return nil }))
	require.Equal(t, "anonymous-handler", plain.Name())
	require.Equal(t, "anonymous-handler", plain.(interface{ LogicalName() string }).LogicalName())

	logical := (&EventBusImpl{}).traced("memory", logicalHandler{})
	require.Equal(t, "billing", logical.Name())
	require.Equal(t, "billing-group", logical.(interface{ LogicalName() string }).LogicalName())
}

func TestUnsubscribeStillStopsDelivery(t *testing.T) {
	bus, _, _ := tracedBus(t, "memory")

	calls := make(chan struct{}, 2)
	handler := logicalHandler{EventHandlerFunc: func(context.Context, *core.Event) error {
		calls <- struct{}{}

		return nil
	}}

	require.NoError(t, bus.Subscribe("order.placed", handler))
	require.NoError(t, bus.Unsubscribe("order.placed", "billing"))
	require.NoError(t, bus.Publish(t.Context(), core.NewEvent("order.placed", "o-1", "x")))

	select {
	case <-calls:
		t.Fatal("handler ran after Unsubscribe")
	case <-time.After(100 * time.Millisecond):
	}
}

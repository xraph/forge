package kafka

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/IBM/sarama/mocks"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
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

func tracedClient(t *testing.T) (*kafkaClient, forge.Metrics, *tracetest.InMemoryExporter) {
	t.Helper()

	m := forge.New(forge.WithAppName("kafka-trace")).Metrics()
	exp := installTracing(t)
	cfg := DefaultConfig()

	return &kafkaClient{
		config: cfg, logger: logger.NewNoopLogger(), metrics: m,
		telemetry: newTelemetry(cfg, m),
	}, m, exp
}

func TestKafkaSendMessageContextInjectsAndSpans(t *testing.T) {
	client, m, exp := tracedClient(t)
	producer := mocks.NewSyncProducer(t, nil)
	client.producer = producer

	producer.ExpectSendMessageWithMessageCheckerFunctionAndSucceed(func(msg *sarama.ProducerMessage) error {
		got := trace.SpanContextFromContext(messaging.Extract(context.Background(), ProducerHeaderCarrier(msg)))
		if !got.IsValid() {
			t.Error("no traceparent header")
		}

		return nil
	})

	if err := client.SendMessageContext(t.Context(), "orders", nil, []byte("x")); err != nil {
		t.Fatal(err)
	}

	if s := exp.GetSpans().Snapshots()[0]; s.Name() != "send orders" || s.SpanKind() != trace.SpanKindProducer {
		t.Fatalf("span %q %v", s.Name(), s.SpanKind())
	}

	rec := httptest.NewRecorder()
	m.(shared.PrometheusProvider).PrometheusHandler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))

	if body := rec.Body.String(); !strings.Contains(body, "messaging_client_operation_duration_seconds_count{") || strings.Contains(body, "kafka_messages_sent") {
		t.Fatalf("scrape:\n%s", body)
	}
}

func TestKafkaSendMessageAsyncContextInjectsSendSpan(t *testing.T) {
	client, _, exp := tracedClient(t)
	producer := mocks.NewAsyncProducer(t, nil)
	client.asyncProducer = producer

	injected := make(chan trace.SpanContext, 1)

	producer.ExpectInputWithMessageCheckerFunctionAndSucceed(func(msg *sarama.ProducerMessage) error {
		injected <- trace.SpanContextFromContext(messaging.Extract(context.Background(), ProducerHeaderCarrier(msg)))

		return nil
	})

	parentCtx, parent := otel.Tracer("test").Start(t.Context(), "caller")
	defer parent.End()

	if err := client.SendMessageAsyncContext(parentCtx, "orders", nil, []byte("x")); err != nil {
		t.Fatal(err)
	}

	if err := producer.Close(); err != nil {
		t.Fatal(err)
	}

	got := <-injected

	var send sdktrace.ReadOnlySpan

	for _, s := range exp.GetSpans().Snapshots() {
		if s.Name() == "send orders" && s.SpanKind() == trace.SpanKindProducer {
			send = s
		}
	}

	if send == nil {
		t.Fatal("no send span")
	}

	if send.Parent().SpanID() != parent.SpanContext().SpanID() {
		t.Fatalf("send span parent %v, want the caller span", send.Parent())
	}

	if got.SpanID() != send.SpanContext().SpanID() {
		t.Fatalf("record carries span %s, want the send span %s", got.SpanID(), send.SpanContext().SpanID())
	}
}

func TestKafkaSendMessagesBatchShape(t *testing.T) {
	client, _, exp := tracedClient(t)
	producer := mocks.NewSyncProducer(t, nil)
	client.producer = producer

	parents := map[trace.SpanID]bool{}

	for range 3 {
		producer.ExpectSendMessageWithMessageCheckerFunctionAndSucceed(func(msg *sarama.ProducerMessage) error {
			parents[trace.SpanContextFromContext(messaging.Extract(context.Background(), ProducerHeaderCarrier(msg))).SpanID()] = true

			return nil
		})
	}

	err := client.SendMessagesContext(t.Context(), []*ProducerMessage{
		{Topic: "orders", Value: []byte("1")}, {Topic: "orders", Value: []byte("2")}, {Topic: "orders", Value: []byte("3")},
	})
	if err != nil {
		t.Fatal(err)
	}

	var creates, sends []sdktrace.ReadOnlySpan

	for _, s := range exp.GetSpans().Snapshots() {
		switch s.Name() {
		case "create orders":
			creates = append(creates, s)
		case "send orders":
			sends = append(sends, s)
		}
	}

	if len(creates) != 3 || len(sends) != 1 || len(sends[0].Links()) != 3 || len(parents) != 3 {
		t.Fatalf("creates %d sends %d links %d distinct record parents %d", len(creates), len(sends), len(sends[0].Links()), len(parents))
	}

	for _, kv := range sends[0].Attributes() {
		if kv.Key == "messaging.batch.message_count" && kv.Value.AsInt64() != 3 {
			t.Fatalf("batch count %d", kv.Value.AsInt64())
		}
	}
}

func TestKafkaMixedTopicBatchHasNoDestination(t *testing.T) {
	client, _, exp := tracedClient(t)
	producer := mocks.NewSyncProducer(t, nil)
	client.producer = producer

	producer.ExpectSendMessageAndSucceed()
	producer.ExpectSendMessageAndSucceed()

	if err := client.SendMessagesContext(t.Context(), []*ProducerMessage{{Topic: "a"}, {Topic: "b"}}); err != nil {
		t.Fatal(err)
	}

	for _, s := range exp.GetSpans().Snapshots() {
		if s.SpanKind() == trace.SpanKindProducer && s.Name() == "send" {
			return
		}
	}

	t.Fatal("no send span named \"send\" for a mixed-topic batch")
}

func consumeOneKafka(t *testing.T, client *kafkaClient, msg *sarama.ConsumerMessage, ctxHandler ContextMessageHandler, plain MessageHandler) {
	t.Helper()

	consumer := mocks.NewConsumer(t, nil)
	consumer.ExpectConsumePartition("orders", 0, sarama.OffsetOldest).YieldMessage(msg)
	client.consumer = consumer

	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)

	var err error
	if ctxHandler != nil {
		err = client.ConsumePartitionContext(ctx, "orders", 0, sarama.OffsetOldest, ctxHandler)
	} else {
		err = client.ConsumePartition(ctx, "orders", 0, sarama.OffsetOldest, plain)
	}

	if err != nil {
		t.Fatal(err)
	}
}

func waitConsumerSpan(t *testing.T, exp *tracetest.InMemoryExporter) sdktrace.ReadOnlySpan {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range exp.GetSpans().Snapshots() {
			if s.SpanKind() == trace.SpanKindConsumer {
				return s
			}
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatal("no consumer span")

	return nil
}

func TestKafkaConsumerSpanIsChildForBothHandlerKinds(t *testing.T) {
	for _, withCtx := range []bool{true, false} {
		client, _, exp := tracedClient(t)

		parentCtx, parent := otel.Tracer("test").Start(t.Context(), "producer")
		msg := &sarama.ConsumerMessage{Topic: "orders", Value: []byte("x")}
		messaging.Inject(parentCtx, ConsumerHeaderCarrier(msg))
		parent.End()

		done := make(chan trace.SpanContext, 1)

		if withCtx {
			consumeOneKafka(t, client, msg, func(ctx context.Context, _ *sarama.ConsumerMessage) error {
				done <- trace.SpanContextFromContext(ctx)

				return nil
			}, nil)
		} else {
			consumeOneKafka(t, client, msg, nil, func(*sarama.ConsumerMessage) error {
				done <- trace.SpanContext{}

				return errors.New("plain handler error")
			})
		}

		got := <-done
		consumer := waitConsumerSpan(t, exp)

		if consumer.Parent().SpanID() != parent.SpanContext().SpanID() || consumer.Name() != "process orders" {
			t.Fatalf("withCtx=%v: consumer %q parent %v", withCtx, consumer.Name(), consumer.Parent())
		}

		if withCtx && got.SpanID() != consumer.SpanContext().SpanID() {
			t.Fatal("ctx handler doesn't see the consumer span")
		}
	}
}

func TestKafkaConsumeWithoutHeaders(t *testing.T) {
	client, _, exp := tracedClient(t)

	consumeOneKafka(t, client, &sarama.ConsumerMessage{Topic: "orders"}, func(context.Context, *sarama.ConsumerMessage) error { return nil }, nil)

	if waitConsumerSpan(t, exp).Parent().IsValid() {
		t.Fatal("a record without headers should start a root span")
	}
}

func TestHeaderCarriersRoundTrip(t *testing.T) {
	pm := &sarama.ProducerMessage{Headers: []sarama.RecordHeader{{Key: []byte("k"), Value: []byte("old")}}}
	pc := ProducerHeaderCarrier(pm)
	pc.Set("k", "new")
	pc.Set("traceparent", "tp")

	if pc.Get("k") != "new" || pc.Get("traceparent") != "tp" || len(pm.Headers) != 2 || len(pc.Keys()) != 2 {
		t.Fatalf("producer headers %v", pm.Headers)
	}

	cm := &sarama.ConsumerMessage{}
	cc := ConsumerHeaderCarrier(cm)
	cc.Set("traceparent", "tp")
	cc.Set("traceparent", "tp2")

	if cc.Get("traceparent") != "tp2" || len(cm.Headers) != 1 || cc.Get("missing") != "" {
		t.Fatalf("consumer headers %v", cm.Headers)
	}
}

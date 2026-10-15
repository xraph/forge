package kafka

import (
	"context"
	"testing"
	"time"

	"github.com/IBM/sarama"
	"github.com/IBM/sarama/mocks"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/logger"
)

func mockClient(t *testing.T) (*kafkaClient, *mocks.SyncProducer) {
	t.Helper()

	producer := mocks.NewSyncProducer(t, nil)

	return &kafkaClient{
		config: DefaultConfig(), logger: logger.NewNoopLogger(), metrics: forge.NewNoOpMetrics(),
		producer: producer,
	}, producer
}

func TestSendMessagesKeepsHeadersAndTimestamp(t *testing.T) {
	client, producer := mockClient(t)
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	producer.ExpectSendMessageWithMessageCheckerFunctionAndSucceed(func(msg *sarama.ProducerMessage) error {
		if len(msg.Headers) == 0 || string(msg.Headers[0].Key) != "tenant" || string(msg.Headers[0].Value) != "a" {
			t.Errorf("headers %v", msg.Headers)
		}

		if !msg.Timestamp.Equal(at) {
			t.Errorf("timestamp %v", msg.Timestamp)
		}

		return nil
	})

	err := client.SendMessages([]*ProducerMessage{{
		Topic: "orders", Value: []byte("x"), Timestamp: at,
		Headers: []MessageHeader{{Key: "tenant", Value: []byte("a")}},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestConsumePartitionContextPassesCtx(t *testing.T) {
	consumer := mocks.NewConsumer(t, nil)
	consumer.ExpectConsumePartition("orders", 0, sarama.OffsetOldest).
		YieldMessage(&sarama.ConsumerMessage{Topic: "orders", Value: []byte("x")})

	client := &kafkaClient{config: DefaultConfig(), logger: logger.NewNoopLogger(), metrics: forge.NewNoOpMetrics(), consumer: consumer}

	type key struct{}

	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), key{}, "v"))
	defer cancel()

	got := make(chan any, 1)

	if err := client.ConsumePartitionContext(ctx, "orders", 0, sarama.OffsetOldest, func(ctx context.Context, _ *sarama.ConsumerMessage) error {
		got <- ctx.Value(key{})

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case v := <-got:
		if v != "v" {
			t.Fatalf("ctx value %v", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler not called")
	}
}

func TestServiceDelegatesContextMethods(t *testing.T) {
	var _ interface {
		SendMessageContext(ctx context.Context, topic string, key, value []byte) error
		SendMessageAsyncContext(ctx context.Context, topic string, key, value []byte) error
		SendMessagesContext(ctx context.Context, messages []*ProducerMessage) error
		ConsumeContext(ctx context.Context, topics []string, handler ContextMessageHandler) error
		ConsumePartitionContext(ctx context.Context, topic string, partition int32, offset int64, handler ContextMessageHandler) error
	} = (*KafkaService)(nil)
}

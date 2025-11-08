package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

func newTestInMemoryQueue() *InMemoryQueue {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	return NewInMemoryQueue(config, logger, metrics)
}

func TestInMemoryQueue_Connect(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Try connecting again
	err = q.Connect(ctx)
	if !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("expected ErrAlreadyConnected, got %v", err)
	}
}

func TestInMemoryQueue_Disconnect(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	// Disconnect without connecting
	err := q.Disconnect(ctx)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}

	// Connect then disconnect
	err = q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = q.Disconnect(ctx)
	if err != nil {
		t.Fatalf("failed to disconnect: %v", err)
	}
}

func TestInMemoryQueue_Ping(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	// Ping without connecting
	err := q.Ping(ctx)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got %v", err)
	}

	// Connect then ping
	err = q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	err = q.Ping(ctx)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
}

func TestInMemoryQueue_DeclareQueue(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	// Try declaring same queue again
	err = q.DeclareQueue(ctx, "test", opts)
	if !errors.Is(err, ErrQueueAlreadyExists) {
		t.Errorf("expected ErrQueueAlreadyExists, got %v", err)
	}
}

func TestInMemoryQueue_DeleteQueue(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Try deleting non-existent queue
	err = q.DeleteQueue(ctx, "nonexistent")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}

	// Declare and delete queue
	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	err = q.DeleteQueue(ctx, "test")
	if err != nil {
		t.Fatalf("failed to delete queue: %v", err)
	}
}

func TestInMemoryQueue_ListQueues(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// List empty queues
	queues, err := q.ListQueues(ctx)
	if err != nil {
		t.Fatalf("failed to list queues: %v", err)
	}

	if len(queues) != 0 {
		t.Errorf("expected 0 queues, got %d", len(queues))
	}

	// Declare queues
	opts := DefaultQueueOptions()
	_ = q.DeclareQueue(ctx, "queue1", opts)
	_ = q.DeclareQueue(ctx, "queue2", opts)

	queues, err = q.ListQueues(ctx)
	if err != nil {
		t.Fatalf("failed to list queues: %v", err)
	}

	if len(queues) != 2 {
		t.Errorf("expected 2 queues, got %d", len(queues))
	}
}

func TestInMemoryQueue_GetQueueInfo(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Try getting info for non-existent queue
	_, err = q.GetQueueInfo(ctx, "nonexistent")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}

	// Declare queue and get info
	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	info, err := q.GetQueueInfo(ctx, "test")
	if err != nil {
		t.Fatalf("failed to get queue info: %v", err)
	}

	if info.Name != "test" {
		t.Errorf("expected name 'test', got '%s'", info.Name)
	}

	if info.Messages != 0 {
		t.Errorf("expected 0 messages, got %d", info.Messages)
	}
}

func TestInMemoryQueue_PurgeQueue(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	// Publish some messages
	for range 5 {
		_ = q.Publish(ctx, "test", Message{Body: []byte("test")})
	}

	// Verify messages exist
	info, _ := q.GetQueueInfo(ctx, "test")
	if info.Messages != 5 {
		t.Errorf("expected 5 messages, got %d", info.Messages)
	}

	// Purge queue
	err = q.PurgeQueue(ctx, "test")
	if err != nil {
		t.Fatalf("failed to purge queue: %v", err)
	}

	// Verify messages are gone
	info, _ = q.GetQueueInfo(ctx, "test")
	if info.Messages != 0 {
		t.Errorf("expected 0 messages after purge, got %d", info.Messages)
	}
}

func TestInMemoryQueue_Publish(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	msg := Message{
		Body:     []byte("test message"),
		Priority: 5,
	}

	err = q.Publish(ctx, "test", msg)
	if err != nil {
		t.Fatalf("failed to publish message: %v", err)
	}

	// Try publishing to non-existent queue
	err = q.Publish(ctx, "nonexistent", msg)
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}
}

func TestInMemoryQueue_PublishBatch(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	messages := []Message{
		{Body: []byte("msg1")},
		{Body: []byte("msg2")},
		{Body: []byte("msg3")},
	}

	err = q.PublishBatch(ctx, "test", messages)
	if err != nil {
		t.Fatalf("failed to publish batch: %v", err)
	}

	// Verify messages were published
	info, _ := q.GetQueueInfo(ctx, "test")
	if info.Messages != 3 {
		t.Errorf("expected 3 messages, got %d", info.Messages)
	}
}

func TestInMemoryQueue_PublishDelayed(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	msg := Message{Body: []byte("delayed message")}

	start := time.Now()
	err = q.PublishDelayed(ctx, "test", msg, 100*time.Millisecond)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("failed to publish delayed message: %v", err)
	}

	if duration < 100*time.Millisecond {
		t.Error("message was not delayed")
	}
}

func TestInMemoryQueue_Consume(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	// Publish a message
	msg := Message{Body: []byte("test")}

	err = q.Publish(ctx, "test", msg)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// Consume messages
	received := make(chan Message, 1)
	handler := func(ctx context.Context, msg Message) error {
		received <- msg

		return nil
	}

	consumeOpts := DefaultConsumeOptions()

	err = q.Consume(ctx, "test", handler, consumeOpts)
	if err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	// Wait for message
	select {
	case msg := <-received:
		if string(msg.Body) != "test" {
			t.Errorf("expected 'test', got '%s'", string(msg.Body))
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for message")
	}
}

func TestInMemoryQueue_ConsumeWithError(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	// Publish a message
	msg := Message{Body: []byte("test")}

	err = q.Publish(ctx, "test", msg)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// Consumer that returns error
	handler := func(ctx context.Context, msg Message) error {
		return ErrConsumeFailed
	}

	consumeOpts := DefaultConsumeOptions()

	err = q.Consume(ctx, "test", handler, consumeOpts)
	if err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	// Wait for message to be moved to DLQ
	time.Sleep(200 * time.Millisecond)

	dlq, err := q.GetDeadLetterQueue(ctx, "test")
	if err != nil {
		t.Fatalf("failed to get DLQ: %v", err)
	}

	if len(dlq) != 1 {
		t.Errorf("expected 1 message in DLQ, got %d", len(dlq))
	}
}

func TestInMemoryQueue_StopConsuming(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	handler := func(ctx context.Context, msg Message) error {
		return nil
	}

	consumeOpts := DefaultConsumeOptions()

	err = q.Consume(ctx, "test", handler, consumeOpts)
	if err != nil {
		t.Fatalf("failed to start consumer: %v", err)
	}

	err = q.StopConsuming(ctx, "test")
	if err != nil {
		t.Fatalf("failed to stop consuming: %v", err)
	}
}

func TestInMemoryQueue_AckNackReject(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Declare a queue and publish a message
	err = q.DeclareQueue(ctx, "test-queue", QueueOptions{})
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	msg := Message{
		ID:   "test-msg-1",
		Body: []byte("test message"),
	}

	err = q.Publish(ctx, "test-queue", msg)
	if err != nil {
		t.Fatalf("failed to publish message: %v", err)
	}

	// Start consuming without AutoAck to track messages in flight
	received := make(chan Message, 1)
	handler := func(ctx context.Context, m Message) error {
		received <- m

		return nil
	}

	err = q.Consume(ctx, "test-queue", handler, ConsumeOptions{
		AutoAck: false, // Don't auto-ack so message stays in flight
	})
	if err != nil {
		t.Fatalf("failed to consume: %v", err)
	}

	// Wait for message to be received
	select {
	case receivedMsg := <-received:
		// Test Ack on existing message
		err = q.Ack(ctx, receivedMsg.ID)
		if err != nil {
			t.Errorf("Ack should not error for existing message: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for message")
	}

	// Test operations on non-existent message should return ErrMessageNotFound
	err = q.Ack(ctx, "nonexistent")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("Ack on non-existent message should return ErrMessageNotFound, got: %v", err)
	}

	err = q.Nack(ctx, "nonexistent", true)
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("Nack on non-existent message should return ErrMessageNotFound, got: %v", err)
	}

	err = q.Reject(ctx, "nonexistent")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("Reject on non-existent message should return ErrMessageNotFound, got: %v", err)
	}
}

func TestInMemoryQueue_GetDeadLetterQueue(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Try getting DLQ for non-existent queue
	_, err = q.GetDeadLetterQueue(ctx, "nonexistent")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("expected ErrQueueNotFound, got %v", err)
	}

	// Declare queue
	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	dlq, err := q.GetDeadLetterQueue(ctx, "test")
	if err != nil {
		t.Fatalf("failed to get DLQ: %v", err)
	}

	if len(dlq) != 0 {
		t.Errorf("expected empty DLQ, got %d messages", len(dlq))
	}
}

func TestInMemoryQueue_RequeueDeadLetter(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	// Try requeuing non-existent message
	err = q.RequeueDeadLetter(ctx, "test", "nonexistent")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("expected ErrMessageNotFound, got %v", err)
	}
}

func TestInMemoryQueue_Stats(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Get stats before any operations
	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}

	if stats.QueueCount != 0 {
		t.Errorf("expected 0 queues, got %d", stats.QueueCount)
	}

	// Declare queue and publish messages
	opts := DefaultQueueOptions()
	_ = q.DeclareQueue(ctx, "test", opts)
	_ = q.Publish(ctx, "test", Message{Body: []byte("test")})

	// Get stats after operations
	stats, err = q.Stats(ctx)
	if err != nil {
		t.Fatalf("stats failed: %v", err)
	}

	if stats.QueueCount != 1 {
		t.Errorf("expected 1 queue, got %d", stats.QueueCount)
	}

	if stats.TotalMessages != 1 {
		t.Errorf("expected 1 message, got %d", stats.TotalMessages)
	}

	if stats.Uptime == 0 {
		t.Error("expected non-zero uptime")
	}
}

func TestInMemoryQueue_NotConnectedErrors(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	opts := DefaultQueueOptions()
	msg := Message{}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"DeclareQueue", func() error { return q.DeclareQueue(ctx, "test", opts) }},
		{"DeleteQueue", func() error { return q.DeleteQueue(ctx, "test") }},
		{"ListQueues", func() error {
			_, err := q.ListQueues(ctx)

			return err
		}},
		{"GetQueueInfo", func() error {
			_, err := q.GetQueueInfo(ctx, "test")

			return err
		}},
		{"PurgeQueue", func() error { return q.PurgeQueue(ctx, "test") }},
		{"Publish", func() error { return q.Publish(ctx, "test", msg) }},
		{"PublishBatch", func() error { return q.PublishBatch(ctx, "test", []Message{msg}) }},
		{"Consume", func() error { return q.Consume(ctx, "test", nil, DefaultConsumeOptions()) }},
		{"GetDeadLetterQueue", func() error {
			_, err := q.GetDeadLetterQueue(ctx, "test")

			return err
		}},
		{"RequeueDeadLetter", func() error { return q.RequeueDeadLetter(ctx, "test", "id") }},
		{"Stats", func() error {
			_, err := q.Stats(ctx)

			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if !errors.Is(err, ErrNotConnected) {
				t.Errorf("%s: expected ErrNotConnected, got %v", tt.name, err)
			}
		})
	}
}

func TestInMemoryQueue_ConcurrentAccess(t *testing.T) {
	q := newTestInMemoryQueue()
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	opts := DefaultQueueOptions()

	err = q.DeclareQueue(ctx, "test", opts)
	if err != nil {
		t.Fatalf("failed to declare queue: %v", err)
	}

	// Concurrent publishes
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			_ = q.Publish(ctx, "test", Message{Body: []byte("test")})
		})
	}

	wg.Wait()

	// Verify all messages were published
	info, _ := q.GetQueueInfo(ctx, "test")
	if info.Messages != 10 {
		t.Errorf("expected 10 messages, got %d", info.Messages)
	}
}

func BenchmarkInMemoryQueue_Publish(b *testing.B) {
	q := newTestInMemoryQueue()
	ctx := context.Background()
	_ = q.Connect(ctx)

	opts := DefaultQueueOptions()
	_ = q.DeclareQueue(ctx, "test", opts)

	msg := Message{Body: []byte("test message")}

	for b.Loop() {
		_ = q.Publish(ctx, "test", msg)
	}
}

func BenchmarkInMemoryQueue_Consume(b *testing.B) {
	q := newTestInMemoryQueue()
	ctx := context.Background()
	_ = q.Connect(ctx)

	opts := DefaultQueueOptions()
	_ = q.DeclareQueue(ctx, "test", opts)

	// Pre-publish messages
	for b.Loop() {
		_ = q.Publish(ctx, "test", Message{Body: []byte("test")})
	}

	b.ResetTimer()
	// Note: This is simplified benchmark, actual consumption happens async
}

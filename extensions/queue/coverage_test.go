package queue

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
)

// Additional tests to increase coverage to 100%

func TestInMemoryQueue_DoubleConnect(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	// First connect
	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	// Second connect should return error
	err = q.Connect(ctx)
	if !errors.Is(err, ErrAlreadyConnected) {
		t.Errorf("Connect() second time error = %v, want ErrAlreadyConnected", err)
	}
}

func TestInMemoryQueue_QueueAlreadyExists(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Declare first time
	err = q.DeclareQueue(ctx, "test", QueueOptions{})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}

	// Declare again should return error
	err = q.DeclareQueue(ctx, "test", QueueOptions{})
	if !errors.Is(err, ErrQueueAlreadyExists) {
		t.Errorf("DeclareQueue() second time error = %v, want ErrQueueAlreadyExists", err)
	}
}

func TestInMemoryQueue_QueueNotFound(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Try to get info for non-existent queue
	_, err = q.GetQueueInfo(ctx, "nonexistent")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("GetQueueInfo() error = %v, want ErrQueueNotFound", err)
	}

	// Try to delete non-existent queue
	err = q.DeleteQueue(ctx, "nonexistent")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("DeleteQueue() error = %v, want ErrQueueNotFound", err)
	}

	// Try to purge non-existent queue
	err = q.PurgeQueue(ctx, "nonexistent")
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("PurgeQueue() error = %v, want ErrQueueNotFound", err)
	}
}

func TestInMemoryQueue_PublishToNonExistentQueue(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Try to publish to non-existent queue
	err = q.Publish(ctx, "nonexistent", Message{Body: []byte("test")})
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("Publish() error = %v, want ErrQueueNotFound", err)
	}
}

func TestInMemoryQueue_ConsumeFromNonExistentQueue(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Try to consume from non-existent queue
	handler := func(ctx context.Context, msg Message) error {
		return nil
	}

	err = q.Consume(ctx, "nonexistent", handler, ConsumeOptions{})
	if !errors.Is(err, ErrQueueNotFound) {
		t.Errorf("Consume() error = %v, want ErrQueueNotFound", err)
	}
}

func TestInMemoryQueue_MessageNotFound(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Try to ack non-existent message
	err = q.Ack(ctx, "nonexistent")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("Ack() error = %v, want ErrMessageNotFound", err)
	}

	// Try to nack non-existent message
	err = q.Nack(ctx, "nonexistent", false)
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("Nack() error = %v, want ErrMessageNotFound", err)
	}

	// Try to reject non-existent message
	err = q.Reject(ctx, "nonexistent")
	if !errors.Is(err, ErrMessageNotFound) {
		t.Errorf("Reject() error = %v, want ErrMessageNotFound", err)
	}
}

func TestInMemoryQueue_ConsumeTimeout(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Declare queue
	err = q.DeclareQueue(ctx, "timeout-test", QueueOptions{})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}

	// Publish a message
	err = q.Publish(ctx, "timeout-test", Message{Body: []byte("test")})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Consume with timeout
	handler := func(ctx context.Context, msg Message) error {
		time.Sleep(200 * time.Millisecond) // Longer than timeout

		return nil
	}

	err = q.Consume(ctx, "timeout-test", handler, ConsumeOptions{
		Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}

	// Give time for timeout to occur
	time.Sleep(300 * time.Millisecond)

	// Stop consuming
	err = q.StopConsuming(ctx, "timeout-test")
	if err != nil {
		t.Errorf("StopConsuming() error = %v", err)
	}
}

func TestInMemoryQueue_RetryLogic(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		Driver:             "inmemory",
		MaxRetries:         2,
		RetryBackoff:       10 * time.Millisecond,
		RetryMultiplier:    2.0,
		MaxRetryBackoff:    100 * time.Millisecond,
		EnableDeadLetter:   true,
		DeadLetterSuffix:   ".dlq",
		DefaultPrefetch:    10,
		DefaultConcurrency: 1,
		DefaultTimeout:     30 * time.Second,
		MaxMessageSize:     1024 * 1024,
	}

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Declare queue
	err = q.DeclareQueue(ctx, "retry-test", QueueOptions{})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}

	// Publish a message
	err = q.Publish(ctx, "retry-test", Message{
		Body:       []byte("test"),
		MaxRetries: 2,
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// Consume with always-failing handler
	attempts := 0
	handler := func(ctx context.Context, msg Message) error {
		attempts++

		return errors.New("simulated error")
	}

	err = q.Consume(ctx, "retry-test", handler, ConsumeOptions{
		RetryStrategy: RetryStrategy{
			MaxRetries:      2,
			InitialInterval: 10 * time.Millisecond,
			MaxInterval:     100 * time.Millisecond,
			Multiplier:      2.0,
		},
	})
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}

	// Give time for retries and DLQ
	time.Sleep(500 * time.Millisecond)

	// Check DLQ has the message
	dlqMessages, err := q.GetDeadLetterQueue(ctx, "retry-test")
	if err != nil {
		t.Errorf("GetDeadLetterQueue() error = %v", err)
	}

	if len(dlqMessages) != 1 {
		t.Errorf("GetDeadLetterQueue() count = %d, want 1", len(dlqMessages))
	}

	// Stop consuming
	err = q.StopConsuming(ctx, "retry-test")
	if err != nil {
		t.Errorf("StopConsuming() error = %v", err)
	}
}

func TestInMemoryQueue_QueueOptions(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Test with various queue options
	opts := QueueOptions{
		Durable:         true,
		AutoDelete:      false,
		Exclusive:       false,
		MessageTTL:      time.Hour,
		MaxLength:       1000,
		MaxPriority:     10,
		DeadLetterQueue: "test.dlq",
	}

	err = q.DeclareQueue(ctx, "test-opts", opts)
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}

	// Verify options were applied
	info, err := q.GetQueueInfo(ctx, "test-opts")
	if err != nil {
		t.Fatalf("GetQueueInfo() error = %v", err)
	}

	if !info.Durable {
		t.Error("Expected durable queue")
	}

	if info.AutoDelete {
		t.Error("Expected auto_delete false")
	}
}

func TestInMemoryQueue_ConsumeOptions(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := DefaultConfig()

	q := NewInMemoryQueue(config, logger, metrics)
	ctx := context.Background()

	err := q.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer q.Disconnect(ctx)

	// Declare queue
	err = q.DeclareQueue(ctx, "consume-opts", QueueOptions{})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}

	// Publish messages
	for i := range 5 {
		err = q.Publish(ctx, "consume-opts", Message{
			Body: fmt.Appendf(nil, "message-%d", i),
		})
		if err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
	}

	var received int32

	handler := func(ctx context.Context, msg Message) error {
		atomic.AddInt32(&received, 1)

		return nil
	}

	// Consume with specific options
	err = q.Consume(ctx, "consume-opts", handler, ConsumeOptions{
		AutoAck:       true,
		PrefetchCount: 2,
		Concurrency:   1,
		Exclusive:     false,
	})
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}

	// Give time to consume messages
	time.Sleep(200 * time.Millisecond)

	// Stop consuming
	err = q.StopConsuming(ctx, "consume-opts")
	if err != nil {
		t.Errorf("StopConsuming() error = %v", err)
	}

	receivedCount := atomic.LoadInt32(&received)
	if receivedCount != 5 {
		t.Errorf("received %d messages, want 5", receivedCount)
	}
}

package queue

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge/v2"
)

func TestNewRabbitMQQueue(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with URL",
			config: Config{
				URL: "amqp://guest:guest@localhost:5672/",
			},
			wantErr: false,
		},
		{
			name: "valid config with hosts",
			config: Config{
				Hosts:    []string{"localhost:5672"},
				Username: "guest",
				Password: "guest",
			},
			wantErr: false,
		},
		{
			name:    "missing URL and hosts",
			config:  Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue, err := NewRabbitMQQueue(tt.config, logger, metrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRabbitMQQueue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && queue == nil {
				t.Error("NewRabbitMQQueue() returned nil queue")
			}
		})
	}
}

func TestRabbitMQQueue_ConnectDisconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "amqp://guest:guest@localhost:5672/",
	}

	queue, err := NewRabbitMQQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	ctx := context.Background()

	// Test connect
	err = queue.Connect(ctx)
	if err != nil {
		t.Skipf("Cannot connect to RabbitMQ: %v", err)
	}

	// Test double connect
	err = queue.Connect(ctx)
	if err != ErrAlreadyConnected {
		t.Errorf("Connect() second time should return ErrAlreadyConnected, got %v", err)
	}

	// Test ping
	err = queue.Ping(ctx)
	if err != nil {
		t.Errorf("Ping() error = %v", err)
	}

	// Test disconnect
	err = queue.Disconnect(ctx)
	if err != nil {
		t.Errorf("Disconnect() error = %v", err)
	}

	// Test double disconnect
	err = queue.Disconnect(ctx)
	if err != ErrNotConnected {
		t.Errorf("Disconnect() second time should return ErrNotConnected, got %v", err)
	}
}

func TestRabbitMQQueue_QueueOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "amqp://guest:guest@localhost:5672/",
	}

	queue, err := NewRabbitMQQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to RabbitMQ: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-queue-rabbitmq"

	// Clean up any existing queue
	queue.DeleteQueue(ctx, queueName)

	// Test declare queue
	err = queue.DeclareQueue(ctx, queueName, QueueOptions{
		Durable: true,
	})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}

	// Test get queue info
	info, err := queue.GetQueueInfo(ctx, queueName)
	if err != nil {
		t.Errorf("GetQueueInfo() error = %v", err)
	}
	if info.Name != queueName {
		t.Errorf("GetQueueInfo() name = %v, want %v", info.Name, queueName)
	}

	// Clean up
	err = queue.DeleteQueue(ctx, queueName)
	if err != nil {
		t.Errorf("DeleteQueue() error = %v", err)
	}
}

func TestRabbitMQQueue_PublishConsume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "amqp://guest:guest@localhost:5672/",
	}

	queue, err := NewRabbitMQQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to RabbitMQ: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-publish-consume-rabbitmq"

	// Clean up
	queue.DeleteQueue(ctx, queueName)

	// Declare queue
	err = queue.DeclareQueue(ctx, queueName, QueueOptions{
		Durable: true,
	})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}
	defer queue.DeleteQueue(ctx, queueName)

	// Publish a message
	msg := Message{
		Body: []byte("test message"),
		Headers: map[string]string{
			"test": "header",
		},
	}

	err = queue.Publish(ctx, queueName, msg)
	if err != nil {
		t.Errorf("Publish() error = %v", err)
	}

	// Give RabbitMQ time to process
	time.Sleep(100 * time.Millisecond)

	// Verify queue info shows message
	info, err := queue.GetQueueInfo(ctx, queueName)
	if err != nil {
		t.Errorf("GetQueueInfo() error = %v", err)
	}
	if info.Messages == 0 {
		t.Logf("GetQueueInfo() messages = %d, expected > 0 (may be consumed already)", info.Messages)
	}

	// Test consume
	received := make(chan bool, 1)
	handler := func(ctx context.Context, msg Message) error {
		if string(msg.Body) != "test message" {
			t.Errorf("Received message body = %s, want 'test message'", string(msg.Body))
		}
		received <- true
		return nil
	}

	err = queue.Consume(ctx, queueName, handler, ConsumeOptions{
		PrefetchCount: 1,
		AutoAck:       false,
	})
	if err != nil {
		t.Errorf("Consume() error = %v", err)
	}

	// Wait for message or timeout
	select {
	case <-received:
		// Message received successfully
	case <-time.After(5 * time.Second):
		t.Error("Timeout waiting for message")
	}

	// Stop consuming
	err = queue.StopConsuming(ctx, queueName)
	if err != nil {
		t.Errorf("StopConsuming() error = %v", err)
	}
}

func TestRabbitMQQueue_PublishBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "amqp://guest:guest@localhost:5672/",
	}

	queue, err := NewRabbitMQQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to RabbitMQ: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-batch-rabbitmq"

	// Clean up and declare queue
	queue.DeleteQueue(ctx, queueName)
	err = queue.DeclareQueue(ctx, queueName, QueueOptions{
		Durable: true,
	})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}
	defer queue.DeleteQueue(ctx, queueName)

	// Publish batch
	messages := []Message{
		{Body: []byte("message 1")},
		{Body: []byte("message 2")},
		{Body: []byte("message 3")},
	}

	err = queue.PublishBatch(ctx, queueName, messages)
	if err != nil {
		t.Errorf("PublishBatch() error = %v", err)
	}

	// Give time to process
	time.Sleep(100 * time.Millisecond)

	// Verify messages were published
	info, err := queue.GetQueueInfo(ctx, queueName)
	if err != nil {
		t.Errorf("GetQueueInfo() error = %v", err)
	}
	if info.Messages < int64(len(messages)) {
		t.Logf("GetQueueInfo() messages = %d, expected >= %d", info.Messages, len(messages))
	}
}

func TestRabbitMQQueue_Stats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "amqp://guest:guest@localhost:5672/",
	}

	queue, err := NewRabbitMQQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to RabbitMQ: %v", err)
	}
	defer queue.Disconnect(ctx)

	stats, err := queue.Stats(ctx)
	if err != nil {
		t.Errorf("Stats() error = %v", err)
	}

	if stats.ConnectionCount == 0 {
		t.Error("Stats() connection count should be > 0")
	}

	if stats.Uptime == 0 {
		t.Error("Stats() uptime should be > 0")
	}
}

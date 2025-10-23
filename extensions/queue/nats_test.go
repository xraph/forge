package queue

import (
	"context"
	"testing"
	"time"

	"github.com/xraph/forge"
)

func TestNewNATSQueue(t *testing.T) {
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
				URL: "nats://localhost:4222",
			},
			wantErr: false,
		},
		{
			name: "valid config with hosts",
			config: Config{
				Hosts: []string{"localhost:4222"},
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
			queue, err := NewNATSQueue(tt.config, logger, metrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewNATSQueue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && queue == nil {
				t.Error("NewNATSQueue() returned nil queue")
			}
		})
	}
}

func TestNATSQueue_ConnectDisconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "nats://localhost:4222",
	}

	queue, err := NewNATSQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewNATSQueue() error = %v", err)
	}

	ctx := context.Background()

	// Test connect
	err = queue.Connect(ctx)
	if err != nil {
		t.Skipf("Cannot connect to NATS: %v", err)
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

func TestNATSQueue_QueueOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "nats://localhost:4222",
	}

	queue, err := NewNATSQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewNATSQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to NATS: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-queue-nats"

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

	// Test list queues
	queues, err := queue.ListQueues(ctx)
	if err != nil {
		t.Errorf("ListQueues() error = %v", err)
	}
	found := false
	for _, q := range queues {
		if q == queueName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListQueues() did not include %s", queueName)
	}

	// Clean up
	err = queue.DeleteQueue(ctx, queueName)
	if err != nil {
		t.Errorf("DeleteQueue() error = %v", err)
	}
}

func TestNATSQueue_PublishConsume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "nats://localhost:4222",
	}

	queue, err := NewNATSQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewNATSQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to NATS: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-publish-consume-nats"

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

	// Test consume first (start consumer before publishing)
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

	// Give consumer time to start
	time.Sleep(100 * time.Millisecond)

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

func TestNATSQueue_PublishBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "nats://localhost:4222",
	}

	queue, err := NewNATSQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewNATSQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to NATS: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-batch-nats"

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

func TestNATSQueue_PurgeQueue(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "nats://localhost:4222",
	}

	queue, err := NewNATSQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewNATSQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to NATS: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-purge-nats"

	// Clean up and declare queue
	queue.DeleteQueue(ctx, queueName)
	err = queue.DeclareQueue(ctx, queueName, QueueOptions{
		Durable: true,
	})
	if err != nil {
		t.Fatalf("DeclareQueue() error = %v", err)
	}
	defer queue.DeleteQueue(ctx, queueName)

	// Publish messages
	messages := []Message{
		{Body: []byte("message 1")},
		{Body: []byte("message 2")},
	}
	err = queue.PublishBatch(ctx, queueName, messages)
	if err != nil {
		t.Errorf("PublishBatch() error = %v", err)
	}

	// Give time to process
	time.Sleep(100 * time.Millisecond)

	// Purge queue
	err = queue.PurgeQueue(ctx, queueName)
	if err != nil {
		t.Errorf("PurgeQueue() error = %v", err)
	}

	// Verify queue is empty
	info, err := queue.GetQueueInfo(ctx, queueName)
	if err != nil {
		t.Errorf("GetQueueInfo() error = %v", err)
	}
	if info.Messages != 0 {
		t.Errorf("GetQueueInfo() after purge messages = %d, want 0", info.Messages)
	}
}

func TestNATSQueue_Stats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "nats://localhost:4222",
	}

	queue, err := NewNATSQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewNATSQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to NATS: %v", err)
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

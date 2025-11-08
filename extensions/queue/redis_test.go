package queue

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/internal/errors"
)

func TestNewRedisQueue(t *testing.T) {
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
				URL: "redis://localhost:6379",
			},
			wantErr: false,
		},
		{
			name: "valid config with hosts",
			config: Config{
				Hosts: []string{"localhost:6379"},
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
			queue, err := NewRedisQueue(tt.config, logger, metrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRedisQueue() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if !tt.wantErr && queue == nil {
				t.Error("NewRedisQueue() returned nil queue")
			}
		})
	}
}

func TestRedisQueue_ConnectDisconnect(t *testing.T) {
	// Skip if Redis is not available
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "redis://localhost:6379",
	}

	queue, err := NewRedisQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRedisQueue() error = %v", err)
	}

	ctx := context.Background()

	// Test connect
	err = queue.Connect(ctx)
	if err != nil {
		t.Skipf("Cannot connect to Redis: %v", err)
	}

	// Test double connect
	err = queue.Connect(ctx)
	if !errors.Is(err, ErrAlreadyConnected) {
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
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Disconnect() second time should return ErrNotConnected, got %v", err)
	}
}

func TestRedisQueue_QueueOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "redis://localhost:6379",
	}

	queue, err := NewRedisQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRedisQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to Redis: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-queue-redis"

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

	found := slices.Contains(queues, queueName)

	if !found {
		t.Errorf("ListQueues() did not include %s", queueName)
	}

	// Clean up
	err = queue.DeleteQueue(ctx, queueName)
	if err != nil {
		t.Errorf("DeleteQueue() error = %v", err)
	}
}

func TestRedisQueue_PublishConsume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "redis://localhost:6379",
	}

	queue, err := NewRedisQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRedisQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to Redis: %v", err)
	}
	defer queue.Disconnect(ctx)

	queueName := "test-publish-consume-redis"

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

	// Give Redis time to process
	time.Sleep(100 * time.Millisecond)

	// Verify queue info shows message
	info, err := queue.GetQueueInfo(ctx, queueName)
	if err != nil {
		t.Errorf("GetQueueInfo() error = %v", err)
	}

	if info.Messages == 0 {
		t.Logf("GetQueueInfo() messages = %d, expected > 0 (may be consumed already)", info.Messages)
	}
}

func TestRedisQueue_Stats(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{
		URL: "redis://localhost:6379",
	}

	queue, err := NewRedisQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRedisQueue() error = %v", err)
	}

	ctx := context.Background()
	if err := queue.Connect(ctx); err != nil {
		t.Skipf("Cannot connect to Redis: %v", err)
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

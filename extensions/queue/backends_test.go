package queue

import (
	"context"
	"testing"

	"github.com/xraph/forge"
)

// Test all backend constructors and basic error paths

func TestRedisQueue_Constructor(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid with URL",
			config: Config{
				URL: "redis://localhost:6379",
			},
			wantErr: false,
		},
		{
			name: "valid with hosts",
			config: Config{
				Hosts: []string{"localhost:6379"},
			},
			wantErr: false,
		},
		{
			name:    "missing connection info",
			config:  Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := NewRedisQueue(tt.config, logger, metrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRedisQueue() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && q == nil {
				t.Error("expected non-nil queue")
			}
		})
	}
}

func TestRabbitMQQueue_Constructor(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid with URL",
			config: Config{
				URL: "amqp://guest:guest@localhost:5672/",
			},
			wantErr: false,
		},
		{
			name: "valid with hosts",
			config: Config{
				Hosts:    []string{"localhost:5672"},
				Username: "guest",
				Password: "guest",
			},
			wantErr: false,
		},
		{
			name:    "missing connection info",
			config:  Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := NewRabbitMQQueue(tt.config, logger, metrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRabbitMQQueue() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && q == nil {
				t.Error("expected non-nil queue")
			}
		})
	}
}

func TestNATSQueue_Constructor(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()

	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid with URL",
			config: Config{
				URL: "nats://localhost:4222",
			},
			wantErr: false,
		},
		{
			name: "valid with hosts",
			config: Config{
				Hosts: []string{"localhost:4222"},
			},
			wantErr: false,
		},
		{
			name:    "missing connection info",
			config:  Config{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q, err := NewNATSQueue(tt.config, logger, metrics)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewNATSQueue() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && q == nil {
				t.Error("expected non-nil queue")
			}
		})
	}
}

// Test not connected errors for all backends

func TestRedisQueue_NotConnectedErrors(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{URL: "redis://localhost:6379"}

	q, err := NewRedisQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRedisQueue() error = %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Ping", func() error { return q.Ping(ctx) }},
		{"DeclareQueue", func() error { return q.DeclareQueue(ctx, "test", QueueOptions{}) }},
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
		{"Publish", func() error { return q.Publish(ctx, "test", Message{}) }},
		{"PublishBatch", func() error { return q.PublishBatch(ctx, "test", []Message{}) }},
		{"PublishDelayed", func() error { return q.PublishDelayed(ctx, "test", Message{}, 0) }},
		{"Consume", func() error { return q.Consume(ctx, "test", nil, ConsumeOptions{}) }},
		{"StopConsuming", func() error { return q.StopConsuming(ctx, "test") }},
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
				t.Errorf("%s() error = %v, want ErrNotConnected", tt.name, err)
			}
		})
	}

	// Test double disconnect
	err = q.Disconnect(ctx)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Disconnect() on not connected = %v, want ErrNotConnected", err)
	}
}

func TestRabbitMQQueue_NotConnectedErrors(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{URL: "amqp://guest:guest@localhost:5672/"}

	q, err := NewRabbitMQQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewRabbitMQQueue() error = %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Ping", func() error { return q.Ping(ctx) }},
		{"DeclareQueue", func() error { return q.DeclareQueue(ctx, "test", QueueOptions{}) }},
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
		{"Publish", func() error { return q.Publish(ctx, "test", Message{}) }},
		{"PublishBatch", func() error { return q.PublishBatch(ctx, "test", []Message{}) }},
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
				t.Errorf("%s() error = %v, want ErrNotConnected", tt.name, err)
			}
		})
	}

	// Test Ack/Nack/Reject (should return nil)
	if err := q.Ack(ctx, "id"); err != nil {
		t.Errorf("Ack() error = %v, want nil", err)
	}

	if err := q.Nack(ctx, "id", false); err != nil {
		t.Errorf("Nack() error = %v, want nil", err)
	}

	if err := q.Reject(ctx, "id"); err != nil {
		t.Errorf("Reject() error = %v, want nil", err)
	}

	// Test double disconnect
	err = q.Disconnect(ctx)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Disconnect() on not connected = %v, want ErrNotConnected", err)
	}
}

func TestNATSQueue_NotConnectedErrors(t *testing.T) {
	logger := forge.NewNoopLogger()
	metrics := forge.NewNoOpMetrics()
	config := Config{URL: "nats://localhost:4222"}

	q, err := NewNATSQueue(config, logger, metrics)
	if err != nil {
		t.Fatalf("NewNATSQueue() error = %v", err)
	}

	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Ping", func() error { return q.Ping(ctx) }},
		{"DeclareQueue", func() error { return q.DeclareQueue(ctx, "test", QueueOptions{}) }},
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
		{"Publish", func() error { return q.Publish(ctx, "test", Message{}) }},
		{"PublishBatch", func() error { return q.PublishBatch(ctx, "test", []Message{}) }},
		{"PublishDelayed", func() error { return q.PublishDelayed(ctx, "test", Message{}, 0) }},
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
				t.Errorf("%s() error = %v, want ErrNotConnected", tt.name, err)
			}
		})
	}

	// Test Ack/Nack/Reject (should return nil)
	if err := q.Ack(ctx, "id"); err != nil {
		t.Errorf("Ack() error = %v, want nil", err)
	}

	if err := q.Nack(ctx, "id", false); err != nil {
		t.Errorf("Nack() error = %v, want nil", err)
	}

	if err := q.Reject(ctx, "id"); err != nil {
		t.Errorf("Reject() error = %v, want nil", err)
	}

	// Test double disconnect
	err = q.Disconnect(ctx)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Disconnect() on not connected = %v, want ErrNotConnected", err)
	}
}

// Test extension with different drivers

func TestExtension_RedisDriver(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension(
		WithDriver("redis"),
		WithURL("redis://localhost:6379"),
	)

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Errorf("Register() error = %v", err)
	}

	// Verify queue can be resolved
	queue, err := forge.Resolve[Queue](app.Container(), "queue")
	if err != nil {
		t.Errorf("Resolve() error = %v", err)
	}

	if queue == nil {
		t.Error("expected non-nil queue")
	}
}

func TestExtension_RabbitMQDriver(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension(
		WithDriver("rabbitmq"),
		WithURL("amqp://guest:guest@localhost:5672/"),
	)

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Errorf("Register() error = %v", err)
	}

	// Verify queue can be resolved
	queue, err := forge.Resolve[Queue](app.Container(), "queue")
	if err != nil {
		t.Errorf("Resolve() error = %v", err)
	}

	if queue == nil {
		t.Error("expected non-nil queue")
	}
}

func TestExtension_NATSDriver(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension(
		WithDriver("nats"),
		WithURL("nats://localhost:4222"),
	)

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Errorf("Register() error = %v", err)
	}

	// Verify queue can be resolved
	queue, err := forge.Resolve[Queue](app.Container(), "queue")
	if err != nil {
		t.Errorf("Resolve() error = %v", err)
	}

	if queue == nil {
		t.Error("expected non-nil queue")
	}
}

func TestExtension_Queue(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension(WithDriver("inmemory"))

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// Test Queue() method
	queueExt, ok := ext.(*Extension)
	if !ok {
		t.Fatal("expected *Extension")
	}

	queue := queueExt.Queue()
	if queue == nil {
		t.Error("Queue() returned nil")
	}
}

func TestExtension_StopWithError(t *testing.T) {
	app := forge.NewApp(forge.DefaultAppConfig())
	ext := NewExtension(WithDriver("inmemory"))

	err := ext.(*Extension).Register(app)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	ctx := context.Background()

	err = ext.Start(ctx)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Manually disconnect to cause error on stop
	queueExt := ext.(*Extension)
	queueExt.queue.Disconnect(ctx)

	// Stop should not fail even if disconnect fails
	err = ext.Stop(ctx)
	if err != nil {
		t.Errorf("Stop() error = %v", err)
	}
}

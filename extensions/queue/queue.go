package queue

import (
	"context"
	"time"
)

// Queue represents a unified message queue interface supporting multiple backends.
type Queue interface {
	// Connection management
	Connect(ctx context.Context) error
	Disconnect(ctx context.Context) error
	Ping(ctx context.Context) error

	// Queue management
	DeclareQueue(ctx context.Context, name string, opts QueueOptions) error
	DeleteQueue(ctx context.Context, name string) error
	ListQueues(ctx context.Context) ([]string, error)
	GetQueueInfo(ctx context.Context, name string) (*QueueInfo, error)
	PurgeQueue(ctx context.Context, name string) error

	// Publishing
	Publish(ctx context.Context, queue string, message Message) error
	PublishBatch(ctx context.Context, queue string, messages []Message) error
	PublishDelayed(ctx context.Context, queue string, message Message, delay time.Duration) error

	// Consuming
	Consume(ctx context.Context, queue string, handler MessageHandler, opts ConsumeOptions) error
	StopConsuming(ctx context.Context, queue string) error

	// Message operations
	Ack(ctx context.Context, messageID string) error
	Nack(ctx context.Context, messageID string, requeue bool) error
	Reject(ctx context.Context, messageID string) error

	// Dead letter queue
	GetDeadLetterQueue(ctx context.Context, queue string) ([]Message, error)
	RequeueDeadLetter(ctx context.Context, queue string, messageID string) error

	// Stats
	Stats(ctx context.Context) (*QueueStats, error)
}

// Message represents a queue message.
type Message struct {
	ID          string                 `json:"id"`
	Queue       string                 `json:"queue"`
	Body        []byte                 `json:"body"`
	Headers     map[string]string      `json:"headers,omitempty"`
	Priority    int                    `json:"priority,omitempty"`    // 0-9, higher = more priority
	Delay       time.Duration          `json:"delay,omitempty"`       // Delay before processing
	Expiration  time.Duration          `json:"expiration,omitempty"`  // Message TTL
	Retries     int                    `json:"retries,omitempty"`     // Current retry count
	MaxRetries  int                    `json:"max_retries,omitempty"` // Maximum retry attempts
	PublishedAt time.Time              `json:"published_at"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// QueueOptions contains queue configuration.
type QueueOptions struct {
	Durable         bool                   `json:"durable"`           // Survives broker restart
	AutoDelete      bool                   `json:"auto_delete"`       // Deleted when no consumers
	Exclusive       bool                   `json:"exclusive"`         // Used by only one connection
	MaxLength       int64                  `json:"max_length"`        // Maximum number of messages
	MaxLengthBytes  int64                  `json:"max_length_bytes"`  // Maximum total message bytes
	MessageTTL      time.Duration          `json:"message_ttl"`       // Message time-to-live
	MaxPriority     int                    `json:"max_priority"`      // Maximum priority (0-255)
	DeadLetterQueue string                 `json:"dead_letter_queue"` // DLQ for failed messages
	Arguments       map[string]interface{} `json:"arguments,omitempty"`
}

// QueueInfo contains queue metadata.
type QueueInfo struct {
	Name          string    `json:"name"`
	Messages      int64     `json:"messages"`
	Consumers     int       `json:"consumers"`
	MessageBytes  int64     `json:"message_bytes"`
	PublishRate   float64   `json:"publish_rate"`
	DeliverRate   float64   `json:"deliver_rate"`
	AckRate       float64   `json:"ack_rate"`
	Durable       bool      `json:"durable"`
	AutoDelete    bool      `json:"auto_delete"`
	CreatedAt     time.Time `json:"created_at"`
	LastMessageAt time.Time `json:"last_message_at,omitempty"`
}

// MessageHandler is called for each received message.
type MessageHandler func(ctx context.Context, msg Message) error

// ConsumeOptions contains consumer configuration.
type ConsumeOptions struct {
	ConsumerTag   string                 `json:"consumer_tag,omitempty"`
	AutoAck       bool                   `json:"auto_ack"`           // Auto-acknowledge messages
	Exclusive     bool                   `json:"exclusive"`          // Exclusive consumer
	PrefetchCount int                    `json:"prefetch_count"`     // Number of messages to prefetch
	Priority      int                    `json:"priority,omitempty"` // Consumer priority
	Concurrency   int                    `json:"concurrency"`        // Number of concurrent workers
	RetryStrategy RetryStrategy          `json:"retry_strategy"`     // Retry configuration
	Timeout       time.Duration          `json:"timeout,omitempty"`  // Message processing timeout
	Arguments     map[string]interface{} `json:"arguments,omitempty"`
}

// RetryStrategy defines retry behavior.
type RetryStrategy struct {
	MaxRetries      int           `json:"max_retries"`
	InitialInterval time.Duration `json:"initial_interval"`
	MaxInterval     time.Duration `json:"max_interval"`
	Multiplier      float64       `json:"multiplier"` // Exponential backoff multiplier
}

// QueueStats contains queue system statistics.
type QueueStats struct {
	QueueCount      int64                  `json:"queue_count"`
	TotalMessages   int64                  `json:"total_messages"`
	TotalConsumers  int                    `json:"total_consumers"`
	PublishRate     float64                `json:"publish_rate"`
	DeliverRate     float64                `json:"deliver_rate"`
	AckRate         float64                `json:"ack_rate"`
	UnackedMessages int64                  `json:"unacked_messages"`
	ReadyMessages   int64                  `json:"ready_messages"`
	Uptime          time.Duration          `json:"uptime"`
	MemoryUsed      int64                  `json:"memory_used"`
	ConnectionCount int                    `json:"connection_count"`
	Version         string                 `json:"version"`
	Extra           map[string]interface{} `json:"extra,omitempty"`
}

// DefaultQueueOptions returns default queue options.
func DefaultQueueOptions() QueueOptions {
	return QueueOptions{
		Durable:     true,
		AutoDelete:  false,
		Exclusive:   false,
		MaxLength:   0, // Unlimited
		MessageTTL:  0, // No expiration
		MaxPriority: 0,
	}
}

// DefaultConsumeOptions returns default consume options.
func DefaultConsumeOptions() ConsumeOptions {
	return ConsumeOptions{
		AutoAck:       false,
		Exclusive:     false,
		PrefetchCount: 10,
		Concurrency:   1,
		RetryStrategy: RetryStrategy{
			MaxRetries:      3,
			InitialInterval: 1 * time.Second,
			MaxInterval:     30 * time.Second,
			Multiplier:      2.0,
		},
		Timeout: 30 * time.Second,
	}
}

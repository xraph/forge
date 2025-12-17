# Queue Extension

Production-ready message queue extension for the Forge framework, providing a unified interface across multiple backend implementations (in-memory, Redis, RabbitMQ, NATS).

## Features

- **Multiple Backends**: InMemory, Redis, RabbitMQ, NATS
- **Rich Queue Operations**: Declare, delete, purge, list queues
- **Flexible Publishing**: Single, batch, and delayed message publishing
- **Robust Consuming**: Worker pools with configurable concurrency
- **Reliability**: Automatic retries with exponential backoff
- **Priority Queues**: Message prioritization (0-9 scale)
- **Dead Letter Queues**: Automatic DLQ for failed messages
- **Message TTL**: Expiration and time-to-live support
- **Observability**: Built-in metrics and tracing
- **Health Checks**: Connection monitoring and stats
- **Thread-Safe**: Concurrent operations across all backends

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [API Reference](#api-reference)
- [Usage Examples](#usage-examples)
- [Backend Comparison](#backend-comparison)
- [Testing](#testing)
- [Best Practices](#best-practices)
- [Performance Tuning](#performance-tuning)
- [Troubleshooting](#troubleshooting)

## Installation

```go
import "github.com/xraph/forge/extensions/queue"
```

## Quick Start

### Basic Usage

```go
package main

import (
    "context"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/queue"
)

func main() {
    app := forge.New(
        forge.WithAppName("queue-demo"),
        forge.WithExtensions(
            queue.NewExtension(
                queue.WithDriver("inmemory"),
            ),
        ),
    )
    
    ctx := context.Background()
    if err := app.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer app.Stop(ctx)
    
    // Get queue service from container (using helper function)
    q, err := queue.Get(app.Container())
    if err != nil {
        log.Fatal(err)
    }
    
    // Declare a queue
    err = q.DeclareQueue(ctx, "tasks", queue.DefaultQueueOptions())
    if err != nil {
        log.Fatal(err)
    }
    
    // Publish a message
    err = q.Publish(ctx, "tasks", queue.Message{
        Body: []byte(`{"action":"send_email","to":"user@example.com"}`),
    })
    if err != nil {
        log.Fatal(err)
    }
    
    // Consume messages
    err = q.Consume(ctx, "tasks", func(ctx context.Context, msg queue.Message) error {
        log.Printf("Processing message: %s", msg.Body)
        return nil // Auto-ack on success
    }, queue.DefaultConsumeOptions())
    if err != nil {
        log.Fatal(err)
    }
    
    // Keep running
    select {}
}
```

## Configuration

### YAML Configuration

```yaml
queue:
  # Backend driver: inmemory, redis, rabbitmq, nats
  driver: redis
  
  # Connection (choose url OR hosts)
  url: redis://localhost:6379
  # hosts: ["localhost:6379"]
  
  username: ""
  password: ""
  vhost: "/"  # RabbitMQ only
  
  # Connection pool
  max_connections: 10
  max_idle_connections: 5
  connect_timeout: 10s
  read_timeout: 30s
  write_timeout: 30s
  keep_alive: 60s
  
  # Retry policy
  max_retries: 3
  retry_backoff: 100ms
  retry_multiplier: 2.0
  max_retry_backoff: 30s
  
  # Default queue settings
  default_prefetch: 10
  default_concurrency: 1
  default_timeout: 30s
  enable_dead_letter: true
  dead_letter_suffix: ".dlq"
  
  # Performance
  enable_persistence: true
  enable_priority: false
  enable_delayed: false
  max_message_size: 1048576  # 1MB
  
  # Security
  enable_tls: false
  tls_cert_file: ""
  tls_key_file: ""
  tls_ca_file: ""
  insecure_skip_verify: false
  
  # Monitoring
  enable_metrics: true
  enable_tracing: true
```

### Programmatic Configuration

```go
ext := queue.NewExtension(
    queue.WithDriver("rabbitmq"),
    queue.WithURL("amqp://localhost:5672"),
    queue.WithAuth("guest", "guest"),
    queue.WithVHost("/"),
    queue.WithMaxConnections(20),
    queue.WithPrefetch(50),
    queue.WithConcurrency(10),
    queue.WithTimeout(60 * time.Second),
    queue.WithDeadLetter(true),
    queue.WithPersistence(true),
    queue.WithPriority(true),
    queue.WithDelayed(true),
    queue.WithMetrics(true),
    queue.WithTracing(true),
)
```

### Backend-Specific URLs

```go
// In-Memory (no URL needed)
queue.WithDriver("inmemory")

// Redis
queue.WithURL("redis://localhost:6379")
queue.WithURL("redis://:password@localhost:6379/0")
queue.WithURL("redis://localhost:6379?db=0&max_retries=3")

// RabbitMQ
queue.WithURL("amqp://guest:guest@localhost:5672/")
queue.WithURL("amqps://user:pass@rabbitmq.example.com:5671/vhost")

// NATS
queue.WithURL("nats://localhost:4222")
queue.WithURL("nats://user:pass@nats.example.com:4222")
queue.WithHosts("nats://server1:4222", "nats://server2:4222")  // Cluster
```

### Using Database Extension's Redis Connection

If you're already using the database extension with a Redis connection, you can reuse it for the queue instead of creating a separate connection:

#### YAML Configuration

```yaml
database:
  databases:
    - name: redis-cache
      type: redis
      dsn: redis://localhost:6379/0

queue:
  driver: redis
  database_redis_connection: redis-cache  # Reuse database connection
```

#### Programmatic Configuration

```go
app := forge.New(
    forge.WithExtensions(
        database.NewExtension(
            database.WithDatabase(database.DatabaseConfig{
                Name: "redis-cache",
                Type: database.TypeRedis,
                DSN:  "redis://localhost:6379/0",
            }),
        ),
        queue.NewExtension(
            queue.WithDriver("redis"),
            queue.WithDatabaseRedisConnection("redis-cache"),
        ),
    ),
)
```

#### Benefits

- Single Redis connection shared between database and queue
- Simplified configuration
- Reduced connection overhead
- Consistent connection pooling and settings

### DI Helper Functions

Easy ways to resolve queue services from the DI container:

```go
// Package-level helpers
q, err := queue.Get(app.Container())
q := queue.MustGet(app.Container())  // Panics on error

// App-level helpers
q, err := queue.GetFromApp(app)
q := queue.MustGetFromApp(app)  // Panics on error

// Extension method (if you have the extension instance)
ext := queue.NewExtension(...)
app.RegisterExtension(ext)
q := ext.(*queue.Extension).Queue()
```

## API Reference

### Queue Interface

```go
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
```

### Message

```go
type Message struct {
    ID          string                 // Unique message identifier
    Queue       string                 // Queue name
    Body        []byte                 // Message payload
    Headers     map[string]string      // Custom headers
    Priority    int                    // 0-9, higher = more priority
    Delay       time.Duration          // Delay before processing
    Expiration  time.Duration          // Message TTL
    Retries     int                    // Current retry count
    MaxRetries  int                    // Maximum retry attempts
    PublishedAt time.Time              // Publication timestamp
    Metadata    map[string]interface{} // Additional metadata
}
```

### QueueOptions

```go
type QueueOptions struct {
    Durable         bool          // Survives broker restart
    AutoDelete      bool          // Deleted when no consumers
    Exclusive       bool          // Used by only one connection
    MaxLength       int64         // Maximum number of messages
    MaxLengthBytes  int64         // Maximum total message bytes
    MessageTTL      time.Duration // Message time-to-live
    MaxPriority     int           // Maximum priority (0-255)
    DeadLetterQueue string        // DLQ for failed messages
    Arguments       map[string]interface{}
}
```

### ConsumeOptions

```go
type ConsumeOptions struct {
    ConsumerTag   string        // Consumer identifier
    AutoAck       bool          // Auto-acknowledge messages
    Exclusive     bool          // Exclusive consumer
    PrefetchCount int           // Number of messages to prefetch
    Priority      int           // Consumer priority
    Concurrency   int           // Number of concurrent workers
    RetryStrategy RetryStrategy // Retry configuration
    Timeout       time.Duration // Message processing timeout
    Arguments     map[string]interface{}
}

type RetryStrategy struct {
    MaxRetries      int           // Maximum retry attempts
    InitialInterval time.Duration // Initial backoff interval
    MaxInterval     time.Duration // Maximum backoff interval
    Multiplier      float64       // Exponential backoff multiplier
}
```

### QueueInfo

```go
type QueueInfo struct {
    Name          string
    Messages      int64     // Total messages
    Consumers     int       // Active consumers
    MessageBytes  int64     // Total message size
    PublishRate   float64   // Messages/sec
    DeliverRate   float64   // Messages/sec
    AckRate       float64   // Messages/sec
    Durable       bool
    AutoDelete    bool
    CreatedAt     time.Time
    LastMessageAt time.Time
}
```

### QueueStats

```go
type QueueStats struct {
    QueueCount      int64
    TotalMessages   int64
    TotalConsumers  int
    PublishRate     float64
    DeliverRate     float64
    AckRate         float64
    UnackedMessages int64
    ReadyMessages   int64
    Uptime          time.Duration
    MemoryUsed      int64
    ConnectionCount int
    Version         string
    Extra           map[string]interface{}
}
```

## Usage Examples

### Publishing Messages

```go
q, _ := queue.Get(app.Container())

// Simple publish
err := q.Publish(ctx, "orders", queue.Message{
    Body: []byte(`{"order_id":"12345","amount":99.99}`),
})

// With headers and priority
err = q.Publish(ctx, "orders", queue.Message{
    Body: []byte(`{"order_id":"12345"}`),
    Headers: map[string]string{
        "content-type": "application/json",
        "trace-id":     "abc123",
    },
    Priority: 8, // High priority
})

// With expiration
err = q.Publish(ctx, "orders", queue.Message{
    Body:       []byte(`{"order_id":"12345"}`),
    Expiration: 5 * time.Minute, // Message expires in 5 minutes
})
```

### Batch Publishing

```go
messages := []queue.Message{
    {Body: []byte(`{"id":1}`)},
    {Body: []byte(`{"id":2}`)},
    {Body: []byte(`{"id":3}`)},
}

err := q.PublishBatch(ctx, "bulk-orders", messages)
```

### Delayed Messages

```go
// Execute in 1 hour
err := q.PublishDelayed(ctx, "reminders", queue.Message{
    Body: []byte(`{"reminder":"meeting at 3pm"}`),
}, 1*time.Hour)

// Execute in 24 hours (scheduled jobs)
err = q.PublishDelayed(ctx, "daily-reports", queue.Message{
    Body: []byte(`{"type":"daily_summary"}`),
}, 24*time.Hour)
```

### Consuming Messages

```go
// Basic consumer
err := q.Consume(ctx, "orders", func(ctx context.Context, msg queue.Message) error {
    log.Printf("Processing order: %s", msg.Body)
    // Process message...
    return nil // Auto-ack on success
}, queue.DefaultConsumeOptions())

// Custom consumer with manual ack
opts := queue.ConsumeOptions{
    AutoAck:       false, // Manual acknowledgment
    PrefetchCount: 50,
    Concurrency:   10, // 10 concurrent workers
    Timeout:       60 * time.Second,
}

err = q.Consume(ctx, "orders", func(ctx context.Context, msg queue.Message) error {
    // Process message...
    if err := processOrder(msg.Body); err != nil {
        // Nack and requeue
        q.Nack(ctx, msg.ID, true)
        return err
    }
    
    // Acknowledge success
    return q.Ack(ctx, msg.ID)
}, opts)
```

### Worker Pool with Concurrency

```go
opts := queue.ConsumeOptions{
    AutoAck:       false,
    PrefetchCount: 100,
    Concurrency:   20, // 20 concurrent workers
    Timeout:       30 * time.Second,
    RetryStrategy: queue.RetryStrategy{
        MaxRetries:      5,
        InitialInterval: 1 * time.Second,
        MaxInterval:     60 * time.Second,
        Multiplier:      2.0,
    },
}

err := q.Consume(ctx, "heavy-tasks", func(ctx context.Context, msg queue.Message) error {
    // CPU-intensive work
    result, err := processHeavyTask(ctx, msg.Body)
    if err != nil {
        return err // Will retry based on RetryStrategy
    }
    
    log.Printf("Task completed: %v", result)
    return nil
}, opts)
```

### Priority Queues

```go
// Declare queue with priority support
opts := queue.QueueOptions{
    Durable:     true,
    MaxPriority: 10, // Enable priorities 0-10
}
q.DeclareQueue(ctx, "priority-tasks", opts)

// Publish high-priority message
q.Publish(ctx, "priority-tasks", queue.Message{
    Body:     []byte(`{"task":"critical"}`),
    Priority: 10, // Highest priority
})

// Publish normal-priority message
q.Publish(ctx, "priority-tasks", queue.Message{
    Body:     []byte(`{"task":"normal"}`),
    Priority: 5,
})

// High-priority messages are consumed first
```

### Dead Letter Queues

```go
// Declare queue with DLQ
opts := queue.QueueOptions{
    Durable:         true,
    DeadLetterQueue: "failed-orders", // DLQ name
}
q.DeclareQueue(ctx, "orders", opts)

// Consumer with retry strategy
consumeOpts := queue.ConsumeOptions{
    AutoAck:       false,
    PrefetchCount: 10,
    RetryStrategy: queue.RetryStrategy{
        MaxRetries:      3,
        InitialInterval: 1 * time.Second,
        MaxInterval:     10 * time.Second,
        Multiplier:      2.0,
    },
}

err := q.Consume(ctx, "orders", func(ctx context.Context, msg queue.Message) error {
    if err := processOrder(msg.Body); err != nil {
        // After max retries, message goes to DLQ
        return err
    }
    return nil
}, consumeOpts)

// Check DLQ
deadLetters, err := q.GetDeadLetterQueue(ctx, "orders")
for _, msg := range deadLetters {
    log.Printf("Failed message: %s", msg.Body)
    
    // Optionally requeue after fixing issue
    if shouldRetry(msg) {
        q.RequeueDeadLetter(ctx, "orders", msg.ID)
    }
}
```

### Queue Management

```go
// List all queues
queues, err := q.ListQueues(ctx)
for _, name := range queues {
    log.Printf("Queue: %s", name)
}

// Get queue info
info, err := q.GetQueueInfo(ctx, "orders")
log.Printf("Queue: %s, Messages: %d, Consumers: %d", 
    info.Name, info.Messages, info.Consumers)

// Purge queue (delete all messages)
err = q.PurgeQueue(ctx, "orders")

// Delete queue
err = q.DeleteQueue(ctx, "old-queue")
```

### Health Checks and Stats

```go
// Check connection health
err := q.Ping(ctx)
if err != nil {
    log.Printf("Queue unhealthy: %v", err)
}

// Get system stats
stats, err := q.Stats(ctx)
log.Printf("Queues: %d, Messages: %d, Consumers: %d",
    stats.QueueCount, stats.TotalMessages, stats.TotalConsumers)
log.Printf("Publish rate: %.2f msg/s, Deliver rate: %.2f msg/s",
    stats.PublishRate, stats.DeliverRate)
```

### Graceful Shutdown

```go
// Stop specific consumer
err := q.StopConsuming(ctx, "orders")

// Stop all consumers and disconnect
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err = q.Disconnect(ctx)
```

## Backend Comparison

| Feature | InMemory | Redis | RabbitMQ | NATS |
|---------|----------|-------|----------|------|
| **Persistence** | ❌ | ✅ | ✅ | ✅ |
| **Distributed** | ❌ | ✅ | ✅ | ✅ |
| **Priority Queues** | ✅ | ✅ | ✅ | ❌ |
| **Delayed Messages** | ✅ | ✅ | ✅ (plugin) | ❌ |
| **Dead Letter Queue** | ✅ | ✅ | ✅ | ✅ |
| **Max Throughput** | ~1M msg/s | ~100K msg/s | ~50K msg/s | ~500K msg/s |
| **Latency** | <1μs | <1ms | ~2ms | <1ms |
| **Clustering** | ❌ | ✅ | ✅ | ✅ |
| **Best For** | Testing | General purpose | Complex routing | High throughput |

### When to Use Each Backend

**InMemory**
- Local development and testing
- Proof of concepts
- Single-node deployments
- No persistence needed

**Redis**
- Simple pub/sub patterns
- Already using Redis for caching
- Moderate throughput requirements
- Good operational tooling

**RabbitMQ**
- Complex routing requirements
- Need for strong message guarantees
- Enterprise messaging patterns
- Existing AMQP infrastructure

**NATS**
- Very high throughput needs
- Microservices communication
- Cloud-native architectures
- Low-latency requirements

## Testing

### Unit Testing

```go
func TestQueueOperations(t *testing.T) {
    // Use in-memory backend for tests
    config := queue.Config{
        Driver: "inmemory",
    }
    
    logger := logger.NewNoopLogger()
    metrics := metrics.NewNoopMetrics()
    
    q := queue.NewInMemoryQueue(config, logger, metrics)
    
    ctx := context.Background()
    err := q.Connect(ctx)
    if err != nil {
        t.Fatal(err)
    }
    defer q.Disconnect(ctx)
    
    // Test publish
    err = q.Publish(ctx, "test", queue.Message{
        Body: []byte("test"),
    })
    if err != nil {
        t.Errorf("Publish failed: %v", err)
    }
    
    // Test consume
    received := make(chan queue.Message, 1)
    err = q.Consume(ctx, "test", func(ctx context.Context, msg queue.Message) error {
        received <- msg
        return nil
    }, queue.DefaultConsumeOptions())
    if err != nil {
        t.Errorf("Consume failed: %v", err)
    }
    
    select {
    case msg := <-received:
        if string(msg.Body) != "test" {
            t.Errorf("Expected 'test', got '%s'", msg.Body)
        }
    case <-time.After(1 * time.Second):
        t.Error("Timeout waiting for message")
    }
}
```

### Integration Testing

```go
func TestRabbitMQIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }
    
    config := queue.Config{
        Driver: "rabbitmq",
        URL:    "amqp://guest:guest@localhost:5672/",
    }
    
    logger := logger.NewTestLogger(t)
    metrics := metrics.NewNoopMetrics()
    
    q, err := queue.NewRabbitMQQueue(config, logger, metrics)
    if err != nil {
        t.Fatal(err)
    }
    
    // Run tests...
}
```

## Best Practices

### Message Design

```go
// ✅ DO: Use structured messages with metadata
type OrderMessage struct {
    OrderID   string    `json:"order_id"`
    UserID    string    `json:"user_id"`
    Amount    float64   `json:"amount"`
    CreatedAt time.Time `json:"created_at"`
}

msg := queue.Message{
    Body: json.Marshal(OrderMessage{...}),
    Headers: map[string]string{
        "content-type": "application/json",
        "version":      "v1",
        "trace-id":     ctx.Value("trace-id"),
    },
}

// ❌ DON'T: Send unstructured or overly large messages
msg := queue.Message{
    Body: []byte("process order 12345"), // Too vague
}
```

### Error Handling

```go
// ✅ DO: Implement proper retry logic
err := q.Consume(ctx, "orders", func(ctx context.Context, msg queue.Message) error {
    if err := processOrder(msg.Body); err != nil {
        if isRetryable(err) {
            return err // Will retry with backoff
        }
        // Permanent error - log and ack
        log.Printf("Permanent error: %v", err)
        return nil // Ack to prevent retry
    }
    return nil
}, queue.ConsumeOptions{
    RetryStrategy: queue.RetryStrategy{
        MaxRetries:      5,
        InitialInterval: 1 * time.Second,
        MaxInterval:     60 * time.Second,
        Multiplier:      2.0,
    },
})

// ❌ DON'T: Swallow errors
err := q.Consume(ctx, "orders", func(ctx context.Context, msg queue.Message) error {
    processOrder(msg.Body) // Ignoring error
    return nil
}, opts)
```

### Context Propagation

```go
// ✅ DO: Pass context with timeouts
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

err := q.Publish(ctx, "orders", msg)

// ✅ DO: Propagate trace IDs
err = q.Consume(ctx, "orders", func(ctx context.Context, msg queue.Message) error {
    traceID := msg.Headers["trace-id"]
    ctx = context.WithValue(ctx, "trace-id", traceID)
    
    return processOrder(ctx, msg.Body)
}, opts)
```

### Resource Management

```go
// ✅ DO: Use connection pooling
config := queue.Config{
    MaxConnections:     20,
    MaxIdleConnections: 10,
    ConnectTimeout:     10 * time.Second,
}

// ✅ DO: Implement graceful shutdown
func shutdown(q queue.Queue) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    
    // Stop consumers first
    q.StopConsuming(ctx, "orders")
    
    // Then disconnect
    q.Disconnect(ctx)
}

// ❌ DON'T: Create new connections for each operation
for i := 0; i < 1000; i++ {
    q, _ := queue.NewRedisQueue(config, logger, metrics)
    q.Connect(ctx)
    q.Publish(ctx, "orders", msg)
    q.Disconnect(ctx)
}
```

### Monitoring

```go
// ✅ DO: Monitor queue depth
ticker := time.NewTicker(30 * time.Second)
go func() {
    for range ticker.C {
        info, _ := q.GetQueueInfo(ctx, "orders")
        if info.Messages > 10000 {
            log.Printf("WARNING: Queue depth high: %d", info.Messages)
        }
    }
}()

// ✅ DO: Track processing metrics
err := q.Consume(ctx, "orders", func(ctx context.Context, msg queue.Message) error {
    start := time.Now()
    defer func() {
        duration := time.Since(start)
        metrics.Histogram("queue.process.duration", duration.Seconds())
    }()
    
    return processOrder(msg.Body)
}, opts)
```

## Performance Tuning

### Throughput Optimization

```go
// High throughput configuration
config := queue.Config{
    MaxConnections:     50,
    MaxIdleConnections: 25,
    DefaultPrefetch:    100,
    DefaultConcurrency: 20,
    EnablePersistence:  false, // Trade durability for speed (if acceptable)
}

opts := queue.ConsumeOptions{
    AutoAck:       true,  // Skip ack overhead if messages can be lost
    PrefetchCount: 100,   // Fetch many messages at once
    Concurrency:   20,    // Process many messages concurrently
}
```

### Latency Optimization

```go
// Low latency configuration
config := queue.Config{
    MaxConnections:     10,
    DefaultPrefetch:    1,
    DefaultConcurrency: 1,
    ConnectTimeout:     5 * time.Second,
    ReadTimeout:        5 * time.Second,
}

opts := queue.ConsumeOptions{
    PrefetchCount: 1,  // Minimize prefetch for lowest latency
    Concurrency:   1,  // Sequential processing
}
```

### Memory Optimization

```go
// Memory-constrained configuration
config := queue.Config{
    MaxConnections:     5,
    MaxIdleConnections: 2,
    DefaultPrefetch:    10,
    MaxMessageSize:     65536, // 64KB max
}

opts := queue.QueueOptions{
    MaxLength:      1000,  // Limit queue size
    MaxLengthBytes: 10485760, // 10MB max
    MessageTTL:     5 * time.Minute,
}
```

## Troubleshooting

### Connection Issues

```go
// Check connectivity
err := q.Ping(ctx)
if err != nil {
    log.Printf("Connection failed: %v", err)
    
    // Try reconnecting
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    
    if err := q.Connect(ctx); err != nil {
        log.Printf("Reconnection failed: %v", err)
    }
}
```

### Message Not Consumed

1. **Check queue exists**: `ListQueues(ctx)`
2. **Verify consumer is running**: `GetQueueInfo(ctx, "queue-name")`
3. **Check prefetch settings**: Increase `PrefetchCount`
4. **Monitor consumer errors**: Add logging to `MessageHandler`

### High Latency

1. **Reduce prefetch**: Lower `PrefetchCount` to 1-10
2. **Increase concurrency**: Raise `Concurrency` to match workload
3. **Check network**: Use same datacenter/region for queue backend
4. **Profile handler**: Identify slow operations in `MessageHandler`

### Memory Usage

1. **Limit queue size**: Set `MaxLength` or `MaxLengthBytes`
2. **Add message TTL**: Set `MessageTTL` to expire old messages
3. **Reduce prefetch**: Lower `PrefetchCount`
4. **Monitor stats**: Use `Stats()` to track memory usage

### Common Errors

```go
// ErrNotConnected
// Solution: Call Connect() before operations

// ErrQueueNotFound
// Solution: Call DeclareQueue() first

// ErrTimeout
// Solution: Increase timeout or check backend health

// ErrMessageTooLarge
// Solution: Reduce message size or increase MaxMessageSize

// ErrQueueFull
// Solution: Increase MaxLength or add consumers
```

## Architecture

### Extension Lifecycle

```
Register → Start → [Running] → Stop
    ↓        ↓                    ↓
  Config  Connect            Disconnect
  Create Queue            Stop Consumers
  DI Register
```

### Message Flow

```
Publisher → Publish() → [Backend Queue] → Consume() → Handler
                             ↓                           ↓
                        Dead Letter ←────────────── Error + Max Retries
```

### Concurrency Model

Each consumer spawns a worker pool:

```
Consumer → Worker Pool (n goroutines) → Handler
              ↓
        Prefetch Buffer (m messages)
              ↓
        Rate Limiting & Backpressure
```

## License

MIT License - Part of the Forge Framework

## Contributing

Contributions welcome! Please ensure:
- Tests pass: `go test ./...`
- Coverage maintained: `go test -cover`
- Linting clean: `golangci-lint run`
- Examples work: Test with real backends

## Support

- Documentation: https://github.com/xraph/forge
- Issues: https://github.com/xraph/forge/issues
- Discussions: https://github.com/xraph/forge/discussions


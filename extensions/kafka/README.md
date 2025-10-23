# Kafka Extension

The Kafka extension provides a production-ready Apache Kafka client with support for producers, consumers, and consumer groups.

## Features

- **Producer Support**: Synchronous and asynchronous message publishing
- **Consumer Support**: Simple consumers and consumer groups
- **Admin Operations**: Topic management and metadata queries
- **TLS/SSL**: Secure connections with mTLS support
- **SASL Authentication**: Multiple SASL mechanisms (PLAIN, SCRAM-SHA-256, SCRAM-SHA-512)
- **Compression**: Support for gzip, snappy, lz4, and zstd
- **Idempotent Producer**: Exactly-once semantics support
- **Consumer Groups**: Automatic partition rebalancing
- **Metrics & Tracing**: Built-in observability

## Installation

```bash
go get github.com/IBM/sarama
go get github.com/xdg-go/scram
```

## Basic Usage

```go
package main

import (
    "context"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/kafka"
)

func main() {
    app := forge.New("my-app")
    
    // Add Kafka extension
    app.AddExtension(kafka.NewExtension(
        kafka.WithBrokers("localhost:9092"),
        kafka.WithClientID("my-app"),
        kafka.WithProducer(true),
        kafka.WithConsumer(true),
    ))
    
    // Start application
    if err := app.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    
    // Get Kafka client
    var client kafka.Kafka
    app.Container().Resolve(&client)
    
    // Produce message
    err := client.SendMessage("my-topic", []byte("key"), []byte("value"))
    if err != nil {
        log.Fatal(err)
    }
    
    // Consume messages
    err = client.Consume(context.Background(), []string{"my-topic"}, func(msg *sarama.ConsumerMessage) error {
        log.Printf("Received: %s", string(msg.Value))
        return nil
    })
    
    app.Wait()
}
```

## Configuration

### YAML Configuration

```yaml
kafka:
  brokers:
    - localhost:9092
    - localhost:9093
  client_id: my-app
  version: "3.0.0"
  
  # Producer settings
  producer_enabled: true
  producer_compression: snappy
  producer_idempotent: true
  producer_acks: all
  
  # Consumer settings
  consumer_enabled: true
  consumer_group_id: my-group
  consumer_offsets: newest
  consumer_group_rebalance: sticky
  
  # TLS settings
  enable_tls: true
  tls_cert_file: /path/to/cert.pem
  tls_key_file: /path/to/key.pem
  tls_ca_file: /path/to/ca.pem
  
  # SASL settings
  enable_sasl: true
  sasl_mechanism: SCRAM-SHA-512
  sasl_username: user
  sasl_password: pass
```

### Programmatic Configuration

```go
app.AddExtension(kafka.NewExtension(
    kafka.WithBrokers("localhost:9092"),
    kafka.WithVersion("3.0.0"),
    kafka.WithTLS("cert.pem", "key.pem", "ca.pem", false),
    kafka.WithSASL("SCRAM-SHA-512", "user", "pass"),
    kafka.WithCompression("snappy"),
    kafka.WithIdempotent(true),
    kafka.WithConsumerGroup("my-group"),
))
```

## Producer Examples

### Synchronous Publishing

```go
err := client.SendMessage(
    "my-topic",
    []byte("key"),
    []byte("message-value"),
)
```

### Asynchronous Publishing

```go
err := client.SendMessageAsync(
    "my-topic",
    []byte("key"),
    []byte("message-value"),
)
```

### Batch Publishing

```go
messages := []*kafka.ProducerMessage{
    {Topic: "topic1", Key: []byte("k1"), Value: []byte("v1")},
    {Topic: "topic2", Key: []byte("k2"), Value: []byte("v2")},
}

err := client.SendMessages(messages)
```

## Consumer Examples

### Simple Consumer

```go
err := client.Consume(ctx, []string{"my-topic"}, func(msg *sarama.ConsumerMessage) error {
    log.Printf("Topic: %s, Partition: %d, Offset: %d, Value: %s",
        msg.Topic, msg.Partition, msg.Offset, string(msg.Value))
    return nil
})
```

### Partition Consumer

```go
err := client.ConsumePartition(
    ctx,
    "my-topic",
    0,  // partition
    sarama.OffsetNewest,
    func(msg *sarama.ConsumerMessage) error {
        // Process message
        return nil
    },
)
```

### Consumer Group

```go
type MyHandler struct{}

func (h *MyHandler) Setup(sarama.ConsumerGroupSession) error {
    return nil
}

func (h *MyHandler) Cleanup(sarama.ConsumerGroupSession) error {
    return nil
}

func (h *MyHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
    for msg := range claim.Messages() {
        log.Printf("Message: %s", string(msg.Value))
        session.MarkMessage(msg, "")
    }
    return nil
}

// Join consumer group
err := client.JoinConsumerGroup(ctx, "my-group", []string{"my-topic"}, &MyHandler{})
```

## Admin Operations

### Create Topic

```go
err := client.CreateTopic("my-topic", kafka.TopicConfig{
    NumPartitions:     3,
    ReplicationFactor: 2,
})
```

### List Topics

```go
topics, err := client.ListTopics()
```

### Describe Topic

```go
metadata, err := client.DescribeTopic("my-topic")
log.Printf("Partitions: %d", len(metadata.Partitions))
```

### Delete Topic

```go
err := client.DeleteTopic("my-topic")
```

## Security

### TLS Configuration

```go
app.AddExtension(kafka.NewExtension(
    kafka.WithTLS(
        "/path/to/client-cert.pem",
        "/path/to/client-key.pem",
        "/path/to/ca-cert.pem",
        false, // skipVerify
    ),
))
```

### SASL/PLAIN

```go
app.AddExtension(kafka.NewExtension(
    kafka.WithSASL("PLAIN", "username", "password"),
))
```

### SASL/SCRAM

```go
app.AddExtension(kafka.NewExtension(
    kafka.WithSASL("SCRAM-SHA-512", "username", "password"),
))
```

## Observability

### Metrics

The extension automatically tracks:
- `kafka.messages.sent` - Messages sent counter
- `kafka.messages.received` - Messages received counter
- `kafka.bytes.sent` - Bytes sent gauge
- `kafka.bytes.received` - Bytes received gauge

### Client Statistics

```go
stats := client.GetStats()
log.Printf("Messages sent: %d", stats.MessagesSent)
log.Printf("Messages received: %d", stats.MessagesReceived)
log.Printf("Errors: %d", stats.Errors)
```

## Best Practices

1. **Use Consumer Groups**: For scalable consumption, use consumer groups instead of simple consumers
2. **Enable Idempotence**: For exactly-once semantics, enable idempotent producer
3. **Compression**: Use compression (snappy or lz4) for better throughput
4. **Batch Publishing**: Use `SendMessages()` for batch operations
5. **Error Handling**: Always check for errors and implement retry logic
6. **Graceful Shutdown**: Always call `Close()` on shutdown

## Error Handling

```go
if err := client.SendMessage(topic, key, value); err != nil {
    // Check specific error types
    switch err {
    case kafka.ErrProducerNotEnabled:
        log.Println("Producer is not enabled")
    case kafka.ErrConnectionFailed:
        log.Println("Connection failed, will retry")
    default:
        log.Printf("Send failed: %v", err)
    }
}
```

## Testing

```go
func TestKafkaIntegration(t *testing.T) {
    app := forge.New("test-app")
    app.AddExtension(kafka.NewExtension(
        kafka.WithBrokers("localhost:9092"),
    ))
    
    ctx := context.Background()
    if err := app.Start(ctx); err != nil {
        t.Fatal(err)
    }
    defer app.Stop(ctx)
    
    var client kafka.Kafka
    app.Container().Resolve(&client)
    
    // Test producer
    err := client.SendMessage("test-topic", []byte("key"), []byte("value"))
    if err != nil {
        t.Fatalf("Send failed: %v", err)
    }
}
```

## License

Part of the Forge framework.

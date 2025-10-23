# MQTT Extension

The MQTT extension provides a production-ready MQTT client with support for pub/sub operations, QoS levels, and connection management.

## Features

- **Pub/Sub Support**: Publish and subscribe to MQTT topics
- **QoS Levels**: Support for QoS 0, 1, and 2
- **TLS/SSL**: Secure connections with mTLS support
- **Auto-Reconnect**: Automatic reconnection on connection loss
- **Last Will and Testament**: LWT message support
- **Message Persistence**: Memory and file-based message stores
- **Metrics & Tracing**: Built-in observability

## Installation

```bash
go get github.com/eclipse/paho.mqtt.golang
```

## Basic Usage

```go
package main

import (
    "context"
    "log"
    
    "github.com/xraph/forge"
    "github.com/xraph/forge/extensions/mqtt"
)

func main() {
    app := forge.New("my-app")
    
    // Add MQTT extension
    app.AddExtension(mqtt.NewExtension(
        mqtt.WithBroker("tcp://localhost:1883"),
        mqtt.WithClientID("my-app"),
    ))
    
    // Start application
    if err := app.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
    
    // Get MQTT client
    var client mqtt.MQTT
    app.Container().Resolve(&client)
    
    // Subscribe to topic
    err := client.Subscribe("sensors/temperature", 1, func(c mqttclient.Client, msg mqttclient.Message) {
        log.Printf("Received: %s", string(msg.Payload()))
    })
    
    // Publish message
    err = client.Publish("sensors/temperature", 1, false, "25.5")
    
    app.Wait()
}
```

## Configuration

### YAML Configuration

```yaml
mqtt:
  broker: tcp://localhost:1883
  client_id: my-app
  username: user
  password: pass
  clean_session: true
  keep_alive: 60s
  
  # TLS settings
  enable_tls: true
  tls_cert_file: /path/to/cert.pem
  tls_key_file: /path/to/key.pem
  tls_ca_file: /path/to/ca.pem
  
  # QoS and reliability
  default_qos: 1
  auto_reconnect: true
  max_reconnect_delay: 10m
  
  # Last Will and Testament
  will_enabled: true
  will_topic: clients/status
  will_payload: offline
  will_qos: 1
  will_retained: true
  
  # Message handling
  message_store: file
  store_directory: /var/mqtt/store
```

### Programmatic Configuration

```go
app.AddExtension(mqtt.NewExtension(
    mqtt.WithBroker("tcp://localhost:1883"),
    mqtt.WithClientID("my-app"),
    mqtt.WithCredentials("user", "pass"),
    mqtt.WithTLS("cert.pem", "key.pem", "ca.pem", false),
    mqtt.WithQoS(1),
    mqtt.WithWill("clients/status", "offline", 1, true),
    mqtt.WithAutoReconnect(true),
))
```

## Publishing

### Synchronous Publishing

```go
err := client.Publish(
    "sensors/temperature",
    1,     // QoS
    false, // retained
    "25.5",
)
```

### Asynchronous Publishing

```go
err := client.PublishAsync(
    "sensors/temperature",
    1,
    false,
    "25.5",
)
```

### Publishing JSON

```go
type SensorData struct {
    Temperature float64 `json:"temperature"`
    Humidity    float64 `json:"humidity"`
}

data := SensorData{Temperature: 25.5, Humidity: 60.0}
err := client.Publish("sensors/data", 1, false, data)
```

## Subscribing

### Single Topic

```go
err := client.Subscribe("sensors/+/temperature", 1, func(c mqttclient.Client, msg mqttclient.Message) {
    log.Printf("Topic: %s, Payload: %s", msg.Topic(), string(msg.Payload()))
})
```

### Multiple Topics

```go
filters := map[string]byte{
    "sensors/+/temperature": 1,
    "sensors/+/humidity":    2,
}

err := client.SubscribeMultiple(filters, func(c mqttclient.Client, msg mqttclient.Message) {
    log.Printf("Received from %s: %s", msg.Topic(), string(msg.Payload()))
})
```

### Unsubscribe

```go
err := client.Unsubscribe("sensors/+/temperature")
```

## QoS Levels

MQTT supports three Quality of Service levels:

- **QoS 0**: At most once (fire and forget)
- **QoS 1**: At least once (acknowledged delivery)
- **QoS 2**: Exactly once (assured delivery)

```go
// QoS 0 - Fire and forget
client.Publish("topic", 0, false, "message")

// QoS 1 - At least once
client.Publish("topic", 1, false, "message")

// QoS 2 - Exactly once
client.Publish("topic", 2, false, "message")
```

## Topic Wildcards

MQTT supports two wildcards:

- **+**: Single level wildcard
- **#**: Multi-level wildcard

```go
// Subscribe to all sensors
client.Subscribe("sensors/+/temperature", 1, handler)

// Subscribe to all messages under sensors
client.Subscribe("sensors/#", 1, handler)
```

## Retained Messages

```go
// Publish retained message
client.Publish("sensors/temperature", 1, true, "25.5")

// New subscribers will immediately receive the last retained message
```

## Last Will and Testament

```go
app.AddExtension(mqtt.NewExtension(
    mqtt.WithWill(
        "clients/status",  // topic
        "offline",         // payload
        1,                 // qos
        true,              // retained
    ),
))
```

## TLS/SSL Configuration

### Basic TLS

```go
app.AddExtension(mqtt.NewExtension(
    mqtt.WithBroker("ssl://localhost:8883"),
    mqtt.WithTLS("", "", "ca.pem", false),
))
```

### Mutual TLS (mTLS)

```go
app.AddExtension(mqtt.NewExtension(
    mqtt.WithBroker("ssl://localhost:8883"),
    mqtt.WithTLS(
        "client-cert.pem",
        "client-key.pem",
        "ca.pem",
        false, // skipVerify
    ),
))
```

## Connection Management

### Connection Status

```go
if client.IsConnected() {
    log.Println("Connected to broker")
}
```

### Manual Reconnect

```go
if err := client.Reconnect(); err != nil {
    log.Printf("Reconnect failed: %v", err)
}
```

### Disconnect

```go
if err := client.Disconnect(ctx); err != nil {
    log.Printf("Disconnect failed: %v", err)
}
```

## Observability

### Metrics

The extension automatically tracks:
- `mqtt.connections` - Connection status counter
- `mqtt.messages.published` - Messages published counter
- `mqtt.messages.received` - Messages received counter
- `mqtt.reconnects` - Reconnection attempts counter

### Client Statistics

```go
stats := client.GetStats()
log.Printf("Connected: %v", stats.Connected)
log.Printf("Messages sent: %d", stats.MessagesSent)
log.Printf("Messages received: %d", stats.MessagesReceived)
log.Printf("Reconnects: %d", stats.Reconnects)
```

### Subscriptions

```go
subs := client.GetSubscriptions()
for _, sub := range subs {
    log.Printf("Topic: %s, QoS: %d", sub.Topic, sub.QoS)
}
```

## Best Practices

1. **Use Clean Session**: Set `clean_session: false` for persistent sessions
2. **QoS Selection**: Use QoS 1 for most use cases (balance between reliability and performance)
3. **Topic Design**: Use hierarchical topics (e.g., `building/floor/room/sensor`)
4. **Retained Messages**: Use for status messages that new subscribers should receive
5. **LWT Configuration**: Always configure Last Will and Testament for clients
6. **Wildcard Subscriptions**: Be careful with `#` wildcard, it can receive many messages

## Message Store

### Memory Store (Default)

```go
app.AddExtension(mqtt.NewExtension(
    mqtt.WithConfig(mqtt.Config{
        MessageStore: "memory",
    }),
))
```

### File Store

```go
app.AddExtension(mqtt.NewExtension(
    mqtt.WithConfig(mqtt.Config{
        MessageStore:   "file",
        StoreDirectory: "/var/mqtt/store",
    }),
))
```

## Error Handling

```go
if err := client.Publish(topic, qos, retained, payload); err != nil {
    switch err {
    case mqtt.ErrNotConnected:
        log.Println("Not connected to broker")
    case mqtt.ErrPublishFailed:
        log.Println("Publish failed")
    default:
        log.Printf("Error: %v", err)
    }
}
```

## Testing

```go
func TestMQTTIntegration(t *testing.T) {
    app := forge.New("test-app")
    app.AddExtension(mqtt.NewExtension(
        mqtt.WithBroker("tcp://localhost:1883"),
    ))
    
    ctx := context.Background()
    if err := app.Start(ctx); err != nil {
        t.Fatal(err)
    }
    defer app.Stop(ctx)
    
    var client mqtt.MQTT
    app.Container().Resolve(&client)
    
    // Test publish
    err := client.Publish("test/topic", 1, false, "test message")
    if err != nil {
        t.Fatalf("Publish failed: %v", err)
    }
}
```

## License

Part of the Forge framework.

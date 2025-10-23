# Events Extension Demo

This example demonstrates the Forge v2 Events Extension, which provides event-driven architecture with:

- **Event Bus** - Publish/subscribe pattern for events
- **Event Store** - Event sourcing with persistence
- **Event Handlers** - Type-safe event processing
- **Multiple Brokers** - Memory, NATS, Redis support

## Features Demonstrated

1. **Event Publishing** - Creating and publishing domain events
2. **Event Subscription** - Subscribing to events with handlers
3. **Event Storage** - Persisting events to a store
4. **Event Sourcing** - Retrieving events by aggregate ID
5. **Event Bus Stats** - Monitoring event bus health and statistics

## Running the Example

```bash
cd v2/examples/events-demo
go run main.go
```

## Expected Output

```
Published event: <event-id-1>
Received event: user.created (ID: <event-id-1>, Aggregate: user-1)
Published event: <event-id-2>
Received event: user.created (ID: <event-id-2>, Aggregate: user-2)
...

Total events in store: 5
Event bus stats: map[brokers_count:1 default_broker:memory name:event-bus started:true ...]

Events demo completed successfully!
```

## Configuration

The Events Extension supports configuration through `config.yaml`:

```yaml
extensions:
  events:
    bus:
      default_broker: memory
      max_retries: 3
      retry_delay: 5s
      buffer_size: 1000
      worker_count: 10
    
    store:
      type: memory  # or postgres, mongodb
      database: events_db
      table: events
    
    brokers:
      - name: memory
        type: memory
        enabled: true
        priority: 1
```

## Key Concepts

### Event Structure
```go
event := core.NewEvent("user.created", "user-123", userData).
    WithSource("user-service").
    WithCorrelationID(requestID).
    WithMetadata("ip_address", "192.168.1.1")
```

### Event Handlers
```go
handler := core.NewTypedEventHandler(
    "user-handler",
    []string{"user.created", "user.updated"},
    func(ctx context.Context, event *core.Event) error {
        // Process event
        return nil
    },
)
```

### Event Sourcing
```go
// Save events
eventStore.SaveEvent(ctx, event)

// Retrieve events by aggregate
events, _ := eventStore.GetEventsByAggregate(ctx, "order-123", 0)

// Create snapshot
snapshot := core.NewSnapshot(aggregateID, "Order", orderData, version)
eventStore.CreateSnapshot(ctx, snapshot)
```

## Next Steps

- Explore typed handlers with retry policies
- Implement event projections
- Add NATS or Redis brokers
- Create event sagas for complex workflows
- Implement CQRS patterns


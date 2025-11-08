package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events"
	"github.com/xraph/forge/extensions/events/core"
	forgetesting "github.com/xraph/forge/testing"
)

func TestEventsExtension_Registration(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	// Verify extension is registered
	assert.Equal(t, "events", ext.Name())
	assert.Equal(t, "2.0.0", ext.Version())
	assert.Contains(t, ext.Description(), "Event-driven")
}

func TestEventsExtension_Lifecycle(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()

	// Start
	err = app.Start(ctx)
	require.NoError(t, err)

	// Health check
	err = ext.Health(ctx)
	assert.NoError(t, err)

	// Stop
	err = app.Stop(ctx)
	require.NoError(t, err)
}

func TestEventsExtension_DIRegistration(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event service
	eventService := forge.Must[*events.EventService](app.Container(), "events")
	assert.NotNil(t, eventService)

	// Get event bus
	eventBus := forge.Must[core.EventBus](app.Container(), "eventBus")
	assert.NotNil(t, eventBus)

	// Get event store
	eventStore := forge.Must[core.EventStore](app.Container(), "eventStore")
	assert.NotNil(t, eventStore)

	// Get handler registry
	registry := forge.Must[*core.HandlerRegistry](app.Container(), "eventHandlerRegistry")
	assert.NotNil(t, registry)
}

func TestEventsExtension_PublishSubscribe(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event bus
	eventBus := forge.Must[core.EventBus](app.Container(), "eventBus")

	// Create a handler
	received := make(chan *core.Event, 1)
	handler := core.EventHandlerFunc(func(ctx context.Context, event *core.Event) error {
		received <- event

		return nil
	})

	// Subscribe
	err = eventBus.Subscribe("user.created", handler)
	require.NoError(t, err)

	// Give subscription time to register
	time.Sleep(100 * time.Millisecond)

	// Publish event
	event := core.NewEvent("user.created", "user-123", map[string]string{
		"name":  "John Doe",
		"email": "john@example.com",
	})
	err = eventBus.Publish(ctx, event)
	require.NoError(t, err)

	// Wait for event
	select {
	case receivedEvent := <-received:
		assert.Equal(t, "user.created", receivedEvent.Type)
		assert.Equal(t, "user-123", receivedEvent.AggregateID)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventsExtension_EventStore(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event store
	eventStore := forge.Must[core.EventStore](app.Container(), "eventStore")

	// Save event
	event := core.NewEvent("order.created", "order-123", map[string]any{
		"total":    100.00,
		"currency": "USD",
	})
	err = eventStore.SaveEvent(ctx, event)
	require.NoError(t, err)

	// Retrieve event
	retrievedEvent, err := eventStore.GetEvent(ctx, event.ID)
	require.NoError(t, err)
	assert.Equal(t, event.ID, retrievedEvent.ID)
	assert.Equal(t, event.Type, retrievedEvent.Type)
	assert.Equal(t, event.AggregateID, retrievedEvent.AggregateID)

	// Get events by aggregate
	events, err := eventStore.GetEventsByAggregate(ctx, "order-123", 0)
	require.NoError(t, err)
	assert.Len(t, events, 1)
	assert.Equal(t, event.ID, events[0].ID)

	// Get event count
	count, err := eventStore.GetEventCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

func TestEventsExtension_TypedHandler(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event bus
	eventBus := forge.Must[core.EventBus](app.Container(), "eventBus")

	// Create typed handler
	received := make(chan *core.Event, 1)
	handler := core.NewTypedEventHandler(
		"user-handler",
		[]string{"user.created", "user.updated"},
		func(ctx context.Context, event *core.Event) error {
			received <- event

			return nil
		},
	)

	// Subscribe
	err = eventBus.Subscribe("user.created", handler)
	require.NoError(t, err)

	// Give subscription time to register
	time.Sleep(100 * time.Millisecond)

	// Publish matching event
	event := core.NewEvent("user.created", "user-123", map[string]string{
		"name": "Jane Doe",
	})
	err = eventBus.Publish(ctx, event)
	require.NoError(t, err)

	// Wait for event
	select {
	case receivedEvent := <-received:
		assert.Equal(t, "user.created", receivedEvent.Type)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventsExtension_MultipleHandlers(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event bus
	eventBus := forge.Must[core.EventBus](app.Container(), "eventBus")

	// Create multiple handlers
	received1 := make(chan *core.Event, 1)
	handler1 := core.EventHandlerFunc(func(ctx context.Context, event *core.Event) error {
		received1 <- event

		return nil
	})

	received2 := make(chan *core.Event, 1)
	handler2 := core.EventHandlerFunc(func(ctx context.Context, event *core.Event) error {
		received2 <- event

		return nil
	})

	// Subscribe both handlers
	err = eventBus.Subscribe("product.created", handler1)
	require.NoError(t, err)
	err = eventBus.Subscribe("product.created", handler2)
	require.NoError(t, err)

	// Give subscriptions time to register
	time.Sleep(100 * time.Millisecond)

	// Publish event
	event := core.NewEvent("product.created", "product-123", map[string]any{
		"name":  "Widget",
		"price": 29.99,
	})
	err = eventBus.Publish(ctx, event)
	require.NoError(t, err)

	// Both handlers should receive the event
	timeout := time.After(5 * time.Second)

	select {
	case <-received1:
	case <-timeout:
		t.Fatal("timeout waiting for handler1")
	}

	select {
	case <-received2:
	case <-timeout:
		t.Fatal("timeout waiting for handler2")
	}
}

func TestEventsExtension_EventSourcing(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event store
	eventStore := forge.Must[core.EventStore](app.Container(), "eventStore")

	// Create sequence of events for an aggregate
	aggregateID := "order-456"

	events := []*core.Event{
		core.NewEvent("order.created", aggregateID, map[string]any{"status": "pending"}).WithVersion(1),
		core.NewEvent("order.paid", aggregateID, map[string]any{"amount": 50.00}).WithVersion(2),
		core.NewEvent("order.shipped", aggregateID, map[string]any{"tracking": "ABC123"}).WithVersion(3),
		core.NewEvent("order.delivered", aggregateID, map[string]any{"delivered_at": time.Now()}).WithVersion(4),
	}

	// Save events
	for _, event := range events {
		err = eventStore.SaveEvent(ctx, event)
		require.NoError(t, err)
	}

	// Retrieve all events for aggregate
	retrievedEvents, err := eventStore.GetEventsByAggregate(ctx, aggregateID, 0)
	require.NoError(t, err)
	assert.Len(t, retrievedEvents, 4)

	// Verify order
	assert.Equal(t, "order.created", retrievedEvents[0].Type)
	assert.Equal(t, "order.paid", retrievedEvents[1].Type)
	assert.Equal(t, "order.shipped", retrievedEvents[2].Type)
	assert.Equal(t, "order.delivered", retrievedEvents[3].Type)

	// Get events from specific version
	partialEvents, err := eventStore.GetEventsByAggregate(ctx, aggregateID, 2)
	require.NoError(t, err)
	assert.Len(t, partialEvents, 3) // versions 2, 3, 4
}

func TestEventsExtension_Snapshot(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event store
	eventStore := forge.Must[core.EventStore](app.Container(), "eventStore")

	// Create snapshot
	aggregateID := "account-789"
	snapshot := core.NewSnapshot(aggregateID, "Account", map[string]any{
		"balance": 1000.00,
		"status":  "active",
	}, 10)

	err = eventStore.CreateSnapshot(ctx, snapshot)
	require.NoError(t, err)

	// Retrieve snapshot
	retrievedSnapshot, err := eventStore.GetSnapshot(ctx, aggregateID)
	require.NoError(t, err)
	assert.Equal(t, aggregateID, retrievedSnapshot.AggregateID)
	assert.Equal(t, "Account", retrievedSnapshot.Type)
	assert.Equal(t, 10, retrievedSnapshot.Version)
}

func TestEventsExtension_CustomConfig(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	config := events.Config{
		Bus: events.BusConfig{
			DefaultBroker:     "memory",
			MaxRetries:        5,
			RetryDelay:        time.Second,
			EnableMetrics:     true,
			BufferSize:        500,
			WorkerCount:       5,
			ProcessingTimeout: time.Second * 60,
		},
		Store: events.StoreConfig{
			Type:     "memory",
			Database: "",
			Table:    "custom_events",
		},
		Brokers: []events.BrokerConfig{
			{
				Name:     "memory",
				Type:     "memory",
				Enabled:  true,
				Priority: 1,
			},
		},
		Metrics: events.MetricsConfig{
			Enabled:          true,
			PublishInterval:  time.Second * 15,
			EnablePerType:    true,
			EnablePerHandler: true,
		},
	}

	ext := events.NewExtensionWithConfig(config)
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Verify extension started with custom config
	err = ext.Health(ctx)
	assert.NoError(t, err)
}

func TestEventsExtension_BusStats(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event bus
	eventBus := forge.Must[core.EventBus](app.Container(), "eventBus")

	// Get stats
	stats := eventBus.GetStats()
	assert.NotNil(t, stats)
	assert.Equal(t, "event-bus", stats["name"])
	assert.True(t, stats["started"].(bool))
	assert.GreaterOrEqual(t, stats["brokers_count"].(int), 1)
}

func TestEventsExtension_Unsubscribe(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event bus
	eventBus := forge.Must[core.EventBus](app.Container(), "eventBus")

	// Create handler
	handler := core.NewTypedEventHandler(
		"test-handler",
		[]string{"test.event"},
		func(ctx context.Context, event *core.Event) error {
			return nil
		},
	)

	// Subscribe
	err = eventBus.Subscribe("test.event", handler)
	require.NoError(t, err)

	// Unsubscribe
	err = eventBus.Unsubscribe("test.event", "test-handler")
	require.NoError(t, err)
}

func TestEventsExtension_BatchSave(t *testing.T) {
	app := forgetesting.NewTestApp("test-app", "1.0.0")

	ext := events.NewExtension()
	err := app.RegisterExtension(ext)
	require.NoError(t, err)

	ctx := context.Background()
	err = app.Start(ctx)
	require.NoError(t, err)

	defer app.Stop(ctx)

	// Get event store
	eventStore := forge.Must[core.EventStore](app.Container(), "eventStore")

	// Create multiple events
	events := []*core.Event{
		core.NewEvent("batch.test1", "agg-1", map[string]string{"data": "test1"}),
		core.NewEvent("batch.test2", "agg-2", map[string]string{"data": "test2"}),
		core.NewEvent("batch.test3", "agg-3", map[string]string{"data": "test3"}),
	}

	// Save events in batch
	err = eventStore.SaveEvents(ctx, events)
	require.NoError(t, err)

	// Verify all events are saved
	count, err := eventStore.GetEventCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

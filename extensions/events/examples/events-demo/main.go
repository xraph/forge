package main

import (
	"context"
	"fmt"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/events"
	"github.com/xraph/forge/extensions/events/core"
)

func main() {
	// Create app
	app := forge.NewApp(forge.AppConfig{
		Name:    "events-demo",
		Version: "1.0.0",
	})

	// Register events extension
	ext := events.NewExtension()
	if err := app.RegisterExtension(ext); err != nil {
		panic(err)
	}

	// Start app
	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		panic(err)
	}
	defer app.Stop(ctx)

	// Get event bus
	eventBus := forge.Must[core.EventBus](app.Container(), "eventBus")

	// Subscribe to events
	handler := core.EventHandlerFunc(func(ctx context.Context, event *core.Event) error {
		fmt.Printf("Received event: %s (ID: %s, Aggregate: %s)\n", event.Type, event.ID, event.AggregateID)
		return nil
	})

	if err := eventBus.Subscribe("user.created", handler); err != nil {
		panic(err)
	}

	// Give subscription time to register
	time.Sleep(100 * time.Millisecond)

	// Publish events
	for i := 1; i <= 5; i++ {
		event := core.NewEvent("user.created", fmt.Sprintf("user-%d", i), map[string]interface{}{
			"name":  fmt.Sprintf("User %d", i),
			"email": fmt.Sprintf("user%d@example.com", i),
		})

		if err := eventBus.Publish(ctx, event); err != nil {
			fmt.Printf("Failed to publish event: %v\n", err)
		} else {
			fmt.Printf("Published event: %s\n", event.ID)
		}
	}

	// Wait for events to be processed
	time.Sleep(2 * time.Second)

	// Get event store stats
	eventStore := forge.Must[core.EventStore](app.Container(), "eventStore")
	count, _ := eventStore.GetEventCount(ctx)
	fmt.Printf("\nTotal events in store: %d\n", count)

	// Get event bus stats
	stats := eventBus.GetStats()
	fmt.Printf("Event bus stats: %+v\n", stats)

	fmt.Println("\nEvents demo completed successfully!")
}

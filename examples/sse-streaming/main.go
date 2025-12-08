package main

import (
	"fmt"
	"log"
	"time"

	"github.com/xraph/forge"
)

// Event type definitions for AsyncAPI documentation
type ConnectedEvent struct {
	Message   string `json:"message" description:"Welcome message"`
	Timestamp int64  `json:"timestamp" description:"Connection timestamp"`
}

type UpdateEvent struct {
	Timestamp int64  `json:"timestamp" description:"Event timestamp"`
	Message   string `json:"message" description:"Update message"`
	Counter   int    `json:"counter" description:"Update counter"`
}

type MilestoneEvent struct {
	Count   int    `json:"count" description:"Milestone count"`
	Message string `json:"message" description:"Milestone message"`
}

func main() {
	// Create a new Forge application with AsyncAPI enabled
	app := forge.NewApp(forge.AppConfig{
		Name:        "SSE Streaming Example",
		Version:     "1.0.0",
		HTTPAddress: ":8080",
		RouterOptions: []forge.RouterOption{
			forge.WithAsyncAPI(forge.AsyncAPIConfig{
				Title:       "SSE Streaming API",
				Description: "Real-time event streaming using Server-Sent Events",
				Version:     "1.0.0",
				UIEnabled:   true,
				SpecEnabled: true,
			}),
		},
	})

	// Get the router
	router := app.Router()
	apiGroup := router.Group("/api/v1")
	apiGroup.SSE("/maximus", sseHandler,
		forge.WithName("maximus-events"),
		forge.WithTags("streaming", "maximus"),
		forge.WithSummary("Maximus Server-Sent Events stream"),
		forge.WithDescription("Streams periodic updates to clients using Server-Sent Events"),
		forge.WithSSEMessage("connected", ConnectedEvent{}),
		forge.WithSSEMessage("update", UpdateEvent{}),
	)

	// Register SSE endpoint with full AsyncAPI documentation
	router.SSE("/events", sseHandler,
		// OpenAPI/general metadata
		forge.WithName("sse-events"),
		forge.WithTags("streaming", "events"),
		forge.WithSummary("Server-Sent Events stream"),
		forge.WithDescription("Streams periodic updates to clients using Server-Sent Events"),

		// AsyncAPI-specific documentation
		forge.WithSSEMessage("connected", ConnectedEvent{}),
		forge.WithSSEMessage("update", UpdateEvent{}),
		forge.WithSSEMessage("milestone", MilestoneEvent{}),
		forge.WithAsyncAPITags("real-time", "notifications"),
		forge.WithAsyncAPIChannelDescription("Real-time event stream with periodic updates and milestones"),
		forge.WithAsyncAPIOperationID("subscribeToEvents"),
	)

	// Start the application
	if err := app.Run(); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

	fmt.Println("Server running on http://localhost:8080")
	fmt.Println("")
	fmt.Println("Endpoints:")
	fmt.Println("  SSE Stream:    curl -N http://localhost:8080/events")
	fmt.Println("  AsyncAPI Spec: http://localhost:8080/asyncapi.json")
	fmt.Println("  AsyncAPI UI:   http://localhost:8080/asyncapi")
	fmt.Println("")
	fmt.Println("The AsyncAPI spec documents all event types:")

	// Wait indefinitely
	select {}
}

// sseHandler demonstrates SSE streaming with ctx.WriteSSE
func sseHandler(ctx forge.Context) error {
	// Headers are already set automatically by router.SSE():
	// - Content-Type: text/event-stream
	// - Cache-Control: no-cache
	// - Connection: keep-alive
	// - X-Accel-Buffering: no

	// Send initial connection event (matches ConnectedEvent schema in AsyncAPI)
	connectedData := ConnectedEvent{
		Message:   "Welcome to SSE stream",
		Timestamp: time.Now().Unix(),
	}
	if err := ctx.WriteSSE("connected", connectedData); err != nil {
		return fmt.Errorf("failed to send connected event: %w", err)
	}

	// Send periodic updates
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	counter := 0

	for {
		select {
		case <-ctx.Context().Done():
			// Client disconnected
			return nil

		case t := <-ticker.C:
			counter++

			// Send structured update event (matches UpdateEvent schema in AsyncAPI)
			updateData := UpdateEvent{
				Timestamp: t.Unix(),
				Message:   fmt.Sprintf("Periodic update #%d", counter),
				Counter:   counter,
			}

			if err := ctx.WriteSSE("update", updateData); err != nil {
				return fmt.Errorf("failed to send update: %w", err)
			}

			// Send milestone event every 3rd update (matches MilestoneEvent schema)
			if counter%3 == 0 {
				milestoneData := MilestoneEvent{
					Count:   counter,
					Message: fmt.Sprintf("Milestone reached: %d updates sent", counter),
				}
				if err := ctx.WriteSSE("milestone", milestoneData); err != nil {
					return fmt.Errorf("failed to send milestone: %w", err)
				}
			}
		}
	}
}

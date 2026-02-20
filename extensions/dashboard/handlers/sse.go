package handlers

import (
	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/sse"
)

// HandleSSEEndpoint returns the SSE handler for the dashboard event stream.
// This handler is registered with router.EventStream() in the extension.
func HandleSSEEndpoint(broker *sse.Broker) forge.SSEHandler {
	return sse.HandleSSE(broker)
}

// HandleSSEStatus returns a JSON handler that reports SSE broker status.
func HandleSSEStatus(broker *sse.Broker) forge.Handler {
	return func(ctx forge.Context) error {
		return ctx.JSON(200, map[string]any{
			"connected_clients": broker.ClientCount(),
			"status":            "active",
		})
	}
}

package sse

import (
	"time"

	"github.com/xraph/forge"
)

// HandleSSE returns a forge SSE handler that connects clients to the broker.
// The handler runs for the lifetime of the client connection, sending keep-alive
// comments at the configured interval. The broker broadcasts events to all
// connected clients.
func HandleSSE(broker *Broker) forge.SSEHandler {
	return func(ctx forge.Context, stream forge.Stream) error {
		// Set retry interval for client reconnection (3 seconds)
		if err := stream.SetRetry(3000); err != nil {
			return err
		}

		// Register this client
		clientID := broker.AddClient(stream)
		defer broker.RemoveClient(clientID)

		// Send initial connection event
		if err := stream.SendJSON("connected", map[string]string{
			"client_id": clientID,
			"status":    "connected",
		}); err != nil {
			return err
		}

		// Keep-alive loop — sends comments to prevent connection timeout
		ticker := time.NewTicker(broker.KeepAlive())
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Context().Done():
				// Client disconnected
				return nil
			case <-stream.Context().Done():
				// Stream closed
				return nil
			case <-ticker.C:
				// Send keep-alive comment
				if err := stream.SendComment("keepalive"); err != nil {
					return nil // Client gone, exit cleanly
				}
			}
		}
	}
}

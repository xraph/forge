package forge

import (
	"github.com/xraph/forge/v2/internal/router"
)

// Connection represents a WebSocket connection
type Connection = router.Connection

// Stream represents a Server-Sent Events stream
type Stream = router.Stream

// WebSocketHandler handles WebSocket connections
type WebSocketHandler = router.WebSocketHandler

// SSEHandler handles Server-Sent Events
type SSEHandler = router.SSEHandler

// StreamConfig configures streaming behavior
type StreamConfig = router.StreamConfig

// DefaultStreamConfig returns default streaming configuration
func DefaultStreamConfig() StreamConfig {
	return router.DefaultStreamConfig()
}

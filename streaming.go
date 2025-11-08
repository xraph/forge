package forge

import (
	"github.com/xraph/forge/internal/router"
)

// Connection represents a WebSocket connection.
type Connection = router.Connection

// Stream represents a Server-Sent Events stream.
type Stream = router.Stream

// WebSocketHandler handles WebSocket connections.
type WebSocketHandler = router.WebSocketHandler

// SSEHandler handles Server-Sent Events.
type SSEHandler = router.SSEHandler

// WebTransportSession represents a WebTransport session.
type WebTransportSession = router.WebTransportSession

// WebTransportStream represents a WebTransport stream.
type WebTransportStream = router.WebTransportStream

// WebTransportHandler handles WebTransport sessions.
type WebTransportHandler = router.WebTransportHandler

// WebTransportConfig configures WebTransport behavior.
type WebTransportConfig = router.WebTransportConfig

// StreamConfig configures streaming behavior.
type StreamConfig = router.StreamConfig

// DefaultStreamConfig returns default streaming configuration.
func DefaultStreamConfig() StreamConfig {
	return router.DefaultStreamConfig()
}

// DefaultWebTransportConfig returns default WebTransport configuration.
func DefaultWebTransportConfig() WebTransportConfig {
	return router.DefaultWebTransportConfig()
}

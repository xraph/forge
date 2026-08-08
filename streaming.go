package forge

import (
	"github.com/xraph/forge/internal/router"
)

// Connection represents a WebSocket connection.
type Connection = router.Connection

// Stream represents a Server-Sent Events stream.
type Stream = router.Stream

// EventLog stores recent events so a reconnecting SSE client can be handed the
// ones it missed. See WithEventLog.
type EventLog = router.EventLog

// LoggedEvent is one recorded event, as it will be replayed.
type LoggedEvent = router.LoggedEvent

// MemoryEventLogOptions configures NewMemoryEventLog.
type MemoryEventLogOptions = router.MemoryEventLogOptions

// NewMemoryEventLog creates a bounded in-memory event log.
var NewMemoryEventLog = router.NewMemoryEventLog

// WithEventLog makes an SSE route resumable. See router.WithEventLog.
var WithEventLog = router.WithEventLog

// ErrEventIDAssignedByLog is returned by SendWithID and SendJSONWithID on a
// route registered WithEventLog: the log owns event IDs there. Callers that
// supply their own should fall back to Send/SendJSON rather than drop the event.
var ErrEventIDAssignedByLog = router.ErrEventIDAssignedByLog

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

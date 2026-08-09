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

// MemoryEventLog is the bounded in-memory log NewMemoryEventLog returns.
// Aliased so applications can name it in a struct field or signature — the
// constructor alone leaves the type unwriteable outside this package.
type MemoryEventLog = router.MemoryEventLog

// NewMemoryEventLog creates a bounded in-memory event log.
var NewMemoryEventLog = router.NewMemoryEventLog

// WithEventLog makes an SSE route resumable on a best-effort basis. A reconnect
// with nothing to replay is reported as a gap, because a connection-written log
// cannot tell that apart from nothing having been recorded. See
// router.WithEventLog.
var WithEventLog = router.WithEventLog

// WithProducerEventLog makes an SSE route resumable where the application's own
// producer appends to the log, so an empty replay may be reported as a
// completed resume. See router.WithProducerEventLog.
var WithProducerEventLog = router.WithProducerEventLog

// Reserved control event names and their payloads.
//
// Exported so an application can name what it is told not to emit: an event
// under either name would convince a client that a gap was filled when it was
// not, and a name that cannot be referred to cannot be checked against.
const (
	EventResumed = router.EventResumed
	EventGap     = router.EventGap
)

// ResumedPayload closes a replay: the position resumed from and how many events
// were delivered.
type ResumedPayload = router.ResumedPayload

// GapPayload tells the client the gap could not be filled.
type GapPayload = router.GapPayload

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

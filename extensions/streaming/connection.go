package streaming

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/streaming/internal"
)

// Transport type constants.
const (
	TransportWebSocket = "websocket"
	TransportSSE       = "sse"
)

// connOptions carries the tunables for an enhanced connection.
type connOptions struct {
	queueCapacity int
	flushTimeout  time.Duration
	exitTimeout   time.Duration
	metrics       forge.Metrics
	logger        forge.Logger
}

// ConnOption configures an enhanced connection.
type ConnOption func(*connOptions)

// WithSendQueueCapacity sets how many frames may be queued for the socket
// before the per-message-type overflow policy applies. Zero or negative
// selects DefaultSendQueueCapacity.
func WithSendQueueCapacity(capacity int) ConnOption {
	return func(o *connOptions) {
		o.queueCapacity = capacity
	}
}

// WithSendQueueTimeouts bounds connection teardown: flush is how long Close
// waits for queued frames to reach the socket, exit is how long it then waits
// for the writer goroutine to unwind after the socket has been closed.
func WithSendQueueTimeouts(flush, exit time.Duration) ConnOption {
	return func(o *connOptions) {
		o.flushTimeout = flush
		o.exitTimeout = exit
	}
}

// WithConnectionMetrics gives the connection a metrics handle for send queue
// depth, drops, and overflow disconnects. Without one the same numbers remain
// readable through SendQueueStats.
func WithConnectionMetrics(metrics forge.Metrics) ConnOption {
	return func(o *connOptions) {
		o.metrics = metrics
	}
}

// WithConnectionLogger gives the connection a logger for writer-goroutine
// teardown reasons.
func WithConnectionLogger(logger forge.Logger) ConnOption {
	return func(o *connOptions) {
		o.logger = logger
	}
}

// enhancedConn implements EnhancedConnection.
//
// Outbound traffic does not touch the socket directly. Write and WriteJSON
// serialise the frame and hand it to a bounded per-connection send queue; a
// single dedicated writer goroutine drains that queue to the socket. One slow
// consumer therefore cannot stall delivery to anyone else, and a broadcast
// needs no goroutine per recipient.
type enhancedConn struct {
	forge.Connection

	mu            sync.RWMutex
	userID        string
	sessionID     string
	transport     string // "websocket" or "sse"
	contentType   string // preferred content type (e.g. "application/json")
	metadata      map[string]any
	joinedRooms   map[string]bool
	subscriptions map[string]bool
	lastActivity  time.Time
	closed        bool

	queue     *sendQueue
	logger    forge.Logger
	closeOnce sync.Once
	closeErr  error

	// observer keeps the manager's fan-out indexes in step with the membership
	// sets above. See membershipObserver.
	observer membershipObserver
}

// NewConnection creates a new enhanced connection with default transport "websocket".
func NewConnection(conn forge.Connection, opts ...ConnOption) Connection {
	return NewConnectionWithTransport(conn, TransportWebSocket, opts...)
}

// NewConnectionWithTransport creates a new enhanced connection with a specified transport type.
func NewConnectionWithTransport(conn forge.Connection, transport string, opts ...ConnOption) Connection {
	options := connOptions{
		queueCapacity: DefaultSendQueueCapacity,
		flushTimeout:  DefaultSendQueueFlushTimeout,
		exitTimeout:   DefaultSendQueueExitTimeout,
	}

	for _, opt := range opts {
		opt(&options)
	}

	c := &enhancedConn{
		Connection:    conn,
		transport:     transport,
		metadata:      make(map[string]any),
		joinedRooms:   make(map[string]bool),
		subscriptions: make(map[string]bool),
		lastActivity:  time.Now(),
		closed:        false,
		logger:        options.logger,
	}

	c.queue = newSendQueue(
		options.queueCapacity,
		options.flushTimeout,
		options.exitTimeout,
		options.metrics,
		c.writeFrame,
		func() { _ = c.closeUnderlying() },
		c.onWriterExit,
	)
	c.queue.start()

	return c
}

// Write queues a pre-encoded frame for the socket.
//
// The frame carries no message type, so it falls under the disconnect overflow
// policy: a full queue tears the connection down rather than losing the frame.
// The queue retains data until it is written, so callers must not reuse the
// slice.
func (c *enhancedConn) Write(data []byte) error {
	return c.queue.enqueue(sendFrame{payload: data})
}

// WriteJSON encodes v and queues it for the socket.
//
// Encoding happens here, on the caller's goroutine, rather than in the writer.
// That keeps encoding errors synchronous and means the writer never reads a
// *Message the broadcaster may still be mutating.
func (c *enhancedConn) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return c.queue.enqueue(sendFrame{
		msgType: messageTypeOf(v),
		payload: data,
		kind:    frameJSON,
	})
}

// WriteBinary queues a binary frame for the socket.
//
// Defined here rather than inherited from the embedded connection: without it
// the promoted method would put binary frames straight on the socket and bypass
// this queue entirely. Like Write, a binary frame carries no message type and so
// takes the disconnect policy - use WriteFrame to keep the type.
func (c *enhancedConn) WriteBinary(data []byte) error {
	return c.queue.enqueue(sendFrame{payload: data, kind: frameBinary})
}

// OutboundFrame is a pre-encoded frame plus the two things its bytes no longer
// carry: the streaming message type it was encoded from, and whether the wire
// needs a binary frame rather than a text one.
type OutboundFrame struct {
	// Data is the encoded payload. The queue retains it until it is written, so
	// callers must not reuse the slice.
	Data []byte

	// Type is the streaming message type, one of the MessageType* constants. It
	// selects the overflow policy; empty means unknown, which is treated
	// conservatively.
	Type string

	// Binary requests a binary frame. A text frame promises valid UTF-8, and a
	// browser fails the connection with close code 1007 when that is broken.
	Binary bool
}

// WriteFrame queues a pre-encoded frame that still knows what it was encoded
// from.
//
// Write and WriteBinary take only bytes, so a codec-encoded frame arrives at the
// queue with no message type and has to be treated as undroppable. That is safe
// but blunt: a connection that negotiated a non-JSON content type sends every
// frame down that path, which switched the per-type overflow policy off
// wholesale for that client. This is the path that keeps it on.
func (c *enhancedConn) WriteFrame(f OutboundFrame) error {
	kind := frameRaw
	if f.Binary {
		kind = frameBinary
	}

	return c.queue.enqueue(sendFrame{
		msgType: f.Type,
		payload: f.Data,
		kind:    kind,
	})
}

// Close stops the send queue and tears down the underlying connection. It is
// idempotent and safe to call concurrently with Write and WriteJSON.
func (c *enhancedConn) Close() error {
	c.queue.close()
	c.MarkClosed()

	return c.closeUnderlying()
}

// Flush blocks until everything queued before the call has reached the socket,
// ctx is done, or the connection closes.
//
// Delivery is asynchronous, so Write and WriteJSON returning nil means "queued",
// not "delivered". Callers that genuinely need delivery confirmation - a
// shutdown path sending a final notice, or a test asserting on what reached the
// wire - use this rather than reintroducing a synchronous write.
func (c *enhancedConn) Flush(ctx context.Context) error {
	return c.queue.flush(ctx)
}

// SendQueueStats returns a snapshot of this connection's outbound queue.
func (c *enhancedConn) SendQueueStats() SendQueueStats {
	return c.queue.stats()
}

// writeFrame performs the actual socket write. Called only from the writer
// goroutine, so the socket has exactly one writer.
//
// A JSON frame is replayed through WriteJSON as a json.RawMessage so the
// transport's own JSON path still runs and the bytes on the wire are unchanged.
func (c *enhancedConn) writeFrame(f sendFrame) error {
	switch f.kind {
	case frameJSON:
		return c.Connection.WriteJSON(json.RawMessage(f.payload))

	case frameBinary:
		return c.Connection.WriteBinary(f.payload)

	case frameRaw:
		return c.Connection.Write(f.payload)

	default:
		return c.Connection.Write(f.payload)
	}
}

// onWriterExit runs once, on the writer goroutine, after it has stopped. It is
// the single place that marks the connection closed, so IsClosed reports the
// truth whether the connection was closed by the caller, by a write failure, or
// by a send queue overflow.
func (c *enhancedConn) onWriterExit(cause error) {
	c.MarkClosed()
	_ = c.closeUnderlying()

	if cause != nil && c.logger != nil {
		c.logger.Warn("streaming connection writer stopped",
			forge.F("conn_id", c.Connection.ID()),
			forge.F("reason", cause.Error()),
		)
	}
}

// closeUnderlying closes the wrapped connection exactly once and memoises the
// result, so the writer goroutine and Close cannot double-close it.
func (c *enhancedConn) closeUnderlying() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.Connection.Close()
	})

	return c.closeErr
}

// messageTypeOf extracts the streaming message type from an outbound payload.
// An unrecognised payload reports no type, which selects the conservative
// disconnect overflow policy.
func messageTypeOf(v any) string {
	switch msg := v.(type) {
	case *internal.Message:
		if msg == nil {
			return ""
		}

		return msg.Type
	case internal.Message:
		return msg.Type
	default:
		return ""
	}
}

func (c *enhancedConn) GetUserID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.userID
}

func (c *enhancedConn) SetUserID(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.userID = userID
}

func (c *enhancedConn) GetSessionID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.sessionID
}

func (c *enhancedConn) SetSessionID(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sessionID = sessionID
}

func (c *enhancedConn) GetMetadata(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	val, ok := c.metadata[key]

	return val, ok
}

func (c *enhancedConn) SetMetadata(key string, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metadata[key] = value
}

func (c *enhancedConn) GetJoinedRooms() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Sorted. This list is handed to callers and serialised to clients, and Go
	// randomises map iteration.
	return slices.Sorted(maps.Keys(c.joinedRooms))
}

// membershipObserver is notified whenever a connection's room or channel
// membership changes, so a holder of derived state can keep it in step.
//
// The manager keeps room->connections and channel->connections indexes to make
// broadcast cost proportional to the size of the target rather than to the
// number of sockets on the node. Those indexes are derived from the sets below,
// and AddRoom/RemoveRoom are exported on EnhancedConnection — so without this
// callback any caller reaching for them directly would desync the index, and
// the symptom would be silent: a connection that is "in" a room by its own
// account and invisible to every broadcast to it.
type membershipObserver interface {
	onRoomJoined(connID, roomID string)
	onRoomLeft(connID, roomID string)
	onChannelSubscribed(connID, channelID string)
	onChannelUnsubscribed(connID, channelID string)
}

// setMembershipObserver attaches the observer. Called by the manager at
// registration; nil until then, which is why every dispatch below is guarded.
func (c *enhancedConn) setMembershipObserver(obs membershipObserver) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.observer = obs
}

// observerLocked reads the observer under the lock and returns it for dispatch
// after the lock is dropped. Notifying while holding c.mu would invert the lock
// order — the manager takes its own lock inside these callbacks, and it calls
// into the connection while holding it elsewhere.
func (c *enhancedConn) observerLocked() membershipObserver {
	return c.observer
}

func (c *enhancedConn) AddRoom(roomID string) {
	c.mu.Lock()
	c.joinedRooms[roomID] = true
	obs := c.observerLocked()
	c.mu.Unlock()

	if obs != nil {
		obs.onRoomJoined(c.ID(), roomID)
	}
}

func (c *enhancedConn) RemoveRoom(roomID string) {
	c.mu.Lock()
	delete(c.joinedRooms, roomID)
	obs := c.observerLocked()
	c.mu.Unlock()

	if obs != nil {
		obs.onRoomLeft(c.ID(), roomID)
	}
}

func (c *enhancedConn) IsInRoom(roomID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.joinedRooms[roomID]
}

func (c *enhancedConn) GetSubscriptions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Sorted, for the same reason GetJoinedRooms is.
	return slices.Sorted(maps.Keys(c.subscriptions))
}

func (c *enhancedConn) AddSubscription(channelID string) {
	c.mu.Lock()
	c.subscriptions[channelID] = true
	obs := c.observerLocked()
	c.mu.Unlock()

	if obs != nil {
		obs.onChannelSubscribed(c.ID(), channelID)
	}
}

func (c *enhancedConn) RemoveSubscription(channelID string) {
	c.mu.Lock()
	delete(c.subscriptions, channelID)
	obs := c.observerLocked()
	c.mu.Unlock()

	if obs != nil {
		obs.onChannelUnsubscribed(c.ID(), channelID)
	}
}

func (c *enhancedConn) IsSubscribed(channelID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.subscriptions[channelID]
}

func (c *enhancedConn) GetLastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.lastActivity
}

func (c *enhancedConn) UpdateActivity() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lastActivity = time.Now()
}

func (c *enhancedConn) IsClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.closed
}

func (c *enhancedConn) MarkClosed() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
}

// GetTransport returns the connection transport type ("websocket" or "sse").
func (c *enhancedConn) GetTransport() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.transport
}

// SetTransport sets the connection transport type.
func (c *enhancedConn) SetTransport(transport string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.transport = transport
}

// GetContentType returns the connection's preferred content type for message encoding.
func (c *enhancedConn) GetContentType() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.contentType
}

// SetContentType sets the connection's preferred content type.
func (c *enhancedConn) SetContentType(contentType string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.contentType = contentType
}

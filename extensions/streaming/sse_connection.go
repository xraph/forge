package streaming

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"

	"github.com/google/uuid"
	"github.com/xraph/forge"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// sseConnection wraps a forge.Stream (SSE) to implement forge.Connection.
// This allows SSE clients to be registered with the streaming manager
// and receive broadcasts alongside WebSocket clients.
type sseConnection struct {
	id         string
	stream     forge.Stream
	remoteAddr string
	localAddr  string

	// mu guards cursor. Writes to one SSE connection can arrive from several
	// broadcast goroutines at once, and the cursor is read-modify-written on
	// each — an unguarded map here is both a data race and a lost update.
	mu sync.Mutex

	// ownsIDs latches once the stream has refused a caller-supplied event id,
	// meaning the router's event log is driving resumption on this route.
	ownsIDs bool

	// cursor is the high-water sequence this connection has emitted per room.
	// Encoded into every sequenced event's `id:` field, which is what the client
	// echoes back as Last-Event-ID to resume.
	cursor replayCursor
}

// NewSSEConnection creates a new SSE connection adapter.
func NewSSEConnection(stream forge.Stream, remoteAddr, localAddr string) forge.Connection {
	return &sseConnection{
		id:         uuid.New().String(),
		stream:     stream,
		remoteAddr: remoteAddr,
		localAddr:  localAddr,
		cursor:     make(replayCursor),
	}
}

func (c *sseConnection) ID() string {
	return c.id
}

func (c *sseConnection) Read() ([]byte, error) {
	return nil, errors.New("read not supported on SSE connection")
}

func (c *sseConnection) ReadJSON(v any) error {
	return errors.New("read not supported on SSE connection")
}

func (c *sseConnection) Write(data []byte) error {
	return c.stream.Send("message", data)
}

// WriteBinary sends binary data over SSE as base64.
//
// SSE is a text protocol: its grammar is line-oriented and a raw byte stream
// would both corrupt the framing and, if it contained a newline, allow event
// injection. Base64 is the only honest way to carry bytes here, so the encoding
// is applied rather than the call being refused — a caller broadcasting a binary
// frame to a mixed room of WebSocket and SSE clients should not have to special-case
// the transport. The `binary` event name tells the client to decode.
func (c *sseConnection) WriteBinary(data []byte) error {
	encoded := make([]byte, base64.StdEncoding.EncodedLen(len(data)))
	base64.StdEncoding.Encode(encoded, data)

	return c.stream.Send("binary", encoded)
}

// WriteJSON sends a message, tagging it with a resume cursor when it carries a
// room sequence.
//
// The id is the full cursor across every room this connection has delivered,
// not just the sequence of the message being written. EventSource hands back
// only the most recent id it saw, so anything the id omits is a room the client
// cannot resume — it would silently restart from the beginning of that room, or
// never receive its backlog at all.
func (c *sseConnection) WriteJSON(v any) error {
	msg, ok := v.(*streaming.Message)
	if !ok || msg.RoomID == "" || msg.Sequence <= 0 {
		// Unsequenced: presence, typing, system frames. These are not part of
		// any room's ordered history, and inventing an id for them would move
		// the client's resume point to a place it was never actually at.
		return c.stream.SendJSON("message", v)
	}

	id := c.advanceCursor(msg.RoomID, msg.Sequence)

	// Fall back cleanly on a transport with no id support, so an SSE stream
	// predating event ids still delivers — just without resumability.
	sender, ok := c.stream.(interface {
		SendJSONWithID(id, event string, v any) error
	})
	if !ok || c.streamOwnsIDs() {
		return c.stream.SendJSON("message", v)
	}

	err := sender.SendJSONWithID(id, "message", v)
	if err == nil {
		return nil
	}

	// The route was registered with forge.WithEventLog, so the router's event
	// log assigns positions and refuses a caller-supplied id. Both mechanisms
	// solve reconnect, and only one can own the id field — but the message
	// itself must still arrive. Dropping it would turn "two replay features
	// enabled at once" into total message loss on that route.
	//
	// Latched so the refusal costs one failed call per connection rather than
	// one per message; the answer cannot change for the life of the stream.
	if errors.Is(err, forge.ErrEventIDAssignedByLog) {
		c.markStreamOwnsIDs()

		return c.stream.SendJSON("message", v)
	}

	return err
}

// streamOwnsIDs reports whether this stream has refused a caller-supplied event
// id, meaning the router's event log is driving resumption instead.
func (c *sseConnection) streamOwnsIDs() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.ownsIDs
}

func (c *sseConnection) markStreamOwnsIDs() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.ownsIDs = true
}

// advanceCursor records a room sequence and returns the encoded cursor.
//
// Monotonic per room: a lower sequence arriving late (ordinary in distributed
// mode, where two nodes relay independently) must not rewind the mark, or the
// client would be told to resume from before messages it already has and
// receive them twice.
func (c *sseConnection) advanceCursor(roomID string, seq int64) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if seq > c.cursor[roomID] {
		c.cursor[roomID] = seq
	}

	return encodeReplayCursor(c.cursor)
}

// Cursor returns a copy of this connection's current resume position.
func (c *sseConnection) Cursor() replayCursor {
	c.mu.Lock()
	defer c.mu.Unlock()

	cp := make(replayCursor, len(c.cursor))
	for room, seq := range c.cursor {
		cp[room] = seq
	}

	return cp
}

func (c *sseConnection) Close() error {
	return c.stream.Close()
}

func (c *sseConnection) Context() context.Context {
	return c.stream.Context()
}

func (c *sseConnection) RemoteAddr() string {
	return c.remoteAddr
}

func (c *sseConnection) LocalAddr() string {
	return c.localAddr
}

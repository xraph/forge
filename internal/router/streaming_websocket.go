package router

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
	"github.com/google/uuid"
)

// wsConnection implements Connection using gobwas/ws.
type wsConnection struct {
	id     string
	conn   net.Conn
	ctx    context.Context //nolint:containedctx // context needed for WebSocket connection lifecycle and cancellation
	cancel context.CancelFunc

	// readMu serializes Read: WebSocket framing is stateful, so concurrent
	// readers would interleave partial frames. Held across the network read, so
	// it must not be the same lock that Close and Write use.
	readMu sync.Mutex

	mu           sync.Mutex
	closed       bool
	readLimit    int64
	writeTimeout time.Duration
	remoteAddr   string
	localAddr    string
}

// limitedConn caps how many bytes may be read from a connection for one
// message, without disturbing writes.
type limitedConn struct {
	net.Conn

	remaining int64
}

func newLimitedConn(c net.Conn, limit int64) net.Conn {
	return &limitedConn{Conn: c, remaining: limit}
}

func (l *limitedConn) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		return 0, ErrMessageTooLarge
	}

	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}

	n, err := l.Conn.Read(p)
	l.remaining -= int64(n)

	return n, err
}

// newWSConnection creates a new WebSocket connection.
func newWSConnection(id string, conn net.Conn, ctx context.Context) *wsConnection {
	connCtx, cancel := context.WithCancel(ctx)

	var remoteAddr, localAddr string
	if conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}

	if conn.LocalAddr() != nil {
		localAddr = conn.LocalAddr().String()
	}

	return &wsConnection{
		id:           id,
		conn:         conn,
		ctx:          connCtx,
		cancel:       cancel,
		readLimit:    DefaultMaxWebSocketMessageSize,
		writeTimeout: DefaultWebSocketWriteTimeout,
		remoteAddr:   remoteAddr,
		localAddr:    localAddr,
	}
}

// ID returns the connection ID.
func (c *wsConnection) ID() string {
	return c.id
}

// DefaultMaxWebSocketMessageSize caps a single inbound WebSocket message.
// Frame length is client-controlled, so an unbounded read lets one peer request
// an arbitrarily large allocation.
const DefaultMaxWebSocketMessageSize int64 = 1 << 20 // 1 MiB

// ErrMessageTooLarge is returned when an inbound message exceeds the read limit.
var ErrMessageTooLarge = errors.New("websocket message exceeds read limit")

// SetReadLimit overrides the maximum inbound message size for this connection.
// A value <= 0 restores the default.
func (c *wsConnection) SetReadLimit(limit int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if limit <= 0 {
		limit = DefaultMaxWebSocketMessageSize
	}

	c.readLimit = limit
}

// DefaultWebSocketWriteTimeout bounds a single outbound write.
//
// Writes hold the connection mutex, so an unbounded write lets one slow or
// hostile peer that simply stops reading pin the writing goroutine — and the
// mutex — forever. Blocked writers accumulate one per stalled connection, which
// is a cheap way to exhaust the server.
const DefaultWebSocketWriteTimeout = 10 * time.Second

// SetWriteTimeout overrides how long a single write may block before it fails.
// A value <= 0 restores the default.
func (c *wsConnection) SetWriteTimeout(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if d <= 0 {
		d = DefaultWebSocketWriteTimeout
	}

	c.writeTimeout = d
}

// Read reads a message from the WebSocket.
//
// Reads are serialized: the underlying framing is stateful, so two concurrent
// readers would interleave partial frames.
func (c *wsConnection) Read() ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()

	c.mu.Lock()
	closed := c.closed
	limit := c.readLimit
	c.mu.Unlock()

	if closed {
		return nil, errors.New("connection closed")
	}

	if limit <= 0 {
		limit = DefaultMaxWebSocketMessageSize
	}

	// Bound the read rather than trusting the advertised frame length.
	data, _, err := wsutil.ReadClientData(newLimitedConn(c.conn, limit))
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ReadJSON reads JSON from the WebSocket.
func (c *wsConnection) ReadJSON(v any) error {
	data, err := c.Read()
	if err != nil {
		return err
	}

	return json.Unmarshal(data, v)
}

// write sends one message with the given opcode, bounded by the write deadline.
//
// The deadline is set inside the lock and cleared before releasing it, so it
// only ever covers this write; leaving it armed would make the next write fail
// against a stale deadline.
func (c *wsConnection) write(op ws.OpCode, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errors.New("connection closed")
	}

	timeout := c.writeTimeout
	if timeout <= 0 {
		timeout = DefaultWebSocketWriteTimeout
	}

	// Deadline errors are ignored rather than failing the write: not every
	// net.Conn honours deadlines, and refusing to write to one that does not is
	// worse than writing without the bound.
	_ = c.conn.SetWriteDeadline(time.Now().Add(timeout))

	defer func() { _ = c.conn.SetWriteDeadline(time.Time{}) }()

	return wsutil.WriteServerMessage(c.conn, op, data)
}

// Write sends a message to the WebSocket as a text frame.
func (c *wsConnection) Write(data []byte) error {
	return c.write(ws.OpText, data)
}

// WriteBinary sends a message to the WebSocket as a binary frame.
//
// Text frames must carry valid UTF-8; a browser fails the connection with close
// code 1007 when they do not. Arbitrary bytes therefore have to go out as
// binary, so binary codecs must call this rather than Write.
func (c *wsConnection) WriteBinary(data []byte) error {
	return c.write(ws.OpBinary, data)
}

// WriteJSON sends JSON to the WebSocket.
func (c *wsConnection) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	return c.Write(data)
}

// Close closes the WebSocket connection.
func (c *wsConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	c.cancel()

	return c.conn.Close()
}

// Context returns the connection context.
func (c *wsConnection) Context() context.Context {
	return c.ctx
}

// RemoteAddr returns the remote address.
func (c *wsConnection) RemoteAddr() string {
	return c.remoteAddr
}

// LocalAddr returns the local address.
func (c *wsConnection) LocalAddr() string {
	return c.localAddr
}

// errOriginNotAllowed is returned when an upgrade request's Origin header is
// rejected. It is deliberately vague to the client; the detail goes to the log.
var errOriginNotAllowed = errors.New("origin not allowed")

// upgradeToWebSocket upgrades an HTTP connection to WebSocket after validating
// the request Origin. See origin.go for why this check is mandatory rather than
// opt-in.
func upgradeToWebSocket(w http.ResponseWriter, r *http.Request, allowedOrigins []string) (net.Conn, error) {
	if !requestOriginAllowed(r, allowedOrigins) {
		return nil, errOriginNotAllowed
	}

	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// generateConnectionID generates a unique connection ID.
//
// Uses a UUID rather than a timestamp: time.Now().UnixNano() collides for
// upgrades landing in the same clock tick, and two live connections sharing an
// ID means cross-talk wherever connections are tracked by ID. It is also
// trivially guessable.
func generateConnectionID() string {
	return "ws_" + uuid.NewString()
}

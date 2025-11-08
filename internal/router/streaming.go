package router

import (
	"context"
)

// Connection represents a WebSocket connection.
type Connection interface {
	// ID returns unique connection ID
	ID() string

	// Read reads a message from the connection
	Read() ([]byte, error)

	// ReadJSON reads JSON from the connection
	ReadJSON(v any) error

	// Write sends a message to the connection
	Write(data []byte) error

	// WriteJSON sends JSON to the connection
	WriteJSON(v any) error

	// Close closes the connection
	Close() error

	// Context returns the connection context
	Context() context.Context

	// RemoteAddr returns the remote address
	RemoteAddr() string

	// LocalAddr returns the local address
	LocalAddr() string
}

// Stream represents a Server-Sent Events stream.
type Stream interface {
	// Send sends an event to the stream
	Send(event string, data []byte) error

	// SendJSON sends JSON event to the stream
	SendJSON(event string, v any) error

	// Flush flushes any buffered data
	Flush() error

	// Close closes the stream
	Close() error

	// Context returns the stream context
	Context() context.Context

	// SetRetry sets the retry timeout for SSE
	SetRetry(milliseconds int) error

	// SendComment sends a comment (keeps connection alive)
	SendComment(comment string) error
}

// WebSocketHandler handles WebSocket connections.
type WebSocketHandler func(ctx Context, conn Connection) error

// SSEHandler handles Server-Sent Events.
type SSEHandler func(ctx Context, stream Stream) error

// WebTransportHandler handles WebTransport sessions (re-export from webtransport.go)
// type WebTransportHandler func(ctx Context, session WebTransportSession) error

// StreamConfig configures streaming behavior.
type StreamConfig struct {
	// WebSocket configuration
	ReadBufferSize    int
	WriteBufferSize   int
	EnableCompression bool

	// SSE configuration
	RetryInterval int // milliseconds
	KeepAlive     bool

	// WebTransport configuration
	EnableWebTransport      bool
	MaxBidiStreams          int64
	MaxUniStreams           int64
	MaxDatagramFrameSize    int64
	EnableDatagrams         bool
	StreamReceiveWindow     uint64
	ConnectionReceiveWindow uint64
	WebTransportKeepAlive   int // milliseconds
	WebTransportMaxIdle     int // milliseconds
}

// DefaultStreamConfig returns default streaming configuration.
func DefaultStreamConfig() StreamConfig {
	return StreamConfig{
		ReadBufferSize:          4096,
		WriteBufferSize:         4096,
		EnableCompression:       false,
		RetryInterval:           3000, // 3 seconds
		KeepAlive:               true,
		EnableWebTransport:      false,
		MaxBidiStreams:          100,
		MaxUniStreams:           100,
		MaxDatagramFrameSize:    65536, // 64KB
		EnableDatagrams:         true,
		StreamReceiveWindow:     6 * 1024 * 1024,  // 6MB
		ConnectionReceiveWindow: 15 * 1024 * 1024, // 15MB
		WebTransportKeepAlive:   30000,            // 30 seconds
		WebTransportMaxIdle:     60000,            // 60 seconds
	}
}

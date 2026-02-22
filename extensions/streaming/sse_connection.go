package streaming

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/xraph/forge"
)

// sseConnection wraps a forge.Stream (SSE) to implement forge.Connection.
// This allows SSE clients to be registered with the streaming manager
// and receive broadcasts alongside WebSocket clients.
type sseConnection struct {
	id         string
	stream     forge.Stream
	remoteAddr string
	localAddr  string
}

// NewSSEConnection creates a new SSE connection adapter.
func NewSSEConnection(stream forge.Stream, remoteAddr, localAddr string) forge.Connection {
	return &sseConnection{
		id:         uuid.New().String(),
		stream:     stream,
		remoteAddr: remoteAddr,
		localAddr:  localAddr,
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

func (c *sseConnection) WriteJSON(v any) error {
	return c.stream.SendJSON("message", v)
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

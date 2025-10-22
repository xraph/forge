package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// wsConnection implements Connection using gobwas/ws
type wsConnection struct {
	id         string
	conn       net.Conn
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	closed     bool
	remoteAddr string
	localAddr  string
}

// newWSConnection creates a new WebSocket connection
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
		id:         id,
		conn:       conn,
		ctx:        connCtx,
		cancel:     cancel,
		remoteAddr: remoteAddr,
		localAddr:  localAddr,
	}
}

// ID returns the connection ID
func (c *wsConnection) ID() string {
	return c.id
}

// Read reads a message from the WebSocket
func (c *wsConnection) Read() ([]byte, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("connection closed")
	}
	c.mu.Unlock()

	data, _, err := wsutil.ReadClientData(c.conn)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// ReadJSON reads JSON from the WebSocket
func (c *wsConnection) ReadJSON(v any) error {
	data, err := c.Read()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Write sends a message to the WebSocket
func (c *wsConnection) Write(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return fmt.Errorf("connection closed")
	}

	return wsutil.WriteServerMessage(c.conn, ws.OpText, data)
}

// WriteJSON sends JSON to the WebSocket
func (c *wsConnection) WriteJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.Write(data)
}

// Close closes the WebSocket connection
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

// Context returns the connection context
func (c *wsConnection) Context() context.Context {
	return c.ctx
}

// RemoteAddr returns the remote address
func (c *wsConnection) RemoteAddr() string {
	return c.remoteAddr
}

// LocalAddr returns the local address
func (c *wsConnection) LocalAddr() string {
	return c.localAddr
}

// upgradeToWebSocket upgrades an HTTP connection to WebSocket
func upgradeToWebSocket(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	conn, _, _, err := ws.UpgradeHTTP(r, w)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// generateConnectionID generates a unique connection ID
func generateConnectionID() string {
	return fmt.Sprintf("ws_%d", time.Now().UnixNano())
}

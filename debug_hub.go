//go:build forge_debug

package forge

import (
	"net"
	"sync"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// debugHub manages active WebSocket client connections and broadcasts messages.
type debugHub struct {
	mu      sync.Mutex
	clients map[net.Conn]struct{}
}

func newDebugHub() *debugHub {
	return &debugHub{clients: make(map[net.Conn]struct{})}
}

func (h *debugHub) add(conn net.Conn) {
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
}

func (h *debugHub) remove(conn net.Conn) {
	h.mu.Lock()
	delete(h.clients, conn)
	h.mu.Unlock()
	conn.Close()
}

func (h *debugHub) broadcast(data []byte) {
	h.mu.Lock()
	conns := make([]net.Conn, 0, len(h.clients))
	for c := range h.clients {
		conns = append(conns, c)
	}
	h.mu.Unlock()

	for _, c := range conns {
		if err := wsutil.WriteServerMessage(c, ws.OpText, data); err != nil {
			h.remove(c)
		}
	}
}

func (h *debugHub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

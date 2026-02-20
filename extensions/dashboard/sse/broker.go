package sse

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// EventType identifies the kind of SSE event sent to clients.
type EventType string

const (
	EventHealthUpdate  EventType = "health-update"
	EventMetricsUpdate EventType = "metrics-update"
	EventWidgetRefresh EventType = "widget-refresh"
	EventNotification  EventType = "notification"
	EventNavBadge      EventType = "nav-badge-update"
)

// Event is a single SSE event to broadcast to all connected clients.
type Event struct {
	Type EventType `json:"type"`
	Data any       `json:"data"`
}

// client represents a connected SSE client with its send channel.
type client struct {
	id     string
	stream forge.Stream
	done   chan struct{}
}

// Broker manages SSE client connections and broadcasts events.
type Broker struct {
	mu        sync.RWMutex
	clients   map[string]*client
	keepAlive time.Duration
	logger    forge.Logger
	nextID    uint64
	closed    bool
}

// NewBroker creates a new SSE event broker.
func NewBroker(keepAlive time.Duration, logger forge.Logger) *Broker {
	return &Broker{
		clients:   make(map[string]*client),
		keepAlive: keepAlive,
		logger:    logger,
	}
}

// AddClient registers a new SSE stream as a client.
// Returns the client ID for later removal.
func (b *Broker) AddClient(stream forge.Stream) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := formatClientID(b.nextID)

	c := &client{
		id:     id,
		stream: stream,
		done:   make(chan struct{}),
	}
	b.clients[id] = c

	b.logger.Debug("SSE client connected",
		forge.F("client_id", id),
		forge.F("total_clients", len(b.clients)),
	)

	return id
}

// RemoveClient removes a client by ID.
func (b *Broker) RemoveClient(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if c, ok := b.clients[id]; ok {
		close(c.done)
		delete(b.clients, id)

		b.logger.Debug("SSE client disconnected",
			forge.F("client_id", id),
			forge.F("total_clients", len(b.clients)),
		)
	}
}

// Broadcast sends an event to all connected clients.
// Failed sends are logged and the client is removed.
func (b *Broker) Broadcast(event Event) {
	data, err := json.Marshal(event.Data)
	if err != nil {
		b.logger.Warn("failed to marshal SSE event data",
			forge.F("event_type", string(event.Type)),
			forge.F("error", err.Error()),
		)

		return
	}

	b.mu.RLock()

	clients := make([]*client, 0, len(b.clients))
	for _, c := range b.clients {
		clients = append(clients, c)
	}

	b.mu.RUnlock()

	var failedIDs []string

	for _, c := range clients {
		if err := c.stream.Send(string(event.Type), data); err != nil {
			failedIDs = append(failedIDs, c.id)
		}
	}

	// Remove failed clients
	for _, id := range failedIDs {
		b.RemoveClient(id)
	}
}

// BroadcastJSON sends a JSON event to all connected clients.
func (b *Broker) BroadcastJSON(eventType EventType, data any) {
	b.Broadcast(Event{Type: eventType, Data: data})
}

// ClientCount returns the number of connected clients.
func (b *Broker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.clients)
}

// Close shuts down the broker and disconnects all clients.
func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.closed = true

	for id, c := range b.clients {
		close(c.done)
		delete(b.clients, id)
	}

	b.logger.Info("SSE broker closed")
}

// KeepAlive returns the configured keep-alive interval.
func (b *Broker) KeepAlive() time.Duration {
	return b.keepAlive
}

// formatClientID returns a string client ID from a numeric counter.
func formatClientID(id uint64) string {
	return "sse-" + formatUint(id)
}

func formatUint(n uint64) string {
	if n == 0 {
		return "0"
	}

	buf := make([]byte, 0, 20)
	for n > 0 {
		buf = append(buf, byte('0'+n%10))
		n /= 10
	}
	// reverse
	for i, j := 0, len(buf)-1; i < j; i, j = i+1, j-1 {
		buf[i], buf[j] = buf[j], buf[i]
	}

	return string(buf)
}

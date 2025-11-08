package streaming

import (
	"sync"
	"time"

	"github.com/xraph/forge"
)

// enhancedConn implements EnhancedConnection.
type enhancedConn struct {
	forge.Connection

	mu            sync.RWMutex
	userID        string
	sessionID     string
	metadata      map[string]any
	joinedRooms   map[string]bool
	subscriptions map[string]bool
	lastActivity  time.Time
	closed        bool
}

// NewConnection creates a new enhanced connection.
func NewConnection(conn forge.Connection) Connection {
	return &enhancedConn{
		Connection:    conn,
		metadata:      make(map[string]any),
		joinedRooms:   make(map[string]bool),
		subscriptions: make(map[string]bool),
		lastActivity:  time.Now(),
		closed:        false,
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

	rooms := make([]string, 0, len(c.joinedRooms))
	for room := range c.joinedRooms {
		rooms = append(rooms, room)
	}

	return rooms
}

func (c *enhancedConn) AddRoom(roomID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.joinedRooms[roomID] = true
}

func (c *enhancedConn) RemoveRoom(roomID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.joinedRooms, roomID)
}

func (c *enhancedConn) IsInRoom(roomID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.joinedRooms[roomID]
}

func (c *enhancedConn) GetSubscriptions() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	subs := make([]string, 0, len(c.subscriptions))
	for sub := range c.subscriptions {
		subs = append(subs, sub)
	}

	return subs
}

func (c *enhancedConn) AddSubscription(channelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.subscriptions[channelID] = true
}

func (c *enhancedConn) RemoveSubscription(channelID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.subscriptions, channelID)
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

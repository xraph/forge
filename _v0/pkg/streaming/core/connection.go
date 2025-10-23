package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
)

// ConnectionState represents the state of a connection
type ConnectionState string

const (
	ConnectionStateConnecting ConnectionState = "connecting"
	ConnectionStateConnected  ConnectionState = "connected"
	ConnectionStateClosing    ConnectionState = "closing"
	ConnectionStateClosed     ConnectionState = "closed"
	ConnectionStateError      ConnectionState = "error"
)

// Connection represents a streaming connection
type Connection interface {
	// Identity
	ID() string
	UserID() string
	RoomID() string
	SessionID() string

	// Connection management
	Send(ctx context.Context, message *Message) error
	SendEvent(ctx context.Context, message Event) error
	Close(ctx context.Context) error
	IsAlive() bool
	ConnectedAt() time.Time
	LastActivity() time.Time
	GetState() ConnectionState

	SetTimeout(timeout time.Duration) error
	SetUserID(userID string) error
	SetRoomID(roomID string) error
	SetMaxMessageSize(size int) error

	// Context and metadata
	Context() context.Context
	SetContext(ctx context.Context)
	Metadata() map[string]interface{}
	SetMetadata(key string, value interface{})

	// Protocol information
	Protocol() ProtocolType
	RemoteAddr() string
	UserAgent() string

	// Events
	OnMessage(handler MessageHandler)
	OnClose(handler CloseHandler)
	OnError(handler ErrorHandler)

	// Statistics
	GetStats() ConnectionStats
}

// ProtocolType represents the connection protocol
type ProtocolType string

const (
	ProtocolWebSocket   ProtocolType = "websocket"
	ProtocolSSE         ProtocolType = "sse"
	ProtocolLongPolling ProtocolType = "longpolling"
)

// MessageHandler handles incoming messages
type MessageHandler func(conn Connection, message *Message) error

// CloseHandler handles connection close events
type CloseHandler func(conn Connection, reason string)

// ErrorHandler handles connection errors
type ErrorHandler func(conn Connection, err error)

// ConnectionStats represents connection statistics
type ConnectionStats struct {
	ConnectedAt      time.Time     `json:"connected_at"`
	LastActivity     time.Time     `json:"last_activity"`
	MessagesSent     int64         `json:"messages_sent"`
	MessagesReceived int64         `json:"messages_received"`
	BytesSent        int64         `json:"bytes_sent"`
	BytesReceived    int64         `json:"bytes_received"`
	Errors           int64         `json:"errors"`
	Latency          time.Duration `json:"latency"`
}

// ConnectionConfig contains configuration for connections
type ConnectionConfig struct {
	WriteTimeout      time.Duration `yaml:"write_timeout" default:"30s"`
	ReadTimeout       time.Duration `yaml:"read_timeout" default:"60s"`
	PingInterval      time.Duration `yaml:"ping_interval" default:"30s"`
	PongTimeout       time.Duration `yaml:"pong_timeout" default:"60s"`
	MaxMessageSize    int64         `yaml:"max_message_size" default:"32768"`
	BufferSize        int           `yaml:"buffer_size" default:"1024"`
	EnablePing        bool          `yaml:"enable_ping" default:"true"`
	UserID            string        `yaml:"user_id"`
	RoomID            string        `yaml:"room_id"`
	ConnectionTimeout time.Duration `yaml:"connectionTimeout"`
}

// DefaultConnectionConfig returns default connection configuration
func DefaultConnectionConfig() ConnectionConfig {
	return ConnectionConfig{
		WriteTimeout:   30 * time.Second,
		ReadTimeout:    60 * time.Second,
		PingInterval:   30 * time.Second,
		PongTimeout:    60 * time.Second,
		MaxMessageSize: 32768, // 32KB
		BufferSize:     1024,
		EnablePing:     true,
	}
}

// ConnectionManager manages multiple connections
type ConnectionManager interface {
	AddConnection(conn Connection) error
	RemoveConnection(connectionID string) error
	GetConnection(connectionID string) (Connection, error)
	GetConnectionsByUser(userID string) []Connection
	GetConnectionsByRoom(roomID string) []Connection
	GetAllConnections() []Connection
	BroadcastToRoom(ctx context.Context, roomID string, message *Message) error
	BroadcastToUser(ctx context.Context, userID string, message *Message) error
	BroadcastToAll(ctx context.Context, message *Message) error
	CloseConnection(ctx context.Context, connectionID string, reason string) error
	CloseUserConnections(ctx context.Context, userID string, reason string) error
	GetStats() ConnectionManagerStats
}

// ConnectionManagerStats represents connection manager statistics
type ConnectionManagerStats struct {
	TotalConnections      int                  `json:"total_connections"`
	ConnectionsByProtocol map[ProtocolType]int `json:"connections_by_protocol"`
	ConnectionsByRoom     map[string]int       `json:"connections_by_room"`
	ConnectionsByUser     map[string]int       `json:"connections_by_user"`
	ActiveConnections     int                  `json:"active_connections"`
	TotalBytesTransferred int64                `json:"total_bytes_transferred"`
	TotalMessages         int64                `json:"total_messages"`
	ErrorRate             float64              `json:"error_rate"`
	AverageLatency        time.Duration        `json:"average_latency"`
}

// DefaultConnectionManager implements ConnectionManager
type DefaultConnectionManager struct {
	connections       map[string]Connection
	connectionsByUser map[string][]string
	connectionsByRoom map[string][]string
	mu                sync.RWMutex
	logger            common.Logger
	metrics           common.Metrics
}

// NewDefaultConnectionManager creates a new default connection manager
func NewDefaultConnectionManager(logger common.Logger, metrics common.Metrics) ConnectionManager {
	return &DefaultConnectionManager{
		connections:       make(map[string]Connection),
		connectionsByUser: make(map[string][]string),
		connectionsByRoom: make(map[string][]string),
		logger:            logger,
		metrics:           metrics,
	}
}

// AddConnection adds a connection to the manager
func (cm *DefaultConnectionManager) AddConnection(conn Connection) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	connectionID := conn.ID()
	userID := conn.UserID()
	roomID := conn.RoomID()

	// Check if connection already exists
	if _, exists := cm.connections[connectionID]; exists {
		return common.ErrServiceAlreadyExists(connectionID)
	}

	// Add to main connections map
	cm.connections[connectionID] = conn

	// Add to user connections
	cm.connectionsByUser[userID] = append(cm.connectionsByUser[userID], connectionID)

	// Add to room connections
	if roomID != "" {
		cm.connectionsByRoom[roomID] = append(cm.connectionsByRoom[roomID], connectionID)
	}

	if cm.logger != nil {
		cm.logger.Info("connection added",
			logger.String("connection_id", connectionID),
			logger.String("user_id", userID),
			logger.String("room_id", roomID),
			logger.String("protocol", string(conn.Protocol())),
		)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("streaming.connections.added").Inc()
		cm.metrics.Gauge("streaming.connections.active").Set(float64(len(cm.connections)))
	}

	return nil
}

// RemoveConnection removes a connection from the manager
func (cm *DefaultConnectionManager) RemoveConnection(connectionID string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conn, exists := cm.connections[connectionID]
	if !exists {
		return common.ErrServiceNotFound(connectionID)
	}

	userID := conn.UserID()
	roomID := conn.RoomID()

	// Remove from main connections map
	delete(cm.connections, connectionID)

	// Remove from user connections
	if userConnections, exists := cm.connectionsByUser[userID]; exists {
		for i, id := range userConnections {
			if id == connectionID {
				cm.connectionsByUser[userID] = append(userConnections[:i], userConnections[i+1:]...)
				break
			}
		}
		if len(cm.connectionsByUser[userID]) == 0 {
			delete(cm.connectionsByUser, userID)
		}
	}

	// Remove from room connections
	if roomID != "" {
		if roomConnections, exists := cm.connectionsByRoom[roomID]; exists {
			for i, id := range roomConnections {
				if id == connectionID {
					cm.connectionsByRoom[roomID] = append(roomConnections[:i], roomConnections[i+1:]...)
					break
				}
			}
			if len(cm.connectionsByRoom[roomID]) == 0 {
				delete(cm.connectionsByRoom, roomID)
			}
		}
	}

	if cm.logger != nil {
		cm.logger.Info("connection removed",
			logger.String("connection_id", connectionID),
			logger.String("user_id", userID),
			logger.String("room_id", roomID),
		)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("streaming.connections.removed").Inc()
		cm.metrics.Gauge("streaming.connections.active").Set(float64(len(cm.connections)))
	}

	return nil
}

// GetConnection gets a connection by ID
func (cm *DefaultConnectionManager) GetConnection(connectionID string) (Connection, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conn, exists := cm.connections[connectionID]
	if !exists {
		return nil, common.ErrServiceNotFound(connectionID)
	}

	return conn, nil
}

// GetConnectionsByUser gets all connections for a user
func (cm *DefaultConnectionManager) GetConnectionsByUser(userID string) []Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	connectionIDs, exists := cm.connectionsByUser[userID]
	if !exists {
		return []Connection{}
	}

	connections := make([]Connection, 0, len(connectionIDs))
	for _, id := range connectionIDs {
		if conn, exists := cm.connections[id]; exists {
			connections = append(connections, conn)
		}
	}

	return connections
}

// GetConnectionsByRoom gets all connections for a room
func (cm *DefaultConnectionManager) GetConnectionsByRoom(roomID string) []Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	connectionIDs, exists := cm.connectionsByRoom[roomID]
	if !exists {
		return []Connection{}
	}

	connections := make([]Connection, 0, len(connectionIDs))
	for _, id := range connectionIDs {
		if conn, exists := cm.connections[id]; exists {
			connections = append(connections, conn)
		}
	}

	return connections
}

// GetAllConnections gets all connections
func (cm *DefaultConnectionManager) GetAllConnections() []Connection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	connections := make([]Connection, 0, len(cm.connections))
	for _, conn := range cm.connections {
		connections = append(connections, conn)
	}

	return connections
}

// BroadcastToRoom broadcasts a message to all connections in a room
func (cm *DefaultConnectionManager) BroadcastToRoom(ctx context.Context, roomID string, message *Message) error {
	connections := cm.GetConnectionsByRoom(roomID)

	var errors []error
	for _, conn := range connections {
		if err := conn.Send(ctx, message); err != nil {
			errors = append(errors, fmt.Errorf("failed to send to connection %s: %w", conn.ID(), err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("broadcast errors: %v", errors)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("streaming.messages.broadcast_room").Inc()
		cm.metrics.Histogram("streaming.broadcast.size").Observe(float64(len(connections)))
	}

	return nil
}

// BroadcastToUser broadcasts a message to all connections for a user
func (cm *DefaultConnectionManager) BroadcastToUser(ctx context.Context, userID string, message *Message) error {
	connections := cm.GetConnectionsByUser(userID)

	var errors []error
	for _, conn := range connections {
		if err := conn.Send(ctx, message); err != nil {
			errors = append(errors, fmt.Errorf("failed to send to connection %s: %w", conn.ID(), err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("broadcast errors: %v", errors)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("streaming.messages.broadcast_user").Inc()
		cm.metrics.Histogram("streaming.broadcast.size").Observe(float64(len(connections)))
	}

	return nil
}

// BroadcastToAll broadcasts a message to all connections
func (cm *DefaultConnectionManager) BroadcastToAll(ctx context.Context, message *Message) error {
	connections := cm.GetAllConnections()

	var errors []error
	for _, conn := range connections {
		if err := conn.Send(ctx, message); err != nil {
			errors = append(errors, fmt.Errorf("failed to send to connection %s: %w", conn.ID(), err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("broadcast errors: %v", errors)
	}

	if cm.metrics != nil {
		cm.metrics.Counter("streaming.messages.broadcast_all").Inc()
		cm.metrics.Histogram("streaming.broadcast.size").Observe(float64(len(connections)))
	}

	return nil
}

// CloseConnection closes a specific connection
func (cm *DefaultConnectionManager) CloseConnection(ctx context.Context, connectionID string, reason string) error {
	conn, err := cm.GetConnection(connectionID)
	if err != nil {
		return err
	}

	return conn.Close(ctx)
}

// CloseUserConnections closes all connections for a user
func (cm *DefaultConnectionManager) CloseUserConnections(ctx context.Context, userID string, reason string) error {
	connections := cm.GetConnectionsByUser(userID)

	var errors []error
	for _, conn := range connections {
		if err := conn.Close(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to close connection %s: %w", conn.ID(), err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("close errors: %v", errors)
	}

	return nil
}

// GetStats returns connection manager statistics
func (cm *DefaultConnectionManager) GetStats() ConnectionManagerStats {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	stats := ConnectionManagerStats{
		TotalConnections:      len(cm.connections),
		ConnectionsByProtocol: make(map[ProtocolType]int),
		ConnectionsByRoom:     make(map[string]int),
		ConnectionsByUser:     make(map[string]int),
	}

	var totalBytes, totalMessages, totalErrors int64
	var totalLatency time.Duration
	activeConnections := 0

	for _, conn := range cm.connections {
		if conn.IsAlive() {
			activeConnections++
		}

		// Count by protocol
		stats.ConnectionsByProtocol[conn.Protocol()]++

		// Get connection stats
		connStats := conn.GetStats()
		totalBytes += connStats.BytesSent + connStats.BytesReceived
		totalMessages += connStats.MessagesSent + connStats.MessagesReceived
		totalErrors += connStats.Errors
		totalLatency += connStats.Latency
	}

	for room, connections := range cm.connectionsByRoom {
		stats.ConnectionsByRoom[room] = len(connections)
	}

	for user, connections := range cm.connectionsByUser {
		stats.ConnectionsByUser[user] = len(connections)
	}

	stats.ActiveConnections = activeConnections
	stats.TotalBytesTransferred = totalBytes
	stats.TotalMessages = totalMessages

	if totalMessages > 0 {
		stats.ErrorRate = float64(totalErrors) / float64(totalMessages)
		stats.AverageLatency = totalLatency / time.Duration(len(cm.connections))
	}

	return stats
}

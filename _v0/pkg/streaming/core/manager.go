package core

import (
	"context"
	"net/http"
	"time"

	"github.com/xraph/forge/v0/pkg/common"
)

// StreamingManager is the main interface for managing streaming processes, including rooms, connections, and message delivery.
type StreamingManager interface {
	common.Service // Implements the service interface for DI integration

	// Room management
	CreateRoom(ctx context.Context, roomID string, config RoomConfig) (Room, error)
	GetRoom(roomID string) (Room, error)
	DeleteRoom(ctx context.Context, roomID string) error
	GetRooms() []Room
	GetRoomCount() int

	// Protocol-specific connection handlers
	HandleWebSocket(w http.ResponseWriter, r *http.Request, config ConnectionConfig) (Connection, error)
	HandleSSE(w http.ResponseWriter, r *http.Request, config ConnectionConfig) (Connection, error)
	HandleLongPolling(w http.ResponseWriter, r *http.Request, config ConnectionConfig) (Connection, error)

	// Connection management
	HandleConnection(w http.ResponseWriter, r *http.Request) error
	GetConnection(connectionID string) (Connection, error)
	GetConnections() []Connection
	GetConnectionsByUser(userID string) []Connection
	GetConnectionsByRoom(roomID string) []Connection
	DisconnectUser(ctx context.Context, userID string, reason string) error
	DisconnectConnection(ctx context.Context, connectionID string, reason string) error

	// Message handling
	BroadcastToRoom(ctx context.Context, roomID string, message *Message) error
	BroadcastToUser(ctx context.Context, userID string, message *Message) error
	BroadcastToAll(ctx context.Context, message *Message) error
	SendToConnection(ctx context.Context, connectionID string, message *Message) error

	// Protocol management
	RegisterProtocolHandler(handler ProtocolHandler) error
	UnregisterProtocolHandler(name string) error
	GetProtocolHandlers() []ProtocolHandler

	// Event handling
	OnConnectionEstablished(handler ConnectionEstablishedHandler)
	OnConnectionClosed(handler ConnectionClosedHandler)
	OnMessage(handler GlobalMessageHandler)
	OnError(handler GlobalErrorHandler)

	// Statistics and monitoring
	GetStats() StreamingStats
	GetRoomStats(roomID string) (RoomStats, error)
	GetConnectionStats(connectionID string) (ConnectionStats, error)

	// Configuration
	GetConfig() StreamingConfig
	UpdateConfig(config StreamingConfig) error
}

// ConnectionEstablishedHandler is a function type invoked when a new connection is successfully established.
type ConnectionEstablishedHandler func(conn Connection)

// ConnectionClosedHandler is a function type invoked when a connection is closed, providing the connection and the reason.
type ConnectionClosedHandler func(conn Connection, reason string)

// GlobalMessageHandler defines a function to handle global messages, providing the room, message, and sender connection.
type GlobalMessageHandler func(room Room, message *Message, sender Connection)

// GlobalErrorHandler is a function type used to handle errors at a global level along with related contextual information.
type GlobalErrorHandler func(err error, context map[string]interface{})

// StreamingConfig contains configuration settings for managing streaming services, including connections, messaging, and scaling.
type StreamingConfig struct {
	// Connection settings
	MaxConnections    int           `yaml:"max_connections" default:"10000"`
	ConnectionTimeout time.Duration `yaml:"connection_timeout" default:"60s"`
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval" default:"30s"`

	// Room settings
	MaxRooms          int        `yaml:"max_rooms" default:"1000"`
	DefaultRoomConfig RoomConfig `yaml:"default_room_config"`

	// Message settings
	MaxMessageSize   int64 `yaml:"max_message_size" default:"32768"`
	MessageQueueSize int   `yaml:"message_queue_size" default:"1000"`

	// Protocol settings
	EnableWebSocket   bool `yaml:"enable_websocket" default:"true"`
	EnableSSE         bool `yaml:"enable_sse" default:"true"`
	EnableLongPolling bool `yaml:"enable_long_polling" default:"true"`

	// Security settings
	RequireAuth      bool `yaml:"require_auth" default:"false"`
	AllowCrossOrigin bool `yaml:"allow_cross_origin" default:"true"`

	// Scaling settings
	EnableRedisScaling bool         `yaml:"enable_redis_scaling" default:"false"`
	RedisConfig        *RedisConfig `yaml:"redis_config,omitempty"`

	// Persistence settings
	EnablePersistence bool               `yaml:"enable_persistence" default:"false"`
	PersistenceConfig *PersistenceConfig `yaml:"persistence_config,omitempty"`
}

// RedisConfig represents the configuration settings for connecting to a Redis instance.
type RedisConfig struct {
	Address  string `yaml:"address" default:"localhost:6379"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db" default:"0"`
	Prefix   string `yaml:"prefix" default:"forge:streaming"`
}

// PersistenceConfig defines configuration options for persistence, including database name and table prefix.
type PersistenceConfig struct {
	DatabaseName string `yaml:"database_name" default:"forge_streaming"`
	TablePrefix  string `yaml:"table_prefix" default:"streaming_"`
}

// StreamingStats represents statistics and metrics for streaming management.
type StreamingStats struct {
	TotalConnections      int                  `json:"total_connections"`
	ActiveConnections     int                  `json:"active_connections"`
	TotalRooms            int                  `json:"total_rooms"`
	ActiveRooms           int                  `json:"active_rooms"`
	TotalMessages         int64                `json:"total_messages"`
	MessagesPerSecond     float64              `json:"messages_per_second"`
	ConnectionsByProtocol map[ProtocolType]int `json:"connections_by_protocol"`
	ConnectionsByRoom     map[string]int       `json:"connections_by_room"`
	BytesTransferred      int64                `json:"bytes_transferred"`
	ErrorCount            int64                `json:"error_count"`
	Uptime                time.Duration        `json:"uptime"`
	LastMessage           time.Time            `json:"last_message"`
}

// ProtocolHandler defines the interface for handling streaming protocols, providing methods for managing protocol-specific behavior.
type ProtocolHandler interface {
	Name() string
	CanHandle(r *http.Request) bool
	HandleUpgrade(w http.ResponseWriter, r *http.Request) (Connection, error)
	Priority() int
}

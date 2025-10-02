package transport

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"time"
)

// Transport defines the interface for consensus communication
type Transport interface {
	// Start starts the transport
	Start(ctx context.Context) error

	// Stop stops the transport
	Stop(ctx context.Context) error

	// Send sends a message to a peer
	Send(ctx context.Context, target string, message Message) error

	// Receive receives messages from peers
	Receive(ctx context.Context) (<-chan IncomingMessage, error)

	// AddPeer adds a peer to the transport
	AddPeer(peerID, address string) error

	// RemovePeer removes a peer from the transport
	RemovePeer(peerID string) error

	// GetPeers returns all peers
	GetPeers() []PeerInfo

	// LocalAddress returns the local address
	LocalAddress() string

	// HealthCheck performs a health check
	HealthCheck(ctx context.Context) error

	// GetAddress retrieves the address of the specified node using its unique identifier. Returns the address or an error.
	GetAddress(nodeID string) (string, error)

	// GetStats returns transport statistics
	GetStats() TransportStats

	// Close closes the transport
	Close() error
}

// Message represents a message sent between nodes
type Message struct {
	Type      MessageType            `json:"type"`
	From      string                 `json:"from"`
	To        string                 `json:"to"`
	Data      []byte                 `json:"data"`
	Metadata  map[string]interface{} `json:"metadata"`
	Timestamp time.Time              `json:"timestamp"`
	ID        string                 `json:"id"`
}

// MessageType represents the type of message
type MessageType string

const (
	MessageTypeVoteRequest     MessageType = "vote_request"
	MessageTypeVoteResponse    MessageType = "vote_response"
	MessageTypeAppendEntries   MessageType = "append_entries"
	MessageTypeAppendResponse  MessageType = "append_response"
	MessageTypeInstallSnapshot MessageType = "install_snapshot"
	MessageTypeHeartbeat       MessageType = "heartbeat"
	MessageTypeClientRequest   MessageType = "client_request"
	MessageTypeClientResponse  MessageType = "client_response"
)

// IncomingMessage represents a message received from a peer
type IncomingMessage struct {
	Message Message
	Peer    PeerInfo
	Error   error
}

// PeerInfo represents information about a peer
type PeerInfo struct {
	ID       string                 `json:"id"`
	Address  string                 `json:"address"`
	Status   PeerStatus             `json:"status"`
	LastSeen time.Time              `json:"last_seen"`
	Metadata map[string]interface{} `json:"metadata"`
}

// PeerStatus represents the status of a peer
type PeerStatus string

const (
	PeerStatusConnected    PeerStatus = "connected"
	PeerStatusDisconnected PeerStatus = "disconnected"
	PeerStatusConnecting   PeerStatus = "connecting"
	PeerStatusError        PeerStatus = "error"
)

// TransportStats contains transport statistics
type TransportStats struct {
	MessagesSent     int64         `json:"messages_sent"`
	MessagesReceived int64         `json:"messages_received"`
	BytesSent        int64         `json:"bytes_sent"`
	BytesReceived    int64         `json:"bytes_received"`
	ErrorCount       int64         `json:"error_count"`
	ConnectedPeers   int           `json:"connected_peers"`
	AverageLatency   time.Duration `json:"average_latency"`
	Uptime           time.Duration `json:"uptime"`
}

// TransportConfig contains configuration for transport
type TransportConfig struct {
	Type           string                 `json:"type"`
	Address        string                 `json:"address"`
	Port           int                    `json:"port"`
	Timeout        time.Duration          `json:"timeout"`
	MaxMessageSize int                    `json:"max_message_size"`
	BufferSize     int                    `json:"buffer_size"`
	EnableTLS      bool                   `json:"enable_tls"`
	TLSConfig      *TLSConfig             `json:"tls_config"`
	Compression    bool                   `json:"compression"`
	KeepAlive      bool                   `json:"keep_alive"`
	KeepAliveTime  time.Duration          `json:"keep_alive_time"`
	MaxConnections int                    `json:"max_connections"`
	Options        map[string]interface{} `json:"options"`
}

// TLSConfig contains TLS configuration
type TLSConfig struct {
	CertFile   string `json:"cert_file"`
	KeyFile    string `json:"key_file"`
	CAFile     string `json:"ca_file"`
	ServerName string `json:"server_name"`
	SkipVerify bool   `json:"skip_verify"`
}

// TransportFactory creates transport instances
type TransportFactory interface {
	// Create creates a new transport
	Create(config TransportConfig) (Transport, error)

	// Name returns the factory name
	Name() string

	// Version returns the factory version
	Version() string

	// ValidateConfig validates the configuration
	ValidateConfig(config TransportConfig) error
}

// TransportManager manages multiple transports
type TransportManager interface {
	// RegisterFactory registers a transport factory
	RegisterFactory(factory TransportFactory) error

	// CreateTransport creates a transport
	CreateTransport(config TransportConfig) (Transport, error)

	// GetTransport returns a transport by name
	GetTransport(name string) (Transport, error)

	// GetFactories returns all registered factories
	GetFactories() map[string]TransportFactory

	// Close closes all transports
	Close(ctx context.Context) error
}

// Connection represents a connection to a peer
type Connection interface {
	// Send sends a message
	Send(ctx context.Context, message Message) error

	// Receive receives a message
	Receive(ctx context.Context) (Message, error)

	// Close closes the connection
	Close() error

	// RemoteAddr returns the remote address
	RemoteAddr() net.Addr

	// LocalAddr returns the local address
	LocalAddr() net.Addr

	// IsConnected returns true if connected
	IsConnected() bool

	// LastActivity returns the last activity time
	LastActivity() time.Time
}

// ConnectionPool manages connections to peers
type ConnectionPool interface {
	// Get gets a connection to a peer
	Get(ctx context.Context, peerID string) (Connection, error)

	// Put returns a connection to the pool
	Put(peerID string, conn Connection) error

	// Remove removes a connection from the pool
	Remove(peerID string) error

	// Close closes all connections
	Close() error

	// Size returns the number of connections
	Size() int

	// Stats returns pool statistics
	Stats() ConnectionPoolStats
}

// ConnectionPoolStats contains connection pool statistics
type ConnectionPoolStats struct {
	ActiveConnections int           `json:"active_connections"`
	IdleConnections   int           `json:"idle_connections"`
	TotalConnections  int           `json:"total_connections"`
	HitRate           float64       `json:"hit_rate"`
	MissRate          float64       `json:"miss_rate"`
	AverageLatency    time.Duration `json:"average_latency"`
}

// Serializer handles message serialization
type Serializer interface {
	// Serialize serializes a message
	Serialize(message Message) ([]byte, error)

	// Deserialize deserializes a message
	Deserialize(data []byte) (Message, error)

	// ContentType returns the content type
	ContentType() string
}

// Codec handles message encoding/decoding
type Codec interface {
	// Encode encodes data
	Encode(data interface{}) ([]byte, error)

	// Decode decodes data
	Decode(data []byte, target interface{}) error

	// ContentType returns the content type
	ContentType() string
}

// MessageHandler handles incoming messages
type MessageHandler interface {
	// HandleMessage handles a message
	HandleMessage(ctx context.Context, message IncomingMessage) error

	// MessageTypes returns the message types this handler supports
	MessageTypes() []MessageType
}

// TransportError represents a transport error
type TransportError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	PeerID  string `json:"peer_id,omitempty"`
	Cause   error  `json:"cause,omitempty"`
}

func (e *TransportError) WithCause(err error) *TransportError {
	e.Cause = err
	return e
}

func (e *TransportError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

func (e *TransportError) Unwrap() error {
	return e.Cause
}

// Common error codes
const (
	ErrCodeConnectionFailed     = "connection_failed"
	ErrCodePeerNotFound         = "peer_not_found"
	ErrCodeMessageTooLarge      = "message_too_large"
	ErrCodeSerializationError   = "serialization_error"
	ErrCodeTimeout              = "timeout"
	ErrCodeAuthenticationFailed = "authentication_failed"
	ErrCodeInvalidMessage       = "invalid_message"
	ErrCodeNetworkError         = "network_error"
)

// NewTransportError creates a new transport error
func NewTransportError(code, message string) *TransportError {
	return &TransportError{
		Code:    code,
		Message: message,
	}
}

// Common transport types
const (
	TransportTypeHTTP = "http"
	TransportTypeGRPC = "grpc"
	TransportTypeTCP  = "tcp"
	TransportTypeUDP  = "udp"
)

// MessageBuilder helps build messages
type MessageBuilder struct {
	message Message
}

// NewMessageBuilder creates a new message builder
func NewMessageBuilder() *MessageBuilder {
	return &MessageBuilder{
		message: Message{
			Metadata:  make(map[string]interface{}),
			Timestamp: time.Now(),
		},
	}
}

// WithType sets the message type
func (b *MessageBuilder) WithType(msgType MessageType) *MessageBuilder {
	b.message.Type = msgType
	return b
}

// WithFrom sets the from field
func (b *MessageBuilder) WithFrom(from string) *MessageBuilder {
	b.message.From = from
	return b
}

// WithTo sets the to field
func (b *MessageBuilder) WithTo(to string) *MessageBuilder {
	b.message.To = to
	return b
}

// WithData sets the data
func (b *MessageBuilder) WithData(data []byte) *MessageBuilder {
	b.message.Data = data
	return b
}

// WithMetadata adds metadata
func (b *MessageBuilder) WithMetadata(key string, value interface{}) *MessageBuilder {
	b.message.Metadata[key] = value
	return b
}

// WithID sets the message ID
func (b *MessageBuilder) WithID(id string) *MessageBuilder {
	b.message.ID = id
	return b
}

// Build builds the message
func (b *MessageBuilder) Build() Message {
	return b.message
}

// Utility functions

// ValidateMessage validates a message
func ValidateMessage(message Message) error {
	if message.Type == "" {
		return NewTransportError(ErrCodeInvalidMessage, "message type is required")
	}
	if message.From == "" {
		return NewTransportError(ErrCodeInvalidMessage, "from field is required")
	}
	if message.To == "" {
		return NewTransportError(ErrCodeInvalidMessage, "to field is required")
	}
	if message.Data == nil {
		return NewTransportError(ErrCodeInvalidMessage, "data field is required")
	}
	return nil
}

// GenerateMessageID generates a unique message ID
func GenerateMessageID() string {
	return fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), rand.Int63())
}

// IsValidMessageType checks if a message type is valid
func IsValidMessageType(msgType MessageType) bool {
	switch msgType {
	case MessageTypeVoteRequest, MessageTypeVoteResponse,
		MessageTypeAppendEntries, MessageTypeAppendResponse,
		MessageTypeInstallSnapshot, MessageTypeHeartbeat,
		MessageTypeClientRequest, MessageTypeClientResponse:
		return true
	default:
		return false
	}
}

// CloneMessage creates a deep copy of a message
func CloneMessage(original Message) Message {
	clone := Message{
		Type:      original.Type,
		From:      original.From,
		To:        original.To,
		Data:      make([]byte, len(original.Data)),
		Metadata:  make(map[string]interface{}),
		Timestamp: original.Timestamp,
		ID:        original.ID,
	}

	copy(clone.Data, original.Data)
	for k, v := range original.Metadata {
		clone.Metadata[k] = v
	}

	return clone
}

// MessageSize calculates the size of a message in bytes
func MessageSize(message Message) int {
	size := len(message.Type) + len(message.From) + len(message.To) + len(message.Data) + len(message.ID)

	// Approximate size of metadata
	for k, v := range message.Metadata {
		size += len(k)
		if s, ok := v.(string); ok {
			size += len(s)
		} else {
			size += 8 // Approximate size for other types
		}
	}

	return size
}

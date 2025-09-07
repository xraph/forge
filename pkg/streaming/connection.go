package streaming

import (
	"github.com/xraph/forge/pkg/common"
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

// ConnectionState represents the state of a connection
type ConnectionState = streamingcore.ConnectionState

const (
	ConnectionStateConnecting = streamingcore.ConnectionStateConnecting
	ConnectionStateConnected  = streamingcore.ConnectionStateConnected
	ConnectionStateClosing    = streamingcore.ConnectionStateClosing
	ConnectionStateClosed     = streamingcore.ConnectionStateClosed
	ConnectionStateError      = streamingcore.ConnectionStateError
)

// Connection represents a streaming connection
type Connection = streamingcore.Connection

// ProtocolType represents the connection protocol
type ProtocolType = streamingcore.ProtocolType

const (
	ProtocolWebSocket   = streamingcore.ProtocolWebSocket
	ProtocolSSE         = streamingcore.ProtocolSSE
	ProtocolLongPolling = streamingcore.ProtocolLongPolling
)

// MessageHandler handles incoming messages
type MessageHandler func(conn Connection, message *Message) error

// CloseHandler handles connection close events
type CloseHandler func(conn Connection, reason string)

// ErrorHandler handles connection errors
type ErrorHandler func(conn Connection, err error)

// ConnectionStats represents connection statistics
type ConnectionStats = streamingcore.ConnectionStats

// ConnectionConfig contains configuration for connections
type ConnectionConfig = streamingcore.ConnectionConfig

// DefaultConnectionConfig returns default connection configuration
func DefaultConnectionConfig() ConnectionConfig {
	return streamingcore.DefaultConnectionConfig()
}

// ConnectionManager manages multiple connections
type ConnectionManager = streamingcore.ConnectionManager

// ConnectionManagerStats represents connection manager statistics
type ConnectionManagerStats = streamingcore.ConnectionManagerStats

// DefaultConnectionManager implements ConnectionManager
type DefaultConnectionManager = streamingcore.DefaultConnectionManager

// NewDefaultConnectionManager creates a new default connection manager
func NewDefaultConnectionManager(logger common.Logger, metrics common.Metrics) ConnectionManager {
	return streamingcore.NewDefaultConnectionManager(logger, metrics)
}

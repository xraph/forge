package streaming

import (
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

// StreamingManager manages all streaming operations
type StreamingManager = streamingcore.StreamingManager

// ConnectionEstablishedHandler Event handlers
type ConnectionEstablishedHandler = streamingcore.ConnectionEstablishedHandler
type ConnectionClosedHandler = streamingcore.ConnectionClosedHandler
type GlobalMessageHandler = streamingcore.GlobalMessageHandler
type GlobalErrorHandler = streamingcore.GlobalErrorHandler

// StreamingConfig contains configuration for the streaming manager
type StreamingConfig = streamingcore.StreamingConfig

// RedisConfig contains Redis configuration for scaling
type RedisConfig = streamingcore.RedisConfig

// PersistenceConfig contains persistence configuration
type PersistenceConfig = streamingcore.PersistenceConfig

// StreamingStats represents streaming manager statistics
type StreamingStats = streamingcore.StreamingStats

// ProtocolHandler interface (referenced but not fully defined)
type ProtocolHandler = streamingcore.ProtocolHandler

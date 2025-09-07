package protocols

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	streamingcore "github.com/xraph/forge/pkg/streaming/core"
)

// BaseConnection provides common functionality for all connection types
type BaseConnection struct {
	id                string
	userID            string
	roomID            string
	sessionID         string
	protocol          streamingcore.ProtocolType
	state             streamingcore.ConnectionState
	connectionTimeout time.Duration
	maxMessageSize    int
	metadata          map[string]interface{}
	remoteAddr        string
	userAgent         string

	// Handlers
	messageHandler streamingcore.MessageHandler
	closeHandler   streamingcore.CloseHandler
	errorHandler   streamingcore.ErrorHandler

	// Statistics
	stats streamingcore.ConnectionStats

	// Synchronization
	mu     sync.RWMutex
	sendMu sync.Mutex

	// Context and cancellation
	ctx    context.Context
	cancel context.CancelFunc

	// Configuration
	config streamingcore.ConnectionConfig
	logger common.Logger
}

func (c *BaseConnection) ConnectedAt() time.Time {
	return c.stats.ConnectedAt
}

func (c *BaseConnection) LastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats.LastActivity
}

func (c *BaseConnection) SetTimeout(timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connectionTimeout = timeout
	return nil
}

func (c *BaseConnection) SetUserID(userID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userID = userID
	return nil
}

func (c *BaseConnection) SetRoomID(roomID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.roomID = roomID
	return nil
}

func (c *BaseConnection) SetMaxMessageSize(size int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.maxMessageSize = size
	return nil
}

func (c *BaseConnection) SetContext(ctx context.Context) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	c.ctx = ctx
}

func (c *BaseConnection) Send(ctx context.Context, message *streamingcore.Message) error {
	c.logger.Debug("send must be implemented by the protocol")
	return nil
}

func (c *BaseConnection) SendEvent(ctx context.Context, message streamingcore.Event) error {
	c.logger.Debug("sendEvent must be implemented by the protocol")
	return nil
}

func (c *BaseConnection) Close(ctx context.Context) error {
	c.logger.Debug("close must be implemented by the protocol")
	return nil
}

// NewBaseConnection creates a new base connection
func NewBaseConnection(id, userID, roomID, sessionID string, protocol streamingcore.ProtocolType, config streamingcore.ConnectionConfig, logger common.Logger) streamingcore.Connection {
	ctx, cancel := context.WithCancel(context.Background())

	return &BaseConnection{
		id:        id,
		userID:    userID,
		roomID:    roomID,
		sessionID: sessionID,
		protocol:  protocol,
		state:     streamingcore.ConnectionStateConnecting,
		metadata:  make(map[string]interface{}),
		ctx:       ctx,
		cancel:    cancel,
		config:    config,
		logger:    logger,
		stats: streamingcore.ConnectionStats{
			ConnectedAt: time.Now(),
		},
	}
}

// ID returns the connection ID
func (c *BaseConnection) ID() string {
	return c.id
}

// UserID returns the user ID
func (c *BaseConnection) UserID() string {
	return c.userID
}

// RoomID returns the room ID
func (c *BaseConnection) RoomID() string {
	return c.roomID
}

// SessionID returns the session ID
func (c *BaseConnection) SessionID() string {
	return c.sessionID
}

// Protocol returns the connection protocol
func (c *BaseConnection) Protocol() streamingcore.ProtocolType {
	return c.protocol
}

// RemoteAddr returns the remote address
func (c *BaseConnection) RemoteAddr() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.remoteAddr
}

// UserAgent returns the user agent
func (c *BaseConnection) UserAgent() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userAgent
}

// SetRemoteAddr sets the remote address
func (c *BaseConnection) SetRemoteAddr(addr string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remoteAddr = addr
}

// SetUserAgent sets the user agent
func (c *BaseConnection) SetUserAgent(ua string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.userAgent = ua
}

// GetState returns the current connection state
func (c *BaseConnection) GetState() streamingcore.ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// setState sets the connection state
func (c *BaseConnection) setState(state streamingcore.ConnectionState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	oldState := c.state
	c.state = state

	if c.logger != nil && oldState != state {
		c.logger.Debug("connection state changed",
			logger.String("connection_id", c.id),
			logger.String("user_id", c.userID),
			logger.String("old_state", string(oldState)),
			logger.String("new_state", string(state)),
		)
	}
}

// IsAlive returns true if the connection is alive
func (c *BaseConnection) IsAlive() bool {
	state := c.GetState()
	return state == streamingcore.ConnectionStateConnected || state == streamingcore.ConnectionStateConnecting
}

// Metadata returns the connection metadata
func (c *BaseConnection) Metadata() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metadata := make(map[string]interface{})
	for k, v := range c.metadata {
		metadata[k] = v
	}
	return metadata
}

// SetMetadata sets a metadata key-value pair
func (c *BaseConnection) SetMetadata(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metadata[key] = value
}

// OnMessage sets the message handler
func (c *BaseConnection) OnMessage(handler streamingcore.MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messageHandler = handler
}

// OnClose sets the close handler
func (c *BaseConnection) OnClose(handler streamingcore.CloseHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeHandler = handler
}

// OnError sets the error handler
func (c *BaseConnection) OnError(handler streamingcore.ErrorHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errorHandler = handler
}

// GetStats returns connection statistics
func (c *BaseConnection) GetStats() streamingcore.ConnectionStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.stats
}

// updateStats updates connection statistics
func (c *BaseConnection) updateStats(messagesSent, messagesReceived, bytesSent, bytesReceived, errors int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.MessagesSent += messagesSent
	c.stats.MessagesReceived += messagesReceived
	c.stats.BytesSent += bytesSent
	c.stats.BytesReceived += bytesReceived
	c.stats.Errors += errors
	c.stats.LastActivity = time.Now()
}

// handleMessage handles an incoming message
func (c *BaseConnection) handleMessage(message *streamingcore.Message) error {
	c.updateStats(0, 1, 0, 0, 0)

	c.mu.RLock()
	handler := c.messageHandler
	c.mu.RUnlock()

	if handler != nil {
		return handler(c, message)
	}

	return nil
}

// handleClose handles connection close
func (c *BaseConnection) handleClose(reason string) {
	c.setState(streamingcore.ConnectionStateClosed)

	c.mu.RLock()
	handler := c.closeHandler
	c.mu.RUnlock()

	if handler != nil {
		handler(c, reason)
	}

	// Cancel context
	c.cancel()
}

// handleError handles connection errors
func (c *BaseConnection) handleError(err error) {
	c.updateStats(0, 0, 0, 0, 1)

	if c.logger != nil {
		c.logger.Error("connection error",
			logger.String("connection_id", c.id),
			logger.String("user_id", c.userID),
			logger.Error(err),
		)
	}

	c.mu.RLock()
	handler := c.errorHandler
	c.mu.RUnlock()

	if handler != nil {
		handler(c, err)
	}
}

// Context returns the connection context
func (c *BaseConnection) Context() context.Context {
	return c.ctx
}

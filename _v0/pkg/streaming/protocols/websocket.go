package protocols

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xraph/forge/v0/pkg/common"
	"github.com/xraph/forge/v0/pkg/logger"
	streamingcore "github.com/xraph/forge/v0/pkg/streaming/core"
)

// WebSocketHandler implements the ProtocolHandler interface for WebSocket connections
type WebSocketHandler struct {
	upgrader websocket.Upgrader
	logger   common.Logger
	metrics  common.Metrics
	config   WebSocketConfig
}

// WebSocketConfig contains WebSocket-specific configuration
type WebSocketConfig struct {
	CheckOrigin       bool          `yaml:"check_origin" default:"false"`
	AllowedOrigins    []string      `yaml:"allowed_origins"`
	ReadBufferSize    int           `yaml:"read_buffer_size" default:"1024"`
	WriteBufferSize   int           `yaml:"write_buffer_size" default:"1024"`
	HandshakeTimeout  time.Duration `yaml:"handshake_timeout" default:"10s"`
	EnableCompression bool          `yaml:"enable_compression" default:"true"`
	CompressionLevel  int           `yaml:"compression_level" default:"1"`
	SubProtocols      []string      `yaml:"sub_protocols"`
}

// DefaultWebSocketConfig returns default WebSocket configuration
func DefaultWebSocketConfig() WebSocketConfig {
	return WebSocketConfig{
		CheckOrigin:       false,
		AllowedOrigins:    []string{"*"},
		ReadBufferSize:    1024,
		WriteBufferSize:   1024,
		HandshakeTimeout:  10 * time.Second,
		EnableCompression: true,
		CompressionLevel:  1,
		SubProtocols:      []string{},
	}
}

// NewWebSocketHandler creates a new WebSocket protocol handler
func NewWebSocketHandler(logger common.Logger, metrics common.Metrics) streamingcore.ProtocolHandler {
	config := DefaultWebSocketConfig()

	upgrader := websocket.Upgrader{
		ReadBufferSize:    config.ReadBufferSize,
		WriteBufferSize:   config.WriteBufferSize,
		HandshakeTimeout:  config.HandshakeTimeout,
		EnableCompression: config.EnableCompression,
		CheckOrigin:       createOriginChecker(config),
		Subprotocols:      config.SubProtocols,
	}

	return &WebSocketHandler{
		upgrader: upgrader,
		logger:   logger,
		metrics:  metrics,
		config:   config,
	}
}

// Name returns the protocol handler name
func (h *WebSocketHandler) Name() string {
	return "websocket"
}

// Priority returns the protocol handler priority
func (h *WebSocketHandler) Priority() int {
	return 100 // Highest priority for WebSocket
}

// CanHandle checks if the request can be handled by this protocol
func (h *WebSocketHandler) CanHandle(r *http.Request) bool {
	// Check Connection header - can contain multiple values like "keep-alive, Upgrade"
	connectionHeader := r.Header.Get("Connection")
	if connectionHeader == "" {
		return false
	}

	hasUpgrade := false
	// Split by comma and check each value
	connectionValues := strings.Split(connectionHeader, ",")
	for _, value := range connectionValues {
		if strings.ToLower(strings.TrimSpace(value)) == "upgrade" {
			hasUpgrade = true
			break
		}
	}

	if !hasUpgrade {
		return false
	}

	// Check Upgrade header - should be "websocket"
	upgradeHeader := r.Header.Get("Upgrade")
	if strings.ToLower(strings.TrimSpace(upgradeHeader)) != "websocket" {
		return false
	}

	// Check for WebSocket key - required for WebSocket handshake
	if strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key")) == "" {
		return false
	}

	// Optional: Check WebSocket version (should be 13 for modern clients)
	version := r.Header.Get("Sec-WebSocket-Version")
	if version != "" && strings.TrimSpace(version) != "13" {
		// Some older clients might not send version or use different versions
		// We can be lenient here, but log it
		if h.logger != nil {
			h.logger.Debug("WebSocket version not 13",
				logger.String("version", version),
				logger.String("user_agent", r.UserAgent()),
			)
		}
	}

	return true
}

// HandleUpgrade handles the WebSocket upgrade
func (h *WebSocketHandler) HandleUpgrade(w http.ResponseWriter, r *http.Request) (streamingcore.Connection, error) {
	// Extract connection parameters
	userID := r.URL.Query().Get("user_id")
	roomID := r.URL.Query().Get("room_id")
	sessionID := r.URL.Query().Get("session_id")

	if sessionID == "" {
		sessionID = generateSessionID()
	}

	// Upgrade connection
	wsConn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade WebSocket connection: %w", err)
	}

	// Create connection
	connectionID := generateConnectionID("ws")
	config := streamingcore.DefaultConnectionConfig()

	conn := NewWebSocketConnection(
		connectionID,
		userID,
		roomID,
		sessionID,
		wsConn,
		config,
		h.logger,
		h.metrics,
	)

	// Set remote address and user agent
	if baseConn, ok := conn.(*WebSocketConnection); ok {
		baseConn.SetRemoteAddr(r.RemoteAddr)
		baseConn.SetUserAgent(r.UserAgent())
	}

	if h.logger != nil {
		h.logger.Info("WebSocket connection upgraded",
			logger.String("connection_id", connectionID),
			logger.String("user_id", userID),
			logger.String("room_id", roomID),
			logger.String("remote_addr", r.RemoteAddr),
		)
	}

	if h.metrics != nil {
		h.metrics.Counter("streaming.websocket.connections.upgraded").Inc()
	}

	return conn, nil
}

// createOriginChecker creates an origin checker function
func createOriginChecker(config WebSocketConfig) func(*http.Request) bool {
	if !config.CheckOrigin {
		return func(*http.Request) bool { return true }
	}

	allowedOrigins := make(map[string]bool)
	for _, origin := range config.AllowedOrigins {
		allowedOrigins[origin] = true
	}

	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Allow requests without Origin header
		}

		// Check wildcard
		if allowedOrigins["*"] {
			return true
		}

		// Check exact match
		return allowedOrigins[origin]
	}
}

// WebSocketConnection implements the Connection interface for WebSocket
type WebSocketConnection struct {
	*BaseConnection
	wsConn     *websocket.Conn
	sendCh     chan *streamingcore.Message
	pingTicker *time.Ticker
	pongCh     chan struct{}
	closeOnce  sync.Once
	serializer streamingcore.MessageSerializer
	validator  streamingcore.MessageValidator
}

// NewWebSocketConnection creates a new WebSocket connection
func NewWebSocketConnection(
	id, userID, roomID, sessionID string,
	wsConn *websocket.Conn,
	config streamingcore.ConnectionConfig,
	logger common.Logger,
	metrics common.Metrics,
) streamingcore.Connection {
	base := NewBaseConnection(id, userID, roomID, sessionID, streamingcore.ProtocolWebSocket, config, logger)

	conn := &WebSocketConnection{
		BaseConnection: base.(*BaseConnection),
		wsConn:         wsConn,
		sendCh:         make(chan *streamingcore.Message, config.BufferSize),
		pongCh:         make(chan struct{}, 1),
		serializer:     streamingcore.NewJSONMessageSerializer(),
		validator:      streamingcore.NewDefaultMessageValidator(),
	}

	// Start goroutines
	go conn.readPump()
	go conn.writePump()

	if config.EnablePing {
		conn.startPingPong()
	}

	// Mark as connected
	conn.setState(streamingcore.ConnectionStateConnected)

	return conn
}

// Send sends a message through the WebSocket connection
func (c *WebSocketConnection) Send(ctx context.Context, message *streamingcore.Message) error {
	if !c.IsAlive() {
		return fmt.Errorf("connection is not alive")
	}

	// Validate message
	if err := c.validator.Validate(message); err != nil {
		return fmt.Errorf("message validation failed: %w", err)
	}

	select {
	case c.sendCh <- message:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-c.Context().Done():
		return fmt.Errorf("connection closed")
	default:
		return fmt.Errorf("send buffer full")
	}
}

// SendEvent sends a message through the WebSocket connection
func (c *WebSocketConnection) SendEvent(ctx context.Context, message streamingcore.Event) error {
	return c.Send(ctx, message.ToMessage())
}

// Close closes the WebSocket connection
func (c *WebSocketConnection) Close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		c.setState(streamingcore.ConnectionStateClosing)

		// Stop ping ticker
		if c.pingTicker != nil {
			c.pingTicker.Stop()
		}

		// Close send channel
		close(c.sendCh)

		// Send close message
		c.wsConn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			time.Now().Add(time.Second))

		// Close WebSocket connection
		c.wsConn.Close()

		c.setState(streamingcore.ConnectionStateClosed)
		c.handleClose("normal_closure")
	})

	return nil
}

// readPump handles reading messages from the WebSocket
func (c *WebSocketConnection) readPump() {
	defer func() {
		if r := recover(); r != nil {
			c.handleError(fmt.Errorf("read pump panic: %v", r))
		}
		c.Close(context.Background())
	}()

	// Set read deadline and limits
	c.wsConn.SetReadLimit(c.config.MaxMessageSize)
	c.wsConn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))

	// Set pong handler
	c.wsConn.SetPongHandler(func(string) error {
		c.wsConn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		select {
		case c.pongCh <- struct{}{}:
		default:
		}
		return nil
	})

	for {
		// Read message
		messageType, data, err := c.wsConn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.handleError(fmt.Errorf("unexpected close error: %w", err))
			}
			break
		}

		// Update read deadline
		c.wsConn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))

		// Handle different message types
		switch messageType {
		case websocket.TextMessage:
			if err := c.handleTextMessage(data); err != nil {
				c.handleError(fmt.Errorf("failed to handle text message: %w", err))
			}
		case websocket.BinaryMessage:
			if err := c.handleBinaryMessage(data); err != nil {
				c.handleError(fmt.Errorf("failed to handle binary message: %w", err))
			}
		case websocket.CloseMessage:
			c.handleClose("client_initiated")
			return
		}
	}
}

// writePump handles writing messages to the WebSocket
func (c *WebSocketConnection) writePump() {
	defer func() {
		if r := recover(); r != nil {
			c.handleError(fmt.Errorf("write pump panic: %v", r))
		}
		c.Close(context.Background())
	}()

	for {
		select {
		case message, ok := <-c.sendCh:
			if !ok {
				// Channel closed
				return
			}

			if err := c.writeMessage(message); err != nil {
				c.handleError(fmt.Errorf("failed to write message: %w", err))
				return
			}

		case <-c.Context().Done():
			return
		}
	}
}

// writeMessage writes a message to the WebSocket
func (c *WebSocketConnection) writeMessage(message *streamingcore.Message) error {
	// Set write deadline
	c.wsConn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))

	// Serialize message
	data, err := c.serializer.Serialize(message)
	if err != nil {
		return fmt.Errorf("failed to serialize message: %w", err)
	}

	// Write message
	if err := c.wsConn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to write WebSocket message: %w", err)
	}

	// Update statistics
	c.updateStats(1, 0, int64(len(data)), 0, 0)

	return nil
}

// handleTextMessage handles incoming text messages
func (c *WebSocketConnection) handleTextMessage(data []byte) error {
	// Deserialize message
	message, err := c.serializer.Deserialize(data)
	if err != nil {
		return fmt.Errorf("failed to deserialize message: %w", err)
	}

	// Update statistics
	c.updateStats(0, 1, 0, int64(len(data)), 0)

	// Handle message
	return c.handleMessage(message)
}

// handleBinaryMessage handles incoming binary messages
func (c *WebSocketConnection) handleBinaryMessage(data []byte) error {
	// For now, treat binary messages as text
	return c.handleTextMessage(data)
}

// startPingPong starts the ping-pong mechanism
func (c *WebSocketConnection) startPingPong() {
	c.pingTicker = time.NewTicker(c.config.PingInterval)

	go func() {
		defer c.pingTicker.Stop()

		for {
			select {
			case <-c.pingTicker.C:
				if err := c.ping(); err != nil {
					c.handleError(fmt.Errorf("ping failed: %w", err))
					return
				}

			case <-c.Context().Done():
				return
			}
		}
	}()
}

// ping sends a ping frame and waits for pong
func (c *WebSocketConnection) ping() error {
	// Set write deadline
	c.wsConn.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout))

	// Send ping
	if err := c.wsConn.WriteMessage(websocket.PingMessage, nil); err != nil {
		return fmt.Errorf("failed to send ping: %w", err)
	}

	// Wait for pong with timeout
	select {
	case <-c.pongCh:
		return nil
	case <-time.After(c.config.PongTimeout):
		return fmt.Errorf("pong timeout")
	case <-c.Context().Done():
		return fmt.Errorf("connection closed")
	}
}

// Helper functions

// generateConnectionID generates a unique connection ID
func generateConnectionID(prefix string) string {
	return fmt.Sprintf("%s-%d-%s", prefix, time.Now().UnixNano(), randomString(8))
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	return fmt.Sprintf("sess-%d-%s", time.Now().UnixNano(), randomString(12))
}

// randomString generates a random string of given length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

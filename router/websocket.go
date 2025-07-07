package router

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xraph/forge/logger"
)

// webSocketHub implements the WebSocketHub interface
type webSocketHub struct {
	connections map[string]WebSocketConnection
	groups      map[string]map[string]bool // group -> connection IDs
	connGroups  map[string]map[string]bool // connection ID -> groups
	register    chan WebSocketConnection
	unregister  chan WebSocketConnection
	broadcast   chan []byte
	broadcastTo chan broadcastMessage
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	logger      logger.Logger
	config      WebSocketConfig
	stats       WebSocketStats
	started     bool
}

// broadcastMessage represents a targeted broadcast message
type broadcastMessage struct {
	connIDs []string
	groups  []string
	message []byte
}

// NewWebSocketHub creates a new WebSocket hub
func NewWebSocketHub(config WebSocketConfig, logger logger.Logger) WebSocketHub {
	ctx, cancel := context.WithCancel(context.Background())

	return &webSocketHub{
		connections: make(map[string]WebSocketConnection),
		groups:      make(map[string]map[string]bool),
		connGroups:  make(map[string]map[string]bool),
		register:    make(chan WebSocketConnection, 256),
		unregister:  make(chan WebSocketConnection, 256),
		broadcast:   make(chan []byte, 256),
		broadcastTo: make(chan broadcastMessage, 256),
		ctx:         ctx,
		cancel:      cancel,
		logger:      logger,
		config:      config,
		stats:       WebSocketStats{},
	}
}

// Start starts the WebSocket hub
func (h *webSocketHub) Start(ctx context.Context) error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return fmt.Errorf("hub already started")
	}
	h.started = true
	h.mu.Unlock()

	h.logger.Info("Starting WebSocket hub")

	go h.run()
	return nil
}

// Stop stops the WebSocket hub
func (h *webSocketHub) Stop(ctx context.Context) error {
	h.mu.Lock()
	if !h.started {
		h.mu.Unlock()
		return nil
	}
	h.started = false
	h.mu.Unlock()

	h.logger.Info("Stopping WebSocket hub")

	h.cancel()

	// Close all connections
	h.mu.RLock()
	for _, conn := range h.connections {
		conn.Close()
	}
	h.mu.RUnlock()

	return nil
}

// run is the main hub loop
func (h *webSocketHub) run() {
	defer func() {
		if r := recover(); r != nil {
			h.logger.Error("WebSocket hub panic recovered",
				logger.Any("panic", r),
				logger.Stack("stacktrace"),
			)
		}
	}()

	for {
		select {
		case <-h.ctx.Done():
			return

		case conn := <-h.register:
			h.handleRegister(conn)

		case conn := <-h.unregister:
			h.handleUnregister(conn)

		case message := <-h.broadcast:
			h.handleBroadcast(message)

		case msg := <-h.broadcastTo:
			h.handleBroadcastTo(msg)
		}
	}
}

// handleRegister handles connection registration
func (h *webSocketHub) handleRegister(conn WebSocketConnection) {
	h.mu.Lock()
	h.connections[conn.ID()] = conn
	h.connGroups[conn.ID()] = make(map[string]bool)
	h.stats.TotalConnections++
	h.stats.ActiveConnections++
	h.mu.Unlock()

	h.logger.Debug("WebSocket connection registered",
		logger.String("connection_id", conn.ID()),
		logger.String("remote_addr", conn.RemoteAddr()),
	)
}

// handleUnregister handles connection unregistration
func (h *webSocketHub) handleUnregister(conn WebSocketConnection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	connID := conn.ID()
	if _, exists := h.connections[connID]; !exists {
		return
	}

	// Remove from all groups
	if groups, exists := h.connGroups[connID]; exists {
		for group := range groups {
			if h.groups[group] != nil {
				delete(h.groups[group], connID)
				if len(h.groups[group]) == 0 {
					delete(h.groups, group)
				}
			}
		}
		delete(h.connGroups, connID)
	}

	// Remove connection
	delete(h.connections, connID)
	h.stats.ActiveConnections--

	h.logger.Debug("WebSocket connection unregistered",
		logger.String("connection_id", connID),
	)
}

// handleBroadcast handles broadcasting to all connections
func (h *webSocketHub) handleBroadcast(message []byte) {
	h.mu.RLock()
	connections := make([]WebSocketConnection, 0, len(h.connections))
	for _, conn := range h.connections {
		connections = append(connections, conn)
	}
	h.mu.RUnlock()

	for _, conn := range connections {
		if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
			h.logger.Error("Failed to broadcast message",
				logger.Error(err),
				logger.String("connection_id", conn.ID()),
			)
			h.stats.ErrorCount++
			h.stats.LastError = err
			h.stats.LastErrorTime = time.Now()
		} else {
			h.stats.MessagesSent++
		}
	}
}

// handleBroadcastTo handles targeted broadcasting
func (h *webSocketHub) handleBroadcastTo(msg broadcastMessage) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	targetConnections := make(map[string]WebSocketConnection)

	// Add connections by ID
	for _, connID := range msg.connIDs {
		if conn, exists := h.connections[connID]; exists {
			targetConnections[connID] = conn
		}
	}

	// Add connections by group
	for _, group := range msg.groups {
		if groupConns, exists := h.groups[group]; exists {
			for connID := range groupConns {
				if conn, exists := h.connections[connID]; exists {
					targetConnections[connID] = conn
				}
			}
		}
	}

	// Send to target connections
	for _, conn := range targetConnections {
		if err := conn.WriteMessage(websocket.TextMessage, msg.message); err != nil {
			h.logger.Error("Failed to send targeted message",
				logger.Error(err),
				logger.String("connection_id", conn.ID()),
			)
			h.stats.ErrorCount++
			h.stats.LastError = err
			h.stats.LastErrorTime = time.Now()
		} else {
			h.stats.MessagesSent++
		}
	}
}

// Register registers a WebSocket connection
func (h *webSocketHub) Register(conn WebSocketConnection) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	h.register <- conn
	return nil
}

// Unregister unregisters a WebSocket connection
func (h *webSocketHub) Unregister(conn WebSocketConnection) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	h.unregister <- conn
	return nil
}

// Broadcast broadcasts a message to all connections
func (h *webSocketHub) Broadcast(message []byte) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	select {
	case h.broadcast <- message:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("broadcast timeout")
	}
}

// BroadcastJSON broadcasts a JSON message to all connections
func (h *webSocketHub) BroadcastJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return h.Broadcast(data)
}

// BroadcastTo broadcasts a message to specific connections
func (h *webSocketHub) BroadcastTo(connIDs []string, message []byte) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	msg := broadcastMessage{
		connIDs: connIDs,
		message: message,
	}

	select {
	case h.broadcastTo <- msg:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("broadcast timeout")
	}
}

// BroadcastToGroups broadcasts a message to specific groups
func (h *webSocketHub) BroadcastToGroups(groups []string, message []byte) error {
	if !h.started {
		return fmt.Errorf("hub not started")
	}

	msg := broadcastMessage{
		groups:  groups,
		message: message,
	}

	select {
	case h.broadcastTo <- msg:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("broadcast timeout")
	}
}

// JoinGroup adds a connection to a group
func (h *webSocketHub) JoinGroup(connID, group string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.connections[connID]; !exists {
		return fmt.Errorf("connection not found: %s", connID)
	}

	// Add to group
	if h.groups[group] == nil {
		h.groups[group] = make(map[string]bool)
	}
	h.groups[group][connID] = true

	// Add to connection groups
	h.connGroups[connID][group] = true

	h.logger.Debug("Connection joined group",
		logger.String("connection_id", connID),
		logger.String("group", group),
	)

	return nil
}

// LeaveGroup removes a connection from a group
func (h *webSocketHub) LeaveGroup(connID, group string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.connections[connID]; !exists {
		return fmt.Errorf("connection not found: %s", connID)
	}

	// Remove from group
	if h.groups[group] != nil {
		delete(h.groups[group], connID)
		if len(h.groups[group]) == 0 {
			delete(h.groups, group)
		}
	}

	// Remove from connection groups
	delete(h.connGroups[connID], group)

	h.logger.Debug("Connection left group",
		logger.String("connection_id", connID),
		logger.String("group", group),
	)

	return nil
}

// GetGroups returns the groups a connection belongs to
func (h *webSocketHub) GetGroups(connID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	groups := make([]string, 0)
	if connGroups, exists := h.connGroups[connID]; exists {
		for group := range connGroups {
			groups = append(groups, group)
		}
	}

	return groups
}

// GetGroupMembers returns the members of a group
func (h *webSocketHub) GetGroupMembers(group string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	members := make([]string, 0)
	if groupMembers, exists := h.groups[group]; exists {
		for connID := range groupMembers {
			members = append(members, connID)
		}
	}

	return members
}

// ConnectionCount returns the number of active connections
func (h *webSocketHub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.connections)
}

// GroupCount returns the number of active groups
func (h *webSocketHub) GroupCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.groups)
}

// Stats returns WebSocket statistics
func (h *webSocketHub) Stats() WebSocketStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := h.stats
	stats.ActiveConnections = len(h.connections)
	stats.TotalGroups = len(h.groups)

	return stats
}

// Enhanced WebSocket connection implementation
type webSocketConnection struct {
	conn       *websocket.Conn
	id         string
	ctx        context.Context
	cancel     context.CancelFunc
	logger     logger.Logger
	hub        WebSocketHub
	remoteAddr string
	userAgent  string
	headers    http.Header

	// Message channels
	send    chan []byte
	receive chan []byte

	// Control
	closed bool
	mu     sync.RWMutex

	// Ping/Pong
	pingInterval time.Duration
	pongTimeout  time.Duration
	pingTimer    *time.Timer
	pongTimer    *time.Timer
}

// NewWebSocketConnection creates a new enhanced WebSocket connection
func NewWebSocketConnection(conn *websocket.Conn, req *http.Request, config WebSocketConfig, hub WebSocketHub, logger logger.Logger) WebSocketConnection {
	ctx, cancel := context.WithCancel(req.Context())

	wsConn := &webSocketConnection{
		conn:         conn,
		id:           generateConnectionID(),
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
		hub:          hub,
		remoteAddr:   req.RemoteAddr,
		userAgent:    req.UserAgent(),
		headers:      req.Header,
		send:         make(chan []byte, 256),
		receive:      make(chan []byte, 256),
		pingInterval: config.PingInterval,
		pongTimeout:  config.PongTimeout,
	}

	// Set connection limits
	if config.MaxMessageSize > 0 {
		conn.SetReadLimit(config.MaxMessageSize)
	}

	// Set timeouts
	if config.ReadTimeout > 0 {
		conn.SetReadDeadline(time.Now().Add(config.ReadTimeout))
	}
	if config.WriteTimeout > 0 {
		conn.SetWriteDeadline(time.Now().Add(config.WriteTimeout))
	}

	// Start message handling
	go wsConn.readPump()
	go wsConn.writePump()

	// Start ping/pong if configured
	if wsConn.pingInterval > 0 {
		go wsConn.pingPump()
	}

	return wsConn
}

// Basic connection methods
func (c *webSocketConnection) ID() string                     { return c.id }
func (c *webSocketConnection) RemoteAddr() string             { return c.remoteAddr }
func (c *webSocketConnection) UserAgent() string              { return c.userAgent }
func (c *webSocketConnection) Headers() http.Header           { return c.headers }
func (c *webSocketConnection) Context() context.Context       { return c.ctx }
func (c *webSocketConnection) SetContext(ctx context.Context) { c.ctx = ctx }

// Message handling
func (c *webSocketConnection) ReadMessage() (messageType int, data []byte, err error) {
	return c.conn.ReadMessage()
}

func (c *webSocketConnection) WriteMessage(messageType int, data []byte) error {
	c.mu.RLock()
	if c.closed {
		c.mu.RUnlock()
		return fmt.Errorf("connection closed")
	}
	c.mu.RUnlock()

	select {
	case c.send <- data:
		return nil
	case <-time.After(time.Second):
		return fmt.Errorf("write timeout")
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
}

func (c *webSocketConnection) ReadJSON(v interface{}) error {
	_, data, err := c.ReadMessage()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func (c *webSocketConnection) WriteJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return c.WriteMessage(websocket.TextMessage, data)
}

// Control methods
func (c *webSocketConnection) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()

	c.cancel()

	// Clean up timers
	if c.pingTimer != nil {
		c.pingTimer.Stop()
	}
	if c.pongTimer != nil {
		c.pongTimer.Stop()
	}

	// Unregister from hub
	if c.hub != nil {
		c.hub.Unregister(c)
	}

	return c.conn.Close()
}

func (c *webSocketConnection) CloseWithCode(code int, text string) error {
	message := websocket.FormatCloseMessage(code, text)
	c.conn.WriteMessage(websocket.CloseMessage, message)
	return c.Close()
}

func (c *webSocketConnection) Ping(data []byte) error {
	return c.conn.WriteMessage(websocket.PingMessage, data)
}

func (c *webSocketConnection) Pong(data []byte) error {
	return c.conn.WriteMessage(websocket.PongMessage, data)
}

// Deadline methods
func (c *webSocketConnection) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *webSocketConnection) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// Handler methods
func (c *webSocketConnection) SetPongHandler(h func(string) error) {
	c.conn.SetPongHandler(h)
}

func (c *webSocketConnection) SetPingHandler(h func(string) error) {
	c.conn.SetPingHandler(h)
}

func (c *webSocketConnection) SetCloseHandler(h func(int, string) error) {
	c.conn.SetCloseHandler(h)
}

// Message pumps
func (c *webSocketConnection) readPump() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("WebSocket read pump panic",
				logger.Any("panic", r),
				logger.String("connection_id", c.id),
			)
		}
		c.Close()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
			messageType, data, err := c.conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					c.logger.Error("WebSocket read error",
						logger.Error(err),
						logger.String("connection_id", c.id),
					)
				}
				return
			}

			if messageType == websocket.TextMessage || messageType == websocket.BinaryMessage {
				select {
				case c.receive <- data:
				case <-c.ctx.Done():
					return
				}
			}
		}
	}
}

func (c *webSocketConnection) writePump() {
	defer func() {
		if r := recover(); r != nil {
			c.logger.Error("WebSocket write pump panic",
				logger.Any("panic", r),
				logger.String("connection_id", c.id),
			)
		}
		c.Close()
	}()

	for {
		select {
		case <-c.ctx.Done():
			return
		case data := <-c.send:
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				c.logger.Error("WebSocket write error",
					logger.Error(err),
					logger.String("connection_id", c.id),
				)
				return
			}
		}
	}
}

func (c *webSocketConnection) pingPump() {
	if c.pingInterval <= 0 {
		return
	}

	c.pingTimer = time.NewTimer(c.pingInterval)
	defer c.pingTimer.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.pingTimer.C:
			if err := c.Ping(nil); err != nil {
				c.logger.Error("Failed to send ping",
					logger.Error(err),
					logger.String("connection_id", c.id),
				)
				c.Close()
				return
			}
			c.pingTimer.Reset(c.pingInterval)
		}
	}
}

// WebSocket option implementations
type wsOption struct {
	apply func(*WebSocketConnectionConfig)
}

func (o *wsOption) Apply(config *WebSocketConnectionConfig) {
	o.apply(config)
}

// WithSubprotocols sets the WebSocket subprotocols
func WithSubprotocols(protocols ...string) WebSocketOption {
	return &wsOption{
		apply: func(config *WebSocketConnectionConfig) {
			config.Subprotocols = protocols
		},
	}
}

// WithBufferSizes sets the read and write buffer sizes
func WithBufferSizes(readSize, writeSize int) WebSocketOption {
	return &wsOption{
		apply: func(config *WebSocketConnectionConfig) {
			config.ReadBufferSize = readSize
			config.WriteBufferSize = writeSize
		},
	}
}

// WithOriginChecker sets the origin checker function
func WithOriginChecker(checker func(*http.Request) bool) WebSocketOption {
	return &wsOption{
		apply: func(config *WebSocketConnectionConfig) {
			config.CheckOrigin = checker
		},
	}
}

// WithCompression enables or disables compression
func WithCompression(enabled bool) WebSocketOption {
	return &wsOption{
		apply: func(config *WebSocketConnectionConfig) {
			config.EnableCompression = enabled
		},
	}
}

// Default configurations
func DefaultWebSocketConfig() WebSocketConfig {
	return WebSocketConfig{
		MaxConnections:    1000,
		MaxMessageSize:    1024 * 1024, // 1MB
		HandshakeTimeout:  10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Second,
		PingInterval:      54 * time.Second,
		PongTimeout:       60 * time.Second,
		ReadBufferSize:    4096,
		WriteBufferSize:   4096,
		EnableCompression: false,
		CheckOrigin:       nil, // Allow all origins
		Subprotocols:      []string{},
	}
}

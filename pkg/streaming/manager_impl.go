package streaming

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
	"github.com/xraph/forge/pkg/streaming/protocols"
)

// DefaultStreamingManager implements StreamingManager
type DefaultStreamingManager struct {
	// Configuration
	config StreamingConfig

	// Room management
	rooms   map[string]Room
	roomsMu sync.RWMutex

	// Connection management
	connectionManager ConnectionManager

	// Protocol handlers
	protocolHandlers map[string]ProtocolHandler
	protocolsMu      sync.RWMutex

	// Event handlers
	connectionEstablishedHandler ConnectionEstablishedHandler
	connectionClosedHandler      ConnectionClosedHandler
	messageHandler               GlobalMessageHandler
	errorHandler                 GlobalErrorHandler
	handlersMu                   sync.RWMutex

	// Statistics
	stats       StreamingStats
	statsMu     sync.RWMutex
	startTime   time.Time
	lastMessage time.Time

	// Dependencies
	logger  common.Logger
	metrics common.Metrics

	// Lifecycle
	started bool
	stopCh  chan struct{}
	mu      sync.RWMutex

	// Extensions
	messageSerializer MessageSerializer
	messageValidator  MessageValidator
}

// DefaultStreamingConfig returns default streaming configuration
func DefaultStreamingConfig() StreamingConfig {
	return StreamingConfig{
		MaxConnections:     10000,
		ConnectionTimeout:  60 * time.Second,
		HeartbeatInterval:  30 * time.Second,
		MaxRooms:           1000,
		DefaultRoomConfig:  DefaultRoomConfig(),
		MaxMessageSize:     32768, // 32KB
		MessageQueueSize:   1000,
		EnableWebSocket:    true,
		EnableSSE:          true,
		EnableLongPolling:  true,
		RequireAuth:        false,
		AllowCrossOrigin:   true,
		EnableRedisScaling: false,
		EnablePersistence:  false,
	}
}

// NewStreamingManager creates a new default streaming manager
func NewStreamingManager(config StreamingConfig, logger common.Logger, metrics common.Metrics) StreamingManager {
	manager := &DefaultStreamingManager{
		config:            config,
		rooms:             make(map[string]Room),
		protocolHandlers:  make(map[string]ProtocolHandler),
		logger:            logger,
		metrics:           metrics,
		stopCh:            make(chan struct{}),
		startTime:         time.Now(),
		messageSerializer: NewJSONMessageSerializer(),
		messageValidator:  NewDefaultMessageValidator(),
	}

	// Initialize connection manager
	manager.connectionManager = NewDefaultConnectionManager(logger, metrics)

	// Register default protocol handlers
	if config.EnableWebSocket {
		manager.RegisterProtocolHandler(NewWebSocketHandler(logger, metrics))
	}
	if config.EnableSSE {
		manager.RegisterProtocolHandler(NewSSEHandler(logger, metrics))
	}
	if config.EnableLongPolling {
		manager.RegisterProtocolHandler(NewLongPollingHandler(logger, metrics))
	}

	return manager
}

// Service interface implementation

// Name returns the service name
func (sm *DefaultStreamingManager) Name() string {
	return "streaming-manager"
}

// Dependencies returns service dependencies
func (sm *DefaultStreamingManager) Dependencies() []string {
	deps := []string{common.ConfigKey}

	if sm.config.EnablePersistence {
		deps = append(deps, "database-manager")
	}

	if sm.config.EnableRedisScaling {
		deps = append(deps, "redis-client")
	}

	return deps
}

// OnStart starts the streaming manager
func (sm *DefaultStreamingManager) OnStart(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.started {
		return nil
	}

	sm.started = true
	sm.startTime = time.Now()

	// Initialize statistics
	sm.stats = StreamingStats{
		ConnectionsByProtocol: make(map[ProtocolType]int),
		ConnectionsByRoom:     make(map[string]int),
	}

	if sm.logger != nil {
		sm.logger.Info("streaming manager started",
			logger.Int("max_connections", sm.config.MaxConnections),
			logger.Int("max_rooms", sm.config.MaxRooms),
			logger.Bool("websocket_enabled", sm.config.EnableWebSocket),
			logger.Bool("sse_enabled", sm.config.EnableSSE),
			logger.Bool("long_polling_enabled", sm.config.EnableLongPolling),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.manager.started").Inc()
	}

	return nil
}

// OnStop stops the streaming manager
func (sm *DefaultStreamingManager) OnStop(ctx context.Context) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if !sm.started {
		return nil
	}

	// Close all rooms
	sm.roomsMu.Lock()
	for roomID, room := range sm.rooms {
		if err := room.Close(ctx); err != nil {
			if sm.logger != nil {
				sm.logger.Warn("failed to close room during shutdown",
					logger.String("room_id", roomID),
					logger.Error(err),
				)
			}
		}
	}
	sm.rooms = make(map[string]Room)
	sm.roomsMu.Unlock()

	// Close stop channel
	close(sm.stopCh)

	sm.started = false

	if sm.logger != nil {
		sm.logger.Info("streaming manager stopped")
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.manager.stopped").Inc()
	}

	return nil
}

// OnHealthCheck performs health check
func (sm *DefaultStreamingManager) OnHealthCheck(ctx context.Context) error {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if !sm.started {
		return common.ErrHealthCheckFailed("streaming_manager", fmt.Errorf("streaming manager not started"))
	}

	// Check if we're within connection limits
	stats := sm.connectionManager.GetStats()
	if stats.ActiveConnections > sm.config.MaxConnections {
		return common.ErrHealthCheckFailed("streaming_manager",
			fmt.Errorf("connection count exceeds limit: %d > %d", stats.ActiveConnections, sm.config.MaxConnections))
	}

	// Check room count
	sm.roomsMu.RLock()
	roomCount := len(sm.rooms)
	sm.roomsMu.RUnlock()

	if roomCount > sm.config.MaxRooms {
		return common.ErrHealthCheckFailed("streaming_manager",
			fmt.Errorf("room count exceeds limit: %d > %d", roomCount, sm.config.MaxRooms))
	}

	return nil
}

// Room management

// CreateRoom creates a new room
func (sm *DefaultStreamingManager) CreateRoom(ctx context.Context, roomID string, config RoomConfig) (Room, error) {
	sm.roomsMu.Lock()
	defer sm.roomsMu.Unlock()

	// Check if room already exists
	if _, exists := sm.rooms[roomID]; exists {
		return nil, common.ErrServiceAlreadyExists(roomID)
	}

	// Check room limit
	if len(sm.rooms) >= sm.config.MaxRooms {
		return nil, common.ErrInvalidConfig("max_rooms",
			fmt.Errorf("maximum number of rooms reached: %d", sm.config.MaxRooms))
	}

	// Create room
	room := NewDefaultRoom(roomID, config, sm.logger, sm.metrics)

	// Set up room event handlers
	room.OnUserJoin(func(r Room, userID string, conn Connection) {
		sm.handleUserJoin(r, userID, conn)
	})

	room.OnUserLeave(func(r Room, userID string, conn Connection) {
		sm.handleUserLeave(r, userID, conn)
	})

	room.OnMessage(func(r Room, message *Message, sender Connection) {
		sm.handleRoomMessage(r, message, sender)
	})

	room.OnError(func(r Room, err error) {
		sm.handleRoomError(r, err)
	})

	// OnStart room
	if err := room.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start room: %w", err)
	}

	// Add to rooms map
	sm.rooms[roomID] = room

	// Update statistics
	sm.updateRoomStats()

	if sm.logger != nil {
		sm.logger.Info("room created",
			logger.String("room_id", roomID),
			logger.String("room_type", string(config.Type)),
			logger.Int("max_connections", config.MaxConnections),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.rooms.created").Inc()
		sm.metrics.Gauge("streaming.rooms.active").Set(float64(len(sm.rooms)))
	}

	return room, nil
}

// GetRoom gets a room by ID
func (sm *DefaultStreamingManager) GetRoom(roomID string) (Room, error) {
	sm.roomsMu.RLock()
	defer sm.roomsMu.RUnlock()

	room, exists := sm.rooms[roomID]
	if !exists {
		return nil, common.ErrServiceNotFound(roomID)
	}

	return room, nil
}

// DeleteRoom deletes a room
func (sm *DefaultStreamingManager) DeleteRoom(ctx context.Context, roomID string) error {
	sm.roomsMu.Lock()
	defer sm.roomsMu.Unlock()

	room, exists := sm.rooms[roomID]
	if !exists {
		return common.ErrServiceNotFound(roomID)
	}

	// Close and remove room
	if err := room.Close(ctx); err != nil {
		return fmt.Errorf("failed to close room: %w", err)
	}

	delete(sm.rooms, roomID)

	// Update statistics
	sm.updateRoomStats()

	if sm.logger != nil {
		sm.logger.Info("room deleted", logger.String("room_id", roomID))
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.rooms.deleted").Inc()
		sm.metrics.Gauge("streaming.rooms.active").Set(float64(len(sm.rooms)))
	}

	return nil
}

// GetRooms returns all rooms
func (sm *DefaultStreamingManager) GetRooms() []Room {
	sm.roomsMu.RLock()
	defer sm.roomsMu.RUnlock()

	rooms := make([]Room, 0, len(sm.rooms))
	for _, room := range sm.rooms {
		rooms = append(rooms, room)
	}

	return rooms
}

// GetRoomCount returns the number of rooms
func (sm *DefaultStreamingManager) GetRoomCount() int {
	sm.roomsMu.RLock()
	defer sm.roomsMu.RUnlock()
	return len(sm.rooms)
}

// Connection management

// HandleConnection handles a new connection request
func (sm *DefaultStreamingManager) HandleConnection(w http.ResponseWriter, r *http.Request) error {
	// Find appropriate protocol handler
	sm.protocolsMu.RLock()
	defer sm.protocolsMu.RUnlock()

	var selectedHandler ProtocolHandler
	maxPriority := -1

	for _, handler := range sm.protocolHandlers {
		if handler.CanHandle(r) && handler.Priority() > maxPriority {
			selectedHandler = handler
			maxPriority = handler.Priority()
		}
	}

	if selectedHandler == nil {
		return common.ErrServiceNotFound("no suitable protocol handler found")
	}

	// Handle the connection
	conn, err := selectedHandler.HandleUpgrade(w, r)
	if err != nil {
		return fmt.Errorf("failed to handle connection upgrade: %w", err)
	}

	// Add connection to manager
	if err := sm.connectionManager.AddConnection(conn); err != nil {
		conn.Close(context.Background())
		return fmt.Errorf("failed to add connection to manager: %w", err)
	}

	// Set up connection event handlers
	conn.OnMessage(func(c Connection, message *Message) error {
		return sm.handleConnectionMessage(c, message)
	})

	conn.OnClose(func(c Connection, reason string) {
		sm.handleConnectionClose(c, reason)
	})

	conn.OnError(func(c Connection, err error) {
		sm.handleConnectionError(c, err)
	})

	// Notify connection established handler
	sm.handlersMu.RLock()
	handler := sm.connectionEstablishedHandler
	sm.handlersMu.RUnlock()

	if handler != nil {
		go handler(conn)
	}

	// Update statistics
	sm.updateConnectionStats()

	if sm.logger != nil {
		sm.logger.Info("connection established",
			logger.String("connection_id", conn.ID()),
			logger.String("user_id", conn.UserID()),
			logger.String("protocol", string(conn.Protocol())),
			logger.String("remote_addr", conn.RemoteAddr()),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.connections.established").Inc()
	}

	return nil
}

// GetConnection gets a connection by ID
func (sm *DefaultStreamingManager) GetConnection(connectionID string) (Connection, error) {
	return sm.connectionManager.GetConnection(connectionID)
}

// GetConnections returns all connections
func (sm *DefaultStreamingManager) GetConnections() []Connection {
	return sm.connectionManager.GetAllConnections()
}

// GetConnectionsByUser returns connections for a specific user
func (sm *DefaultStreamingManager) GetConnectionsByUser(userID string) []Connection {
	return sm.connectionManager.GetConnectionsByUser(userID)
}

// GetConnectionsByRoom returns connections for a specific room
func (sm *DefaultStreamingManager) GetConnectionsByRoom(roomID string) []Connection {
	return sm.connectionManager.GetConnectionsByRoom(roomID)
}

// DisconnectUser disconnects all connections for a user
func (sm *DefaultStreamingManager) DisconnectUser(ctx context.Context, userID string, reason string) error {
	return sm.connectionManager.CloseUserConnections(ctx, userID, reason)
}

// DisconnectConnection disconnects a specific connection
func (sm *DefaultStreamingManager) DisconnectConnection(ctx context.Context, connectionID string, reason string) error {
	return sm.connectionManager.CloseConnection(ctx, connectionID, reason)
}

// Message handling

// BroadcastToRoom broadcasts a message to all connections in a room
func (sm *DefaultStreamingManager) BroadcastToRoom(ctx context.Context, roomID string, message *Message) error {
	room, err := sm.GetRoom(roomID)
	if err != nil {
		return err
	}

	return room.Broadcast(ctx, message)
}

// BroadcastToUser broadcasts a message to all connections of a user
func (sm *DefaultStreamingManager) BroadcastToUser(ctx context.Context, userID string, message *Message) error {
	return sm.connectionManager.BroadcastToUser(ctx, userID, message)
}

// BroadcastToAll broadcasts a message to all connections
func (sm *DefaultStreamingManager) BroadcastToAll(ctx context.Context, message *Message) error {
	return sm.connectionManager.BroadcastToAll(ctx, message)
}

// SendToConnection sends a message to a specific connection
func (sm *DefaultStreamingManager) SendToConnection(ctx context.Context, connectionID string, message *Message) error {
	conn, err := sm.GetConnection(connectionID)
	if err != nil {
		return err
	}

	return conn.Send(ctx, message)
}

// Protocol management

// RegisterProtocolHandler registers a protocol handler
func (sm *DefaultStreamingManager) RegisterProtocolHandler(handler ProtocolHandler) error {
	sm.protocolsMu.Lock()
	defer sm.protocolsMu.Unlock()

	name := handler.Name()
	if _, exists := sm.protocolHandlers[name]; exists {
		return common.ErrServiceAlreadyExists(name)
	}

	sm.protocolHandlers[name] = handler

	if sm.logger != nil {
		sm.logger.Info("protocol handler registered",
			logger.String("handler", name),
			logger.Int("priority", handler.Priority()),
		)
	}

	return nil
}

// UnregisterProtocolHandler unregisters a protocol handler
func (sm *DefaultStreamingManager) UnregisterProtocolHandler(name string) error {
	sm.protocolsMu.Lock()
	defer sm.protocolsMu.Unlock()

	if _, exists := sm.protocolHandlers[name]; !exists {
		return common.ErrServiceNotFound(name)
	}

	delete(sm.protocolHandlers, name)

	if sm.logger != nil {
		sm.logger.Info("protocol handler unregistered", logger.String("handler", name))
	}

	return nil
}

// GetProtocolHandlers returns all protocol handlers
func (sm *DefaultStreamingManager) GetProtocolHandlers() []ProtocolHandler {
	sm.protocolsMu.RLock()
	defer sm.protocolsMu.RUnlock()

	handlers := make([]ProtocolHandler, 0, len(sm.protocolHandlers))
	for _, handler := range sm.protocolHandlers {
		handlers = append(handlers, handler)
	}

	return handlers
}

// Event handling

// OnConnectionEstablished sets the connection established handler
func (sm *DefaultStreamingManager) OnConnectionEstablished(handler ConnectionEstablishedHandler) {
	sm.handlersMu.Lock()
	defer sm.handlersMu.Unlock()
	sm.connectionEstablishedHandler = handler
}

// OnConnectionClosed sets the connection closed handler
func (sm *DefaultStreamingManager) OnConnectionClosed(handler ConnectionClosedHandler) {
	sm.handlersMu.Lock()
	defer sm.handlersMu.Unlock()
	sm.connectionClosedHandler = handler
}

// OnMessage sets the global message handler
func (sm *DefaultStreamingManager) OnMessage(handler GlobalMessageHandler) {
	sm.handlersMu.Lock()
	defer sm.handlersMu.Unlock()
	sm.messageHandler = handler
}

// OnError sets the global error handler
func (sm *DefaultStreamingManager) OnError(handler GlobalErrorHandler) {
	sm.handlersMu.Lock()
	defer sm.handlersMu.Unlock()
	sm.errorHandler = handler
}

// Event handlers

// handleUserJoin handles user join events
func (sm *DefaultStreamingManager) handleUserJoin(room Room, userID string, conn Connection) {
	if sm.logger != nil {
		sm.logger.Debug("user joined room",
			logger.String("user_id", userID),
			logger.String("room_id", room.ID()),
			logger.String("connection_id", conn.ID()),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.room.user_joins").Inc()
	}
}

// handleUserLeave handles user leave events
func (sm *DefaultStreamingManager) handleUserLeave(room Room, userID string, conn Connection) {
	if sm.logger != nil {
		sm.logger.Debug("user left room",
			logger.String("user_id", userID),
			logger.String("room_id", room.ID()),
			logger.String("connection_id", conn.ID()),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.room.user_leaves").Inc()
	}
}

// handleRoomMessage handles room message events
func (sm *DefaultStreamingManager) handleRoomMessage(room Room, message *Message, sender Connection) {
	sm.lastMessage = time.Now()

	sm.handlersMu.RLock()
	handler := sm.messageHandler
	sm.handlersMu.RUnlock()

	if handler != nil {
		go handler(room, message, sender)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.messages.room").Inc()
	}
}

// handleRoomError handles room error events
func (sm *DefaultStreamingManager) handleRoomError(room Room, err error) {
	sm.handlersMu.RLock()
	handler := sm.errorHandler
	sm.handlersMu.RUnlock()

	if handler != nil {
		context := map[string]interface{}{
			"room_id":      room.ID(),
			"error_source": "room",
		}
		go handler(err, context)
	}

	if sm.logger != nil {
		sm.logger.Error("room error",
			logger.String("room_id", room.ID()),
			logger.Error(err),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.errors.room").Inc()
	}
}

// handleConnectionMessage handles connection message events
func (sm *DefaultStreamingManager) handleConnectionMessage(conn Connection, message *Message) error {
	// Validate message
	if err := sm.messageValidator.Validate(message); err != nil {
		return fmt.Errorf("message validation failed: %w", err)
	}

	// Route message to appropriate room
	if message.RoomID != "" {
		room, err := sm.GetRoom(message.RoomID)
		if err != nil {
			return fmt.Errorf("room not found: %w", err)
		}

		// Check if user is muted
		if room.IsUserMuted(conn.UserID()) {
			return common.ErrValidationError("user_muted", fmt.Errorf("user is muted in room %s", message.RoomID))
		}

		// Broadcast message to room (excluding sender)
		return room.BroadcastExcept(context.Background(), conn.ID(), message)
	}

	return nil
}

// handleConnectionClose handles connection close events
func (sm *DefaultStreamingManager) handleConnectionClose(conn Connection, reason string) {
	// Remove connection from manager
	sm.connectionManager.RemoveConnection(conn.ID())

	// Update statistics
	sm.updateConnectionStats()

	// Notify global handler
	sm.handlersMu.RLock()
	handler := sm.connectionClosedHandler
	sm.handlersMu.RUnlock()

	if handler != nil {
		go handler(conn, reason)
	}

	if sm.logger != nil {
		sm.logger.Info("connection closed",
			logger.String("connection_id", conn.ID()),
			logger.String("user_id", conn.UserID()),
			logger.String("reason", reason),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.connections.closed").Inc()
	}
}

// handleConnectionError handles connection error events
func (sm *DefaultStreamingManager) handleConnectionError(conn Connection, err error) {
	sm.handlersMu.RLock()
	handler := sm.errorHandler
	sm.handlersMu.RUnlock()

	if handler != nil {
		context := map[string]interface{}{
			"connection_id": conn.ID(),
			"user_id":       conn.UserID(),
			"error_source":  "connection",
		}
		go handler(err, context)
	}

	if sm.logger != nil {
		sm.logger.Error("connection error",
			logger.String("connection_id", conn.ID()),
			logger.String("user_id", conn.UserID()),
			logger.Error(err),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.errors.connection").Inc()
	}
}

// Statistics and monitoring

// GetStats returns streaming manager statistics
func (sm *DefaultStreamingManager) GetStats() StreamingStats {
	sm.statsMu.Lock()
	defer sm.statsMu.Unlock()

	// Update dynamic statistics
	connStats := sm.connectionManager.GetStats()
	sm.stats.TotalConnections = connStats.TotalConnections
	sm.stats.ActiveConnections = connStats.ActiveConnections
	sm.stats.ConnectionsByProtocol = connStats.ConnectionsByProtocol
	sm.stats.ConnectionsByRoom = connStats.ConnectionsByRoom
	sm.stats.BytesTransferred = connStats.TotalBytesTransferred
	sm.stats.ErrorCount = int64(connStats.ErrorRate * float64(connStats.TotalMessages))

	// Update room statistics
	sm.roomsMu.RLock()
	sm.stats.TotalRooms = len(sm.rooms)
	activeRooms := 0
	for _, room := range sm.rooms {
		if room.IsActive() {
			activeRooms++
		}
	}
	sm.stats.ActiveRooms = activeRooms
	sm.roomsMu.RUnlock()

	// Calculate uptime
	sm.stats.Uptime = time.Since(sm.startTime)

	// Calculate messages per second
	if !sm.lastMessage.IsZero() {
		duration := time.Since(sm.startTime)
		if duration.Seconds() > 0 {
			sm.stats.MessagesPerSecond = float64(sm.stats.TotalMessages) / duration.Seconds()
		}
	}

	sm.stats.LastMessage = sm.lastMessage

	return sm.stats
}

// GetRoomStats returns statistics for a specific room
func (sm *DefaultStreamingManager) GetRoomStats(roomID string) (RoomStats, error) {
	room, err := sm.GetRoom(roomID)
	if err != nil {
		return RoomStats{}, err
	}

	return room.GetStats(), nil
}

// GetConnectionStats returns statistics for a specific connection
func (sm *DefaultStreamingManager) GetConnectionStats(connectionID string) (ConnectionStats, error) {
	conn, err := sm.GetConnection(connectionID)
	if err != nil {
		return ConnectionStats{}, err
	}

	return conn.GetStats(), nil
}

// Configuration

// GetConfig returns the current configuration
func (sm *DefaultStreamingManager) GetConfig() StreamingConfig {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.config
}

// UpdateConfig updates the configuration
func (sm *DefaultStreamingManager) UpdateConfig(config StreamingConfig) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.config = config

	if sm.logger != nil {
		sm.logger.Info("streaming manager configuration updated")
	}

	return nil
}

// Helper methods

// updateConnectionStats updates connection statistics
func (sm *DefaultStreamingManager) updateConnectionStats() {
	stats := sm.connectionManager.GetStats()

	if sm.metrics != nil {
		sm.metrics.Gauge("streaming.connections.active").Set(float64(stats.ActiveConnections))
		sm.metrics.Gauge("streaming.connections.total").Set(float64(stats.TotalConnections))
	}
}

// updateRoomStats updates room statistics
func (sm *DefaultStreamingManager) updateRoomStats() {
	roomCount := len(sm.rooms)

	if sm.metrics != nil {
		sm.metrics.Gauge("streaming.rooms.active").Set(float64(roomCount))
	}
}

// HandleWebSocket specifically handles WebSocket upgrade requests
func (sm *DefaultStreamingManager) HandleWebSocket(w http.ResponseWriter, r *http.Request, config ConnectionConfig) (Connection, error) {
	// Find WebSocket protocol handler
	sm.protocolsMu.RLock()
	var wsHandler ProtocolHandler
	for _, handler := range sm.protocolHandlers {
		if handler.Name() == "websocket" && handler.CanHandle(r) {
			wsHandler = handler
			break
		}
	}
	sm.protocolsMu.RUnlock()

	if wsHandler == nil {
		return nil, common.ErrServiceNotFound("WebSocket protocol handler not found")
	}

	// Handle the WebSocket upgrade
	conn, err := wsHandler.HandleUpgrade(w, r)
	if err != nil {
		return nil, fmt.Errorf("failed to upgrade WebSocket connection: %w", err)
	}

	// Apply connection configuration
	if err := sm.applyConnectionConfig(conn, config); err != nil {
		conn.Close(context.Background())
		return nil, fmt.Errorf("failed to apply connection config: %w", err)
	}

	// Add connection to manager
	if err := sm.connectionManager.AddConnection(conn); err != nil {
		conn.Close(context.Background())
		return nil, fmt.Errorf("failed to add connection to manager: %w", err)
	}

	// Set up connection event handlers
	sm.setupConnectionHandlers(conn)

	// Update statistics
	sm.updateConnectionStats()

	if sm.logger != nil {
		sm.logger.Info("WebSocket connection established",
			logger.String("connection_id", conn.ID()),
			logger.String("user_id", conn.UserID()),
			logger.String("remote_addr", conn.RemoteAddr()),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.connections.websocket").Inc()
	}

	return conn, nil
}

// HandleSSE specifically handles Server-Sent Events connections
func (sm *DefaultStreamingManager) HandleSSE(w http.ResponseWriter, r *http.Request, config ConnectionConfig) (Connection, error) {
	// Find SSE protocol handler
	sm.protocolsMu.RLock()
	var sseHandler ProtocolHandler
	for _, handler := range sm.protocolHandlers {
		if handler.Name() == "sse" && handler.CanHandle(r) {
			sseHandler = handler
			break
		}
	}
	sm.protocolsMu.RUnlock()

	if sseHandler == nil {
		return nil, common.ErrServiceNotFound("SSE protocol handler not found")
	}

	// Handle the SSE connection
	conn, err := sseHandler.HandleUpgrade(w, r)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSE connection: %w", err)
	}

	// Apply connection configuration
	if err := sm.applyConnectionConfig(conn, config); err != nil {
		conn.Close(context.Background())
		return nil, fmt.Errorf("failed to apply connection config: %w", err)
	}

	// Add connection to manager
	if err := sm.connectionManager.AddConnection(conn); err != nil {
		conn.Close(context.Background())
		return nil, fmt.Errorf("failed to add connection to manager: %w", err)
	}

	// Set up connection event handlers
	sm.setupConnectionHandlers(conn)

	// Update statistics
	sm.updateConnectionStats()

	if sm.logger != nil {
		sm.logger.Info("SSE connection established",
			logger.String("connection_id", conn.ID()),
			logger.String("user_id", conn.UserID()),
			logger.String("remote_addr", conn.RemoteAddr()),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.connections.sse").Inc()
	}

	return conn, nil
}

// HandleLongPolling specifically handles Long Polling connections
func (sm *DefaultStreamingManager) HandleLongPolling(w http.ResponseWriter, r *http.Request, config ConnectionConfig) (Connection, error) {
	// Find Long Polling protocol handler
	sm.protocolsMu.RLock()
	var lpHandler ProtocolHandler
	for _, handler := range sm.protocolHandlers {
		if handler.Name() == "longpolling" && handler.CanHandle(r) {
			lpHandler = handler
			break
		}
	}
	sm.protocolsMu.RUnlock()

	if lpHandler == nil {
		return nil, common.ErrServiceNotFound("Long Polling protocol handler not found")
	}

	// Handle the Long Polling connection
	conn, err := lpHandler.HandleUpgrade(w, r)
	if err != nil {
		return nil, fmt.Errorf("failed to create Long Polling connection: %w", err)
	}

	// Apply connection configuration
	if err := sm.applyConnectionConfig(conn, config); err != nil {
		conn.Close(context.Background())
		return nil, fmt.Errorf("failed to apply connection config: %w", err)
	}

	// Add connection to manager
	if err := sm.connectionManager.AddConnection(conn); err != nil {
		conn.Close(context.Background())
		return nil, fmt.Errorf("failed to add connection to manager: %w", err)
	}

	// Set up connection event handlers
	sm.setupConnectionHandlers(conn)

	// Update statistics
	sm.updateConnectionStats()

	if sm.logger != nil {
		sm.logger.Info("Long Polling connection established",
			logger.String("connection_id", conn.ID()),
			logger.String("user_id", conn.UserID()),
			logger.String("remote_addr", conn.RemoteAddr()),
		)
	}

	if sm.metrics != nil {
		sm.metrics.Counter("streaming.connections.longpolling").Inc()
	}

	return conn, nil
}

// applyConnectionConfig applies configuration to a connection
func (sm *DefaultStreamingManager) applyConnectionConfig(conn Connection, config ConnectionConfig) error {
	// Set connection timeout if specified
	if config.ConnectionTimeout > 0 {
		conn.SetTimeout(config.ConnectionTimeout)
	}

	// Set user ID if specified
	if config.UserID != "" {
		err := conn.SetUserID(config.UserID)
		if err != nil {
			return err
		}
	}

	// Set room ID if specified
	if config.RoomID != "" {
		err := conn.SetRoomID(config.RoomID)
		if err != nil {
			return err
		}
	}

	// Apply other configuration options as needed
	if config.MaxMessageSize > 0 {
		err := conn.SetMaxMessageSize(int(config.MaxMessageSize))
		if err != nil {
			return err
		}
	}

	return nil
}

// setupConnectionHandlers sets up event handlers for a connection
func (sm *DefaultStreamingManager) setupConnectionHandlers(conn Connection) {
	// Set up message handler
	conn.OnMessage(func(c Connection, message *Message) error {
		return sm.handleConnectionMessage(c, message)
	})

	// Set up close handler
	conn.OnClose(func(c Connection, reason string) {
		sm.handleConnectionClose(c, reason)
	})

	// Set up error handler
	conn.OnError(func(c Connection, err error) {
		sm.handleConnectionError(c, err)
	})

	// Notify connection established handler
	sm.handlersMu.RLock()
	handler := sm.connectionEstablishedHandler
	sm.handlersMu.RUnlock()

	if handler != nil {
		go handler(conn)
	}
}

// Stub implementations for protocol handlers (will be implemented in protocols/ directory)

// NewWebSocketHandler creates a new WebSocket protocol handler
func NewWebSocketHandler(logger common.Logger, metrics common.Metrics) ProtocolHandler {
	return protocols.NewWebSocketHandler(logger, metrics)
}

// NewSSEHandler creates a new SSE protocol handler
func NewSSEHandler(logger common.Logger, metrics common.Metrics) ProtocolHandler {
	return protocols.NewSSEHandler(logger, metrics)
}

// NewLongPollingHandler creates a new long polling protocol handler
func NewLongPollingHandler(logger common.Logger, metrics common.Metrics) ProtocolHandler {
	return protocols.NewLongPollingHandler(logger, metrics)
}

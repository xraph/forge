package streaming

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/internal/errors"
)

// manager implements the Manager interface.
type manager struct {
	mu sync.RWMutex

	// Stores
	roomStore    RoomStore
	channelStore ChannelStore
	messageStore MessageStore

	// Trackers
	presenceTracker PresenceTracker
	typingTracker   TypingTracker

	// Distributed backend (optional)
	distributed DistributedBackend

	// Connection registry
	connections map[string]Connection // connID -> connection
	userConns   map[string][]string   // userID -> []connID

	// Configuration
	config Config

	// Logger and metrics
	logger  forge.Logger
	metrics forge.Metrics

	// Lifecycle
	started bool
}

// NewManager creates a new streaming manager.
func NewManager(
	config Config,
	roomStore RoomStore,
	channelStore ChannelStore,
	messageStore MessageStore,
	presenceTracker PresenceTracker,
	typingTracker TypingTracker,
	distributed DistributedBackend,
	logger forge.Logger,
	metrics forge.Metrics,
) Manager {
	return &manager{
		roomStore:       roomStore,
		channelStore:    channelStore,
		messageStore:    messageStore,
		presenceTracker: presenceTracker,
		typingTracker:   typingTracker,
		distributed:     distributed,
		connections:     make(map[string]Connection),
		userConns:       make(map[string][]string),
		config:          config,
		logger:          logger,
		metrics:         metrics,
		started:         false,
	}
}

// Connection management

func (m *manager) Register(conn Connection) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	connID := conn.ID()
	userID := conn.GetUserID()

	// Check connection limit per user
	if userID != "" {
		if userConns, exists := m.userConns[userID]; exists {
			if len(userConns) >= m.config.MaxConnectionsPerUser {
				return ErrConnectionLimitReached
			}
		}
	}

	// Register connection
	m.connections[connID] = conn

	// Index by user
	if userID != "" {
		m.userConns[userID] = append(m.userConns[userID], connID)
	}

	// Track metrics
	if m.metrics != nil {
		m.metrics.Gauge("streaming.connections.active").Inc()
		m.metrics.Counter("streaming.connections.total").Inc()
	}

	if m.logger != nil {
		m.logger.Debug("connection registered",
			forge.F("conn_id", connID),
			forge.F("user_id", userID),
		)
	}

	return nil
}

func (m *manager) Unregister(connID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, exists := m.connections[connID]
	if !exists {
		return ErrConnectionNotFound
	}

	userID := conn.GetUserID()

	// Remove from user index
	if userID != "" {
		if userConns, exists := m.userConns[userID]; exists {
			m.userConns[userID] = removeFromSlice(userConns, connID)
			if len(m.userConns[userID]) == 0 {
				delete(m.userConns, userID)
			}
		}
	}

	// Remove connection
	delete(m.connections, connID)

	// Track metrics
	if m.metrics != nil {
		m.metrics.Gauge("streaming.connections.active").Dec()
	}

	if m.logger != nil {
		m.logger.Debug("connection unregistered",
			forge.F("conn_id", connID),
			forge.F("user_id", userID),
		)
	}

	return nil
}

func (m *manager) GetConnection(connID string) (Connection, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.connections[connID]
	if !exists {
		return nil, ErrConnectionNotFound
	}

	return conn, nil
}

func (m *manager) GetUserConnections(userID string) []Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	connIDs, exists := m.userConns[userID]
	if !exists {
		return []Connection{}
	}

	conns := make([]Connection, 0, len(connIDs))
	for _, connID := range connIDs {
		if conn, exists := m.connections[connID]; exists {
			conns = append(conns, conn)
		}
	}

	return conns
}

func (m *manager) GetAllConnections() []Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conns := make([]Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		conns = append(conns, conn)
	}

	return conns
}

func (m *manager) ConnectionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.connections)
}

// Room operations

func (m *manager) CreateRoom(ctx context.Context, room Room) error {
	if !m.config.EnableRooms {
		return errors.New("rooms are disabled")
	}

	if err := m.roomStore.Create(ctx, room); err != nil {
		return err
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.rooms.created").Inc()
	}

	return nil
}

func (m *manager) GetRoom(ctx context.Context, roomID string) (Room, error) {
	return m.roomStore.Get(ctx, roomID)
}

func (m *manager) DeleteRoom(ctx context.Context, roomID string) error {
	if err := m.roomStore.Delete(ctx, roomID); err != nil {
		return err
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.rooms.deleted").Inc()
	}

	return nil
}

func (m *manager) JoinRoom(ctx context.Context, connID, roomID string) error {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	userID := conn.GetUserID()
	if userID == "" {
		return errors.New("connection has no user ID")
	}

	// Check room limit
	userRooms, _ := m.roomStore.GetUserRooms(ctx, userID)
	if len(userRooms) >= m.config.MaxRoomsPerUser {
		return ErrRoomLimitReached
	}

	// Add to room (handled by room store separately if needed)
	conn.AddRoom(roomID)

	if m.metrics != nil {
		m.metrics.Counter("streaming.rooms.joins").Inc()
	}

	return nil
}

func (m *manager) LeaveRoom(ctx context.Context, connID, roomID string) error {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	conn.RemoveRoom(roomID)

	if m.metrics != nil {
		m.metrics.Counter("streaming.rooms.leaves").Inc()
	}

	return nil
}

func (m *manager) GetRoomMembers(ctx context.Context, roomID string) ([]Member, error) {
	return m.roomStore.GetMembers(ctx, roomID)
}

func (m *manager) ListRooms(ctx context.Context) ([]Room, error) {
	return m.roomStore.List(ctx, nil)
}

// Channel operations

func (m *manager) CreateChannel(ctx context.Context, channel Channel) error {
	if !m.config.EnableChannels {
		return errors.New("channels are disabled")
	}

	if err := m.channelStore.Create(ctx, channel); err != nil {
		return err
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.channels.created").Inc()
	}

	return nil
}

func (m *manager) GetChannel(ctx context.Context, channelID string) (Channel, error) {
	return m.channelStore.Get(ctx, channelID)
}

func (m *manager) DeleteChannel(ctx context.Context, channelID string) error {
	if err := m.channelStore.Delete(ctx, channelID); err != nil {
		return err
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.channels.deleted").Inc()
	}

	return nil
}

func (m *manager) Subscribe(ctx context.Context, connID, channelID string, filters map[string]any) error {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	userID := conn.GetUserID()

	// Check channel limit
	userChannels, _ := m.channelStore.GetUserChannels(ctx, userID)
	if len(userChannels) >= m.config.MaxChannelsPerUser {
		return errors.New("channel limit reached")
	}

	conn.AddSubscription(channelID)

	if m.metrics != nil {
		m.metrics.Counter("streaming.channels.subscriptions").Inc()
	}

	return nil
}

func (m *manager) Unsubscribe(ctx context.Context, connID, channelID string) error {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	conn.RemoveSubscription(channelID)

	if m.metrics != nil {
		m.metrics.Counter("streaming.channels.unsubscriptions").Inc()
	}

	return nil
}

func (m *manager) ListChannels(ctx context.Context) ([]Channel, error) {
	return m.channelStore.List(ctx)
}

// Message broadcasting

func (m *manager) Broadcast(ctx context.Context, message *Message) error {
	conns := m.GetAllConnections()

	for _, conn := range conns {
		if err := conn.WriteJSON(message); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to broadcast message",
					forge.F("conn_id", conn.ID()),
					forge.F("error", err),
				)
			}
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.broadcast").Inc()
	}

	return nil
}

func (m *manager) BroadcastToRoom(ctx context.Context, roomID string, message *Message) error {
	conns := m.GetAllConnections()

	count := 0

	for _, conn := range conns {
		if conn.IsInRoom(roomID) {
			if err := conn.WriteJSON(message); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to send room message",
						forge.F("conn_id", conn.ID()),
						forge.F("room_id", roomID),
						forge.F("error", err),
					)
				}
			} else {
				count++
			}
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.room_broadcast").Inc()
		m.metrics.Gauge("streaming.messages.room_recipients").Set(float64(count))
	}

	return nil
}

func (m *manager) BroadcastToChannel(ctx context.Context, channelID string, message *Message) error {
	conns := m.GetAllConnections()

	count := 0

	for _, conn := range conns {
		if conn.IsSubscribed(channelID) {
			if err := conn.WriteJSON(message); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to send channel message",
						forge.F("conn_id", conn.ID()),
						forge.F("channel_id", channelID),
						forge.F("error", err),
					)
				}
			} else {
				count++
			}
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.channel_broadcast").Inc()
		m.metrics.Gauge("streaming.messages.channel_recipients").Set(float64(count))
	}

	return nil
}

func (m *manager) BroadcastToRooms(ctx context.Context, roomIDs []string, message *Message) error {
	for _, roomID := range roomIDs {
		if err := m.BroadcastToRoom(ctx, roomID, message); err != nil {
			return err
		}
	}

	return nil
}

func (m *manager) BroadcastToUsers(ctx context.Context, userIDs []string, message *Message) error {
	for _, userID := range userIDs {
		if err := m.SendToUser(ctx, userID, message); err != nil {
			return err
		}
	}

	return nil
}

func (m *manager) SendToUser(ctx context.Context, userID string, message *Message) error {
	conns := m.GetUserConnections(userID)

	for _, conn := range conns {
		if err := conn.WriteJSON(message); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to send user message",
					forge.F("conn_id", conn.ID()),
					forge.F("user_id", userID),
					forge.F("error", err),
				)
			}
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.user").Inc()
	}

	return nil
}

func (m *manager) SendToConnection(ctx context.Context, connID string, message *Message) error {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	if err := conn.WriteJSON(message); err != nil {
		return NewConnectionError(connID, "send", err)
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.direct").Inc()
	}

	return nil
}

func (m *manager) BroadcastExcept(ctx context.Context, message *Message, excludeConnIDs []string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	excludeMap := make(map[string]bool)
	for _, id := range excludeConnIDs {
		excludeMap[id] = true
	}

	for connID, conn := range m.connections {
		if !excludeMap[connID] {
			if err := conn.WriteJSON(message); err != nil {
				if m.logger != nil {
					m.logger.Error("Failed to send message to connection",
						forge.F("conn_id", connID),
						forge.F("error", err),
					)
				}
			}
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.broadcast_except").Inc()
	}

	return nil
}

// Presence operations

func (m *manager) SetPresence(ctx context.Context, userID, status string) error {
	if !m.config.EnablePresence {
		return errors.New("presence tracking is disabled")
	}

	return m.presenceTracker.SetPresence(ctx, userID, status)
}

func (m *manager) GetPresence(ctx context.Context, userID string) (*UserPresence, error) {
	return m.presenceTracker.GetPresence(ctx, userID)
}

func (m *manager) GetOnlineUsers(ctx context.Context, roomID string) ([]string, error) {
	if roomID != "" {
		return m.presenceTracker.GetOnlineUsersInRoom(ctx, roomID)
	}

	return m.presenceTracker.GetOnlineUsers(ctx)
}

func (m *manager) TrackActivity(ctx context.Context, userID string) error {
	if !m.config.EnablePresence {
		return nil
	}

	return m.presenceTracker.TrackActivity(ctx, userID)
}

// Typing operations

func (m *manager) StartTyping(ctx context.Context, userID, roomID string) error {
	if !m.config.EnableTypingIndicators {
		return errors.New("typing indicators are disabled")
	}

	return m.typingTracker.StartTyping(ctx, userID, roomID)
}

func (m *manager) StopTyping(ctx context.Context, userID, roomID string) error {
	if !m.config.EnableTypingIndicators {
		return nil
	}

	return m.typingTracker.StopTyping(ctx, userID, roomID)
}

func (m *manager) GetTypingUsers(ctx context.Context, roomID string) ([]string, error) {
	return m.typingTracker.GetTypingUsers(ctx, roomID)
}

// Message history

func (m *manager) SaveMessage(ctx context.Context, message *Message) error {
	if !m.config.EnableMessageHistory {
		return nil
	}

	return m.messageStore.Save(ctx, message)
}

func (m *manager) GetHistory(ctx context.Context, roomID string, query HistoryQuery) ([]*Message, error) {
	return m.messageStore.GetHistory(ctx, roomID, query)
}

// Lifecycle

func (m *manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	// Connect stores
	if err := m.roomStore.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect room store: %w", err)
	}

	if err := m.channelStore.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect channel store: %w", err)
	}

	if m.config.EnableMessageHistory {
		if err := m.messageStore.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect message store: %w", err)
		}
	}

	// Start trackers
	if m.config.EnablePresence {
		if err := m.presenceTracker.Start(ctx); err != nil {
			return fmt.Errorf("failed to start presence tracker: %w", err)
		}
	}

	if m.config.EnableTypingIndicators {
		if err := m.typingTracker.Start(ctx); err != nil {
			return fmt.Errorf("failed to start typing tracker: %w", err)
		}
	}

	// Connect distributed backend if enabled
	if m.config.EnableDistributed && m.distributed != nil {
		if err := m.distributed.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect distributed backend: %w", err)
		}
	}

	m.started = true

	if m.logger != nil {
		m.logger.Info("streaming manager started")
	}

	return nil
}

func (m *manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	// Stop trackers
	if m.config.EnablePresence && m.presenceTracker != nil {
		_ = m.presenceTracker.Stop(ctx)
	}

	if m.config.EnableTypingIndicators && m.typingTracker != nil {
		_ = m.typingTracker.Stop(ctx)
	}

	// Disconnect stores
	_ = m.roomStore.Disconnect(ctx)

	_ = m.channelStore.Disconnect(ctx)
	if m.config.EnableMessageHistory {
		_ = m.messageStore.Disconnect(ctx)
	}

	// Disconnect distributed backend
	if m.config.EnableDistributed && m.distributed != nil {
		_ = m.distributed.Disconnect(ctx)
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("streaming manager stopped")
	}

	return nil
}

func (m *manager) Health(ctx context.Context) error {
	// Check stores
	if err := m.roomStore.Ping(ctx); err != nil {
		return fmt.Errorf("room store unhealthy: %w", err)
	}

	if err := m.channelStore.Ping(ctx); err != nil {
		return fmt.Errorf("channel store unhealthy: %w", err)
	}

	if m.config.EnableMessageHistory {
		if err := m.messageStore.Ping(ctx); err != nil {
			return fmt.Errorf("message store unhealthy: %w", err)
		}
	}

	// Check distributed backend
	if m.config.EnableDistributed && m.distributed != nil {
		if err := m.distributed.Ping(ctx); err != nil {
			return fmt.Errorf("distributed backend unhealthy: %w", err)
		}
	}

	return nil
}

func (m *manager) GetConnectionsByStatus(status string) []streaming.EnhancedConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []streaming.EnhancedConnection

	for _, conn := range m.connections {
		// Check if connection has the specified status
		// For now, we'll check if the connection is closed or not
		if status == "closed" && conn.IsClosed() {
			result = append(result, conn)
		} else if status == "active" && !conn.IsClosed() {
			result = append(result, conn)
		}
	}

	return result
}

func (m *manager) KickConnection(ctx context.Context, connID string, reason string) error {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	// Send kick message to connection
	kickMessage := &streaming.Message{
		ID:        fmt.Sprintf("kick_%d", time.Now().UnixNano()),
		Type:      "system",
		Event:     "kicked",
		UserID:    "system",
		Data:      map[string]any{"reason": reason},
		Timestamp: time.Now(),
	}

	if err := conn.WriteJSON(kickMessage); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to send kick message",
				forge.F("conn_id", connID),
				forge.F("error", err),
			)
		}
	}

	// Close the connection
	conn.Close()

	// Unregister the connection
	if err := m.Unregister(connID); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to unregister kicked connection",
				forge.F("conn_id", connID),
				forge.F("error", err),
			)
		}
	}

	if m.logger != nil {
		m.logger.Info("connection kicked",
			forge.F("conn_id", connID),
			forge.F("reason", reason),
		)
	}

	return nil
}

func (m *manager) GetConnectionInfo(connID string) (*streaming.ConnectionInfo, error) {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return nil, err
	}

	info := &streaming.ConnectionInfo{
		ID:            conn.ID(),
		UserID:        conn.GetUserID(),
		SessionID:     conn.GetSessionID(),
		ConnectedAt:   time.Now(), // TODO: track actual connection time
		LastActivity:  conn.GetLastActivity(),
		RoomsJoined:   conn.GetJoinedRooms(),
		Subscriptions: conn.GetSubscriptions(),
		Metadata:      make(map[string]any),
	}

	// Get metadata from connection
	for key, value := range map[string]string{
		"ip_address": "unknown",
		"user_agent": "unknown",
	} {
		if val, exists := conn.GetMetadata(key); exists {
			info.Metadata[key] = val
		} else {
			info.Metadata[key] = value
		}
	}

	return info, nil
}

func (m *manager) GetIdleConnections(idleFor time.Duration) []streaming.EnhancedConnection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var idleConns []streaming.EnhancedConnection

	cutoff := time.Now().Add(-idleFor)

	for _, conn := range m.connections {
		if conn.GetLastActivity().Before(cutoff) {
			idleConns = append(idleConns, conn)
		}
	}

	return idleConns
}

func (m *manager) CleanupIdleConnections(ctx context.Context, idleFor time.Duration) (int, error) {
	idleConns := m.GetIdleConnections(idleFor)
	cleanedCount := 0

	for _, conn := range idleConns {
		connID := conn.ID()

		// Send cleanup notification
		cleanupMessage := &streaming.Message{
			ID:        fmt.Sprintf("cleanup_%d", time.Now().UnixNano()),
			Type:      "system",
			Event:     "idle_cleanup",
			UserID:    "system",
			Data:      map[string]any{"reason": "idle timeout"},
			Timestamp: time.Now(),
		}

		if err := conn.WriteJSON(cleanupMessage); err != nil {
			if m.logger != nil {
				m.logger.Debug("failed to send cleanup message to idle connection",
					forge.F("conn_id", connID),
					forge.F("error", err),
				)
			}
		}

		// Close and unregister the connection
		conn.Close()

		if err := m.Unregister(connID); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to unregister idle connection",
					forge.F("conn_id", connID),
					forge.F("error", err),
				)
			}
		} else {
			cleanedCount++
		}
	}

	if m.logger != nil && cleanedCount > 0 {
		m.logger.Info("cleaned up idle connections",
			forge.F("count", cleanedCount),
			forge.F("idle_for", idleFor),
		)
	}

	return cleanedCount, nil
}

func (m *manager) UpdateRoom(ctx context.Context, roomID string, updates map[string]any) error {
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Update the room using its Update method
	if err := room.Update(ctx, updates); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("room updated",
			forge.F("room_id", roomID),
			forge.F("updates", updates),
		)
	}

	return nil
}

func (m *manager) SearchRooms(ctx context.Context, query string, filters map[string]any) ([]streaming.Room, error) {
	// Get all rooms first
	allRooms, err := m.ListRooms(ctx)
	if err != nil {
		return nil, err
	}

	var results []streaming.Room

	for _, room := range allRooms {
		// Simple text search in room name and description
		roomName := room.GetName()
		roomDesc := room.GetDescription()

		// Check if query matches room name or description
		matches := false
		if query == "" {
			matches = true
		} else {
			matches = containsIgnoreCase(roomName, query) || containsIgnoreCase(roomDesc, query)
		}

		// Apply filters
		if matches && filters != nil {
			if category, ok := filters["category"]; ok {
				if room.GetCategory() != category {
					matches = false
				}
			}

			if isPrivate, ok := filters["private"]; ok {
				if room.IsPrivate() != isPrivate {
					matches = false
				}
			}

			if tags, ok := filters["tags"]; ok {
				if tagSlice, ok := tags.([]string); ok {
					roomTags := room.GetTags()
					hasMatchingTag := false

					for _, filterTag := range tagSlice {
						if slices.Contains(roomTags, filterTag) {
							hasMatchingTag = true
						}
					}

					if !hasMatchingTag {
						matches = false
					}
				}
			}
		}

		if matches {
			results = append(results, room)
		}
	}

	return results, nil
}

func (m *manager) GetPublicRooms(ctx context.Context, limit int) ([]streaming.Room, error) {
	// Get all rooms and filter for public ones
	allRooms, err := m.ListRooms(ctx)
	if err != nil {
		return nil, err
	}

	var publicRooms []streaming.Room

	for _, room := range allRooms {
		if !room.IsPrivate() {
			publicRooms = append(publicRooms, room)
			if limit > 0 && len(publicRooms) >= limit {
				break
			}
		}
	}

	return publicRooms, nil
}

func (m *manager) GetUserRoomCount(ctx context.Context, userID string) (int, error) {
	// Get user's rooms from the room store
	userRooms, err := m.roomStore.GetUserRooms(ctx, userID)
	if err != nil {
		return 0, err
	}

	return len(userRooms), nil
}

func (m *manager) ArchiveRoom(ctx context.Context, roomID string) error {
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Archive the room using its Archive method
	if err := room.Archive(ctx); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("room archived",
			forge.F("room_id", roomID),
		)
	}

	return nil
}

func (m *manager) RestoreRoom(ctx context.Context, roomID string) error {
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Restore the room using its Unarchive method
	if err := room.Unarchive(ctx); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("room restored",
			forge.F("room_id", roomID),
		)
	}

	return nil
}

func (m *manager) TransferRoomOwnership(ctx context.Context, roomID, newOwnerID string) error {
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Transfer ownership using the room's TransferOwnership method
	if err := room.TransferOwnership(ctx, newOwnerID); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("room ownership transferred",
			forge.F("room_id", roomID),
			forge.F("new_owner", newOwnerID),
		)
	}

	return nil
}

func (m *manager) UpdateChannel(ctx context.Context, channelID string, updates map[string]any) error {
	_, err := m.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}

	// For now, we'll use the channel store to update the channel
	// This is a simplified implementation - in a real system, channels might have Update methods
	// We'll need to recreate the channel with updated properties
	if m.logger != nil {
		m.logger.Info("channel update requested",
			forge.F("channel_id", channelID),
			forge.F("updates", updates),
		)
	}

	// Note: This is a placeholder implementation
	// In a real system, you'd need to implement proper channel updating
	// based on your channel store's capabilities
	return errors.New("channel updates not yet implemented")
}

func (m *manager) GetChannelSubscribers(ctx context.Context, channelID string) ([]string, error) {
	// Get subscriptions from the channel store
	subscriptions, err := m.channelStore.GetSubscriptions(ctx, channelID)
	if err != nil {
		return nil, err
	}

	// Extract connection IDs from subscriptions
	var subscriberIDs []string
	for _, sub := range subscriptions {
		subscriberIDs = append(subscriberIDs, sub.GetConnID())
	}

	return subscriberIDs, nil
}

func (m *manager) GetUserChannels(ctx context.Context, userID string) ([]streaming.Channel, error) {
	// Get user's channels from the channel store
	userChannels, err := m.channelStore.GetUserChannels(ctx, userID)
	if err != nil {
		return nil, err
	}

	return userChannels, nil
}

func (m *manager) BulkJoinRoom(ctx context.Context, connIDs []string, roomID string) error {
	var errors []error

	successCount := 0

	for _, connID := range connIDs {
		if err := m.JoinRoom(ctx, connID, roomID); err != nil {
			errors = append(errors, fmt.Errorf("failed to join room for connection %s: %w", connID, err))
		} else {
			successCount++
		}
	}

	if len(errors) > 0 {
		if m.logger != nil {
			m.logger.Error("bulk join room completed with errors",
				forge.F("room_id", roomID),
				forge.F("success_count", successCount),
				forge.F("error_count", len(errors)),
			)
		}

		return fmt.Errorf("bulk join completed with %d errors", len(errors))
	}

	if m.logger != nil {
		m.logger.Info("bulk join room completed successfully",
			forge.F("room_id", roomID),
			forge.F("connection_count", successCount),
		)
	}

	return nil
}

func (m *manager) GetPresenceForUsers(ctx context.Context, userIDs []string) ([]*streaming.UserPresence, error) {
	var presences []*streaming.UserPresence

	for _, userID := range userIDs {
		presence, err := m.GetPresence(ctx, userID)
		if err != nil {
			// If user has no presence, create a default offline presence
			presence = &streaming.UserPresence{
				UserID:   userID,
				Status:   streaming.StatusOffline,
				LastSeen: time.Now(),
			}
		}

		presences = append(presences, presence)
	}

	return presences, nil
}

func (m *manager) SetCustomStatus(ctx context.Context, userID, customStatus string) error {
	// Get current presence
	presence, err := m.GetPresence(ctx, userID)
	if err != nil {
		// Create new presence if none exists
		presence = &streaming.UserPresence{
			UserID:   userID,
			Status:   streaming.StatusOnline,
			LastSeen: time.Now(),
		}
	}

	// Update custom status
	presence.CustomStatus = customStatus

	// Set the updated presence
	return m.SetPresence(ctx, userID, presence.Status)
}

func (m *manager) GetOnlineCount(ctx context.Context) (int, error) {
	// Get all online users
	onlineUsers, err := m.GetOnlineUsers(ctx, "")
	if err != nil {
		return 0, err
	}

	return len(onlineUsers), nil
}

func (m *manager) GetPresenceInRooms(ctx context.Context, roomIDs []string) (map[string][]string, error) {
	result := make(map[string][]string)

	for _, roomID := range roomIDs {
		onlineUsers, err := m.GetOnlineUsers(ctx, roomID)
		if err != nil {
			// If we can't get users for a room, continue with empty list
			result[roomID] = []string{}

			continue
		}

		result[roomID] = onlineUsers
	}

	return result, nil
}

func (m *manager) GetTypingUsersInChannels(ctx context.Context, channelIDs []string) (map[string][]string, error) {
	result := make(map[string][]string)

	for _, channelID := range channelIDs {
		// For now, return empty list for each channel
		// In a real implementation, you'd query the typing tracker for each channel
		result[channelID] = []string{}
	}

	return result, nil
}

func (m *manager) IsTyping(ctx context.Context, userID, roomID string) (bool, error) {
	// Get typing users for the room
	typingUsers, err := m.GetTypingUsers(ctx, roomID)
	if err != nil {
		return false, err
	}

	// Check if user is in the typing list
	if slices.Contains(typingUsers, userID) {
		return true, nil
	}

	return false, nil
}

func (m *manager) ClearTyping(ctx context.Context, userID string) error {
	// Stop typing for all rooms the user might be typing in
	// This is a simplified implementation - in reality you'd need to track which rooms the user is typing in
	return m.typingTracker.StopTyping(ctx, userID, "")
}

func (m *manager) GetThreadHistory(ctx context.Context, roomID, threadID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	// For now, return empty list
	// In a real implementation, you'd query the message store for thread-specific messages
	return []*streaming.Message{}, nil
}

func (m *manager) GetUserMessages(ctx context.Context, userID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	// For now, return empty list
	// In a real implementation, you'd query the message store for user-specific messages
	return []*streaming.Message{}, nil
}

func (m *manager) SearchMessages(ctx context.Context, roomID, searchTerm string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	// For now, return empty list
	// In a real implementation, you'd query the message store with search terms
	return []*streaming.Message{}, nil
}

func (m *manager) DeleteMessage(ctx context.Context, messageID string) error {
	// For now, return not implemented
	// In a real implementation, you'd delete the message from the message store
	return errors.New("message deletion not yet implemented")
}

func (m *manager) EditMessage(ctx context.Context, messageID string, newContent any) error {
	// For now, return not implemented
	// In a real implementation, you'd update the message in the message store
	return errors.New("message editing not yet implemented")
}

func (m *manager) AddReaction(ctx context.Context, messageID, userID, emoji string) error {
	// For now, return not implemented
	// In a real implementation, you'd add the reaction to the message store
	return errors.New("reactions not yet implemented")
}

func (m *manager) RemoveReaction(ctx context.Context, messageID, userID, emoji string) error {
	// For now, return not implemented
	// In a real implementation, you'd remove the reaction from the message store
	return errors.New("reactions not yet implemented")
}

func (m *manager) GetReactions(ctx context.Context, messageID string) (map[string][]string, error) {
	// For now, return empty map
	// In a real implementation, you'd query the message store for reactions
	return map[string][]string{}, nil
}

func (m *manager) MuteUser(ctx context.Context, userID, roomID string, duration time.Duration) error {
	// Get the room to access moderation methods
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Use the room's mute functionality
	if err := room.MuteMember(ctx, userID, duration); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("user muted",
			forge.F("user_id", userID),
			forge.F("room_id", roomID),
			forge.F("duration", duration),
		)
	}

	return nil
}

func (m *manager) UnmuteUser(ctx context.Context, userID, roomID string) error {
	// Get the room to access moderation methods
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Use the room's unmute functionality
	if err := room.UnmuteMember(ctx, userID); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("user unmuted",
			forge.F("user_id", userID),
			forge.F("room_id", roomID),
		)
	}

	return nil
}

func (m *manager) BanUser(ctx context.Context, userID, roomID string, reason string, until *time.Time) error {
	// Get the room to access moderation methods
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Use the room's ban functionality
	if err := room.BanMember(ctx, userID, reason, until); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("user banned",
			forge.F("user_id", userID),
			forge.F("room_id", roomID),
			forge.F("reason", reason),
			forge.F("until", until),
		)
	}

	return nil
}

func (m *manager) UnbanUser(ctx context.Context, userID, roomID string) error {
	// Get the room to access moderation methods
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return err
	}

	// Use the room's unban functionality
	if err := room.UnbanMember(ctx, userID); err != nil {
		return err
	}

	if m.logger != nil {
		m.logger.Info("user unbanned",
			forge.F("user_id", userID),
			forge.F("room_id", roomID),
		)
	}

	return nil
}

func (m *manager) GetModerationLog(ctx context.Context, roomID string, limit int) ([]streaming.ModerationEvent, error) {
	// Get the room to access moderation methods
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return nil, err
	}

	// Use the room's moderation log functionality
	return room.GetModerationLog(ctx, limit)
}

func (m *manager) CheckRateLimit(ctx context.Context, userID string, action string) (bool, error) {
	// For now, return true (allowed) for all actions
	// In a real implementation, you'd check against rate limiting rules
	// This would typically involve checking user's recent activity against configured limits
	if m.logger != nil {
		m.logger.Debug("rate limit check",
			forge.F("user_id", userID),
			forge.F("action", action),
			forge.F("allowed", true),
		)
	}

	return true, nil
}

func (m *manager) GetRateLimitStatus(ctx context.Context, userID string) (*streaming.RateLimitStatus, error) {
	// For now, return a default status indicating no limits
	// In a real implementation, you'd calculate actual rate limit status
	status := &streaming.RateLimitStatus{
		Allowed:    true,
		Remaining:  1000, // Default high limit
		ResetAt:    time.Now().Add(time.Hour),
		RetryAfter: 0,
	}

	return status, nil
}

func (m *manager) GetStats(ctx context.Context) (*streaming.ManagerStats, error) {
	// Get basic statistics
	connectionCount := m.ConnectionCount()

	// Get room and channel counts
	rooms, err := m.ListRooms(ctx)
	if err != nil {
		return nil, err
	}

	channels, err := m.ListChannels(ctx)
	if err != nil {
		return nil, err
	}

	// Get online user count
	onlineCount, err := m.GetOnlineCount(ctx)
	if err != nil {
		onlineCount = 0 // Default to 0 if we can't get the count
	}

	stats := &streaming.ManagerStats{
		TotalConnections: connectionCount,
		TotalRooms:       len(rooms),
		TotalChannels:    len(channels),
		TotalMessages:    0, // Would need to query message store
		OnlineUsers:      onlineCount,
		MessagesPerSec:   0.0,                    // Would need to track over time
		Uptime:           time.Since(time.Now()), // Placeholder
		MemoryUsage:      0,                      // Would need to get from runtime
	}

	return stats, nil
}

func (m *manager) GetRoomStats(ctx context.Context, roomID string) (*streaming.RoomStats, error) {
	// Get the room
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return nil, err
	}

	// Get room members
	members, err := m.GetRoomMembers(ctx, roomID)
	if err != nil {
		return nil, err
	}

	// Get message count from room
	messageCount, err := room.GetMessageCount(ctx)
	if err != nil {
		messageCount = 0 // Default to 0 if we can't get the count
	}

	// Get active members (simplified - members active in last hour)
	activeMembers, err := room.GetActiveMembers(ctx, time.Hour)
	if err != nil {
		activeMembers = members // Fallback to all members
	}

	stats := &streaming.RoomStats{
		TotalMessages:   messageCount,
		TotalMembers:    len(members),
		ActiveMembers:   len(activeMembers),
		MessagesToday:   0,            // Would need to query with date filter
		AverageMessages: 0.0,          // Would need to calculate over time
		PeakOnline:      len(members), // Simplified
		CreatedAt:       room.GetCreated(),
		LastActivity:    room.GetUpdated(),
	}

	return stats, nil
}

func (m *manager) GetUserStats(ctx context.Context, userID string) (*streaming.UserStats, error) {
	// Get user's room count
	roomCount, err := m.GetUserRoomCount(ctx, userID)
	if err != nil {
		roomCount = 0 // Default to 0 if we can't get the count
	}

	// Get user's presence for last seen
	presence, err := m.GetPresence(ctx, userID)

	var lastSeen time.Time
	if err != nil {
		lastSeen = time.Now() // Default to now if no presence
	} else {
		lastSeen = presence.LastSeen
	}

	stats := &streaming.UserStats{
		MessagesSent:    0, // Would need to query message store
		RoomsJoined:     roomCount,
		OnlineTime:      0, // Would need to track over time
		LastSeen:        lastSeen,
		AverageActivity: 0.0, // Would need to calculate over time
	}

	return stats, nil
}

func (m *manager) GetActiveRooms(ctx context.Context, since time.Duration) ([]streaming.Room, error) {
	// Get all rooms
	allRooms, err := m.ListRooms(ctx)
	if err != nil {
		return nil, err
	}

	var activeRooms []streaming.Room

	cutoff := time.Now().Add(-since)

	for _, room := range allRooms {
		// Check if room has been active since the cutoff time
		if room.GetUpdated().After(cutoff) {
			activeRooms = append(activeRooms, room)
		}
	}

	return activeRooms, nil
}

func (m *manager) CreateDirectMessage(ctx context.Context, fromUserID, toUserID string) (string, error) {
	// Create a direct message room
	// For simplicity, we'll create a room with a special naming convention
	roomID := fmt.Sprintf("dm_%s_%s", fromUserID, toUserID)

	// Ensure consistent ordering for bidirectional DMs
	if fromUserID > toUserID {
		roomID = fmt.Sprintf("dm_%s_%s", toUserID, fromUserID)
	}

	// Check if DM room already exists
	_, err := m.GetRoom(ctx, roomID)
	if err == nil {
		// Room already exists, return the ID
		return roomID, nil
	}

	// For now, return a placeholder implementation
	// In a real implementation, you'd create the room using the room store
	// and handle the room creation properly

	if m.logger != nil {
		m.logger.Info("direct message room creation requested",
			forge.F("room_id", roomID),
			forge.F("from_user", fromUserID),
			forge.F("to_user", toUserID),
		)
	}

	// Return the room ID (in a real implementation, you'd create the room first)
	return roomID, errors.New("direct message creation not yet fully implemented")
}

func (m *manager) GetDirectMessages(ctx context.Context, userID string) ([]streaming.Room, error) {
	// Get user's rooms and filter for direct messages
	userRooms, err := m.roomStore.GetUserRooms(ctx, userID)
	if err != nil {
		return nil, err
	}

	var dmRooms []streaming.Room

	for _, room := range userRooms {
		// Check if room is a direct message based on metadata
		if metadata := room.GetMetadata(); metadata != nil {
			if roomType, ok := metadata["type"]; ok && roomType == "direct_message" {
				dmRooms = append(dmRooms, room)
			}
		}
	}

	return dmRooms, nil
}

func (m *manager) IsDirectMessage(ctx context.Context, roomID string) (bool, error) {
	// Get the room
	room, err := m.GetRoom(ctx, roomID)
	if err != nil {
		return false, err
	}

	// Check if room is a direct message based on metadata
	if metadata := room.GetMetadata(); metadata != nil {
		if roomType, ok := metadata["type"]; ok && roomType == "direct_message" {
			return true, nil
		}
	}

	return false, nil
}

// Helper functions

func removeFromSlice(slice []string, value string) []string {
	for i, v := range slice {
		if v == value {
			return append(slice[:i], slice[i+1:]...)
		}
	}

	return slice
}

// containsIgnoreCase performs case-insensitive string matching.
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) &&
		strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

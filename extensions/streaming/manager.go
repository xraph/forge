package streaming

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
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
		return fmt.Errorf("rooms are disabled")
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
		return fmt.Errorf("connection has no user ID")
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
		return fmt.Errorf("channels are disabled")
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
		return fmt.Errorf("channel limit reached")
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
		return fmt.Errorf("presence tracking is disabled")
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
		return fmt.Errorf("typing indicators are disabled")
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
	// TODO implement me
	panic("implement me")
}

func (m *manager) KickConnection(ctx context.Context, connID string, reason string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetConnectionInfo(connID string) (*streaming.ConnectionInfo, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetIdleConnections(idleFor time.Duration) []streaming.EnhancedConnection {
	// TODO implement me
	panic("implement me")
}

func (m *manager) CleanupIdleConnections(ctx context.Context, idleFor time.Duration) (int, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) UpdateRoom(ctx context.Context, roomID string, updates map[string]any) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) SearchRooms(ctx context.Context, query string, filters map[string]any) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetPublicRooms(ctx context.Context, limit int) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetUserRoomCount(ctx context.Context, userID string) (int, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) ArchiveRoom(ctx context.Context, roomID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) RestoreRoom(ctx context.Context, roomID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) TransferRoomOwnership(ctx context.Context, roomID, newOwnerID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) UpdateChannel(ctx context.Context, channelID string, updates map[string]any) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetChannelSubscribers(ctx context.Context, channelID string) ([]string, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetUserChannels(ctx context.Context, userID string) ([]streaming.Channel, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) BulkJoinRoom(ctx context.Context, connIDs []string, roomID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetPresenceForUsers(ctx context.Context, userIDs []string) ([]*streaming.UserPresence, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) SetCustomStatus(ctx context.Context, userID, customStatus string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetOnlineCount(ctx context.Context) (int, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetPresenceInRooms(ctx context.Context, roomIDs []string) (map[string][]string, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetTypingUsersInChannels(ctx context.Context, channelIDs []string) (map[string][]string, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) IsTyping(ctx context.Context, userID, roomID string) (bool, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) ClearTyping(ctx context.Context, userID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetThreadHistory(ctx context.Context, roomID, threadID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetUserMessages(ctx context.Context, userID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) SearchMessages(ctx context.Context, roomID, searchTerm string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) DeleteMessage(ctx context.Context, messageID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) EditMessage(ctx context.Context, messageID string, newContent any) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) AddReaction(ctx context.Context, messageID, userID, emoji string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) RemoveReaction(ctx context.Context, messageID, userID, emoji string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetReactions(ctx context.Context, messageID string) (map[string][]string, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) MuteUser(ctx context.Context, userID, roomID string, duration time.Duration) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) UnmuteUser(ctx context.Context, userID, roomID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) BanUser(ctx context.Context, userID, roomID string, reason string, until *time.Time) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) UnbanUser(ctx context.Context, userID, roomID string) error {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetModerationLog(ctx context.Context, roomID string, limit int) ([]streaming.ModerationEvent, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) CheckRateLimit(ctx context.Context, userID string, action string) (bool, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetRateLimitStatus(ctx context.Context, userID string) (*streaming.RateLimitStatus, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetStats(ctx context.Context) (*streaming.ManagerStats, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetRoomStats(ctx context.Context, roomID string) (*streaming.RoomStats, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetUserStats(ctx context.Context, userID string) (*streaming.UserStats, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetActiveRooms(ctx context.Context, since time.Duration) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) CreateDirectMessage(ctx context.Context, fromUserID, toUserID string) (string, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) GetDirectMessages(ctx context.Context, userID string) ([]streaming.Room, error) {
	// TODO implement me
	panic("implement me")
}

func (m *manager) IsDirectMessage(ctx context.Context, roomID string) (bool, error) {
	// TODO implement me
	panic("implement me")
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

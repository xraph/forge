package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	"github.com/xraph/forge/extensions/streaming/coordinator"
	"github.com/xraph/forge/extensions/streaming/filters"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/extensions/streaming/lb"
	"github.com/xraph/forge/extensions/streaming/ratelimit"
	"github.com/xraph/forge/extensions/streaming/validation"
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

	// Coordinator for cross-node messaging (optional)
	coordinator coordinator.StreamCoordinator

	// Message pipeline (optional)
	filterChain filters.FilterChain
	validator   validation.MessageValidator
	rateLimiter ratelimit.RateLimiter

	// Load balancer (optional, distributed mode)
	loadBalancer  lb.LoadBalancer
	healthChecker lb.HealthChecker

	// Session resumption (optional)
	sessionStore SessionStore

	// Message deduplication for distributed mode
	dedup *messageDedup

	// Connection registry
	connections map[string]Connection // connID -> connection
	userConns   map[string][]string   // userID -> []connID

	// Configuration
	config Config
	nodeID string

	// Logger and metrics
	logger  forge.Logger
	metrics forge.Metrics

	// Lifecycle
	started bool
}

// ManagerOption configures the manager.
type ManagerOption func(*manager)

// WithCoordinator sets the distributed coordinator.
func WithCoordinator(c coordinator.StreamCoordinator) ManagerOption {
	return func(m *manager) { m.coordinator = c }
}

// WithFilterChain sets the message filter chain.
func WithFilterChain(fc filters.FilterChain) ManagerOption {
	return func(m *manager) { m.filterChain = fc }
}

// WithValidator sets the message validator.
func WithValidator(v validation.MessageValidator) ManagerOption {
	return func(m *manager) { m.validator = v }
}

// WithRateLimiter sets the rate limiter.
func WithRateLimiter(rl ratelimit.RateLimiter) ManagerOption {
	return func(m *manager) { m.rateLimiter = rl }
}

// WithManagerLoadBalancer sets the load balancer.
func WithManagerLoadBalancer(l lb.LoadBalancer) ManagerOption {
	return func(m *manager) { m.loadBalancer = l }
}

// WithManagerHealthChecker sets the health checker.
func WithManagerHealthChecker(hc lb.HealthChecker) ManagerOption {
	return func(m *manager) { m.healthChecker = hc }
}

// WithSessionStore sets the session store for session resumption.
func WithSessionStore(ss SessionStore) ManagerOption {
	return func(m *manager) { m.sessionStore = ss }
}

// WithNodeID sets the node ID for distributed mode.
func WithManagerNodeID(id string) ManagerOption {
	return func(m *manager) { m.nodeID = id }
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
	opts ...ManagerOption,
) Manager {
	m := &manager{
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
	for _, opt := range opts {
		opt(m)
	}
	return m
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

	// Update load balancer connection count
	if m.loadBalancer != nil && m.nodeID != "" {
		if updater, ok := m.loadBalancer.(interface{ ReleaseConnection(string) }); ok {
			_ = updater // connection count incremented via SelectNode
		}
	}

	// Track user on this node for coordinator
	if m.coordinator != nil && userID != "" && m.nodeID != "" {
		if tracker, ok := m.coordinator.(interface {
			TrackUserNode(ctx context.Context, userID, nodeID string) error
		}); ok {
			_ = tracker.TrackUserNode(context.Background(), userID, m.nodeID)
		}
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

	// Save session snapshot for resumption before cleanup
	if m.sessionStore != nil && m.config.EnableSessionResumption {
		sessionID := conn.GetSessionID()
		if sessionID != "" {
			snapshot := &SessionSnapshot{
				SessionID:      sessionID,
				UserID:         userID,
				Rooms:          conn.GetJoinedRooms(),
				Channels:       conn.GetSubscriptions(),
				DisconnectedAt: time.Now(),
			}
			if err := m.sessionStore.Save(context.Background(), snapshot, m.config.SessionResumptionTTL); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to save session snapshot",
						forge.F("session_id", sessionID),
						forge.F("error", err),
					)
				}
			} else if m.logger != nil {
				m.logger.Debug("session snapshot saved for resumption",
					forge.F("session_id", sessionID),
					forge.F("ttl", m.config.SessionResumptionTTL),
				)
			}
		}
	}

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

	// Update load balancer connection count
	if m.loadBalancer != nil && m.nodeID != "" {
		if releaser, ok := m.loadBalancer.(interface{ ReleaseConnection(string) }); ok {
			releaser.ReleaseConnection(m.nodeID)
		}
	}

	// Untrack user from this node if no more connections
	if m.coordinator != nil && userID != "" && m.nodeID != "" {
		if _, stillHasConns := m.userConns[userID]; !stillHasConns {
			if tracker, ok := m.coordinator.(interface {
				UntrackUserNode(ctx context.Context, userID, nodeID string) error
			}); ok {
				_ = tracker.UntrackUserNode(context.Background(), userID, m.nodeID)
			}
		}
	}

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

// ResumeSession restores room and channel state from a previous session.
// Returns true if session was found and restored, false otherwise.
func (m *manager) ResumeSession(ctx context.Context, connID, sessionID string) (bool, error) {
	if m.sessionStore == nil || !m.config.EnableSessionResumption || sessionID == "" {
		return false, nil
	}

	snapshot, err := m.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return false, nil // Session not found or expired — not an error
	}

	// Restore room memberships
	for _, roomID := range snapshot.Rooms {
		if err := m.JoinRoom(ctx, connID, roomID); err != nil {
			if m.logger != nil {
				m.logger.Debug("session resume: failed to rejoin room",
					forge.F("session_id", sessionID),
					forge.F("room_id", roomID),
					forge.F("error", err),
				)
			}
		}
	}

	// Restore channel subscriptions
	for _, channelID := range snapshot.Channels {
		if err := m.Subscribe(ctx, connID, channelID, nil); err != nil {
			if m.logger != nil {
				m.logger.Debug("session resume: failed to resubscribe to channel",
					forge.F("session_id", sessionID),
					forge.F("channel_id", channelID),
					forge.F("error", err),
				)
			}
		}
	}

	// Delete the snapshot so it can't be reused
	_ = m.sessionStore.Delete(ctx, sessionID)

	if m.logger != nil {
		m.logger.Info("session resumed",
			forge.F("session_id", sessionID),
			forge.F("conn_id", connID),
			forge.F("rooms", len(snapshot.Rooms)),
			forge.F("channels", len(snapshot.Channels)),
		)
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.sessions.resumed").Inc()
	}

	return true, nil
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

// Message pipeline helpers

// processMessage runs a message through rate limiting, validation, and filters.
// Returns the (possibly modified) message, or nil if it should be dropped.
func (m *manager) processMessage(ctx context.Context, message *Message, sender Connection) (*Message, error) {
	// 1. Rate limiting
	if m.rateLimiter != nil && sender != nil {
		userID := sender.GetUserID()
		if userID != "" {
			allowed, err := m.rateLimiter.Allow(ctx, userID, "message")
			if err != nil {
				if m.logger != nil {
					m.logger.Error("rate limiter error", forge.F("error", err))
				}
			} else if !allowed {
				if m.metrics != nil {
					m.metrics.Counter("streaming.messages.rate_limited").Inc()
				}
				return nil, errors.New("rate limit exceeded")
			}
		}
	}

	// 2. Validation
	if m.validator != nil && sender != nil {
		if err := m.validator.Validate(ctx, message, sender); err != nil {
			if m.metrics != nil {
				m.metrics.Counter("streaming.messages.validation_failed").Inc()
			}
			return nil, fmt.Errorf("message validation failed: %w", err)
		}
	}

	return message, nil
}

// deliverToConnection delivers a message to a single connection, applying per-recipient filters.
func (m *manager) deliverToConnection(ctx context.Context, conn Connection, message *Message) error {
	msg := message
	if m.filterChain != nil {
		filtered, err := m.filterChain.Apply(ctx, msg, conn)
		if err != nil {
			return err
		}
		if filtered == nil {
			return nil // message blocked by filter for this recipient
		}
		msg = filtered
	}
	return conn.WriteJSON(msg)
}

// coordinatorBroadcast sends a message to other nodes via the coordinator.
// It tags the message with the local nodeID so remote nodes can deduplicate.
func (m *manager) coordinatorBroadcast(ctx context.Context, broadcastType string, targetID string, message *Message) {
	if m.coordinator == nil {
		return
	}
	// Tag message with originating node
	if message.Metadata == nil {
		message.Metadata = make(map[string]any)
	}
	message.Metadata["_origin_node"] = m.nodeID

	var err error
	switch broadcastType {
	case "global":
		err = m.coordinator.BroadcastGlobal(ctx, message)
	case "room":
		err = m.coordinator.BroadcastToRoom(ctx, targetID, message)
	case "user":
		err = m.coordinator.BroadcastToUser(ctx, targetID, message)
	}
	if err != nil && m.logger != nil {
		m.logger.Error("coordinator broadcast failed",
			forge.F("type", broadcastType),
			forge.F("target", targetID),
			forge.F("error", err),
		)
	}
}

// Message broadcasting

func (m *manager) Broadcast(ctx context.Context, message *Message) error {
	conns := m.GetAllConnections()

	for _, conn := range conns {
		if err := m.deliverToConnection(ctx, conn, message); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to broadcast message",
					forge.F("conn_id", conn.ID()),
					forge.F("error", err),
				)
			}
		}
	}

	// Relay to other nodes
	m.coordinatorBroadcast(ctx, "global", "", message)

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
			if err := m.deliverToConnection(ctx, conn, message); err != nil {
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

	// Relay to other nodes
	m.coordinatorBroadcast(ctx, "room", roomID, message)

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
			if err := m.deliverToConnection(ctx, conn, message); err != nil {
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
		if err := m.deliverToConnection(ctx, conn, message); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to send user message",
					forge.F("conn_id", conn.ID()),
					forge.F("user_id", userID),
					forge.F("error", err),
				)
			}
		}
	}

	// Relay to other nodes (user may be connected on other nodes too)
	m.coordinatorBroadcast(ctx, "user", userID, message)

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

	if err := m.deliverToConnection(ctx, conn, message); err != nil {
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
			if err := m.deliverToConnection(ctx, conn, message); err != nil {
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

	// Initialize message deduplication for distributed mode
	if m.coordinator != nil {
		m.dedup = newMessageDedup(10000, 30*time.Second)
	}

	// Start coordinator if enabled
	if m.coordinator != nil {
		// Subscribe to receive messages from other nodes
		if err := m.coordinator.Subscribe(ctx, m.handleCoordinatorMessage); err != nil {
			return fmt.Errorf("failed to subscribe to coordinator: %w", err)
		}
		if err := m.coordinator.Start(ctx); err != nil {
			return fmt.Errorf("failed to start coordinator: %w", err)
		}
		// Register this node
		if m.nodeID != "" {
			if err := m.coordinator.RegisterNode(ctx, m.nodeID, map[string]any{
				"started_at": time.Now().Unix(),
			}); err != nil {
				if m.logger != nil {
					m.logger.Error("failed to register node with coordinator", forge.F("error", err))
				}
			}
		}
	}

	// Start load balancer and register local node
	if m.loadBalancer != nil && m.nodeID != "" {
		localNode := &lb.NodeInfo{
			ID:      m.nodeID,
			Healthy: true,
			Weight:  100,
			Metadata: map[string]any{
				"started_at": time.Now().Unix(),
			},
		}
		if err := m.loadBalancer.RegisterNode(ctx, localNode); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to register node with load balancer", forge.F("error", err))
			}
		}
		// Start health checker if configured
		if m.healthChecker != nil {
			m.healthChecker.RegisterNode(localNode)
			go m.healthChecker.Start(ctx)
		}
		if m.logger != nil {
			m.logger.Info("load balancer started",
				forge.F("node_id", m.nodeID),
				forge.F("strategy", m.config.LoadBalancerStrategy),
			)
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

	// Stop load balancer and health checker
	if m.healthChecker != nil {
		m.healthChecker.Stop(ctx)
	}
	if m.loadBalancer != nil && m.nodeID != "" {
		_ = m.loadBalancer.UnregisterNode(ctx, m.nodeID)
	}

	// Stop coordinator
	if m.coordinator != nil {
		if m.nodeID != "" {
			_ = m.coordinator.UnregisterNode(ctx, m.nodeID)
		}
		_ = m.coordinator.Stop(ctx)
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
		var matches bool
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
	channel, err := m.GetChannel(ctx, channelID)
	if err != nil {
		return err
	}

	// Apply updates via channel's Update method if available
	if updatable, ok := channel.(interface {
		Update(ctx context.Context, updates map[string]any) error
	}); ok {
		if err := updatable.Update(ctx, updates); err != nil {
			return err
		}
	}

	if m.logger != nil {
		m.logger.Info("channel updated",
			forge.F("channel_id", channelID),
			forge.F("updates", updates),
		)
	}

	return nil
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
		users, err := m.typingTracker.GetTypingUsers(ctx, channelID)
		if err != nil {
			result[channelID] = []string{}
			continue
		}
		result[channelID] = users
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
	if !m.config.EnableMessageHistory {
		return nil, errors.New("message history is disabled")
	}
	return m.messageStore.GetThreadHistory(ctx, roomID, threadID, query)
}

func (m *manager) GetUserMessages(ctx context.Context, userID string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	if !m.config.EnableMessageHistory {
		return nil, errors.New("message history is disabled")
	}
	return m.messageStore.GetUserMessages(ctx, userID, query)
}

func (m *manager) SearchMessages(ctx context.Context, roomID, searchTerm string, query streaming.HistoryQuery) ([]*streaming.Message, error) {
	if !m.config.EnableMessageHistory {
		return nil, errors.New("message history is disabled")
	}
	return m.messageStore.Search(ctx, roomID, searchTerm, query)
}

func (m *manager) DeleteMessage(ctx context.Context, messageID string) error {
	if !m.config.EnableMessageHistory {
		return errors.New("message history is disabled")
	}

	// Get the message first to know which room to notify
	msg, err := m.messageStore.Get(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to get message for deletion: %w", err)
	}

	if err := m.messageStore.Delete(ctx, messageID); err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	// Broadcast deletion event to the room
	if msg.RoomID != "" {
		deleteEvent := &streaming.Message{
			ID:        fmt.Sprintf("del_%d", time.Now().UnixNano()),
			Type:      streaming.MessageTypeSystem,
			Event:     "message.deleted",
			RoomID:    msg.RoomID,
			UserID:    "system",
			Data:      map[string]any{"message_id": messageID},
			Timestamp: time.Now(),
		}
		_ = m.BroadcastToRoom(ctx, msg.RoomID, deleteEvent)
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.deleted").Inc()
	}

	return nil
}

func (m *manager) EditMessage(ctx context.Context, messageID string, newContent any) error {
	if !m.config.EnableMessageHistory {
		return errors.New("message history is disabled")
	}

	// Get existing message
	msg, err := m.messageStore.Get(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to get message for editing: %w", err)
	}

	// Update the content
	msg.Data = newContent
	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}
	msg.Metadata["edited"] = true
	msg.Metadata["edited_at"] = time.Now()

	// Save updated message
	if err := m.messageStore.Save(ctx, msg); err != nil {
		return fmt.Errorf("failed to save edited message: %w", err)
	}

	// Broadcast edit event to the room
	if msg.RoomID != "" {
		editEvent := &streaming.Message{
			ID:     fmt.Sprintf("edit_%d", time.Now().UnixNano()),
			Type:   streaming.MessageTypeSystem,
			Event:  "message.edited",
			RoomID: msg.RoomID,
			UserID: "system",
			Data: map[string]any{
				"message_id":  messageID,
				"new_content": newContent,
			},
			Timestamp: time.Now(),
		}
		_ = m.BroadcastToRoom(ctx, msg.RoomID, editEvent)
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.edited").Inc()
	}

	return nil
}

func (m *manager) AddReaction(ctx context.Context, messageID, userID, emoji string) error {
	if !m.config.EnableMessageHistory {
		return errors.New("message history is disabled")
	}

	msg, err := m.messageStore.Get(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to get message for reaction: %w", err)
	}

	if msg.Metadata == nil {
		msg.Metadata = make(map[string]any)
	}

	// Get or create reactions map
	reactions, _ := msg.Metadata["reactions"].(map[string]any)
	if reactions == nil {
		reactions = make(map[string]any)
	}

	// Get or create user list for this emoji
	users, _ := reactions[emoji].([]any)

	// Check if user already reacted
	for _, u := range users {
		if uStr, ok := u.(string); ok && uStr == userID {
			return nil // Already reacted
		}
	}

	users = append(users, userID)
	reactions[emoji] = users
	msg.Metadata["reactions"] = reactions

	if err := m.messageStore.Save(ctx, msg); err != nil {
		return fmt.Errorf("failed to save reaction: %w", err)
	}

	// Broadcast reaction event
	if msg.RoomID != "" {
		reactionEvent := &streaming.Message{
			ID:     fmt.Sprintf("react_%d", time.Now().UnixNano()),
			Type:   streaming.MessageTypeSystem,
			Event:  "message.reaction.added",
			RoomID: msg.RoomID,
			UserID: userID,
			Data: map[string]any{
				"message_id": messageID,
				"emoji":      emoji,
				"user_id":    userID,
			},
			Timestamp: time.Now(),
		}
		_ = m.BroadcastToRoom(ctx, msg.RoomID, reactionEvent)
	}

	return nil
}

func (m *manager) RemoveReaction(ctx context.Context, messageID, userID, emoji string) error {
	if !m.config.EnableMessageHistory {
		return errors.New("message history is disabled")
	}

	msg, err := m.messageStore.Get(ctx, messageID)
	if err != nil {
		return fmt.Errorf("failed to get message for reaction removal: %w", err)
	}

	if msg.Metadata == nil {
		return nil
	}

	reactions, _ := msg.Metadata["reactions"].(map[string]any)
	if reactions == nil {
		return nil
	}

	users, _ := reactions[emoji].([]any)
	if len(users) == 0 {
		return nil
	}

	// Remove user from reaction list
	filtered := make([]any, 0, len(users))
	for _, u := range users {
		if uStr, ok := u.(string); ok && uStr != userID {
			filtered = append(filtered, u)
		}
	}

	if len(filtered) == 0 {
		delete(reactions, emoji)
	} else {
		reactions[emoji] = filtered
	}
	msg.Metadata["reactions"] = reactions

	if err := m.messageStore.Save(ctx, msg); err != nil {
		return fmt.Errorf("failed to save reaction removal: %w", err)
	}

	// Broadcast reaction removed event
	if msg.RoomID != "" {
		reactionEvent := &streaming.Message{
			ID:     fmt.Sprintf("unreact_%d", time.Now().UnixNano()),
			Type:   streaming.MessageTypeSystem,
			Event:  "message.reaction.removed",
			RoomID: msg.RoomID,
			UserID: userID,
			Data: map[string]any{
				"message_id": messageID,
				"emoji":      emoji,
				"user_id":    userID,
			},
			Timestamp: time.Now(),
		}
		_ = m.BroadcastToRoom(ctx, msg.RoomID, reactionEvent)
	}

	return nil
}

func (m *manager) GetReactions(ctx context.Context, messageID string) (map[string][]string, error) {
	if !m.config.EnableMessageHistory {
		return nil, errors.New("message history is disabled")
	}

	msg, err := m.messageStore.Get(ctx, messageID)
	if err != nil {
		return nil, fmt.Errorf("failed to get message for reactions: %w", err)
	}

	result := make(map[string][]string)
	if msg.Metadata == nil {
		return result, nil
	}

	reactions, _ := msg.Metadata["reactions"].(map[string]any)
	if reactions == nil {
		return result, nil
	}

	for emoji, usersRaw := range reactions {
		users, _ := usersRaw.([]any)
		userStrs := make([]string, 0, len(users))
		for _, u := range users {
			if uStr, ok := u.(string); ok {
				userStrs = append(userStrs, uStr)
			}
		}
		result[emoji] = userStrs
	}

	return result, nil
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
	if m.rateLimiter != nil {
		return m.rateLimiter.Allow(ctx, userID, action)
	}

	return true, nil
}

func (m *manager) GetRateLimitStatus(ctx context.Context, userID string) (*streaming.RateLimitStatus, error) {
	if m.rateLimiter != nil {
		status, err := m.rateLimiter.GetStatus(ctx, userID, "message")
		if err != nil {
			return nil, err
		}
		return &streaming.RateLimitStatus{
			Allowed:    status.Allowed,
			Remaining:  status.Remaining,
			ResetAt:    status.ResetAt,
			RetryAfter: status.RetryIn,
		}, nil
	}

	return &streaming.RateLimitStatus{
		Allowed:    true,
		Remaining:  1000,
		ResetAt:    time.Now().Add(time.Hour),
		RetryAfter: 0,
	}, nil
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
	if !m.config.EnableRooms {
		return "", errors.New("rooms are disabled")
	}

	// Ensure consistent ordering for bidirectional DMs
	user1, user2 := fromUserID, toUserID
	if user1 > user2 {
		user1, user2 = user2, user1
	}
	roomID := fmt.Sprintf("dm_%s_%s", user1, user2)

	// Check if DM room already exists
	_, err := m.GetRoom(ctx, roomID)
	if err == nil {
		return roomID, nil
	}

	// Create a new private DM room using RoomOptions
	roomOpts := streaming.RoomOptions{
		ID:      roomID,
		Name:    fmt.Sprintf("DM: %s & %s", fromUserID, toUserID),
		Owner:   fromUserID,
		Private: true,
		Metadata: map[string]any{
			"type":    "direct_message",
			"members": []string{fromUserID, toUserID},
		},
	}

	if err := m.CreateRoom(ctx, NewLocalRoom(roomOpts)); err != nil {
		return "", fmt.Errorf("failed to create DM room: %w", err)
	}

	if m.logger != nil {
		m.logger.Info("direct message room created",
			forge.F("room_id", roomID),
			forge.F("from_user", fromUserID),
			forge.F("to_user", toUserID),
		)
	}

	return roomID, nil
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

// Coordinator message handler — receives messages from other nodes and delivers locally.
func (m *manager) handleCoordinatorMessage(ctx context.Context, msg *coordinator.CoordinatorMessage) error {
	if msg == nil {
		return nil
	}

	// Handle node lifecycle events for load balancer
	switch msg.Type {
	case coordinator.MessageTypeNodeRegister:
		m.handleNodeRegister(ctx, msg)
		return nil
	case coordinator.MessageTypeNodeUnregister:
		m.handleNodeUnregister(ctx, msg)
		return nil
	}

	if msg.Payload == nil {
		return nil
	}

	// Extract the streaming message from payload
	var streamMsg *streaming.Message

	switch payload := msg.Payload.(type) {
	case *streaming.Message:
		streamMsg = payload
	case map[string]any:
		// Coordinator deserializes JSON into map — re-marshal and unmarshal
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		streamMsg = &streaming.Message{}
		if err := json.Unmarshal(data, streamMsg); err != nil {
			return err
		}
	default:
		return nil
	}

	// Check if this message originated from us (node-level dedup)
	if streamMsg.Metadata != nil {
		if origin, ok := streamMsg.Metadata["_origin_node"].(string); ok && origin == m.nodeID {
			return nil
		}
	}

	// Check message-ID-level dedup (same message may arrive via multiple paths)
	if m.dedup != nil && streamMsg.ID != "" {
		if m.dedup.IsDuplicate(streamMsg.ID) {
			return nil
		}
	}

	// Deliver to local connections based on message type
	switch msg.Type {
	case coordinator.MessageTypeBroadcast:
		if msg.RoomID != "" {
			// Room broadcast — deliver to local room members only
			conns := m.GetAllConnections()
			for _, conn := range conns {
				if conn.IsInRoom(msg.RoomID) {
					_ = m.deliverToConnection(ctx, conn, streamMsg)
				}
			}
		} else if msg.UserID != "" {
			// User-targeted — deliver to local connections for this user
			conns := m.GetUserConnections(msg.UserID)
			for _, conn := range conns {
				_ = m.deliverToConnection(ctx, conn, streamMsg)
			}
		} else {
			// Global broadcast — deliver to all local connections
			conns := m.GetAllConnections()
			for _, conn := range conns {
				_ = m.deliverToConnection(ctx, conn, streamMsg)
			}
		}
	}

	return nil
}

// handleNodeRegister registers a remote node with the load balancer when it joins the cluster.
func (m *manager) handleNodeRegister(ctx context.Context, msg *coordinator.CoordinatorMessage) {
	if m.loadBalancer == nil || msg.NodeID == "" || msg.NodeID == m.nodeID {
		return
	}

	node := &lb.NodeInfo{
		ID:       msg.NodeID,
		Healthy:  true,
		Weight:   100,
		Metadata: make(map[string]any),
	}

	// Extract metadata from payload if available
	if meta, ok := msg.Payload.(map[string]any); ok {
		node.Metadata = meta
	}

	if err := m.loadBalancer.RegisterNode(ctx, node); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to register remote node with load balancer",
				forge.F("node_id", msg.NodeID),
				forge.F("error", err),
			)
		}
		return
	}

	// Also register with health checker
	if m.healthChecker != nil {
		m.healthChecker.RegisterNode(node)
	}

	if m.logger != nil {
		m.logger.Info("remote node registered with load balancer",
			forge.F("node_id", msg.NodeID),
		)
	}
}

// handleNodeUnregister removes a remote node from the load balancer when it leaves the cluster.
func (m *manager) handleNodeUnregister(ctx context.Context, msg *coordinator.CoordinatorMessage) {
	if m.loadBalancer == nil || msg.NodeID == "" || msg.NodeID == m.nodeID {
		return
	}

	if m.healthChecker != nil {
		m.healthChecker.UnregisterNode(msg.NodeID)
	}

	if err := m.loadBalancer.UnregisterNode(ctx, msg.NodeID); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to unregister remote node from load balancer",
				forge.F("node_id", msg.NodeID),
				forge.F("error", err),
			)
		}
		return
	}

	if m.logger != nil {
		m.logger.Info("remote node unregistered from load balancer",
			forge.F("node_id", msg.NodeID),
		)
	}
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

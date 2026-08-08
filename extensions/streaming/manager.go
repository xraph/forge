package streaming

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xraph/forge"
	"github.com/xraph/forge/errors"
	streamauth "github.com/xraph/forge/extensions/streaming/auth"
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

	// Authorization (optional). When nil, the corresponding check is skipped —
	// which is why the extension wires a default pair in Register rather than
	// leaving them unset. An unset authorizer is an open door, so the default
	// must be deny-by-policy, not absent.
	roomAuth    streamauth.RoomAuthorizer
	messageAuth streamauth.MessageAuthorizer

	// Load balancer (optional, distributed mode)
	loadBalancer  lb.LoadBalancer
	healthChecker lb.HealthChecker

	// Session resumption (optional)
	sessionStore SessionStore

	// Hooks and codecs
	hooks  *HookRegistry
	codecs *CodecRegistry

	// Message deduplication for distributed mode
	dedup *messageDedup

	// Connection registry
	connections map[string]Connection // connID -> connection
	userConns   map[string][]string   // userID -> []connID

	// Fan-out indexes. Without these every room broadcast walked the entire
	// connection map testing IsInRoom, so a three-person room on a node holding
	// 50k sockets cost 50k map iterations and a 50k-entry slice allocation per
	// message. Delivery is now proportional to the size of the target, which is
	// what a broadcast should cost.
	roomConns    map[string]map[string]Connection // roomID -> connID -> conn
	channelConns map[string]map[string]Connection // channelID -> connID -> conn

	// anonConns counts registered connections with no user ID, so the anonymous
	// cap can be enforced without an identity to key on. Guarded by mu.
	anonConns int

	// startedAt is when Start last succeeded, for uptime. Guarded by mu.
	startedAt time.Time

	// messagesSent counts frames accepted for delivery, for throughput. Atomic
	// rather than mutex-guarded: it is incremented on every fan-out, and taking
	// the manager lock per message would serialise all delivery behind it.
	messagesSent atomic.Int64

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

// WithRoomAuthorizer sets the room authorizer. Join, leave and room-targeted
// sends are checked against it.
func WithRoomAuthorizer(ra streamauth.RoomAuthorizer) ManagerOption {
	return func(m *manager) { m.roomAuth = ra }
}

// WithMessageAuthorizer sets the message authorizer. Sends, edits, deletes and
// reactions are checked against it.
func WithMessageAuthorizer(ma streamauth.MessageAuthorizer) ManagerOption {
	return func(m *manager) { m.messageAuth = ma }
}

// WithNodeID sets the node ID for distributed mode.
func WithManagerNodeID(id string) ManagerOption {
	return func(m *manager) { m.nodeID = id }
}

// WithHookRegistry sets the hook registry for lifecycle hooks.
func WithHookRegistry(hr *HookRegistry) ManagerOption {
	return func(m *manager) { m.hooks = hr }
}

// WithCodecRegistry sets the codec registry for message encoding/decoding.
func WithCodecRegistry(cr *CodecRegistry) ManagerOption {
	return func(m *manager) { m.codecs = cr }
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
		roomConns:       make(map[string]map[string]Connection),
		channelConns:    make(map[string]map[string]Connection),
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
	} else if m.config.MaxAnonymousConnections > 0 {
		// Anonymous connections have no user to key a per-user limit on, so
		// without a separate cap they were unlimited: an unauthenticated client
		// could open sockets until the node ran out of file descriptors. Counted
		// rather than tracked per-identity precisely because there is no identity.
		if m.anonConns >= m.config.MaxAnonymousConnections {
			if m.metrics != nil {
				m.metrics.Counter("streaming.connections.anon_rejected").Inc()
			}

			return ErrConnectionLimitReached
		}
	}

	// Global cap, independent of identity. The per-user limit does not bound
	// total sockets on the node — a large enough user population exceeds any
	// per-user number.
	if m.config.MaxTotalConnections > 0 && len(m.connections) >= m.config.MaxTotalConnections {
		if m.metrics != nil {
			m.metrics.Counter("streaming.connections.rejected_at_capacity").Inc()
		}

		return ErrConnectionLimitReached
	}

	// Register connection
	m.connections[connID] = conn

	if userID == "" {
		m.anonConns++
	}

	// Attach the membership observer, then back-fill the indexes from whatever
	// the connection already holds.
	//
	// The back-fill is not belt-and-braces: a connection may legitimately be
	// given its rooms before it is registered — session resumption and the SSE
	// query-parameter path both do exactly that — and those joins happened
	// before there was an observer to hear them.
	if observable, ok := conn.(interface{ setMembershipObserver(membershipObserver) }); ok {
		observable.setMembershipObserver(m)
	}

	for _, roomID := range conn.GetJoinedRooms() {
		m.indexRoomJoinLocked(roomID, connID)
	}

	for _, channelID := range conn.GetSubscriptions() {
		m.indexChannelSubscribeLocked(channelID, connID)
	}

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

	// Drop the connection from every fan-out index it appears in. Missing this
	// would leak a closed connection into room and channel buckets, where
	// broadcasts would keep finding and writing to it forever.
	for _, roomID := range conn.GetJoinedRooms() {
		m.unindexRoomLocked(roomID, connID)
	}

	for _, channelID := range conn.GetSubscriptions() {
		m.unindexChannelLocked(channelID, connID)
	}

	// Remove connection
	delete(m.connections, connID)

	if userID == "" && m.anonConns > 0 {
		m.anonConns--
	}

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
//
// The snapshot is bound to the user who created it. Without that check, anyone
// who learned a session id could present it on a fresh connection and inherit
// the victim's room and channel subscriptions — receiving every message
// broadcast to them. The id is a v4 UUID and so not guessable, but it is not a
// secret either: it travels in a query string today, which means proxy logs,
// Referer headers and browser history. An identifier that leaks by design
// cannot also be an authorization token.
func (m *manager) ResumeSession(ctx context.Context, connID, sessionID string) (bool, error) {
	if m.sessionStore == nil || !m.config.EnableSessionResumption || sessionID == "" {
		return false, nil
	}

	conn, err := m.GetConnection(connID)
	if err != nil {
		return false, err
	}

	snapshot, err := m.sessionStore.Get(ctx, sessionID)
	if err != nil {
		return false, nil // Session not found or expired — not an error
	}

	// Bind the snapshot to its owner. An anonymous snapshot (empty UserID) can
	// only be resumed by an anonymous connection, and never carries one user's
	// state onto another's socket.
	if snapshot.UserID != conn.GetUserID() {
		if m.logger != nil {
			m.logger.Warn("session resume rejected: owner mismatch",
				forge.F("session_id", sessionID),
				forge.F("conn_id", connID),
			)
		}

		if m.metrics != nil {
			m.metrics.Counter("streaming.sessions.resume_denied").Inc()
		}

		return false, ErrSessionNotOwned
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

	// Fire pre-hook
	if m.hooks != nil {
		if err := m.hooks.FireOnRoomCreate(ctx, room); err != nil {
			return err
		}
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

	// Fire post-hook
	if m.hooks != nil {
		m.hooks.FireOnRoomDelete(ctx, roomID)
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.rooms.deleted").Inc()
	}

	return nil
}

// JoinRoom adds a connection to a room.
//
// The order here is load-bearing: authorize, then enforce the limit, then fire
// the hook, then write the store, and only then update the connection. An
// earlier version updated only the connection, which left roomStore membership
// permanently empty — so GetRoomMembers returned nothing, MaxRoomsPerUser never
// tripped (it counts store rows), and every authorization check that consults
// IsMember answered "no" for members and "yes" for nobody.
func (m *manager) JoinRoom(ctx context.Context, connID, roomID string) error {
	if !m.config.EnableRooms {
		return ErrRoomsDisabled
	}

	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	userID := conn.GetUserID()
	if userID == "" {
		return errors.New("connection has no user ID")
	}

	// Authorization first: a rejected join must not consume the limit budget,
	// touch the store, or fire hooks.
	if m.roomAuth != nil {
		allowed, authErr := m.roomAuth.CanJoin(ctx, userID, roomID)
		if authErr != nil {
			return fmt.Errorf("room join authorization failed: %w", authErr)
		}

		if !allowed {
			if m.metrics != nil {
				m.metrics.Counter("streaming.rooms.joins_denied").Inc()
			}

			return ErrRoomAccessDenied
		}
	}

	// Check room limit. Counts store membership, which is only meaningful
	// because the store write below actually happens.
	userRooms, _ := m.roomStore.GetUserRooms(ctx, userID)
	if m.config.MaxRoomsPerUser > 0 && len(userRooms) >= m.config.MaxRoomsPerUser {
		// Already in this room? Then it is a rejoin, not a new one, and the
		// limit does not apply. Matters for session resumption, which replays
		// a full room list for a user who may already be at the cap.
		if !slices.ContainsFunc(userRooms, func(r Room) bool { return r.GetID() == roomID }) {
			return ErrRoomLimitReached
		}
	}

	// Fire pre-hook
	if m.hooks != nil {
		if err := m.hooks.FireOnRoomJoin(ctx, conn, roomID); err != nil {
			return err
		}
	}

	// Persist membership. ErrAlreadyRoomMember is success: joins are idempotent,
	// and a second tab or a resumed session legitimately rejoins.
	member := NewMember(streaming.MemberOptions{
		UserID: userID,
		Role:   streaming.RoleMember,
	})
	if err := m.roomStore.AddMember(ctx, roomID, member); err != nil &&
		!errors.Is(err, streaming.ErrAlreadyRoomMember) {
		return fmt.Errorf("failed to add room member: %w", err)
	}

	conn.AddRoom(roomID)
	m.indexRoomJoin(roomID, connID)

	if m.metrics != nil {
		m.metrics.Counter("streaming.rooms.joins").Inc()
	}

	return nil
}

// LeaveRoom removes a connection from a room.
//
// Store membership is only dropped once the user's LAST connection leaves the
// room. Removing it on the first would evict a user who still has another tab
// open in the same room.
func (m *manager) LeaveRoom(ctx context.Context, connID, roomID string) error {
	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	userID := conn.GetUserID()

	conn.RemoveRoom(roomID)
	m.indexRoomLeave(roomID, connID)

	if userID != "" && !m.userStillInRoom(userID, roomID) {
		if err := m.roomStore.RemoveMember(ctx, roomID, userID); err != nil &&
			!errors.Is(err, streaming.ErrNotRoomMember) &&
			!errors.Is(err, streaming.ErrRoomNotFound) {
			if m.logger != nil {
				m.logger.Error("failed to remove room member",
					forge.F("room_id", roomID),
					forge.F("user_id", userID),
					forge.F("error", err),
				)
			}
		}
	}

	// Fire post-hook
	if m.hooks != nil {
		m.hooks.FireOnRoomLeave(ctx, conn, roomID)
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.rooms.leaves").Inc()
	}

	return nil
}

// userStillInRoom reports whether any of the user's other live connections
// remain in the room.
func (m *manager) userStillInRoom(userID, roomID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, connID := range m.userConns[userID] {
		if conn, ok := m.connections[connID]; ok && conn.IsInRoom(roomID) {
			return true
		}
	}

	return false
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

// Subscribe adds a connection to a channel.
//
// Like JoinRoom, this now writes through to the channel store. Previously it
// touched only the connection, so GetChannelSubscribers was permanently empty
// and MaxChannelsPerUser — which counts store rows — could never trip.
func (m *manager) Subscribe(ctx context.Context, connID, channelID string, filters map[string]any) error {
	if !m.config.EnableChannels {
		return ErrChannelsDisabled
	}

	conn, err := m.GetConnection(connID)
	if err != nil {
		return err
	}

	userID := conn.GetUserID()

	// Check channel limit
	userChannels, _ := m.channelStore.GetUserChannels(ctx, userID)
	if m.config.MaxChannelsPerUser > 0 && len(userChannels) >= m.config.MaxChannelsPerUser {
		if !slices.ContainsFunc(userChannels, func(c Channel) bool { return c.GetID() == channelID }) {
			return ErrChannelLimitReached
		}
	}

	// Persist the subscription. Already-subscribed is success: resubscribing is
	// idempotent, and session resumption replays the full channel list.
	sub := NewSubscription(streaming.SubscriptionOptions{
		ConnID:  connID,
		UserID:  userID,
		Filters: filters,
	})
	if err := m.channelStore.AddSubscription(ctx, channelID, sub); err != nil &&
		!errors.Is(err, streaming.ErrAlreadySubscribed) {
		return fmt.Errorf("failed to subscribe to channel: %w", err)
	}

	conn.AddSubscription(channelID)
	m.indexChannelSubscribe(channelID, connID)

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
	m.indexChannelUnsubscribe(channelID, connID)

	if err := m.channelStore.RemoveSubscription(ctx, channelID, connID); err != nil &&
		!errors.Is(err, streaming.ErrNotSubscribed) {
		if m.logger != nil {
			m.logger.Error("failed to unsubscribe from channel",
				forge.F("conn_id", connID),
				forge.F("channel_id", channelID),
				forge.F("error", err),
			)
		}
	}

	if m.metrics != nil {
		m.metrics.Counter("streaming.channels.unsubscriptions").Inc()
	}

	return nil
}

func (m *manager) ListChannels(ctx context.Context) ([]Channel, error) {
	return m.channelStore.List(ctx)
}

// Fan-out index maintenance
//
// The indexes are derived state: the connection's own joinedRooms/subscriptions
// sets remain the source of truth, and these mirror them for lookup. Every
// mutation of one must update the other, which is why AddRoom/RemoveRoom are
// only ever called from JoinRoom/LeaveRoom and the unregister path below.

// membershipObserver implementation. The connection calls these whenever its
// room or channel sets change, so the indexes stay correct even when a caller
// reaches for EnhancedConnection.AddRoom directly rather than going through
// JoinRoom.

func (m *manager) onRoomJoined(connID, roomID string) { m.indexRoomJoin(roomID, connID) }

func (m *manager) onRoomLeft(connID, roomID string) { m.indexRoomLeave(roomID, connID) }

func (m *manager) onChannelSubscribed(connID, channelID string) {
	m.indexChannelSubscribe(channelID, connID)
}

func (m *manager) onChannelUnsubscribed(connID, channelID string) {
	m.indexChannelUnsubscribe(channelID, connID)
}

func (m *manager) indexRoomJoin(roomID, connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.indexRoomJoinLocked(roomID, connID)
}

func (m *manager) indexRoomJoinLocked(roomID, connID string) {
	conn, ok := m.connections[connID]
	if !ok {
		return
	}

	if m.roomConns[roomID] == nil {
		m.roomConns[roomID] = make(map[string]Connection)
	}

	m.roomConns[roomID][connID] = conn
}

func (m *manager) indexRoomLeave(roomID, connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.unindexRoomLocked(roomID, connID)
}

// unindexRoomLocked drops one connection from a room index, removing the room
// bucket entirely once empty so the map does not grow without bound across the
// lifetime of a long-running node.
func (m *manager) unindexRoomLocked(roomID, connID string) {
	conns, ok := m.roomConns[roomID]
	if !ok {
		return
	}

	delete(conns, connID)

	if len(conns) == 0 {
		delete(m.roomConns, roomID)
	}
}

func (m *manager) indexChannelSubscribe(channelID, connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.indexChannelSubscribeLocked(channelID, connID)
}

func (m *manager) indexChannelSubscribeLocked(channelID, connID string) {
	conn, ok := m.connections[connID]
	if !ok {
		return
	}

	if m.channelConns[channelID] == nil {
		m.channelConns[channelID] = make(map[string]Connection)
	}

	m.channelConns[channelID][connID] = conn
}

func (m *manager) indexChannelUnsubscribe(channelID, connID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.unindexChannelLocked(channelID, connID)
}

func (m *manager) unindexChannelLocked(channelID, connID string) {
	conns, ok := m.channelConns[channelID]
	if !ok {
		return
	}

	delete(conns, connID)

	if len(conns) == 0 {
		delete(m.channelConns, channelID)
	}
}

// roomRecipients returns a snapshot of the connections in a room.
//
// A snapshot rather than a live view on purpose: delivery must not hold the
// manager lock, because a write to a slow socket would then block every
// registration and unregistration on the node for the duration of the fan-out.
func (m *manager) roomRecipients(roomID string) []Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conns := m.roomConns[roomID]
	if len(conns) == 0 {
		return nil
	}

	out := make([]Connection, 0, len(conns))
	for _, conn := range conns {
		out = append(out, conn)
	}

	return out
}

// channelRecipients returns a snapshot of the connections subscribed to a channel.
func (m *manager) channelRecipients(channelID string) []Connection {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conns := m.channelConns[channelID]
	if len(conns) == 0 {
		return nil
	}

	out := make([]Connection, 0, len(conns))
	for _, conn := range conns {
		out = append(out, conn)
	}

	return out
}

// Message pipeline helpers

// ProcessInbound runs a client-originated message through rate limiting,
// validation and authorization before it is allowed to fan out.
//
// This is the gate every inbound message must pass. Its predecessor was an
// unexported helper with no callers at all, which meant the configured rate
// limiter and validator were constructed, wired, reported in the dashboard, and
// never consulted — an unmetered path from any socket straight to a broadcast.
//
// Returns the (possibly transformed) message, or an error naming the reason it
// was rejected. The error is returned rather than swallowed so the caller can
// tell the client why, which is what lets a web client back off instead of
// hammering a limit it cannot see.
func (m *manager) ProcessInbound(ctx context.Context, message *Message, sender Connection) (*Message, error) {
	if message == nil {
		return nil, ErrInvalidMessage
	}

	var userID string
	if sender != nil {
		userID = sender.GetUserID()
	}

	// 1. Size. Checked before anything expensive, and before the message is
	// handed to a validator that would have to walk it.
	if m.config.MaxMessageSize > 0 {
		if size := approximateMessageSize(message); size > m.config.MaxMessageSize {
			if m.metrics != nil {
				m.metrics.Counter("streaming.messages.oversize").Inc()
			}

			return nil, fmt.Errorf("%w: %d bytes exceeds limit of %d",
				ErrMessageTooLarge, size, m.config.MaxMessageSize)
		}
	}

	// 2. Rate limiting.
	if m.rateLimiter != nil && userID != "" {
		allowed, err := m.rateLimiter.Allow(ctx, userID, "message")
		if err != nil {
			// A limiter that cannot answer must not become an open door in one
			// direction or a hard outage in the other. Log and allow: the
			// backing store being briefly unreachable should not stop a chat.
			if m.logger != nil {
				m.logger.Error("rate limiter error, allowing message",
					forge.F("user_id", userID),
					forge.F("error", err),
				)
			}
		} else if !allowed {
			if m.metrics != nil {
				m.metrics.Counter("streaming.messages.rate_limited").Inc()
			}

			return nil, ErrRateLimitExceeded
		}
	}

	// 3. Authorization for the declared target. This is what stops a client
	// naming any room id it likes and having the server fan the message out to
	// a room it was never a member of.
	if err := m.authorizeSend(ctx, message, sender, userID); err != nil {
		return nil, err
	}

	// 4. Validation (content rules, schema, security scanning).
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

// authorizeSend checks that the sender may publish to the target the message
// names.
func (m *manager) authorizeSend(ctx context.Context, message *Message, sender Connection, userID string) error {
	if sender == nil {
		return nil // Server-originated message; not subject to client authorization.
	}

	switch {
	case message.RoomID != "":
		// Membership is the baseline check and does not depend on an authorizer
		// being configured. A client may only publish to a room this very
		// connection has joined.
		if !sender.IsInRoom(message.RoomID) {
			if m.metrics != nil {
				m.metrics.Counter("streaming.messages.send_denied").Inc()
			}

			return fmt.Errorf("%w: not joined to room %s", ErrSendDenied, message.RoomID)
		}

		if m.messageAuth != nil && userID != "" {
			allowed, err := m.messageAuth.CanSend(ctx, userID, message.RoomID, streamauth.TargetTypeRoom)
			if err != nil {
				return fmt.Errorf("send authorization failed: %w", err)
			}

			if !allowed {
				if m.metrics != nil {
					m.metrics.Counter("streaming.messages.send_denied").Inc()
				}

				return ErrSendDenied
			}
		}

		// Moderation state, which the room already tracks and nothing consulted.
		if userID != "" {
			if room, err := m.roomStore.Get(ctx, message.RoomID); err == nil {
				if muted, mErr := room.IsMuted(ctx, userID); mErr == nil && muted {
					return ErrUserMuted
				}

				if banned, bErr := room.IsBanned(ctx, userID); bErr == nil && banned {
					return ErrUserBanned
				}
			}
		}

	case message.ChannelID != "":
		if !sender.IsSubscribed(message.ChannelID) {
			if m.metrics != nil {
				m.metrics.Counter("streaming.messages.send_denied").Inc()
			}

			return fmt.Errorf("%w: not subscribed to channel %s", ErrSendDenied, message.ChannelID)
		}

		if m.messageAuth != nil && userID != "" {
			allowed, err := m.messageAuth.CanSend(ctx, userID, message.ChannelID, streamauth.TargetTypeChannel)
			if err != nil {
				return fmt.Errorf("send authorization failed: %w", err)
			}

			if !allowed {
				return ErrSendDenied
			}
		}
	}

	return nil
}

// approximateMessageSize estimates the wire size of a message without paying
// for a full encode.
//
// Binary payloads are measured exactly; structured data is JSON-marshalled,
// which is the same work the default codec does on delivery. Marshal failure
// returns 0 rather than an error: an unencodable message will fail later in
// delivery with a better diagnostic than a size check can give.
func approximateMessageSize(msg *Message) int {
	if len(msg.RawData) > 0 {
		return len(msg.RawData)
	}

	data, err := json.Marshal(msg.Data)
	if err != nil {
		return 0
	}

	return len(data)
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

	// Fire post-delivery hook asynchronously
	if m.hooks != nil {
		m.hooks.FireOnMessageDelivered(ctx, conn, msg)
	}

	// Determine content type for encoding
	contentType := msg.ContentType
	if contentType == "" {
		contentType = conn.GetContentType()
	}

	// Use WriteJSON for default JSON or empty content type (backward compatible)
	if contentType == "" || contentType == streaming.ContentTypeJSON {
		return conn.WriteJSON(msg)
	}

	// Use codec for non-JSON content types.
	//
	// EncodeWithType, not Encode: Encode dispatches on msg.ContentType, so when
	// the message carried none and the content type came from the connection's
	// preference, it fell through to the default codec and silently emitted JSON.
	// The per-connection preference set via SetContentType therefore never
	// reached a codec at all.
	if m.codecs != nil {
		data, err := m.codecs.EncodeWithType(contentType, msg)
		if err != nil {
			return err
		}

		// Binary payloads must go out as binary frames. Writing them through
		// the text path emits a WebSocket text frame, and a browser closes the
		// connection with 1007 on the first byte that is not valid UTF-8.
		binary := contentType == streaming.ContentTypeBinary

		// Carry the message type across the encode boundary. Write and
		// WriteBinary take bytes alone, so without this the connection's send
		// queue cannot tell a typing snapshot from a chat message and has to
		// assume the latter. Since the content type is usually a per-connection
		// preference, every frame to a non-JSON client came through here — which
		// switched the per-type overflow policy off for that client entirely.
		if fw, ok := conn.(interface{ WriteFrame(OutboundFrame) error }); ok {
			return fw.WriteFrame(OutboundFrame{
				Data:   data,
				Type:   msg.Type,
				Binary: binary,
			})
		}

		if binary {
			return conn.WriteBinary(data)
		}

		return conn.Write(data)
	}

	// Fallback to JSON if no codec registry
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
	m.fanOut(ctx, m.GetAllConnections(), message, "broadcast", "global")

	// Relay to other nodes
	m.coordinatorBroadcast(ctx, "global", "", message)

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.broadcast").Inc()
	}

	return nil
}

func (m *manager) BroadcastToRoom(ctx context.Context, roomID string, message *Message) error {
	// Index lookup, not a scan of every connection on the node.
	members := m.roomRecipients(roomID)

	count := m.fanOut(ctx, members, message, "room", roomID)

	// Relay to other nodes
	m.coordinatorBroadcast(ctx, "room", roomID, message)

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.room_broadcast").Inc()
		m.metrics.Gauge("streaming.messages.room_recipients").Set(float64(count))
	}

	return nil
}

func (m *manager) BroadcastToChannel(ctx context.Context, channelID string, message *Message) error {
	subscribers := m.channelRecipients(channelID)

	count := m.fanOut(ctx, subscribers, message, "channel", channelID)

	if m.metrics != nil {
		m.metrics.Counter("streaming.messages.channel_broadcast").Inc()
		m.metrics.Gauge("streaming.messages.channel_recipients").Set(float64(count))
	}

	return nil
}

// maxFanOutConcurrency bounds the goroutines one broadcast may spawn.
//
// The previous implementation spawned one goroutine per recipient above a
// threshold of 8, so a 10k-member room cost 10k goroutines for a single message
// — and a busy room multiplied that by its message rate. A fixed worker count
// delivers the same parallelism benefit for slow sockets without letting room
// size choose the scheduler's workload.
const maxFanOutConcurrency = 64

// fanOut delivers a message to a set of connections and returns the number that
// accepted it.
//
// Delivery never runs under the manager lock: recipients are snapshotted first.
// A write that blocks would otherwise stall every connect and disconnect on the
// node for as long as the slowest socket in the room takes to drain.
func (m *manager) fanOut(
	ctx context.Context,
	conns []Connection,
	message *Message,
	targetKind string,
	targetID string,
) int64 {
	if len(conns) == 0 {
		return 0
	}

	logFailure := func(c Connection, err error) {
		if m.logger != nil {
			m.logger.Error("failed to deliver message",
				forge.F("conn_id", c.ID()),
				forge.F(targetKind+"_id", targetID),
				forge.F("error", err),
			)
		}
	}

	// Serial below the concurrency floor: spinning up workers to write a
	// handful of frames costs more than it saves.
	if len(conns) <= 8 {
		var count int64

		for _, conn := range conns {
			if err := m.deliverToConnection(ctx, conn, message); err != nil {
				logFailure(conn, err)
			} else {
				count++
			}
		}

		m.messagesSent.Add(count)

		return count
	}

	var (
		count int64
		wg    sync.WaitGroup
	)

	work := make(chan Connection)

	workers := min(maxFanOutConcurrency, len(conns))

	for range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for conn := range work {
				if err := m.deliverToConnection(ctx, conn, message); err != nil {
					logFailure(conn, err)
				} else {
					atomic.AddInt64(&count, 1)
				}
			}
		}()
	}

	for _, conn := range conns {
		work <- conn
	}

	close(work)
	wg.Wait()

	delivered := atomic.LoadInt64(&count)
	m.messagesSent.Add(delivered)

	return delivered
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
	m.fanOut(ctx, m.GetUserConnections(userID), message, "user", userID)

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

// BroadcastExcept sends a message to every connection except those belonging to
// the given users.
//
// The parameter is USER ids, matching the Manager interface and both Room
// implementations. It was previously treated as connection ids here, so the
// ordinary call — "broadcast to the room, except the sender" — excluded nothing
// and echoed every message back to its author.
func (m *manager) BroadcastExcept(ctx context.Context, message *Message, excludeUserIDs []string) error {
	exclude := make(map[string]struct{}, len(excludeUserIDs))
	for _, id := range excludeUserIDs {
		exclude[id] = struct{}{}
	}

	// Snapshot under the lock, deliver outside it. Holding RLock across the
	// writes blocked every Register and Unregister for the whole fan-out.
	m.mu.RLock()
	recipients := make([]Connection, 0, len(m.connections))

	for _, conn := range m.connections {
		if _, skip := exclude[conn.GetUserID()]; !skip {
			recipients = append(recipients, conn)
		}
	}
	m.mu.RUnlock()

	m.fanOut(ctx, recipients, message, "broadcast", "except")

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

	// Get old status for hook
	var oldStatus string
	if m.hooks != nil {
		if existing, err := m.presenceTracker.GetPresence(ctx, userID); err == nil && existing != nil {
			oldStatus = existing.Status
		}
	}

	if err := m.presenceTracker.SetPresence(ctx, userID, status); err != nil {
		return err
	}

	// Fire post-hook
	if m.hooks != nil && oldStatus != status {
		m.hooks.FireOnPresenceChange(ctx, userID, oldStatus, status)
	}

	return nil
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

	if err := m.typingTracker.StartTyping(ctx, userID, roomID); err != nil {
		return err
	}

	return m.broadcastTyping(ctx, userID, roomID, true)
}

func (m *manager) StopTyping(ctx context.Context, userID, roomID string) error {
	if !m.config.EnableTypingIndicators {
		return nil
	}

	if err := m.typingTracker.StopTyping(ctx, userID, roomID); err != nil {
		return err
	}

	return m.broadcastTyping(ctx, userID, roomID, false)
}

// broadcastTyping publishes a typing indicator to the room.
//
// The tracker records who is typing and expires the entry, but nothing ever put
// that on the wire — typingTracker.BroadcastTyping is a stub returning nil, and
// no other path published it — so the feature was invisible to every client
// despite being fully wired on the inbound side. handleMessage accepts a typing
// frame, sets the state, and until now that was where it ended.
//
// The frame mirrors what the inbound handler parses and what the AsyncAPI spec
// documents: Data is the boolean, and the room is the fan-out scope. Going
// through BroadcastToRoom rather than writing to members directly is what relays
// the indicator to other nodes, so typing works across a cluster.
//
// The author is included in the fan-out. Excluding them would need a room-scoped
// exclusion primitive that does not exist here, and a client ignoring the echo of
// its own indicator is both trivial and already required — the same frame arrives
// from other nodes.
func (m *manager) broadcastTyping(ctx context.Context, userID, roomID string, isTyping bool) error {
	return m.BroadcastToRoom(ctx, roomID, &Message{
		ID:        fmt.Sprintf("typing_%s_%d", userID, time.Now().UnixNano()),
		Type:      MessageTypeTyping,
		RoomID:    roomID,
		UserID:    userID,
		Data:      isTyping,
		Timestamp: time.Now(),
	})
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
	m.startedAt = time.Now()

	if m.logger != nil {
		m.logger.Info("streaming manager started")
	}

	return nil
}

// Drain gracefully closes all active connections, allowing in-flight messages to complete.
// It sends a system close message to each connection and waits for them to disconnect
// or for the context to expire. Call Drain before Stop for graceful shutdown.
func (m *manager) Drain(ctx context.Context) error {
	m.mu.RLock()
	conns := make([]Connection, 0, len(m.connections))
	for _, conn := range m.connections {
		conns = append(conns, conn)
	}
	m.mu.RUnlock()

	if m.logger != nil {
		m.logger.Info("draining streaming connections",
			forge.F("count", len(conns)),
		)
	}

	// Send close notification to all connections concurrently
	closeMsg := &Message{
		Type:      streaming.MessageTypeSystem,
		Event:     "server_shutdown",
		Data:      "server is shutting down",
		Timestamp: time.Now(),
	}

	var wg sync.WaitGroup

	for _, conn := range conns {
		wg.Add(1)

		go func(c Connection) {
			defer wg.Done()

			// Best-effort send close message
			_ = m.deliverToConnection(ctx, c, closeMsg)

			// Close the underlying connection
			if err := c.Close(); err != nil && m.logger != nil {
				m.logger.Debug("error closing connection during drain",
					forge.F("conn_id", c.ID()),
					forge.F("error", err),
				)
			}
		}(conn)
	}

	// Wait for all close operations or context cancellation
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		if m.logger != nil {
			m.logger.Info("all connections drained")
		}
	case <-ctx.Done():
		if m.logger != nil {
			m.logger.Warn("drain timed out, forcing shutdown")
		}
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

	// Stop the background cleanup goroutines. Both the dedup tracker and the
	// in-memory session store run an unstoppable ticker loop until closed, so
	// without this every start/stop cycle leaked one of each — most visibly in
	// tests, which construct and discard managers repeatedly.
	if m.dedup != nil {
		_ = m.dedup.Close()
		m.dedup = nil
	}

	if closer, ok := m.sessionStore.(interface{ Close() error }); ok && m.sessionStore != nil {
		_ = closer.Close()
	}

	m.started = false

	if m.logger != nil {
		m.logger.Info("streaming manager stopped")
	}

	return nil
}

func (m *manager) Health(ctx context.Context) error {
	// L1: Manager initialized and running
	m.mu.RLock()
	started := m.started
	m.mu.RUnlock()

	if !started {
		return fmt.Errorf("streaming manager not started")
	}

	// L2: Backend stores reachable
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

	// L3: Distributed coordinator connected (if configured)
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

// SearchRooms finds rooms matching a query and optional filters.
//
// Delegates to the store, which can push the predicate down to its backend —
// a Redis index scan rather than shipping every room in the deployment to this
// process to be filtered in a Go loop. The in-memory path below remains as the
// fallback for stores that do not implement Search.
func (m *manager) SearchRooms(ctx context.Context, query string, filters map[string]any) ([]streaming.Room, error) {
	if results, err := m.roomStore.Search(ctx, query, filters); err == nil {
		return results, nil
	}

	// Fallback: filter in process.
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
	// The store can apply both the predicate and the limit at the backend.
	if rooms, err := m.roomStore.GetPublicRooms(ctx, limit); err == nil {
		return rooms, nil
	}

	// Fallback: filter in process.
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

	// Real numbers, not placeholders.
	//
	// This previously reported Uptime as time.Since(time.Now()) — always about
	// zero — alongside a hardcoded 0 for messages, throughput and memory. The
	// dashboard rendered all four as though they were measurements, which is
	// worse than showing nothing: an operator reading "0 messages/sec" during an
	// incident concludes traffic has stopped.
	m.mu.RLock()
	startedAt := m.startedAt
	totalMessages := m.messagesSent.Load()
	m.mu.RUnlock()

	var uptime time.Duration
	if !startedAt.IsZero() {
		uptime = time.Since(startedAt)
	}

	var messagesPerSec float64
	if seconds := uptime.Seconds(); seconds > 0 {
		messagesPerSec = float64(totalMessages) / seconds
	}

	var mem runtime.MemStats

	runtime.ReadMemStats(&mem)

	stats := &streaming.ManagerStats{
		TotalConnections: connectionCount,
		TotalRooms:       len(rooms),
		TotalChannels:    len(channels),
		TotalMessages:    totalMessages,
		OnlineUsers:      onlineCount,
		MessagesPerSec:   messagesPerSec,
		Uptime:           uptime,
		MemoryUsage:      int64(mem.Alloc),
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

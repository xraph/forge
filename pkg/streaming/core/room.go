package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// RateLimitConfig contains rate limiting configuration
type RateLimitConfig struct {
	MessagesPerMinute int           `json:"messages_per_minute"`
	BurstSize         int           `json:"burst_size"`
	WindowSize        time.Duration `json:"window_size"`
}

// RateLimiter interface for rate limiting
type RateLimiter interface {
	Allow(userID string) bool
	Reset(userID string)
	GetStats(userID string) RateLimitStats
}

// RateLimitStats represents rate limiting statistics
type RateLimitStats struct {
	RequestCount   int       `json:"request_count"`
	LastRequest    time.Time `json:"last_request"`
	RemainingQuota int       `json:"remaining_quota"`
	ResetTime      time.Time `json:"reset_time"`
}

// TokenBucketRateLimiter implements token bucket rate limiting
type TokenBucketRateLimiter struct {
	config  RateLimitConfig
	buckets map[string]*tokenBucket
	mu      sync.RWMutex
}

type tokenBucket struct {
	tokens     int
	lastRefill time.Time
	requests   int
}

// NewTokenBucketRateLimiter creates a new token bucket rate limiter
func NewTokenBucketRateLimiter(config RateLimitConfig) RateLimiter {
	return &TokenBucketRateLimiter{
		config:  config,
		buckets: make(map[string]*tokenBucket),
	}
}

// Allow checks if a request is allowed for the user
func (rl *TokenBucketRateLimiter) Allow(userID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[userID]
	if !exists {
		bucket = &tokenBucket{
			tokens:     rl.config.BurstSize,
			lastRefill: time.Now(),
		}
		rl.buckets[userID] = bucket
	}

	// Refill tokens
	now := time.Now()
	elapsed := now.Sub(bucket.lastRefill)
	tokensToAdd := int(elapsed.Minutes() * float64(rl.config.MessagesPerMinute))

	if tokensToAdd > 0 {
		bucket.tokens += tokensToAdd
		if bucket.tokens > rl.config.BurstSize {
			bucket.tokens = rl.config.BurstSize
		}
		bucket.lastRefill = now
	}

	// Check if request is allowed
	if bucket.tokens > 0 {
		bucket.tokens--
		bucket.requests++
		return true
	}

	return false
}

// Reset resets the rate limit for a user
func (rl *TokenBucketRateLimiter) Reset(userID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, userID)
}

// GetStats returns rate limiting statistics for a user
func (rl *TokenBucketRateLimiter) GetStats(userID string) RateLimitStats {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	bucket, exists := rl.buckets[userID]
	if !exists {
		return RateLimitStats{}
	}

	return RateLimitStats{
		RequestCount:   bucket.requests,
		LastRequest:    bucket.lastRefill,
		RemainingQuota: bucket.tokens,
		ResetTime:      bucket.lastRefill.Add(time.Minute),
	}
}

// RoomType represents different types of rooms
type RoomType string

const (
	RoomTypePublic    RoomType = "public"
	RoomTypePrivate   RoomType = "private"
	RoomTypeDirect    RoomType = "direct"
	RoomTypeBroadcast RoomType = "broadcast"
	RoomTypeSystem    RoomType = "system"
)

// RoomConfig contains room configuration
type RoomConfig struct {
	Type              RoomType               `json:"type"`
	MaxConnections    int                    `json:"max_connections"`
	MaxMessageHistory int                    `json:"max_message_history"`
	MessageTTL        time.Duration          `json:"message_ttl"`
	AllowAnonymous    bool                   `json:"allow_anonymous"`
	RequireInvitation bool                   `json:"require_invitation"`
	ModeratedMode     bool                   `json:"moderated_mode"`
	PersistMessages   bool                   `json:"persist_messages"`
	EnablePresence    bool                   `json:"enable_presence"`
	RateLimitConfig   *RateLimitConfig       `json:"rate_limit_config,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}

// DefaultRoomConfig returns default room configuration
func DefaultRoomConfig() RoomConfig {
	return RoomConfig{
		Type:              RoomTypePublic,
		MaxConnections:    1000,
		MaxMessageHistory: 100,
		MessageTTL:        24 * time.Hour,
		AllowAnonymous:    true,
		RequireInvitation: false,
		ModeratedMode:     false,
		PersistMessages:   true,
		EnablePresence:    true,
		RateLimitConfig: &RateLimitConfig{
			MessagesPerMinute: 60,
			BurstSize:         10,
		},
		Metadata: make(map[string]interface{}),
	}
}

// RoomStats represents room statistics
type RoomStats struct {
	ID                string        `json:"id"`
	Type              RoomType      `json:"type"`
	ConnectionCount   int           `json:"connection_count"`
	OnlineUserCount   int           `json:"online_user_count"`
	TotalMessageCount int64         `json:"total_message_count"`
	MessageHistory    int           `json:"message_history"`
	CreatedAt         time.Time     `json:"created_at"`
	LastActivity      time.Time     `json:"last_activity"`
	BytesTransferred  int64         `json:"bytes_transferred"`
	MessagesPerMinute float64       `json:"messages_per_minute"`
	AverageLatency    time.Duration `json:"average_latency"`
	ErrorCount        int64         `json:"error_count"`
}

// Room represents a streaming room
type Room interface {
	// Identity
	ID() string
	Type() RoomType
	GetConfig() RoomConfig
	GetMetadata() map[string]interface{}
	SetMetadata(key string, value interface{})

	// Connection management
	AddConnection(conn Connection) error
	RemoveConnection(connectionID string) error
	GetConnection(connectionID string) (Connection, error)
	GetConnections() []Connection
	GetConnectionCount() int
	HasConnection(connectionID string) bool

	// User management
	GetUsers() []string
	GetUserConnections(userID string) []Connection
	IsUserInRoom(userID string) bool
	GetUserCount() int

	// Message handling
	Broadcast(ctx context.Context, message *Message) error
	BroadcastToUser(ctx context.Context, userID string, message *Message) error
	BroadcastExcept(ctx context.Context, excludeConnectionID string, message *Message) error
	SendToConnection(ctx context.Context, connectionID string, message *Message) error

	// Message history
	GetMessageHistory(filter *MessageFilter) ([]*Message, error)
	AddToHistory(message *Message) error
	ClearHistory() error

	// Presence management
	GetPresence() PresenceTracker
	UpdateUserPresence(ctx context.Context, userID string, status PresenceStatus, metadata map[string]interface{}) error

	// Room lifecycle
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Close(ctx context.Context) error
	IsActive() bool

	// Moderation
	MuteUser(ctx context.Context, userID string, duration time.Duration) error
	UnmuteUser(ctx context.Context, userID string) error
	IsUserMuted(userID string) bool
	BanUser(ctx context.Context, userID string, reason string) error
	UnbanUser(ctx context.Context, userID string) error
	IsUserBanned(userID string) bool

	// Events
	OnUserJoin(handler UserJoinHandler)
	OnUserLeave(handler UserLeaveHandler)
	OnMessage(handler RoomMessageHandler)
	OnError(handler RoomErrorHandler)

	// Statistics
	GetStats() RoomStats
}

// Event handlers
type UserJoinHandler func(room Room, userID string, conn Connection)
type UserLeaveHandler func(room Room, userID string, conn Connection)
type RoomMessageHandler func(room Room, message *Message, sender Connection)
type RoomErrorHandler func(room Room, err error)

// UserModeration represents user moderation state
type UserModeration struct {
	UserID     string    `json:"user_id"`
	IsMuted    bool      `json:"is_muted"`
	MutedUntil time.Time `json:"muted_until,omitempty"`
	IsBanned   bool      `json:"is_banned"`
	BannedAt   time.Time `json:"banned_at,omitempty"`
	BanReason  string    `json:"ban_reason,omitempty"`
}

// DefaultRoom implements the Room interface
type DefaultRoom struct {
	id              string
	config          RoomConfig
	connections     map[string]Connection
	userConnections map[string][]string // userID -> []connectionID
	messageHistory  []*Message
	presence        PresenceTracker
	moderation      map[string]*UserModeration
	stats           RoomStats
	active          bool

	// Event handlers
	userJoinHandler  UserJoinHandler
	userLeaveHandler UserLeaveHandler
	messageHandler   RoomMessageHandler
	errorHandler     RoomErrorHandler

	// Message filtering and rate limiting
	messageFilters []MessageFilter
	rateLimiter    RateLimiter

	// Synchronization
	mu sync.RWMutex

	// Dependencies
	logger  common.Logger
	metrics common.Metrics

	// Cleanup
	stopCh chan struct{}
}

// NewDefaultRoom creates a new default room
func NewDefaultRoom(id string, config RoomConfig, logger common.Logger, metrics common.Metrics) Room {
	room := &DefaultRoom{
		id:              id,
		config:          config,
		connections:     make(map[string]Connection),
		userConnections: make(map[string][]string),
		messageHistory:  make([]*Message, 0, config.MaxMessageHistory),
		moderation:      make(map[string]*UserModeration),
		logger:          logger,
		metrics:         metrics,
		stopCh:          make(chan struct{}),
		stats: RoomStats{
			ID:        id,
			Type:      config.Type,
			CreatedAt: time.Now(),
		},
	}

	// Initialize presence tracker if enabled
	if config.EnablePresence {
		presenceConfig := DefaultPresenceConfig()
		room.presence = NewLocalPresenceTracker(presenceConfig, logger, metrics)
	}

	// Initialize rate limiter if configured
	if config.RateLimitConfig != nil {
		room.rateLimiter = NewTokenBucketRateLimiter(*config.RateLimitConfig)
	}

	return room
}

// ID returns the room ID
func (r *DefaultRoom) ID() string {
	return r.id
}

// Type returns the room type
func (r *DefaultRoom) Type() RoomType {
	return r.config.Type
}

// GetConfig returns the room configuration
func (r *DefaultRoom) GetConfig() RoomConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config
}

// GetMetadata returns room metadata
func (r *DefaultRoom) GetMetadata() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	metadata := make(map[string]interface{})
	for k, v := range r.config.Metadata {
		metadata[k] = v
	}
	return metadata
}

// SetMetadata sets room metadata
func (r *DefaultRoom) SetMetadata(key string, value interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.config.Metadata == nil {
		r.config.Metadata = make(map[string]interface{})
	}
	r.config.Metadata[key] = value
}

// AddConnection adds a connection to the room
func (r *DefaultRoom) AddConnection(conn Connection) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	connectionID := conn.ID()
	userID := conn.UserID()

	// Check if room is at capacity
	if len(r.connections) >= r.config.MaxConnections {
		return common.ErrInvalidConfig("max_connections", fmt.Errorf("room is at maximum capacity (%d)", r.config.MaxConnections))
	}

	// Check if user is banned
	if r.isUserBannedUnsafe(userID) {
		return common.ErrValidationError("user_banned", fmt.Errorf("user %s is banned from room %s", userID, r.id))
	}

	// Check if anonymous users are allowed
	if !r.config.AllowAnonymous && userID == "" {
		return common.ErrValidationError("anonymous_not_allowed", fmt.Errorf("anonymous users not allowed in room %s", r.id))
	}

	// Add connection
	r.connections[connectionID] = conn

	// Add to user connections
	r.userConnections[userID] = append(r.userConnections[userID], connectionID)

	// Update presence if enabled
	if r.presence != nil {
		metadata := conn.Metadata()
		if err := r.presence.Join(context.Background(), userID, r.id, metadata); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to update presence on join",
					logger.String("user_id", userID),
					logger.String("room_id", r.id),
					logger.Error(err),
				)
			}
		}
	}

	// Update stats
	r.stats.ConnectionCount = len(r.connections)
	r.stats.LastActivity = time.Now()

	// Call join handler
	if r.userJoinHandler != nil {
		go r.userJoinHandler(r, userID, conn)
	}

	if r.logger != nil {
		r.logger.Info("connection added to room",
			logger.String("connection_id", connectionID),
			logger.String("user_id", userID),
			logger.String("room_id", r.id),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.connections.added").Inc()
		r.metrics.Gauge("streaming.room.connections.active").Set(float64(len(r.connections)))
	}

	return nil
}

// RemoveConnection removes a connection from the room
func (r *DefaultRoom) RemoveConnection(connectionID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	conn, exists := r.connections[connectionID]
	if !exists {
		return common.ErrServiceNotFound(connectionID)
	}

	userID := conn.UserID()

	// Remove from connections
	delete(r.connections, connectionID)

	// Remove from user connections
	if userConnections, exists := r.userConnections[userID]; exists {
		for i, id := range userConnections {
			if id == connectionID {
				r.userConnections[userID] = append(userConnections[:i], userConnections[i+1:]...)
				break
			}
		}
		// Clean up empty user connections
		if len(r.userConnections[userID]) == 0 {
			delete(r.userConnections, userID)
		}
	}

	// Update presence if enabled and user has no more connections
	if r.presence != nil && len(r.userConnections[userID]) == 0 {
		if err := r.presence.Leave(context.Background(), userID, r.id); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to update presence on leave",
					logger.String("user_id", userID),
					logger.String("room_id", r.id),
					logger.Error(err),
				)
			}
		}
	}

	// Update stats
	r.stats.ConnectionCount = len(r.connections)
	r.stats.LastActivity = time.Now()

	// Call leave handler
	if r.userLeaveHandler != nil {
		go r.userLeaveHandler(r, userID, conn)
	}

	if r.logger != nil {
		r.logger.Info("connection removed from room",
			logger.String("connection_id", connectionID),
			logger.String("user_id", userID),
			logger.String("room_id", r.id),
		)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.connections.removed").Inc()
		r.metrics.Gauge("streaming.room.connections.active").Set(float64(len(r.connections)))
	}

	return nil
}

// GetConnection gets a connection by ID
func (r *DefaultRoom) GetConnection(connectionID string) (Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	conn, exists := r.connections[connectionID]
	if !exists {
		return nil, common.ErrServiceNotFound(connectionID)
	}

	return conn, nil
}

// GetConnections returns all connections in the room
func (r *DefaultRoom) GetConnections() []Connection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	connections := make([]Connection, 0, len(r.connections))
	for _, conn := range r.connections {
		connections = append(connections, conn)
	}

	return connections
}

// GetConnectionCount returns the number of connections
func (r *DefaultRoom) GetConnectionCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.connections)
}

// HasConnection checks if a connection exists in the room
func (r *DefaultRoom) HasConnection(connectionID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.connections[connectionID]
	return exists
}

// GetUsers returns all unique users in the room
func (r *DefaultRoom) GetUsers() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	users := make([]string, 0, len(r.userConnections))
	for userID := range r.userConnections {
		users = append(users, userID)
	}

	return users
}

// GetUserConnections returns all connections for a specific user
func (r *DefaultRoom) GetUserConnections(userID string) []Connection {
	r.mu.RLock()
	defer r.mu.RUnlock()

	connectionIDs, exists := r.userConnections[userID]
	if !exists {
		return []Connection{}
	}

	connections := make([]Connection, 0, len(connectionIDs))
	for _, id := range connectionIDs {
		if conn, exists := r.connections[id]; exists {
			connections = append(connections, conn)
		}
	}

	return connections
}

// IsUserInRoom checks if a user is in the room
func (r *DefaultRoom) IsUserInRoom(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.userConnections[userID]
	return exists
}

// GetUserCount returns the number of unique users
func (r *DefaultRoom) GetUserCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.userConnections)
}

// Broadcast sends a message to all connections in the room
func (r *DefaultRoom) Broadcast(ctx context.Context, message *Message) error {
	connections := r.GetConnections()

	if len(connections) == 0 {
		return nil // No connections to broadcast to
	}

	// Add to message history
	if r.config.PersistMessages {
		r.AddToHistory(message)
	}

	// Send to all connections
	var errors []error
	successCount := 0

	for _, conn := range connections {
		if err := conn.Send(ctx, message); err != nil {
			errors = append(errors, fmt.Errorf("failed to send to connection %s: %w", conn.ID(), err))
		} else {
			successCount++
		}
	}

	// Update stats
	r.mu.Lock()
	r.stats.TotalMessageCount++
	r.stats.LastActivity = time.Now()
	r.mu.Unlock()

	// Call message handler
	if r.messageHandler != nil {
		go r.messageHandler(r, message, nil) // No specific sender for broadcasts
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.messages.broadcast").Inc()
		r.metrics.Histogram("streaming.room.broadcast.size").Observe(float64(len(connections)))
		r.metrics.Histogram("streaming.room.broadcast.success_rate").Observe(float64(successCount) / float64(len(connections)))
	}

	if len(errors) > 0 {
		return fmt.Errorf("broadcast errors (%d/%d failed): %v", len(errors), len(connections), errors)
	}

	return nil
}

// BroadcastToUser sends a message to all connections of a specific user
func (r *DefaultRoom) BroadcastToUser(ctx context.Context, userID string, message *Message) error {
	connections := r.GetUserConnections(userID)

	if len(connections) == 0 {
		return common.ErrServiceNotFound(fmt.Sprintf("user %s not found in room %s", userID, r.id))
	}

	var errors []error
	for _, conn := range connections {
		if err := conn.Send(ctx, message); err != nil {
			errors = append(errors, fmt.Errorf("failed to send to connection %s: %w", conn.ID(), err))
		}
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.messages.user_broadcast").Inc()
	}

	if len(errors) > 0 {
		return fmt.Errorf("user broadcast errors: %v", errors)
	}

	return nil
}

// BroadcastExcept sends a message to all connections except one
func (r *DefaultRoom) BroadcastExcept(ctx context.Context, excludeConnectionID string, message *Message) error {
	connections := r.GetConnections()

	var errors []error
	sentCount := 0

	for _, conn := range connections {
		if conn.ID() != excludeConnectionID {
			if err := conn.Send(ctx, message); err != nil {
				errors = append(errors, fmt.Errorf("failed to send to connection %s: %w", conn.ID(), err))
			} else {
				sentCount++
			}
		}
	}

	// Add to message history
	if r.config.PersistMessages {
		r.AddToHistory(message)
	}

	// Update stats
	r.mu.Lock()
	r.stats.TotalMessageCount++
	r.stats.LastActivity = time.Now()
	r.mu.Unlock()

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.messages.broadcast_except").Inc()
		r.metrics.Histogram("streaming.room.broadcast_except.size").Observe(float64(sentCount))
	}

	if len(errors) > 0 {
		return fmt.Errorf("broadcast except errors: %v", errors)
	}

	return nil
}

// SendToConnection sends a message to a specific connection
func (r *DefaultRoom) SendToConnection(ctx context.Context, connectionID string, message *Message) error {
	conn, err := r.GetConnection(connectionID)
	if err != nil {
		return err
	}

	if err := conn.Send(ctx, message); err != nil {
		return fmt.Errorf("failed to send to connection %s: %w", connectionID, err)
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.messages.direct").Inc()
	}

	return nil
}

// GetMessageHistory returns message history with optional filtering
func (r *DefaultRoom) GetMessageHistory(filter *MessageFilter) ([]*Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if filter == nil {
		// Return all messages
		messages := make([]*Message, len(r.messageHistory))
		copy(messages, r.messageHistory)
		return messages, nil
	}

	// Apply filters
	var filtered []*Message
	for _, msg := range r.messageHistory {
		if r.messageMatchesFilter(msg, filter) {
			filtered = append(filtered, msg)
		}
	}

	// Apply limit
	if filter.Limit > 0 && len(filtered) > filter.Limit {
		filtered = filtered[len(filtered)-filter.Limit:]
	}

	return filtered, nil
}

// messageMatchesFilter checks if a message matches the filter criteria
func (r *DefaultRoom) messageMatchesFilter(msg *Message, filter *MessageFilter) bool {
	// Check message types
	if len(filter.Types) > 0 {
		typeMatch := false
		for _, t := range filter.Types {
			if msg.Type == t {
				typeMatch = true
				break
			}
		}
		if !typeMatch {
			return false
		}
	}

	// Check from users
	if len(filter.FromUsers) > 0 {
		userMatch := false
		for _, u := range filter.FromUsers {
			if msg.From == u {
				userMatch = true
				break
			}
		}
		if !userMatch {
			return false
		}
	}

	// Check to users
	if len(filter.ToUsers) > 0 {
		userMatch := false
		for _, u := range filter.ToUsers {
			if msg.To == u {
				userMatch = true
				break
			}
		}
		if !userMatch {
			return false
		}
	}

	// Check time range
	if filter.Since != nil && msg.Timestamp.Before(*filter.Since) {
		return false
	}
	if filter.Until != nil && msg.Timestamp.After(*filter.Until) {
		return false
	}

	return true
}

// AddToHistory adds a message to the room's history
func (r *DefaultRoom) AddToHistory(message *Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Add message to history
	r.messageHistory = append(r.messageHistory, message)

	// Trim history if it exceeds max size
	if len(r.messageHistory) > r.config.MaxMessageHistory {
		// Remove oldest messages
		excess := len(r.messageHistory) - r.config.MaxMessageHistory
		r.messageHistory = r.messageHistory[excess:]
	}

	// Update stats
	r.stats.MessageHistory = len(r.messageHistory)

	return nil
}

// ClearHistory clears the message history
func (r *DefaultRoom) ClearHistory() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.messageHistory = make([]*Message, 0, r.config.MaxMessageHistory)
	r.stats.MessageHistory = 0

	if r.logger != nil {
		r.logger.Info("message history cleared", logger.String("room_id", r.id))
	}

	return nil
}

// GetPresence returns the presence tracker
func (r *DefaultRoom) GetPresence() PresenceTracker {
	return r.presence
}

// UpdateUserPresence updates a user's presence
func (r *DefaultRoom) UpdateUserPresence(ctx context.Context, userID string, status PresenceStatus, metadata map[string]interface{}) error {
	if r.presence == nil {
		return common.ErrServiceNotFound("presence tracker not enabled")
	}

	if !r.IsUserInRoom(userID) {
		return common.ErrServiceNotFound(fmt.Sprintf("user %s not in room %s", userID, r.id))
	}

	return r.presence.SetStatus(ctx, userID, r.id, status)
}

// Start starts the room
func (r *DefaultRoom) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.active {
		return nil // Already active
	}

	r.active = true
	r.stats.CreatedAt = time.Now()

	if r.logger != nil {
		r.logger.Info("room started", logger.String("room_id", r.id))
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.started").Inc()
	}

	return nil
}

// Stop stops the room
func (r *DefaultRoom) Stop(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.active {
		return nil // Already stopped
	}

	r.active = false

	if r.logger != nil {
		r.logger.Info("room stopped", logger.String("room_id", r.id))
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.stopped").Inc()
	}

	return nil
}

// Close closes the room and cleans up resources
func (r *DefaultRoom) Close(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close all connections
	for connectionID, conn := range r.connections {
		if err := conn.Close(ctx); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to close connection during room close",
					logger.String("connection_id", connectionID),
					logger.Error(err),
				)
			}
		}
	}

	// Clear all data
	r.connections = make(map[string]Connection)
	r.userConnections = make(map[string][]string)
	r.messageHistory = nil
	r.moderation = make(map[string]*UserModeration)

	// Stop presence tracker
	if localPresence, ok := r.presence.(*LocalPresenceTracker); ok {
		localPresence.Stop()
	}

	r.active = false

	// Signal cleanup
	close(r.stopCh)

	if r.logger != nil {
		r.logger.Info("room closed", logger.String("room_id", r.id))
	}

	if r.metrics != nil {
		r.metrics.Counter("streaming.room.closed").Inc()
	}

	return nil
}

// IsActive returns true if the room is active
func (r *DefaultRoom) IsActive() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}

// Moderation methods

// MuteUser mutes a user for a specified duration
func (r *DefaultRoom) MuteUser(ctx context.Context, userID string, duration time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	moderation := r.getOrCreateModerationUnsafe(userID)
	moderation.IsMuted = true
	moderation.MutedUntil = time.Now().Add(duration)

	if r.logger != nil {
		r.logger.Info("user muted",
			logger.String("user_id", userID),
			logger.String("room_id", r.id),
			logger.Duration("duration", duration),
		)
	}

	return nil
}

// UnmuteUser unmutes a user
func (r *DefaultRoom) UnmuteUser(ctx context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if moderation, exists := r.moderation[userID]; exists {
		moderation.IsMuted = false
		moderation.MutedUntil = time.Time{}
	}

	if r.logger != nil {
		r.logger.Info("user unmuted",
			logger.String("user_id", userID),
			logger.String("room_id", r.id),
		)
	}

	return nil
}

// IsUserMuted checks if a user is muted
func (r *DefaultRoom) IsUserMuted(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	moderation, exists := r.moderation[userID]
	if !exists || !moderation.IsMuted {
		return false
	}

	// Check if mute has expired
	if !moderation.MutedUntil.IsZero() && time.Now().After(moderation.MutedUntil) {
		// Mute has expired, unmute the user
		go func() {
			r.mu.Lock()
			moderation.IsMuted = false
			moderation.MutedUntil = time.Time{}
			r.mu.Unlock()
		}()
		return false
	}

	return true
}

// BanUser bans a user from the room
func (r *DefaultRoom) BanUser(ctx context.Context, userID string, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	moderation := r.getOrCreateModerationUnsafe(userID)
	moderation.IsBanned = true
	moderation.BannedAt = time.Now()
	moderation.BanReason = reason

	// Disconnect all user's connections
	if connectionIDs, exists := r.userConnections[userID]; exists {
		for _, connectionID := range connectionIDs {
			if conn, exists := r.connections[connectionID]; exists {
				go conn.Close(ctx)
			}
		}
	}

	if r.logger != nil {
		r.logger.Info("user banned",
			logger.String("user_id", userID),
			logger.String("room_id", r.id),
			logger.String("reason", reason),
		)
	}

	return nil
}

// UnbanUser unbans a user
func (r *DefaultRoom) UnbanUser(ctx context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if moderation, exists := r.moderation[userID]; exists {
		moderation.IsBanned = false
		moderation.BannedAt = time.Time{}
		moderation.BanReason = ""
	}

	if r.logger != nil {
		r.logger.Info("user unbanned",
			logger.String("user_id", userID),
			logger.String("room_id", r.id),
		)
	}

	return nil
}

// IsUserBanned checks if a user is banned
func (r *DefaultRoom) IsUserBanned(userID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isUserBannedUnsafe(userID)
}

// isUserBannedUnsafe checks if a user is banned (unsafe, requires lock)
func (r *DefaultRoom) isUserBannedUnsafe(userID string) bool {
	moderation, exists := r.moderation[userID]
	return exists && moderation.IsBanned
}

// getOrCreateModerationUnsafe gets or creates moderation entry (unsafe, requires lock)
func (r *DefaultRoom) getOrCreateModerationUnsafe(userID string) *UserModeration {
	if moderation, exists := r.moderation[userID]; exists {
		return moderation
	}

	moderation := &UserModeration{
		UserID: userID,
	}
	r.moderation[userID] = moderation
	return moderation
}

// Event handler setters

// OnUserJoin sets the user join handler
func (r *DefaultRoom) OnUserJoin(handler UserJoinHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userJoinHandler = handler
}

// OnUserLeave sets the user leave handler
func (r *DefaultRoom) OnUserLeave(handler UserLeaveHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.userLeaveHandler = handler
}

// OnMessage sets the message handler
func (r *DefaultRoom) OnMessage(handler RoomMessageHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messageHandler = handler
}

// OnError sets the error handler
func (r *DefaultRoom) OnError(handler RoomErrorHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorHandler = handler
}

// GetStats returns room statistics
func (r *DefaultRoom) GetStats() RoomStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Update dynamic stats
	r.stats.ConnectionCount = len(r.connections)
	r.stats.OnlineUserCount = len(r.userConnections)
	r.stats.MessageHistory = len(r.messageHistory)

	// Calculate messages per minute
	if !r.stats.CreatedAt.IsZero() {
		duration := time.Since(r.stats.CreatedAt)
		if duration.Minutes() > 0 {
			r.stats.MessagesPerMinute = float64(r.stats.TotalMessageCount) / duration.Minutes()
		}
	}

	// Return a copy to avoid race conditions
	return r.stats
}

// Helper functions

// generateRoomID generates a unique room ID
func generateRoomID(prefix string) string {
	return generateUniqueID(prefix)
}

// RoomType helpers

// String returns the string representation of the room type
func (rt RoomType) String() string {
	return string(rt)
}

// IsValid checks if the room type is valid
func (rt RoomType) IsValid() bool {
	switch rt {
	case RoomTypePublic, RoomTypePrivate, RoomTypeDirect, RoomTypeBroadcast, RoomTypeSystem:
		return true
	default:
		return false
	}
}

// RequiresInvitation returns true if the room type requires invitations
func (rt RoomType) RequiresInvitation() bool {
	return rt == RoomTypePrivate || rt == RoomTypeDirect
}

// AllowsMultipleUsers returns true if the room type allows multiple users
func (rt RoomType) AllowsMultipleUsers() bool {
	return rt != RoomTypeDirect
}

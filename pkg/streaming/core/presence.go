package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/pkg/common"
	"github.com/xraph/forge/pkg/logger"
)

// UserPresence represents a user's presence information
type UserPresence struct {
	UserID          string                 `json:"user_id"`
	Status          PresenceStatus         `json:"status"`
	LastSeen        time.Time              `json:"last_seen"`
	ConnectedAt     time.Time              `json:"connected_at"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	RoomID          string                 `json:"room_id,omitempty"`
	ConnectionCount int                    `json:"connection_count"`
}

// PresenceCallback is called when presence changes
type PresenceCallback func(presence *UserPresence, event PresenceEvent)

// PresenceEvent represents presence change events
type PresenceEvent string

const (
	PresenceEventJoin   PresenceEvent = "join"
	PresenceEventLeave  PresenceEvent = "leave"
	PresenceEventUpdate PresenceEvent = "update"
	PresenceEventExpire PresenceEvent = "expire"
)

// PresenceTracker manages user presence information
type PresenceTracker interface {
	// User presence management
	Join(ctx context.Context, userID, roomID string, metadata map[string]interface{}) error
	Leave(ctx context.Context, userID, roomID string) error
	Update(ctx context.Context, userID, roomID string, metadata map[string]interface{}) error
	SetStatus(ctx context.Context, userID, roomID string, status PresenceStatus) error

	// Presence queries
	GetUserPresence(userID, roomID string) (*UserPresence, error)
	GetRoomPresence(roomID string) ([]*UserPresence, error)
	GetOnlineUsers(roomID string) ([]*UserPresence, error)
	GetUserCount(roomID string) int
	GetOnlineCount(roomID string) int

	// Subscriptions
	Subscribe(callback PresenceCallback) string
	Unsubscribe(subscriptionID string)

	// Cleanup and maintenance
	CleanupExpired(ctx context.Context) error
	SetTTL(ttl time.Duration)

	// Statistics
	GetStats() PresenceStats
}

// PresenceStats represents presence tracker statistics
type PresenceStats struct {
	TotalUsers             int                    `json:"total_users"`
	OnlineUsers            int                    `json:"online_users"`
	UsersByStatus          map[PresenceStatus]int `json:"users_by_status"`
	UsersByRoom            map[string]int         `json:"users_by_room"`
	AverageSessionDuration time.Duration          `json:"average_session_duration"`
	Subscriptions          int                    `json:"subscriptions"`
	LastCleanup            time.Time              `json:"last_cleanup"`
}

// PresenceConfig contains configuration for presence tracking
type PresenceConfig struct {
	TTL               time.Duration `yaml:"ttl" default:"5m"`
	CleanupInterval   time.Duration `yaml:"cleanup_interval" default:"1m"`
	EnablePersistence bool          `yaml:"enable_persistence" default:"false"`
	RedisKeyPrefix    string        `yaml:"redis_key_prefix" default:"forge:presence"`
}

// DefaultPresenceConfig returns default presence configuration
func DefaultPresenceConfig() PresenceConfig {
	return PresenceConfig{
		TTL:               5 * time.Minute,
		CleanupInterval:   1 * time.Minute,
		EnablePersistence: false,
		RedisKeyPrefix:    "forge:presence",
	}
}

// LocalPresenceTracker implements in-memory presence tracking
type LocalPresenceTracker struct {
	presence      map[string]map[string]*UserPresence // roomID -> userID -> presence
	subscriptions map[string]PresenceCallback
	config        PresenceConfig
	mu            sync.RWMutex
	logger        common.Logger
	metrics       common.Metrics
	stopCh        chan struct{}
	cleanupTicker *time.Ticker
}

// NewLocalPresenceTracker creates a new local presence tracker
func NewLocalPresenceTracker(config PresenceConfig, logger common.Logger, metrics common.Metrics) PresenceTracker {
	tracker := &LocalPresenceTracker{
		presence:      make(map[string]map[string]*UserPresence),
		subscriptions: make(map[string]PresenceCallback),
		config:        config,
		logger:        logger,
		metrics:       metrics,
		stopCh:        make(chan struct{}),
	}

	// Start cleanup routine
	tracker.startCleanup()

	return tracker
}

// Join adds a user to a room's presence
func (pt *LocalPresenceTracker) Join(ctx context.Context, userID, roomID string, metadata map[string]interface{}) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.presence[roomID] == nil {
		pt.presence[roomID] = make(map[string]*UserPresence)
	}

	now := time.Now()
	presence := pt.presence[roomID][userID]

	if presence == nil {
		// New user joining
		presence = &UserPresence{
			UserID:          userID,
			Status:          PresenceOnline,
			LastSeen:        now,
			ConnectedAt:     now,
			Metadata:        metadata,
			RoomID:          roomID,
			ConnectionCount: 1,
		}
		pt.presence[roomID][userID] = presence

		// Notify subscribers
		pt.notifySubscribers(presence, PresenceEventJoin)

		if pt.logger != nil {
			pt.logger.Info("user joined room",
				logger.String("user_id", userID),
				logger.String("room_id", roomID),
			)
		}

		if pt.metrics != nil {
			pt.metrics.Counter("streaming.presence.joins").Inc()
		}
	} else {
		// Existing user, increment connection count
		presence.ConnectionCount++
		presence.LastSeen = now
		presence.Status = PresenceOnline
		if metadata != nil {
			for k, v := range metadata {
				presence.Metadata[k] = v
			}
		}

		// Notify subscribers
		pt.notifySubscribers(presence, PresenceEventUpdate)
	}

	return nil
}

// Leave removes a user from a room's presence
func (pt *LocalPresenceTracker) Leave(ctx context.Context, userID, roomID string) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.presence[roomID] == nil {
		return nil // Room doesn't exist
	}

	presence := pt.presence[roomID][userID]
	if presence == nil {
		return nil // User not in room
	}

	presence.ConnectionCount--
	presence.LastSeen = time.Now()

	if presence.ConnectionCount <= 0 {
		// Remove user completely
		delete(pt.presence[roomID], userID)

		// Clean up empty room
		if len(pt.presence[roomID]) == 0 {
			delete(pt.presence, roomID)
		}

		// Notify subscribers
		pt.notifySubscribers(presence, PresenceEventLeave)

		if pt.logger != nil {
			pt.logger.Info("user left room",
				logger.String("user_id", userID),
				logger.String("room_id", roomID),
			)
		}

		if pt.metrics != nil {
			pt.metrics.Counter("streaming.presence.leaves").Inc()
		}
	} else {
		// Still has other connections
		pt.notifySubscribers(presence, PresenceEventUpdate)
	}

	return nil
}

// Update updates a user's presence metadata
func (pt *LocalPresenceTracker) Update(ctx context.Context, userID, roomID string, metadata map[string]interface{}) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.presence[roomID] == nil || pt.presence[roomID][userID] == nil {
		return common.ErrServiceNotFound(fmt.Sprintf("user %s not found in room %s", userID, roomID))
	}

	presence := pt.presence[roomID][userID]
	presence.LastSeen = time.Now()

	if metadata != nil {
		if presence.Metadata == nil {
			presence.Metadata = make(map[string]interface{})
		}
		for k, v := range metadata {
			presence.Metadata[k] = v
		}
	}

	// Notify subscribers
	pt.notifySubscribers(presence, PresenceEventUpdate)

	if pt.metrics != nil {
		pt.metrics.Counter("streaming.presence.updates").Inc()
	}

	return nil
}

// SetStatus sets a user's presence status
func (pt *LocalPresenceTracker) SetStatus(ctx context.Context, userID, roomID string, status PresenceStatus) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	if pt.presence[roomID] == nil || pt.presence[roomID][userID] == nil {
		return common.ErrServiceNotFound(fmt.Sprintf("user %s not found in room %s", userID, roomID))
	}

	presence := pt.presence[roomID][userID]
	oldStatus := presence.Status
	presence.Status = status
	presence.LastSeen = time.Now()

	// Notify subscribers if status changed
	if oldStatus != status {
		pt.notifySubscribers(presence, PresenceEventUpdate)

		if pt.logger != nil {
			pt.logger.Debug("user status changed",
				logger.String("user_id", userID),
				logger.String("room_id", roomID),
				logger.String("old_status", string(oldStatus)),
				logger.String("new_status", string(status)),
			)
		}

		if pt.metrics != nil {
			pt.metrics.Counter("streaming.presence.status_changes").Inc()
		}
	}

	return nil
}

// GetUserPresence gets a specific user's presence in a room
func (pt *LocalPresenceTracker) GetUserPresence(userID, roomID string) (*UserPresence, error) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if pt.presence[roomID] == nil || pt.presence[roomID][userID] == nil {
		return nil, common.ErrServiceNotFound(fmt.Sprintf("user %s not found in room %s", userID, roomID))
	}

	// Return a copy to avoid race conditions
	presence := *pt.presence[roomID][userID]
	return &presence, nil
}

// GetRoomPresence gets all users' presence in a room
func (pt *LocalPresenceTracker) GetRoomPresence(roomID string) ([]*UserPresence, error) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if pt.presence[roomID] == nil {
		return []*UserPresence{}, nil
	}

	presences := make([]*UserPresence, 0, len(pt.presence[roomID]))
	for _, presence := range pt.presence[roomID] {
		// Return copies to avoid race conditions
		presenceCopy := *presence
		presences = append(presences, &presenceCopy)
	}

	return presences, nil
}

// GetOnlineUsers gets all online users in a room
func (pt *LocalPresenceTracker) GetOnlineUsers(roomID string) ([]*UserPresence, error) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if pt.presence[roomID] == nil {
		return []*UserPresence{}, nil
	}

	presences := make([]*UserPresence, 0)
	for _, presence := range pt.presence[roomID] {
		if presence.Status == PresenceOnline {
			// Return copies to avoid race conditions
			presenceCopy := *presence
			presences = append(presences, &presenceCopy)
		}
	}

	return presences, nil
}

// GetUserCount gets total number of users in a room
func (pt *LocalPresenceTracker) GetUserCount(roomID string) int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if pt.presence[roomID] == nil {
		return 0
	}

	return len(pt.presence[roomID])
}

// GetOnlineCount gets number of online users in a room
func (pt *LocalPresenceTracker) GetOnlineCount(roomID string) int {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if pt.presence[roomID] == nil {
		return 0
	}

	count := 0
	for _, presence := range pt.presence[roomID] {
		if presence.Status == PresenceOnline {
			count++
		}
	}

	return count
}

// Subscribe subscribes to presence changes
func (pt *LocalPresenceTracker) Subscribe(callback PresenceCallback) string {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	subscriptionID := generateUniqueID("sub")
	pt.subscriptions[subscriptionID] = callback

	if pt.metrics != nil {
		pt.metrics.Gauge("streaming.presence.subscriptions").Set(float64(len(pt.subscriptions)))
	}

	return subscriptionID
}

// Unsubscribe unsubscribes from presence changes
func (pt *LocalPresenceTracker) Unsubscribe(subscriptionID string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	delete(pt.subscriptions, subscriptionID)

	if pt.metrics != nil {
		pt.metrics.Gauge("streaming.presence.subscriptions").Set(float64(len(pt.subscriptions)))
	}
}

// notifySubscribers notifies all subscribers of presence changes
func (pt *LocalPresenceTracker) notifySubscribers(presence *UserPresence, event PresenceEvent) {
	// Don't hold the lock while calling callbacks
	var callbacks []PresenceCallback
	for _, callback := range pt.subscriptions {
		callbacks = append(callbacks, callback)
	}

	// Call callbacks without holding lock
	for _, callback := range callbacks {
		go func(cb PresenceCallback) {
			defer func() {
				if r := recover(); r != nil {
					if pt.logger != nil {
						pt.logger.Error("presence callback panicked",
							logger.String("panic", fmt.Sprintf("%v", r)),
						)
					}
				}
			}()
			cb(presence, event)
		}(callback)
	}
}

// CleanupExpired removes expired presence entries
func (pt *LocalPresenceTracker) CleanupExpired(ctx context.Context) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	now := time.Now()
	expireTime := now.Add(-pt.config.TTL)
	expiredCount := 0

	for roomID, roomPresence := range pt.presence {
		for userID, presence := range roomPresence {
			if presence.LastSeen.Before(expireTime) {
				delete(roomPresence, userID)
				expiredCount++

				// Notify subscribers
				pt.notifySubscribers(presence, PresenceEventExpire)

				if pt.logger != nil {
					pt.logger.Debug("presence expired",
						logger.String("user_id", userID),
						logger.String("room_id", roomID),
						logger.Time("last_seen", presence.LastSeen),
					)
				}
			}
		}

		// Clean up empty rooms
		if len(roomPresence) == 0 {
			delete(pt.presence, roomID)
		}
	}

	if pt.metrics != nil {
		pt.metrics.Counter("streaming.presence.expired").Add(float64(expiredCount))
	}

	if pt.logger != nil && expiredCount > 0 {
		pt.logger.Info("cleaned up expired presence",
			logger.Int("expired_count", expiredCount),
		)
	}

	return nil
}

// SetTTL sets the time-to-live for presence entries
func (pt *LocalPresenceTracker) SetTTL(ttl time.Duration) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.config.TTL = ttl
}

// GetStats returns presence tracker statistics
func (pt *LocalPresenceTracker) GetStats() PresenceStats {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	stats := PresenceStats{
		UsersByStatus: make(map[PresenceStatus]int),
		UsersByRoom:   make(map[string]int),
		Subscriptions: len(pt.subscriptions),
	}

	var totalUsers, onlineUsers int
	var totalDuration time.Duration
	now := time.Now()

	for roomID, roomPresence := range pt.presence {
		stats.UsersByRoom[roomID] = len(roomPresence)

		for _, presence := range roomPresence {
			totalUsers++
			stats.UsersByStatus[presence.Status]++

			if presence.Status == PresenceOnline {
				onlineUsers++
			}

			sessionDuration := now.Sub(presence.ConnectedAt)
			totalDuration += sessionDuration
		}
	}

	stats.TotalUsers = totalUsers
	stats.OnlineUsers = onlineUsers

	if totalUsers > 0 {
		stats.AverageSessionDuration = totalDuration / time.Duration(totalUsers)
	}

	return stats
}

// startCleanup starts the cleanup routine
func (pt *LocalPresenceTracker) startCleanup() {
	pt.cleanupTicker = time.NewTicker(pt.config.CleanupInterval)

	go func() {
		defer pt.cleanupTicker.Stop()

		for {
			select {
			case <-pt.cleanupTicker.C:
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				if err := pt.CleanupExpired(ctx); err != nil {
					if pt.logger != nil {
						pt.logger.Error("presence cleanup failed", logger.Error(err))
					}
				}
				cancel()

			case <-pt.stopCh:
				return
			}
		}
	}()
}

// Stop stops the presence tracker
func (pt *LocalPresenceTracker) Stop() {
	close(pt.stopCh)
	if pt.cleanupTicker != nil {
		pt.cleanupTicker.Stop()
	}
}

// PresenceEvent helpers

// String returns the string representation of the presence event
func (pe PresenceEvent) String() string {
	return string(pe)
}

// IsValid checks if the presence event is valid
func (pe PresenceEvent) IsValid() bool {
	switch pe {
	case PresenceEventJoin, PresenceEventLeave, PresenceEventUpdate, PresenceEventExpire:
		return true
	default:
		return false
	}
}

// PresenceStatus helpers

// String returns the string representation of the presence status
func (ps PresenceStatus) String() string {
	return string(ps)
}

// IsValid checks if the presence status is valid
func (ps PresenceStatus) IsValid() bool {
	switch ps {
	case PresenceOnline, PresenceOffline, PresenceAway, PresenceBusy, PresenceInvisible:
		return true
	default:
		return false
	}
}

// IsOnline returns true if the status indicates the user is online
func (ps PresenceStatus) IsOnline() bool {
	return ps == PresenceOnline
}

// IsVisible returns true if the status is visible to others
func (ps PresenceStatus) IsVisible() bool {
	return ps != PresenceInvisible && ps != PresenceOffline
}

// UserPresence helpers

// Clone creates a deep copy of the user presence
func (up *UserPresence) Clone() *UserPresence {
	clone := &UserPresence{
		UserID:          up.UserID,
		Status:          up.Status,
		LastSeen:        up.LastSeen,
		ConnectedAt:     up.ConnectedAt,
		RoomID:          up.RoomID,
		ConnectionCount: up.ConnectionCount,
		Metadata:        make(map[string]interface{}),
	}

	for k, v := range up.Metadata {
		clone.Metadata[k] = v
	}

	return clone
}

// IsExpired checks if the presence is expired based on TTL
func (up *UserPresence) IsExpired(ttl time.Duration) bool {
	return time.Since(up.LastSeen) > ttl
}

// SessionDuration returns the duration of the current session
func (up *UserPresence) SessionDuration() time.Duration {
	return time.Since(up.ConnectedAt)
}

// ToJSON converts the presence to JSON
func (up *UserPresence) ToJSON() ([]byte, error) {
	return json.Marshal(up)
}

// UserPresenceFromJSON FromJSON creates a UserPresence from JSON
func UserPresenceFromJSON(data []byte) (*UserPresence, error) {
	var presence UserPresence
	err := json.Unmarshal(data, &presence)
	return &presence, err
}

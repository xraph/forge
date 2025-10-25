package trackers

import (
	"context"
	"time"

	"github.com/xraph/forge"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// presenceTracker implements streaming.PresenceTracker.
type presenceTracker struct {
	store   streaming.PresenceStore
	options streaming.PresenceOptions
	manager streaming.Manager // For broadcasting presence changes
	logger  forge.Logger
	metrics forge.Metrics

	// Cleanup
	stopCleanup chan struct{}
}

// NewPresenceTracker creates a new presence tracker.
func NewPresenceTracker(
	store streaming.PresenceStore,
	options streaming.PresenceOptions,
	logger forge.Logger,
	metrics forge.Metrics,
) streaming.PresenceTracker {
	return &presenceTracker{
		store:       store,
		options:     options,
		logger:      logger,
		metrics:     metrics,
		stopCleanup: make(chan struct{}),
	}
}

func (pt *presenceTracker) SetPresence(ctx context.Context, userID, status string) error {
	// Validate status
	if status != streaming.StatusOnline &&
		status != streaming.StatusAway &&
		status != streaming.StatusBusy &&
		status != streaming.StatusOffline {
		return streaming.ErrInvalidStatus
	}

	// Get existing presence or create new
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		if err == streaming.ErrPresenceNotFound {
			presence = &streaming.UserPresence{
				UserID:      userID,
				Status:      status,
				LastSeen:    time.Now(),
				Connections: []string{},
				Metadata:    make(map[string]any),
			}
		} else {
			return err
		}
	}

	presence.Status = status
	presence.LastSeen = time.Now()

	// Save to store
	if err := pt.store.Set(ctx, userID, presence); err != nil {
		return err
	}

	// Update online status
	if status == streaming.StatusOnline {
		if err := pt.store.SetOnline(ctx, userID, pt.options.OfflineTimeout); err != nil {
			return err
		}
	} else if status == streaming.StatusOffline {
		if err := pt.store.SetOffline(ctx, userID); err != nil {
			return err
		}
	}

	// Track metrics
	if pt.metrics != nil {
		pt.metrics.Counter("streaming.presence.updates").Inc()
		pt.metrics.Gauge("streaming.presence." + status).Set(1.0)
	}

	if pt.logger != nil {
		pt.logger.Debug("presence updated",
			forge.F("user_id", userID),
			forge.F("status", status),
		)
	}

	return nil
}

func (pt *presenceTracker) GetPresence(ctx context.Context, userID string) (*streaming.UserPresence, error) {
	return pt.store.Get(ctx, userID)
}

func (pt *presenceTracker) GetPresences(ctx context.Context, userIDs []string) ([]*streaming.UserPresence, error) {
	return pt.store.GetMultiple(ctx, userIDs)
}

func (pt *presenceTracker) TrackActivity(ctx context.Context, userID string) error {
	if err := pt.store.UpdateActivity(ctx, userID, time.Now()); err != nil {
		return err
	}

	// Refresh online status
	if err := pt.store.SetOnline(ctx, userID, pt.options.OfflineTimeout); err != nil {
		return err
	}

	return nil
}

func (pt *presenceTracker) GetLastSeen(ctx context.Context, userID string) (time.Time, error) {
	return pt.store.GetLastActivity(ctx, userID)
}

func (pt *presenceTracker) GetOnlineUsers(ctx context.Context) ([]string, error) {
	return pt.store.GetOnline(ctx)
}

func (pt *presenceTracker) GetOnlineUsersInRoom(ctx context.Context, roomID string) ([]string, error) {
	// This requires cross-referencing with room membership
	// For now, return all online users
	// TODO: Filter by room membership
	return pt.store.GetOnline(ctx)
}

func (pt *presenceTracker) IsOnline(ctx context.Context, userID string) (bool, error) {
	return pt.store.IsOnline(ctx, userID)
}

func (pt *presenceTracker) SetCustomStatus(ctx context.Context, userID, customStatus string) error {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return err
	}

	presence.CustomStatus = customStatus
	return pt.store.Set(ctx, userID, presence)
}

func (pt *presenceTracker) GetCustomStatus(ctx context.Context, userID string) (string, error) {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return "", err
	}

	return presence.CustomStatus, nil
}

func (pt *presenceTracker) BroadcastPresence(ctx context.Context, roomID, userID, status string) error {
	// Broadcasting is handled by the manager
	// This is a placeholder for future implementation
	return nil
}

func (pt *presenceTracker) CleanupExpired(ctx context.Context) error {
	return pt.store.CleanupExpired(ctx, pt.options.OfflineTimeout)
}

func (pt *presenceTracker) Start(ctx context.Context) error {
	// Start background cleanup goroutine
	go pt.cleanupLoop()

	if pt.logger != nil {
		pt.logger.Info("presence tracker started")
	}

	return nil
}

func (pt *presenceTracker) Stop(ctx context.Context) error {
	close(pt.stopCleanup)

	if pt.logger != nil {
		pt.logger.Info("presence tracker stopped")
	}

	return nil
}

func (pt *presenceTracker) cleanupLoop() {
	ticker := time.NewTicker(pt.options.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx := context.Background()
			if err := pt.CleanupExpired(ctx); err != nil {
				if pt.logger != nil {
					pt.logger.Error("failed to cleanup expired presence",
						forge.F("error", err),
					)
				}
			}

		case <-pt.stopCleanup:
			return
		}
	}
}

// Bulk Operations
func (pt *presenceTracker) SetPresenceForUsers(ctx context.Context, updates map[string]string) error {
	presences := make(map[string]*streaming.UserPresence)
	
	for userID, status := range updates {
		presence := &streaming.UserPresence{
			UserID:      userID,
			Status:      status,
			LastSeen:    time.Now(),
			Connections: []string{},
			Metadata:    make(map[string]any),
		}
		presences[userID] = presence
	}
	
	return pt.store.SetMultiple(ctx, presences)
}

func (pt *presenceTracker) GetPresenceForRooms(ctx context.Context, roomIDs []string) (map[string][]*streaming.UserPresence, error) {
	// This requires cross-referencing with room membership
	// For now, return empty map
	// TODO: Implement room-based presence filtering
	return make(map[string][]*streaming.UserPresence), nil
}

func (pt *presenceTracker) GetPresenceBulk(ctx context.Context, userIDs []string) (map[string]*streaming.UserPresence, error) {
	presences, err := pt.store.GetMultiple(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	
	result := make(map[string]*streaming.UserPresence)
	for _, presence := range presences {
		result[presence.UserID] = presence
	}
	
	return result, nil
}

// Advanced Queries
func (pt *presenceTracker) GetUsersByStatus(ctx context.Context, status string) ([]string, error) {
	presences, err := pt.store.GetByStatus(ctx, status)
	if err != nil {
		return nil, err
	}
	
	userIDs := make([]string, len(presences))
	for i, presence := range presences {
		userIDs[i] = presence.UserID
	}
	
	return userIDs, nil
}

func (pt *presenceTracker) GetRecentlyOnline(ctx context.Context, since time.Duration) ([]string, error) {
	presences, err := pt.store.GetRecent(ctx, streaming.StatusOnline, since)
	if err != nil {
		return nil, err
	}
	
	userIDs := make([]string, len(presences))
	for i, presence := range presences {
		userIDs[i] = presence.UserID
	}
	
	return userIDs, nil
}

func (pt *presenceTracker) GetRecentlyOffline(ctx context.Context, since time.Duration) ([]string, error) {
	presences, err := pt.store.GetRecent(ctx, streaming.StatusOffline, since)
	if err != nil {
		return nil, err
	}
	
	userIDs := make([]string, len(presences))
	for i, presence := range presences {
		userIDs[i] = presence.UserID
	}
	
	return userIDs, nil
}

func (pt *presenceTracker) GetAwayUsers(ctx context.Context) ([]string, error) {
	return pt.GetUsersByStatus(ctx, streaming.StatusAway)
}

func (pt *presenceTracker) GetBusyUsers(ctx context.Context) ([]string, error) {
	return pt.GetUsersByStatus(ctx, streaming.StatusBusy)
}

// Presence History
func (pt *presenceTracker) GetPresenceHistory(ctx context.Context, userID string, since time.Time) ([]*streaming.PresenceEvent, error) {
	return pt.store.GetHistorySince(ctx, userID, since)
}

func (pt *presenceTracker) GetStatusChanges(ctx context.Context, userID string, limit int) ([]*streaming.PresenceEvent, error) {
	return pt.store.GetHistory(ctx, userID, limit)
}

// Device Management
func (pt *presenceTracker) AddDevice(ctx context.Context, userID, deviceID string, deviceInfo streaming.DeviceInfo) error {
	return pt.store.SetDevice(ctx, userID, deviceID, deviceInfo)
}

func (pt *presenceTracker) RemoveDevice(ctx context.Context, userID, deviceID string) error {
	return pt.store.RemoveDevice(ctx, userID, deviceID)
}

func (pt *presenceTracker) GetDevices(ctx context.Context, userID string) ([]streaming.DeviceInfo, error) {
	return pt.store.GetDevices(ctx, userID)
}

func (pt *presenceTracker) GetActiveDevices(ctx context.Context, userID string) ([]streaming.DeviceInfo, error) {
	devices, err := pt.store.GetDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	
	var activeDevices []streaming.DeviceInfo
	for _, device := range devices {
		if device.Active {
			activeDevices = append(activeDevices, device)
		}
	}
	
	return activeDevices, nil
}

// Rich Presence
func (pt *presenceTracker) SetRichPresence(ctx context.Context, userID string, richData map[string]any) error {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return err
	}
	
	if presence.Metadata == nil {
		presence.Metadata = make(map[string]any)
	}
	
	presence.Metadata["rich_presence"] = richData
	return pt.store.Set(ctx, userID, presence)
}

func (pt *presenceTracker) GetRichPresence(ctx context.Context, userID string) (map[string]any, error) {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	
	if richData, exists := presence.Metadata["rich_presence"]; exists {
		if richMap, ok := richData.(map[string]any); ok {
			return richMap, nil
		}
	}
	
	return make(map[string]any), nil
}

func (pt *presenceTracker) SetActivity(ctx context.Context, userID string, activity *streaming.ActivityInfo) error {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return err
	}
	
	if presence.Metadata == nil {
		presence.Metadata = make(map[string]any)
	}
	
	presence.Metadata["activity"] = activity
	return pt.store.Set(ctx, userID, presence)
}

func (pt *presenceTracker) GetActivity(ctx context.Context, userID string) (*streaming.ActivityInfo, error) {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	
	if activityData, exists := presence.Metadata["activity"]; exists {
		if activity, ok := activityData.(*streaming.ActivityInfo); ok {
			return activity, nil
		}
	}
	
	return nil, nil
}

// Availability
func (pt *presenceTracker) SetAvailability(ctx context.Context, userID string, available bool, message string) error {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return err
	}
	
	if presence.Metadata == nil {
		presence.Metadata = make(map[string]any)
	}
	
	availability := &streaming.Availability{
		Available: available,
		Message:   message,
		UpdatedAt: time.Now(),
	}
	
	presence.Metadata["availability"] = availability
	return pt.store.Set(ctx, userID, presence)
}

func (pt *presenceTracker) IsAvailable(ctx context.Context, userID string) (bool, string, error) {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return false, "", err
	}
	
	if availabilityData, exists := presence.Metadata["availability"]; exists {
		if availability, ok := availabilityData.(*streaming.Availability); ok {
			return availability.Available, availability.Message, nil
		}
	}
	
	return true, "", nil // Default to available
}

// Time-based
func (pt *presenceTracker) GetOnlineUsersAt(ctx context.Context, timestamp time.Time) ([]string, error) {
	// This would require historical data
	// For now, return current online users
	// TODO: Implement historical presence queries
	return pt.store.GetOnline(ctx)
}

func (pt *presenceTracker) GetPresenceDuration(ctx context.Context, userID string, status string, since time.Time) (time.Duration, error) {
	events, err := pt.store.GetHistorySince(ctx, userID, since)
	if err != nil {
		return 0, err
	}
	
	var duration time.Duration
	var lastStatusChange time.Time = since
	var currentStatus string
	
	for _, event := range events {
		if event.Type == "status_change" {
			if currentStatus == status && !lastStatusChange.IsZero() {
				duration += event.Timestamp.Sub(lastStatusChange)
			}
			currentStatus = event.Status
			lastStatusChange = event.Timestamp
		}
	}
	
	// Add duration from last status change to now if still in the target status
	if currentStatus == status && !lastStatusChange.IsZero() {
		duration += time.Now().Sub(lastStatusChange)
	}
	
	return duration, nil
}

// Watching/Subscriptions - placeholder implementations
func (pt *presenceTracker) WatchPresence(ctx context.Context, userID string, callback func(*streaming.UserPresence)) error {
	// TODO: Implement presence watching
	return nil
}

func (pt *presenceTracker) UnwatchPresence(ctx context.Context, userID string) error {
	// TODO: Implement presence unwatching
	return nil
}

func (pt *presenceTracker) SubscribeToPresenceChanges(ctx context.Context, filter streaming.PresenceFilters, callback func(*streaming.PresenceEvent)) error {
	// TODO: Implement presence change subscriptions
	return nil
}

func (pt *presenceTracker) UnsubscribeFromPresenceChanges(ctx context.Context, subscriptionID string) error {
	// TODO: Implement presence change unsubscriptions
	return nil
}

// Statistics
func (pt *presenceTracker) GetOnlineStats(ctx context.Context) (*streaming.OnlineStats, error) {
	onlineUsers, err := pt.store.GetOnline(ctx)
	if err != nil {
		return nil, err
	}
	
	// Get status counts
	statusCounts, err := pt.store.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	
	stats := &streaming.OnlineStats{
		Current:    len(onlineUsers),
		Peak24h:    len(onlineUsers), // TODO: Track actual peak
		Average24h: float64(len(onlineUsers)), // TODO: Calculate actual average
		ByStatus:   statusCounts,
		Trend:      "stable", // TODO: Calculate actual trend
	}
	
	return stats, nil
}

func (pt *presenceTracker) GetPresenceStats(ctx context.Context, userID string) (*streaming.UserPresenceStats, error) {
	presence, err := pt.store.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	
	devices, err := pt.store.GetDevices(ctx, userID)
	if err != nil {
		return nil, err
	}
	
	// Get history for calculating stats
	history, err := pt.store.GetHistory(ctx, userID, 100)
	if err != nil {
		return nil, err
	}
	
	stats := &streaming.UserPresenceStats{
		TotalOnlineTime:   0, // TODO: Calculate from history
		AverageOnlineTime: 0, // TODO: Calculate from history
		LastOnline:        presence.LastSeen,
		StatusChanges:     len(history),
		MostCommonStatus:  presence.Status,
		DeviceCount:       len(devices),
		Metadata:          presence.Metadata,
	}
	
	return stats, nil
}

// Watching methods - placeholder implementations
func (pt *presenceTracker) WatchUser(ctx context.Context, watcherID, watchedUserID string) error {
	// TODO: Implement user watching
	return nil
}

func (pt *presenceTracker) UnwatchUser(ctx context.Context, watcherID, watchedUserID string) error {
	// TODO: Implement user unwatching
	return nil
}

func (pt *presenceTracker) GetWatchers(ctx context.Context, userID string) ([]string, error) {
	// TODO: Implement getting watchers
	return []string{}, nil
}

func (pt *presenceTracker) GetWatching(ctx context.Context, userID string) ([]string, error) {
	// TODO: Implement getting watched users
	return []string{}, nil
}

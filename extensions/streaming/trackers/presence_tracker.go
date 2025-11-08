package trackers

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/xraph/forge"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
	"github.com/xraph/forge/internal/errors"
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

	// Watching functionality
	watchers        map[string][]string // userID -> []watcherID
	watchersMu      sync.RWMutex
	subscriptions   map[string]func(*streaming.PresenceEvent) // subscriptionID -> callback
	subscriptionsMu sync.RWMutex
}

// NewPresenceTracker creates a new presence tracker.
func NewPresenceTracker(
	store streaming.PresenceStore,
	options streaming.PresenceOptions,
	logger forge.Logger,
	metrics forge.Metrics,
) streaming.PresenceTracker {
	return &presenceTracker{
		store:         store,
		options:       options,
		logger:        logger,
		metrics:       metrics,
		stopCleanup:   make(chan struct{}),
		watchers:      make(map[string][]string),
		subscriptions: make(map[string]func(*streaming.PresenceEvent)),
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
		if errors.Is(err, streaming.ErrPresenceNotFound) {
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
	switch status {
	case streaming.StatusOnline:
		if err := pt.store.SetOnline(ctx, userID, pt.options.OfflineTimeout); err != nil {
			return err
		}
	case streaming.StatusOffline:
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

// Bulk Operations.
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
	result := make(map[string][]*streaming.UserPresence)

	// For each room, get the online users in that room
	for _, roomID := range roomIDs {
		onlineUsers, err := pt.GetOnlineUsersInRoom(ctx, roomID)
		if err != nil {
			// If we can't get users for a room, continue with empty list
			result[roomID] = []*streaming.UserPresence{}

			continue
		}

		// Get presence for each online user
		var presences []*streaming.UserPresence

		for _, userID := range onlineUsers {
			presence, err := pt.GetPresence(ctx, userID)
			if err != nil {
				continue // Skip users without presence
			}

			presences = append(presences, presence)
		}

		result[roomID] = presences
	}

	return result, nil
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

// Advanced Queries.
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

// Presence History.
func (pt *presenceTracker) GetPresenceHistory(ctx context.Context, userID string, since time.Time) ([]*streaming.PresenceEvent, error) {
	return pt.store.GetHistorySince(ctx, userID, since)
}

func (pt *presenceTracker) GetStatusChanges(ctx context.Context, userID string, limit int) ([]*streaming.PresenceEvent, error) {
	return pt.store.GetHistory(ctx, userID, limit)
}

// Device Management.
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

// Rich Presence.
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

// Availability.
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

// Time-based.
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

	var (
		duration         time.Duration
		lastStatusChange = since
		currentStatus    string
	)

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
		duration += time.Since(lastStatusChange)
	}

	return duration, nil
}

// Watching/Subscriptions - placeholder implementations.
func (pt *presenceTracker) WatchPresence(ctx context.Context, userID string, callback func(*streaming.UserPresence)) error {
	// For now, this is a placeholder implementation
	// In a real implementation, you'd set up a subscription to presence changes
	// and call the callback when the user's presence changes
	if pt.logger != nil {
		pt.logger.Debug("presence watching requested",
			forge.F("user_id", userID),
		)
	}

	return nil
}

func (pt *presenceTracker) UnwatchPresence(ctx context.Context, userID string) error {
	// For now, this is a placeholder implementation
	// In a real implementation, you'd remove the subscription
	if pt.logger != nil {
		pt.logger.Debug("presence unwatching requested",
			forge.F("user_id", userID),
		)
	}

	return nil
}

func (pt *presenceTracker) SubscribeToPresenceChanges(ctx context.Context, filter streaming.PresenceFilters, callback func(*streaming.PresenceEvent)) error {
	// Generate a unique subscription ID
	subscriptionID := fmt.Sprintf("presence_%d", time.Now().UnixNano())

	// Store the subscription
	pt.subscriptionsMu.Lock()
	pt.subscriptions[subscriptionID] = callback
	pt.subscriptionsMu.Unlock()

	if pt.logger != nil {
		pt.logger.Debug("presence change subscription created",
			forge.F("subscription_id", subscriptionID),
			forge.F("filter", filter),
		)
	}

	return nil
}

func (pt *presenceTracker) UnsubscribeFromPresenceChanges(ctx context.Context, subscriptionID string) error {
	// Remove the subscription
	pt.subscriptionsMu.Lock()
	delete(pt.subscriptions, subscriptionID)
	pt.subscriptionsMu.Unlock()

	if pt.logger != nil {
		pt.logger.Debug("presence change subscription removed",
			forge.F("subscription_id", subscriptionID),
		)
	}

	return nil
}

// Statistics.
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

	// Calculate basic statistics
	current := len(onlineUsers)

	// For now, use current as peak and average (in real implementation, you'd track these over time)
	peak24h := current
	average24h := float64(current)

	// Determine trend based on current vs previous (simplified)
	trend := "stable"
	if current > 0 {
		trend = "growing"
	} else if current == 0 {
		trend = "declining"
	}

	stats := &streaming.OnlineStats{
		Current:    current,
		Peak24h:    peak24h,
		Average24h: average24h,
		ByStatus:   statusCounts,
		Trend:      trend,
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

	// Calculate basic statistics from history
	totalOnlineTime := time.Duration(0)
	statusChanges := len(history)
	mostCommonStatus := presence.Status

	// Count status changes by type
	statusCounts := make(map[string]int)
	for _, event := range history {
		statusCounts[event.Status]++
	}

	// Find most common status
	maxCount := 0
	for status, count := range statusCounts {
		if count > maxCount {
			maxCount = count
			mostCommonStatus = status
		}
	}

	// Calculate average online time (simplified)
	averageOnlineTime := time.Duration(0)
	if statusChanges > 0 {
		averageOnlineTime = totalOnlineTime / time.Duration(statusChanges)
	}

	stats := &streaming.UserPresenceStats{
		TotalOnlineTime:   totalOnlineTime,
		AverageOnlineTime: averageOnlineTime,
		LastOnline:        presence.LastSeen,
		StatusChanges:     statusChanges,
		MostCommonStatus:  mostCommonStatus,
		DeviceCount:       len(devices),
		Metadata:          presence.Metadata,
	}

	return stats, nil
}

// Watching methods - placeholder implementations.
func (pt *presenceTracker) WatchUser(ctx context.Context, watcherID, watchedUserID string) error {
	pt.watchersMu.Lock()
	defer pt.watchersMu.Unlock()

	// Add watcher to the list
	if watchers, exists := pt.watchers[watchedUserID]; exists {
		// Check if already watching
		if slices.Contains(watchers, watcherID) {
			return nil // Already watching
		}

		pt.watchers[watchedUserID] = append(watchers, watcherID)
	} else {
		pt.watchers[watchedUserID] = []string{watcherID}
	}

	if pt.logger != nil {
		pt.logger.Debug("user watching started",
			forge.F("watcher_id", watcherID),
			forge.F("watched_user_id", watchedUserID),
		)
	}

	return nil
}

func (pt *presenceTracker) UnwatchUser(ctx context.Context, watcherID, watchedUserID string) error {
	pt.watchersMu.Lock()
	defer pt.watchersMu.Unlock()

	// Remove watcher from the list
	if watchers, exists := pt.watchers[watchedUserID]; exists {
		for i, w := range watchers {
			if w == watcherID {
				pt.watchers[watchedUserID] = append(watchers[:i], watchers[i+1:]...)

				break
			}
		}
		// Clean up empty lists
		if len(pt.watchers[watchedUserID]) == 0 {
			delete(pt.watchers, watchedUserID)
		}
	}

	if pt.logger != nil {
		pt.logger.Debug("user watching stopped",
			forge.F("watcher_id", watcherID),
			forge.F("watched_user_id", watchedUserID),
		)
	}

	return nil
}

func (pt *presenceTracker) GetWatchers(ctx context.Context, userID string) ([]string, error) {
	pt.watchersMu.RLock()
	defer pt.watchersMu.RUnlock()

	watchers, exists := pt.watchers[userID]
	if !exists {
		return []string{}, nil
	}

	// Return a copy to avoid race conditions
	result := make([]string, len(watchers))
	copy(result, watchers)

	return result, nil
}

func (pt *presenceTracker) GetWatching(ctx context.Context, userID string) ([]string, error) {
	pt.watchersMu.RLock()
	defer pt.watchersMu.RUnlock()

	var watching []string

	// Find all users that the given userID is watching
	for watchedUserID, watchers := range pt.watchers {
		if slices.Contains(watchers, userID) {
			watching = append(watching, watchedUserID)
		}
	}

	return watching, nil
}

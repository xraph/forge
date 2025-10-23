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

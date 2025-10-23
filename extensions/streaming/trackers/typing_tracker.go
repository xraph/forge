package trackers

import (
	"context"
	"time"

	"github.com/xraph/forge"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// typingTracker implements streaming.TypingTracker.
type typingTracker struct {
	store   streaming.TypingStore
	options streaming.TypingOptions
	logger  forge.Logger
	metrics forge.Metrics

	// Cleanup
	stopCleanup chan struct{}
}

// NewTypingTracker creates a new typing tracker.
func NewTypingTracker(
	store streaming.TypingStore,
	options streaming.TypingOptions,
	logger forge.Logger,
	metrics forge.Metrics,
) streaming.TypingTracker {
	return &typingTracker{
		store:       store,
		options:     options,
		logger:      logger,
		metrics:     metrics,
		stopCleanup: make(chan struct{}),
	}
}

func (tt *typingTracker) StartTyping(ctx context.Context, userID, roomID string) error {
	expiresAt := time.Now().Add(tt.options.TypingTimeout)

	if err := tt.store.SetTyping(ctx, userID, roomID, expiresAt); err != nil {
		return err
	}

	// Track metrics
	if tt.metrics != nil {
		tt.metrics.Counter("streaming.typing.started").Inc()
	}

	if tt.logger != nil {
		tt.logger.Debug("typing started",
			forge.F("user_id", userID),
			forge.F("room_id", roomID),
		)
	}

	return nil
}

func (tt *typingTracker) StopTyping(ctx context.Context, userID, roomID string) error {
	if err := tt.store.RemoveTyping(ctx, userID, roomID); err != nil {
		return err
	}

	// Track metrics
	if tt.metrics != nil {
		tt.metrics.Counter("streaming.typing.stopped").Inc()
	}

	if tt.logger != nil {
		tt.logger.Debug("typing stopped",
			forge.F("user_id", userID),
			forge.F("room_id", roomID),
		)
	}

	return nil
}

func (tt *typingTracker) GetTypingUsers(ctx context.Context, roomID string) ([]string, error) {
	users, err := tt.store.GetTypingUsers(ctx, roomID)
	if err != nil {
		return nil, err
	}

	// Enforce max typing users limit
	if len(users) > tt.options.MaxTypingUsers {
		users = users[:tt.options.MaxTypingUsers]
	}

	return users, nil
}

func (tt *typingTracker) IsTyping(ctx context.Context, userID, roomID string) (bool, error) {
	return tt.store.IsTyping(ctx, userID, roomID)
}

func (tt *typingTracker) BroadcastTyping(ctx context.Context, roomID, userID string, isTyping bool) error {
	// Broadcasting is handled by the manager
	// This is a placeholder for future implementation
	return nil
}

func (tt *typingTracker) CleanupExpired(ctx context.Context) error {
	return tt.store.CleanupExpired(ctx)
}

func (tt *typingTracker) Start(ctx context.Context) error {
	// Start background cleanup goroutine
	go tt.cleanupLoop()

	if tt.logger != nil {
		tt.logger.Info("typing tracker started")
	}

	return nil
}

func (tt *typingTracker) Stop(ctx context.Context) error {
	close(tt.stopCleanup)

	if tt.logger != nil {
		tt.logger.Info("typing tracker stopped")
	}

	return nil
}

func (tt *typingTracker) cleanupLoop() {
	ticker := time.NewTicker(tt.options.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx := context.Background()
			if err := tt.CleanupExpired(ctx); err != nil {
				if tt.logger != nil {
					tt.logger.Error("failed to cleanup expired typing indicators",
						forge.F("error", err),
					)
				}
			}

		case <-tt.stopCleanup:
			return
		}
	}
}

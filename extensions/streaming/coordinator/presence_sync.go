package coordinator

import (
	"context"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// PresenceSynchronizer synchronizes presence across nodes.
type PresenceSynchronizer struct {
	coordinator StreamCoordinator
	store       streaming.PresenceStore
	interval    time.Duration
	stopCh      chan struct{}
	running     bool
}

// NewPresenceSynchronizer creates a presence synchronizer.
func NewPresenceSynchronizer(
	coordinator StreamCoordinator,
	store streaming.PresenceStore,
	interval time.Duration,
) *PresenceSynchronizer {
	return &PresenceSynchronizer{
		coordinator: coordinator,
		store:       store,
		interval:    interval,
		stopCh:      make(chan struct{}),
	}
}

// Start begins periodic presence sync.
func (ps *PresenceSynchronizer) Start(ctx context.Context) error {
	if ps.running {
		return nil
	}

	ps.running = true
	go ps.syncLoop(ctx)

	return nil
}

// Stop stops the synchronizer.
func (ps *PresenceSynchronizer) Stop(ctx context.Context) error {
	if !ps.running {
		return nil
	}

	close(ps.stopCh)
	ps.running = false

	return nil
}

// SyncUserPresence syncs single user.
func (ps *PresenceSynchronizer) SyncUserPresence(ctx context.Context, userID string) error {
	presence, err := ps.store.Get(ctx, userID)
	if err != nil {
		return err
	}

	if presence == nil {
		return nil
	}

	return ps.coordinator.SyncPresence(ctx, presence)
}

// OnPresenceChange broadcasts presence change to all nodes.
func (ps *PresenceSynchronizer) OnPresenceChange(ctx context.Context, event *streaming.PresenceEvent) error {
	// Get current presence
	presence, err := ps.store.Get(ctx, event.UserID)
	if err != nil {
		return err
	}

	if presence == nil {
		return nil
	}

	// Broadcast to coordinator
	return ps.coordinator.SyncPresence(ctx, presence)
}

func (ps *PresenceSynchronizer) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(ps.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ps.stopCh:
			return
		case <-ticker.C:
			ps.syncAllPresence(ctx)
		}
	}
}

func (ps *PresenceSynchronizer) syncAllPresence(ctx context.Context) {
	// Get all online users
	users, err := ps.store.GetOnline(ctx)
	if err != nil {
		// Log error
		return
	}

	// Sync each user's presence
	for _, userID := range users {
		if err := ps.SyncUserPresence(ctx, userID); err != nil {
			// Log error but continue
			continue
		}
	}
}

// HandlePresenceUpdate handles incoming presence updates from coordinator.
func (ps *PresenceSynchronizer) HandlePresenceUpdate(ctx context.Context, presence *streaming.UserPresence) error {
	// Update local store
	return ps.store.Set(ctx, presence.UserID, presence)
}

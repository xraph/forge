package coordinator

import (
	"context"
	"sync"
	"time"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// PresenceSynchronizer synchronizes presence across nodes.
type PresenceSynchronizer struct {
	coordinator StreamCoordinator
	store       streaming.PresenceStore
	interval    time.Duration

	// mu guards stopCh and running. They were previously read and written
	// without synchronisation, so a Start racing a Stop was a data race.
	mu      sync.Mutex
	stopCh  chan struct{}
	running bool
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
	}
}

// Start begins periodic presence sync. Calling it on an already-running
// synchronizer is a no-op.
//
// A fresh stop channel is allocated per run. Reusing one across runs meant a
// restarted sync loop saw the channel still closed from the previous Stop and
// exited immediately, and the next Stop then closed an already-closed channel
// and panicked.
func (ps *PresenceSynchronizer) Start(ctx context.Context) error {
	ps.mu.Lock()

	if ps.running {
		ps.mu.Unlock()

		return nil
	}

	ps.running = true
	ps.stopCh = make(chan struct{})
	stopCh := ps.stopCh

	ps.mu.Unlock()

	go ps.syncLoop(ctx, stopCh)

	return nil
}

// Stop stops the synchronizer. It is idempotent and safe to call before Start.
func (ps *PresenceSynchronizer) Stop(ctx context.Context) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()

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

// syncLoop drains the ticker until the context is cancelled or this run's stop
// channel closes. The channel is passed in rather than read off the struct so
// the loop watches the run it belongs to, not whatever a later Start installed.
func (ps *PresenceSynchronizer) syncLoop(ctx context.Context, stopCh <-chan struct{}) {
	ticker := time.NewTicker(ps.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stopCh:
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

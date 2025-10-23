package coordinator

import (
	"context"
	"fmt"
	"sync"

	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

// RoomStateSynchronizer synchronizes room state across nodes.
type RoomStateSynchronizer struct {
	coordinator StreamCoordinator
	store       streaming.RoomStore
	mu          sync.RWMutex
	versions    map[string]int64 // roomID -> version
}

// NewRoomStateSynchronizer creates a room state synchronizer.
func NewRoomStateSynchronizer(
	coordinator StreamCoordinator,
	store streaming.RoomStore,
) *RoomStateSynchronizer {
	return &RoomStateSynchronizer{
		coordinator: coordinator,
		store:       store,
		versions:    make(map[string]int64),
	}
}

// SyncRoomMembers syncs member list across nodes.
func (rss *RoomStateSynchronizer) SyncRoomMembers(ctx context.Context, roomID string) error {
	room, err := rss.store.Get(ctx, roomID)
	if err != nil {
		return fmt.Errorf("failed to get room: %w", err)
	}

	members, err := rss.store.GetMembers(ctx, roomID)
	if err != nil {
		return fmt.Errorf("failed to get members: %w", err)
	}

	memberIDs := make([]string, len(members))
	for i, member := range members {
		memberIDs[i] = member.GetUserID()
	}

	// Create state
	state := &RoomState{
		RoomID:  roomID,
		Members: memberIDs,
		Settings: map[string]any{
			"name":        room.GetName(),
			"description": room.GetDescription(),
			"max_members": room.GetMaxMembers(),
			"private":     room.IsPrivate(),
		},
		Version: rss.getNextVersion(roomID),
	}

	return rss.coordinator.SyncRoomState(ctx, roomID, state)
}

// SyncRoomSettings syncs room configuration.
func (rss *RoomStateSynchronizer) SyncRoomSettings(ctx context.Context, roomID string) error {
	return rss.SyncRoomMembers(ctx, roomID) // Full sync for now
}

// HandleMemberJoin broadcasts join event.
func (rss *RoomStateSynchronizer) HandleMemberJoin(ctx context.Context, roomID, userID string) error {
	// Sync full room state
	return rss.SyncRoomMembers(ctx, roomID)
}

// HandleMemberLeave broadcasts leave event.
func (rss *RoomStateSynchronizer) HandleMemberLeave(ctx context.Context, roomID, userID string) error {
	// Sync full room state
	return rss.SyncRoomMembers(ctx, roomID)
}

// HandleRoomStateUpdate handles incoming room state updates.
func (rss *RoomStateSynchronizer) HandleRoomStateUpdate(ctx context.Context, state *RoomState) error {
	rss.mu.Lock()
	currentVersion := rss.versions[state.RoomID]
	rss.mu.Unlock()

	// Check version for conflict detection
	if state.Version <= currentVersion {
		// Stale update, ignore
		return nil
	}

	// Update local store
	room, err := rss.store.Get(ctx, state.RoomID)
	if err != nil {
		return fmt.Errorf("failed to get room: %w", err)
	}

	// Apply updates
	updates := make(map[string]any)
	if name, ok := state.Settings["name"].(string); ok {
		updates["name"] = name
	}
	if desc, ok := state.Settings["description"].(string); ok {
		updates["description"] = desc
	}
	if private, ok := state.Settings["private"].(bool); ok {
		if private {
			_ = room.SetPrivate(ctx, true)
		} else {
			_ = room.SetPrivate(ctx, false)
		}
	}

	if len(updates) > 0 {
		if err := room.Update(ctx, updates); err != nil {
			return fmt.Errorf("failed to update room: %w", err)
		}
	}

	// Update version
	rss.mu.Lock()
	rss.versions[state.RoomID] = state.Version
	rss.mu.Unlock()

	return nil
}

func (rss *RoomStateSynchronizer) getNextVersion(roomID string) int64 {
	rss.mu.Lock()
	defer rss.mu.Unlock()

	version := rss.versions[roomID] + 1
	rss.versions[roomID] = version
	return version
}

// ResolveConflict resolves state conflicts using last-write-wins.
func (rss *RoomStateSynchronizer) ResolveConflict(ctx context.Context, local, remote *RoomState) (*RoomState, error) {
	// Last-write-wins based on timestamp
	if remote.UpdatedAt.After(local.UpdatedAt) {
		return remote, nil
	}
	return local, nil
}

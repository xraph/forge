package coordinator

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/xraph/forge/extensions/streaming/backends/local"
	streaming "github.com/xraph/forge/extensions/streaming/internal"
)

var errCoord = errors.New("coordinator failure")

// recordingCoordinator implements StreamCoordinator and captures what the
// synchronizers push to it. Only the methods the synchronizers call carry
// behavior; the rest satisfy the interface.
type recordingCoordinator struct {
	mu sync.Mutex

	roomStates []*RoomState
	presences  []*streaming.UserPresence

	syncRoomErr     error
	syncPresenceErr error
}

func (c *recordingCoordinator) BroadcastToNode(ctx context.Context, nodeID string, msg *streaming.Message) error {
	return nil
}

func (c *recordingCoordinator) BroadcastToUser(ctx context.Context, userID string, msg *streaming.Message) error {
	return nil
}

func (c *recordingCoordinator) BroadcastToRoom(ctx context.Context, roomID string, msg *streaming.Message) error {
	return nil
}

func (c *recordingCoordinator) BroadcastGlobal(ctx context.Context, msg *streaming.Message) error {
	return nil
}

func (c *recordingCoordinator) SyncPresence(ctx context.Context, presence *streaming.UserPresence) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.presences = append(c.presences, presence)

	return c.syncPresenceErr
}

func (c *recordingCoordinator) SyncRoomState(ctx context.Context, roomID string, state *RoomState) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.roomStates = append(c.roomStates, state)

	return c.syncRoomErr
}

func (c *recordingCoordinator) GetUserNodes(ctx context.Context, userID string) ([]string, error) {
	return nil, nil
}

func (c *recordingCoordinator) GetRoomNodes(ctx context.Context, roomID string) ([]string, error) {
	return nil, nil
}

func (c *recordingCoordinator) RegisterNode(ctx context.Context, nodeID string, metadata map[string]any) error {
	return nil
}

func (c *recordingCoordinator) UnregisterNode(ctx context.Context, nodeID string) error { return nil }

func (c *recordingCoordinator) Subscribe(ctx context.Context, handler MessageHandler) error {
	return nil
}

func (c *recordingCoordinator) Start(ctx context.Context) error { return nil }

func (c *recordingCoordinator) Stop(ctx context.Context) error { return nil }

func (c *recordingCoordinator) roomStateCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.roomStates)
}

func (c *recordingCoordinator) lastRoomState() *RoomState {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.roomStates) == 0 {
		return nil
	}

	return c.roomStates[len(c.roomStates)-1]
}

func (c *recordingCoordinator) presenceCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.presences)
}

// --- Room state synchronizer -----------------------------------------------

func seedRoomWithMembers(t *testing.T, store streaming.RoomStore, roomID string, userIDs ...string) {
	t.Helper()

	ctx := context.Background()

	room := local.NewRoom(streaming.RoomOptions{ID: roomID, Name: roomID, Description: "desc", Owner: "owner"})
	if err := store.Create(ctx, room); err != nil {
		t.Fatalf("Create(%s): %v", roomID, err)
	}

	for _, userID := range userIDs {
		member := local.NewLocalMember(streaming.MemberOptions{UserID: userID, Role: streaming.RoleMember})
		if err := store.AddMember(ctx, roomID, member); err != nil {
			t.Fatalf("AddMember(%s): %v", userID, err)
		}
	}
}

func TestRoomStateSynchronizer_SyncRoomMembers(t *testing.T) {
	ctx := context.Background()

	store := local.NewRoomStore()
	seedRoomWithMembers(t, store, "room-1", "alice", "bob")

	coord := &recordingCoordinator{}
	syncer := NewRoomStateSynchronizer(coord, store)

	if err := syncer.SyncRoomMembers(ctx, "room-1"); err != nil {
		t.Fatalf("SyncRoomMembers: %v", err)
	}

	state := coord.lastRoomState()
	if state == nil {
		t.Fatal("no room state was pushed to the coordinator")
	}

	if state.RoomID != "room-1" {
		t.Errorf("RoomID = %q, want room-1", state.RoomID)
	}

	if len(state.Members) != 2 {
		t.Errorf("Members = %v, want two entries", state.Members)
	}

	if state.Settings["name"] != "room-1" {
		t.Errorf("Settings[name] = %v, want room-1", state.Settings["name"])
	}

	if state.Settings["description"] != "desc" {
		t.Errorf("Settings[description] = %v, want desc", state.Settings["description"])
	}

	if state.Version != 1 {
		t.Errorf("Version = %d, want 1 for the first sync", state.Version)
	}
}

func TestRoomStateSynchronizer_VersionIncrementsPerRoom(t *testing.T) {
	ctx := context.Background()

	store := local.NewRoomStore()
	seedRoomWithMembers(t, store, "room-1", "alice")
	seedRoomWithMembers(t, store, "room-2", "bob")

	coord := &recordingCoordinator{}
	syncer := NewRoomStateSynchronizer(coord, store)

	for range 3 {
		if err := syncer.SyncRoomMembers(ctx, "room-1"); err != nil {
			t.Fatalf("SyncRoomMembers: %v", err)
		}
	}

	if got := coord.lastRoomState().Version; got != 3 {
		t.Errorf("room-1 version = %d, want 3", got)
	}

	// Versions are per-room, so room-2 starts at 1 rather than continuing.
	if err := syncer.SyncRoomMembers(ctx, "room-2"); err != nil {
		t.Fatalf("SyncRoomMembers: %v", err)
	}

	if got := coord.lastRoomState().Version; got != 1 {
		t.Errorf("room-2 version = %d, want 1", got)
	}
}

func TestRoomStateSynchronizer_SyncErrors(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		roomID     string
		coordinate *recordingCoordinator
		seed       bool
	}{
		{
			name:       "missing room",
			roomID:     "nope",
			coordinate: &recordingCoordinator{},
		},
		{
			name:       "coordinator rejects the sync",
			roomID:     "room-1",
			coordinate: &recordingCoordinator{syncRoomErr: errCoord},
			seed:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := local.NewRoomStore()
			if tt.seed {
				seedRoomWithMembers(t, store, "room-1", "alice")
			}

			syncer := NewRoomStateSynchronizer(tt.coordinate, store)

			if err := syncer.SyncRoomMembers(ctx, tt.roomID); err == nil {
				t.Error("SyncRoomMembers = nil, want an error")
			}
		})
	}
}

func TestRoomStateSynchronizer_MemberEventsTriggerAFullSync(t *testing.T) {
	// HandleMemberJoin and HandleMemberLeave both delegate to a full state sync
	// rather than sending a delta; pinned so a move to deltas is deliberate.
	ctx := context.Background()

	store := local.NewRoomStore()
	seedRoomWithMembers(t, store, "room-1", "alice")

	coord := &recordingCoordinator{}
	syncer := NewRoomStateSynchronizer(coord, store)

	if err := syncer.HandleMemberJoin(ctx, "room-1", "bob"); err != nil {
		t.Fatalf("HandleMemberJoin: %v", err)
	}

	if err := syncer.HandleMemberLeave(ctx, "room-1", "bob"); err != nil {
		t.Fatalf("HandleMemberLeave: %v", err)
	}

	if err := syncer.SyncRoomSettings(ctx, "room-1"); err != nil {
		t.Fatalf("SyncRoomSettings: %v", err)
	}

	if got := coord.roomStateCount(); got != 3 {
		t.Errorf("coordinator received %d state syncs, want 3", got)
	}
}

func TestRoomStateSynchronizer_HandleRoomStateUpdateAppliesPrivacy(t *testing.T) {
	// Privacy is applied through the real LocalRoom setter rather than Update,
	// so a settings payload carrying only "private" does land.
	ctx := context.Background()

	store := local.NewRoomStore()
	seedRoomWithMembers(t, store, "room-1", "alice")

	syncer := NewRoomStateSynchronizer(&recordingCoordinator{}, store)

	state := &RoomState{
		RoomID:   "room-1",
		Version:  1,
		Settings: map[string]any{"private": true},
	}

	if err := syncer.HandleRoomStateUpdate(ctx, state); err != nil {
		t.Fatalf("HandleRoomStateUpdate: %v", err)
	}

	room, err := store.Get(ctx, "room-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !room.IsPrivate() {
		t.Error("IsPrivate = false, want true")
	}
}

func TestRoomStateSynchronizer_HandleRoomStateUpdateIgnoresStaleVersions(t *testing.T) {
	// Version gating is exercised with a privacy-only payload, since a payload
	// carrying a name would fail on LocalRoom.Update (see
	// TestRoomStateSynchronizer_HandleRoomStateUpdateFailsOnTheLocalBackend).
	ctx := context.Background()

	store := local.NewRoomStore()
	seedRoomWithMembers(t, store, "room-1", "alice")

	syncer := NewRoomStateSynchronizer(&recordingCoordinator{}, store)

	apply := func(t *testing.T, version int64, private bool) {
		t.Helper()

		state := &RoomState{
			RoomID:   "room-1",
			Version:  version,
			Settings: map[string]any{"private": private},
		}

		if err := syncer.HandleRoomStateUpdate(ctx, state); err != nil {
			t.Fatalf("HandleRoomStateUpdate(v%d): %v", version, err)
		}
	}

	isPrivate := func(t *testing.T) bool {
		t.Helper()

		room, err := store.Get(ctx, "room-1")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}

		return room.IsPrivate()
	}

	apply(t, 5, true)

	if !isPrivate(t) {
		t.Fatal("the version-5 update did not apply")
	}

	tests := []struct {
		name        string
		version     int64
		private     bool
		wantPrivate bool
	}{
		{name: "older version is ignored", version: 3, private: false, wantPrivate: true},
		{name: "equal version is ignored", version: 5, private: false, wantPrivate: true},
		{name: "newer version is applied", version: 6, private: false, wantPrivate: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apply(t, tt.version, tt.private)

			if got := isPrivate(t); got != tt.wantPrivate {
				t.Errorf("IsPrivate = %v, want %v", got, tt.wantPrivate)
			}
		})
	}
}

func TestRoomStateSynchronizer_HandleRoomStateUpdate(t *testing.T) {
	ctx := context.Background()

	store := local.NewRoomStore()
	seedRoomWithMembers(t, store, "room-1", "alice")

	syncer := NewRoomStateSynchronizer(&recordingCoordinator{}, store)

	state := &RoomState{
		RoomID:  "room-1",
		Version: 5,
		Settings: map[string]any{
			"name":        "renamed",
			"description": "new description",
			"private":     true,
		},
	}

	if err := syncer.HandleRoomStateUpdate(ctx, state); err != nil {
		t.Fatalf("HandleRoomStateUpdate: %v", err)
	}

	room, err := store.Get(ctx, "room-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if room.GetName() != "renamed" {
		t.Errorf("name = %q, want renamed", room.GetName())
	}

	if room.GetDescription() != "new description" {
		t.Errorf("description = %q, want %q", room.GetDescription(), "new description")
	}

	if !room.IsPrivate() {
		t.Error("IsPrivate = false, want true after the update")
	}
}

func TestRoomStateSynchronizer_HandleRoomStateUpdateUnknownRoom(t *testing.T) {
	syncer := NewRoomStateSynchronizer(&recordingCoordinator{}, local.NewRoomStore())

	state := &RoomState{RoomID: "nope", Version: 1, Settings: map[string]any{"name": "x"}}

	if err := syncer.HandleRoomStateUpdate(context.Background(), state); err == nil {
		t.Error("HandleRoomStateUpdate for an unknown room = nil, want an error")
	}
}

func TestRoomStateSynchronizer_ResolveConflict(t *testing.T) {
	ctx := context.Background()
	syncer := NewRoomStateSynchronizer(&recordingCoordinator{}, local.NewRoomStore())

	earlier := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)

	tests := []struct {
		name       string
		local      *RoomState
		remote     *RoomState
		wantRoomID string
	}{
		{
			name:       "remote is newer",
			local:      &RoomState{RoomID: "local", UpdatedAt: earlier},
			remote:     &RoomState{RoomID: "remote", UpdatedAt: later},
			wantRoomID: "remote",
		},
		{
			name:       "local is newer",
			local:      &RoomState{RoomID: "local", UpdatedAt: later},
			remote:     &RoomState{RoomID: "remote", UpdatedAt: earlier},
			wantRoomID: "local",
		},
		{
			name:       "ties go to local",
			local:      &RoomState{RoomID: "local", UpdatedAt: earlier},
			remote:     &RoomState{RoomID: "remote", UpdatedAt: earlier},
			wantRoomID: "local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := syncer.ResolveConflict(ctx, tt.local, tt.remote)
			if err != nil {
				t.Fatalf("ResolveConflict: %v", err)
			}

			if got.RoomID != tt.wantRoomID {
				t.Errorf("winner = %q, want %q", got.RoomID, tt.wantRoomID)
			}
		})
	}
}

func TestRoomStateSynchronizer_ConcurrentSync(t *testing.T) {
	ctx := context.Background()

	store := local.NewRoomStore()
	seedRoomWithMembers(t, store, "room-1", "alice")

	syncer := NewRoomStateSynchronizer(&recordingCoordinator{}, store)

	var wg sync.WaitGroup

	for range 8 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for i := range 25 {
				_ = syncer.SyncRoomMembers(ctx, "room-1")
				_ = syncer.HandleRoomStateUpdate(ctx, &RoomState{
					RoomID:   "room-1",
					Version:  int64(i),
					Settings: map[string]any{"name": "room-1"},
				})
			}
		}()
	}

	wg.Wait()
}

// --- Presence synchronizer -------------------------------------------------

func TestPresenceSynchronizer_SyncUserPresence(t *testing.T) {
	ctx := context.Background()

	store := local.NewPresenceStore()
	if err := store.Set(ctx, "alice", &streaming.UserPresence{
		UserID: "alice",
		Status: streaming.StatusOnline,
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	coord := &recordingCoordinator{}
	syncer := NewPresenceSynchronizer(coord, store, time.Minute)

	if err := syncer.SyncUserPresence(ctx, "alice"); err != nil {
		t.Fatalf("SyncUserPresence: %v", err)
	}

	if got := coord.presenceCount(); got != 1 {
		t.Errorf("coordinator received %d presence syncs, want 1", got)
	}
}

func TestPresenceSynchronizer_SyncUnknownUser(t *testing.T) {
	coord := &recordingCoordinator{}
	syncer := NewPresenceSynchronizer(coord, local.NewPresenceStore(), time.Minute)

	if err := syncer.SyncUserPresence(context.Background(), "ghost"); err == nil {
		t.Error("SyncUserPresence for an unknown user = nil, want the store's not-found error")
	}

	if got := coord.presenceCount(); got != 0 {
		t.Errorf("coordinator received %d presence syncs, want 0", got)
	}
}

func TestPresenceSynchronizer_OnPresenceChange(t *testing.T) {
	ctx := context.Background()

	store := local.NewPresenceStore()
	if err := store.Set(ctx, "alice", &streaming.UserPresence{
		UserID: "alice",
		Status: streaming.StatusAway,
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	coord := &recordingCoordinator{}
	syncer := NewPresenceSynchronizer(coord, store, time.Minute)

	event := &streaming.PresenceEvent{UserID: "alice", Status: streaming.StatusAway}

	if err := syncer.OnPresenceChange(ctx, event); err != nil {
		t.Fatalf("OnPresenceChange: %v", err)
	}

	if got := coord.presenceCount(); got != 1 {
		t.Errorf("coordinator received %d presence syncs, want 1", got)
	}
}

func TestPresenceSynchronizer_HandlePresenceUpdateWritesLocally(t *testing.T) {
	ctx := context.Background()

	store := local.NewPresenceStore()
	syncer := NewPresenceSynchronizer(&recordingCoordinator{}, store, time.Minute)

	incoming := &streaming.UserPresence{UserID: "remote-user", Status: streaming.StatusOnline}

	if err := syncer.HandlePresenceUpdate(ctx, incoming); err != nil {
		t.Fatalf("HandlePresenceUpdate: %v", err)
	}

	got, err := store.Get(ctx, "remote-user")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got.Status != streaming.StatusOnline {
		t.Errorf("status = %q, want online", got.Status)
	}
}

func TestPresenceSynchronizer_SyncLoopPushesOnlineUsers(t *testing.T) {
	ctx := context.Background()

	store := local.NewPresenceStore()
	for _, u := range []string{"alice", "bob"} {
		if err := store.Set(ctx, u, &streaming.UserPresence{UserID: u, Status: streaming.StatusOnline}); err != nil {
			t.Fatalf("Set: %v", err)
		}
	}

	coord := &recordingCoordinator{}
	syncer := NewPresenceSynchronizer(coord, store, 10*time.Millisecond)

	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && coord.presenceCount() < 2 {
		time.Sleep(time.Millisecond)
	}

	if got := coord.presenceCount(); got < 2 {
		t.Fatalf("coordinator received %d presence syncs, want at least 2", got)
	}

	if err := syncer.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	settled := coord.presenceCount()
	time.Sleep(60 * time.Millisecond)

	if got := coord.presenceCount(); got > settled+2 {
		t.Errorf("sync ran %d more times after Stop, want it halted", got-settled)
	}
}

func TestPresenceSynchronizer_StartIsIdempotent(t *testing.T) {
	ctx := context.Background()

	syncer := NewPresenceSynchronizer(&recordingCoordinator{}, local.NewPresenceStore(), time.Hour)

	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}

	if err := syncer.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestPresenceSynchronizer_StopIsIdempotent(t *testing.T) {
	ctx := context.Background()

	syncer := NewPresenceSynchronizer(&recordingCoordinator{}, local.NewPresenceStore(), time.Hour)

	// Stop before Start short-circuits on the running flag rather than closing
	// the channel, so it is safe.
	if err := syncer.Stop(ctx); err != nil {
		t.Fatalf("Stop before Start: %v", err)
	}

	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := syncer.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if err := syncer.Stop(ctx); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestPresenceSynchronizer_CanRestart(t *testing.T) {
	ctx := context.Background()

	store := local.NewPresenceStore()
	if err := store.Set(ctx, "alice", &streaming.UserPresence{UserID: "alice", Status: streaming.StatusOnline}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	coord := &recordingCoordinator{}
	syncer := NewPresenceSynchronizer(coord, store, 10*time.Millisecond)

	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := syncer.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	before := coord.presenceCount()

	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("restart: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if coord.presenceCount() <= before {
		t.Error("the restarted sync loop produced no syncs")
	}

	if err := syncer.Stop(ctx); err != nil {
		t.Fatalf("final Stop: %v", err)
	}
}

func TestPresenceSynchronizer_SyncLoopStopsWithTheContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	store := local.NewPresenceStore()
	if err := store.Set(ctx, "alice", &streaming.UserPresence{UserID: "alice", Status: streaming.StatusOnline}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	coord := &recordingCoordinator{}
	syncer := NewPresenceSynchronizer(coord, store, 10*time.Millisecond)

	if err := syncer.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && coord.presenceCount() == 0 {
		time.Sleep(time.Millisecond)
	}

	cancel()

	settled := coord.presenceCount()
	time.Sleep(60 * time.Millisecond)

	if got := coord.presenceCount(); got > settled+2 {
		t.Errorf("sync ran %d more times after the context was cancelled, want it halted", got-settled)
	}
}
